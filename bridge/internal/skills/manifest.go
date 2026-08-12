package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var idRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// Manifest is the on-disk SKILL.yaml schema.
type Manifest struct {
	ID          string            `yaml:"id" json:"id"`
	Name        string            `yaml:"name" json:"name"`
	Version     string            `yaml:"version" json:"version"`
	Description string            `yaml:"description" json:"description"`
	Author      string            `yaml:"author" json:"author"`
	Tags        []string          `yaml:"tags" json:"tags"`
	Entry       Entry             `yaml:"entry" json:"entry"`
	Tools       []ToolDef         `yaml:"tools" json:"tools"`
	ClientSync  map[string]SyncDir `yaml:"client_sync" json:"client_sync"`
	MCP         map[string]any    `yaml:"mcp,omitempty" json:"mcp,omitempty"`
}

type Entry struct {
	Type       string   `yaml:"type" json:"type"` // shell | prompt_only | http
	Command    string   `yaml:"command" json:"command"`
	Args       []string `yaml:"args" json:"args"`
	TimeoutSec int      `yaml:"timeout_sec" json:"timeout_sec"`
}

type ToolDef struct {
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description" json:"description"`
	Parameters  map[string]any `yaml:"parameters" json:"parameters"`
}

type SyncDir struct {
	Path string `yaml:"path" json:"path"`
}

// Package is an installed skill on disk.
type Package struct {
	Manifest Manifest
	Dir      string
	SkillMD  string
}

func (p *Package) ToolExportName(toolName string) string {
	return "skill_" + p.Manifest.ID + "__" + toolName
}

func ParseToolExportName(export string) (skillID, toolName string, ok bool) {
	if !strings.HasPrefix(export, "skill_") {
		return "", "", false
	}
	rest := strings.TrimPrefix(export, "skill_")
	i := strings.Index(rest, "__")
	if i <= 0 || i >= len(rest)-2 {
		return "", "", false
	}
	return rest[:i], rest[i+2:], true
}

func LoadDir(dir string) (*Package, error) {
	dir = filepath.Clean(dir)
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read SKILL.yaml: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse SKILL.yaml: %w", err)
	}
	if err := ValidateManifest(&m); err != nil {
		return nil, err
	}
	base := filepath.Base(dir)
	if base != m.ID {
		return nil, fmt.Errorf("skill dir name %q must match id %q", base, m.ID)
	}
	pkg := &Package{Manifest: m, Dir: dir}
	if md, err := os.ReadFile(filepath.Join(dir, "SKILL.md")); err == nil {
		pkg.SkillMD = string(md)
	}
	return pkg, nil
}

func ValidateManifest(m *Manifest) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}
	if !idRe.MatchString(m.ID) {
		return fmt.Errorf("invalid skill id %q", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("skill name required")
	}
	if m.Version == "" {
		m.Version = "0.0.0"
	}
	t := strings.ToLower(strings.TrimSpace(m.Entry.Type))
	if t == "" {
		t = "prompt_only"
		m.Entry.Type = t
	}
	switch t {
	case "shell", "prompt_only", "http":
	default:
		return fmt.Errorf("invalid entry.type %q", m.Entry.Type)
	}
	if t == "shell" && strings.TrimSpace(m.Entry.Command) == "" {
		return fmt.Errorf("entry.command required for shell skills")
	}
	if m.Entry.TimeoutSec <= 0 {
		m.Entry.TimeoutSec = 120
	}
	seen := map[string]struct{}{}
	for i, tool := range m.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return fmt.Errorf("tools[%d].name required", i)
		}
		if strings.Contains(name, "__") {
			return fmt.Errorf("tool name %q must not contain __", name)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate tool name %q", name)
		}
		seen[name] = struct{}{}
		m.Tools[i].Name = name
		if m.Tools[i].Parameters == nil {
			m.Tools[i].Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
	}
	return nil
}
