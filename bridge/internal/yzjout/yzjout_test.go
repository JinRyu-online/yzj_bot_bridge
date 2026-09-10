package yzjout

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestSplitMessageShortContentSingleChunk(t *testing.T) {
	got := SplitMessage("短消息", chunkLimit)
	if len(got) != 1 || got[0] != "短消息" {
		t.Fatalf("%v", got)
	}
}

func TestSplitMessageParagraphBoundaries(t *testing.T) {
	para := strings.Repeat("段", 1450)
	content := strings.Join([]string{para, para, para}, "\n\n") // 3 * 1450 + separators
	got := SplitMessage(content, chunkLimit)
	if len(got) != 3 {
		t.Fatalf("want 3 chunks, got %d", len(got))
	}
	for i, c := range got {
		if n := utf8.RuneCountInString(c); n > chunkLimit {
			t.Fatalf("chunk %d len %d > %d", i, n, chunkLimit)
		}
	}
	// Reassembly loses only the blank-line separators.
	rejoined := strings.Join(got, "\n\n")
	if utf8.RuneCountInString(rejoined) < utf8.RuneCountInString(content)-10 {
		t.Fatalf("content lost across chunks: %d < %d", utf8.RuneCountInString(rejoined), utf8.RuneCountInString(content))
	}
}

func TestSplitMessageHardCutNoBoundaries(t *testing.T) {
	content := strings.Repeat("测", 7000) // no \n, no paragraphs
	got := SplitMessage(content, chunkLimit)
	if len(got) < 3 {
		t.Fatalf("want >=3 chunks, got %d", len(got))
	}
	total := 0
	for i, c := range got {
		n := utf8.RuneCountInString(c)
		if n > chunkLimit {
			t.Fatalf("chunk %d len %d > %d", i, n, chunkLimit)
		}
		total += n
	}
	if total != 7000 {
		t.Fatalf("total=%d want 7000", total)
	}
}

func TestSplitMessageNeverExceedsMaxChunks(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 300; i++ { // 300 paragraphs -> naive split would be >9 chunks
		b.WriteString(strings.Repeat("p", 100))
		b.WriteString("\n\n")
	}
	got := SplitMessage(b.String(), chunkLimit)
	if len(got) > maxChunks {
		t.Fatalf("chunks=%d > max=%d", len(got), maxChunks)
	}
	for i, c := range got {
		if n := utf8.RuneCountInString(c); n > chunkLimit {
			t.Fatalf("chunk %d len %d > %d", i, n, chunkLimit)
		}
	}
}

func TestSendTextPaginatesLongContent(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if c, ok := m["content"].(string); ok {
			bodies = append(bodies, c)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	content := strings.Repeat("x", 5000)
	if err := SendText(srv.URL, content, "", nil); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(bodies) < 2 {
		t.Fatalf("expected paged sends, got %d", len(bodies))
	}
	for i, b := range bodies {
		if !strings.HasSuffix(b, fmt.Sprintf("(%d/%d)", i+1, len(bodies))) {
			t.Fatalf("chunk %d missing page marker: %q", i, b[max(0, len(b)-40):])
		}
	}
	// Reassembled content must cover the original (minus separators/markers).
	joined := ""
	for _, b := range bodies {
		b = strings.TrimSpace(b)
		b = b[:strings.LastIndex(b, "\n(")]
		joined += b
	}
	if utf8.RuneCountInString(joined) < 4000 {
		t.Fatalf("content lost: got %d runes", utf8.RuneCountInString(joined))
	}
}

func TestSendTextBizErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":null,"error":"消息内容太长","errorCode":1401002,"success":false}`))
	}))
	defer srv.Close()
	err := SendText(srv.URL, "hello", "", nil)
	if err == nil || !strings.Contains(err.Error(), "消息内容太长") || !strings.Contains(err.Error(), "1401002") {
		t.Fatalf("want biz error, got %v", err)
	}
}

func TestSendTextSuccessBodyNotBizError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"msgId":"m1"},"error":null,"errorCode":0,"success":true}`))
	}))
	defer srv.Close()
	if err := SendText(srv.URL, "hello", "", nil); err != nil {
		t.Fatalf("SendText: %v", err)
	}
}
