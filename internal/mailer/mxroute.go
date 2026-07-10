package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultMXRouteEndpoint is MXroute's hosted HTTP SMTP API.
// https://docs.mxroute.com/docs/api/smtp-api.html
const DefaultMXRouteEndpoint = "https://smtpapi.mxroute.com/"

// MXRouteConfig holds credentials for MXroute's HTTP SMTP API. Like the SMTP
// Config, it is built fresh per send so admin-UI rotation takes effect at once.
type MXRouteConfig struct {
	Server   string // MXroute server hostname, e.g. "tuesday.mxrouting.net"
	Username string // usually the full email address
	Password string
	Endpoint string // override for tests; defaults to DefaultMXRouteEndpoint
}

type mxRouteRequest struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
}

type mxRouteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// SendMXRoute delivers a message through MXroute's HTTP API.
//
// The API accepts a single recipient per call and has no cc/bcc concept, so we
// fan out one request per recipient across to+cc+bcc. Delivery is at-least-once:
// if one request in a multi-recipient message fails, the whole message is
// retried, which may re-deliver to recipients that already succeeded. For the
// common single-recipient case each send is atomic. HTML in body is supported;
// other fields are plain text.
func SendMXRoute(ctx context.Context, cfg MXRouteConfig, m *Message) (*Result, error) {
	if cfg.Server == "" {
		return nil, &SendError{Permanent: true, Err: errors.New("MXroute API not configured")}
	}
	if m.From == "" {
		return nil, &SendError{Permanent: true, Err: errors.New("from address required")}
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultMXRouteEndpoint
	}
	// The API has one body field; prefer HTML, fall back to text.
	body := m.BodyHTML
	if body == "" {
		body = m.BodyText
	}

	recipients := make([]string, 0, len(m.To)+len(m.Cc)+len(m.Bcc))
	recipients = append(recipients, m.To...)
	recipients = append(recipients, m.Cc...)
	recipients = append(recipients, m.Bcc...)
	if len(recipients) == 0 {
		return nil, &SendError{Permanent: true, Err: errors.New("no recipients")}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var lastResp string
	for _, rcpt := range recipients {
		reqBody, err := json.Marshal(mxRouteRequest{
			Server:   cfg.Server,
			Username: cfg.Username,
			Password: cfg.Password,
			From:     m.From,
			To:       rcpt,
			Subject:  m.Subject,
			Body:     body,
		})
		if err != nil {
			return nil, &SendError{Permanent: true, Err: fmt.Errorf("encode request: %w", err)}
		}
		resp, sendErr := doMXRoute(ctx, client, endpoint, reqBody)
		if sendErr != nil {
			return &Result{Response: resp}, sendErr
		}
		lastResp = resp
	}
	return &Result{Response: orDefault(lastResp, "200 OK")}, nil
}

// doMXRoute issues one API request and classifies the outcome into the same
// transient/permanent scheme the SMTP path uses.
func doMXRoute(ctx context.Context, client *http.Client, endpoint string, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", &SendError{Permanent: true, Err: err}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		// Network / transport failure — retry.
		return "", &SendError{Permanent: false, Err: fmt.Errorf("mxroute request: %w", err)}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var parsed mxRouteResponse
	_ = json.Unmarshal(raw, &parsed)
	respText := strings.TrimSpace(fmt.Sprintf("%d %s", resp.StatusCode, parsed.Message))

	if resp.StatusCode == http.StatusOK && parsed.Success {
		return respText, nil
	}
	// 5xx or rate limiting from the API host — retry.
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return respText, &SendError{Permanent: false, Response: respText, Err: errors.New(respText)}
	}
	// Logical failure (HTTP 200 with success:false, or a 4xx). Auth/config
	// errors won't fix themselves; everything else is treated as transient.
	msg := orDefault(parsed.Message, respText)
	return respText, &SendError{Permanent: mxRoutePermanent(parsed.Message), Response: respText, Err: errors.New(msg)}
}

// mxRoutePermanent flags MXroute error messages that retrying can't fix.
func mxRoutePermanent(msg string) bool {
	m := strings.ToLower(msg)
	for _, marker := range []string{
		"authentication failed",
		"invalid server",
		"missing required field",
		"invalid json",
	} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}
