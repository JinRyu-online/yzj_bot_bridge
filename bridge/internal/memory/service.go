package memory

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"yzj-bridge/internal/bot"
)

// Service is the runtime facade used by orchestrator / inbound / control API.
type Service struct {
	mu       sync.Mutex
	Store    *Store
	Cfg      Config
	Defaults map[string]any
	Sched    *Scheduler
	now      func() time.Time
}

// NewService builds a Service. root empty → DefaultRoot().
func NewService(root string, cfg Config) *Service {
	st := NewStore(root)
	_ = st.EnsureDirs()
	s := &Service{
		Store: st,
		Cfg:   cfg,
		now:   time.Now,
	}
	s.Sched = NewScheduler(s)
	return s
}

// SetConfig replaces memory config (e.g. after Runtime.Load).
func (s *Service) SetConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Cfg = cfg
}

// SetDefaults stores global defaults for profiler engine fallback.
func (s *Service) SetDefaults(defaults map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Defaults = defaults
}

// ConfigSnapshot returns a copy of current config.
func (s *Service) ConfigSnapshot() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Cfg
}

func (s *Service) cfg() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Cfg
}

func (s *Service) defaults() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Defaults
}

// MemoryPromptFor builds the appendix for this turn (empty if skipped).
func (s *Service) MemoryPromptFor(botCfg bot.Config, openID, bindOpenID string) string {
	cfg := s.cfg()
	memID, ok := ResolveMemoryOpenID(cfg, openID, bindOpenID)
	if !ok {
		return ""
	}
	p, err := s.Store.Get(memID)
	if err != nil || p == nil {
		return ""
	}
	if !ShouldInject(cfg, botCfg, openID, bindOpenID, p.OptedOut) {
		return ""
	}
	return RenderAppendix(p, cfg, s.now())
}

// AfterDispatch records a completed pair and may enqueue profiling (async).
func (s *Service) AfterDispatch(botCfg bot.Config, openID, bindOpenID, name, userText string, result bot.RunResult) {
	cfg := s.cfg()
	memID, ok := ResolveMemoryOpenID(cfg, openID, bindOpenID)
	if !ok {
		return
	}
	p, err := s.Store.GetOrCreate(memID)
	if err != nil {
		log.Printf("memory get profile %s: %v", memID, err)
		return
	}
	if p.OptedOut {
		return
	}
	if !ShouldRecord(cfg, botCfg, openID, bindOpenID, p.OptedOut, result.Status, result.Reply) {
		return
	}
	turn := Turn{
		TS:        s.now().UTC().Format(time.RFC3339),
		BotID:     botCfg.ID,
		Group:     botCfg.Group,
		User:      userText,
		Assistant: result.Reply,
	}
	if err := s.Store.AppendTurn(memID, turn); err != nil {
		log.Printf("memory append turn %s: %v", memID, err)
		return
	}
	// Touch profile metadata (does not move profiled_count).
	p.LastSeen = turn.TS
	if name = strings.TrimSpace(name); name != "" {
		p.DisplayName = name
	}
	p.BotsSeen = addUnique(p.BotsSeen, botCfg.ID)
	if err := s.Store.Save(p); err != nil {
		log.Printf("memory save profile %s: %v", memID, err)
	}
	unprofiled, err := s.Store.UnprofiledTurns(memID, p.ProfiledCount)
	if err != nil {
		return
	}
	if len(unprofiled) >= cfg.AfterTurns && s.Sched != nil {
		s.Sched.Enqueue(memID, "after_turns")
	}
}

// SetOptOut sets opted_out for openID.
func (s *Service) SetOptOut(openID string, out bool) (*Profile, error) {
	p, err := s.Store.GetOrCreate(openID)
	if err != nil {
		return nil, err
	}
	p.OptedOut = out
	if err := s.Store.Save(p); err != nil {
		return nil, err
	}
	return p, nil
}

// HandleForget implements IM /forget double-confirm (pending window).
func (s *Service) HandleForget(openID string) (reply string, cleared bool, err error) {
	cfg := s.cfg()
	pendingSec := cfg.ForgetPendingSec
	if pendingSec <= 0 {
		pendingSec = 300
	}
	p, err := s.Store.GetOrCreate(openID)
	if err != nil {
		return "", false, err
	}
	now := s.now()
	if p.ForgetPendingUntil != "" {
		until, perr := time.Parse(time.RFC3339, p.ForgetPendingUntil)
		if perr == nil && now.Before(until) {
			if _, err := s.Store.Forget(openID); err != nil {
				return "", false, err
			}
			// Also clear turns so N resets cleanly after explicit forget.
			_ = s.Store.ClearTurns(openID)
			p2, _ := s.Store.Get(openID)
			if p2 != nil {
				p2.ProfiledCount = 0
				_ = s.Store.Save(p2)
			}
			return "已清除你的用户记忆档案。", true, nil
		}
	}
	// First call or expired pending → (re)enter pending.
	p.ForgetPendingUntil = now.Add(time.Duration(pendingSec) * time.Second).UTC().Format(time.RFC3339)
	if err := s.Store.Save(p); err != nil {
		return "", false, err
	}
	mins := pendingSec / 60
	if mins < 1 {
		mins = 1
	}
	return fmt.Sprintf("再次发送 /forget 以确认清除记忆（%d 分钟内有效）。", mins), false, nil
}

