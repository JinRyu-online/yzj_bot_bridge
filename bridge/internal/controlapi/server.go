package controlapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"yzj-bridge/internal/backends"
	"yzj-bridge/internal/chatstore"
	"yzj-bridge/internal/logbuf"
	"yzj-bridge/internal/paths"
	"yzj-bridge/internal/runtime"
)

type Server struct {
	RT      *runtime.Runtime
	Addr    string
	Token   string
	Logs    *logbuf.Buffer
	OnShutdown func()

	// Chat is the lazily-initialized GUI chat test store. Leave nil to
	// auto-create one at ChatPath (or the default path on first use).
	Chat     *chatstore.Store
	ChatPath string

	chatOnce sync.Once
	mu  sync.Mutex
	srv *http.Server
}

func NewToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Server) WriteTokenFile() (string, error) {
	dir := os.TempDir()
	path := filepath.Join(dir, "yzj-bridge.token")
	content := s.Token + "\n" + s.Addr + "\n"
	return path, os.WriteFile(path, []byte(content), 0o600)
}

func (s *Server) Start() error {
	if s.Addr == "" {
		s.Addr = "127.0.0.1:18765"
	}
	if s.Token == "" {
		s.Token = NewToken()
	}
	if s.Logs == nil {
		s.Logs = logbuf.New(2000)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.auth(s.health))
	mux.HandleFunc("/v1/status", s.auth(s.status))
	mux.HandleFunc("/v1/wss/start", s.auth(s.wssStart))
	mux.HandleFunc("/v1/wss/stop", s.auth(s.wssStop))
	mux.HandleFunc("/v1/wss/role/", s.auth(s.wssRole))
	mux.HandleFunc("/v1/wss/channel/", s.auth(s.wssChannel))
	mux.HandleFunc("/v1/config", s.auth(s.config))
	mux.HandleFunc("/v1/reload", s.auth(s.reload))
	mux.HandleFunc("/v1/logs", s.auth(s.logs))
	mux.HandleFunc("/v1/shutdown", s.auth(s.shutdown))
	mux.HandleFunc("/v1/paths", s.auth(s.paths))
	mux.HandleFunc("/v1/backends/cursor/models", s.auth(s.cursorModels))
	mux.HandleFunc("/v1/backends/claude/models", s.auth(s.claudeModels))
	mux.HandleFunc("/v1/backends/dsh/models", s.auth(s.dshModels))
	mux.HandleFunc("/v1/backends/available", s.auth(s.available))
	mux.HandleFunc("/v1/backends/openai/probe", s.auth(s.openaiProbe))
	mux.HandleFunc("/v1/backends/cli/discover", s.auth(s.cliDiscover))
	mux.HandleFunc("/v1/skills", s.auth(s.skillsRoot))
	mux.HandleFunc("/v1/skills/", s.auth(s.skillsRoot))
	mux.HandleFunc("/v1/chat/sessions", s.auth(s.chatSessions))
	mux.HandleFunc("/v1/chat/sessions/", s.auth(s.chatSessionsPath))
	mux.HandleFunc("/v1/memory/profiles", s.auth(s.memoryProfiles))
	mux.HandleFunc("/v1/memory/profiles/", s.auth(s.memoryProfilesPath))
	mux.HandleFunc("/v1/memory/enable-check", s.auth(s.memoryEnableCheck))

	s.srv = &http.Server{Addr: s.Addr, Handler: mux}
	path, _ := s.WriteTokenFile()
	log.Printf("control API on http://%s token file %s", s.Addr, path)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("control api: %v", err)
		}
	}()
	return nil
}

func (s *Server) Stop() {
	if s.srv != nil {
		_ = s.srv.Close()
	}
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			// health allows unauthenticated lightweight check optionally with token
		}
		auth := r.Header.Get("Authorization")
		tok := strings.TrimPrefix(auth, "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		if r.URL.Path != "/health" && tok != s.Token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/health" && tok != "" && tok != s.Token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"bots": s.RT.SnapshotStatus()})
}

func (s *Server) wssStart(w http.ResponseWriter, r *http.Request) {
	s.RT.StartAllWSS()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) wssStop(w http.ResponseWriter, r *http.Request) {
	s.RT.StopAllWSS()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) wssRole(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/wss/role/")
	var body struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.RT.SetRoleEnabled(id, body.Enabled)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) wssChannel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/wss/channel/")
	var body struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.RT.SetChannelEnabled(id, body.Enabled); err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.RT.GetRawConfig())
	case http.MethodPut:
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		keep := r.URL.Query().Get("keep_wss_state") != "0"
		if err := s.RT.SaveAndReload(m, keep); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "method", 405)
	}
}

