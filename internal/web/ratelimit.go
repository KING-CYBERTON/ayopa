package web

import (
	"sync"
	"time"
)

// limiter is a small in-memory fixed-window counter. It is per process,
// which is fine for a single instance (see ADR-0014 for when to replace it).
type limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*hit
}

type hit struct {
	count   int
	resetAt time.Time
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{limit: limit, window: window, hits: make(map[string]*hit)}
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.hits) > 10000 {
		for k, h := range l.hits {
			if now.After(h.resetAt) {
				delete(l.hits, k)
			}
		}
	}

	h, ok := l.hits[key]
	if !ok || now.After(h.resetAt) {
		l.hits[key] = &hit{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	if h.count >= l.limit {
		return false
	}
	h.count++
	return true
}
