// Package admin implements the server-rendered admin UI.
package admin

import (
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/alexander/bifrost/internal/auth"
	"github.com/alexander/bifrost/internal/crypto"
	"github.com/alexander/bifrost/internal/store"
	"github.com/go-chi/chi/v5"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// templateSet holds compiled templates for each page. Each page is parsed
// together with base.html so {{template "content"}} resolves.
type templateSet struct {
	pages map[string]*template.Template
}

func loadTemplates() (*templateSet, error) {
	base := "templates/base.html"
	entries, err := templatesFS.ReadDir("templates")
	if err != nil {
		return nil, err
	}
	set := &templateSet{pages: map[string]*template.Template{}}
	funcs := template.FuncMap{
		"add":     func(a, b int) int { return a + b },
		"sub":     func(a, b int) int { return a - b },
		"hasItem": hasItem,
		"div_ratio": func(n, d int) int {
			if d == 0 {
				return 0
			}
			return n * 100 / d
		},
		"join": func(sep string, ss []string) string {
			out := ""
			for i, s := range ss {
				if i > 0 {
					out += sep
				}
				out += s
			}
			return out
		},
	}
	for _, e := range entries {
		name := e.Name()
		if name == "base.html" {
			continue
		}
		t, err := template.New("base.html").Funcs(funcs).ParseFS(templatesFS, base, "templates/"+name)
		if err != nil {
			return nil, err
		}
		set.pages[name] = t
	}
	return set, nil
}

func hasItem(haystack []int64, needle int64) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

type Server struct {
	store    *store.Store
	cipher   *crypto.Cipher
	sessions *auth.SessionManager
	tmpls    *templateSet
	log      *slog.Logger
	flash    *flashCodec
}

func NewServer(s *store.Store, c *crypto.Cipher, sm *auth.SessionManager, log *slog.Logger) (*Server, error) {
	t, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	return &Server{
		store:    s,
		cipher:   c,
		sessions: sm,
		tmpls:    t,
		log:      log.With("component", "admin"),
		flash:    &flashCodec{cipher: c},
	}, nil
}

// Mount registers all /admin/* routes onto r.
func (s *Server) Mount(r chi.Router) {
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/admin/static/", http.FileServer(http.FS(staticSub))))

	r.Group(func(r chi.Router) {
		r.Use(s.sessions.Optional())
		r.Get("/login", s.getLogin)
		r.Post("/login", s.postLogin)
	})

	r.Group(func(r chi.Router) {
		r.Use(s.sessions.Require("/admin/login"))
		r.Get("/", s.dashboard)
		r.Post("/logout", s.postLogout)
		r.Get("/sending", s.sending)

		r.Get("/keys", s.listKeys)
		r.Get("/keys/new", s.newKey)
		r.Post("/keys", s.createKey)
		r.Get("/keys/{id}", s.editKey)
		r.Post("/keys/{id}", s.updateKey)
		r.Post("/keys/{id}/rotate", s.rotateKey)
		r.Post("/keys/{id}/delete", s.deleteKey)

		r.Get("/templates", s.listTemplates)
		r.Get("/templates/new", s.newTemplate)
		r.Post("/templates", s.createTemplate)
		r.Get("/templates/{id}", s.editTemplate)
		r.Post("/templates/{id}", s.updateTemplate)
		r.Post("/templates/{id}/delete", s.deleteTemplate)
		r.Post("/templates/preview", s.previewTemplate)

		r.Get("/messages", s.listMessages)
		r.Get("/messages/{id}", s.messageDetail)

		r.Get("/smtp", s.getSMTP)
		r.Post("/smtp", s.saveSMTP)
		r.Post("/smtp/test", s.testSMTP)

		r.Get("/settings", s.getSettings)
		r.Post("/settings", s.saveSettings)
	})
}
