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
	// atTokenRe 只抓「@+非空白」的提及 token。Go 正则的单词边界（\b）只认 ASCII，
	// 中文机器人名（如「寄件日志查询」）后面永远匹配不到边界。
	atTokenRe = regexp.MustCompile(`(?i)@[^\s@]+`)
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
		// 不能用 \b：Go 正则的 \b 只认 ASCII 单词边界，中文机器人名后面
		// 永远不是边界，导致「@寄件日志查询 /clear」原样留下，命令退化成
		// 「命令 + 正文」而触发模型。这里改判「后跟空白/结尾/非标识符」，
		// 并把作为分隔的字符原样保留（否则「@名字/clear」会丢掉前导 /）。
		// 允许名字后带「机器人」后缀（云之家 @ 机器人时常见写法）。
		re := regexp.MustCompile(`(?i)@` + regexp.QuoteMeta(n) + `(?:机器人)?(\s|$|[^\p{L}\p{N}_])`)
		out = re.ReplaceAllString(out, "$1")
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
	// 兜底：@机器人 的提及没被剥离时（中文名、@名字带前缀/后缀等），残留的
	// 「@token」会被当成用户正文，令纯命令退化为「命令 + 空提问」并白跑一轮
	// 模型。剩余文本若只由 @提及 构成，按纯命令处理。
	if res.RestText != "" && strings.TrimSpace(atTokenRe.ReplaceAllString(res.RestText, "")) == "" {
		res.RestText = ""
	}
	if res.RestText != "" {
		res.Handled = false
	}
	_ = lower
	_ = reg
	return res
}

func MessageTargetsBot(content string, receive *bot.Bot, reg *registry.Registry, groupTypes ...int) bool {
	if receive == nil || reg == nil {
		return true
	}
	// groupType=3 是个人机器人/私聊；私聊里的邮箱、@scope/pkg 不应被当成“@了别人”。
	isPrivate := len(groupTypes) > 0 && groupTypes[0] == 3
	mentioned := reg.ResolveMention(content, receive.Config.Group)
	if len(mentioned) == 0 {
		// 群里 @ 的是别的机器人/人：本 bot 不在目标范围内，不能当成私聊兜底。
		// 旧逻辑这里直接 return true，导致 A 群 @botA 时其它群的 bot 也抢答。
		if !isPrivate && reg.HasMention(content) {
			return false
		}
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
