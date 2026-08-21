package backends

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/paths"
	"yzj-bridge/internal/processutil"
)

// dshPoolKey 标识一个 DSH 池进程：provider|model|profile|nodeBin|dshEntry。
// dshEntry 取绝对路径，因此 stub 冒烟与 live 冒烟因入口不同天然隔离。
type dshPoolKey struct {
	Provider string
	Model    string
	Profile  string
	NodeBin  string
	DSHEntry string
}

func (k dshPoolKey) String() string {
	return strings.Join([]string{k.Provider, k.Model, k.Profile, k.NodeBin, k.DSHEntry}, "|")
}

// dshRPCError 是 JSON-RPC error 对象（服务端错误响应）。
type dshRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// dshFrame 是 JSON-RPC 帧（请求/响应/通知共用，均按行分隔 NDJSON）。
type dshFrame struct {
	ID     int64          `json:"id,omitempty"`
	Method string         `json:"method,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	Result map[string]any `json:"result,omitempty"`
	Error  *dshRPCError   `json:"error,omitempty"`
}

// 包级共享进程池：每 poolKey（= provider|model|profile|node|entry）一个常驻进程。
var (
	dshPoolMu    sync.Mutex
	dshPool      = map[dshPoolKey]*dshProc{}
	dshSpawnMu   sync.Mutex // 串行化 spawn，避免同 key 双创建
	dshPoolWarm  int        // 池内各进程 maxWarm 的最大值（0 = 默认 3）
	dshReaperOne sync.Once
)

const (
	dshReaperInterval = 30 * time.Second // 回收扫描周期
	dshCallTimeout    = 30 * time.Second // 单个 JSON-RPC 请求（非 turn）超时
	dshStopGrace      = 2 * time.Second  // shutdown 宽限，超时兜底 kill
)

// dshProc 是一个 DSH JSON-RPC 池进程：stdin 写帧、stdout 常驻 reader 分发事件。
type dshProc struct {
	key dshPoolKey
	cmd *exec.Cmd
	ttl time.Duration // 空闲回收阈值（来自配置 dsh_ttl_seconds）

	stdinMu sync.Mutex
	stdin   io.WriteCloser
	stdout  io.ReadCloser

	mu         sync.Mutex
	dead       bool
	deadCh     chan struct{}
	known      map[string]bool                 // sessionId 是否在本进程内有活会话
	inflight   map[string]bool                 // sessionId 是否有未结束的 turn
	lastActive time.Time
	nextID     int64
	pending    map[int64]chan *dshFrame
	subs       map[string]map[chan *dshFrame]struct{}
}

// resetPoolForTest 关闭并清空整个池（测试用例间隔离用）。
func resetPoolForTest() {
	dshPoolMu.Lock()
	procs := make([]*dshProc, 0, len(dshPool))
	for k, p := range dshPool {
		delete(dshPool, k)
		procs = append(procs, p)
	}
	dshPoolWarm = 0
	dshPoolMu.Unlock()
	for _, p := range procs {
		p.stop(dshStopGrace)
	}
}

// dshPoolGetOrCreate 取或建池进程（double-check + 全局 spawn 串行化）。
func dshPoolGetOrCreate(key dshPoolKey, cfg bot.Config, provider, model string) (*dshProc, error) {
	dshEnsureReaper()
	dshPoolMu.Lock()
	if p, ok := dshPool[key]; ok && !p.isDead() {
		dshPoolMu.Unlock()
		return p, nil
	}
	dshPoolMu.Unlock()

	dshSpawnMu.Lock()
	defer dshSpawnMu.Unlock()

	dshPoolMu.Lock()
	if p, ok := dshPool[key]; ok && !p.isDead() {
		dshPoolMu.Unlock()
		return p, nil
	}
	dshPoolMu.Unlock()

	p, err := spawnDshProc(key, cfg, provider, model)
	if err != nil {
		return nil, err
	}
	dshPoolMu.Lock()
	dshPool[key] = p
	if cfg.DSHMaxWarm > dshPoolWarm {
		dshPoolWarm = cfg.DSHMaxWarm
	}
	dshPoolMu.Unlock()
	return p, nil
}

// dshPoolGet 取池进程（不存在或已死返回 nil）。
func dshPoolGet(key dshPoolKey) *dshProc {
	dshPoolMu.Lock()
	defer dshPoolMu.Unlock()
	if p, ok := dshPool[key]; ok && !p.isDead() {
		return p
	}
	return nil
}

// spawnDshProc 启动 node 直调 bin.js + 握手 initialize。失败视为 spawn 失败（进程已清理）。
func spawnDshProc(key dshPoolKey, cfg bot.Config, provider, model string) (*dshProc, error) {
	// 中性工作目录：非任何 bot workspace，session 的 cwd 由协议参数（header.cwd）决定。
	neutral := filepath.Join(paths.UserDataDir(), "dsh-pool")
	if err := os.MkdirAll(neutral, 0o755); err != nil {
		return nil, err
	}
	cmd := exec.Command(key.NodeBin, key.DSHEntry, "--profile", key.Profile)
	processutil.HideWindow(cmd)
	cmd.Dir = neutral
	env := os.Environ()
	env = append(env, "DSH_PERMISSION_MODE=danger-full-access")
	if home := strings.TrimSpace(cfg.DSHHome); home != "" {
		env = append(env, "DSH_HOME="+home)
	}
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ttl := time.Duration(cfg.DSHTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 300 * time.Second
	}
	p := &dshProc{
		key:        key,
		cmd:        cmd,
		ttl:        ttl,
		stdin:      stdin,
		stdout:     stdout,
		deadCh:     make(chan struct{}),
		known:      map[string]bool{},
		inflight:   map[string]bool{},
		lastActive: time.Now(),
		pending:    map[int64]chan *dshFrame{},
		subs:       map[string]map[chan *dshFrame]struct{}{},
	}
	go p.readLoop()
	// 握手：initialize 是 Loader 就绪边界；30s 内未回 serverInfo 视为 spawn 失败。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := p.callCtx(ctx, "initialize", map[string]any{
		"cwd": neutral, "provider": provider, "model": model,
	}); err != nil {
		p.kill()
		return nil, fmt.Errorf("initialize 握手失败: %w", err)
	}
	return p, nil
}

// readLoop 常驻 stdout reader：NDJSON 逐行解析，session.event 按 sessionId 分发。
// stdin EOF / 进程 exit → 标记 dead 并通知全部等待者。
func (p *dshProc) readLoop() {
	defer func() {
		_ = p.cmd.Wait() // 回收进程资源
		p.markDead()
	}()
	sc := bufio.NewScanner(p.stdout)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var f dshFrame
		if err := json.Unmarshal(line, &f); err != nil {
			// stdout 理论只承载 JSON-RPC 帧，防御性跳过非 JSON 行。
			continue
		}
		p.handleFrame(&f)
	}
	// 单行超 8MB（bufio.ErrTooLong 等非 EOF 错误）会让 server 写 stdout 被卡住、
	// defer 里 cmd.Wait() 永久阻塞：先 kill 解除 Wait，再进入 defer 收尾 markDead。
	if err := sc.Err(); err != nil {
		p.kill()
	}
}

// handleFrame 分发一帧：session.event → 订阅者；id 响应 → pending 调用者。
func (p *dshProc) handleFrame(f *dshFrame) {
	p.mu.Lock()
	p.lastActive = time.Now()
	if f.Method == "session.event" {
		sid, _ := f.Params["sessionId"].(string)
		if ev, ok := f.Params["event"].(map[string]any); ok {
			switch fmt.Sprint(ev["type"]) {
			case "turn/start":
				p.inflight[sid] = true
			case "turn/end":
				delete(p.inflight, sid)
			}
		}
		chans := make([]chan *dshFrame, 0, len(p.subs[sid]))
		for ch := range p.subs[sid] {
			chans = append(chans, ch)
		}
		p.mu.Unlock()
		for _, ch := range chans {
			select {
			case ch <- f:
			default: // 无订阅者 / 订阅者积压：丢弃，不阻塞 reader。
			}
		}
		return
	}
	if f.ID != 0 {
		if ch, ok := p.pending[f.ID]; ok {
			delete(p.pending, f.ID)
			p.mu.Unlock()
			select {
			case ch <- f:
			default:
			}
			return
		}
	}
	p.mu.Unlock()
}

// markDead 标记进程死亡：清 known/inflight，通知全部 pending 等待者（nil 帧）。
func (p *dshProc) markDead() {
	p.mu.Lock()
	if p.dead {
		p.mu.Unlock()
		return
	}
	p.dead = true
	close(p.deadCh)
	p.known = map[string]bool{}
	p.inflight = map[string]bool{}
	for id, ch := range p.pending {
		delete(p.pending, id)
		select {
		case ch <- nil:
		default:
		}
	}
	p.mu.Unlock()
}

func (p *dshProc) isDead() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dead
}

func (p *dshProc) knownSession(sid string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.known[sid]
}

func (p *dshProc) sessionInflight(sid string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inflight[sid]
}

func (p *dshProc) markKnown(sid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.known[sid] = true
}

// evictSession 把 sessionId 从 knownSessions/订阅/inflight 中移除（服务端无删除方法，旧记忆靠 TTL 兜底）。
func (p *dshProc) evictSession(sid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.known, sid)
	delete(p.inflight, sid)
	delete(p.subs, sid)
}

func (p *dshProc) lastActiveTime() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastActive
}

func (p *dshProc) registerPending() (int64, chan *dshFrame) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	id := p.nextID
	ch := make(chan *dshFrame, 1)
	p.pending[id] = ch
	return id, ch
}

func (p *dshProc) unregisterPending(id int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pending, id)
}

// call 发一个 request 并等待响应（超时 timeout）。
func (p *dshProc) call(method string, params map[string]any, timeout time.Duration) (*dshFrame, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return p.callCtx(ctx, method, params)
}

// callCtx 发一个 request 并等待响应（ctx 控制超时/取消）。
func (p *dshProc) callCtx(ctx context.Context, method string, params map[string]any) (*dshFrame, error) {
	if p.isDead() {
		return nil, fmt.Errorf("dsh 进程已退出")
	}
	id, ch := p.registerPending()
	data, err := json.Marshal(dshFrame{ID: id, Method: method, Params: params})
	if err != nil {
		p.unregisterPending(id)
		return nil, err
	}
	p.stdinMu.Lock()
	_, werr := p.stdin.Write(append(data, '\n'))
	p.stdinMu.Unlock()
	if werr != nil {
		p.unregisterPending(id)
		p.markDead()
		return nil, fmt.Errorf("写入 dsh stdin 失败: %w", werr)
	}
	p.mu.Lock()
	p.lastActive = time.Now()
	p.mu.Unlock()
	select {
	case resp := <-ch:
		if resp == nil {
			return nil, fmt.Errorf("dsh 进程已退出")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("dsh %s 失败: %s", method, resp.Error.Message)
		}
		return resp, nil
	case <-ctx.Done():
		p.unregisterPending(id)
		return nil, ctx.Err()
	case <-p.deadCh:
		p.unregisterPending(id)
		return nil, fmt.Errorf("dsh 进程已退出")
	}
}

// subscribe 订阅某 sessionId 的事件流；返回的取消函数必须在 Run 结束调用。
func (p *dshProc) subscribe(sid string) (chan *dshFrame, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dead {
		return nil, nil, fmt.Errorf("dsh 进程已退出")
	}
	ch := make(chan *dshFrame, 512)
	if p.subs[sid] == nil {
		p.subs[sid] = map[chan *dshFrame]struct{}{}
	}
	p.subs[sid][ch] = struct{}{}
	unsub := func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if s := p.subs[sid]; s != nil {
			delete(s, ch)
			if len(s) == 0 {
				delete(p.subs, sid)
			}
		}
	}
	return ch, unsub, nil
}

// waitIdle 等待某会话的残留 turn 结束（其事件已被丢弃）；超时或进程死亡返回 false。
func (p *dshProc) waitIdle(sid string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		p.mu.Lock()
		busy := p.inflight[sid]
		dead := p.dead
		p.mu.Unlock()
		if dead {
			return false
		}
		if !busy {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stop 优雅停止：发 shutdown，宽限 grace 内等进程退出；失败兜底 kill。
func (p *dshProc) stop(grace time.Duration) {
	if p.isDead() {
		return
	}
	if _, err := p.call("shutdown", map[string]any{}, grace); err == nil {
		select {
		case <-p.deadCh:
			return
		case <-time.After(grace):
		}
	}
	p.kill()
}

// kill 兜底终止：Windows 用 taskkill /T /F 杀进程树，其他平台 Kill()。
func (p *dshProc) kill() {
	if p.isDead() {
		return
	}
	if runtime.GOOS == "windows" {
		c := exec.Command("taskkill", "/PID", strconv.Itoa(p.cmd.Process.Pid), "/T", "/F")
		_ = c.Run()
	} else {
		_ = p.cmd.Process.Kill()
	}
}

// dshEnsureReaper 启动全局回收 goroutine（只启动一次）。
func dshEnsureReaper() {
	dshReaperOne.Do(func() {
		go func() {
			for {
				time.Sleep(dshReaperInterval)
				dshReap()
			}
		}()
	})
}

// dshReap 周期回收：dead 清理；idle 超 TTL 的进程 shutdown；超 maxWarm 按 LRU 提前回收。
// 所有带 inflight 会话的进程跳过（不打断进行中的 turn）。
func dshReap() {
	now := time.Now()
	var toStop []*dshProc
	var alive []*dshProc

	dshPoolMu.Lock()
	for k, p := range dshPool {
		if p.isDead() {
			delete(dshPool, k)
			continue
		}
		p.mu.Lock()
		busy := len(p.inflight) > 0
		idle := now.Sub(p.lastActive)
		ttl := p.ttl
		p.mu.Unlock()
		if !busy && idle >= ttl {
			toStop = append(toStop, p)
			delete(dshPool, k)
			continue
		}
		alive = append(alive, p)
	}
	maxWarm := dshPoolWarm
	if maxWarm <= 0 {
		maxWarm = 3
	}
	if len(alive) > maxWarm {
		sort.Slice(alive, func(i, j int) bool {
			return alive[i].lastActiveTime().Before(alive[j].lastActiveTime())
		})
		trim := len(alive) - maxWarm
		for i := 0; i < trim; i++ {
			p := alive[i]
			p.mu.Lock()
			busy := len(p.inflight) > 0
			p.mu.Unlock()
			if busy {
				continue
			}
			toStop = append(toStop, p)
			delete(dshPool, p.key)
		}
	}
	dshPoolMu.Unlock()

	for _, p := range toStop {
		go p.stop(dshStopGrace)
	}
}
