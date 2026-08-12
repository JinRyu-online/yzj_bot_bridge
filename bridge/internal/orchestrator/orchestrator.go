package orchestrator

import (
	"context"
	"log"
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
	HandlerBotID string
	ReceiveBotID string
}

func (o *Orchestrator) Dispatch(receiveBotID, content, openID, name string, overrides map[string]string) DispatchResult {
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
		Overrides: overrides,
	}
	if o.Skills != nil && len(handler.Config.Skills) > 0 {
		pkgs, err := skills.Resolve(o.Skills, handler.Config.Skills)
		if err != nil {
			log.Printf("bot=%s skills resolve: %v", handlerID, err)
		} else {
			if err := skills.Materialize(pkgs, ws, handler.Config.Backend); err != nil {
				log.Printf("bot=%s skills materialize: %v", handlerID, err)
			}
			opts.SkillPrompt = skills.PromptAppendix(pkgs)
			for _, t := range skills.OpenAITools(pkgs) {
				opts.SkillTools = append(opts.SkillTools, bot.ToolSpec{
					Name: t.Name, Description: t.Description, Parameters: t.Parameters,
				})
			}
			store := o.Skills
			runner := &skills.Runner{}
			opts.SkillDispatch = func(toolName, argsJSON, workspace string) string {
				return runner.ExecByExportName(context.Background(), store, toolName, argsJSON, workspace)
			}
		}
	}
	result := handler.Backend.Run(clean, opts)
	log.Printf("bot=%s reply: %s", handlerID, clip(result.Reply, 2000))
	return DispatchResult{Reply: result.Reply, HandlerBotID: handlerID, ReceiveBotID: receiveBotID}
}

func clip(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
