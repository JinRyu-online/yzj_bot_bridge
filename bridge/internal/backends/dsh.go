package backends

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/sessions"
)

// DSH 是 DSH（DeepSeek Harness）JSON-RPC 后端：每模型一个共享进程池
// （见 dshpool.go），会话以 bot 稳定 id 寻址，进程重启后 session/resume 冷载。
type DSH struct {
	cfg   bot.Config
	store *sessions.Store
}

func NewDSH(cfg bot.Config, store *sessions.Store) *DSH {
	return &DSH{cfg: cfg, store: store}
}

// CreateSession 生成 UUID 风格的会话 id（复用 Claude 后端的生成方式）。
func (d *DSH) CreateSession() (string, error) {
	return newSessionID(), nil
}

// ClearSession 返回新 UUID，并把旧 sessionId 从池进程的 knownSessions/订阅中 evict
// （服务端无删除方法，旧记忆靠 TTL 兜底）。
func (d *DSH) ClearSession(sessionID string) (string, error) {
	sid, err := d.CreateSession()
	if err != nil {
		return "", err
	}
	if p := dshPoolGet(d.poolKey(d.resolveProvider(), d.resolveModel(""))); p != nil {
		p.evictSession(sessionID)
	}
	return sid, nil
}

// Run 同步执行一轮：冷/热分流 → 订阅事件流 → 提取 assistant text → store 持久化。
func (d *DSH) Run(prompt string, opts bot.RunOpts) bot.RunResult {
	model := d.resolveModel(opts.Model)
	provider := d.resolveProvider()

	// 会话寻址：复用桥 store（Claude 同款模式）；workspace 变更则换新会话。
	sessionID := opts.SessionID
	entryKey, hasKey := sessions.ResolveSessionKey(d.cfg, opts.OperatorOpenID)
	var prevCWD string
	if hasKey && d.store != nil {
		e := d.store.GetEntry(d.cfg.ID, entryKey)
		sessionID = e.Current
		prevCWD = e.AgentCWD
	}
	if prevCWD != "" && opts.Workspace != "" && filepath.Clean(prevCWD) != filepath.Clean(opts.Workspace) {
		sessionID = ""
	}
	if sessionID == "" {
		sessionID, _ = d.CreateSession()
	}

	// 官方会话 header 要求 cwd 为绝对路径，否则 agents.create 抛错：归一为绝对路径。
	if abs, err := filepath.Abs(opts.Workspace); err == nil {
		opts.Workspace = abs
	}
	_ = os.MkdirAll(opts.Workspace, 0o755)
	// system prompt / skills / memory 注入同 Claude 后端（claude.go L80-92）。
	if strings.TrimSpace(d.cfg.SystemPrompt) != "" {
		prompt = strings.TrimSpace(d.cfg.SystemPrompt) + "\n\n" + prompt
	}
	if opts.SkillPrompt != "" {
		prompt = prompt + opts.SkillPrompt
	} else if len(opts.Skills) > 0 {
		prompt = prompt + "\n\n优先使用这些 skills: " + strings.Join(opts.Skills, ", ")
	} else if len(d.cfg.Skills) > 0 {
		prompt = prompt + "\n\n优先使用这些 skills: " + strings.Join(d.cfg.Skills, ", ")
	}
	if opts.MemoryPrompt != "" {
		prompt = prompt + opts.MemoryPrompt
	}

	timeout := d.cfg.DSHTimeout
	if timeout <= 0 {
		timeout = 600
	}
	ctx, cancel := withRunTimeout(opts, time.Duration(timeout)*time.Second)
	defer cancel()

	key := d.poolKey(provider, model)
	proc, err := dshPoolGetOrCreate(key, d.cfg, provider, model)
	if err != nil {
		return bot.RunResult{Reply: "启动 dsh 失败: " + err.Error(), Status: "agent_missing"}
	}

	contentBlocks := []map[string]any{{"type": "text", "text": prompt}}

	// 冷热分流：会话在池中 → session/prompt（热）；不在 → resume 冷载（not found 回退创建）。
	if proc.knownSession(sessionID) {
		if proc.sessionInflight(sessionID) {
			// 上次 Run 超时弃置的残留 turn：等它跑完（事件丢弃）再发。
			if !proc.waitIdle(sessionID, time.Duration(timeout)*time.Second) {
				if proc.isDead() {
					return bot.RunResult{Reply: "dsh 进程已退出，请重试（会话会自动恢复）", Status: "error", SessionID: sessionID}
				}
				res, _ := resultFromCtx(ctx, "dsh 超时（上一轮未结束）")
				res.SessionID = sessionID
				return res
			}
		}
		ch, unsub, err := proc.subscribe(sessionID)
		if err != nil {
			return bot.RunResult{Reply: err.Error(), Status: "error", SessionID: sessionID}
		}
		defer unsub()
		if _, err := proc.call("session/prompt", map[string]any{
			"sessionId": sessionID, "cwd": opts.Workspace, "contentBlocks": contentBlocks,
		}, dshCallTimeout); err != nil {
			return d.dshErrResult(err, sessionID)
		}
		res := d.collectReply(ctx, proc, ch, sessionID)
		d.persist(sessionID, opts, hasKey, entryKey)
		return res
	}

	// 未知 sid：先 session/resume 冷载（从磁盘 header 恢复 cwd，无需参数）。
	ch, unsub, err := proc.subscribe(sessionID)
	if err != nil {
		return bot.RunResult{Reply: err.Error(), Status: "error", SessionID: sessionID}
	}
	defer unsub()
	_, err = proc.call("session/resume", map[string]any{
		"sessionId": sessionID, "contentBlocks": contentBlocks,
	}, dshCallTimeout)
	if err == nil {
		proc.markKnown(sessionID)
		res := d.collectReply(ctx, proc, ch, sessionID)
		d.persist(sessionID, opts, hasKey, entryKey)
		return res
	}
	// 仅 not-found 回退 session/prompt 创建（cwd 参数此时生效）；其他错误直接上报。
	if !strings.Contains(err.Error(), "not found") {
		return d.dshErrResult(err, sessionID)
	}
	if _, err := proc.call("session/prompt", map[string]any{
		"sessionId": sessionID, "cwd": opts.Workspace, "contentBlocks": contentBlocks,
	}, dshCallTimeout); err != nil {
		return d.dshErrResult(err, sessionID)
	}
	proc.markKnown(sessionID)
	res := d.collectReply(ctx, proc, ch, sessionID)
	d.persist(sessionID, opts, hasKey, entryKey)
	return res
}

