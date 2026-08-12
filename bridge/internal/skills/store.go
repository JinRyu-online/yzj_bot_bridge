package skills

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"yzj-bridge/internal/paths"
)

type Store struct {
	Root string
}

func NewStore(root string) *Store {
	if strings.TrimSpace(root) == "" {
		root = paths.SkillsDir()
	}
	return &Store{Root: filepath.Clean(root)}
}

func (s *Store) EnsureRoot() error {
	return os.MkdirAll(s.Root, 0o755)
}

func (s *Store) List() ([]*Package, error) {
	if err := s.EnsureRoot(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	var out []*Package
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pkg, err := LoadDir(filepath.Join(s.Root, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, pkg)
	}
	return out, nil
}

func (s *Store) Get(id string) (*Package, error) {
	if !idRe.MatchString(id) {
		return nil, fmt.Errorf("invalid skill id %q", id)
	}
	return LoadDir(filepath.Join(s.Root, id))
}

func (s *Store) Has(id string) bool {
	_, err := s.Get(id)
	return err == nil
}

func (s *Store) InstallFromDir(src string) (*Package, error) {
	src = filepath.Clean(src)
	pkg, err := LoadDir(src)
	if err != nil {
		return nil, err
	}
	if err := s.EnsureRoot(); err != nil {
		return nil, err
	}
	dst := filepath.Join(s.Root, pkg.Manifest.ID)
	if err := os.RemoveAll(dst); err != nil {
		return nil, err
	}
	if err := copyDir(src, dst); err != nil {
		return nil, err
	}
	return LoadDir(dst)
}

func (s *Store) InstallFromZip(zipPath string) (*Package, error) {
	tmp, err := os.MkdirTemp("", "yzj-skill-zip-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := unzipSafe(zipPath, tmp); err != nil {
		return nil, err
	}
	root, err := findSkillRoot(tmp)
	if err != nil {
		return nil, err
	}
	return s.InstallFromDir(root)
}

func (s *Store) Uninstall(id string) error {
	if !idRe.MatchString(id) {
		return fmt.Errorf("invalid skill id %q", id)
	}
	dst := filepath.Join(s.Root, id)
	if _, err := os.Stat(dst); err != nil {
		return fmt.Errorf("skill %q not installed", id)
	}
	return os.RemoveAll(dst)
}

// ValidateIDs ensures every id is installed.
func (s *Store) ValidateIDs(ids []string) error {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !s.Has(id) {
			return fmt.Errorf("skill %q is not installed", id)
		}
	}
	return nil
}

func findSkillRoot(dir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, "SKILL.yaml")); err == nil {
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(p, "SKILL.yaml")); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("SKILL.yaml not found in zip")
}

func unzipSafe(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	dest = filepath.Clean(dest)
	for _, f := range r.File {
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("unsafe zip path %q", f.Name)
		}
		target := filepath.Join(dest, name)
		rel, err := filepath.Rel(dest, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("unsafe zip path %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, 32<<20))
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
