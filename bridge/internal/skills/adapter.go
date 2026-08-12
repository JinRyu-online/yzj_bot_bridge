package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenAIToolSpec is backend-agnostic function-tool description.
type OpenAIToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any
}

func Resolve(store *Store, ids []string) ([]*Package, error) {
	var out []*Package
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		pkg, err := store.Get(id)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", id, err)
		}
		out = append(out, pkg)
	}
	return out, nil
}

func OpenAITools(pkgs []*Package) []OpenAIToolSpec {
	var out []OpenAIToolSpec
	for _, pkg := range pkgs {
		for _, t := range pkg.Manifest.Tools {
			desc := t.Description
			if desc == "" {
				desc = pkg.Manifest.Description
			}
			params := t.Parameters
			if params == nil {
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			out = append(out, OpenAIToolSpec{
				Name:        pkg.ToolExportName(t.Name),
				Description: fmt.Sprintf("[%s] %s", pkg.Manifest.ID, desc),
				Parameters:  params,
			})
		}
		// prompt_only with no tools: expose a single invoke tool
		if len(pkg.Manifest.Tools) == 0 {
			out = append(out, OpenAIToolSpec{
				Name:        pkg.ToolExportName("invoke"),
				Description: fmt.Sprintf("[%s] %s", pkg.Manifest.ID, pkg.Manifest.Description),
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"input": map[string]any{"type": "string", "description": "optional input"},
					},
				},
			})
		}
	}
	return out
}

func PromptAppendix(pkgs []*Package) string {
	if len(pkgs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## 可用桥接 Skills\n")
	for _, pkg := range pkgs {
		b.WriteString("- **")
		b.WriteString(pkg.Manifest.ID)
		b.WriteString("** (")
		b.WriteString(pkg.Manifest.Name)
		b.WriteString("): ")
		b.WriteString(pkg.Manifest.Description)
		b.WriteByte('\n')
		if pkg.SkillMD != "" {
			b.WriteString(pkg.SkillMD)
			b.WriteByte('\n')
		}
		for _, t := range pkg.Manifest.Tools {
			b.WriteString("  - tool `")
			b.WriteString(pkg.ToolExportName(t.Name))
			b.WriteString("`: ")
			b.WriteString(t.Description)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Materialize copies enabled skills into workspace client dirs for cursor/claude discovery.
func Materialize(pkgs []*Package, workspace, backend string) error {
	if len(pkgs) == 0 || strings.TrimSpace(workspace) == "" {
		return nil
	}
	backend = strings.ToLower(backend)
	for _, pkg := range pkgs {
		rel := clientRelPath(pkg, backend)
		if rel == "" {
			continue
		}
		dst := filepath.Join(workspace, filepath.FromSlash(rel))
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		if err := copyDir(pkg.Dir, dst); err != nil {
			return err
		}
	}
	return nil
}

func clientRelPath(pkg *Package, backend string) string {
	key := ""
	switch backend {
	case "cursor_cli", "cursor":
		key = "cursor"
	case "claude_code", "claude":
		key = "claude"
	default:
		return ""
	}
	if pkg.Manifest.ClientSync != nil {
		if s, ok := pkg.Manifest.ClientSync[key]; ok && strings.TrimSpace(s.Path) != "" {
			return strings.TrimPrefix(filepath.ToSlash(s.Path), "/")
		}
	}
	if key == "cursor" {
		return ".cursor/skills/" + pkg.Manifest.ID
	}
	return ".claude/skills/" + pkg.Manifest.ID
}
