package yzjcli

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// MessageLister is the subset of Client needed by the watcher (fakeable).
type MessageLister interface {
	MessageList(ctx context.Context, groupID, typ, anchorMsgID string, limit int) (MessagePage, error)
}

// Watcher polls one configured group chat and reports new messages.
//
// It never polls groups that are not in the bridge config: the runtime creates
// one Watcher per (profile, groupID) referenced by bots/channels.
// sharedPollCooldown 是进程内所有 watcher 共享的限流冷却：某个群触发
// 10000429 后，其它群不要在冷却期内继续撞接口。
var sharedPollCooldown struct {
	mu    sync.Mutex
	until time.Time
}

func waitSharedPoll(ctx context.Context) bool {
	for {
		sharedPollCooldown.mu.Lock()
		d := time.Until(sharedPollCooldown.until)
		sharedPollCooldown.mu.Unlock()
		if d <= 0 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
		}
	}
}

func resetSharedPollCooldown() {
	sharedPollCooldown.mu.Lock()
	sharedPollCooldown.until = time.Time{}
	sharedPollCooldown.mu.Unlock()
}

func noteSharedThrottle() {
	sharedPollCooldown.mu.Lock()
	next := time.Now().Add(30 * time.Second)
	if next.After(sharedPollCooldown.until) {
		sharedPollCooldown.until = next
	}
	sharedPollCooldown.mu.Unlock()
}

type Watcher struct {
	Client       MessageLister
	Store        *StateStore
	Key          string // profile|groupID (state key)
	GroupID      string
	GroupName    string
	Interval     time.Duration
	CatchupPages int
	StartupMode  string // baseline (default) | replay
	OnMessage    func(ctx context.Context, msg Message)
	OnAuthGiveUp func(key, reason string)

	mu           sync.Mutex
	enabled      bool
	cancel       context.CancelFunc
	lastPoll     time.Time
	lastMsgAt    time.Time
	lastErr      string
	authFails    int
	backoff      time.Duration
	lastThrottle time.Time
}

// SetEnabled starts/stops the polling loop.
func (w *Watcher) SetEnabled(v bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if v == w.enabled {
		return
	}
	w.enabled = v
	if v {
		if w.cancel == nil {
			ctx, cancel := context.WithCancel(context.Background())
			w.cancel = cancel
			go w.loop(ctx)
		}
		return
	}
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
}

// Enabled reports the current switch state.
func (w *Watcher) Enabled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enabled
}

// Dispose stops the watcher.
func (w *Watcher) Dispose() { w.SetEnabled(false) }

// Snapshot returns runtime status for the control API.
func (w *Watcher) Snapshot() (enabled bool, lastPoll, lastMsg time.Time, lastErr string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enabled, w.lastPoll, w.lastMsgAt, w.lastErr
}

