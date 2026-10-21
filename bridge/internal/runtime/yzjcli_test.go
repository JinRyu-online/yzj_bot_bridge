package runtime

import (
	"testing"

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
			FileID: "f1", Ext: "png", FType: intPtr(1),
			ReplyOpenID: "BOT-abc", ReplyPersonName: "Fairy",
		},
	}
	imagePlain := yzjcli.Message{
		MsgID: "m3", MsgType: "file", Content: "[图片]",
		Param: yzjcli.MessageParam{FileID: "f1", Ext: "png", FType: intPtr(1)},
	}
	imageOtherBot := yzjcli.Message{
		MsgID: "m4", MsgType: "file", Content: "[图片]",
		Param: yzjcli.MessageParam{
			FileID: "f1", Ext: "png", FType: intPtr(1),
			ReplyOpenID: "BOT-other", ReplyPersonName: "别的机器人",
		},
	}
	target := pollTarget{BotID: "fairy", Aliases: []string{"Fairy"}, ImageTrigger: "quote_bot"}

	cases := []struct {
		name  string
		t     pollTarget
		msg   yzjcli.Message
		want  bool
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

func intPtr(n int) *int { return &n }
