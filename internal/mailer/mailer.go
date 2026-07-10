// Package mailer wraps go-mail to deliver outbox messages over SMTP.
//
// Failures are classified into transient (retry) vs permanent (dead-letter)
// so the worker can decide what to do. We treat all SMTP 4xx and any
// network error as transient; SMTP 5xx is permanent.
package mailer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wneessen/go-mail"
)

// Config holds SMTP credentials. The mailer is constructed fresh per send
// so credential rotation in the admin UI takes effect immediately.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	TLSMode  string // "none" | "starttls" | "tls"
}

// Message is what the worker hands to Send.
type Message struct {
	From         string
	FromName     string
	To           []string
	Cc           []string
	Bcc          []string
	Subject      string
	BodyText     string
	BodyHTML     string
	MessageID    string
	UserAgent    string
}

// Result holds the SMTP server response, used for the audit trail.
type Result struct {
	Response string
}

// SendError carries failure classification. Permanent errors should not
// be retried.
type SendError struct {
	Permanent bool
	Response  string
	Err       error
}

func (e *SendError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *SendError) Unwrap() error { return e.Err }

// Send dials the SMTP server and delivers one message. Caller passes in
// SMTP config; we don't keep state.
func Send(ctx context.Context, cfg Config, m *Message) (*Result, error) {
	if cfg.Host == "" {
		return nil, &SendError{Permanent: true, Err: errors.New("SMTP not configured")}
	}

	msg := mail.NewMsg()
	if m.FromName != "" {
		if err := msg.FromFormat(m.FromName, m.From); err != nil {
			return nil, &SendError{Permanent: true, Err: fmt.Errorf("from: %w", err)}
		}
	} else {
		if err := msg.From(m.From); err != nil {
			return nil, &SendError{Permanent: true, Err: fmt.Errorf("from: %w", err)}
		}
	}
	if len(m.To) > 0 {
		if err := msg.To(m.To...); err != nil {
			return nil, &SendError{Permanent: true, Err: fmt.Errorf("to: %w", err)}
		}
	}
	if len(m.Cc) > 0 {
		if err := msg.Cc(m.Cc...); err != nil {
			return nil, &SendError{Permanent: true, Err: fmt.Errorf("cc: %w", err)}
		}
	}
	if len(m.Bcc) > 0 {
		if err := msg.Bcc(m.Bcc...); err != nil {
			return nil, &SendError{Permanent: true, Err: fmt.Errorf("bcc: %w", err)}
		}
	}
	msg.Subject(m.Subject)
	if m.MessageID != "" {
		msg.SetMessageIDWithValue(m.MessageID)
	}
	msg.SetUserAgent(orDefault(m.UserAgent, "postern"))
	msg.SetDate()

	switch {
	case m.BodyText != "" && m.BodyHTML != "":
		msg.SetBodyString(mail.TypeTextPlain, m.BodyText)
		msg.AddAlternativeString(mail.TypeTextHTML, m.BodyHTML)
	case m.BodyHTML != "":
		msg.SetBodyString(mail.TypeTextHTML, m.BodyHTML)
	default:
		msg.SetBodyString(mail.TypeTextPlain, m.BodyText)
	}

	opts := []mail.Option{
		mail.WithPort(cfg.Port),
		mail.WithTimeout(30 * time.Second),
	}
	if cfg.Username != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(cfg.Username),
			mail.WithPassword(cfg.Password),
		)
	}
	switch cfg.TLSMode {
	case "tls":
		opts = append(opts, mail.WithSSLPort(false))
	case "starttls":
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	case "none":
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	default:
		opts = append(opts, mail.WithTLSPolicy(mail.TLSOpportunistic))
	}

	client, err := mail.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, &SendError{Permanent: true, Err: fmt.Errorf("client: %w", err)}
	}

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return &Result{Response: extractSMTPResponse(err)}, classify(err)
	}
	return &Result{Response: "250 OK"}, nil
}

// classify decides whether an error is transient. The heuristic is:
//   - DNS / network / context errors → transient
//   - SMTP 4xx → transient
//   - SMTP 5xx → permanent
//   - Anything else → transient (safer to retry than to dead-letter)
func classify(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// SMTP responses look like "550 5.1.1 user unknown".
	if i := strings.Index(msg, " "); i == 3 {
		switch msg[0] {
		case '5':
			return &SendError{Permanent: true, Response: msg, Err: err}
		case '4':
			return &SendError{Permanent: false, Response: msg, Err: err}
		}
	}
	for _, marker := range []string{" 5.", "550 ", "551 ", "552 ", "553 ", "554 "} {
		if strings.Contains(msg, marker) {
			return &SendError{Permanent: true, Response: msg, Err: err}
		}
	}
	return &SendError{Permanent: false, Response: msg, Err: err}
}

func extractSMTPResponse(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
