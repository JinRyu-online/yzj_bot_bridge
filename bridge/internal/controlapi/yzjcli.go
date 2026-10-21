package controlapi

import (
	"net/http"
	"strconv"
	"strings"
)

// yzjCliStatus reports yzj-cli discovery / version / login state.
func (s *Server) yzjCliStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.RT == nil {
		http.Error(w, "runtime unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, s.RT.YzjCLIStatus(r.Context()))
}

// yzjCliGroups lists recent/searchable conversations for the GUI group picker.
func (s *Server) yzjCliGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.RT == nil {
		http.Error(w, "runtime unavailable", http.StatusServiceUnavailable)
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	limit := 20
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	groups, err := s.RT.YzjCLIGroups(r.Context(), keyword, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"list": groups})
}
