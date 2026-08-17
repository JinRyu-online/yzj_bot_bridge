package jobs

import "testing"

func TestUserQueueMergesSameOpenIDAndAllowsOtherUsers(t *testing.T) {
	m := New()
	sA := Scope("bot", "a", false)
	sB := Scope("bot", "b", false)
	if r := m.TryAccept(sA, "a", "A", "q1", "p1"); r.Status != StatusAccepted {
		t.Fatalf("%+v", r)
	}
	if r := m.TryAccept(sA, "a", "A", "q2", "p2"); r.Status != StatusMerged {
		t.Fatalf("%+v", r)
	}
	if r := m.TryAccept(sB, "b", "B", "qb", "pb"); r.Status != StatusAccepted {
		t.Fatalf("other user should run in parallel: %+v", r)
	}
	if got := m.DrainExtra(sA, "a"); got != "q2" {
		t.Fatalf("extra=%q", got)
	}
	next, notices := m.Finish(sA, "a")
	if next != nil || len(notices) != 0 {
		t.Fatalf("user queue should not carry others: next=%v notices=%v", next, notices)
	}
}

func TestChannelQueuePositionsAndFIFO(t *testing.T) {
	m := New()
	s := Scope("bot", "a", true)
	if r := m.TryAccept(s, "a", "甲", "问A", 1); r.Status != StatusAccepted {
		t.Fatalf("%+v", r)
	}
	b := m.TryAccept(s, "b", "乙", "问B", 2)
	if b.Status != StatusQueued || b.Position != 1 {
		t.Fatalf("b=%+v", b)
	}
	c := m.TryAccept(s, "c", "丙", "问C", 3)
	if c.Status != StatusQueued || c.Position != 2 {
		t.Fatalf("c=%+v", c)
	}
	next, notices := m.Finish(s, "a")
	if next == nil || next.OpenID != "b" || next.Content != "问B" {
		t.Fatalf("next=%+v", next)
	}
	if len(notices) != 1 || notices[0].OpenID != "c" || notices[0].Position != 1 {
		t.Fatalf("notices=%+v", notices)
	}
	next2, notices2 := m.Finish(s, "b")
	if next2 == nil || next2.OpenID != "c" || len(notices2) != 0 {
		t.Fatalf("next2=%+v notices2=%+v", next2, notices2)
	}
	next3, notices3 := m.Finish(s, "c")
	if next3 != nil || len(notices3) != 0 {
		t.Fatalf("expected idle, next3=%v", next3)
	}
}

func TestChannelQueueMergesRunningUserAndQueuedUser(t *testing.T) {
	m := New()
	s := Scope("bot", "a", true)
	_ = m.TryAccept(s, "a", "甲", "问A", 1)
	if r := m.TryAccept(s, "a", "甲", "补充A", 11); r.Status != StatusMerged {
		t.Fatalf("%+v", r)
	}
	if got := m.DrainExtra(s, "a"); got != "补充A" {
		t.Fatalf("extra=%q", got)
	}
	q := m.TryAccept(s, "b", "乙", "问B1", 2)
	if q.Status != StatusQueued || q.Position != 1 {
		t.Fatalf("%+v", q)
	}
	q2 := m.TryAccept(s, "b", "乙", "问B2", 22)
	if q2.Status != StatusQueued || q2.Position != 1 || !q2.Updated {
		t.Fatalf("%+v", q2)
	}
	next, _ := m.Finish(s, "a")
	if next == nil || next.Content != "问B1\n问B2" || next.Payload != 22 {
		t.Fatalf("merged queue item %+v", next)
	}
}

func TestSnapshotListsCurrentAndQueue(t *testing.T) {
	m := New()
	s := Scope("bot", "a", true)
	if got := m.Snapshot(s); got.Current.OpenID != "" {
		t.Fatalf("idle snapshot %+v", got)
	}
	_ = m.TryAccept(s, "a", "甲", "问A", 1)
	_ = m.TryAccept(s, "a", "甲", "补充A", 11)
	_ = m.TryAccept(s, "b", "乙", "问B", 2)
	_ = m.TryAccept(s, "c", "丙", "问C", 3)
	got := m.Snapshot(s)
	if got.Current.OpenID != "a" || got.Current.Name != "甲" || got.Current.Content != "问A" {
		t.Fatalf("current %+v", got.Current)
	}
	if got.Extra != "补充A" {
		t.Fatalf("extra=%q", got.Extra)
	}
	if len(got.Queue) != 2 || got.Queue[0].OpenID != "b" || got.Queue[1].Content != "问C" {
		t.Fatalf("queue %+v", got.Queue)
	}
	got.Queue[0].Content = "mutated"
	if m.Snapshot(s).Queue[0].Content != "问B" {
		t.Fatal("snapshot queue should be a copy")
	}
}

func TestCancelAbortsRunningContext(t *testing.T) {
	m := New()
	s := Scope("bot", "a", true)
	if r := m.TryAccept(s, "a", "A", "q", nil); r.Status != StatusAccepted {
		t.Fatalf("%+v", r)
	}
	ctx := m.Context(s)
	if !m.Busy(s) {
		t.Fatal("expected busy")
	}
	if !m.Cancel(s) {
		t.Fatal("first cancel should succeed")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("context was not cancelled")
	}
	if m.Cancel(s) {
		t.Fatal("second cancel should be a no-op")
	}
	if !m.Busy(s) {
		t.Fatal("cancel must not clear the running slot")
	}
	next, _ := m.Finish(s, "a")
	if next != nil {
		t.Fatalf("expected idle after finish, next=%+v", next)
	}
}

func TestUseChannelQueue(t *testing.T) {
	if !UseChannelQueue("shared", "") {
		t.Fatal("shared should default to channel queue")
	}
	if UseChannelQueue("per_user", "") {
		t.Fatal("per_user should default to user queue")
	}
	if !UseChannelQueue("per_user", "channel") {
		t.Fatal("explicit channel")
	}
	if UseChannelQueue("shared", "user") {
		t.Fatal("explicit user overrides shared")
	}
}
