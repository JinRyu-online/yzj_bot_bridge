package yzjout

import (
	"strings"
	"testing"

	"yzj-bridge/internal/jobs"
)

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
