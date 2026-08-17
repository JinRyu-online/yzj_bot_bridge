package inbound

import (
	"strings"
	"sync"
	"testing"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/dedupe"
	"yzj-bridge/internal/jobs"
	"yzj-bridge/internal/orchestrator"
	"yzj-bridge/internal/registry"
	"yzj-bridge/internal/yzjout"
)

type gateBackend struct {
	mu      sync.Mutex
	started []string
	enter   chan string
	release chan struct{}
}

func (g *gateBackend) CreateSession() (string, error) { return "s", nil }
func (g *gateBackend) ClearSession(string) (string, error) {
	return "s", nil
}
func (g *gateBackend) Run(prompt string, opts bot.RunOpts) bot.RunResult {
	g.mu.Lock()
	g.started = append(g.started, opts.OperatorOpenID+":"+prompt)
	g.mu.Unlock()
	select {
	case g.enter <- opts.OperatorOpenID:
	default:
	}
	if opts.Context != nil {
		select {
		case <-g.release:
			return bot.RunResult{Reply: "done " + prompt, Status: "ok"}
		case <-opts.Context.Done():
			return bot.RunResult{Reply: "任务已中断", Status: "interrupted"}
		}
	}
	<-g.release
	return bot.RunResult{Reply: "done " + prompt, Status: "ok"}
}

type sentMsg struct {
	OpenID  string
	Content string
}

func newSharedDispatcher(t *testing.T, backend bot.Backend) (*Dispatcher, *[]sentMsg, *sync.Mutex) {
	t.Helper()
	reg := registry.New()
	b := &bot.Bot{
		Config: bot.Config{
			ID: "logbot", Name: "LogBot", RoleID: "logbot", Group: "g",
			SessionMode: "shared", AckPending: true, MentionOnReply: true,
			CommandsEnabled: false, SendMsgURL: "http://example.invalid",
		},
		Backend: backend,
	}
	reg.Replace([]*bot.Bot{b})
	var mu sync.Mutex
	var sent []sentMsg
	d := &Dispatcher{
		Reg:    reg,
		Orch:   &orchestrator.Orchestrator{Reg: reg},
		Dedupe: dedupe.New(),
		Jobs:   jobs.New(),
		SendText: func(sendURL, content, openID string, reply *yzjout.ReplyMeta) error {
			mu.Lock()
			sent = append(sent, sentMsg{OpenID: openID, Content: content})
			mu.Unlock()
			return nil
		},
	}
	return d, &sent, &mu
}

func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout")
}

func TestSharedChannelQueuesAndUpdatesPositions(t *testing.T) {
	g := &gateBackend{enter: make(chan string, 8), release: make(chan struct{})}
	d, sentPtr, mu := newSharedDispatcher(t, g)

	d.Handle("logbot", Normalized{OpenID: "a", Name: "甲", MsgID: "m1", Content: "问A"})
	select {
	case id := <-g.enter:
		if id != "a" {
			t.Fatalf("first runner=%s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("A did not start")
	}

	d.Handle("logbot", Normalized{OpenID: "b", Name: "乙", MsgID: "m2", Content: "问B"})
	d.Handle("logbot", Normalized{OpenID: "c", Name: "丙", MsgID: "m3", Content: "问C"})

	mu.Lock()
	sent := append([]sentMsg(nil), *sentPtr...)
	mu.Unlock()
	if !hasMsg(sent, "b", "正在处理之前的问题，你的问题排在第1位") {
		t.Fatalf("missing B pos1: %+v", sent)
	}
	if !hasMsg(sent, "c", "正在处理之前的问题，你的问题排在第2位") {
		t.Fatalf("missing C pos2: %+v", sent)
	}

	g.release <- struct{}{} // finish A
	select {
	case id := <-g.enter:
		if id != "b" {
			t.Fatalf("second runner=%s want b", id)
		}
	case <-time.After(time.Second):
		t.Fatal("B did not start after A")
	}

	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hasMsg(*sentPtr, "c", "正在处理之前的问题，你的问题排在第1位") &&
			hasMsg(*sentPtr, "a", "任务已完成\n\ndone 问A")
	})

	g.release <- struct{}{} // finish B
	select {
	case id := <-g.enter:
		if id != "c" {
			t.Fatalf("third runner=%s want c", id)
		}
	case <-time.After(time.Second):
		t.Fatal("C did not start after B")
	}
	g.release <- struct{}{}
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hasMsg(*sentPtr, "c", "任务已完成\n\ndone 问C")
	})
}

