package commands

import (
	"fmt"
	"regexp"
	"strings"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/registry"
)

var (
	cmdFlagRe = regexp.MustCompile(`(?i)(?:^|\s)(?:--|/)(clear|new|help|status|whoami|plan|ask|agent|prompt)(?:\s|$)`)
	projectRe = regexp.MustCompile(`(?i)(?:--|/)project(?:\s+(\S+))?`)
	modelRe   = regexp.MustCompile(`(?i)(?:--|/)model(?:\s+(\S+))`)
	atRe      = regexp.MustCompile(`(?i)@([^\s@]+)\b\s*`)
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
		case "help":
			res.Reply += "命令: --clear/--new/--help/--status/--whoami/--plan/--ask/--agent/--prompt --project --model\n"
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
