package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alexander/bifrost/internal/mailer"
)

type smtpData struct {
	Host           string
	Port           int
	Username       string
	HasPassword    bool
	TLSMode        string
	TestRecipient  string
	TestResult     string
	TestErr        string
}

func (s *Server) loadSMTPData(ctx context.Context) (smtpData, error) {
	settings, err := s.store.AllSettings(ctx)
	if err != nil {
		return smtpData{}, err
	}
	port, _ := strconv.Atoi(settings["smtp_port"])
	d := smtpData{
		Host:        settings["smtp_host"],
		Port:        port,
		Username:    settings["smtp_username"],
		HasPassword: settings["smtp_password_enc"] != "",
		TLSMode:     settings["smtp_tls_mode"],
	}
	if d.TLSMode == "" {
		d.TLSMode = "starttls"
	}
	return d, nil
}

func (s *Server) getSMTP(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadSMTPData(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "smtp.html", "SMTP", "smtp", d)
}

func (s *Server) saveSMTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	host := strings.TrimSpace(r.FormValue("host"))
	port := strings.TrimSpace(r.FormValue("port"))
	username := strings.TrimSpace(r.FormValue("username"))
	tlsMode := r.FormValue("tls_mode")
	password := r.FormValue("password")

	if _, err := strconv.Atoi(port); err != nil || port == "" {
		s.flashError(w, "port must be a number")
		http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	_ = s.store.SetSetting(ctx, "smtp_host", host)
	_ = s.store.SetSetting(ctx, "smtp_port", port)
	_ = s.store.SetSetting(ctx, "smtp_username", username)
	if tlsMode != "" {
		_ = s.store.SetSetting(ctx, "smtp_tls_mode", tlsMode)
	}
	// Empty password means "leave unchanged".
	if password != "" {
		enc, err := s.cipher.Encrypt([]byte(password))
		if err != nil {
			s.flashError(w, "encrypt failed: "+err.Error())
			http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
			return
		}
		_ = s.store.SetSetting(ctx, "smtp_password_enc", enc)
	}
	s.flashInfo(w, "SMTP settings saved.")
	http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
}

func (s *Server) testSMTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	to := strings.TrimSpace(r.FormValue("test_recipient"))
	from := strings.TrimSpace(r.FormValue("test_from"))
	if to == "" {
		s.flashError(w, "test_recipient is required")
		http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
		return
	}
	if from == "" {
		s.flashError(w, "test_from is required (must be allowed by your SMTP account)")
		http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
		return
	}

	settings, err := s.store.AllSettings(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	port, _ := strconv.Atoi(settings["smtp_port"])
	pwEnc := settings["smtp_password_enc"]
	var pw []byte
	if pwEnc != "" {
		pw, err = s.cipher.Decrypt(pwEnc)
		if err != nil {
			s.flashError(w, "decrypt password failed: "+err.Error())
			http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cfg := mailer.Config{
		Host:     settings["smtp_host"],
		Port:     port,
		Username: settings["smtp_username"],
		Password: string(pw),
		TLSMode:  settings["smtp_tls_mode"],
	}
	_, err = mailer.Send(ctx, cfg, &mailer.Message{
		From:     from,
		To:       []string{to},
		Subject:  "Bifrost SMTP test",
		BodyText: "This is a test message from Bifrost. If you got it, SMTP is configured correctly.",
	})
	if err != nil {
		s.flashError(w, "Test failed: "+err.Error())
	} else {
		s.flashInfo(w, "Test message sent.")
	}
	http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
}
