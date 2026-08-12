package skills

import (
	"encoding/json"
	"strings"
)

type Runner struct{}

// LoadSkillTool handles OpenCode-style skill({ "name": "..." }) calls.
func (r *Runner) LoadSkillTool(store *Store, argsJSON string) string {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "invalid skill tool args: " + err.Error()
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return `skill tool requires { "name": "<skill-name>" }`
	}
	pkg, err := store.Get(name)
	if err != nil {
		return err.Error()
	}
	return FormatSkillLoadResult(pkg)
}