func TestPerUserAllowsParallelDifferentOpenIDs(t *testing.T) {
	g := &gateBackend{enter: make(chan string, 8), release: make(chan struct{})}
	reg := registry.New()
	b := &bot.Bot{
		Config: bot.Config{
			ID: "logbot", Name: "LogBot", SessionMode: "per_user",
			AckPending: true, CommandsEnabled: false, SendMsgURL: "http://example.invalid",
		},
		Backend: g,
	}
	reg.Replace([]*bot.Bot{b})
	d := &Dispatcher{
		Reg: reg, Orch: &orchestrator.Orchestrator{Reg: reg},
		Dedupe: dedupe.New(), Jobs: jobs.New(),
		SendText: func(sendURL, content, openID string, reply *yzjout.ReplyMeta) error { return nil },
	}
	d.Handle("logbot", Normalized{OpenID: "a", MsgID: "1", Content: "问A"})
	d.Handle("logbot", Normalized{OpenID: "b", MsgID: "2", Content: "问B"})
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case id := <-g.enter:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatalf("expected two parallel runners, saw %v", seen)
		}
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("seen=%v", seen)
	}
	g.release <- struct{}{}
	g.release <- struct{}{}
}

func TestStopCancelsRunningJobAndStartsNext(t *testing.T) {
	g := &gateBackend{enter: make(chan string, 8), release: make(chan struct{})}
	d, sentPtr, mu := newSharedDispatcher(t, g)

	d.Handle("logbot", Normalized{OpenID: "a", Name: "甲", MsgID: "m1", Content: "问A"})
	select {
	case id := <-g.enter:
		if id != "a" {
			t.Fatalf("first runner=%s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("A did not start")
	}
	d.Handle("logbot", Normalized{OpenID: "b", Name: "乙", MsgID: "m2", Content: "问B"})
	d.Handle("logbot", Normalized{OpenID: "a", Name: "甲", MsgID: "m3", Content: "--stop"})

	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hasMsg(*sentPtr, "a", "已中断当前任务")
	})
	select {
	case id := <-g.enter:
		if id != "b" {
			t.Fatalf("second runner=%s want b", id)
		}
	case <-time.After(time.Second):
		t.Fatal("B did not start after stop")
	}
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hasMsg(*sentPtr, "a", "任务已中断")
	})
	mu.Lock()
	for _, m := range *sentPtr {
		if m.OpenID == "a" && strings.Contains(m.Content, "任务已完成") && strings.Contains(m.Content, "任务已中断") {
			mu.Unlock()
			t.Fatalf("interrupt should not use completion prefix: %q", m.Content)
		}
	}
	mu.Unlock()
	g.release <- struct{}{}
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hasMsg(*sentPtr, "b", "任务已完成\n\ndone 问B")
	})
}

func TestJobsListsCurrentAndQueued(t *testing.T) {
	g := &gateBackend{enter: make(chan string, 8), release: make(chan struct{})}
	d, sentPtr, mu := newSharedDispatcher(t, g)

	d.Handle("logbot", Normalized{OpenID: "a", Name: "甲", MsgID: "m1", Content: "问A"})
	select {
	case <-g.enter:
	case <-time.After(time.Second):
		t.Fatal("A did not start")
	}
	d.Handle("logbot", Normalized{OpenID: "b", Name: "乙", MsgID: "m2", Content: "问B"})
	d.Handle("logbot", Normalized{OpenID: "c", Name: "丙", MsgID: "m3", Content: "问C"})
	d.Handle("logbot", Normalized{OpenID: "a", Name: "甲", MsgID: "m4", Content: "--jobs"})

	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hasMsg(*sentPtr, "a", "执行中：甲") &&
			hasMsg(*sentPtr, "a", "内容：问A") &&
			hasMsg(*sentPtr, "a", "1. 乙：问B") &&
			hasMsg(*sentPtr, "a", "2. 丙：问C")
	})
	g.release <- struct{}{}
	g.release <- struct{}{}
	g.release <- struct{}{}
}

func TestJobsWhenIdle(t *testing.T) {
	g := &gateBackend{enter: make(chan string, 8), release: make(chan struct{})}
	d, sentPtr, mu := newSharedDispatcher(t, g)
	d.Handle("logbot", Normalized{OpenID: "a", Name: "甲", MsgID: "m1", Content: "/任务"})
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hasMsg(*sentPtr, "a", "当前没有正在执行或排队的任务")
	})
}

func TestStopWhenIdle(t *testing.T) {
	g := &gateBackend{enter: make(chan string, 8), release: make(chan struct{})}
	d, sentPtr, mu := newSharedDispatcher(t, g)
	d.Handle("logbot", Normalized{OpenID: "a", Name: "甲", MsgID: "m1", Content: "/中断"})
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hasMsg(*sentPtr, "a", "当前没有正在执行的任务")
	})
}

func hasMsg(sent []sentMsg, openID, content string) bool {
	for _, m := range sent {
		if m.OpenID == openID && strings.Contains(m.Content, content) {
			return true
		}
	}
	return false
}
