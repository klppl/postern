package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type OutboxStatus string

const (
	StatusPending OutboxStatus = "pending"
	StatusSending OutboxStatus = "sending"
	StatusSent    OutboxStatus = "sent"
	StatusFailed  OutboxStatus = "failed"
	StatusDead    OutboxStatus = "dead"
)

type OutboxMessage struct {
	ID            int64
	MessageID     string
	APIKeyID      int64
	FromAddress   string
	FromName      string
	ToAddresses   []string
	CcAddresses   []string
	BccAddresses  []string
	Subject       string
	BodyText      string
	BodyHTML      string
	Status        OutboxStatus
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	SMTPResponse  string
	CreatedAt     time.Time
	SentAt        time.Time
}

func scanOutbox(s rowScanner) (*OutboxMessage, error) {
	var m OutboxMessage
	var to, cc, bcc string
	var sentAt sql.NullTime
	if err := s.Scan(
		&m.ID, &m.MessageID, &m.APIKeyID,
		&m.FromAddress, &m.FromName,
		&to, &cc, &bcc,
		&m.Subject, &m.BodyText, &m.BodyHTML,
		&m.Status, &m.Attempts, &m.NextAttemptAt,
		&m.LastError, &m.SMTPResponse,
		&m.CreatedAt, &sentAt,
	); err != nil {
		return nil, err
	}
	m.SentAt = nullableTime(sentAt)
	json.Unmarshal([]byte(to), &m.ToAddresses)
	json.Unmarshal([]byte(cc), &m.CcAddresses)
	json.Unmarshal([]byte(bcc), &m.BccAddresses)
	return &m, nil
}

const outboxCols = `id, message_id, api_key_id, from_address, from_name,
		to_addresses, cc_addresses, bcc_addresses,
		subject, body_text, body_html,
		status, attempts, next_attempt_at,
		last_error, smtp_response,
		created_at, sent_at`

func (s *Store) EnqueueOutbox(ctx context.Context, m *OutboxMessage) (int64, error) {
	to, _ := json.Marshal(stringsOrEmpty(m.ToAddresses))
	cc, _ := json.Marshal(stringsOrEmpty(m.CcAddresses))
	bcc, _ := json.Marshal(stringsOrEmpty(m.BccAddresses))
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO outbox
		 (message_id, api_key_id, from_address, from_name,
		  to_addresses, cc_addresses, bcc_addresses,
		  subject, body_text, body_html,
		  status, next_attempt_at)
		 VALUES (?,?,?,?, ?,?,?, ?,?,?, 'pending', CURRENT_TIMESTAMP)`,
		m.MessageID, m.APIKeyID, m.FromAddress, m.FromName,
		string(to), string(cc), string(bcc),
		m.Subject, m.BodyText, m.BodyHTML,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ClaimNext atomically picks the next due message, marks it 'sending', and
// returns it. Returns ErrNotFound when nothing is due.
func (s *Store) ClaimNext(ctx context.Context, now time.Time) (*OutboxMessage, error) {
	var claimed *OutboxMessage
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx,
			`SELECT `+outboxCols+` FROM outbox
			 WHERE status = 'pending' AND next_attempt_at <= ?
			 ORDER BY next_attempt_at ASC
			 LIMIT 1`, now)
		m, err := scanOutbox(row)
		if err != nil {
			return wrapNotFound(err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE outbox SET status = 'sending', attempts = attempts + 1 WHERE id = ?`,
			m.ID); err != nil {
			return err
		}
		m.Status = StatusSending
		m.Attempts++
		claimed = m
		return nil
	})
	return claimed, err
}

// MarkSent finalizes a successful delivery and records the SMTP response.
func (s *Store) MarkSent(ctx context.Context, id int64, smtpResponse string) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE outbox SET status = 'sent', sent_at = CURRENT_TIMESTAMP, smtp_response = ?, last_error = '' WHERE id = ?`,
			smtpResponse, id); err != nil {
			return err
		}
		return logAttempt(ctx, tx, id, smtpResponse, "")
	})
}

// MarkRetry schedules the next attempt or dead-letters on the final try.
func (s *Store) MarkRetry(ctx context.Context, id int64, nextAt time.Time, smtpResponse, errMsg string) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE outbox SET status = 'pending', next_attempt_at = ?, smtp_response = ?, last_error = ? WHERE id = ?`,
			nextAt, smtpResponse, errMsg, id); err != nil {
			return err
		}
		return logAttempt(ctx, tx, id, smtpResponse, errMsg)
	})
}