// resolveModel 模型优先级：opts.Model → cfg.Model → dsh_model → 默认 deepseek-v4-flash。
func (d *DSH) resolveModel(override string) string {
	m := strings.TrimSpace(override)
	if m == "" {
		m = strings.TrimSpace(d.cfg.Model)
	}
	if m == "" {
		m = strings.TrimSpace(d.cfg.DSHModel)
	}
	if m == "" {
		m = "deepseek-v4-flash"
	}
	return m
}

// resolveProvider provider 默认 kuaidi100（settings.yaml 已有凭据，可配 dsh_provider）。
func (d *DSH) resolveProvider() string {
	p := strings.TrimSpace(d.cfg.DSHProvider)
	if p == "" {
		p = "kuaidi100"
	}
	return p
}

// poolKey 计算该 bot 命中的池键（provider|model|profile|nodeBin|dshEntry）。
func (d *DSH) poolKey(provider, model string) dshPoolKey {
	return dshPoolKey{
		Provider: provider,
		Model:    model,
		Profile:  firstNonEmptyStr(d.cfg.DSHProfile, "jsonrpc"),
		NodeBin:  resolveNodeBin(d.cfg.NodeBin),
		DSHEntry: resolveDSHEntry(d.cfg),
	}
}

// collectReply 订阅事件流直至本 turn 结束（turn/end）或超时/进程退出，提取 assistant text。
func (d *DSH) collectReply(ctx context.Context, proc *dshProc, ch chan *dshFrame, sessionID string) bot.RunResult {
	var text strings.Builder
	depth := 0
	sawStart := false
	reason := ""
	for {
		select {
		case <-ctx.Done():
			res, _ := resultFromCtx(ctx, "dsh 超时")
			res.SessionID = sessionID
			return res
		case <-proc.deadCh:
			return bot.RunResult{Reply: "dsh 进程已退出，请重试（会话会自动恢复）", Status: "error", SessionID: sessionID}
		case f := <-ch:
			if f == nil {
				continue
			}
			ev, _ := f.Params["event"].(map[string]any)
			if ev == nil {
				continue
			}
			switch fmt.Sprint(ev["type"]) {
			case "turn/start":
				if !sawStart {
					sawStart = true
					depth = 1
				} else {
					depth++
				}
			case "turn/end":
				if !sawStart {
					continue
				}
				depth--
				if reason == "" {
					reason = dshTurnReason(ev)
				}
				if depth <= 0 {
					return d.finishTurn(text.String(), reason, sessionID)
				}
			case "assistant/message":
				if sawStart {
					text.WriteString(extractDSHAssistant(ev))
				}
			}
		}
	}
}

