package backends

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/sessions"
)

func TestSplitForCompactBelowThresholdKeepsAll(t *testing.T) {
	hist := nTurns(8, "x")
	lim := compactLimits{Enabled: true, Keep: 6, AfterTurns: 10, AfterRunes: 8000}
	_, recent, ok := splitForCompact(hist, lim)
	if ok {
		t.Fatal("should not compact")
	}
	if len(recent) != 8 {
		t.Fatalf("recent=%d", len(recent))
	}
}

func TestSplitForCompactByTurnsKeepsTail(t *testing.T) {
	hist := []bot.HistoryTurn{
		{Role: "user", Content: "查询301027582"},
		{Role: "assistant", Content: "旧单"},
	}
	hist = append(hist, nTurns(10, "pad")...)
	hist = append(hist,
		bot.HistoryTurn{Role: "user", Content: "查询301038245"},
		bot.HistoryTurn{Role: "assistant", Content: "推送失败5次"},
	)
	lim := compactLimits{Enabled: true, Keep: 6, AfterTurns: 10, AfterRunes: 8000}
	prefix, recent, ok := splitForCompact(hist, lim)
	if !ok {
		t.Fatal("want compact")
	}
	if len(recent) != 6 {
		t.Fatalf("recent=%d", len(recent))
	}
	if recent[len(recent)-2].Content != "查询301038245" {
		t.Fatalf("lost current order in recent: %+v", recent)
	}
	if len(prefix)+len(recent) != len(hist) {
		t.Fatalf("split lost turns %d+%d != %d", len(prefix), len(recent), len(hist))
	}
}

func TestSplitForCompactByRunes(t *testing.T) {
	long := strings.Repeat("推送失败", 400) // 1600 runes * 8 turns = 12800
	hist := nTurns(8, long)
	lim := compactLimits{Enabled: true, Keep: 6, AfterTurns: 20, AfterRunes: 8000}
	prefix, recent, ok := splitForCompact(hist, lim)
	if !ok {
		t.Fatal("want compact by runes")
	}
	if len(prefix) != 2 || len(recent) != 6 {
		t.Fatalf("prefix=%d recent=%d", len(prefix), len(recent))
	}
}

func TestApplyCompactReusesStoredHash(t *testing.T) {
	dir := t.TempDir()
	store, err := sessions.Open(dir + "/s.json")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	o := &OpenAIBackend{
		cfg:   bot.Config{ID: "bot", OpenAICompact: true, OpenAICompactKeep: 2, OpenAICompactAfterTurns: 4, OpenAICompactAfterRunes: 999999},
		store: store,
		summarize: func(old string, prefix []bot.HistoryTurn) (string, error) {
			calls++
			return "摘要含301038245", nil
		},
	}
	hist := []bot.HistoryTurn{
		{Role: "user", Content: "查询301038245"},
		{Role: "assistant", Content: "推送失败"},
		{Role: "user", Content: "为什么"},
		{Role: "assistant", Content: "格式不一致"},
		{Role: "user", Content: "再看下代码"},
		{Role: "assistant", Content: "已看"},
	}
	sum1, recent := o.applyCompact(hist, "u1", "m")
	if calls != 1 || sum1 != "摘要含301038245" {
		t.Fatalf("calls=%d sum=%q", calls, sum1)
	}
	if len(recent) != 2 {
		t.Fatalf("recent=%d", len(recent))
	}
	sum2, _ := o.applyCompact(hist, "u1", "m")
	if calls != 1 {
		t.Fatalf("should reuse stored summary, calls=%d", calls)
	}
	if sum2 != sum1 {
		t.Fatalf("sum2=%q", sum2)
	}
}

