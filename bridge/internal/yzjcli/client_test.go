package yzjcli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	stdout, stderr []byte
	code           int
	err            error
	gotArgs        []string
	gotEnv         []string
}

func (f *fakeRunner) Run(ctx context.Context, env []string, args ...string) ([]byte, []byte, int, error) {
	f.gotEnv = env
	f.gotArgs = args
	return f.stdout, f.stderr, f.code, f.err
}

func TestWhoAmIParsesEnvelopeAndProfile(t *testing.T) {
	r := &fakeRunner{stdout: []byte(`{"success":true,"identity":"user","data":{"openId":"o1","name":"n1","expiresAt":"2026-10-21T15:43:01+08:00"}}`)}
	c := NewClient("yzj-cli", "work", "", 0)
	c.Runner = r
	id, err := c.WhoAmI(context.Background())
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if id.OpenID != "o1" || id.Name != "n1" {
		t.Fatalf("unexpected identity: %+v", id)
	}
	want := []string{"--profile", "work", "whoami"}
	if strings.Join(r.gotArgs, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v want %v", r.gotArgs, want)
	}
}

func TestAuthErrorFromStderr(t *testing.T) {
	r := &fakeRunner{
		code:   3,
		stderr: []byte(`{"success":false,"error":{"type":"authentication","subtype":"credentials_missing","message":"no credentials found","hint":"run auth login"}}`),
	}
	c := NewClient("yzj-cli", "", "", 0)
	c.Runner = r
	_, err := c.GroupRecent(context.Background(), 5, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsAuthError(err) {
		t.Fatalf("expected auth error, got %v", err)
	}
	var ce *CLIError
	if !errors.As(err, &ce) || ce.Message != "no credentials found" || ce.ExitCode != 3 {
		t.Fatalf("bad CLIError: %+v", ce)
	}
}

func TestRateLimitError(t *testing.T) {
	r := &fakeRunner{code: 4, stderr: []byte(`{"success":false,"error":{"message":"too many"}}`)}
	c := NewClient("x", "", "", 0)
	c.Runner = r
	_, err := c.MessageList(context.Background(), "g", "newest", "", 10)
	if !IsRateLimited(err) {
		t.Fatalf("expected rate limit, got %v", err)
	}
}

func TestMessageListClampsLimitAndDecodes(t *testing.T) {
	body := `{"success":true,"data":{"count":2,"groupId":"g1","more":true,"list":[
		{"msgId":"m1","fromOpenId":"u1","msgType":"text","content":"hi","sendTime":"2026-10-21 11:00:00.000","param":{"desc":[]}},
		{"msgId":"m2","fromOpenId":"u2","msgType":"file","content":"[图片]","sendTime":"2026-10-21 11:01:00.000","param":{"file_id":"f2","ext":"png","ftype":1}}
	]}}`
	r := &fakeRunner{stdout: []byte(body)}
	c := NewClient("yzj-cli", "", "", 0)
	c.Runner = r
	page, err := c.MessageList(context.Background(), "g1", "newest", "", 999)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.List) != 2 || page.List[1].MsgID != "m2" {
		t.Fatalf("bad page: %+v", page)
	}
	args := strings.Join(r.gotArgs, " ")
	if !strings.Contains(args, "--limit 20") {
		t.Fatalf("limit not clamped: %s", args)
	}
}

func TestFileDownloadUsesPlainTextOutput(t *testing.T) {
	r := &fakeRunner{stdout: []byte("downloaded 10 bytes to /tmp/x.png")}
	c := NewClient("yzj-cli", "p", "", 0)
	c.Runner = r
	if err := c.FileDownload(context.Background(), "f1", "/tmp/x.png"); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(r.gotArgs, " ")
	for _, want := range []string{"file download", "--id f1", "--output /tmp/x.png", "--overwrite", "--profile p"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
}

func TestInvalidJSONEnvelope(t *testing.T) {
	r := &fakeRunner{stdout: []byte("not json")}
	c := NewClient("x", "", "", 0)
	c.Runner = r
	if _, err := c.GroupRecent(context.Background(), 5, 1); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestMessagePageIgnoresEmptyMsgIDs(t *testing.T) {
	body := `{"success":true,"data":{"list":[{"msgId":"","content":"x"},{"msgId":"ok","content":"y"}]}}`
	r := &fakeRunner{stdout: []byte(body)}
	c := NewClient("x", "", "", 0)
	c.Runner = r
	page, err := c.MessageList(context.Background(), "g", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.List) != 1 || page.List[0].MsgID != "ok" {
		t.Fatalf("unexpected list: %s", mustJSON(page.List))
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestIsRateLimitedPlatformThrottle(t *testing.T) {
	err := &CLIError{ExitCode: 1, Type: "api_error", Message: "API error: code=10000429, 请求过于频繁"}
	if !IsRateLimited(err) {
		t.Fatal("api throttle must be rate limited")
	}
	if IsRateLimited(&CLIError{ExitCode: 1, Type: "api_error", Message: "invalid group id"}) {
		t.Fatal("plain api error must not be rate limited")
	}
}
