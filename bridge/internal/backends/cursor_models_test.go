package backends

import (
	"strings"
	"testing"
)

func TestParseAgentModelsOutputLines(t *testing.T) {
	raw := `Available models
auto - Auto (current, default)
composer-2.5 - Composer 2.5
composer-2.5-fast - Composer 2.5 Fast
Tip: use --model <id>
`
	got := parseAgentModelsOutput(raw)
	if len(got) < 3 {
		t.Fatalf("got %d models: %+v", len(got), got)
	}
	if got[0].ID != "auto" || !strings.Contains(got[0].Label, "Auto") {
		t.Fatalf("first=%+v", got[0])
	}
}

func TestParseAgentModelsOutputFlattened(t *testing.T) {
	raw := `Available models auto - Auto (current, default) cursor-grok-4.5-high - Cursor Grok 4.5 composer-2.5 - Composer 2.5 kimi-k3-max - Kimi K3 Tip: use --model <id> (or /model <id>`
	got := parseAgentModelsOutput(raw)
	if len(got) < 4 {
		t.Fatalf("got %d models: %+v", len(got), got)
	}
	ids := map[string]bool{}
	for _, m := range got {
		ids[m.ID] = true
	}
	for _, want := range []string{"auto", "cursor-grok-4.5-high", "composer-2.5", "kimi-k3-max"} {
		if !ids[want] {
			t.Fatalf("missing %s in %+v", want, got)
		}
	}
}