func (w *Watcher) loop(ctx context.Context) {
	base := w.Interval
	if base <= 0 {
		base = 5 * time.Second
	}
	for {
		if !w.Enabled() {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		w.pollOnce(ctx)
		wait := jitter(base)
		if b := w.currentBackoff(); b > wait {
			wait = b
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	// ±20%
	factor := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(d) * factor)
}

func (w *Watcher) currentBackoff() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.backoff
}

func (w *Watcher) pollOnce(ctx context.Context) {
	if !waitSharedPoll(ctx) {
		return
	}
	page, err := w.Client.MessageList(ctx, w.GroupID, "newest", "", 20)
	if err != nil {
		w.noteError(err)
		return
	}
	w.mu.Lock()
	w.lastPoll = time.Now()
	w.lastErr = ""
	w.authFails = 0
	// Only drop the throttle penalty after a quiet period; a single success
	// right after a 10000429 must not immediately return to the fast cadence.
	if !w.lastThrottle.IsZero() && time.Since(w.lastThrottle) > 5*time.Minute {
		w.backoff = 0
	}
	w.mu.Unlock()
	if len(page.List) == 0 {
		return
	}
	newest := page.List[len(page.List)-1]
	st, ok := w.Store.Get(w.Key)
	if !ok || strings.TrimSpace(st.LastMsgID) == "" {
		// First run: establish a baseline without replaying history.
		st.LastMsgID = newest.MsgID
		st.LastSendTime = newest.SendTime
		if err := w.Store.Put(w.Key, st); err != nil {
			log.Printf("yzjcli watcher=%s save baseline: %v", w.Key, err)
		}
		if strings.EqualFold(strings.TrimSpace(w.StartupMode), "replay") {
			w.dispatch(ctx, page.List)
		}
		return
	}
	if st.LastMsgID == newest.MsgID {
		return
	}
	msgs, found, gerr := w.gather(ctx, page, st.LastMsgID)
	if gerr != nil {
		// 补拉历史失败：不要推进游标，下一轮重试，避免把出错窗口内的消息永久跳过。
		w.noteError(gerr)
		return
	}
	if !found {
		w.noteErrorf("cursor %s not found within catch-up window; skipping %d message(s) to avoid replay", st.LastMsgID, len(msgs))
	}
	if len(msgs) > 0 && found {
		w.dispatch(ctx, msgs)
	}
	// Always advance the cursor, even when messages were skipped. Use Update
	// (not Put with the pre-dispatch snapshot) so the recent-msg ring written
	// while dispatching is preserved.
	if err := w.Store.Update(w.Key, func(s *GroupState) {
		s.LastMsgID = newest.MsgID
		s.LastSendTime = newest.SendTime
	}); err != nil {
		log.Printf("yzjcli watcher=%s save cursor: %v", w.Key, err)
	}
}

// gather collects new messages in ascending time order. page.List is ascending
// (platform order). When cursor is not in the newest page, older pages are
// pulled until it is found or the catch-up budget is exhausted.
func (w *Watcher) gather(ctx context.Context, page MessagePage, cursor string) ([]Message, bool, error) {
	list := page.List
	if idx := indexOfMsg(list, cursor); idx >= 0 {
		return dedupeMsgs(list[idx+1:]), true, nil
	}
	catchup := w.CatchupPages
	if catchup <= 0 {
		catchup = 3
	}
	var older []Message
	anchor := list[0].MsgID
	for i := 0; i < catchup; i++ {
		prev, err := w.Client.MessageList(ctx, w.GroupID, "old", anchor, 20)
		if err != nil {
			return nil, false, err
		}
		if len(prev.List) == 0 {
			break
		}
		if idx := indexOfMsg(prev.List, cursor); idx >= 0 {
			older = append(append([]Message{}, prev.List[idx+1:]...), older...)
			return dedupeMsgs(append(older, list...)), true, nil
		}
		older = append(append([]Message{}, prev.List...), older...)
		anchor = prev.List[0].MsgID
		if len(prev.List) < 20 {
			break
		}
	}
	return dedupeMsgs(append(older, list...)), false, nil
}

func (w *Watcher) dispatch(ctx context.Context, msgs []Message) {
	for _, msg := range msgs {
		if msg.IsRecall() {
			continue
		}
		// Skip anything already handled in a previous cycle (crash-safe ring).
		if st, ok := w.Store.Get(w.Key); ok && HasRecent(st.RecentMsgIDs, msg.MsgID) {
			continue
		}
		if w.OnMessage != nil {
			w.OnMessage(ctx, msg)
		}
		if t := msg.SendTimeValue(); !t.IsZero() {
			w.mu.Lock()
			w.lastMsgAt = t
			w.mu.Unlock()
		}
		_ = w.Store.Update(w.Key, func(st *GroupState) {
			st.RecentMsgIDs = AppendRecent(st.RecentMsgIDs, msg.MsgID, 200)
			st.LastMsgID = msg.MsgID
			st.LastSendTime = msg.SendTime
		})
	}
}

func (w *Watcher) noteError(err error) {
	w.mu.Lock()
	w.lastErr = err.Error()
	if IsRateLimited(err) {
		noteSharedThrottle()
		next := 30 * time.Second
		if w.backoff > 0 {
			next = w.backoff * 2
		}
		if next > 5*time.Minute {
			next = 5 * time.Minute
		}
		w.backoff = next
		w.lastThrottle = time.Now()
	}
	auth := IsAuthError(err)
	if auth {
		w.authFails++
	}
	fails := w.authFails
	w.mu.Unlock()

	if auth && fails >= 3 {
		log.Printf("yzjcli watcher=%s: auth failed %d times, disabling: %v", w.Key, fails, err)
		w.SetEnabled(false)
		if w.OnAuthGiveUp != nil {
			w.OnAuthGiveUp(w.Key, err.Error())
		}
		return
	}
	log.Printf("yzjcli watcher=%s poll: %v", w.Key, err)
}

func (w *Watcher) noteErrorf(format string, args ...any) {
	w.mu.Lock()
	w.lastErr = fmt.Sprintf(format, args...)
	w.mu.Unlock()
	log.Printf("yzjcli watcher=%s "+format, append([]any{w.Key}, args...)...)
}

func indexOfMsg(list []Message, id string) int {
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].MsgID == id {
			return i
		}
	}
	return -1
}

func dedupeMsgs(list []Message) []Message {
	seen := make(map[string]bool, len(list))
	out := list[:0]
	for _, m := range list {
		if m.MsgID == "" || seen[m.MsgID] {
			continue
		}
		seen[m.MsgID] = true
		out = append(out, m)
	}
	return out
}
