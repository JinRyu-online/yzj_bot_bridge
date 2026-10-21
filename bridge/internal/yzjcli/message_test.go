package yzjcli

import (
	"testing"
	"time"
)

func TestMessageAttachmentsFileImage(t *testing.T) {
	msg := Message{
		MsgType: "file",
		Param: MessageParam{
			FileID: "f1", Ext: "png", Name: "图片.png", FType: flexIntPtr(1),
			Size: []byte(`"108992"`), PicWidth: 720, PicHeight: 1005,
		},
	}
	att := msg.Attachments()
	if len(att) != 1 {
		t.Fatalf("attachments = %d want 1", len(att))
	}
	a := att[0]
	if a.Kind != "image" || a.FileID != "f1" || a.Size != 108992 || a.Width != 720 {
		t.Fatalf("unexpected attachment: %+v", a)
	}
	if !msg.HasImages() {
		t.Fatal("expected images")
	}
}

func TestMessageAttachmentsFileNonImageIgnored(t *testing.T) {
	msg := Message{MsgType: "file", Param: MessageParam{FileID: "f9", Ext: "pdf", FType: flexIntPtr(2)}}
	if got := msg.Attachments(); len(got) != 0 {
		t.Fatalf("unexpected attachments: %+v", got)
	}
}

func TestMessageAttachmentsRichTextMultiple(t *testing.T) {
	msg := Message{
		MsgType: "richText", Content: "[图片][图片]",
		Param: MessageParam{Desc: []Segment{
			{Type: "image", Data: "f1", W: 10, H: 20},
			{Type: "at", Data: "u1"},
			{Type: "image", Data: "f2"},
			{Type: "image", Data: ""},
		}},
	}
	att := msg.Attachments()
	if len(att) != 2 || att[0].FileID != "f1" || att[1].FileID != "f2" {
		t.Fatalf("unexpected attachments: %+v", att)
	}
}

func TestMentionsBotsRobotSegment(t *testing.T) {
	msg := Message{
		Content: "@游泳管家 测试",
		Param:   MessageParam{Desc: []Segment{{Type: "at", Data: "BOT-6ad82e8d"}}},
	}
	if !msg.MentionsBots([]string{"游泳管家"}, nil) {
		t.Fatal("expected robot mention")
	}
	if msg.MentionsBots([]string{"游泳管家"}, []string{"BOT-other"}) {
		t.Fatal("configured open id must match")
	}
	if msg.MentionsBots([]string{"别的机器人"}, []string{"BOT-other"}) {
		t.Fatal("unexpected mention")
	}
}

func TestMentionsBotsContentFallback(t *testing.T) {
	msg := Message{Content: "@Fairy 帮我查日志"}
	if !msg.MentionsBots([]string{"Fairy"}, nil) {
		t.Fatal("expected content fallback mention")
	}
	if msg.MentionsBots([]string{"Other"}, nil) {
		t.Fatal("unexpected mention")
	}
}

func TestMentionsBotsIgnoresAtAll(t *testing.T) {
	msg := Message{Content: "@ALL 通知", Param: MessageParam{Desc: []Segment{{Type: "at", Data: "all"}}}}
	if msg.MentionsBots([]string{"Fairy"}, nil) {
		t.Fatal("@all must not trigger a bot")
	}
}

func TestQuotesBot(t *testing.T) {
	msg := Message{
		MsgType: "file", Content: "[图片]", Param: MessageParam{
			FileID: "f1", Ext: "jpg", FType: flexIntPtr(1),
			ReplyOpenID: "BOT-6a953c", ReplyPersonName: "同城日志查询",
		},
	}
	if !msg.QuotesBot([]string{"同城日志查询"}, nil) {
		t.Fatal("expected quote match")
	}
	if msg.QuotesBot([]string{"Fairy"}, nil) {
		t.Fatal("different bot name must not match")
	}
	if msg.QuotesBot([]string{"同城日志查询"}, []string{"BOT-other"}) {
		t.Fatal("open id mismatch must not match")
	}
}

func TestQuotesBotRequiresRobot(t *testing.T) {
	msg := Message{Param: MessageParam{ReplyOpenID: "67bfd544e4b0cc79e38a4272", ReplyPersonName: "张三"}}
	if msg.QuotesBot([]string{"Fairy"}, nil) {
		t.Fatal("human quote must not match")
	}
}

func TestMessageRecall(t *testing.T) {
	msg := Message{MsgType: "other", Param: MessageParam{SysType: "withdrawMsg"}}
	if !msg.IsRecall() {
		t.Fatal("expected recall")
	}
	if (Message{MsgType: "text"}).IsRecall() {
		t.Fatal("text is not recall")
	}
}

func TestSendTimeValue(t *testing.T) {
	msg := Message{SendTime: "2026-10-21 11:59:13.446"}
	got := msg.SendTimeValue()
	if got.IsZero() || got.Year() != 2026 || got.Second() != 13 {
		t.Fatalf("bad time: %v", got)
	}
	if !(Message{SendTime: "bad"}).SendTimeValue().IsZero() {
		t.Fatal("expected zero time")
	}
}

func TestFileIDsFromText(t *testing.T) {
	got := FileIDsFromText("看 https://x/docrest/doc/user/downloadfile?fileId=6ad72fe7fc8fc40001e8b887 和 https://x/microblog/filesvr/6ad83891831f080001c5571a")
	if len(got) != 2 {
		t.Fatalf("ids = %v", got)
	}
	if !(time.Duration(0) == time.Duration(0)) {
		t.Fatal("unreachable")
	}
}

func flexIntPtr(n int) *FlexInt { v := FlexInt(n); return &v }
func TestMentionsBotsSameGroupOnlyTarget(t *testing.T) {
	msg := Message{
		Content: "@寄件日志查询 查下此单",
		Param:   MessageParam{Desc: []Segment{{Type: "at", Data: "BOT-6a850de1e4b0e54b21614295"}}},
	}
	if !msg.MentionsBots([]string{"寄件日志查询"}, nil) {
		t.Fatal("target bot should match")
	}
	if msg.MentionsBots([]string{"同城日志查询"}, nil) {
		t.Fatal("same-group other bot must not match without open id")
	}
	if msg.MentionsBots([]string{"同城日志查询"}, []string{"BOT-other"}) {
		t.Fatal("other bot with its own open id must not match")
	}
}
