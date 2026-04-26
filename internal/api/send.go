package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/alexander/bifrost/internal/auth"
	"github.com/alexander/bifrost/internal/store"
	"github.com/alexander/bifrost/internal/templates"
	"github.com/google/uuid"
)

type sendRequest struct {
	Subject      string         `json:"subject"`
	Body         string         `json:"body"`      // plain text
	BodyHTML     string         `json:"body_html"` // optional html
	TemplateID   int64          `json:"template_id"`
	TemplateName string         `json:"template_name"`
	Variables    map[string]any `json:"variables"`
	To           []string       `json:"to,omitempty"`  // requires AllowRequestRecipients
	Cc           []string       `json:"cc,omitempty"`
	Bcc          []string       `json:"bcc,omitempty"`
}

// maxRequestRecipients caps the per-call recipient list across to+cc+bcc to
// stop a leaked key from being used for fan-out spam.
const maxRequestRecipients = 50

type sendResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	key := auth.APIKeyFrom(r.Context())
	if key == nil {
		jsonError(w, http.StatusUnauthorized, "unauthorized", "missing API key")
		return
	}

	// Rate limit before reading the body — cheaper to reject early.
	d, err := s.limiter.Check(r.Context(), key)
	if err != nil {
		s.log.Error("ratelimit check", "err", err)
		jsonError(w, http.StatusInternalServerError, "internal", "rate limit error")
		return
	}
	if !d.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(d.RetryAfter.Seconds())))
		jsonError(w, http.StatusTooManyRequests, "rate_limited",
			"rate limit exceeded for "+d.Bucket+" bucket (limit "+strconv.Itoa(d.Limit)+")")
		return
	}

	var req sendRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)) // 1 MiB
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	to, cc, bcc, err := resolveRecipients(key, req)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid_recipients", err.Error())
		return
	}
	if len(to) == 0 {
		jsonError(w, http.StatusUnprocessableEntity, "no_recipients",
			"this API key has no configured recipients and the request did not provide any")
		return
	}

	subject, bodyText, bodyHTML, err := s.resolveContent(r, key, req)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid_content", err.Error())
		return
	}
	if subject == "" {
		jsonError(w, http.StatusBadRequest, "missing_subject", "subject is required")
		return
	}
	if bodyText == "" && bodyHTML == "" {
		jsonError(w, http.StatusBadRequest, "missing_body", "body or body_html is required")
		return
	}

	messageID := uuid.NewString() + "@bifrost.local"
	msg := &store.OutboxMessage{
		MessageID:    messageID,
		APIKeyID:     key.ID,
		FromAddress:  key.FromAddress,
		FromName:     key.FromName,
		ToAddresses:  to,
		CcAddresses:  cc,
		BccAddresses: bcc,
		Subject:      subject,
		BodyText:     bodyText,
		BodyHTML:     bodyHTML,
	}
	if _, err := s.store.EnqueueOutbox(r.Context(), msg); err != nil {
		s.log.Error("enqueue", "err", err)
		jsonError(w, http.StatusInternalServerError, "enqueue_failed", "could not queue message")
		return
	}
	s.worker.Notify()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(sendResponse{MessageID: messageID, Status: "queued"})
}

// resolveRecipients picks which to/cc/bcc to use for this send.
//
// Semantics:
//   - If the request supplies any of to/cc/bcc, the request takes over all
//     three together (no field-level merge — keeps the privacy model
//     unambiguous: each send either uses key defaults or explicitly
//     enumerates everyone who'll receive the email).
//   - If the request supplies none, the key's defaults are used.
//   - The override path requires the key's AllowRequestRecipients flag.
func resolveRecipients(key *store.APIKey, req sendRequest) (to, cc, bcc []string, err error) {
	to = cleanAddresses(req.To)
	cc = cleanAddresses(req.Cc)
	bcc = cleanAddresses(req.Bcc)
	if len(to) == 0 && len(cc) == 0 && len(bcc) == 0 {
		return key.ToAddresses, key.CcAddresses, key.BccAddresses, nil
	}
	if !key.AllowRequestRecipients {
		return nil, nil, nil, errors.New(
			"this API key does not allow request-supplied recipients; remove to/cc/bcc from the body or enable the flag on the key")
	}
	if len(to)+len(cc)+len(bcc) > maxRequestRecipients {
		return nil, nil, nil, fmt.Errorf("too many recipients (max %d across to+cc+bcc)", maxRequestRecipients)
	}
	for _, addr := range append(append([]string{}, to...), append(cc, bcc...)...) {
		if _, parseErr := mail.ParseAddress(addr); parseErr != nil {
			return nil, nil, nil, fmt.Errorf("invalid address %q: %w", addr, parseErr)
		}
	}
	return to, cc, bcc, nil
}

// cleanAddresses trims whitespace and drops empty entries.
func cleanAddresses(in []string) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

// resolveContent expands the template if one was referenced, or returns the
// inline subject/body unchanged. Returns (subject, text, html, error).
func (s *Server) resolveContent(r *http.Request, key *store.APIKey, req sendRequest) (string, string, string, error) {
	hasTemplate := req.TemplateID != 0 || req.TemplateName != ""
	if !hasTemplate {
		return req.Subject, req.Body, req.BodyHTML, nil
	}

	var tmpl *store.Template
	var err error
	if req.TemplateID != 0 {
		tmpl, err = s.store.GetTemplate(r.Context(), req.TemplateID)
	} else {
		tmpl, err = s.store.GetTemplateByName(r.Context(), req.TemplateName)
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", "", "", errors.New("template not found")
		}
		return "", "", "", err
	}

	if tmpl.Restricted {
		ids, err := s.store.AllowedTemplateIDs(r.Context(), key.ID)
		if err != nil {
			return "", "", "", err
		}
		permitted := false
		for _, id := range ids {
			if id == tmpl.ID {
				permitted = true
				break
			}
		}
		if !permitted {
			return "", "", "", errors.New("this API key is not allowed to use that template")
		}
	}

	rendered, err := templates.Render(tmpl.Subject, tmpl.BodyText, tmpl.BodyHTML, req.Variables)
	if err != nil {
		return "", "", "", err
	}
	subj := rendered.Subject
	if req.Subject != "" {
		// Allow caller to override the subject.
		subj = req.Subject
	}
	return subj, rendered.BodyText, rendered.BodyHTML, nil
}
