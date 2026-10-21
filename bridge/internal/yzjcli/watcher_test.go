package yzjcli

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeLister struct {
	mu     sync.Mutex
	newest MessagePage
	old    map[string]MessagePage
	err    error
	oldErr error
	calls  []string
}

func (f *fakeLister) MessageList(ctx context.Context, groupID, typ, anchorMsgID string, limit int) (MessagePage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, typ+"|"+anchorMsgID)
	if typ == "old" && f.oldErr != nil {
		return MessagePage{}, f.oldErr
	}
	if f.err != nil {
		return MessagePage{}, f.err
	}
	if typ == "newest" {
		return f.newest, nil
	}
	if p, ok := f.old[anchorMsgID]; ok {
		return p, nil
	}
	return MessagePage{}, nil
}

func testMsg(id, sendTime string) Message {
	return Message{MsgID: id, FromOpenID: "u1", MsgType: "text", Content: id, SendTime: sendTime}
}

func newTestWatcher(t *testing.T, lister MessageLister) (*Watcher, *StateStore, *[]Message) {
	t.Helper()
	resetSharedPollCooldown()
	st, err := OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got []Message
	var mu sync.Mutex
	w := &Watcher{
		Client: lister, Store: st, Key: "default|g1", GroupID: "g1",
		Interval: time.Second, CatchupPages: 3,
		OnMessage: func(ctx context.Context, msg Message) {
			mu.Lock()
			got = append(got, msg)
			mu.Unlock()
		},
	}
	return w, st, &got
}

func TestWatcherBaselineDoesNotReplay(t *testing.T) {
	lister := &fakeLister{newest: MessagePage{List: []Message{
		testMsg("m1", "2026-10-21 11:00:00.000"),
		testMsg("m2", "2026-10-21 11:01:00.000"),
	}}}
	w, st, got := newTestWatcher(t, lister)
	w.pollOnce(context.Background())
	if len(*got) != 0 {
		t.Fatalf("baseline dispatched %d messages", len(*got))
	}
	s, _ := st.Get("default|g1")
	if s.LastMsgID != "m2" {
		t.Fatalf("cursor = %q want m2", s.LastMsgID)
	}
}

func TestWatcherDispatchesNewAscending(t *testing.T) {
	lister := &fakeLister{newest: MessagePage{List: []Message{
		testMsg("m1", "2026-10-21 11:00:00.000"),
		testMsg("m2", "2026-10-21 11:01:00.000"),
		testMsg("m3", "2026-10-21 11:02:00.000"),
	}}}
	w, st, got := newTestWatcher(t, lister)
	if err := st.Put("default|g1", GroupState{LastMsgID: "m1"}); err != nil {
		t.Fatal(err)
	}
	w.pollOnce(context.Background())
	if len(*got) != 2 || (*got)[0].MsgID != "m2" || (*got)[1].MsgID != "m3" {
		t.Fatalf("dispatched = %+v", *got)
	}
	s, _ := st.Get("default|g1")
	if s.LastMsgID != "m3" {
		t.Fatalf("cursor = %q", s.LastMsgID)
	}
	if !HasRecent(s.RecentMsgIDs, "m2") || !HasRecent(s.RecentMsgIDs, "m3") {
		t.Fatalf("recent = %v", s.RecentMsgIDs)
	}
}

func TestWatcherNoNewMessages(t *testing.T) {
	lister := &fakeLister{newest: MessagePage{List: []Message{
		testMsg("m1", "2026-10-21 11:00:00.000"),
		testMsg("m2", "2026-10-21 11:01:00.000"),
	}}}
	w, st, got := newTestWatcher(t, lister)
	if err := st.Put("default|g1", GroupState{LastMsgID: "m2"}); err != nil {
		t.Fatal(err)
	}
	w.pollOnce(context.Background())
	if len(*got) != 0 {
		t.Fatalf("unexpected dispatch: %+v", *got)
	}
}

func TestWatcherCatchUpUsesOlderPages(t *testing.T) {
	lister := &fakeLister{
		newest: MessagePage{List: []Message{
			testMsg("m2", "2026-10-21 11:02:00.000"),
			testMsg("m3", "2026-10-21 11:03:00.000"),
		}},
		old: map[string]MessagePage{
			"m2": {List: []Message{
				testMsg("m0", "2026-10-21 11:00:00.000"),
				testMsg("m1", "2026-10-21 11:01:00.000"),
			}},
		},
	}
	w, st, got := newTestWatcher(t, lister)
	if err := st.Put("default|g1", GroupState{LastMsgID: "m0"}); err != nil {
		t.Fatal(err)
	}
	w.pollOnce(context.Background())
	if len(*got) != 3 || (*got)[0].MsgID != "m1" || (*got)[2].MsgID != "m3" {
		t.Fatalf("dispatched = %+v", *got)
	}
}

