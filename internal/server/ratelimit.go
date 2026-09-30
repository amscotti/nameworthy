package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Every name audit spends paid API tokens, so the endpoint gets a small
// per-IP fixed window: generous to humans, expensive to bots.
const (
	rateLimitMax    = 10
	rateLimitWindow = time.Minute
)

type rateBucket struct {
	start time.Time
	count int
}

// rateLimiter is a mutex-protected per-key fixed-window limiter.
type rateLimiter struct {
	mu     sync.Mutex
	now    func() time.Time
	max    int
	window time.Duration
	hits   map[string]rateBucket
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{now: time.Now, max: max, window: window, hits: map[string]rateBucket{}}
}

// allow records a hit for key and reports whether it fits in the window. On
// denial it also reports how long until the window resets. Expired buckets
// are swept on every call so the map can't grow forever.
func (l *rateLimiter) allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, b := range l.hits {
		if now.Sub(b.start) >= l.window {
			delete(l.hits, k)
		}
	}
	b, seen := l.hits[key]
	if !seen {
		l.hits[key] = rateBucket{start: now, count: 1}
		return true, 0
	}
	if b.count >= l.max {
		return false, l.window - now.Sub(b.start)
	}
	b.count++
	l.hits[key] = b
	return true, 0
}

// clientIP keys the limiter by the peer address. X-Forwarded-For is
// deliberately ignored: trusting a client-controlled header would let anyone
// mint fresh identities and dodge the limit.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
