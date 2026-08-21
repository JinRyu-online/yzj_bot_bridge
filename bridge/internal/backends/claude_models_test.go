package backends

import "testing"

func TestListClaudeModels(t *testing.T) {
	models, warn := ListClaudeModels("claude")
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
