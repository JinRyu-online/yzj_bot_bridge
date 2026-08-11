package registry

import (
	"regexp"
	"strings"
	"sync"

	"yzj-bridge/internal/bot"
)

var mentionRe = regexp.MustCompile(`@([^\s@]+)`)

type Registry struct {
	mu    sync.RWMutex
	bots  map[string]*bot.Bot
	order []string // stable insertion order for UI / status
}

func New() *Registry {
	return &Registry{bots: map[string]*bot.Bot{}}
}

func (r *Registry) Replace(bots []*bot.Bot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bots = map[string]*bot.Bot{}
	r.order = make([]string, 0, len(bots))
	for _, b := range bots {
		r.bots[b.Config.ID] = b
		r.order = append(r.order, b.Config.ID)
	}
}

func (r *Registry) Get(id string) *bot.Bot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.bots[id]
}

func (r *Registry) List() []*bot.Bot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*bot.Bot, 0, len(r.order))
	for _, id := range r.order {
		if b := r.bots[id]; b != nil {
			out = append(out, b)
		}
	}
	return out
}

func (r *Registry) Resolve(token, groupID string) *bot.Bot {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var fallback *bot.Bot
	for _, b := range r.bots {
		id := strings.ToLower(b.Config.ID)
		role := strings.ToLower(b.Config.RoleID)
		name := strings.ToLower(b.Config.Name)
		if id != token && role != token && name != token {
			continue
		}
		if groupID != "" && b.Config.Group == groupID {
			return b
		}
		if fallback == nil {
			fallback = b
		}
	}
	if groupID != "" {
		return nil
	}
	return fallback
}

func (r *Registry) ResolveMention(content, groupID string) []*bot.Bot {
	matches := mentionRe.FindAllStringSubmatch(content, -1)
	seen := map[string]struct{}{}
	var out []*bot.Bot
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		b := r.Resolve(m[1], groupID)
		if b == nil {
			continue
		}
		if _, ok := seen[b.Config.ID]; ok {
			continue
		}
		seen[b.Config.ID] = struct{}{}
		out = append(out, b)
	}
	return out
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.order))
	for _, id := range r.order {
		b := r.bots[id]
		if b != nil && b.Config.Name != "" {
			out = append(out, b.Config.Name)
		}
	}
	return out
}
