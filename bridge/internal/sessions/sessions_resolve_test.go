package sessions

import (
	"testing"

	"yzj-bridge/internal/bot"
)

func TestResolveSessionKeyGUIBypassesShared(t *testing.T) {
	cfg := bot.Config{SessionMode: "shared", SharedSessionKey: "__shared__"}
	gui := GUIChatOpenID("0a7c0510-293e-4818-bb10-d3000987aa00")

	key, ok := ResolveSessionKey(cfg, gui)
	if !ok || key != gui {
		t.Fatalf("gui key=%q ok=%v want %q true", key, ok, gui)
	}

	key, ok = ResolveSessionKey(cfg, "67bfd544e4b0cc79e38a4272")
	if !ok || key != "__shared__" {
		t.Fatalf("im key=%q ok=%v want __shared__ true", key, ok)
	}
}

func TestResolveSessionKeyGUIPerSessionEvenOneshot(t *testing.T) {
	cfg := bot.Config{SessionMode: "oneshot"}
	gui := GUIChatOpenID("sess-1")

	key, ok := ResolveSessionKey(cfg, gui)
	if !ok || key != gui {
		t.Fatalf("gui oneshot key=%q ok=%v want %q true", key, ok, gui)
	}

	key, ok = ResolveSessionKey(cfg, "user-a")
	if ok || key != "" {
		t.Fatalf("im oneshot key=%q ok=%v want empty false", key, ok)
	}
}

func TestResolveSessionKeyGUIPerUserUnchanged(t *testing.T) {
	cfg := bot.Config{SessionMode: "per_user"}
	gui := GUIChatOpenID("sess-2")

	key, ok := ResolveSessionKey(cfg, gui)
	if !ok || key != gui {
		t.Fatalf("gui key=%q ok=%v", key, ok)
	}
}

func TestResolveSessionKeyGUIStoreEntriesIndependent(t *testing.T) {
	st, err := Open(t.TempDir() + "/s.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg := bot.Config{ID: "border_bot", SessionMode: "shared", SharedSessionKey: "__shared__"}

	guiA := GUIChatOpenID("sess-a")
	guiB := GUIChatOpenID("sess-b")
	keyA, _ := ResolveSessionKey(cfg, guiA)
	keyB, _ := ResolveSessionKey(cfg, guiB)
	imKey, _ := ResolveSessionKey(cfg, "67bfd544e4b0cc79e38a4272")

	st.SetChatID(cfg.ID, keyA, "cursor-chat-a", "GUI")
	st.SetChatID(cfg.ID, keyB, "cursor-chat-b", "GUI")
	st.SetChatID(cfg.ID, imKey, "cursor-shared", "彭小星")

	gotA := st.GetEntry(cfg.ID, keyA)
	gotB := st.GetEntry(cfg.ID, keyB)
	gotIM := st.GetEntry(cfg.ID, imKey)
	if gotA.Current != "cursor-chat-a" || gotB.Current != "cursor-chat-b" || gotIM.Current != "cursor-shared" {
		t.Fatalf("A=%+v B=%+v IM=%+v", gotA, gotB, gotIM)
	}
	if keyA == keyB || keyA == imKey || keyB == imKey {
		t.Fatalf("keys must differ: A=%q B=%q IM=%q", keyA, keyB, imKey)
	}
}

func TestIsGUIChatOpenID(t *testing.T) {
	if !IsGUIChatOpenID(GUIChatOpenID("abc")) {
		t.Fatal("expected gui open id")
	}
	if IsGUIChatOpenID(GUIChatOpenIDPrefix) {
		t.Fatal("prefix alone is not a gui session")
	}
	if IsGUIChatOpenID("67bfd544e4b0cc79e38a4272") {
		t.Fatal("yunzhijia open id is not gui")
	}
}
