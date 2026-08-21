package commands

import (
	"fmt"
	"regexp"
	"strings"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/registry"
)

var (
	cmdFlagRe  = regexp.MustCompile(`(?i)(?:^|\s)(?:--|/)(clear|new|help|status|whoami|plan|ask|agent|prompt|stop|abort|interrupt|停止|中断|jobs|queue|list|tasks|任务|队列|列表|memory|forget)(?:\s|$)`)
	stopFlagRe = regexp.MustCompile(`(?i)(?:^|\s)(?:--|/)(stop|abort|interrupt|停止|中断)(?:\s|$)`)
	jobsFlagRe = regexp.MustCompile(`(?i)(?:^|\s)(?:--|/)(jobs|queue|list|tasks|任务|队列|列表)(?:\s|$)`)
	memoryRe   = regexp.MustCompile(`(?i)(?:--|/)memory(?:\s+(off|on|show))?`)
	forgetRe   = regexp.MustCompile(`(?i)(?:--|/)forget(?:\s|$)`)
	projectRe  = regexp.MustCompile(`(?i)(?:--|/)project(?:\s+(\S+))?`)
	modelRe    = regexp.MustCompile(`(?i)(?:--|/)model(?:\s+(\S+))`)
	atRe       = regexp.MustCompile(`(?i)@([^\s@]+)\b\s*`)
)

type Result struct {
	Reply     string
	RestText  string
	Overrides map[string]string
	Handled   bool // pure command, no agent
}

func StripBotMention(text string, b *bot.Bot, extra []string, onlySelf bool) string {
	names := []string{}
	if b != nil {
		names = append(names, b.Config.Name, b.Config.ID, b.Config.RoleID)
	}
	if !onlySelf {
		names = append(names, "Fairy")
		names = append(names, extra...)
	}
	out := text
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		re := regexp.MustCompile(`(?i)@` + regexp.QuoteMeta(n) + `\b\s*`)
		out = re.ReplaceAllString(out, "")
	}
	if !onlySelf {
		out = regexp.MustCompile(`(?m)^@\S+\s*`).ReplaceAllString(out, "")
	}
	return strings.TrimSpace(out)
}

func Parse(text string, b *bot.Bot, reg *registry.Registry) Result {
	res := Result{Overrides: map[string]string{}, RestText: text}
	// Interrupt is always available so a long engine run can be aborted
	// even when other slash commands are disabled.
	if stopFlagRe.MatchString(text) {
		res.Overrides["stop"] = "1"
		res.Handled = true
		res.RestText = ""
		return res
	}
	if jobsFlagRe.MatchString(text) {
		res.Overrides["jobs"] = "1"
		res.Handled = true
		res.RestText = ""
		return res
	}
	if b == nil || !b.Config.CommandsEnabled {
		return res
	}
	lower := strings.ToLower(text)
	if m := modelRe.FindStringSubmatch(text); len(m) > 1 {
		res.Overrides["model"] = m[1]
		res.Reply += fmt.Sprintf("已设置本轮模型: %s\n", m[1])
		text = modelRe.ReplaceAllString(text, "")
	}
	if m := projectRe.FindStringSubmatch(text); m != nil {
		arg := ""
		if len(m) > 1 {
			arg = m[1]
		}
		res.Reply += fmt.Sprintf("project 命令已接收: %s（请在会话中确认项目目录）\n", arg)
		text = projectRe.ReplaceAllString(text, "")
		res.Handled = strings.TrimSpace(text) == "" && arg != ""
	}
	if m := memoryRe.FindStringSubmatch(text); m != nil {
		sub := "show"
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			sub = strings.ToLower(strings.TrimSpace(m[1]))
		}
		res.Overrides["memory"] = sub
		text = memoryRe.ReplaceAllString(text, "")
		res.Handled = true
	}
	if forgetRe.MatchString(text) {
		res.Overrides["forget"] = "1"
		text = forgetRe.ReplaceAllString(text, "")
		res.Handled = true
	}
	flags := cmdFlagRe.FindAllStringSubmatch(text, -1)
	for _, f := range flags {
		if len(f) < 2 {
			continue
		}
		switch strings.ToLower(f[1]) {
		case "clear", "new":
			res.Overrides["clear"] = "1"
			res.Reply += "已清除会话\n"
			res.Handled = true
		case "stop", "abort", "interrupt", "停止", "中断":
			res.Overrides["stop"] = "1"
			res.Handled = true
		case "help":
			res.Reply += "命令: --clear/--new/--stop/--jobs/--help/--status/--whoami/--plan/--ask/--agent/--prompt --project --model --memory off|on|show --forget\n"
			res.Handled = true
		case "status":
			st := b.SnapshotStatus()
			res.Reply += fmt.Sprintf("bot=%s backend=%s connected=%v ws_enabled=%v err=%s\n",
				b.Config.ID, b.Config.Backend, st.Connected, st.WSEnabled, st.LastError)
			res.Handled = true
		case "whoami":
			res.Reply += fmt.Sprintf("role=%s name=%s group=%s\n", b.Config.RoleID, b.Config.Name, b.Config.Group)
			res.Handled = true
		case "plan", "ask", "agent":
			res.Overrides["mode"] = strings.ToLower(f[1])
			res.Reply += fmt.Sprintf("本轮模式: %s\n", f[1])
		case "prompt":
			res.Reply += "system_prompt:\n" + b.Config.SystemPrompt + "\n"
			res.Handled = true
		case "memory", "forget":
			// Handled above via dedicated regex; keep Handled if only these remain.
			res.Handled = true
		}
	}
	text = cmdFlagRe.ReplaceAllString(text, " ")
	res.RestText = strings.TrimSpace(text)
	if res.RestText != "" {
		res.Handled = false
	}
	_ = lower
	_ = reg
	return res
}

func MessageTargetsBot(content string, receive *bot.Bot, reg *registry.Registry) bool {
	if receive == nil || reg == nil {
		return true
	}
	mentioned := reg.ResolveMention(content, receive.Config.Group)
	if len(mentioned) == 0 {
		return true
	}
	for _, m := range mentioned {
		if m.Config.ID == receive.Config.ID || m.Config.RoleID == receive.Config.RoleID ||
			strings.EqualFold(m.Config.Name, receive.Config.Name) {
			return true
		}
	}
	return false
}

func Allowed(cfg bot.Config, openID, name string) bool {
	if len(cfg.AllowOpenIDs) == 0 && len(cfg.AllowUsers) == 0 {
		return true
	}
	for _, id := range cfg.AllowOpenIDs {
		if id == openID {
			return true
		}
	}
	for _, u := range cfg.AllowUsers {
		if strings.EqualFold(u, name) {
			return true
		}
	}
	return false
}