// MarkDead transitions the message into a permanent failure state.
func (s *Store) MarkDead(ctx context.Context, id int64, smtpResponse, errMsg string) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE outbox SET status = 'dead', smtp_response = ?, last_error = ? WHERE id = ?`,
			smtpResponse, errMsg, id); err != nil {
			return err
		}
		return logAttempt(ctx, tx, id, smtpResponse, errMsg)
	})
}

// ResetStuckSending finds messages stuck in 'sending' (e.g. crash mid-send)
// and returns them to 'pending' on startup.
func (s *Store) ResetStuckSending(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE outbox SET status = 'pending', next_attempt_at = CURRENT_TIMESTAMP WHERE status = 'sending'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func logAttempt(ctx context.Context, tx *sql.Tx, outboxID int64, smtpResp, errMsg string) error {
	var attemptNo int
	if err := tx.QueryRowContext(ctx, `SELECT attempts FROM outbox WHERE id = ?`, outboxID).Scan(&attemptNo); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO outbox_attempts (outbox_id, attempt_no, smtp_response, error) VALUES (?, ?, ?, ?)`,
		outboxID, attemptNo, smtpResp, errMsg)
	return err
}

type OutboxAttempt struct {
	ID           int64
	AttemptNo    int
	SMTPResponse string
	Error        string
	AttemptedAt  time.Time
}

func (s *Store) ListAttempts(ctx context.Context, outboxID int64) ([]*OutboxAttempt, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, attempt_no, smtp_response, error, attempted_at
		 FROM outbox_attempts WHERE outbox_id = ? ORDER BY attempt_no`, outboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OutboxAttempt
	for rows.Next() {
		var a OutboxAttempt
		if err := rows.Scan(&a.ID, &a.AttemptNo, &a.SMTPResponse, &a.Error, &a.AttemptedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// MessageFilter narrows the message log query.
type MessageFilter struct {
	APIKeyID  int64  // 0 = all
	Status    string // empty = all
	Recipient string // substring match in to/cc/bcc JSON
	Since     time.Time
	Until     time.Time
	Limit     int
	Offset    int
}

func (s *Store) ListMessages(ctx context.Context, f MessageFilter) ([]*OutboxMessage, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.APIKeyID > 0 {
		where = append(where, "api_key_id = ?")
		args = append(args, f.APIKeyID)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	if f.Recipient != "" {
		where = append(where, "(to_addresses LIKE ? OR cc_addresses LIKE ? OR bcc_addresses LIKE ?)")
		like := "%" + f.Recipient + "%"
		args = append(args, like, like, like)
	}
	if !f.Since.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, f.Since)
	}
	if !f.Until.IsZero() {
		where = append(where, "created_at <= ?")
		args = append(args, f.Until)
	}
	whereSQL := joinWhere(where)
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args2 := append(append([]any{}, args...), limit, f.Offset)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+outboxCols+` FROM outbox `+whereSQL+` ORDER BY id DESC LIMIT ? OFFSET ?`, args2...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var msgs []*OutboxMessage
	for rows.Next() {
		m, err := scanOutbox(rows)
		if err != nil {
			return nil, 0, err
		}
		msgs = append(msgs, m)
	}
	return msgs, total, rows.Err()
}

func (s *Store) GetMessage(ctx context.Context, id int64) (*OutboxMessage, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+outboxCols+` FROM outbox WHERE id = ?`, id)
	m, err := scanOutbox(row)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	return m, nil
}

func (s *Store) GetMessageByMessageID(ctx context.Context, messageID string) (*OutboxMessage, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+outboxCols+` FROM outbox WHERE message_id = ?`, messageID)
	m, err := scanOutbox(row)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	return m, nil
}

// DeleteOlderThan removes outbox rows older than cutoff (retention cleanup).
func (s *Store) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM outbox WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func joinWhere(parts []string) string {
	out := "WHERE "
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}
