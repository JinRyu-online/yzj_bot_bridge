package memory

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var nonSafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// SanitizeOpenID turns an openID into a safe filename segment.
func SanitizeOpenID(openID string) string {
	s := strings.TrimSpace(openID)
	if s == "" {
		return "_empty"
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := nonSafe.ReplaceAllString(b.String(), "_")
	out = strings.Trim(out, "._-")
	if out == "" {
		return "_empty"
	}
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}

// ProfilesDir returns ~/.yzj-bridge/memory/profiles (or root override).
func ProfilesDir(root string) string {
	return filepath.Join(root, "profiles")
}

// TurnsDir returns ~/.yzj-bridge/memory/turns (or root override).
func TurnsDir(root string) string {
	return filepath.Join(root, "turns")
}

func profilePath(root, openID string) string {
	return filepath.Join(ProfilesDir(root), SanitizeOpenID(openID)+".json")
}

func turnsPath(root, openID string) string {
	return filepath.Join(TurnsDir(root), SanitizeOpenID(openID)+".jsonl")
}
