package admin

import "net/http"

type sendingData struct {
	BaseURL string
}

// sending renders the "how to send from your app" reference page.
func (s *Server) sending(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "sending.html", "Sending", "sending", sendingData{
		BaseURL: s.deriveBaseURL(r),
	})
}

// deriveBaseURL reconstructs the public origin for code-snippet rendering.
// X-Forwarded-Proto / X-Forwarded-Host are only honored when the operator has
// declared a trusted reverse proxy; otherwise they are attacker-controlled and
// we fall back to the scheme/host the request actually arrived on.
func (s *Server) deriveBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if s.trustProxy {
		if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
			scheme = v
		}
		if v := r.Header.Get("X-Forwarded-Host"); v != "" {
			host = v
		}
	}
	return scheme + "://" + host
}
