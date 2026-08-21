package orchestrator_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/memory"
	"yzj-bridge/internal/orchestrator"
	"yzj-bridge/internal/registry"
	"yzj-bridge/internal/sessions"
)

type captureBackend struct {
	mu   sync.Mutex
	opts bot.RunOpts
}

func (c *captureBackend) Run(_ string, opts bot.RunOpts) bot.RunResult {
	c.mu.Lock()
	c.opts = opts
	c.mu.Unlock()
	return bot.RunResult{Reply: "ok-reply", Status: "ok"}
}
func (c *captureBackend) CreateSession() (string, error)       { return "s", nil }
func (c *captureBackend) ClearSession(string) (string, error) { return "", nil }

func TestMemoryPromptInjectedAlongsideSkills(t *testing.T) {
	root := t.TempDir()
	cfg := memory.DefaultConfig()
	cfg.Enabled = true
	svc := memory.NewService(root, cfg)
	_ = svc.Store.Save(&memory.Profile{
		OpenID: "u1",
		Role:   memory.Field{Inferred: "测试角色"},
	})

	be := &captureBackend{}
	reg := registry.New()
	reg.Replace([]*bot.Bot{{
		Config:  bot.Config{ID: "b1", Backend: "openai", MemoryEnabled: true},
		Backend: be,
	}})
	orch := &orchestrator.Orchestrator{Reg: reg, Memory: svc}
	res := orch.Dispatch("b1", "hello", "u1", "N", nil)
	if res.Status != "ok" {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(be.opts.MemoryPrompt, "测试角色") {
		t.Fatalf("MemoryPrompt=%q", be.opts.MemoryPrompt)
	}
	// wait async AfterDispatch
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, _ := svc.Store.TurnCount("u1")
		if n >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("turn not recorded")
}

func TestGUIOpenIDSkippedByDefault(t *testing.T) {
	root := t.TempDir()
	cfg := memory.DefaultConfig()
	cfg.Enabled = true
	svc := memory.NewService(root, cfg)
	_ = svc.Store.Save(&memory.Profile{OpenID: "real", Role: memory.Field{Inferred: "x"}})

	be := &captureBackend{}
	reg := registry.New()
	reg.Replace([]*bot.Bot{{
		Config:  bot.Config{ID: "b1", Backend: "openai", MemoryEnabled: true},
		Backend: be,
	}})
	orch := &orchestrator.Orchestrator{Reg: reg, Memory: svc}
	gui := sessions.GUIChatOpenID("sess")
	orch.Dispatch("b1", "hello", gui, "GUI", nil)
	if be.opts.MemoryPrompt != "" {
		t.Fatalf("unexpected prompt %q", be.opts.MemoryPrompt)
	}
	time.Sleep(100 * time.Millisecond)
	n, _ := svc.Store.TurnCount("real")
	if n != 0 {
		t.Fatalf("turns=%d", n)
	}
}

func TestClearDoesNotResetMemoryCursor(t *testing.T) {
	root := t.TempDir()
	cfg := memory.DefaultConfig()
	cfg.Enabled = true
	svc := memory.NewService(root, cfg)
	_ = svc.Store.Save(&memory.Profile{OpenID: "u1", ProfiledCount: 5, Role: memory.Field{Inferred: "r"}})

	store, err := sessions.Open(root + "/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	be := &captureBackend{}
	reg := registry.New()
	reg.Replace([]*bot.Bot{{
		Config:  bot.Config{ID: "b1", Backend: "openai", SessionMode: "per_user", MemoryEnabled: true},
		Backend: be,
	}})
	orch := &orchestrator.Orchestrator{Reg: reg, Store: store, Memory: svc}
	orch.Dispatch("b1", "hi", "u1", "N", map[string]string{"clear": "1"})
	p, _ := svc.Store.Get("u1")
	if p.ProfiledCount != 5 {
		t.Fatalf("cursor=%d", p.ProfiledCount)
	}
}
