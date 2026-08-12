package backends

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yzj-bridge/internal/bot"
)

// sseHandler writes a sequence of `data: {...}` lines followed by [DONE].
// Each entry in chunks is written as one SSE data line.
func sseHandler(t *testing.T, chunks []string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Confirm the request asked for streaming.
		var req struct {
			Stream bool `json:"stream"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if !req.Stream {
			t.Errorf("expected stream=true in request body, got body=%s", string(body))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = io.WriteString(w, "data: "+c+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	})
}

func newStreamingBackend(t *testing.T, h http.Handler) *OpenAIBackend {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := bot.Config{
		ID:            "test-bot",
		Backend:       "openai",
		Model:         "test-model",
		OpenAIBaseURL:  srv.URL,
		OpenAIAPIKey:  "test-key",
		OpenAITimeout: 10,
	}
	return NewOpenAI(cfg, nil)
}

func TestChatStreamAssemblesContentAndReasoning(t *testing.T) {
	chunks := []string{
		`{"choices":[{"delta":{"role":"assistant","reasoning_content":"think"}}]}`,
		`{"choices":[{"delta":{"reasoning_content":"ing"}}]}`,
		`{"choices":[{"delta":{"content":"Hel"}}]}`,
		`{"choices":[{"delta":{"content":"lo"}}]}`,
		`{"choices":[{"delta":{"content":" world"}}]}`,
	}
	backend := newStreamingBackend(t, sseHandler(t, chunks))

	var reasoning, content strings.Builder
	onStream := func(ev bot.StreamEvent) {
		switch ev.Type {
		case "reasoning":
			reasoning.WriteString(ev.Text)
		case "content":
			content.WriteString(ev.Text)
		}
	}

	msg, err := backend.chatStream(context.Background(), "test-model", nil, nil, onStream, 1)
	if err != nil {
		t.Fatalf("chatStream: %v", err)
	}
	if msg.Role != "assistant" {
		t.Fatalf("role=%q want assistant", msg.Role)
	}
	if got, want := msg.Content, "Hello world"; got != want {
		t.Fatalf("content=%q want %q", got, want)
	}
	if got, want := msg.ReasoningContent, "thinking"; got != want {
		t.Fatalf("reasoning=%q want %q", got, want)
	}
	if got, want := reasoning.String(), "thinking"; got != want {
		t.Fatalf("reasoning stream=%q want %q", got, want)
	}
	if got, want := content.String(), "Hello world"; got != want {
		t.Fatalf("content stream=%q want %q", got, want)
	}
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(msg.ToolCalls))
	}
}

func TestChatStreamMergesToolCallsByIndex(t *testing.T) {
	chunks := []string{
		`{"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_dir","arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\".\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]}}]}`,
	}
	backend := newStreamingBackend(t, sseHandler(t, chunks))

	msg, err := backend.chatStream(context.Background(), "test-model", nil, nil, nil, 1)
	if err != nil {
		t.Fatalf("chatStream: %v", err)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("tool_calls=%d want 2: %+v", len(msg.ToolCalls), msg.ToolCalls)
	}
	if msg.ToolCalls[0].ID != "call_1" || msg.ToolCalls[0].Function.Name != "list_dir" {
		t.Fatalf("tc0=%+v", msg.ToolCalls[0])
	}
	if got, want := msg.ToolCalls[0].Function.Arguments, `{"path":"."}`; got != want {
		t.Fatalf("tc0 args=%q want %q", got, want)
	}
	if msg.ToolCalls[1].ID != "call_2" || msg.ToolCalls[1].Function.Name != "read_file" {
		t.Fatalf("tc1=%+v", msg.ToolCalls[1])
	}
	if got, want := msg.ToolCalls[1].Function.Arguments, `{"path":"a"}`; got != want {
		t.Fatalf("tc1 args=%q want %q", got, want)
	}
}

func TestChatStreamHandlesErrorPayload(t *testing.T) {
	chunks := []string{
		`{"error":{"message":"rate limited"}}`,
	}
	backend := newStreamingBackend(t, sseHandler(t, chunks))
	_, err := backend.chatStream(context.Background(), "test-model", nil, nil, nil, 1)
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected error 'rate limited', got %v", err)
	}
}

func TestChatStreamIgnoresCommentLines(t *testing.T) {
	// Mix in a comment line and an event: line; only data: lines should parse.
	chunks := []string{
		`: keep-alive`,
		`event: ping`,
		`{"choices":[{"delta":{"content":"hi"}}]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, ": keep-alive\n\n")
		_, _ = io.WriteString(w, "event: ping\ndata: {}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	cfg := bot.Config{ID: "b", Backend: "openai", Model: "m", OpenAIBaseURL: srv.URL, OpenAITimeout: 10}
	backend := NewOpenAI(cfg, nil)

	msg, err := backend.chatStream(context.Background(), "m", nil, nil, nil, 1)
	if err != nil {
		t.Fatalf("chatStream: %v", err)
	}
	if msg.Content != "hi" {
		t.Fatalf("content=%q want hi", msg.Content)
	}
	_ = chunks
}
