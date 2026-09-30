package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterWindowResetAndIsolation(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	lim := newRateLimiter(2, time.Minute)
	lim.now = func() time.Time { return now }

	for i := range 2 {
		if ok, _ := lim.allow("198.51.100.1"); !ok {
			t.Fatalf("hit %d unexpectedly denied", i+1)
		}
	}
	now = now.Add(20 * time.Second)
	ok, retryAfter := lim.allow("198.51.100.1")
	if ok || retryAfter != 40*time.Second {
		t.Fatalf("third hit = (%v, %v), want (false, 40s)", ok, retryAfter)
	}

	// Another peer is unaffected while the first peer is throttled.
	if ok, _ := lim.allow("198.51.100.2"); !ok {
		t.Fatal("distinct peer IP must have its own bucket")
	}

	// Advancing past the window sweeps expired buckets and permits fresh hits.
	now = now.Add(41 * time.Second)
	if ok, _ := lim.allow("198.51.100.1"); !ok {
		t.Fatal("expected bucket reset once window elapsed")
	}
}

func TestClientIPIgnoresForwardingHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/audit", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	if got := clientIP(req); got != "203.0.113.9" {
		t.Errorf("clientIP(host:port) = %q, want 203.0.113.9", got)
	}

	req.RemoteAddr = "203.0.113.9"
	if got := clientIP(req); got != "203.0.113.9" {
		t.Errorf("clientIP(bare host) = %q, want 203.0.113.9", got)
	}
}
