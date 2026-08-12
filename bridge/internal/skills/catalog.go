package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"yzj-bridge/internal/paths"
)

type CatalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Version     string `json:"version"`
}

type Catalog struct {
	Skills []CatalogEntry `json:"skills"`
}

var (
	catalogOnce sync.Once
	catalogRoot string
)

// SetCatalogRoot overrides where catalog.json and bundled skills live (tests / packaging).
func SetCatalogRoot(root string) {
	catalogRoot = root
}

func CatalogRoot() string {
	catalogOnce.Do(func() {
		if catalogRoot != "" {
			return
		}
		candidates := []string{
			"skills-catalog",
			filepath.Join("..", "skills-catalog"),
			filepath.Join("..", "..", "skills-catalog"),
			filepath.Join(paths.UserDataDir(), "skills-catalog"),
		}
		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			candidates = append(candidates,
				filepath.Join(dir, "skills-catalog"),
				filepath.Join(dir, "..", "skills-catalog"),
				filepath.Join(dir, "..", "..", "skills-catalog"),
				filepath.Join(dir, "..", "..", "..", "skills-catalog"),
			)
		}
		for _, c := range candidates {
			p := filepath.Clean(c)
			if st, err := os.Stat(filepath.Join(p, "catalog.json")); err == nil && !st.IsDir() {
				catalogRoot = p
				return
			}
		}
		catalogRoot = "skills-catalog"
	})
	return catalogRoot
}

func LoadCatalog() (*Catalog, error) {
	root := CatalogRoot()
	data, err := os.ReadFile(filepath.Join(root, "catalog.json"))
	if err != nil {
		return &Catalog{Skills: nil}, nil
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	for i := range c.Skills {
		if c.Skills[i].Path == "" {
			c.Skills[i].Path = c.Skills[i].ID
		}
	}
	return &c, nil
}

func (s *Store) InstallFromCatalog(id string) (*Package, error) {
	c, err := LoadCatalog()
	if err != nil {
		return nil, err
	}
	var ent *CatalogEntry
	for i := range c.Skills {
		if c.Skills[i].ID == id {
			ent = &c.Skills[i]
			break
		}
	}
	if ent == nil {
		return nil, fmt.Errorf("catalog skill %q not found", id)
	}
	src := ent.Path
	if !filepath.IsAbs(src) {
		src = filepath.Join(CatalogRoot(), src)
	}
	return s.InstallFromDir(src)
}
