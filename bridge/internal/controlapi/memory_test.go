package controlapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"yzj-bridge/internal/memory"
	"yzj-bridge/internal/runtime"
)

func TestMemoryAPIListFilterAndDeleteConfirm(t *testing.T) {
	root := t.TempDir()
	cfg := memory.DefaultConfig()
	cfg.Enabled = true
	svc := memory.NewService(filepath.Join(root, "memory"), cfg)
	_ = svc.Store.Save(&memory.Profile{OpenID: "u1", Role: memory.Field{Inferred: "r"}, BotsSeen: []string{"b1"}})
	_ = svc.Store.AppendTurn("u1", memory.Turn{User: "q", Assistant: "a", BotID: "b1"})

	rt := &runtime.Runtime{Memory: svc, Defaults: map[string]any{}}
	srv := &Server{RT: rt, Token: "tok"}

	req := httptest.NewRequest(http.MethodGet, "/v1/memory/profiles?bot=b1", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	srv.memoryProfiles(rr, req)
	if rr.Code != 200 {
		t.Fatalf("list status %d %s", rr.Code, rr.Body.String())
	}
	var list struct {
		Profiles []map[string]any `json:"profiles"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list.Profiles) != 1 {
		t.Fatalf("profiles=%v", list.Profiles)
	}
	raw, _ := json.Marshal(list.Profiles[0])
	if strings.Contains(string(raw), `"user":`) || strings.Contains(string(raw), "assistant") {
		t.Fatalf("must not include turn content: %s", raw)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/memory/profiles/u1", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr = httptest.NewRecorder()
	srv.memoryProfilesPath(rr, req)
	if rr.Code != 400 {
		t.Fatalf("want 400 got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/memory/profiles/u1?confirm=1", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr = httptest.NewRecorder()
	srv.memoryProfilesPath(rr, req)
	if rr.Code != 200 {
		t.Fatalf("delete %d %s", rr.Code, rr.Body.String())
	}
	p, _ := svc.Store.Get("u1")
	if p != nil {
		t.Fatal("profile should be gone")
	}
}

func TestMemoryEnableCheck(t *testing.T) {
	rt := &runtime.Runtime{Defaults: map[string]any{
		"openai_base_url": "",
		"openai_api_key":  "",
		"claude_bin":      filepath.Join(t.TempDir(), "missing-claude"),
		"memory":          map[string]any{},
	}}
	srv := &Server{RT: rt, Token: "tok"}
	req := httptest.NewRequest(http.MethodPost, "/v1/memory/enable-check", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	srv.memoryEnableCheck(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d", rr.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if _, ok := out["ok"]; !ok {
		t.Fatalf("%v", out)
	}
}

func TestMemoryLockEndpoint(t *testing.T) {
	root := t.TempDir()
	svc := memory.NewService(filepath.Join(root, "memory"), memory.DefaultConfig())
	_ = svc.Store.Save(&memory.Profile{OpenID: "u1"})
	rt := &runtime.Runtime{Memory: svc}
	srv := &Server{RT: rt, Token: "tok"}

	req := httptest.NewRequest(http.MethodPost, "/v1/memory/profiles/u1/lock", strings.NewReader(`{"fields":{"role":true}}`))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.memoryProfilesPath(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	p, _ := svc.Store.Get("u1")
	if !p.Role.Locked {
		t.Fatal("not locked")
	}
}
