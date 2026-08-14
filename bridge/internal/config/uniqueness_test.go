package config

import (
	"strings"
	"testing"
)

func TestExpandBotsRejectsDuplicateSendURL(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{"id": "a", "group": "g1", "send_msg_url": "https://h/x?yzjtype=0&yzjtoken=same"},
			{"id": "b", "group": "g2", "send_msg_url": "https://h/x?yzjtype=0&yzjtoken=same"},
		},
	}
	_, err := ExpandBots(f)
	if err == nil || !strings.Contains(err.Error(), "send_msg_url") {
		t.Fatalf("want send_msg_url conflict, got %v", err)
	}
}

func TestExpandBotsRejectsDuplicateTokenDifferentPath(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{"id": "a", "group": "g1", "send_msg_url": "https://h/x?yzjtoken=tok1"},
			{"id": "b", "group": "g2", "send_msg_url": "https://h/other?yzjtoken=tok1"},
		},
	}
	_, err := ExpandBots(f)
	if err == nil || !strings.Contains(err.Error(), "yzjtoken") {
		t.Fatalf("want yzjtoken conflict, got %v", err)
	}
}

func TestExpandBotsRejectsDuplicateWebhookPath(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{"id": "a", "group": "g1", "send_msg_url": "https://h/x?yzjtoken=t1", "webhook_path": "/yzj/webhook/shared"},
			{"id": "b", "group": "g2", "send_msg_url": "https://h/x?yzjtoken=t2", "webhook_path": "/yzj/webhook/shared/"},
		},
	}
	_, err := ExpandBots(f)
	if err == nil || !strings.Contains(err.Error(), "webhook_path") {
		t.Fatalf("want webhook_path conflict, got %v", err)
	}
}

func TestExpandBotsRejectsDuplicateTokenAcrossChannels(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{
				"id": "ghost",
				"channels": []any{
					map[string]any{"group": "a", "send_msg_url": "https://h/x?yzjtoken=one"},
					map[string]any{"group": "b", "send_msg_url": "https://h/x?yzjtoken=one"},
				},
			},
		},
	}
	_, err := ExpandBots(f)
	if err == nil {
		t.Fatal("same token on two channels of one bot should fail")
	}
}

func TestExpandBotsAllowsDistinctWebhooks(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{"id": "a", "group": "g1", "send_msg_url": "https://h/x?yzjtoken=t1"},
			{"id": "b", "group": "g2", "send_msg_url": "https://h/x?yzjtoken=t2"},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 {
		t.Fatalf("len=%d", len(bots))
	}
}

func TestExpandBotsAllowsPlaceholderTokens(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{"id": "a", "group": "g1", "send_msg_url": "https://h/a?yzjtoken=REPLACE_ME"},
			{"id": "b", "group": "g2", "send_msg_url": "https://h/b?yzjtoken=REPLACE_ME"},
		},
	}
	if _, err := ExpandBots(f); err != nil {
		t.Fatalf("placeholder tokens should not collide: %v", err)
	}
}