func TestApplyCompactIncrementalUsesOldSummary(t *testing.T) {
	o := &OpenAIBackend{
		cfg:   bot.Config{ID: "bot", OpenAICompact: true, OpenAICompactKeep: 2, OpenAICompactAfterTurns: 4, OpenAICompactAfterRunes: 999999},
		store: mustStore(t),
		summarize: func(old string, prefix []bot.HistoryTurn) (string, error) {
			if old == "" {
				return "第一段", nil
			}
			if old != "第一段" {
				t.Fatalf("old=%q", old)
			}
			if len(prefix) != 2 {
				t.Fatalf("delta len=%d", len(prefix))
			}
			return "第一段+新增", nil
		},
	}
	hist := nTurns(6, "a")
	_, _ = o.applyCompact(hist, "u1", "m")
	hist = append(hist, nTurns(2, "b")...)
	sum, recent := o.applyCompact(hist, "u1", "m")
	if sum != "第一段+新增" {
		t.Fatalf("sum=%q", sum)
	}
	if len(recent) != 2 {
		t.Fatalf("recent=%d", len(recent))
	}
}

func TestOpenAIRunInjectsJsonlWhenHistoryEmpty(t *testing.T) {
	dir := t.TempDir()
	sessions.AppendConversation(dir, "operate_group", "bsame", "u1", "user", "查询301038245")
	sessions.AppendConversation(dir, "operate_group", "bsame", "u1", "assistant", "推送失败5次")

	var captured []oaMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req oaRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		captured = req.Messages
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	o := NewOpenAI(bot.Config{
		ID: "bsame", Group: "operate_group", SessionMode: "per_user",
		ConversationsDir: dir, Model: "m",
		OpenAIBaseURL: srv.URL, OpenAITimeout: 5,
		OpenAICompact: false,
	}, nil)
	res := o.Run("推送为什么报错", bot.RunOpts{OperatorOpenID: "u1", Mode: "ask"})
	if res.Status != "ok" {
		t.Fatalf("%+v", res)
	}
	joined := ""
	for _, m := range captured {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "查询301038245") || !strings.Contains(joined, "推送失败5次") {
		t.Fatalf("history not injected: %s", joined)
	}
	if !strings.Contains(joined, "推送为什么报错") {
		t.Fatalf("current prompt missing: %s", joined)
	}
}

func TestOpenAIRunDoesNotLoadJsonlWhenHistoryProvided(t *testing.T) {
	dir := t.TempDir()
	sessions.AppendConversation(dir, "g", "b", "u1", "user", "查询301038281")
	sessions.AppendConversation(dir, "g", "b", "u1", "assistant", "工作区单号")

	var captured []oaMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req oaRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		captured = req.Messages
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	o := NewOpenAI(bot.Config{
		ID: "b", Group: "g", SessionMode: "per_user",
		ConversationsDir: dir, Model: "m",
		OpenAIBaseURL: srv.URL, OpenAITimeout: 5,
	}, nil)
	res := o.Run("继续", bot.RunOpts{
		OperatorOpenID: "u1", Mode: "ask",
		History: []bot.HistoryTurn{
			{Role: "user", Content: "查询301038245"},
			{Role: "assistant", Content: "推送失败"},
		},
	})
	if res.Status != "ok" {
		t.Fatalf("%+v", res)
	}
	joined := ""
	for _, m := range captured {
		joined += m.Content + "\n"
	}
	if strings.Contains(joined, "301038281") {
		t.Fatalf("jsonl leaked into provided history: %s", joined)
	}
	if !strings.Contains(joined, "301038245") {
		t.Fatalf("provided history missing: %s", joined)
	}
}

func TestAppendHistoryMessagesPutsSummaryBeforeRecent(t *testing.T) {
	msgs := appendHistoryMessages(nil, "当前订单301038245 推送失败", []bot.HistoryTurn{
		{Role: "user", Content: "为什么报错"},
	})
	if len(msgs) != 3 {
		t.Fatalf("len=%d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "301038245") || msgs[0].Role != "user" {
		t.Fatalf("%+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[2].Content != "为什么报错" {
		t.Fatalf("%+v", msgs)
	}
}

func nTurns(n int, content string) []bot.HistoryTurn {
	out := make([]bot.HistoryTurn, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		out[i] = bot.HistoryTurn{Role: role, Content: content}
	}
	return out
}

func mustStore(t *testing.T) *sessions.Store {
	t.Helper()
	st, err := sessions.Open(t.TempDir() + "/s.json")
	if err != nil {
		t.Fatal(err)
	}
	return st
}
