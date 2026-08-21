package config

import (
	"testing"
)

func TestMemoryDefaultsAndBotFlag(t *testing.T) {
	f, err := ParseYAML([]byte(`
defaults:
  memory:
    enabled: true
    after_turns: 3
bots:
  - id: fairy
    name: Fairy
    backend: openai
    group: default
    send_msg_url: "https://h/x?yzjtoken=t1"
  - id: muted
    name: Muted
    backend: openai
    memory_enabled: false
    group: default
    send_msg_url: "https://h/x?yzjtoken=t2"
`))
	if err != nil {
		t.Fatal(err)
	}
	defs := f.MergedDefaults()
	mem, ok := defs["memory"].(map[string]any)
	if !ok {
		t.Fatalf("memory missing: %#v", defs["memory"])
	}
	if mem["enabled"] != true {
		t.Fatalf("enabled=%v", mem["enabled"])
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 {
		t.Fatalf("bots=%d", len(bots))
	}
	if !bots[0].MemoryEnabled {
		t.Fatal("fairy should default memory_enabled true")
	}
	if bots[1].MemoryEnabled {
		t.Fatal("muted should be false")
	}
}
