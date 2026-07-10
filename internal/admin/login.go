package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/alexander/postern/internal/auth"
)

type loginData struct {
	Username string
	Error    string
}

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	if auth.AdminFrom(r.Context()) != nil {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login.html", "Sign in", "", loginData{})
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	// Throttle failed logins per source IP. We deliberately key on IP rather
	// than username: a username-scoped lockout would let anyone lock the admin
	// out on demand (availability DoS). Per-IP self-limits a brute-forcer's
	// own throughput without ever locking out the legitimate operator.
	ipKey := "ip:" + clientIP(r)
	if d := s.logins.retryAfter(ipKey); d > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(d.Round(time.Second).Seconds())))
		s.render(w, r, "login.html", "Sign in", "", loginData{
			Username: username,
			Error:    "Too many failed attempts. Try again in " + d.Round(time.Second).String() + ".",
		})
		return
	}

	a, err := s.sessions.Login(r.Context(), username, password)
	if err != nil {
		s.logins.fail(ipKey)
		s.render(w, r, "login.html", "Sign in", "", loginData{Username: username, Error: "Invalid credentials"})
		return
	}
	s.logins.reset(ipKey)
	if err := s.sessions.SetCookie(w, a); err != nil {
		s.log.Error("set cookie", "err", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.ClearCookie(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
