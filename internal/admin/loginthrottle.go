package admin

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Admin login brute-force protection. The API send path is rate-limited per
// key, but interactive admin login had no throttle — this bounds online
// password guessing. State is in-memory (per process); a restart clears it,
// which is acceptable for a single-binary self-hosted deployment.
const (
	loginMaxFailures = 5                // failures within loginWindow before lockout
	loginWindow      = 15 * time.Minute // rolling window the failures are counted in
	loginLockout     = 15 * time.Minute // how long a key stays locked once tripped
)

type attemptRecord struct {
	failures    int
	windowStart time.Time
	lockedUntil time.Time
}

type loginThrottle struct {
	mu      sync.Mutex
	records map[string]*attemptRecord
	max     int
	window  time.Duration
	lockout time.Duration
	now     func() time.Time
}

func newLoginThrottle() *loginThrottle {
	return &loginThrottle{
		records: make(map[string]*attemptRecord),
		max:     loginMaxFailures,
		window:  loginWindow,
		lockout: loginLockout,
		now:     time.Now,
	}
}

// retryAfter returns how long the caller must wait before another attempt is
// permitted on any of the given keys. Zero means "go ahead".
func (t *loginThrottle) retryAfter(keys ...string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var worst time.Duration
	for _, k := range keys {
		r := t.records[k]
		if r == nil || !now.Before(r.lockedUntil) {
			continue
		}
		if d := r.lockedUntil.Sub(now); d > worst {
			worst = d
		}
	}
	return worst
}

// fail records a failed attempt against each key, locking a key out once it
// exceeds the failure budget within the rolling window.
func (t *loginThrottle) fail(keys ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.gc(now)
	for _, k := range keys {
		r := t.records[k]
		if r == nil || now.Sub(r.windowStart) > t.window {
			r = &attemptRecord{windowStart: now}
			t.records[k] = r
		}
		r.failures++
		if r.failures >= t.max {
			r.lockedUntil = now.Add(t.lockout)
			r.failures = 0
			r.windowStart = now
		}
	}
}

// reset clears failure state for each key after a successful login.
func (t *loginThrottle) reset(keys ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range keys {
		delete(t.records, k)
	}
}

// gc drops stale records so the map can't grow without bound. Caller holds mu.
func (t *loginThrottle) gc(now time.Time) {
	for k, r := range t.records {
		if now.After(r.lockedUntil) && now.Sub(r.windowStart) > t.window {
			delete(t.records, k)
		}
	}
}

// clientIP returns the connecting IP without the port. Behind a trusted proxy
// the RealIP middleware has already rewritten RemoteAddr to the forwarded IP.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
