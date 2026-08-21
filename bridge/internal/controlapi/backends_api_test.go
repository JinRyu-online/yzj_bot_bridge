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

// TestDSHModelsEndpointMissingFile 验证配置文件缺失时返回 ok=false + 中文错误。
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
	if !strings.Contains(resp.Error, "未找到 DSH 配置文件") || !strings.Contains(resp.Error, "请先部署 dsh profile") {
		t.Fatalf("error=%q", resp.Error)
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
