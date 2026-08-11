package dedupe

import "sync"

const maxSeen = 5000

type Store struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func New() *Store {
	return &Store{seen: make(map[string]struct{})}
}

// AlreadySeen returns true if key was seen before; otherwise records it.
func (s *Store) AlreadySeen(key string) bool {
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[key]; ok {
		return true
	}
	s.seen[key] = struct{}{}
	if len(s.seen) > maxSeen {
		// Drop roughly half (order undefined, matches Python intent).
		n := 0
		half := maxSeen / 2
		for k := range s.seen {
			delete(s.seen, k)
			n++
			if n >= half {
				break
			}
		}
	}
	return false
}
