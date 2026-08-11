package logbuf

import "testing"

func TestMatchBotExactChannel(t *testing.T) {
	b := New(100)
	b.Append("info", "fairy__workAssistant", "bot=fairy__workAssistant ask: a")
	b.Append("info", "fairy__devlopment", "bot=fairy__devlopment ask: b")
	b.Append("info", "fairy", "bot=fairy ask: c")

	got := b.Since(0, "fairy__workAssistant")
	if len(got) != 1 || got[0].Bot != "fairy__workAssistant" {
		t.Fatalf("channel filter leaked: %+v", got)
	}

	gotRole := b.Since(0, "fairy")
	if len(gotRole) != 3 {
		t.Fatalf("role filter should match all fairy*, got %d", len(gotRole))
	}
}
