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

// OpenAISkillTool builds an OpenCode-style progressive skill loader:
// advertise name+description in the tool description; full body loads only when called.
func OpenAISkillTool(pkgs []*Package) (OpenAIToolSpec, bool) {
	if len(pkgs) == 0 {
		return OpenAIToolSpec{}, false
	}
	names := make([]string, 0, len(pkgs))
	var list strings.Builder
	list.WriteString("Load a specialized skill by name (Agent Skills / OpenCode-style progressive disclosure). ")
	list.WriteString("Only name and description are listed here; call this tool to load full SKILL.md instructions. ")
	list.WriteString("Available skills:\n")
	for _, pkg := range pkgs {
		names = append(names, pkg.Manifest.Name)
		list.WriteString("- ")
		list.WriteString(pkg.Manifest.Name)
		list.WriteString(": ")
		list.WriteString(pkg.Manifest.Description)
		list.WriteByte('\n')
	}
	return OpenAIToolSpec{
		Name:        "skill",
		Description: strings.TrimSpace(list.String()),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Exact skill name to load",
					"enum":        names,
				},
			},
			"required": []string{"name"},
		},
	}, true
}

// OpenAISkillSystemHint is a short system note (no full skill bodies).
func OpenAISkillSystemHint() string {
	return "\n\nWhen a task matches an available skill, call the skill tool with that skill's name before improvising. " +
		"After loading, follow the skill instructions; use read_file/run_command for scripts/ and references/ under the skill directory when needed."
}

// PromptAppendix is used by Cursor/Claude backends: inject skill summaries into the prompt.
func PromptAppendix(pkgs []*Package) string {
	if len(pkgs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## 可用桥接 Skills\n")
	for _, pkg := range pkgs {
		b.WriteString("- **")
		b.WriteString(pkg.Manifest.ID)
		b.WriteString("**: ")
		b.WriteString(pkg.Manifest.Description)
		b.WriteByte('\n')
		if pkg.SkillMD != "" {
			b.WriteString(pkg.SkillMD)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// FormatSkillLoadResult returns tool output after skill({name}) is called.
func FormatSkillLoadResult(pkg *Package) string {
	if pkg == nil {
		return "skill not found"
	}
	var b strings.Builder
	b.WriteString("# Skill: ")
	b.WriteString(pkg.Manifest.Name)
	b.WriteByte('\n')
	b.WriteString(pkg.Manifest.Description)
	b.WriteString("\n\n")
	b.WriteString("Skill directory: ")
	b.WriteString(pkg.Dir)
	b.WriteString("\n\n")
	if pkg.SkillMD != "" {
		b.WriteString(pkg.SkillMD)
		b.WriteByte('\n')
	}
	if sample := sampleSkillFiles(pkg.Dir, 10); sample != "" {
		b.WriteString("\nSupporting files (read on demand):\n")
		b.WriteString(sample)
	}
	return b.String()
}

func sampleSkillFiles(root string, limit int) string {
	var lines []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.EqualFold(base, "SKILL.md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		lines = append(lines, "- "+filepath.ToSlash(rel))
		if len(lines) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	return strings.Join(lines, "\n")
}

// Materialize copies enabled skills into workspace client dirs for agent discovery.
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
		md, err := FormatSkillMarkdown(pkg.Manifest, pkg.SkillMD)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte(md), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func clientRelPath(pkg *Package, backend string) string {
	switch backend {
	case "cursor_cli", "cursor":
		return ".cursor/skills/" + pkg.Manifest.ID
	case "claude_code", "claude":
		return ".claude/skills/" + pkg.Manifest.ID
	case "openai", "opencode":
		// Cross-client Agent Skills path (OpenCode / agentskills compatible).
		return ".agents/skills/" + pkg.Manifest.ID
	default:
		return ""
	}
}
