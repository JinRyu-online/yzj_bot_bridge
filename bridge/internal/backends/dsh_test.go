package backends

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestDSHPoolKey 验证 poolKey 构成与唯一性（provider|model|profile|nodeBin|dshEntry）。
func TestDSHPoolKey(t *testing.T) {
	k1 := dshPoolKey{Provider: "kuaidi100", Model: "deepseek-v4-flash", Profile: "jsonrpc", NodeBin: "node", DSHEntry: "C:/x/bin.js"}
	k2 := k1
	if k1 != k2 {
		t.Fatal("同字段 key 应相等")
	}
	k3 := k1
	k3.Model = "qwen3.5-plus"
	if k1 == k3 {
		t.Fatal("模型不同应不等")
	}
	k4 := k1
	k4.DSHEntry = "C:/y/bin.js"
	if k1 == k4 {
		t.Fatal("入口不同应不等")
	}
	s := k1.String()
	for _, part := range []string{"kuaidi100", "deepseek-v4-flash", "jsonrpc", "node", "C:/x/bin.js"} {
		if !strings.Contains(s, part) {
			t.Fatalf("poolKey.String() 缺 %q: %q", part, s)
		}
	}
}

// TestParseDSHFrame 验证 NDJSON 帧解析（响应 / 错误 / 事件通知）。
func TestParseDSHFrame(t *testing.T) {
	var f dshFrame
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":3,"result":{"serverInfo":{"name":"x"}}}`), &f); err != nil {
		t.Fatal(err)
	}
	if f.ID != 3 || f.Result == nil || f.Method != "" {
		t.Fatalf("响应帧解析异常: %+v", f)
	}
	var e dshFrame
	_ = json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"session \"x\" not found"}}`), &e)
	if e.Error == nil || !strings.Contains(e.Error.Message, "not found") {
		t.Fatalf("错误帧解析异常: %+v", e)
	}
	var n dshFrame
	_ = json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"session.event","params":{"sessionId":"s1","event":{"type":"turn/start"}}}`), &n)
	if n.Method != "session.event" || n.ID != 0 {
		t.Fatalf("通知帧解析异常: %+v", n)
	}
}

// TestExtractDSHAssistant 验证 assistant/message 的 text 块提取（非 text 块忽略）。
func TestExtractDSHAssistant(t *testing.T) {
	ev := map[string]any{
		"data": map[string]any{
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "hello "},
					map[string]any{"type": "tool_use", "name": "bash"},
					map[string]any{"type": "text", "text": "world"},
				},
			},
		},
	}
	if got := extractDSHAssistant(ev); got != "hello world" {
		t.Fatalf("提取文本 = %q, want %q", got, "hello world")
	}
	if extractDSHAssistant(map[string]any{}) != "" {
		t.Fatal("空事件应返回空串")
	}
}

// TestDSHTurnReason 验证 turn/end 原因提取（字符串或 {kind: ...} 两种形态）。
func TestDSHTurnReason(t *testing.T) {
	if got := dshTurnReason(map[string]any{"data": map[string]any{"reason": map[string]any{"kind": "completed"}}}); got != "completed" {
		t.Fatalf("对象 reason = %q", got)
	}
	if got := dshTurnReason(map[string]any{"data": map[string]any{"reason": "error"}}); got != "error" {
		t.Fatalf("字符串 reason = %q", got)
	}
	if got := dshTurnReason(map[string]any{}); got != "" {
		t.Fatalf("空 reason = %q", got)
	}
}

// newTestProc 构造纯内存 dshProc（不 spawn 进程），供状态机单测。
func newTestProc() *dshProc {
	return &dshProc{
		deadCh:     make(chan struct{}),
		known:      map[string]bool{},
		inflight:   map[string]bool{},
		lastActive: time.Now(),
		pending:    map[int64]chan *dshFrame{},
		subs:       map[string]map[chan *dshFrame]struct{}{},
	}
}

