package sessions

import (
	"testing"
	"time"
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

func TestNeedsDailyReset(t *testing.T) {
	today := LocalSessionDay(time.Now())
	yesterday := LocalSessionDay(time.Now().AddDate(0, 0, -1))
	cases := []struct {
		name    string
		enabled bool
		day     string
		want    bool
	}{
		{"disabled", false, yesterday, false},
		{"empty_legacy", true, "", false},
		{"same_day", true, today, false},
		{"stale", true, yesterday, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeedsDailyReset(tc.enabled, tc.day, today); got != tc.want {
				t.Fatalf("NeedsDailyReset(%v,%q,%q)=%v want %v", tc.enabled, tc.day, today, got, tc.want)
			}
		})
	}
}

func TestSetChatIDStampsSessionDay(t *testing.T) {
	st, err := Open(t.TempDir() + "/day.json")
	if err != nil {
		t.Fatal(err)
	}
	today := LocalSessionDay(time.Now())
	st.SetChatID("bot", "u1", "chat-a", "n")
	got := st.GetEntry("bot", "u1")
	if got.Current != "chat-a" || got.SessionDay != today {
		t.Fatalf("first stamp %+v", got)
	}
	st.SetChatID("bot", "u1", "chat-a", "n2")
	got = st.GetEntry("bot", "u1")
	if got.SessionDay != today || got.Name != "n2" {
		t.Fatalf("same id must keep day %+v", got)
	}
	st.mu.Lock()
	users := st.getUsers("bot")
	e := entryFromAny(users["u1"])
	e.SessionDay = "2020-01-01"
	users["u1"] = e
	st.mu.Unlock()
	st.SetChatID("bot", "u1", "chat-a", "")
	got = st.GetEntry("bot", "u1")
	if got.SessionDay != "2020-01-01" {
		t.Fatalf("same id must not refresh day %+v", got)
	}
	st.SetChatID("bot", "u1", "chat-b", "")
	got = st.GetEntry("bot", "u1")
	if got.Current != "chat-b" || got.SessionDay != today {
		t.Fatalf("new id should stamp today %+v", got)
	}
}

func TestSessionDayPersistsAndClears(t *testing.T) {
	path := t.TempDir() + "/persist.json"
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.SetChatID("bot", "u1", "chat-1", "n")
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := st2.GetEntry("bot", "u1")
	if got.SessionDay != LocalSessionDay(time.Now()) {
		t.Fatalf("reload day %+v", got)
	}
	st2.ClearSession("bot", "u1")
	got = st2.GetEntry("bot", "u1")
	if got.Current != "" || got.SessionDay != "" {
		t.Fatalf("clear must wipe day %+v", got)
	}
	if len(got.History) != 1 || got.History[0].ChatID != "chat-1" {
		t.Fatalf("history %+v", got.History)
	}
}

func TestSetChatIDFillsEmptySessionDay(t *testing.T) {
	st, err := Open(t.TempDir() + "/legacy.json")
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	users := st.getUsers("bot")
	users["u1"] = map[string]any{"current": "legacy-chat"}
	st.mu.Unlock()
	st.SetChatID("bot", "u1", "legacy-chat", "n")
	got := st.GetEntry("bot", "u1")
	if got.SessionDay != LocalSessionDay(time.Now()) {
		t.Fatalf("empty day should stamp today %+v", got)
	}
}
