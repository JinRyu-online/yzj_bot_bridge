package wssstate

import (
	"encoding/json"
	"os"
	"path/filepath"

	"yzj-bridge/internal/paths"
)

type File struct {
	Version int             `json:"version"`
	Enabled map[string]bool `json:"enabled"`
}

func Load() map[string]bool {
	path := paths.WSSEnabledPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]bool{}
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		var flat map[string]bool
		if json.Unmarshal(b, &flat) == nil {
			return flat
		}
		return map[string]bool{}
	}
	if f.Enabled == nil {
		return map[string]bool{}
	}
	return f.Enabled
}

func Save(enabled map[string]bool) error {
	path := paths.WSSEnabledPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f := File{Version: 1, Enabled: enabled}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
