package yzjout

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yzj-bridge/internal/jobs"
)

func TestSendTextPayloadMarkdown(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	content := "## 标题\n**粗体**"
	if err := SendText(srv.URL, content, "oid-1", nil); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if got["msgtype"] != float64(2) {
		t.Fatalf("msgtype=%v want 2", got["msgtype"])
	}
	if got["content"] != content {
		t.Fatalf("content=%q", got["content"])
	}
	param, ok := got["param"].(map[string]any)
	if !ok {
		t.Fatalf("param missing: %s", mustJSON(got))
	}
	if param["formatType"] != "markdown" {
		t.Fatalf("param.formatType=%v want markdown", param["formatType"])
	}
	if _, dup := param["replyMsgId"]; dup {
		t.Fatalf("plain send must not carry reply fields: %s", mustJSON(got))
	}
	if n, ok := got["notifyParams"]; !ok || n == nil {
		t.Fatalf("notifyParams missing for openID: %s", mustJSON(got))
	}
	if _, dup := got["paramType"]; dup {
		t.Fatalf("plain send must not set paramType: %s", mustJSON(got))
	}
}

func TestSendTextPayloadReplyMergesMarkdown(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	reply := &ReplyMeta{MsgID: "m-42", Summary: "原消息", PersonName: "张三"}
	if err := SendText(srv.URL, "**回复内容**", "", reply); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if got["paramType"] != float64(3) {
		t.Fatalf("paramType=%v want 3", got["paramType"])
	}
	param, ok := got["param"].(map[string]any)
	if !ok {
		t.Fatalf("param missing: %s", mustJSON(got))
	}
	if param["formatType"] != "markdown" {
		t.Fatalf("param.formatType=%v want markdown (merged into reply param)", param["formatType"])
	}
	if param["replyMsgId"] != "m-42" {
		t.Fatalf("replyMsgId=%v want m-42", param["replyMsgId"])
	}
	if param["replyPersonName"] != "张三" {
		t.Fatalf("replyPersonName=%v", param["replyPersonName"])
	}
}

func TestSendTextEmptyContentFallback(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := SendText(srv.URL, "   ", "", nil); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if got["content"] != "(空回复)" {
		t.Fatalf("content=%q want (空回复)", got["content"])
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestFormatCompletionReply(t *testing.T) {
	got := FormatCompletionReply("hello", "oid", "Bob", true)
	if got != "任务已完成\n\nhello" {
		t.Fatalf("%q", got)
	}
	got = FormatCompletionReply("hello", "", "Bob", true)
	if got != "@Bob 任务已完成\n\nhello" {
		t.Fatalf("%q", got)
	}
}

func TestFormatStopAck(t *testing.T) {
	if got := FormatStopAck(true); got != "已中断当前任务" {
		t.Fatalf("%q", got)
	}
	if got := FormatStopAck(false); got != "当前没有正在执行的任务" {
		t.Fatalf("%q", got)
	}
}

func TestFormatInterruptReply(t *testing.T) {
	got := FormatInterruptReply("任务已中断", "oid", "Bob", true)
	if got != "任务已中断" {
		t.Fatalf("%q", got)
	}
	got = FormatInterruptReply("", "", "Bob", true)
	if got != "@Bob 任务已中断" {
		t.Fatalf("%q", got)
	}
}

func TestFormatJobList(t *testing.T) {
	if got := FormatJobList(jobs.Snapshot{}); got != "当前没有正在执行或排队的任务" {
		t.Fatalf("%q", got)
	}
	got := FormatJobList(jobs.Snapshot{
		Current: jobs.Item{OpenID: "a", Name: "甲", Content: "问A"},
		Extra:   "补充A",
		Queue: []jobs.Item{
			{OpenID: "b", Name: "乙", Content: "问B"},
			{OpenID: "c", Name: "", Content: "问C"},
		},
	})
	for _, want := range []string{"执行中：甲", "内容：问A", "补充：补充A", "待执行（2）", "1. 乙：问B", "2. c：问C"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	idleQueue := FormatJobList(jobs.Snapshot{
		Current: jobs.Item{OpenID: "a", Name: "甲", Content: "问A"},
	})
	if !strings.Contains(idleQueue, "待执行：无") {
		t.Fatalf("%q", idleQueue)
	}
}

func TestFormatQueuePosition(t *testing.T) {
	got := FormatQueuePosition(2)
	if got != "正在处理之前的问题，你的问题排在第2位" {
		t.Fatalf("%q", got)
	}
	if FormatQueuePosition(0) != "正在处理之前的问题，你的问题排在第1位" {
		t.Fatal("invalid position should clamp to 1")
	}
}