// finishTurn 依据 turn/end 的 reason 归一结果状态（completed → ok，其余 → error）。
func (d *DSH) finishTurn(text, reason, sessionID string) bot.RunResult {
	reply := strings.TrimSpace(text)
	status := "ok"
	switch reason {
	case "", "completed":
		// ok
	case "max-tokens":
		status = "error"
		if reply == "" {
			reply = "dsh 会话达到 max-tokens 上限"
		}
	default:
		status = "error"
		if reply == "" {
			reply = "dsh 会话返回错误: " + reason
		}
	}
	if reply == "" {
		reply = "(空回复)"
	}
	return bot.RunResult{Reply: reply, Status: status, SessionID: sessionID}
}

// extractDSHAssistant 提取 assistant/message 事件的 text 块
// （event.data.message.content 中 type=text 的块，同官方 demo 客户端）。
func extractDSHAssistant(ev map[string]any) string {
	data, _ := ev["data"].(map[string]any)
	if data == nil {
		return ""
	}
	msg, _ := data["message"].(map[string]any)
	if msg == nil {
		return ""
	}
	content, _ := msg["content"].([]any)
	var b strings.Builder
	for _, c := range content {
		m, _ := c.(map[string]any)
		if m == nil {
			continue
		}
		if fmt.Sprint(m["type"]) == "text" {
			b.WriteString(fmt.Sprint(m["text"]))
		}
	}
	return b.String()
}

// dshTurnReason 读取 turn/end 的结束原因：data.reason 可能是字符串或 {kind: "completed"}。
func dshTurnReason(ev map[string]any) string {
	data, _ := ev["data"].(map[string]any)
	if data == nil {
		return ""
	}
	switch r := data["reason"].(type) {
	case string:
		return r
	case map[string]any:
		if k, ok := r["kind"].(string); ok {
			return k
		}
		return fmt.Sprint(r["kind"])
	default:
		return ""
	}
}

// dshErrResult 归一请求失败结果。
func (d *DSH) dshErrResult(err error, sessionID string) bot.RunResult {
	msg := err.Error()
	if strings.Contains(msg, "已退出") {
		return bot.RunResult{Reply: "dsh 进程已退出，请重试（会话会自动恢复）", Status: "error", SessionID: sessionID}
	}
	return bot.RunResult{Reply: "dsh 请求失败: " + msg, Status: "error", SessionID: sessionID}
}

// persist 复用 Claude 后端的 store 持久化模式（sessionID + AgentCWD）。
func (d *DSH) persist(sessionID string, opts bot.RunOpts, hasKey bool, entryKey string) {
	if !hasKey || d.store == nil {
		return
	}
	d.store.SetChatID(d.cfg.ID, entryKey, sessionID, opts.OperatorName)
	e := d.store.GetEntry(d.cfg.ID, entryKey)
	e.AgentCWD = opts.Workspace
	d.store.SetProject(d.cfg.ID, entryKey, e.ProjectName, e.ProjectPath)
	_ = d.store.Save()
}

// newSessionID 生成 UUID 风格会话 id（复用 Claude 后端的生成方式）。
func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// resolveNodeBin 解析 node 可执行文件：裸名走 exec.LookPath，其余原样返回。
func resolveNodeBin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "node"
	}
	if !strings.ContainsAny(raw, `/\`) {
		if p, err := exec.LookPath(raw); err == nil {
			return p
		}
	}
	return raw
}

// dshHome 返回 DSH 家目录：DSHHome 配置优先，默认 ~/.dsh。
func dshHome(cfg bot.Config) string {
	if h := strings.TrimSpace(cfg.DSHHome); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".dsh")
	}
	return filepath.Join(home, ".dsh")
}

// resolveDSHEntry 解析 DSH CLI 入口：dsh_entry 优先；否则从 DSHHome 定位 dsh 包 bin.js；
// 找不到则退回 PATH 上的 dsh（Windows 下为 .cmd，无法被 node 直调，交由启动失败报错兜底）。
func resolveDSHEntry(cfg bot.Config) string {
	if e := strings.TrimSpace(cfg.DSHEntry); e != "" {
		return e
	}
	p := filepath.Join(dshHome(cfg), "profiles", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js")
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	if lp, err := exec.LookPath("dsh"); err == nil {
		return lp
	}
	return p
}