// FormatShow returns a short human-readable profile summary.
func (s *Service) FormatShow(openID string) string {
	p, err := s.Store.Get(openID)
	if err != nil {
		return "读取记忆失败: " + err.Error()
	}
	if p == nil {
		n, _ := s.Store.TurnCount(openID)
		if n == 0 {
			return "暂无用户记忆档案。"
		}
		return fmt.Sprintf("尚无画像字段；已记录完成问答对 %d。", n)
	}
	var b strings.Builder
	if p.OptedOut {
		b.WriteString("记忆已关闭（/memory off）。\n")
	}
	fmt.Fprintf(&b, "open_id=%s\n", p.OpenID)
	if p.DisplayName != "" {
		fmt.Fprintf(&b, "display_name=%s\n", p.DisplayName)
	}
	writeShowField(&b, "how_to_address", p.HowToAddress)
	writeShowField(&b, "role", p.Role)
	writeShowField(&b, "ask_style", p.AskStyle)
	writeShowField(&b, "reply_style", p.ReplyStyle)
	donts := p.Donts.Effective()
	if len(donts) > 0 {
		fmt.Fprintf(&b, "donts=%s\n", strings.Join(donts, "; "))
	}
	if n := p.Notes.Effective(); n != "" {
		fmt.Fprintf(&b, "notes=%s\n", clipRunes(n, 200))
	}
	turns, _ := s.Store.TurnCount(openID)
	fmt.Fprintf(&b, "turns=%d profiled=%d\n", turns, p.ProfiledCount)
	return strings.TrimSpace(b.String())
}

func writeShowField(b *strings.Builder, name string, f Field) {
	v := f.Effective()
	if v == "" {
		return
	}
	src := "inferred"
	if strings.TrimSpace(f.Manual) != "" {
		src = "manual"
	}
	lock := ""
	if f.Locked {
		lock = " locked"
	}
	fmt.Fprintf(b, "%s(%s%s)=%s\n", name, src, lock, v)
}

func addUnique(list []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// ListVisible returns profiles that exist on disk OR have turns>0.
// When botFilter is set, only profiles that have seen that bot_id (or have turns from it) are listed.
func (s *Service) ListVisible(botFilter string) ([]*Profile, error) {
	botFilter = strings.TrimSpace(botFilter)
	byID := map[string]*Profile{}

	profiles, err := s.Store.ListProfiles()
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		byID[p.OpenID] = p
	}

	// Include openIDs that only have turns (no profile file yet).
	// Turns files are named by sanitized openID; cross-check via profile open_ids
	// and by scanning turns directory — for v1 we also scan profiles' turns counts.
	// Practical approach: for each known profile, attach turn count; also walk turns
	// filenames and if no profile, create a stub when turn count > 0.
	turnIDs, _ := s.Store.ListOpenIDsWithTurns()
	for _, sid := range turnIDs {
		n, _ := s.Store.TurnCount(sid)
		if n <= 0 {
			continue
		}
		// Resolve open_id: prefer existing profile whose sanitized id matches.
		found := false
		for id, p := range byID {
			if SanitizeOpenID(id) == sid {
				found = true
				_ = p
				break
			}
		}
		if found {
			continue
		}
		// Stub profile keyed by sanitized id (best-effort when open_id unknown).
		if _, ok := byID[sid]; !ok {
			byID[sid] = &Profile{OpenID: sid}
		}
	}

	var out []*Profile
	for _, p := range byID {
		n, _ := s.Store.TurnCount(p.OpenID)
		if n == 0 {
			// keep if profile file has any meaningful content or existed
			hasFile := false
			for _, existing := range profiles {
				if existing.OpenID == p.OpenID {
					hasFile = true
					break
				}
			}
			if !hasFile {
				continue
			}
		}
		if botFilter != "" {
			if !seenBot(p, botFilter) {
				turns, _ := s.Store.ListTurns(p.OpenID)
				ok := false
				for _, t := range turns {
					if t.BotID == botFilter {
						ok = true
						break
					}
				}
				if !ok {
					continue
				}
			}
		}
		out = append(out, p)
	}
	return out, nil
}

func seenBot(p *Profile, botID string) bool {
	for _, id := range p.BotsSeen {
		if id == botID {
			return true
		}
	}
	return false
}
