package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/alexander/bifrost/internal/auth"
)

type settingsData struct {
	RetentionDays int
	AdminUsername string
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	v, _ := s.store.GetSetting(r.Context(), "retention_days")
	days, _ := strconv.Atoi(v)
	a := auth.AdminFrom(r.Context())
	d := settingsData{RetentionDays: days}
	if a != nil {
		d.AdminUsername = a.Username
	}
	s.render(w, r, "settings.html", "Settings", "settings", d)
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if v := strings.TrimSpace(r.FormValue("retention_days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			_ = s.store.SetSetting(r.Context(), "retention_days", strconv.Itoa(n))
		}
	}
	if pw := r.FormValue("new_password"); pw != "" {
		confirm := r.FormValue("new_password_confirm")
		if pw != confirm {
			s.flashError(w, "Passwords do not match.")
			http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
			return
		}
		if len(pw) < 8 {
			s.flashError(w, "Password must be at least 8 characters.")
			http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
			return
		}
		a := auth.AdminFrom(r.Context())
		if a == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		hash, err := auth.HashPassword(pw)
		if err != nil {
			s.flashError(w, "Hash failed: "+err.Error())
			http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
			return
		}
		if err := s.store.UpdateAdminPassword(r.Context(), a.ID, hash); err != nil {
			s.flashError(w, "Update failed: "+err.Error())
			http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
			return
		}
		// Bumping session_version logged us out; clear cookie and redirect.
		s.sessions.ClearCookie(w)
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	s.flashInfo(w, "Settings saved.")
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}
