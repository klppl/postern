package admin

import (
	"bytes"
	"net/http"

	"github.com/alexander/bifrost/internal/auth"
)

// pageData wraps per-request context that every template expects.
type pageData struct {
	Title      string
	Active     string // which nav item is current
	AdminName  string
	Flash      string
	FlashKind  string
	Data       any
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, page string, title, active string, data any) {
	t, ok := s.tmpls.pages[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	pd := pageData{
		Title:  title,
		Active: active,
		Data:   data,
	}
	if a := auth.AdminFrom(r.Context()); a != nil {
		pd.AdminName = a.Username
	}
	if msg, kind := s.flash.read(r, w); msg != "" {
		pd.Flash = msg
		pd.FlashKind = kind
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base.html", pd); err != nil {
		s.log.Error("template execute", "page", page, "err", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func (s *Server) renderPartial(w http.ResponseWriter, page string, data any) {
	t, ok := s.tmpls.pages[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "content", data); err != nil {
		s.log.Error("partial execute", "page", page, "err", err)
	}
}
