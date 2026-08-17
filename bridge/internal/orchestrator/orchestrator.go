package orchestrator

import (
	"context"
	"log"
	"strings"
	"unicode/utf8"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/commands"
	"yzj-bridge/internal/registry"
	"yzj-bridge/internal/sessions"
	"yzj-bridge/internal/skills"
)

type Orchestrator struct {
	Reg             *registry.Registry
	Store           *sessions.Store
	GlobalWorkspace string
	Skills          *skills.Store
}

type DispatchResult struct {
	Reply        string
	Status       string
	HandlerBotID string
	ReceiveBotID string
}

func (o *Orchestrator) Dispatch(receiveBotID, content, openID, name string, overrides map[string]string) DispatchResult {
	return o.dispatch(receiveBotID, content, openID, name, overrides, nil, nil, nil)
}

func (o *Orchestrator) DispatchWithContext(ctx context.Context, receiveBotID, content, openID, name string, overrides map[string]string) DispatchResult {
	return o.dispatch(receiveBotID, content, openID, name, overrides, nil, nil, ctx)
}

// DispatchWithHistory is like Dispatch but injects prior turns into RunOpts.History
// (used by GUI chat so OpenAI-compatible backends keep multi-turn context).
func (o *Orchestrator) DispatchWithHistory(receiveBotID, content, openID, name string, overrides map[string]string, history []bot.HistoryTurn) DispatchResult {
	return o.dispatch(receiveBotID, content, openID, name, overrides, history, nil, nil)
}

// DispatchWithHistoryStream is like DispatchWithHistory but also forwards an
// OnStream callback into RunOpts so backends that support streaming can emit
// incremental reasoning/content/tool events. The callback may be nil.
func (o *Orchestrator) DispatchWithHistoryStream(receiveBotID, content, openID, name string, overrides map[string]string, history []bot.HistoryTurn, onStream func(bot.StreamEvent)) DispatchResult {
	return o.dispatch(receiveBotID, content, openID, name, overrides, history, onStream, nil)
}

func (o *Orchestrator) dispatch(receiveBotID, content, openID, name string, overrides map[string]string, history []bot.HistoryTurn, onStream func(bot.StreamEvent), ctx context.Context) DispatchResult {
	if overrides == nil {
		overrides = map[string]string{}
	}
	receive := o.Reg.Get(receiveBotID)
	handlerID := receiveBotID
	if receive != nil {
		for _, b := range o.Reg.ResolveMention(content, receive.Config.Group) {
			if b.Config.ID != receive.Config.ID {
				handlerID = b.Config.ID
				break
			}
		}
	}
	handler := o.Reg.Get(handlerID)
	if handler == nil || handler.Backend == nil {
		return DispatchResult{Reply: "未找到处理机器人: " + handlerID, HandlerBotID: handlerID, ReceiveBotID: receiveBotID}
	}
	clean := commands.StripBotMention(content, handler, o.Reg.Names(), false)
	log.Printf("bot=%s ask from=%s: %s", handlerID, name, clip(clean, 800))
	ws := sessions.ResolveAgentWorkspace(handler.Config, openID, o.Store, o.GlobalWorkspace)
	mode := overrides["mode"]
	if mode == "" {
		mode = "agent"
	}
	if overrides["clear"] == "1" {
		if key, ok := sessions.ResolveSessionKey(handler.Config, openID); ok && o.Store != nil {
			o.Store.ClearSession(handler.Config.ID, key)
			_ = o.Store.Save()
		}
	}
	opts := bot.RunOpts{
		Workspace: ws, Mode: mode, Skills: handler.Config.Skills,
		Model: overrides["model"], OperatorOpenID: openID, OperatorName: name,
		Overrides: overrides, History: history, OnStream: onStream, Context: ctx,
	}
	if o.Skills != nil && len(handler.Config.Skills) > 0 {
		pkgs, err := skills.Resolve(o.Skills, handler.Config.Skills)
		if err != nil {
			log.Printf("bot=%s skills resolve: %v", handlerID, err)
		} else {
			backend := strings.ToLower(handler.Config.Backend)
			if err := skills.Materialize(pkgs, ws, backend); err != nil {
				log.Printf("bot=%s skills materialize: %v", handlerID, err)
			}
			switch backend {
			case "openai", "opencode":
				// OpenCode-style: advertise via skill tool; load full body on demand.
				opts.SkillPrompt = skills.OpenAISkillSystemHint()
				if tool, ok := skills.OpenAISkillTool(pkgs); ok {
					opts.SkillTools = append(opts.SkillTools, bot.ToolSpec{
						Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters,
					})
				}
				store := o.Skills
				runner := &skills.Runner{}
				opts.SkillDispatch = func(toolName, argsJSON, _ string) string {
					if toolName == "skill" {
						return runner.LoadSkillTool(store, argsJSON)
					}
					return "unknown skill tool: " + toolName
				}
			default:
				// Cursor / Claude: materialize + prompt appendix; client loads skills.
				opts.SkillPrompt = skills.PromptAppendix(pkgs)
			}
		}
	}
	result := handler.Backend.Run(clean, opts)
	log.Printf("bot=%s reply: %s", handlerID, clip(result.Reply, 2000))
	return DispatchResult{Reply: result.Reply, Status: result.Status, HandlerBotID: handlerID, ReceiveBotID: receiveBotID}
}

func clip(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
