package admin

import (
	"net/http"

	"github.com/alexander/bifrost/internal/auth"
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

	a, err := s.sessions.Login(r.Context(), username, password)
	if err != nil {
		s.render(w, r, "login.html", "Sign in", "", loginData{Username: username, Error: "Invalid credentials"})
		return
	}
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