// TestDSHEventDispatch 验证 session.event 按 sessionId 分发、无订阅者丢弃、inflight 标记/清除。
func TestDSHEventDispatch(t *testing.T) {
	p := newTestProc()
	chA, unsubA, err := p.subscribe("A")
	if err != nil {
		t.Fatal(err)
	}
	defer unsubA()
	chB, unsubB, err := p.subscribe("B")
	if err != nil {
		t.Fatal(err)
	}
	defer unsubB()

	// A 的事件只到 A。
	p.handleFrame(&dshFrame{Method: "session.event", Params: map[string]any{
		"sessionId": "A", "event": map[string]any{"type": "turn/start"},
	}})
	select {
	case f := <-chA:
		if f.Params["sessionId"] != "A" {
			t.Fatalf("A 收到错误会话事件: %v", f.Params["sessionId"])
		}
	default:
		t.Fatal("A 未收到事件")
	}
	select {
	case <-chB:
		t.Fatal("B 不应收到 A 的事件")
	default:
	}

	// B 的事件只到 B。
	p.handleFrame(&dshFrame{Method: "session.event", Params: map[string]any{
		"sessionId": "B", "event": map[string]any{"type": "turn/start"},
	}})
	select {
	case <-chB:
	default:
		t.Fatal("B 未收到事件")
	}
	select {
	case <-chA:
		t.Fatal("A 不应收到 B 的事件")
	default:
	}

	if !p.sessionInflight("A") || !p.sessionInflight("B") {
		t.Fatal("turn/start 后应标记 inflight")
	}
	// turn/end 清除本会话 inflight，不影响其他会话。
	p.handleFrame(&dshFrame{Method: "session.event", Params: map[string]any{
		"sessionId": "A", "event": map[string]any{"type": "turn/end"},
	}})
	if p.sessionInflight("A") {
		t.Fatal("turn/end 后 A 不应 inflight")
	}
	if !p.sessionInflight("B") {
		t.Fatal("B 的 inflight 不应受影响")
	}

	// 无订阅者的会话事件直接丢弃（不 panic、不阻塞）。
	p.handleFrame(&dshFrame{Method: "session.event", Params: map[string]any{
		"sessionId": "nobody", "event": map[string]any{"type": "assistant/message"},
	}})

	// 订阅取消后不再收到。
	unsubB()
	p.handleFrame(&dshFrame{Method: "session.event", Params: map[string]any{
		"sessionId": "B", "event": map[string]any{"type": "turn/end"},
	}})
	select {
	case <-chB:
		t.Fatal("unsub 后 B 不应再收到事件")
	default:
	}
}

// TestDSHColdHotDecision 验证 known/inflight/evict 状态机（冷热分流的判据）。
func TestDSHColdHotDecision(t *testing.T) {
	p := newTestProc()
	sid := "s1"
	if p.knownSession(sid) {
		t.Fatal("新 sid 不应 known")
	}
	p.markKnown(sid)
	if !p.knownSession(sid) {
		t.Fatal("markKnown 后应 known")
	}
	if p.sessionInflight(sid) {
		t.Fatal("新会话不应 inflight")
	}
	// known + inflight（残留 turn）→ 等 idle 后再发。
	p.handleFrame(&dshFrame{Method: "session.event", Params: map[string]any{
		"sessionId": sid, "event": map[string]any{"type": "turn/start"},
	}})
	if !p.sessionInflight(sid) {
		t.Fatal("turn/start 后应 inflight")
	}
	// evict 后 → 冷（known 清除，inflight 一并清除）。
	p.evictSession(sid)
	if p.knownSession(sid) {
		t.Fatal("evict 后不应 known")
	}
	if p.sessionInflight(sid) {
		t.Fatal("evict 后不应 inflight")
	}
}

// TestDSHPendingResponse 验证 request/response 匹配与超时清理。
func TestDSHPendingResponse(t *testing.T) {
	p := newTestProc()
	id, ch := p.registerPending()
	p.mu.Lock()
	got, ok := p.pending[id]
	p.mu.Unlock()
	if !ok || got != ch {
		t.Fatal("registerPending 未登记")
	}
	p.handleFrame(&dshFrame{ID: id, Result: map[string]any{"messageId": "m1"}})
	select {
	case f := <-ch:
		if f.Result["messageId"] != "m1" {
			t.Fatalf("响应内容错误: %v", f.Result)
		}
	default:
		t.Fatal("未收到响应")
	}
	// 响应后 pending 应已清除。
	p.mu.Lock()
	_, ok = p.pending[id]
	p.mu.Unlock()
	if ok {
		t.Fatal("响应后 pending 应清除")
	}
}
