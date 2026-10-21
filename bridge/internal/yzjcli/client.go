package yzjcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultTimeout bounds one CLI invocation (read commands are single HTTP
// calls; downloads get a larger timeout from the caller).
const DefaultTimeout = 20 * time.Second

// Runner executes the CLI binary. It exists so tests can inject fixtures.
type Runner interface {
	Run(ctx context.Context, env []string, args ...string) (stdout, stderr []byte, exitCode int, err error)
}

// ExecRunner runs the real yzj-cli binary.
type ExecRunner struct{ Bin string }

// Run executes yzj-cli with argv passed directly (no shell). On Windows the
// npm shim is a .cmd file, which must be started through cmd.exe.
func (r *ExecRunner) Run(ctx context.Context, env []string, args ...string) ([]byte, []byte, int, error) {
	bin := r.Bin
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" && isBatchFile(bin) {
		all := append([]string{"/c", bin}, args...)
		cmd = exec.CommandContext(ctx, "cmd.exe", all...)
	} else {
		cmd = exec.CommandContext(ctx, bin, args...)
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
			err = nil
		}
	}
	return stdout.Bytes(), stderr.Bytes(), code, err
}

func isBatchFile(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".cmd" || ext == ".bat"
}

// CLIError is a structured failure reported by yzj-cli on stderr.
type CLIError struct {
	ExitCode int    `json:"exitCode"`
	Type     string `json:"type"`
	Subtype  string `json:"subtype"`
	Message  string `json:"message"`
	Hint     string `json:"hint"`
}

func (e *CLIError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "yzj-cli failed"
	}
	out := fmt.Sprintf("yzj-cli: %s", msg)
	if e.Subtype != "" {
		out += " (" + e.Subtype + ")"
	}
	if e.ExitCode != 0 {
		out += fmt.Sprintf(" [exit %d]", e.ExitCode)
	}
	return out
}

// IsAuthError reports a missing/expired credential (user must re-login).
func IsAuthError(err error) bool {
	var e *CLIError
	if !errors.As(err, &e) {
		return false
	}
	if e.ExitCode == 3 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(e.Subtype)) {
	case "credentials_missing", "session_expired", "refresh_failed":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(e.Type)) {
	case "authentication":
		return true
	}
	return false
}

// IsRateLimited reports a retryable server-side throttle/transient failure.
func IsRateLimited(err error) bool {
	var e *CLIError
	if !errors.As(err, &e) {
		return false
	}
	return e.ExitCode == 4
}

// Client is a typed wrapper around `yzj-cli`.
type Client struct {
	Bin       string
	Profile   string
	ConfigDir string
	Timeout   time.Duration
	Runner    Runner
}

// NewClient builds a client for one profile.
func NewClient(bin, profile, configDir string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		Bin: strings.TrimSpace(bin), Profile: strings.TrimSpace(profile),
		ConfigDir: strings.TrimSpace(configDir), Timeout: timeout,
		Runner: &ExecRunner{Bin: bin},
	}
}

// Identity is the whoami payload.
type Identity struct {
	OpenID     string `json:"openId"`
	Name       string `json:"name"`
	ExpiresAt  string `json:"expiresAt"`
	TokenState string `json:"tokenStatus"`
}

// Group is one conversation from `im group recent` / `im group search`.
type Group struct {
	GroupID   string `json:"groupId"`
	GroupName string `json:"groupName"`
	GroupType int    `json:"groupType"`
}

// MessagePage is the parsed `im message list` data payload. Note the platform
// returns List in ascending sendTime order (oldest first).
type MessagePage struct {
	Count   int       `json:"count"`
	GroupID string    `json:"groupId"`
	More    bool      `json:"more"`
	List    []Message `json:"list"`
}

