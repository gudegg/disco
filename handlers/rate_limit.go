package handlers

import (
	"sync"
	"time"
)

type requestLimiter struct {
	mu      sync.Mutex
	entries map[string][]time.Time
	limit   int
	window  time.Duration
	now     func() time.Time
}

func NewRequestLimiter(limit int, window time.Duration) *requestLimiter {
	return &requestLimiter{
		entries: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

func (l *requestLimiter) Allow(key string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)
	hits := l.entries[key]
	kept := hits[:0]
	for _, hit := range hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}

	if len(kept) >= l.limit {
		l.entries[key] = kept
		return false
	}

	l.entries[key] = append(kept, now)
	return true
}
