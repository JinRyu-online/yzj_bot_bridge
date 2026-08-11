package paths

import (
	"os"
	"path/filepath"
)

// UserDataDir returns ~/.yzj-bridge
func UserDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".yzj-bridge")
	}
	return filepath.Join(home, ".yzj-bridge")
}

func ConfigPath() string {
	return filepath.Join(UserDataDir(), "config.yaml")
}

func SessionsPath(name string) string {
	if name == "" {
		name = "sessions.json"
	}
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(UserDataDir(), name)
}

func WSSEnabledPath() string {
	return filepath.Join(UserDataDir(), "wss_enabled.json")
}

func EnsureUserData() error {
	return os.MkdirAll(UserDataDir(), 0o755)
}
