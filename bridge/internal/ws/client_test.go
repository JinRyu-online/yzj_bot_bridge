package ws

import "testing"

func TestAuthFailMessage(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
		msg  string
		fail bool
	}{
		{"auth failure with error", map[string]any{"cmd": "auth", "success": false, "error": "error.user.openToken.expire"}, "error.user.openToken.expire", true},
		{"auth failure no error field", map[string]any{"cmd": "auth", "success": false}, "", true},
		{"auth failure missing success", map[string]any{"cmd": "auth"}, "", true},
		{"auth success", map[string]any{"cmd": "auth", "success": true}, "", false},
		{"uppercase cmd", map[string]any{"cmd": "AUTH", "success": false, "error": "bad"}, "bad", true},
		{"non-auth control", map[string]any{"cmd": "ping"}, "", false},
		{"no cmd", map[string]any{"type": "msgchg"}, "", false},
		{"nil raw", nil, "", false},
	}
	for _, tc := range cases {
		msg, fail := authFailMessage(tc.raw)
		if fail != tc.fail || msg != tc.msg {
			t.Errorf("%s: got (%q,%v) want (%q,%v)", tc.name, msg, fail, tc.msg, tc.fail)
		}
	}
}

func TestAuthOK(t *testing.T) {
	if !authOK(map[string]any{"cmd": "auth", "success": true}) {
		t.Error("authOK should be true for explicit auth success")
	}
	if authOK(map[string]any{"cmd": "auth", "success": false}) {
		t.Error("authOK should be false for auth failure")
	}
	if authOK(map[string]any{"cmd": "auth"}) {
		t.Error("authOK should be false when success is missing")
	}
	if authOK(map[string]any{"cmd": "ping"}) {
		t.Error("authOK should be false for non-auth message")
	}
}
