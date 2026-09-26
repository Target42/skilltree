package auth

import (
	"sync"
	"time"
)

// LoginLimiter is a simple in-memory sliding-window limiter for login attempts.
type LoginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	limit    int
	window   time.Duration
}

func NewLoginLimiter(limit int, window time.Duration) *LoginLimiter {
	if limit <= 0 {
		limit = 10
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	return &LoginLimiter{
		attempts: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (l *LoginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	var kept []time.Time
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.attempts, key)
	} else {
		l.attempts[key] = kept
	}
	return len(kept) < l.limit
}

func (l *LoginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.attempts[key], time.Now())
}

func (l *LoginLimiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
