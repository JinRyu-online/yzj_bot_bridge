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

// Manifest mirrors Cursor / Agent Skills SKILL.md frontmatter.
// See https://cursor.com/docs/skills
type Manifest struct {
	Name                     string         `yaml:"name" json:"name"`
	Description              string         `yaml:"description" json:"description"`
	Paths                    any            `yaml:"paths,omitempty" json:"paths,omitempty"`
	DisableModelInvocation   bool           `yaml:"disable-model-invocation,omitempty" json:"disable_model_invocation,omitempty"`
	Metadata                 map[string]any `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	ID                       string         `yaml:"-" json:"id"` // always equals Name after validate
}

// Package is an installed skill on disk.
type Package struct {
	Manifest Manifest
	Dir      string
	// SkillMD is the markdown body below frontmatter (agent instructions).
	SkillMD string
}

// ParseSkillMarkdown splits Cursor/Claude-style SKILL.md into frontmatter + body.
func ParseSkillMarkdown(raw string) (Manifest, string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	trimmed := strings.TrimSpace(raw)
	var m Manifest
	if !strings.HasPrefix(trimmed, "---") {
		return m, strings.TrimSpace(raw), nil
	}
	rest := strings.TrimPrefix(trimmed, "---")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return m, "", fmt.Errorf("SKILL.md: missing closing frontmatter delimiter")
	}
	fm := rest[:end]
	body := strings.TrimSpace(rest[end+len("\n---"):])
	if err := yaml.Unmarshal([]byte(fm), &m); err != nil {
		return m, "", fmt.Errorf("parse SKILL.md frontmatter: %w", err)
	}
	return m, body, nil
}

func LoadDir(dir string) (*Package, error) {
	dir = filepath.Clean(dir)
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil, fmt.Errorf("skill package requires SKILL.md: %w", err)
	}
	m, body, err := ParseSkillMarkdown(string(data))
	if err != nil {
		return nil, err
	}
	if err := ValidateManifest(&m); err != nil {
		return nil, err
	}
	// Source folder name need not match (e.g. zip/tgz extract temp dirs).
	// InstallFromDir always copies into skills/<name>/.
	return &Package{Manifest: m, Dir: dir, SkillMD: body}, nil
}

func ValidateManifest(m *Manifest) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}
	m.Name = strings.TrimSpace(m.Name)
	m.Description = strings.TrimSpace(m.Description)
	if !idRe.MatchString(m.Name) {
		return fmt.Errorf("invalid skill name %q", m.Name)
	}
	if m.Description == "" {
		return fmt.Errorf("skill description required")
	}
	m.ID = m.Name
	return nil
}

// FormatSkillMarkdown renders official SKILL.md (frontmatter + body).
func FormatSkillMarkdown(m Manifest, body string) (string, error) {
	if err := ValidateManifest(&m); err != nil {
		return "", err
	}
	out := struct {
		Name                   string         `yaml:"name"`
		Description            string         `yaml:"description"`
		Paths                  any            `yaml:"paths,omitempty"`
		DisableModelInvocation bool           `yaml:"disable-model-invocation,omitempty"`
		Metadata               map[string]any `yaml:"metadata,omitempty"`
	}{
		Name:                   m.Name,
		Description:            m.Description,
		Paths:                  m.Paths,
		DisableModelInvocation: m.DisableModelInvocation,
		Metadata:               m.Metadata,
	}
	b, err := yaml.Marshal(&out)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.Write(b)
	if !strings.HasSuffix(string(b), "\n") {
		sb.WriteByte('\n')
	}
	sb.WriteString("---\n")
	body = strings.TrimSpace(body)
	if body != "" {
		sb.WriteByte('\n')
		sb.WriteString(body)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}
