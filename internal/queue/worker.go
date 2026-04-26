// Package queue implements the outbox worker.
//
// Design:
//   - One goroutine, one message at a time. SMTP delivery is the bottleneck
//     and a small VPS doesn't benefit from parallel SMTP fan-out to a single
//     relay. If we ever do, we fan out below the worker, not above.
//   - Wake on a signal channel from the API handler (insert), with a
//     fallback ticker for retries (whose due time may be in the future).
//   - SMTP config is read from settings on every send so the admin UI can
//     rotate it without restart.
package queue

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/alexander/bifrost/internal/crypto"
	"github.com/alexander/bifrost/internal/mailer"
	"github.com/alexander/bifrost/internal/store"
)

type Worker struct {
	store    *store.Store
	cipher   *crypto.Cipher
	log      *slog.Logger
	wake     chan struct{}
	interval time.Duration
}

func NewWorker(s *store.Store, c *crypto.Cipher, log *slog.Logger, interval time.Duration) *Worker {
	if interval <= 0 {
		interval = time.Second
	}
	return &Worker{
		store:    s,
		cipher:   c,
		log:      log.With("component", "queue"),
		wake:     make(chan struct{}, 1),
		interval: interval,
	}
}

// Notify wakes the worker. Non-blocking; coalesces multiple notifications
// into a single processing pass.
func (w *Worker) Notify() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run blocks until ctx is cancelled. Resets stuck 'sending' messages on
// startup, then loops processing due messages.
func (w *Worker) Run(ctx context.Context) {
	if n, err := w.store.ResetStuckSending(ctx); err != nil {
		w.log.Warn("reset stuck", "err", err)
	} else if n > 0 {
		w.log.Info("reset stuck messages", "count", n)
	}

	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		w.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-w.wake:
		}
	}
}

func (w *Worker) drain(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		m, err := w.store.ClaimNext(ctx, time.Now())
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return
			}
			w.log.Error("claim", "err", err)
			return
		}
		w.deliver(ctx, m)
	}
}

func (w *Worker) deliver(ctx context.Context, m *store.OutboxMessage) {
	cfg, err := w.smtpConfig(ctx)
	if err != nil {
		// SMTP misconfigured: defer the retry by one minute and let the
		// admin fix it. Don't mark dead — the message is fine.
		w.log.Warn("smtp config", "err", err, "message_id", m.MessageID)
		next := time.Now().Add(time.Minute)
		_ = w.store.MarkRetry(ctx, m.ID, next, "", "smtp not configured")
		return
	}

	res, sendErr := mailer.Send(ctx, cfg, &mailer.Message{
		From:      m.FromAddress,
		FromName:  m.FromName,
		To:        m.ToAddresses,
		Cc:        m.CcAddresses,
		Bcc:       m.BccAddresses,
		Subject:   m.Subject,
		BodyText:  m.BodyText,
		BodyHTML:  m.BodyHTML,
		MessageID: m.MessageID,
	})
	if sendErr == nil {
		resp := ""
		if res != nil {
			resp = res.Response
		}
		if err := w.store.MarkSent(ctx, m.ID, resp); err != nil {
			w.log.Error("mark sent", "err", err, "message_id", m.MessageID)
		}
		w.log.Info("sent", "message_id", m.MessageID, "to", m.ToAddresses)
		return
	}

	// Failure path. Pull classification + SMTP response.
	var se *mailer.SendError
	permanent := false
	resp := ""
	errMsg := sendErr.Error()
	if errors.As(sendErr, &se) {
		permanent = se.Permanent
		resp = se.Response
	}
	if res != nil && resp == "" {
		resp = res.Response
	}

	if permanent {
		w.log.Warn("permanent failure", "message_id", m.MessageID, "err", errMsg)
		_ = w.store.MarkDead(ctx, m.ID, resp, errMsg)
		return
	}

	delay, ok := NextDelay(m.Attempts)
	if !ok {
		w.log.Warn("max attempts reached", "message_id", m.MessageID)
		_ = w.store.MarkDead(ctx, m.ID, resp, errMsg)
		return
	}
	next := time.Now().Add(delay)
	w.log.Info("transient failure", "message_id", m.MessageID, "attempts", m.Attempts, "next_in", delay, "err", errMsg)
	_ = w.store.MarkRetry(ctx, m.ID, next, resp, errMsg)
}

func (w *Worker) smtpConfig(ctx context.Context) (mailer.Config, error) {
	settings, err := w.store.AllSettings(ctx)
	if err != nil {
		return mailer.Config{}, err
	}
	host := settings["smtp_host"]
	if host == "" {
		return mailer.Config{}, errors.New("smtp_host empty")
	}
	port, _ := strconv.Atoi(settings["smtp_port"])
	if port == 0 {
		port = 587
	}
	pwEnc := settings["smtp_password_enc"]
	var pw []byte
	if pwEnc != "" {
		pw, err = w.cipher.Decrypt(pwEnc)
		if err != nil {
			return mailer.Config{}, err
		}
	}
	tlsMode := settings["smtp_tls_mode"]
	if tlsMode == "" {
		tlsMode = "starttls"
	}
	return mailer.Config{
		Host:     host,
		Port:     port,
		Username: settings["smtp_username"],
		Password: string(pw),
		TLSMode:  tlsMode,
	}, nil
}
