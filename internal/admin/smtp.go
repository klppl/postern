package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alexander/postern/internal/mailer"
)

type smtpData struct {
	DeliveryMode string // "smtp" | "mxroute_api"

	// Regular SMTP
	Host        string
	Port        int
	Username    string
	HasPassword bool
	TLSMode     string

	// MXroute HTTP API
	MXServer      string
	MXUsername    string
	MXHasPassword bool
}

func (s *Server) loadSMTPData(ctx context.Context) (smtpData, error) {
	settings, err := s.store.AllSettings(ctx)
	if err != nil {
		return smtpData{}, err
	}
	port, _ := strconv.Atoi(settings["smtp_port"])
	d := smtpData{
		DeliveryMode:  settings["delivery_mode"],
		Host:          settings["smtp_host"],
		Port:          port,
		Username:      settings["smtp_username"],
		HasPassword:   settings["smtp_password_enc"] != "",
		TLSMode:       settings["smtp_tls_mode"],
		MXServer:      settings["mxroute_server"],
		MXUsername:    settings["mxroute_username"],
		MXHasPassword: settings["mxroute_password_enc"] != "",
	}
	if d.DeliveryMode == "" {
		d.DeliveryMode = "smtp"
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
	ctx := r.Context()

	mode := r.FormValue("delivery_mode")
	if mode != "smtp" && mode != "mxroute_api" {
		mode = "smtp"
	}

	if mode == "mxroute_api" {
		server := strings.TrimSpace(r.FormValue("mx_server"))
		if server == "" {
			s.flashError(w, "MXroute server hostname is required")
			http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
			return
		}
		if err := s.savePassword(ctx, "mxroute_password_enc", r.FormValue("mx_password")); err != nil {
			s.flashError(w, "encrypt failed: "+err.Error())
			http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
			return
		}
		_ = s.store.SetSetting(ctx, "mxroute_server", server)
		_ = s.store.SetSetting(ctx, "mxroute_username", strings.TrimSpace(r.FormValue("mx_username")))
		_ = s.store.SetSetting(ctx, "delivery_mode", mode)
		s.flashInfo(w, "MXroute API settings saved.")
		http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
		return
	}

	host := strings.TrimSpace(r.FormValue("host"))
	port := strings.TrimSpace(r.FormValue("port"))
	username := strings.TrimSpace(r.FormValue("username"))
	tlsMode := r.FormValue("tls_mode")

	if _, err := strconv.Atoi(port); err != nil || port == "" {
		s.flashError(w, "port must be a number")
		http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
		return
	}
	if err := s.savePassword(ctx, "smtp_password_enc", r.FormValue("password")); err != nil {
		s.flashError(w, "encrypt failed: "+err.Error())
		http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
		return
	}
	_ = s.store.SetSetting(ctx, "smtp_host", host)
	_ = s.store.SetSetting(ctx, "smtp_port", port)
	_ = s.store.SetSetting(ctx, "smtp_username", username)
	if tlsMode != "" {
		_ = s.store.SetSetting(ctx, "smtp_tls_mode", tlsMode)
	}
	_ = s.store.SetSetting(ctx, "delivery_mode", mode)
	s.flashInfo(w, "SMTP settings saved.")
	http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
}

// savePassword encrypts and stores a password under key, unless the submitted
// value is empty (which means "leave the existing secret unchanged").
func (s *Server) savePassword(ctx context.Context, key, plain string) error {
	if plain == "" {
		return nil
	}
	enc, err := s.cipher.Encrypt([]byte(plain))
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, key, enc)
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

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	msg := &mailer.Message{
		From:     from,
		To:       []string{to},
		Subject:  "Postern delivery test",
		BodyText: "This is a test message from Postern. If you got it, delivery is configured correctly.",
	}

	provider := "smtp"
	var res *mailer.Result
	if settings["delivery_mode"] == "mxroute_api" {
		provider = "mxroute_api"
		cfg := mailer.MXRouteConfig{
			Server:   settings["mxroute_server"],
			Username: settings["mxroute_username"],
		}
		if pwEnc := settings["mxroute_password_enc"]; pwEnc != "" {
			pw, decErr := s.cipher.Decrypt(pwEnc)
			if decErr != nil {
				s.flashError(w, "decrypt password failed: "+decErr.Error())
				http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
				return
			}
			cfg.Password = string(pw)
		}
		res, err = mailer.SendMXRoute(ctx, cfg, msg)
	} else {
		port, _ := strconv.Atoi(settings["smtp_port"])
		cfg := mailer.Config{
			Host:     settings["smtp_host"],
			Port:     port,
			Username: settings["smtp_username"],
			TLSMode:  settings["smtp_tls_mode"],
		}
		if pwEnc := settings["smtp_password_enc"]; pwEnc != "" {
			pw, decErr := s.cipher.Decrypt(pwEnc)
			if decErr != nil {
				s.flashError(w, "decrypt password failed: "+decErr.Error())
				http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
				return
			}
			cfg.Password = string(pw)
		}
		res, err = mailer.Send(ctx, cfg, msg)
	}

	response := ""
	if res != nil {
		response = res.Response
	}

	if err != nil {
		// Surface the server's response as well as the error — the error
		// string alone is sometimes empty (e.g. a nil-wrapped SendError).
		detail := err.Error()
		if detail == "" {
			detail = response
		}
		if detail == "" {
			detail = "unknown error"
		}
		s.log.Warn("delivery test failed", "provider", provider, "from", from, "to", to, "response", response, "err", err)
		s.flashError(w, "Test to "+to+" failed via "+provider+": "+detail)
	} else {
		s.log.Info("delivery test sent", "provider", provider, "from", from, "to", to, "response", response)
		msg := "Test message sent to " + to + " via " + provider + "."
		if response != "" {
			msg += " Server responded: " + response
		}
		s.flashInfo(w, msg)
	}
	http.Redirect(w, r, "/admin/smtp", http.StatusSeeOther)
}
