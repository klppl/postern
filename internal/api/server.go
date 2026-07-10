// Package api implements the /api/v1/* HTTP handlers.
package api

import (
	"log/slog"
	"net/http"

	"github.com/alexander/postern/internal/auth"
	"github.com/alexander/postern/internal/queue"
	"github.com/alexander/postern/internal/ratelimit"
	"github.com/alexander/postern/internal/store"
	"github.com/go-chi/chi/v5"
)

type Server struct {
	store   *store.Store
	limiter *ratelimit.Limiter
	worker  *queue.Worker
	log     *slog.Logger
}

func NewServer(s *store.Store, l *ratelimit.Limiter, w *queue.Worker, log *slog.Logger) *Server {
	return &Server{store: s, limiter: l, worker: w, log: log.With("component", "api")}
}

// Mount registers all /api/v1/* routes onto r.
func (s *Server) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.APIKeyAuth(s.store))
		r.Post("/send", s.handleSend)
	})
}

// jsonError writes a structured error response.
func jsonError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + code + `","message":"` + jsonEscape(message) + `"}`))
}

// jsonEscape escapes a few characters so we can splice into a literal.
// We don't import encoding/json here for the trivial error envelope.
func jsonEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
