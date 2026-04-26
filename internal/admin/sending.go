package admin

import "net/http"

type sendingData struct {
	BaseURL string
}

// sending renders the "how to send from your app" reference page.
func (s *Server) sending(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "sending.html", "Sending", "sending", sendingData{
		BaseURL: deriveBaseURL(r),
	})
}

// deriveBaseURL reconstructs the public origin for code-snippet rendering.
// Honors X-Forwarded-Proto from a trusting reverse proxy; defaults to the
// scheme the request actually arrived on.
func deriveBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
		scheme = v
	}
	host := r.Host
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
		host = v
	}
	return scheme + "://" + host
}
