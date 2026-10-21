package yzjcli

import (
	"path/filepath"
	"testing"
)

func TestStatePutGetReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Get("p|g"); ok {
		t.Fatal("unexpected state")
	}
	if err := st.Put("p|g", GroupState{LastMsgID: "m1", RecentMsgIDs: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get("p|g")
	if !ok || got.LastMsgID != "m1" {
		t.Fatalf("reloaded state = %+v ok=%v", got, ok)
	}
	if !HasRecent(got.RecentMsgIDs, "m1") {
		t.Fatal("recent ring lost")
	}
}

func TestAppendRecentDedupAndBound(t *testing.T) {
	list := []string{"a", "b"}
	list = AppendRecent(list, "b", 3)
	if len(list) != 2 {
		t.Fatalf("dedupe failed: %v", list)
	}
	list = AppendRecent(list, "c", 3)
	list = AppendRecent(list, "d", 3)
	if len(list) != 3 || list[len(list)-1] != "d" {
		t.Fatalf("bound failed: %v", list)
	}
	if HasRecent(list, "a") {
		t.Fatalf("expected a evicted: %v", list)
	}
}

func TestStateUpdatePersists(t *testing.T) {
	st, err := OpenState(filepath.Join(t.TempDir(), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update("k", func(s *GroupState) {
		s.LastMsgID = "m9"
		s.BotOpenIDs = append(s.BotOpenIDs, "BOT-1")
	}); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Get("k")
	if !ok || got.LastMsgID != "m9" || len(got.BotOpenIDs) != 1 {
		t.Fatalf("unexpected state: %+v", got)
	}
}
