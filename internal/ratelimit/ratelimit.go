// Package ratelimit implements a per-API-key fixed-window rate limiter
// backed by SQLite. We use fixed windows (not sliding/token-bucket) because:
//
//   - Per-minute / per-hour / per-day limits map directly onto fixed buckets
//   - State is just one row per (key, bucket) — survives restarts for free
//   - Edge-of-window bursts are bounded at 2× and acceptable for email
package ratelimit

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/alexander/bifrost/internal/store"
)

type Limiter struct {
	store *store.Store
	now   func() time.Time
}

func New(s *store.Store) *Limiter {
	return &Limiter{store: s, now: time.Now}
}

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
	Limit      int
	Bucket     string
}

// Check increments per-minute, per-hour, per-day counters and returns the
// first bucket that would be exceeded. A limit value of 0 means unlimited.
//
// All three increments happen atomically inside one transaction so partial
// credit can't be granted on contention.
func (l *Limiter) Check(ctx context.Context, key *store.APIKey) (Decision, error) {
	now := l.now()
	type bucket struct {
		name   string
		limit  int
		window time.Duration
		start  time.Time
	}
	buckets := []bucket{
		{"minute", key.RatePerMinute, time.Minute, now.Truncate(time.Minute)},
		{"hour", key.RatePerHour, time.Hour, now.Truncate(time.Hour)},
		{"day", key.RatePerDay, 24 * time.Hour, time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())},
	}

	var decision Decision
	decision.Allowed = true

	err := l.store.Tx(ctx, func(tx *sql.Tx) error {
		// First pass: read all current counters and compute whether each
		// bucket would be over after a +1.
		for _, b := range buckets {
			if b.limit <= 0 {
				continue
			}
			var count int
			var winStart time.Time
			row := tx.QueryRowContext(ctx,
				`SELECT count, window_start FROM rate_counters
				 WHERE api_key_id = ? AND bucket = ?`,
				key.ID, b.name)
			err := row.Scan(&count, &winStart)
			if errors.Is(err, sql.ErrNoRows) {
				count = 0
				winStart = b.start
			} else if err != nil {
				return err
			}
			if !winStart.Equal(b.start) {
				count = 0
				winStart = b.start
			}
			if count+1 > b.limit {
				decision.Allowed = false
				decision.RetryAfter = winStart.Add(b.window).Sub(now)
				if decision.RetryAfter < time.Second {
					decision.RetryAfter = time.Second
				}
				decision.Limit = b.limit
				decision.Bucket = b.name
				return nil
			}
		}
		// Second pass: increment all buckets (or insert).
		for _, b := range buckets {
			if b.limit <= 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO rate_counters (api_key_id, bucket, window_start, count)
				 VALUES (?, ?, ?, 1)
				 ON CONFLICT(api_key_id, bucket) DO UPDATE SET
				   count = CASE WHEN window_start = excluded.window_start THEN count + 1 ELSE 1 END,
				   window_start = excluded.window_start`,
				key.ID, b.name, b.start); err != nil {
				return err
			}
		}
		return nil
	})
	return decision, err
}

// withClock is exported for tests.
func (l *Limiter) withClock(now func() time.Time) *Limiter {
	return &Limiter{store: l.store, now: now}
}
