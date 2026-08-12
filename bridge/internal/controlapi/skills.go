package controlapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"yzj-bridge/internal/skills"
)

func (s *Server) skillsRoot(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/skills")
	path = strings.TrimPrefix(path, "/")
	switch {
	case path == "" && r.Method == http.MethodGet:
		s.skillsList(w, r)
	case path == "catalog" && r.Method == http.MethodGet:
		s.skillsCatalog(w, r)
	case path == "install" && r.Method == http.MethodPost:
		s.skillsInstall(w, r)
	case path != "" && !strings.Contains(path, "/") && r.Method == http.MethodGet:
		s.skillsGet(w, r, path)
	case path != "" && !strings.Contains(path, "/") && r.Method == http.MethodDelete:
		s.skillsDelete(w, r, path)
	default:
		http.Error(w, "method", 405)
	}
}

func (s *Server) skillStore() *skills.Store {
	if s.RT != nil && s.RT.SkillStore != nil {
		return s.RT.SkillStore
	}
	return skills.NewStore("")
}

func (s *Server) skillsList(w http.ResponseWriter, _ *http.Request) {
	list, err := s.skillStore().List()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, p := range list {
		items = append(items, map[string]any{
			"id": p.Manifest.ID, "name": p.Manifest.Name, "version": p.Manifest.Version,
			"description": p.Manifest.Description, "author": p.Manifest.Author,
			"tags": p.Manifest.Tags, "tools": p.Manifest.Tools,
		})
	}
	writeJSON(w, map[string]any{"skills": items})
}

func (s *Server) skillsGet(w http.ResponseWriter, _ *http.Request, id string) {
	pkg, err := s.skillStore().Get(id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	writeJSON(w, map[string]any{
		"id": pkg.Manifest.ID, "name": pkg.Manifest.Name, "version": pkg.Manifest.Version,
		"description": pkg.Manifest.Description, "author": pkg.Manifest.Author,
		"tags": pkg.Manifest.Tags, "tools": pkg.Manifest.Tools,
		"skill_md": pkg.SkillMD, "dir": pkg.Dir,
	})
}

func (s *Server) skillsDelete(w http.ResponseWriter, _ *http.Request, id string) {
	if err := s.skillStore().Uninstall(id); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) skillsCatalog(w http.ResponseWriter, _ *http.Request) {
	c, err := skills.LoadCatalog()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, c)
}

func (s *Server) skillsInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source    string `json:"source"` // dir | zip | catalog
		Path      string `json:"path"`
		URL       string `json:"url"`
		CatalogID string `json:"catalog_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	store := s.skillStore()
	var (
		pkg *skills.Package
		err error
	)
	switch strings.ToLower(strings.TrimSpace(body.Source)) {
	case "dir":
		pkg, err = store.InstallFromDir(body.Path)
	case "zip":
		pkg, err = store.InstallFromZip(body.Path)
	case "catalog":
		id := body.CatalogID
		if id == "" {
			id = body.Path
		}
		pkg, err = store.InstallFromCatalog(id)
	default:
		http.Error(w, `source must be "dir", "zip", or "catalog"`, 400)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, map[string]any{
		"ok": true, "id": pkg.Manifest.ID, "name": pkg.Manifest.Name, "version": pkg.Manifest.Version,
	})
}
