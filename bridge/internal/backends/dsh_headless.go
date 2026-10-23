package backends

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/paths"
	"yzj-bridge/internal/processutil"
)

// DSH headless CLI 执行：每条消息一个短进程
//
//	DeepSeek Harness.exe app.asar/.../dsh/bin.js --profile yzj-headless --patch <overlay> --json [-]
//
// stdin 传任务文本、stdout 收 NDJSON（session / text / final / status），会话用
// --session-id 跨进程续跑。这是“本地安装的 DSH 客户端自带的 headless CLI”。

type dshHeadlessOutput struct {
	SessionID string
	Cwd       string
	Final     string
	Texts     []string
	Thinking  []string
	Err       string
	Reason    string
}

// dshHeadlessParser 逐行解析 --json 输出的 NDJSON。thinking 可在事件到达时
// 立刻回调，用于边跑边写日志（不再等进程退出后一次性刷出）。
type dshHeadlessParser struct {
	out        dshHeadlessOutput
	onThinking func(text string)
}

// feed 解析一行 NDJSON；坏行忽略。
func (p *dshHeadlessParser) feed(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return
	}
	switch fmt.Sprint(obj["type"]) {
	case "session":
		p.out.SessionID = fmt.Sprint(obj["sessionId"])
		p.out.Cwd = fmt.Sprint(obj["cwd"])
	case "thinking":
		if t, ok := obj["text"].(string); ok && strings.TrimSpace(t) != "" {
			p.out.Thinking = append(p.out.Thinking, t)
			if p.onThinking != nil {
				p.onThinking(t)
			}
		}
	case "text":
		if t, ok := obj["text"].(string); ok && t != "" {
			p.out.Texts = append(p.out.Texts, t)
		}
	case "final":
		if t, ok := obj["text"].(string); ok {
			p.out.Final = t
		}
	case "status":
		if fmt.Sprint(obj["phase"]) != "turn_end" {
			return
		}
		reason, _ := obj["reason"].(map[string]any)
		p.out.Reason = fmt.Sprint(reason["kind"])
		if p.out.Reason == "error" {
			if e, ok := reason["error"].(map[string]any); ok {
				p.out.Err = fmt.Sprint(e["message"])
			}
			if p.out.Err == "" {
				p.out.Err = fmt.Sprint(reason)
			}
		}
	case "error":
		p.out.Err = fmt.Sprint(obj["message"])
	}
}

// parseDSHHeadlessOutput 解析完整 NDJSON 输出（测试与批量解析用）。
func parseDSHHeadlessOutput(data []byte) dshHeadlessOutput {
	p := &dshHeadlessParser{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		p.feed(sc.Text())
	}
	return p.out
}

