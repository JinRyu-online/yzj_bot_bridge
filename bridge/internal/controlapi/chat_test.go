package controlapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/orchestrator"
	"yzj-bridge/internal/registry"
	"yzj-bridge/internal/runtime"
)

// fakeBackend returns a fixed reply so the orchestrator can run end-to-end
// without spawning any real CLI process.
type fakeBackend struct{ reply string }

func (f *fakeBackend) Run(prompt string, opts bot.RunOpts) bot.RunResult {
	// If the caller wired an OnStream callback, emit a couple of synthetic
	// deltas so the SSE handler has something to forward before `done`.
	if opts.OnStream != nil {
		opts.OnStream(bot.StreamEvent{Type: "reasoning", Text: "thinking"})
		opts.OnStream(bot.StreamEvent{Type: "content", Text: "OK"})
		opts.OnStream(bot.StreamEvent{Type: "content", Text: ":" + prompt})
	}
	return bot.RunResult{Reply: f.reply + ":" + prompt}
}
func (f *fakeBackend) CreateSession() (string, error) { return "s1", nil }
func (f *fakeBackend) ClearSession(string) (string, error) { return "", nil }

func newChatTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	reg := registry.New()
	reg.Replace([]*bot.Bot{
		{Config: bot.Config{ID: "bot-a", Name: "Alpha", Backend: "fake"}, Backend: &fakeBackend{reply: "OK"}},
	})
	rt := &runtime.Runtime{
		Reg:  reg,
		Orch: &orchestrator.Orchestrator{Reg: reg, GlobalWorkspace: dir},
	}
	s := &Server{
		RT:       rt,
		Token:    "test-token",
		ChatPath: filepath.Join(dir, "chat_sessions.json"),
	}
	return s
}

func doChat(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, nil)
	} else {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
	}
	r.Header.Set("Authorization", "Bearer "+s.Token)
	w := httptest.NewRecorder()
	s.srvChatMux().ServeHTTP(w, r)
	return w
}

// srvChatMux exposes only the chat routes so tests don't need Start().
func (s *Server) srvChatMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/sessions", s.auth(s.chatSessions))
	mux.HandleFunc("/v1/chat/sessions/", s.auth(s.chatSessionsPath))
	return mux
}

