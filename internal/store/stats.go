package store

import (
	"context"
	"time"
)

type StatusCounts struct {
	Pending int
	Sending int
	Sent    int
	Failed  int
	Dead    int
}

func (s *Store) StatusCountsSince(ctx context.Context, since time.Time) (StatusCounts, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM outbox WHERE created_at >= ? GROUP BY status`, since)
	if err != nil {
		return StatusCounts{}, err
	}
	defer rows.Close()
	var c StatusCounts
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return c, err
		}
		switch OutboxStatus(status) {
		case StatusPending:
			c.Pending = n
		case StatusSending:
			c.Sending = n
		case StatusSent:
			c.Sent = n
		case StatusFailed:
			c.Failed = n
		case StatusDead:
			c.Dead = n
		}
	}
	return c, rows.Err()
}

type HourlyVolume struct {
	Hour  time.Time
	Sent  int
	Failed int
}

// VolumeByHour returns up to 24 hourly buckets starting from `since`.
// Used by the dashboard sparkline.
func (s *Store) VolumeByHour(ctx context.Context, since time.Time) ([]HourlyVolume, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT strftime('%Y-%m-%d %H:00:00', created_at) AS hr,
		        SUM(CASE WHEN status = 'sent' THEN 1 ELSE 0 END) AS sent,
		        SUM(CASE WHEN status IN ('failed','dead') THEN 1 ELSE 0 END) AS failed
		 FROM outbox WHERE created_at >= ?
		 GROUP BY hr ORDER BY hr`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HourlyVolume
	for rows.Next() {
		var hrStr string
		var v HourlyVolume
		if err := rows.Scan(&hrStr, &v.Sent, &v.Failed); err != nil {
			return nil, err
		}
		// SQLite returns local time format here; treat as UTC for charting.
		t, err := time.Parse("2006-01-02 15:04:05", hrStr)
		if err == nil {
			v.Hour = t
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