type envelope struct {
	Success  bool            `json:"success"`
	Identity string          `json:"identity"`
	Data     json.RawMessage `json:"data"`
	Error    *struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"error"`
}

func (c *Client) argv(rest ...string) []string {
	args := make([]string, 0, len(rest)+2)
	if c.Profile != "" {
		args = append(args, "--profile", c.Profile)
	}
	return append(args, rest...)
}

func (c *Client) env() []string {
	if c.ConfigDir == "" {
		return nil
	}
	return []string{"YZJ_CLI_CONFIG_DIR=" + c.ConfigDir}
}

// run executes a JSON-returning command and returns the `data` payload.
func (c *Client) run(ctx context.Context, args ...string) (json.RawMessage, error) {
	if strings.TrimSpace(c.Bin) == "" {
		return nil, errors.New("yzj-cli not found")
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	stdout, stderr, code, err := c.Runner.Run(ctx, c.env(), c.argv(args...)...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, parseError(code, stdout, stderr)
	}
	var env envelope
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &env); err != nil {
		return nil, fmt.Errorf("yzj-cli: invalid JSON output: %w", err)
	}
	if !env.Success {
		if env.Error != nil {
			return nil, &CLIError{ExitCode: code, Type: env.Error.Type, Subtype: env.Error.Subtype,
				Message: env.Error.Message, Hint: env.Error.Hint}
		}
		return nil, &CLIError{ExitCode: code, Message: "unsuccessful response"}
	}
	return env.Data, nil
}

func parseError(code int, stdout, stderr []byte) error {
	for _, raw := range [][]byte{stderr, stdout} {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			continue
		}
		var env envelope
		if err := json.Unmarshal(trimmed, &env); err == nil && env.Error != nil {
			return &CLIError{ExitCode: code, Type: env.Error.Type, Subtype: env.Error.Subtype,
				Message: env.Error.Message, Hint: env.Error.Hint}
		}
	}
	msg := strings.TrimSpace(string(stderr))
	if msg == "" {
		msg = strings.TrimSpace(string(stdout))
	}
	if msg == "" {
		msg = "unknown error"
	}
	// Keep a bounded single-line message; CLI help/clap text can be long.
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	return &CLIError{ExitCode: code, Message: msg}
}

// WhoAmI returns the logged-in user identity.
func (c *Client) WhoAmI(ctx context.Context) (Identity, error) {
	data, err := c.run(ctx, "whoami")
	if err != nil {
		return Identity{}, err
	}
	var id Identity
	err = json.Unmarshal(data, &id)
	return id, err
}

// GroupRecent lists the most recent conversations visible to this user.
func (c *Client) GroupRecent(ctx context.Context, limit, page int) ([]Group, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	data, err := c.run(ctx, "im", "group", "recent", "--limit", itoa(limit), "--page", itoa(page))
	if err != nil {
		return nil, err
	}
	return decodeGroups(data)
}

// GroupSearch finds conversations by name.
func (c *Client) GroupSearch(ctx context.Context, keyword string, limit, page int) ([]Group, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, errors.New("keyword required")
	}
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	data, err := c.run(ctx, "im", "group", "search", "--keyword", keyword, "--limit", itoa(limit), "--page", itoa(page))
	if err != nil {
		return nil, err
	}
	return decodeGroups(data)
}

func decodeGroups(data json.RawMessage) ([]Group, error) {
	var payload struct {
		List []Group `json:"list"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("yzj-cli: invalid group list: %w", err)
	}
	return payload.List, nil
}

// MessageList fetches chat history. typ is "newest" (anchor ignored),
// "old" or "new" (anchor required). limit is clamped to the platform max (20).
func (c *Client) MessageList(ctx context.Context, groupID, typ, anchorMsgID string, limit int) (MessagePage, error) {
	if strings.TrimSpace(groupID) == "" {
		return MessagePage{}, errors.New("group id required")
	}
	if typ == "" {
		typ = "newest"
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	args := []string{"im", "message", "list", "--group-id", groupID, "--type", typ, "--limit", itoa(limit)}
	if anchorMsgID != "" {
		args = append(args, "--msg-id", anchorMsgID)
	}
	data, err := c.run(ctx, args...)
	if err != nil {
		return MessagePage{}, err
	}
	var page MessagePage
	if err := json.Unmarshal(data, &page); err != nil {
		return MessagePage{}, fmt.Errorf("yzj-cli: invalid message page: %w", err)
	}
	// Defensive: keep platform order but drop entries without ids.
	kept := page.List[:0]
	for _, m := range page.List {
		if strings.TrimSpace(m.MsgID) != "" {
			kept = append(kept, m)
		}
	}
	page.List = kept
	return page, nil
}

// FileDownload downloads one attachment to an explicit output path. yzj-cli
// prints "downloaded N bytes to <path>" on stdout, not JSON.
func (c *Client) FileDownload(ctx context.Context, fileID, output string) error {
	if strings.TrimSpace(fileID) == "" {
		return errors.New("file id required")
	}
	if strings.TrimSpace(output) == "" {
		return errors.New("output path required")
	}
	timeout := c.Timeout
	if timeout < 3*time.Minute {
		timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout, stderr, code, err := c.Runner.Run(ctx, c.env(),
		c.argv("file", "download", "--id", fileID, "--output", output, "--overwrite")...)
	if err != nil {
		return err
	}
	if code != 0 {
		return parseError(code, stdout, stderr)
	}
	return nil
}

// Version returns the CLI version string from `yzj-cli --version`.
func (c *Client) Version(ctx context.Context) (string, error) {
	if strings.TrimSpace(c.Bin) == "" {
		return "", errors.New("yzj-cli not found")
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	stdout, stderr, code, err := c.Runner.Run(ctx, c.env(), "--version")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", parseError(code, stdout, stderr)
	}
	return strings.TrimSpace(string(stdout)), nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
