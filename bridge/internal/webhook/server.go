package webhook

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/inbound"
	"yzj-bridge/internal/registry"
	"yzj-bridge/internal/signature"
)

type Server struct {
	Reg        *registry.Registry
	Dispatcher *inbound.Dispatcher
	Host       string
	Port       int

	mu     sync.Mutex
	srv    *http.Server
	running bool
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/", s.handle)
	addr := s.Host
	if addr == "" {
		addr = "0.0.0.0"
	}
	port := s.Port
	if port == 0 {
		port = 8765
	}
	s.srv = &http.Server{Addr: addr + ":" + itoa(port), Handler: mux}
	s.running = true
	go func() {
		log.Printf("webhook listening on %s", s.srv.Addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("webhook error: %v", err)
		}
	}()
	return nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		_ = s.srv.Close()
		s.running = false
	}
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad body", 400)
		return
	}
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		http.Error(w, "bad json", 400)
		return
	}
	path := r.URL.Path
	var target *bot.Bot
	for _, b := range s.Reg.List() {
		if !bot.ModeUsesWebhook(b.Config.InboundMode) {
			continue
		}
		if b.Config.WebhookPath == path || strings.TrimSuffix(b.Config.WebhookPath, "/") == strings.TrimSuffix(path, "/") {
			target = b
			break
		}
	}
	if target == nil {
		http.NotFound(w, r)
		return
	}
	n, ok := inbound.Normalize(raw)
	if !ok {
		http.Error(w, "not business", 400)
		return
	}
	if target.Config.Secret != "" && n.RobotID != "test-robotId" {
		sign := signature.ExtractSignHeader(r.Header)
		if ok, msg := signature.VerifySignature(n.Raw, sign, target.Config.Secret); !ok {
			http.Error(w, msg, 401)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"success":true,"data":{"type":2,"content":""}}`))
	go s.Dispatcher.Handle(target.Config.ID, n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
