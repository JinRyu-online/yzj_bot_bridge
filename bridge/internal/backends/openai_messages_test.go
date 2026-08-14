package backends

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOAMessageToolCallMarshalsContentAsEmptyString(t *testing.T) {
	msg := oaMessage{
		Role: "assistant",
		ToolCalls: []oaToolCall{{
			ID:   "call_1",
			Type: "function",
		}},
	}
	msg.ToolCalls[0].Function.Name = "list_dir"
	msg.ToolCalls[0].Function.Arguments = `{"path":"."}`

	raw, err := json.Marshal(oaRequest{Model: "m", Messages: []oaMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		msg,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	msgs, _ := probe["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages=%d", len(msgs))
	}
	asst, _ := msgs[2].(map[string]any)
	c, ok := asst["content"]
	if !ok {
		t.Fatalf("content omitted; gateways treat missing as null: %s", raw)
	}
	s, ok := c.(string)
	if !ok {
		t.Fatalf("content type %T want string: %s", c, raw)
	}
	if s != "" {
		t.Fatalf("content=%q want empty string", s)
	}
	if _, ok := asst["tool_calls"]; !ok {
		t.Fatal("tool_calls missing")
	}
	if strings.Contains(string(raw), `"content":null`) {
		t.Fatalf("must not emit content null: %s", raw)
	}
}
