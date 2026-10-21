package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/yzjcli"
)

func TestPollTriggerMatrix(t *testing.T) {
	textMention := yzjcli.Message{
		MsgID: "m1", MsgType: "text", Content: "@Fairy 查日志",
		Param: yzjcli.MessageParam{Desc: []yzjcli.Segment{{Type: "at", Data: "BOT-abc"}}},
	}
	imageQuote := yzjcli.Message{
		MsgID: "m2", MsgType: "file", Content: "[图片]",
		Param: yzjcli.MessageParam{
			FileID: "f1", Ext: "png", FType: flexIntPtr(1),
			ReplyOpenID: "BOT-abc", ReplyPersonName: "Fairy",
		},
	}
	imagePlain := yzjcli.Message{
		MsgID: "m3", MsgType: "file", Content: "[图片]",
		Param: yzjcli.MessageParam{FileID: "f1", Ext: "png", FType: flexIntPtr(1)},
	}
	imageOtherBot := yzjcli.Message{
		MsgID: "m4", MsgType: "file", Content: "[图片]",
		Param: yzjcli.MessageParam{
			FileID: "f1", Ext: "png", FType: flexIntPtr(1),
			ReplyOpenID: "BOT-other", ReplyPersonName: "别的机器人",
		},
	}
	target := pollTarget{BotID: "fairy", Aliases: []string{"Fairy"}, ImageTrigger: "quote_bot"}

	cases := []struct {
		name string
		t    pollTarget
		msg  yzjcli.Message
		want bool
	}{
		{"text mention", target, textMention, true},
		{"image quotes bot", target, imageQuote, true},
		{"image plain quote_bot", target, imagePlain, false},
		{"image quotes other bot", target, imageOtherBot, false},
		{"image any", pollTarget{Aliases: []string{"Fairy"}, ImageTrigger: "any"}, imagePlain, true},
		{"image off", pollTarget{Aliases: []string{"Fairy"}, ImageTrigger: "off"}, imageQuote, false},
		{"direct text", pollTarget{Trigger: "direct"}, yzjcli.Message{MsgID: "m5", FromOpenID: "u1", MsgType: "text", Content: "直接对话"}, true},
		{"direct image", pollTarget{Trigger: "direct"}, imagePlain, true},
		{"direct bot message", pollTarget{Trigger: "direct"}, yzjcli.Message{MsgID: "m6", FromOpenID: "BOT-6a7294", MsgType: "text", Content: "@你 正在处理中"}, false},
		{"any group text", pollTarget{Trigger: "any"}, yzjcli.Message{MsgID: "m7", FromOpenID: "u1", MsgType: "text", Content: "普通群消息"}, true},
	}
	for _, c := range cases {
		if got := pollTrigger(c.t, c.msg); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestConvertAttachments(t *testing.T) {
	got := convertAttachments([]yzjcli.Attachment{{
		Kind: "image", FileID: "f1", Name: "a.png", Ext: ".png", Size: 12, Width: 3, Height: 4,
	}})
	if len(got) != 1 || got[0].FileID != "f1" || got[0].Size != 12 || got[0].Width != 3 {
		t.Fatalf("unexpected: %+v", got)
	}
	if convertAttachments(nil) != nil {
		t.Fatal("nil should stay nil")
	}
}

func flexIntPtr(n int) *yzjcli.FlexInt { v := yzjcli.FlexInt(n); return &v }

func TestCleanupIncomingMedia(t *testing.T) {
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "incoming_media", "20200101", "old.png")
	newFile := filepath.Join(dir, "incoming_media", "20261021", "new.png")
	for _, p := range []string{oldFile, newFile} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	_ = os.Chtimes(oldFile, old, old)
	r := &Runtime{Defaults: map[string]any{"yzj_cli": map[string]any{
		"image": map[string]any{"keep_days": 7},
	}}}
	r.cleanupIncomingMedia([]*bot.Bot{{Config: bot.Config{Workspace: dir}}})
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("old file should be removed: %v", err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("new file should stay: %v", err)
	}
}
func TestSamePollGroupLabel(t *testing.T) {
	if !samePollGroupLabel("寄件助手群", "寄件助手") {
		t.Fatal("群后缀应视为同一群")
	}
	if !samePollGroupLabel("同城告警群", "同城告警") {
		t.Fatal("群后缀应视为同一群")
	}
	if samePollGroupLabel("同城告警群", "寄件助手") {
		t.Fatal("不同群不应匹配")
	}
	if samePollGroupLabel("", "寄件助手") {
		t.Fatal("空标签不应匹配")
	}
}