// runHeadless 执行一轮 headless CLI，并持久化返回的 session id。
func (d *DSH) runHeadless(prompt string, opts bot.RunOpts, rt dshRuntime, persisted string, hasKey bool, entryKey string) bot.RunResult {
	timeout := d.cfg.DSHTimeout
	if timeout <= 0 {
		timeout = 600
	}
	ctx, cancel := withRunTimeout(opts, time.Duration(timeout)*time.Second)
	defer cancel()

	provider := d.resolveProvider()
	model := d.resolveModel(opts.Model)
	overlay, err := d.headlessOverlay(provider, model)
	if err != nil {
		return bot.RunResult{Reply: "启动 dsh headless 失败: " + err.Error(), Status: "agent_missing"}
	}

	attempt := func(sid string) (bot.RunResult, string, bool) {
		out, stderr, runErr := d.execHeadless(ctx, rt, overlay, sid, opts.Workspace, prompt)
		missing := runErr != nil && strings.TrimSpace(sid) != "" &&
			(strings.Contains(strings.ToLower(stderr), "does not exist") ||
				strings.Contains(strings.ToLower(stderr), "unknown session"))
		if missing {
			return bot.RunResult{}, "", true
		}

		reply := strings.TrimSpace(out.Final)
		if reply == "" {
			reply = strings.TrimSpace(strings.Join(out.Texts, ""))
		}
		status := "ok"
		errText := strings.TrimSpace(out.Err)
		switch {
		case ctx.Err() == context.DeadlineExceeded:
			status = "error"
			errText = fmt.Sprintf("dsh headless 超时（>%ds）", timeout)
		case ctx.Err() == context.Canceled:
			status = "interrupted"
			errText = "任务已中断"
		case runErr != nil:
			status = "error"
			if errText == "" {
				errText = compactSpace(stderr)
				if len(errText) > 800 {
					errText = errText[len(errText)-800:]
				}
			}
			if errText == "" {
				errText = runErr.Error()
			}
		case errText != "":
			status = "error"
		}
		if errText != "" && reply == "" {
			reply = "dsh headless 失败: " + errText
		}
		if reply == "" {
			reply = "(空回复)"
		}
		sidOut := out.SessionID
		if sidOut == "" {
			sidOut = sid
		}
		return bot.RunResult{Reply: reply, Status: status, SessionID: sidOut}, sidOut, false
	}

	res, sid, missing := attempt(persisted)
	if missing {
		// 磁盘上的会话不存在（被清理/换了机器）：从头开新会话，避免把用户卡死。
		res, sid, _ = attempt("")
	}
	if sid != "" {
		d.persist(sid, opts, hasKey, entryKey)
	}
	if res.SessionID == "" {
		res.SessionID = sid
	}
	return res
}

// execHeadless 启动一次 headless CLI，返回解析结果、stderr 与进程错误。
func (d *DSH) execHeadless(ctx context.Context, rt dshRuntime, overlay, sid, workspace, prompt string) (dshHeadlessOutput, string, error) {
	args := []string{rt.Entry, "--profile", rt.Profile}
	if overlay != "" {
		args = append(args, "--patch", overlay)
	}
	args = append(args, "--json")
	if sid != "" {
		args = append(args, "--session-id", sid)
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, rt.NodeBin, args...)
	processutil.HideWindow(cmd)
	if workspace != "" {
		cmd.Dir = workspace
	}
	env := os.Environ()
	if rt.ElectronAsNode {
		env = append(env, "ELECTRON_RUN_AS_NODE=1")
	}
	env = append(env, "DSH_PERMISSION_MODE=danger-full-access")
	if home := strings.TrimSpace(d.cfg.DSHHome); home != "" {
		env = append(env, "DSH_HOME="+home)
	}
	cmd.Env = env
	cmd.Stdin = strings.NewReader(prompt)

	// 逐行读取 stdout：thinking 一到就写日志，不再等进程退出后一次性刷出。
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return dshHeadlessOutput{}, "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return dshHeadlessOutput{}, "", err
	}
	parser := &dshHeadlessParser{onThinking: func(t string) {
		if s := strings.TrimSpace(compactSpace(t)); s != "" {
			log.Printf("bot=%s dsh headless thinking: %s", d.cfg.ID, clipRunes(s, 400))
		}
	}}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		parser.feed(sc.Text())
	}
	runErr := cmd.Wait()
	if scanErr := sc.Err(); scanErr != nil && runErr == nil {
		runErr = scanErr
	}
	if parser.out.Err != "" {
		log.Printf("bot=%s dsh headless: %s", d.cfg.ID, clipRunes(parser.out.Err, 400))
	}
	return parser.out, stderr.String(), runErr
}

// headlessOverlay 生成 --patch 覆盖文件，把本轮 provider/model 固定下来。
func (d *DSH) headlessOverlay(provider, model string) (string, error) {
	entries := []map[string]any{{
		"id":     "agent-default-model",
		"config": dshAgentModelConfig(provider, model, d.cfg.DSHReasoningEffort),
	}}
	data, err := yaml.Marshal(entries)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(provider + "|" + model + "|" + normalizeDSHReasoningEffort(d.cfg.DSHReasoningEffort)))
	path := filepath.Join(paths.UserDataDir(), "dsh-headless", fmt.Sprintf("overlay-%x.yml", sum[:8]))
	if err := writeFileIfChanged(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
