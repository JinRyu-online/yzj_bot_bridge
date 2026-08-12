package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"yzj-bridge/internal/processutil"
)

const maxOutput = 256 * 1024

type Runner struct{}

func (r *Runner) Exec(ctx context.Context, pkg *Package, toolName, argsJSON, workspace string) string {
	if pkg == nil {
		return "skill package is nil"
	}
	workspace = filepath.Clean(workspace)
	entryType := strings.ToLower(pkg.Manifest.Entry.Type)
	switch entryType {
	case "prompt_only":
		var b strings.Builder
		b.WriteString(pkg.Manifest.Description)
		if pkg.SkillMD != "" {
			b.WriteString("\n\n")
			b.WriteString(pkg.SkillMD)
		}
		b.WriteString("\n\ntool=")
		b.WriteString(toolName)
		b.WriteString("\nargs=")
		b.WriteString(argsJSON)
		return b.String()
	case "http":
		return "http skill entry is not implemented yet"
	case "shell":
		return r.execShell(ctx, pkg, toolName, argsJSON, workspace)
	default:
		return "unsupported entry.type: " + entryType
	}
}

func (r *Runner) execShell(ctx context.Context, pkg *Package, toolName, argsJSON, workspace string) string {
	timeout := time.Duration(pkg.Manifest.Entry.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := append([]string{}, pkg.Manifest.Entry.Args...)
	// Resolve script paths relative to skill dir.
	for i, a := range args {
		if strings.Contains(a, "..") {
			continue
		}
		cand := filepath.Join(pkg.Dir, a)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			args[i] = cand
		}
	}

	cmd := exec.CommandContext(cctx, pkg.Manifest.Entry.Command, args...)
	processutil.HideWindow(cmd)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(),
		"YZJ_SKILL_ID="+pkg.Manifest.ID,
		"YZJ_SKILL_TOOL="+toolName,
		"YZJ_SKILL_ARGS="+argsJSON,
		"YZJ_WORKSPACE="+workspace,
		"YZJ_SKILL_DIR="+pkg.Dir,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if stderr.Len() > 0 {
		if out != "" {
			out += "\n"
		}
		out += stderr.String()
	}
	if len(out) > maxOutput {
		out = out[:maxOutput] + "\n...(truncated)"
	}
	if err != nil {
		if out == "" {
			return fmt.Sprintf("skill exec failed: %v", err)
		}
		return fmt.Sprintf("%s\n[exit error: %v]", out, err)
	}
	if strings.TrimSpace(out) == "" {
		return "(skill produced no output)"
	}
	return out
}

// ExecByExportName resolves skill_<id>__<tool> and runs it.
func (r *Runner) ExecByExportName(ctx context.Context, store *Store, exportName, argsJSON, workspace string) string {
	skillID, toolName, ok := ParseToolExportName(exportName)
	if !ok {
		return "unknown skill tool: " + exportName
	}
	pkg, err := store.Get(skillID)
	if err != nil {
		return err.Error()
	}
	found := false
	for _, t := range pkg.Manifest.Tools {
		if t.Name == toolName {
			found = true
			break
		}
	}
	if !found && len(pkg.Manifest.Tools) > 0 {
		return "skill tool not found: " + toolName
	}
	_ = json.Valid([]byte(argsJSON))
	return r.Exec(ctx, pkg, toolName, argsJSON, workspace)
}