func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	keep := r.URL.Query().Get("keep_wss_state") != "0"
	if err := s.RT.ReloadFromDisk(keep); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var since int64
		_, _ = fmtSscanf(r.URL.Query().Get("since_seq"), &since)
		bot := r.URL.Query().Get("bot")
		writeJSON(w, map[string]any{"lines": s.Logs.Since(since, bot)})
	case http.MethodPost:
		var body struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		msg := strings.TrimSpace(body.Message)
		if msg == "" {
			http.Error(w, "message required", 400)
			return
		}
		level := strings.ToUpper(strings.TrimSpace(body.Level))
		if level == "" {
			level = "INFO"
		}
		// bot=gui distinguishes panel events; UI renders a GUI badge (no [GUI] prefix).
		line := s.Logs.Append(level, "gui", msg)
		writeJSON(w, map[string]any{"ok": true, "line": line})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) shutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true})
	go func() {
		s.RT.Shutdown()
		s.Stop()
		if s.OnShutdown != nil {
			s.OnShutdown()
		}
	}()
}

func (s *Server) paths(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"config": s.RT.ConfigPath(),
		"data":   s.RT.DataDir(),
	})
}

// cursorModels 列出 Cursor CLI（agent models）可用模型。
func (s *Server) cursorModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	defs := s.RT.Defaults
	if defs == nil {
		defs = map[string]any{}
	}
	bin, _ := defs["cursor_bin"].(string)
	key, _ := defs["cursor_api_key"].(string)
	ws := ""
	if s.RT != nil && s.RT.Reg != nil {
		for _, b := range s.RT.Reg.List() {
			be := strings.ToLower(strings.TrimSpace(b.Config.Backend))
			if be == "cursor_cli" || be == "cursor" {
				ws = b.Config.Workspace
				break
			}
		}
	}
	if ws == "" {
		ws = filepath.Join(paths.UserDataDir(), "workspace")
	}
	models, err := backends.ListCursorModels(bin, key, ws)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error(), "models": []any{}})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "models": models})
}

// claudeModels 列出 Claude Code 可用模型（本地 CLI 别名）。
func (s *Server) claudeModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	defs := s.RT.Defaults
	if defs == nil {
		defs = map[string]any{}
	}
	bin, _ := defs["claude_bin"].(string)
	models, warn := backends.ListClaudeModels(bin)
	out := map[string]any{"ok": true, "models": models}
	if warn != "" {
		out["warning"] = warn
	}
	writeJSON(w, out)
}

// dshModels 列出 DSH 配置文件（settings.yaml）中的可用模型，供 GUI 模型下拉。
func (s *Server) dshModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	defs := s.RT.Defaults
	if defs == nil {
		defs = map[string]any{}
	}
	dshHome, _ := defs["dsh_home"].(string)
	path := backends.ResolveDSHSettingsPath(dshHome)
	models, err := backends.ListDSHModelsFile(path)
	if err != nil {
		writeJSON(w, map[string]any{
			"ok":     false,
			"error":  "未找到 DSH 配置文件 " + path + "（请先部署 dsh profile）",
			"models": []any{},
		})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "models": models})
}

// available 返回各后端引擎是否已配置可用，供 GUI 机器人表单过滤后端下拉。
func (s *Server) available(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	defs := s.RT.Defaults
	if defs == nil {
		defs = map[string]any{}
	}
	writeJSON(w, map[string]any{"backends": backends.AvailableBackends(defs)})
}

// openaiProbe 用给定或 defaults 中的 Base URL / API Key 做连通性探测。
func (s *Server) openaiProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.BaseURL == "" || body.APIKey == "" {
		defs := s.RT.Defaults
		if defs != nil {
			if body.BaseURL == "" {
				body.BaseURL, _ = defs["openai_base_url"].(string)
			}
			if body.APIKey == "" {
				body.APIKey, _ = defs["openai_api_key"].(string)
			}
		}
	}
	res := backends.ProbeOpenAI(body.BaseURL, body.APIKey)
	writeJSON(w, res)
}

// cliDiscover 扫描本机 Cursor CLI / Claude Code 可执行文件，并返回官方安装提示。
func (s *Server) cliDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	engine := r.URL.Query().Get("engine")
	configured := r.URL.Query().Get("bin")
	if r.Method == http.MethodPost {
		var body struct {
			Engine string `json:"engine"`
			Bin    string `json:"bin"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Engine != "" {
			engine = body.Engine
		}
		if body.Bin != "" {
			configured = body.Bin
		}
	}
	if engine == "" {
		http.Error(w, "engine required (cursor|claude)", 400)
		return
	}
	if configured == "" && s.RT != nil && s.RT.Defaults != nil {
		switch strings.ToLower(strings.TrimSpace(engine)) {
		case "cursor", "cursor_cli", "agent":
			configured, _ = s.RT.Defaults["cursor_bin"].(string)
		case "claude", "claude_code":
			configured, _ = s.RT.Defaults["claude_bin"].(string)
		}
	}
	writeJSON(w, backends.DiscoverCLI(engine, configured))
}

func fmtSscanf(s string, since *int64) (int, error) {
	if s == "" {
		return 0, nil
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	*since = n
	return 1, nil
}
