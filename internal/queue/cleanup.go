package queue

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/alexander/bifrost/internal/store"
)

// RetentionWorker deletes old outbox rows once a day. Retention is
// configurable via the settings table.
type RetentionWorker struct {
	store *store.Store
	log   *slog.Logger
}

func NewRetentionWorker(s *store.Store, log *slog.Logger) *RetentionWorker {
	return &RetentionWorker{store: s, log: log.With("component", "retention")}
}

// Run blocks until ctx is cancelled, sweeping once per day.
func (r *RetentionWorker) Run(ctx context.Context) {
	// First sweep right after startup, then every 24h.
	r.sweep(ctx)
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.sweep(ctx)
		}
	}
}

func (r *RetentionWorker) sweep(ctx context.Context) {
	v, err := r.store.GetSetting(ctx, "retention_days")
	if err != nil {
		r.log.Warn("read retention", "err", err)
		return
	}
	days, _ := strconv.Atoi(v)
	if days <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	n, err := r.store.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		r.log.Warn("delete old", "err", err)
		return
	}
	if n > 0 {
		r.log.Info("retention sweep", "deleted", n, "older_than_days", days)
	}
}