func TestChatCreateListGetDelete(t *testing.T) {
	s := newChatTestServer(t)

	// Create
	w := doChat(t, s, http.MethodPost, "/v1/chat/sessions", map[string]any{"bot_id": "bot-a", "title": "Hello"})
	if w.Code != 200 {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var sess chatSessionJSON
	if err := json.Unmarshal(w.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}
	if sess.ID == "" || sess.BotID != "bot-a" {
		t.Fatalf("sess=%+v", sess)
	}

	// List
	w = doChat(t, s, http.MethodGet, "/v1/chat/sessions", nil)
	if w.Code != 200 {
		t.Fatalf("list status=%d", w.Code)
	}
	var listResp struct {
		Sessions []struct {
			ID, Title, BotID string
			MessageCount     int
		} `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Sessions) != 1 || listResp.Sessions[0].ID != sess.ID {
		t.Fatalf("list=%+v", listResp)
	}
	// Summaries must not embed full messages.
	if raw := w.Body.String(); strings.Contains(raw, "\"messages\"") {
		t.Fatalf("list should omit messages: %s", raw)
	}

	// Get full
	w = doChat(t, s, http.MethodGet, "/v1/chat/sessions/"+sess.ID, nil)
	if w.Code != 200 {
		t.Fatalf("get status=%d", w.Code)
	}

	// Patch
	w = doChat(t, s, http.MethodPatch, "/v1/chat/sessions/"+sess.ID, map[string]any{"title": "Renamed"})
	if w.Code != 200 {
		t.Fatalf("patch status=%d", w.Code)
	}
	var patched chatSessionJSON
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if patched.Title != "Renamed" {
		t.Fatalf("patched title=%q", patched.Title)
	}

	// Delete
	w = doChat(t, s, http.MethodDelete, "/v1/chat/sessions/"+sess.ID, nil)
	if w.Code != 200 {
		t.Fatalf("delete status=%d", w.Code)
	}
	w = doChat(t, s, http.MethodGet, "/v1/chat/sessions/"+sess.ID, nil)
	if w.Code != 404 {
		t.Fatalf("after delete status=%d want 404", w.Code)
	}
}

func TestChatSendMessageDispatchesAndPersists(t *testing.T) {
	s := newChatTestServer(t)

	w := doChat(t, s, http.MethodPost, "/v1/chat/sessions", map[string]any{"bot_id": "bot-a"})
	var sess chatSessionJSON
	_ = json.Unmarshal(w.Body.Bytes(), &sess)

	// Send a message; the fake backend echoes "OK:<prompt>".
	w = doChat(t, s, http.MethodPost, "/v1/chat/sessions/"+sess.ID+"/messages", map[string]any{"content": "hello world"})
	if w.Code != 200 {
		t.Fatalf("send status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Reply          string         `json:"reply"`
		HandlerBotID   string         `json:"handler_bot_id"`
		ReceiveBotID   string         `json:"receive_bot_id"`
		Session        chatSessionJSON `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ReceiveBotID != "bot-a" || resp.HandlerBotID != "bot-a" {
		t.Fatalf("bot ids=%+v", resp)
	}
	if !strings.HasPrefix(resp.Reply, "OK:") {
		t.Fatalf("reply=%q", resp.Reply)
	}
	if len(resp.Session.Messages) != 2 {
		t.Fatalf("messages=%d", len(resp.Session.Messages))
	}
	if resp.Session.Messages[0].Role != "user" || resp.Session.Messages[1].Role != "assistant" {
		t.Fatalf("messages=%+v", resp.Session.Messages)
	}
	// Auto title from first user content.
	if resp.Session.Title != "hello world" {
		t.Fatalf("title=%q", resp.Session.Title)
	}
}

func TestChatSendMessageEmptyContent400(t *testing.T) {
	s := newChatTestServer(t)
	w := doChat(t, s, http.MethodPost, "/v1/chat/sessions", map[string]any{"bot_id": "bot-a"})
	var sess chatSessionJSON
	_ = json.Unmarshal(w.Body.Bytes(), &sess)

	w = doChat(t, s, http.MethodPost, "/v1/chat/sessions/"+sess.ID+"/messages", map[string]any{"content": "   "})
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestChatSendMessageNoBotID400(t *testing.T) {
	s := newChatTestServer(t)
	// Create a session without bot_id.
	w := doChat(t, s, http.MethodPost, "/v1/chat/sessions", map[string]any{})
	var sess chatSessionJSON
	_ = json.Unmarshal(w.Body.Bytes(), &sess)

	w = doChat(t, s, http.MethodPost, "/v1/chat/sessions/"+sess.ID+"/messages", map[string]any{"content": "hi"})
	if w.Code != 400 {
		t.Fatalf("expected 400 for missing bot_id, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestChatSendMessageMentionOverridesReceiveBot(t *testing.T) {
	s := newChatTestServer(t)
	// Register a second bot so @beta resolves to it.
	s.RT.Reg.Replace([]*bot.Bot{
		{Config: bot.Config{ID: "bot-a", Name: "Alpha", Backend: "fake"}, Backend: &fakeBackend{reply: "A"}},
		{Config: bot.Config{ID: "bot-b", Name: "Beta", Backend: "fake"}, Backend: &fakeBackend{reply: "B"}},
	})

	w := doChat(t, s, http.MethodPost, "/v1/chat/sessions", map[string]any{"bot_id": "bot-a"})
	var sess chatSessionJSON
	_ = json.Unmarshal(w.Body.Bytes(), &sess)

	w = doChat(t, s, http.MethodPost, "/v1/chat/sessions/"+sess.ID+"/messages", map[string]any{"content": "@Beta hello"})
	if w.Code != 200 {
		t.Fatalf("send status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ReceiveBotID string `json:"receive_bot_id"`
		Reply        string `json:"reply"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.ReceiveBotID != "bot-b" {
		t.Fatalf("receive=%q want bot-b", resp.ReceiveBotID)
	}
	if !strings.HasPrefix(resp.Reply, "B:") {
		t.Fatalf("reply=%q", resp.Reply)
	}
}

// chatSessionJSON mirrors chatstore.Session for test decoding.
type chatSessionJSON struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	BotID     string `json:"bot_id"`
	UpdatedAt string `json:"updated_at"`
	Messages  []struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		BotID     string `json:"bot_id"`
		Reasoning string `json:"reasoning"`
	} `json:"messages"`
}

func TestChatStreamMessageEmitsSSEAndPersists(t *testing.T) {
	s := newChatTestServer(t)

	w := doChat(t, s, http.MethodPost, "/v1/chat/sessions", map[string]any{"bot_id": "bot-a"})
	var sess chatSessionJSON
	_ = json.Unmarshal(w.Body.Bytes(), &sess)

	// Hit the streaming endpoint. The fake backend emits a reasoning
	// delta plus two content deltas, then the handler appends a `done`.
	w = doChat(t, s, http.MethodPost, "/v1/chat/sessions/"+sess.ID+"/messages/stream", map[string]any{"content": "hello stream"})
	if w.Code != 200 {
		t.Fatalf("stream status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"event: reasoning",
		"event: content",
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in stream body:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `"reply":"OK:hello stream"`) {
		t.Fatalf("missing reply in done event:\n%s", body)
	}

	// The persisted session must include reasoning on the assistant turn.
	got := s.chat().Get(sess.ID)
	if got == nil {
		t.Fatal("session missing after stream")
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages=%d want 2", len(got.Messages))
	}
	if got.Messages[0].Role != "user" || got.Messages[1].Role != "assistant" {
		t.Fatalf("roles=%+v", got.Messages)
	}
	if got.Messages[1].Reasoning != "thinking" {
		t.Fatalf("reasoning=%q want %q", got.Messages[1].Reasoning, "thinking")
	}
	if got.Messages[1].Content != "OK:hello stream" {
		t.Fatalf("content=%q", got.Messages[1].Content)
	}
}

func TestChatStreamMessageEmptyContent400(t *testing.T) {
	s := newChatTestServer(t)
	w := doChat(t, s, http.MethodPost, "/v1/chat/sessions", map[string]any{"bot_id": "bot-a"})
	var sess chatSessionJSON
	_ = json.Unmarshal(w.Body.Bytes(), &sess)

	w = doChat(t, s, http.MethodPost, "/v1/chat/sessions/"+sess.ID+"/messages/stream", map[string]any{"content": "  "})
	if w.Code != 400 {
		t.Fatalf("expected 400 for empty content, got %d body=%s", w.Code, w.Body.String())
	}
}

// openIDCapturingBackend records OperatorOpenID from each Run for isolation tests.
type openIDCapturingBackend struct {
	reply   string
	openIDs []string
}

func (b *openIDCapturingBackend) Run(_ string, opts bot.RunOpts) bot.RunResult {
	b.openIDs = append(b.openIDs, opts.OperatorOpenID)
	return bot.RunResult{Reply: b.reply, Status: "ok"}
}
func (b *openIDCapturingBackend) CreateSession() (string, error) { return "s1", nil }
func (b *openIDCapturingBackend) ClearSession(string) (string, error) {
	return "", nil
}

func newSharedBotChatServer(t *testing.T, cap *openIDCapturingBackend) *Server {
	t.Helper()
	dir := t.TempDir()
	reg := registry.New()
	reg.Replace([]*bot.Bot{
		{
			Config: bot.Config{
				ID: "shared-bot", Name: "Shared", Backend: "fake",
				SessionMode: "shared", SharedSessionKey: "__shared__",
			},
			Backend: cap,
		},
	})
	rt := &runtime.Runtime{
		Reg:  reg,
		Orch: &orchestrator.Orchestrator{Reg: reg, GlobalWorkspace: dir},
	}
	return &Server{
		RT:       rt,
		Token:    "test-token",
		ChatPath: filepath.Join(dir, "chat_sessions.json"),
	}
}

func TestChatGUIUsesPerSessionOpenIDWithSharedBot(t *testing.T) {
	cap := &openIDCapturingBackend{reply: "ok"}
	s := newSharedBotChatServer(t, cap)

	w := doChat(t, s, http.MethodPost, "/v1/chat/sessions", map[string]any{"bot_id": "shared-bot"})
	var sessA chatSessionJSON
	_ = json.Unmarshal(w.Body.Bytes(), &sessA)

	w = doChat(t, s, http.MethodPost, "/v1/chat/sessions", map[string]any{"bot_id": "shared-bot"})
	var sessB chatSessionJSON
	_ = json.Unmarshal(w.Body.Bytes(), &sessB)

	w = doChat(t, s, http.MethodPost, "/v1/chat/sessions/"+sessA.ID+"/messages", map[string]any{"content": "query A"})
	if w.Code != 200 {
		t.Fatalf("sessA status=%d body=%s", w.Code, w.Body.String())
	}
	w = doChat(t, s, http.MethodPost, "/v1/chat/sessions/"+sessB.ID+"/messages", map[string]any{"content": "query B"})
	if w.Code != 200 {
		t.Fatalf("sessB status=%d body=%s", w.Code, w.Body.String())
	}

	if len(cap.openIDs) != 2 {
		t.Fatalf("openIDs=%v want 2 entries", cap.openIDs)
	}
	wantA := "gui-chat:" + sessA.ID
	wantB := "gui-chat:" + sessB.ID
	if cap.openIDs[0] != wantA || cap.openIDs[1] != wantB {
		t.Fatalf("openIDs=%v want [%q %q]", cap.openIDs, wantA, wantB)
	}
	for _, id := range cap.openIDs {
		if id == "__shared__" {
			t.Fatalf("GUI must not use shared key, got %q", id)
		}
	}
}

func TestChatStreamMessageMissingSession404(t *testing.T) {
	s := newChatTestServer(t)
	w := doChat(t, s, http.MethodPost, "/v1/chat/sessions/nope/messages/stream", map[string]any{"content": "hi"})
	if w.Code != 404 {
		t.Fatalf("expected 404 for missing session, got %d", w.Code)
	}
}
