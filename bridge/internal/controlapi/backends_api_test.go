package controlapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yzj-bridge/internal/runtime"
)

func newMetaTestServer(defs map[string]any) *Server {
	if defs == nil {
		defs = map[string]any{}
	}
	return &Server{RT: &runtime.Runtime{Defaults: defs}, Token: "test-token"}
}

func doMetaReq(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer "+s.Token)
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/backends/dsh/models", s.auth(s.dshModels))
	mux.HandleFunc("/v1/backends/available", s.auth(s.available))
	mux.ServeHTTP(w, r)
	return w
}

// TestDSHModelsEndpointOK 验证 GET /v1/backends/dsh/models 从 dsh_home 读 settings.yaml。
func TestDSHModelsEndpointOK(t *testing.T) {
	dir := t.TempDir()
	settings := `llm-deepseek:
  models:
    - id: deepseek-v4-flash
      name: DeepSeek V4 Flash
agent-default-model: kuaidi100/deepseek-v4-flash
`
	if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newMetaTestServer(map[string]any{"dsh_home": dir})
	w := doMetaReq(t, s, "/v1/backends/dsh/models")
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		OK     bool `json:"ok"`
		Models []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || len(resp.Models) != 1 || resp.Models[0].ID != "deepseek-v4-flash" {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.Models[0].Label != "DeepSeek V4 Flash" {
		t.Fatalf("label=%q", resp.Models[0].Label)
	}
}

// TestDSHModelsEndpointMissingFile 验证 settings.yaml / 桌面 profile 都缺失时返回 ok=false + 可操作错误。
func TestDSHModelsEndpointMissingFile(t *testing.T) {
	s := newMetaTestServer(map[string]any{"dsh_home": t.TempDir()})
	w := doMetaReq(t, s, "/v1/backends/dsh/models")
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.OK {
		t.Fatal("expected ok=false")
	}
	if !strings.Contains(resp.Error, "未找到 DSH 模型配置") || !strings.Contains(resp.Error, "DSH 桌面客户端") {
		t.Fatalf("error=%q", resp.Error)
	}
}

// TestDSHModelsEndpointDesktopProfile 验证 DSH 0.2.x 桌面客户端布局：
// 没有 settings.yaml 时从 profiles/desktop/cordis.patch.yml 列模型，并保留当前 dsh_model。
func TestDSHModelsEndpointDesktopProfile(t *testing.T) {
	dir := t.TempDir()
	desktopDir := filepath.Join(dir, "profiles", "desktop")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	patch := `- id: llm-pi-ai
  config:
    providers:
      kuaidi100:
        models:
          - id: deepseek-flash
            name: deepseek-flash
          - id: deepseek-v4-pro
            name: deepseek-v4-pro
- id: agent-default-model
  config:
    provider: kuaidi100
    model: deepseek-flash
`
	if err := os.WriteFile(filepath.Join(desktopDir, "cordis.patch.yml"), []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newMetaTestServer(map[string]any{"dsh_home": dir, "dsh_model": "deepseek-v4-flash-vision-exp"})
	w := doMetaReq(t, s, "/v1/backends/dsh/models")
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		OK     bool   `json:"ok"`
		Source string `json:"source"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("expected ok: %s", w.Body.String())
	}
	var ids []string
	for _, m := range resp.Models {
		ids = append(ids, m.ID)
	}
	for _, want := range []string{"deepseek-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in %v", want, ids)
		}
	}
	if !strings.Contains(resp.Source, "cordis.patch.yml") {
		t.Fatalf("source=%q", resp.Source)
	}
}

// TestAvailableEndpoint 验证 GET /v1/backends/available 返回 5 个引擎的可用性。
func TestAvailableEndpoint(t *testing.T) {
	s := newMetaTestServer(map[string]any{"openai_base_url": "https://example.com/v1"})
	w := doMetaReq(t, s, "/v1/backends/available")
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Backends []struct {
			ID        string `json:"id"`
			Label     string `json:"label"`
			Available bool   `json:"available"`
		} `json:"backends"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Backends) != 5 {
		t.Fatalf("backends=%d want 5: %+v", len(resp.Backends), resp.Backends)
	}
	byID := map[string]bool{}
	for _, b := range resp.Backends {
		byID[b.ID] = b.Available
		if b.Label == "" {
			t.Fatalf("label empty for %s", b.ID)
		}
	}
	if !byID["openai"] {
		t.Fatal("openai should be available")
	}
	if byID["opencode"] {
		t.Fatal("opencode should be unavailable")
	}
}
