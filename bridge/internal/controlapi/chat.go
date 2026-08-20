package controlapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/chatstore"
	"yzj-bridge/internal/paths"
	"yzj-bridge/internal/sessions"
)

var chatMentionRe = regexp.MustCompile(`@([^\s@]+)`)

// chat returns the lazily-initialized ChatStore, creating one at the
// default path on first use. The store is shared across requests so
// in-memory state and on-disk state stay in sync.
func (s *Server) chat() *chatstore.Store {
	s.chatOnce.Do(func() {
		if s.Chat != nil {
			return
		}
		st, err := chatstore.Open(s.chatPath())
		if err != nil {
			log.Printf("chatstore open: %v", err)
			st, _ = chatstore.Open("") // best-effort fallback
		}
		s.Chat = st
	})
	return s.Chat
}

func (s *Server) chatPath() string {
	if s.ChatPath != "" {
		return s.ChatPath
	}
	return paths.UserDataDir() + string(filepath.Separator) + "chat_sessions.json"
}

// chatSessions handles the collection endpoint /v1/chat/sessions.
func (s *Server) chatSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		st := s.chat()
		writeJSON(w, map[string]any{"sessions": st.List()})
	case http.MethodPost:
		var body struct {
			BotID string `json:"bot_id"`
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		st := s.chat()
		sess, err := st.Create(body.BotID, body.Title)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, sess)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// chatSessionsPath handles /v1/chat/sessions/{id} and the
// /v1/chat/sessions/{id}/messages sub-route.
func (s *Server) chatSessionsPath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/chat/sessions/")
	// Split into id and optional sub-resource ("messages").
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}

	switch {
	case sub == "" && r.Method == http.MethodGet:
		st := s.chat()
		sess := st.Get(id)
		if sess == nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		writeJSON(w, sess)
	case sub == "" && r.Method == http.MethodPatch:
		var body struct {
			BotID string `json:"bot_id"`
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		st := s.chat()
		updated, err := st.Update(id, body.Title, body.BotID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if updated == nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		writeJSON(w, updated)
	case sub == "" && r.Method == http.MethodDelete:
		st := s.chat()
		if err := st.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case sub == "messages" && r.Method == http.MethodPost:
		s.chatSendMessage(w, r, id)
	case sub == "messages/stream" && r.Method == http.MethodPost:
		s.chatStreamMessage(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// chatSendMessage implements POST /v1/chat/sessions/{id}/messages.
//
// Flow: resolve the receiving bot (explicit @mention wins, else the
// session's bot_id), dispatch to the orchestrator, persist both the
// user and assistant turns, and return the reply plus refreshed session.
func (s *Server) chatSendMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
	if s.RT == nil || s.RT.Orch == nil {
		http.Error(w, "runtime unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}

	st := s.chat()
	sess := st.Get(sessionID)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	receiveBotID, clean, err := s.resolveChatDispatch(sess, content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Same dispatch path as IM (DispatchWithContext). GUI sessions use
	// gui-chat:{sessionID} and always get their own agent context (see
	// sessions.ResolveSessionKey), even when the bot is session_mode: shared.
	openID := sessions.GUIChatOpenID(sessionID)
	res := s.RT.Orch.DispatchWithContext(r.Context(), receiveBotID, clean, openID, "GUI测试", map[string]string{})

	userMsg := chatstore.Message{Role: "user", BotID: receiveBotID, Content: content}
	assistantMsg := chatstore.Message{Role: "assistant", BotID: res.HandlerBotID, Content: res.Reply}
	if err := st.AppendMessages(sessionID, userMsg, assistantMsg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	updated := st.Get(sessionID)
	writeJSON(w, map[string]any{
		"reply":          res.Reply,
		"handler_bot_id": res.HandlerBotID,
		"receive_bot_id": res.ReceiveBotID,
		"session":        updated,
	})
}

// resolveChatDispatch resolves the target bot and strips a leading @mention.
func (s *Server) resolveChatDispatch(sess *chatstore.Session, content string) (receiveBotID, clean string, err error) {
	// Resolve the receiving bot: a leading @mention overrides the
	// session's bot_id; otherwise fall back to the session default.
	receiveBotID = ""
	clean = content
	if m := chatMentionRe.FindStringSubmatch(content); m != nil && strings.HasPrefix(content, "@"+m[1]) {
		if b := s.RT.Reg.Resolve(m[1], ""); b != nil {
			receiveBotID = b.Config.ID
			clean = strings.TrimSpace(strings.TrimPrefix(content, "@"+m[1]))
		}
	}
	if receiveBotID == "" {
		receiveBotID = sess.BotID
	}
	if receiveBotID == "" {
		return "", "", fmt.Errorf("bot_id required")
	}
	if s.RT.Reg.Get(receiveBotID) == nil {
		return "", "", fmt.Errorf("bot not found: %s", receiveBotID)
	}
	return receiveBotID, clean, nil
}

// chatStreamMessage implements POST /v1/chat/sessions/{id}/messages/stream.
//
// It mirrors chatSendMessage's bot/@mention resolution, then dispatches through
// the orchestrator with an OnStream callback (same path as IM + streaming).
func (s *Server) chatStreamMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
	if s.RT == nil || s.RT.Orch == nil {
		http.Error(w, "runtime unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}

	st := s.chat()
	sess := st.Get(sessionID)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	receiveBotID, clean, err := s.resolveChatDispatch(sess, content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// SSE headers. Flusher support lets us push deltas as they arrive;
	// when the underlying writer is not a Flusher (e.g. httptest without
	// a buffered recorder) we still write a well-formed stream.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var reasoningBuf strings.Builder
	onStream := func(ev bot.StreamEvent) {
		if ev.Type == "done" {
			// Terminal `done` with session is emitted by this handler below.
			return
		}
		if ev.Type == "reasoning" {
			reasoningBuf.WriteString(ev.Text)
		}
		writeSSE(w, ev.Type, ev, flusher)
	}

	openID := sessions.GUIChatOpenID(sessionID)
	res := s.RT.Orch.DispatchWithContextStream(r.Context(), receiveBotID, clean, openID, "GUI测试", map[string]string{}, onStream)

	userMsg := chatstore.Message{Role: "user", BotID: receiveBotID, Content: content}
	assistantMsg := chatstore.Message{
		Role:      "assistant",
		BotID:     res.HandlerBotID,
		Content:   res.Reply,
		Reasoning: reasoningBuf.String(),
	}
	if perr := st.AppendMessages(sessionID, userMsg, assistantMsg); perr != nil {
		writeSSE(w, "error", map[string]any{"message": perr.Error()}, flusher)
		return
	}

	updated := st.Get(sessionID)
	writeSSE(w, "done", map[string]any{
		"reply":          res.Reply,
		"handler_bot_id": res.HandlerBotID,
		"receive_bot_id": res.ReceiveBotID,
		"session":        updated,
	}, flusher)
}

// writeSSE writes a single SSE event of the form:
//
//	event: <type>\n
//	data: <json>\n\n
//
// followed by an optional Flush. Payload is JSON-encoded.
func writeSSE(w http.ResponseWriter, eventType string, payload any, flusher http.Flusher) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
	if flusher != nil {
		flusher.Flush()
	}
}
