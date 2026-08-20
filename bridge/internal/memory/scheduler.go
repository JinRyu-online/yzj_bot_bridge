package memory

import (
	"context"
	"log"
	"sync"
	"time"
)

// Scheduler runs profile jobs with global concurrency 1 and per-openID serial.
type Scheduler struct {
	svc *Service

	mu       sync.Mutex
	inflight map[string]bool
	queue    []job
	sem      chan struct{}
	stopCh   chan struct{}
	wg       sync.WaitGroup
	stopped  bool
	running  bool
}

type job struct {
	OpenID string
	Reason string
}

// NewScheduler creates a scheduler bound to svc.
func NewScheduler(svc *Service) *Scheduler {
	return &Scheduler{
		svc:      svc,
		inflight: map[string]bool{},
		sem:      make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
	}
}

// StartIdleTicker starts the idle refresh loop. Safe to call multiple times;
// stops the previous ticker first.
func (sch *Scheduler) StartIdleTicker() {
	sch.mu.Lock()
	defer sch.mu.Unlock()
	if sch.running {
		sch.stopLocked()
		sch.stopCh = make(chan struct{})
		sch.stopped = false
	}
	sch.running = true
	sch.wg.Add(1)
	go sch.idleLoop()
}

// Stop stops the idle ticker and rejects new jobs. In-flight jobs may finish.
func (sch *Scheduler) Stop() {
	sch.mu.Lock()
	defer sch.mu.Unlock()
	sch.stopLocked()
}

func (sch *Scheduler) stopLocked() {
	if sch.stopped {
		return
	}
	sch.stopped = true
	sch.running = false
	select {
	case <-sch.stopCh:
	default:
		close(sch.stopCh)
	}
}

// Enqueue schedules a profile run for openID (deduped while inflight/queued).
func (sch *Scheduler) Enqueue(openID, reason string) {
	openID = trimSpace(openID)
	if openID == "" {
		return
	}
	sch.mu.Lock()
	defer sch.mu.Unlock()
	if sch.stopped {
		return
	}
	if sch.inflight[openID] {
		return
	}
	for _, j := range sch.queue {
		if j.OpenID == openID {
			return
		}
	}
	sch.queue = append(sch.queue, job{OpenID: openID, Reason: reason})
	go sch.pump()
}

func (sch *Scheduler) pump() {
	for {
		sch.mu.Lock()
		if sch.stopped || len(sch.queue) == 0 {
			sch.mu.Unlock()
			return
		}
		j := sch.queue[0]
		sch.queue = sch.queue[1:]
		if sch.inflight[j.OpenID] {
			sch.mu.Unlock()
			continue
		}
		sch.inflight[j.OpenID] = true
		sch.mu.Unlock()

		sch.sem <- struct{}{}
		func() {
			defer func() {
				<-sch.sem
				sch.mu.Lock()
				delete(sch.inflight, j.OpenID)
				sch.mu.Unlock()
			}()
			if err := sch.svc.RunProfiler(context.Background(), j.OpenID); err != nil {
				log.Printf("memory profiler open_id=%s reason=%s: %v", j.OpenID, j.Reason, err)
			}
		}()
	}
}

func (sch *Scheduler) idleLoop() {
	defer sch.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-sch.stopCh:
			return
		case <-ticker.C:
			sch.scanIdle()
		}
	}
}

func (sch *Scheduler) scanIdle() {
	cfg := sch.svc.cfg()
	if !cfg.Enabled {
		return
	}
	idleSec := cfg.IdleSec
	if idleSec <= 0 {
		idleSec = 1800
	}
	now := sch.svc.now()
	profiles, err := sch.svc.Store.ListProfiles()
	if err != nil {
		return
	}
	for _, p := range profiles {
		if p == nil || p.OptedOut || p.OpenID == "" {
			continue
		}
		if p.LastSeen == "" {
			continue
		}
		last, err := time.Parse(time.RFC3339, p.LastSeen)
		if err != nil {
			continue
		}
		if now.Sub(last) < time.Duration(idleSec)*time.Second {
			continue
		}
		unprofiled, err := sch.svc.Store.UnprofiledTurns(p.OpenID, p.ProfiledCount)
		if err != nil || len(unprofiled) == 0 {
			continue
		}
		sch.Enqueue(p.OpenID, "idle")
	}
}

// WaitIdle waits for idle loop goroutine (tests / shutdown).
func (sch *Scheduler) WaitIdle() {
	sch.wg.Wait()
}
