package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/alexander/bifrost/internal/crypto"
)

const flashCookie = "bifrost_flash"

type flashPayload struct {
	Message string `json:"m"`
	Kind    string `json:"k"` // "info" | "error"
	Expires int64  `json:"e"`
}

type flashCodec struct {
	cipher *crypto.Cipher
}

func (f *flashCodec) write(w http.ResponseWriter, kind, message string) {
	raw, _ := json.Marshal(flashPayload{
		Message: message,
		Kind:    kind,
		Expires: time.Now().Add(5 * time.Minute).Unix(),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    f.cipher.Sign(raw),
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// read consumes the flash cookie if present and returns (message, kind).
func (f *flashCodec) read(r *http.Request, w http.ResponseWriter) (string, string) {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return "", ""
	}
	// Always clear it.
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		MaxAge:   -1,
	})
	raw, err := f.cipher.Verify(c.Value)
	if err != nil {
		return "", ""
	}
	var p flashPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", ""
	}
	if time.Now().Unix() > p.Expires {
		return "", ""
	}
	return p.Message, p.Kind
}

// flashInfo is a Server convenience.
func (s *Server) flashInfo(w http.ResponseWriter, msg string) {
	s.flash.write(w, "info", msg)
}

func (s *Server) flashError(w http.ResponseWriter, msg string) {
	s.flash.write(w, "error", msg)
}
