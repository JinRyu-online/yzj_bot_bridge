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
