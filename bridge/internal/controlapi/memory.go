package controlapi

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"yzj-bridge/internal/backends"
	"yzj-bridge/internal/memory"
)

func (s *Server) memoryProfiles(w http.ResponseWriter, r *http.Request) {
	if s.RT == nil || s.RT.Memory == nil {
		http.Error(w, "memory unavailable", http.StatusServiceUnavailable)
		return
	}
	botFilter := r.URL.Query().Get("bot")
	list, err := s.RT.Memory.ListVisible(botFilter)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, profileToAPI(s.RT.Memory, p))
	}
	writeJSON(w, map[string]any{"profiles": out})
}

func (s *Server) memoryProfilesPath(w http.ResponseWriter, r *http.Request) {
	if s.RT == nil || s.RT.Memory == nil {
		http.Error(w, "memory unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v1/memory/profiles/")
	parts := strings.SplitN(rest, "/", 2)
	openID := parts[0]
	if openID == "" {
		http.Error(w, "missing open_id", 400)
		return
	}
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}

	switch {
	case sub == "" && r.Method == http.MethodGet:
		p, err := s.RT.Memory.Store.Get(openID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if p == nil {
			n, _ := s.RT.Memory.Store.TurnCount(openID)
			if n == 0 {
				http.Error(w, "not found", 404)
				return
			}
			p = &memory.Profile{OpenID: openID}
		}
		writeJSON(w, profileToAPI(s.RT.Memory, p))

	case sub == "" && r.Method == http.MethodPatch:
		s.patchMemoryProfile(w, r, openID)

	case sub == "" && r.Method == http.MethodDelete:
		if r.URL.Query().Get("confirm") != "1" {
			http.Error(w, "confirm=1 required", 400)
			return
		}
		_ = s.RT.Memory.Store.ClearTurns(openID)
		if err := s.RT.Memory.Store.Delete(openID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"ok": true})

	case sub == "reset-inferred" && r.Method == http.MethodPost:
		p, err := s.RT.Memory.Store.ResetInferred(openID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, profileToAPI(s.RT.Memory, p))

	case sub == "lock" && r.Method == http.MethodPost:
		s.lockMemoryFields(w, r, openID)

	case sub == "run" && r.Method == http.MethodPost:
		s.RT.Memory.Sched.Enqueue(openID, "manual")
		writeJSON(w, map[string]any{"ok": true, "queued": true})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) patchMemoryProfile(w http.ResponseWriter, r *http.Request, openID string) {
	var body struct {
		DisplayName  *string                 `json:"display_name"`
		HowToAddress *memory.Field           `json:"how_to_address"`
		Role         *memory.Field           `json:"role"`
		AskStyle     *memory.Field           `json:"ask_style"`
		ReplyStyle   *memory.Field           `json:"reply_style"`
		Donts        *memory.StringListField `json:"donts"`
		Notes        *memory.Field           `json:"notes"`
		OptedOut     *bool                   `json:"opted_out"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	p, err := s.RT.Memory.Store.GetOrCreate(openID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if body.DisplayName != nil {
		p.DisplayName = strings.TrimSpace(*body.DisplayName)
	}
	if body.OptedOut != nil {
		p.OptedOut = *body.OptedOut
	}
	applyField := func(dst *memory.Field, src *memory.Field) {
		if src == nil {
			return
		}
		// 锁定字段不可通过 PATCH 修改 manual（锁定语义：不可编辑 + 画像器不覆盖）。
		// 仅允许本请求同时解锁时解除；显式解锁走 /lock 端点。
		if dst.Locked && !src.Locked {
			return
		}
		dst.Manual = memorySanitizeManual(src.Manual)
		if src.Locked {
			dst.Locked = true
		}
	}
	applyField(&p.HowToAddress, body.HowToAddress)
	applyField(&p.Role, body.Role)
	applyField(&p.AskStyle, body.AskStyle)
	applyField(&p.ReplyStyle, body.ReplyStyle)
	applyField(&p.Notes, body.Notes)
	if body.Donts != nil {
		if !p.Donts.Locked || body.Donts.Locked {
			p.Donts.Manual = memory.SanitizeDonts(body.Donts.Manual, s.RT.Memory.ConfigSnapshot().DontsMax)
			if body.Donts.Locked {
				p.Donts.Locked = true
			}
		}
	}
	if err := s.RT.Memory.Store.Save(p); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, profileToAPI(s.RT.Memory, p))
}

func memorySanitizeManual(s string) string {
	return memory.SanitizeFieldValue(s, 400)
}

func (s *Server) lockMemoryFields(w http.ResponseWriter, r *http.Request, openID string) {
	var body struct {
		Fields map[string]bool `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	p, err := s.RT.Memory.Store.GetOrCreate(openID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for k, locked := range body.Fields {
		switch strings.ToLower(k) {
		case "how_to_address":
			p.HowToAddress.Locked = locked
		case "role":
			p.Role.Locked = locked
		case "ask_style":
			p.AskStyle.Locked = locked
		case "reply_style":
			p.ReplyStyle.Locked = locked
		case "donts":
			p.Donts.Locked = locked
		case "notes":
			p.Notes.Locked = locked
		}
	}
	if err := s.RT.Memory.Store.Save(p); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, profileToAPI(s.RT.Memory, p))
}

func (s *Server) memoryEnableCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if s.RT == nil {
		http.Error(w, "runtime unavailable", 503)
		return
	}
	defs := s.RT.Defaults
	if defs == nil {
		defs = map[string]any{}
	}
	mem := memory.ParseConfig(defs).ResolveEngine(defs)
	oa := backends.ProbeOpenAI(mem.OpenAIBaseURL, mem.OpenAIAPIKey)
	claudeOK := false
	claudeErr := ""
	bin := mem.ClaudeBin
	if bin == "" {
		bin = "claude"
	}
	if path, err := exec.LookPath(bin); err == nil {
		claudeOK = true
		_ = path
	} else if _, err := os.Stat(bin); err == nil {
		claudeOK = true
	} else {
		claudeErr = err.Error()
	}
	ok := oa.OK || claudeOK
	writeJSON(w, map[string]any{
		"ok":            ok,
		"openai":        oa,
		"claude_ok":     claudeOK,
		"claude_error":  claudeErr,
		"claude_bin":    bin,
		"reason":        enableReason(ok, oa.OK, claudeOK),
	})
}

func enableReason(ok, oa, claude bool) string {
	if ok {
		return "ready"
	}
	if !oa && !claude {
		return "openai probe failed and claude bin not found"
	}
	return "unavailable"
}

func profileToAPI(svc *memory.Service, p *memory.Profile) map[string]any {
	turns, _ := svc.Store.TurnCount(p.OpenID)
	return map[string]any{
		"open_id":         p.OpenID,
		"display_name":    p.DisplayName,
		"how_to_address":  p.HowToAddress,
		"role":            p.Role,
		"ask_style":       p.AskStyle,
		"reply_style":     p.ReplyStyle,
		"donts":           p.Donts,
		"notes":           p.Notes,
		"fact_cards":      p.FactCards,
		"last_seen":       p.LastSeen,
		"bots_seen":       p.BotsSeen,
		"opted_out":       p.OptedOut,
		"profiled_count":  p.ProfiledCount,
		"turn_count":      turns,
		"last_profile_at": p.LastProfileAt,
		"last_error":      p.LastError,
		"updated_at":      p.UpdatedAt,
		// Never include turns jsonl content.
	}
}