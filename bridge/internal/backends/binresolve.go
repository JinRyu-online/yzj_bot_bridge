package backends

import (
	"os"
	"path/filepath"
	"strings"
)

func resolveWindowsBin(raw string, altNames ...string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if strings.EqualFold(filepath.Ext(raw), ".cmd") {
		dir := filepath.Dir(raw)
		base := strings.TrimSuffix(filepath.Base(raw), filepath.Ext(raw))
		candidates := append([]string{base + ".exe"}, altNames...)
		for _, name := range candidates {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return raw
}
