package yzjcli

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"yzj-bridge/internal/paths"
)

// Config holds the yzj-cli discovery/install settings from defaults.
type Config struct {
	Bin         string
	Version     string
	Profile     string
	ConfigDir   string
	Timeout     time.Duration
	AutoInstall bool
}

const installCDN = "https://www.yunzhijia.com/mixed/yzj-cli/%s-%s/release/yzj-cli-%s.pak"

// knownChecksums pins the official .pak binary digest per version so a
// tampered CDN cannot silently replace the executable. Update deliberately.
var knownChecksums = map[string]string{
	"0.1.8": "c6db6cc985269906bc4075cf6a0cd8932c119f7ca764e5d11db45b35799e098a",
}

// ManagedDir returns the bridge-owned directory for the CLI binary.
func ManagedDir() string { return filepath.Join(paths.UserDataDir(), "bin") }

// managedBinPath returns the versioned managed executable path.
func managedBinPath(version string) string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return filepath.Join(ManagedDir(), "yzj-cli-"+sanitizeVersion(version)+ext)
}

func sanitizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "latest"
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "latest"
	}
	return b.String()
}

// ResolveBinary locates an existing yzj-cli executable without installing.
func ResolveBinary(cfg Config) (string, error) {
	if p := strings.TrimSpace(cfg.Bin); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
		return "", fmt.Errorf("yzj_cli.bin not found: %s", p)
	}
	if p := managedBinPath(cfg.Version); fileExists(p) {
		return p, nil
	}
	// npm global install: package ships the real binary next to the Node shim.
	for _, base := range npmGlobalDirs() {
		exe := "yzj-cli"
		if runtime.GOOS == "windows" {
			exe += ".exe"
		}
		for _, cand := range []string{
			filepath.Join(base, "node_modules", "@yunzhijia", "cli", "bin", exe),
			filepath.Join(base, "node_modules", "@yunzhijia", "cli", "target", "release", exe),
		} {
			if fileExists(cand) {
				return cand, nil
			}
		}
	}
	// PATH (npm shim may be a .cmd on Windows; ExecRunner handles that).
	if p, err := exec.LookPath("yzj-cli"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("yzj-cli not found (set defaults.yzj_cli.bin or enable auto_install)")
}

func npmGlobalDirs() []string {
	var dirs []string
	home, _ := os.UserHomeDir()
	if home != "" {
		dirs = append(dirs, filepath.Join(home, "AppData", "Roaming", "npm")) // windows default
		dirs = append(dirs, filepath.Join(home, ".npm-global"))
	}
	return dirs
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// EnsureBinary resolves the CLI, auto-installing the pinned version when
// allowed and missing. It returns the resolved executable path.
func EnsureBinary(ctx context.Context, cfg Config) (string, error) {
	if p, err := ResolveBinary(cfg); err == nil {
		return p, nil
	} else if !cfg.AutoInstall {
		return "", err
	}
	return Install(ctx, cfg)
}

// Install downloads the pinned official .pak, verifies its SHA256 and unpacks
// the executable into the bridge-managed directory.
func Install(ctx context.Context, cfg Config) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("automatic yzj-cli install is currently implemented for Windows only")
	}
	version := strings.TrimSpace(cfg.Version)
	if version == "" {
		return "", fmt.Errorf("yzj_cli.version is required for auto install")
	}
	url := fmt.Sprintf(installCDN, "windows", "x64", version)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return "", fmt.Errorf("download yzj-cli: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download yzj-cli: http %d", resp.StatusCode)
	}
	const maxPak = 64 << 20
	pak, err := io.ReadAll(io.LimitReader(resp.Body, maxPak+1))
	if err != nil {
		return "", err
	}
	if len(pak) > maxPak {
		return "", fmt.Errorf("download yzj-cli: package too large")
	}
	if len(pak) < 4 || string(pak[:2]) != "PK" {
		return "", fmt.Errorf("download yzj-cli: not a zip package")
	}
	zr, err := zip.NewReader(bytes.NewReader(pak), int64(len(pak)))
	if err != nil {
		return "", fmt.Errorf("open yzj-cli package: %w", err)
	}
	var updateJSON []byte
	var exeBytes []byte
	for _, f := range zr.File {
		switch strings.ToLower(filepath.Base(f.Name)) {
		case "update.json":
			updateJSON, err = readZipFile(f, 1<<20)
		case "yzj-cli.exe", "yzj-cli":
			exeBytes, err = readZipFile(f, maxPak)
		}
		if err != nil {
			return "", err
		}
	}
	if len(exeBytes) == 0 {
		return "", fmt.Errorf("yzj-cli package missing executable")
	}
	sum := sha256.Sum256(exeBytes)
	digest := hex.EncodeToString(sum[:])
	expected := knownChecksums[version]
	if updateJSON != nil {
		var meta struct {
			Version  string `json:"version"`
			Checksum string `json:"checksum"`
		}
		if err := json.Unmarshal(updateJSON, &meta); err == nil {
			if meta.Version != "" && meta.Version != version {
				return "", fmt.Errorf("yzj-cli package version %s != requested %s", meta.Version, version)
			}
			// update.json 与 exe 来自同一 CDN 包，不能拿它“自证”校验和；
			// 只信任编译进 knownChecksums 的 digest。
			if meta.Checksum != "" && expected != "" && !strings.EqualFold(meta.Checksum, expected) {
				return "", fmt.Errorf("yzj-cli package checksum metadata does not match pinned digest")
			}
		}
	}
	if expected == "" {
		return "", fmt.Errorf("no trusted checksum for yzj-cli %s; refusing to install", version)
	}
	if !strings.EqualFold(digest, expected) {
		return "", fmt.Errorf("yzj-cli checksum mismatch: got %s want %s", digest, expected)
	}
	dir := ManagedDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := managedBinPath(version)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, exeBytes, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dst, nil
}

func readZipFile(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("zip entry %s too large", f.Name)
	}
	return b, nil
}

// Status is the discovery/health projection exposed by the control API.
type Status struct {
	Found       bool      `json:"found"`
	Bin         string    `json:"bin,omitempty"`
	VersionCLI  string    `json:"version,omitempty"`
	VersionWant string    `json:"version_want,omitempty"`
	Profile     string    `json:"profile,omitempty"`
	ConfigDir   string    `json:"config_dir,omitempty"`
	LoggedIn    bool      `json:"logged_in"`
	Identity    *Identity `json:"identity,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// Detect resolves the binary and (optionally) verifies version and login.
func Detect(ctx context.Context, cfg Config) Status {
	st := Status{VersionWant: cfg.Version, Profile: cfg.Profile, ConfigDir: cfg.ConfigDir}
	// 状态探测是只读操作：只解析本地已有 bin，不在这里触发自动下载。
	// 真正启用轮询时由 runtime.setupPollers 调 EnsureBinary。
	bin, err := ResolveBinary(cfg)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Found = true
	st.Bin = bin
	client := NewClient(bin, cfg.Profile, cfg.ConfigDir, cfg.Timeout)
	if v, err := client.Version(ctx); err == nil {
		st.VersionCLI = v
	}
	id, err := client.WhoAmI(ctx)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.LoggedIn = true
	st.Identity = &id
	return st
}