func TestWatcherCursorLostSkipsWithoutReplay(t *testing.T) {
	lister := &fakeLister{newest: MessagePage{List: []Message{
		testMsg("m5", "2026-10-21 11:05:00.000"),
		testMsg("m6", "2026-10-21 11:06:00.000"),
	}}}
	w, st, got := newTestWatcher(t, lister)
	if err := st.Put("default|g1", GroupState{LastMsgID: "gone"}); err != nil {
		t.Fatal(err)
	}
	w.pollOnce(context.Background())
	if len(*got) != 0 {
		t.Fatalf("expected no replay, got %+v", *got)
	}
	s, _ := st.Get("default|g1")
	if s.LastMsgID != "m6" {
		t.Fatalf("cursor = %q", s.LastMsgID)
	}
}

func TestWatcherSkipsRecallAndRecentRing(t *testing.T) {
	recall := testMsg("m3", "2026-10-21 11:03:00.000")
	recall.MsgType = "other"
	recall.Param.SysType = "withdrawMsg"
	lister := &fakeLister{newest: MessagePage{List: []Message{
		testMsg("m1", "2026-10-21 11:01:00.000"),
		testMsg("m2", "2026-10-21 11:02:00.000"),
		recall,
	}}}
	w, st, got := newTestWatcher(t, lister)
	if err := st.Put("default|g1", GroupState{LastMsgID: "m1", RecentMsgIDs: []string{"m2"}}); err != nil {
		t.Fatal(err)
	}
	w.pollOnce(context.Background())
	if len(*got) != 0 {
		t.Fatalf("expected recall/recent filtered, got %+v", *got)
	}
}

func TestWatcherAuthErrorDisablesAfterThree(t *testing.T) {
	lister := &fakeLister{err: &CLIError{ExitCode: 3, Type: "authentication", Subtype: "credentials_missing", Message: "no credentials"}}
	w, _, _ := newTestWatcher(t, lister)
	w.mu.Lock()
	w.enabled = true
	w.mu.Unlock()
	for i := 0; i < 3; i++ {
		w.pollOnce(context.Background())
	}
	if w.Enabled() {
		t.Fatal("watcher should be disabled after 3 auth failures")
	}
}

func TestWatcherRateLimitBacksOff(t *testing.T) {
	lister := &fakeLister{err: &CLIError{ExitCode: 4, Message: "too many requests"}}
	w, _, _ := newTestWatcher(t, lister)
	w.mu.Lock()
	w.enabled = true
	w.mu.Unlock()
	w.pollOnce(context.Background())
	if b := w.currentBackoff(); b < 5*time.Second {
		t.Fatalf("backoff = %v", b)
	}
	resetSharedPollCooldown()
	w.pollOnce(context.Background())
	if b := w.currentBackoff(); b < 10*time.Second {
		t.Fatalf("backoff not growing: %v", b)
	}
}

func TestWatcherGenericErrorRetried(t *testing.T) {
	lister := &fakeLister{err: errors.New("boom")}
	w, _, _ := newTestWatcher(t, lister)
	w.mu.Lock()
	w.enabled = true
	w.mu.Unlock()
	w.pollOnce(context.Background())
	if !w.Enabled() {
		t.Fatal("generic error must not disable watcher")
	}
}
func TestWatcherCatchUpErrorKeepsCursor(t *testing.T) {
	lister := &fakeLister{
		newest: MessagePage{List: []Message{
			testMsg("m2", "2026-10-21 11:02:00.000"),
			testMsg("m3", "2026-10-21 11:03:00.000"),
		}},
		oldErr: errors.New("transient page error"),
	}
	w, st, got := newTestWatcher(t, lister)
	if err := st.Put("default|g1", GroupState{LastMsgID: "m0"}); err != nil {
		t.Fatal(err)
	}
	w.pollOnce(context.Background())
	if len(*got) != 0 {
		t.Fatalf("must not dispatch partial catch-up: %+v", *got)
	}
	s, _ := st.Get("default|g1")
	if s.LastMsgID != "m0" {
		t.Fatalf("cursor advanced on catch-up error: %q", s.LastMsgID)
	}
}
