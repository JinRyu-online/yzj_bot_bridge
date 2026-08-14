package yzjout

import "testing"

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

func TestFormatQueuePosition(t *testing.T) {
	got := FormatQueuePosition(2)
	if got != "正在处理之前的问题，你的问题排在第2位" {
		t.Fatalf("%q", got)
	}
	if FormatQueuePosition(0) != "正在处理之前的问题，你的问题排在第1位" {
		t.Fatal("invalid position should clamp to 1")
	}
}
