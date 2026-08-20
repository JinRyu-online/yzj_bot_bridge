package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProfilerOpenAISuccessAdvancesCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": `{"how_to_address":"小王","role":"研发","ask_style":"简洁","reply_style":"条目","donts":["废话"],"notes":"偏好短答"}`}},
			},
		})
	}))
	defer srv.Close()

	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.OpenAIBaseURL = srv.URL
	cfg.OpenAIAPIKey = "k"
	cfg.Model = "m"
	cfg.AfterTurns = 100
	svc := NewService(root, cfg)

	for i := 0; i < 3; i++ {
		_ = svc.Store.AppendTurn("u", Turn{User: "q", Assistant: "a", BotID: "b1"})
	}
	p := &Profile{OpenID: "u", ProfiledCount: 0}
	_ = svc.Store.Save(p)

	if err := svc.RunProfiler(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	p, _ = svc.Store.Get("u")
	if p.ProfiledCount != 3 {
		t.Fatalf("cursor=%d", p.ProfiledCount)
	}
	if p.Role.Inferred != "研发" || p.HowToAddress.Inferred != "小王" {
		t.Fatalf("%+v", p)
	}
}

func TestProfilerFallsBackToClaudeOnOpenAIFail(t *testing.T) {
	// OpenAI always fails.
	oa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer oa.Close()

	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.OpenAIBaseURL = oa.URL
	cfg.OpenAIAPIKey = "k"
	cfg.Model = "m"
	cfg.ClaudeBin = "false" // will fail too — we only assert openai tried then claude error path
	svc := NewService(root, cfg)
	_ = svc.Store.AppendTurn("u", Turn{User: "q", Assistant: "a"})
	_ = svc.Store.Save(&Profile{OpenID: "u"})

	err := svc.RunProfiler(context.Background(), "u")
	if err == nil {
		t.Fatal("expected error when both fail")
	}
	p, _ := svc.Store.Get("u")
	if p.ProfiledCount != 0 {
		t.Fatalf("cursor moved on failure: %d", p.ProfiledCount)
	}
	if p.LastError == "" {
		t.Fatal("expected last_error")
	}
}

func TestSchedulerGlobalConcurrencyOne(t *testing.T) {
	root := t.TempDir()
	var concurrent int32
	var max int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&concurrent, 1)
		for {
			old := atomic.LoadInt32(&max)
			if n <= old || atomic.CompareAndSwapInt32(&max, old, n) {
				break
			}
		}
		time.Sleep(80 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": `{"role":"x","how_to_address":"","ask_style":"","reply_style":"","donts":[],"notes":""}`}},
			},
		})
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.OpenAIBaseURL = srv.URL
	cfg.OpenAIAPIKey = "k"
	cfg.Model = "m"
	svc := NewService(root, cfg)
	for _, id := range []string{"a", "b", "c"} {
		_ = svc.Store.AppendTurn(id, Turn{User: "q", Assistant: "a"})
		_ = svc.Store.Save(&Profile{OpenID: id})
		svc.Sched.Enqueue(id, "test")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		svc.Sched.mu.Lock()
		idle := len(svc.Sched.queue) == 0 && len(svc.Sched.inflight) == 0
		svc.Sched.mu.Unlock()
		if idle {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&max) > 1 {
		t.Fatalf("max concurrent=%d", max)
	}
}
