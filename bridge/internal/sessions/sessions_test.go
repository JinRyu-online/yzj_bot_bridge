package sessions

import (
	"testing"
)

func TestOpenAICompactPersistsAndClears(t *testing.T) {
	st, err := Open(t.TempDir() + "/s.json")
	if err != nil {
		t.Fatal(err)
	}
	st.SetChatID("bot", "u1", "chat-1", "n")
	st.SetOpenAICompact("bot", "u1", CompactState{Summary: "订单301038245", Count: 4, Hash: "abc"})
	got := st.GetEntry("bot", "u1")
	if got.Current != "chat-1" || got.OpenAICompact.Summary != "订单301038245" {
		t.Fatalf("%+v", got)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(st.path)
	if err != nil {
		t.Fatal(err)
	}
	got = st2.GetEntry("bot", "u1")
	if got.OpenAICompact.Count != 4 || got.OpenAICompact.Hash != "abc" {
		t.Fatalf("reload %+v", got.OpenAICompact)
	}
	st2.ClearSession("bot", "u1")
	got = st2.GetEntry("bot", "u1")
	if got.Current != "" || got.OpenAICompact.Summary != "" {
		t.Fatalf("clear did not wipe compact: %+v", got)
	}
}
