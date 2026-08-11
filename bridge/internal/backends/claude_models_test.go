package backends

import "testing"

func TestMergeClaudeModels(t *testing.T) {
	got := mergeClaudeModels(
		[]ModelInfo{{ID: "sonnet", Label: "Sonnet (latest)"}},
		[]ModelInfo{
			{ID: "sonnet", Label: "dup"},
			{ID: "claude-opus-4-6", Label: "Claude Opus 4.6"},
		},
	)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(got), got)
	}
	if got[0].ID != "sonnet" || got[0].Label != "Sonnet (latest)" {
		t.Fatalf("alias first: %+v", got[0])
	}
	if got[1].ID != "claude-opus-4-6" {
		t.Fatalf("api model: %+v", got[1])
	}
}

func TestListClaudeModelsNoKey(t *testing.T) {
	models, warn := ListClaudeModels("claude", "")
	if warn != "" {
		t.Fatalf("unexpected warn: %q", warn)
	}
	if len(models) < 3 {
		t.Fatalf("expected aliases, got %+v", models)
	}
	if models[0].ID != "sonnet" {
		t.Fatalf("first=%+v", models[0])
	}
}
