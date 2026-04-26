package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/alexander/bifrost/internal/crypto"
	"github.com/alexander/bifrost/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName = "bifrost_session"
	sessionTTL        = 7 * 24 * time.Hour
)

type sessionPayload struct {
	AdminID        int64 `json:"a"`
	SessionVersion int64 `json:"v"`
	Expires        int64 `json:"e"` // unix seconds
}

type SessionManager struct {
	cipher *crypto.Cipher
	store  *store.Store
	secure bool
}

func NewSessionManager(c *crypto.Cipher, s *store.Store, secure bool) *SessionManager {
	return &SessionManager{cipher: c, store: s, secure: secure}
}

// Login verifies a username/password against the admins table. On success,
// returns the admin row; the caller calls SetCookie.
func (m *SessionManager) Login(ctx context.Context, username, password string) (*store.Admin, error) {
	a, err := m.store.GetAdminByUsername(ctx, username)
	if err != nil {
		// Always run a bcrypt comparison even on miss to keep timing similar.
		bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalid.iiiiiiiiiiiiiiiiiiiiiiiiii"), []byte(password))
		if errors.Is(err, store.ErrNotFound) {
			return nil, errors.New("invalid credentials")
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid credentials")
	}
	return a, nil
}

// SetCookie issues a signed session cookie for the given admin.
func (m *SessionManager) SetCookie(w http.ResponseWriter, a *store.Admin) error {
	payload := sessionPayload{
		AdminID:        a.ID,
		SessionVersion: a.SessionVersion,
		Expires:        time.Now().Add(sessionTTL).Unix(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	signed := m.cipher.Sign(raw)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    signed,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(payload.Expires, 0),
	})
	return nil
}

// ClearCookie removes the session cookie.
func (m *SessionManager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// adminContextKey holds the authenticated admin in the request context.
const adminContextKey ctxKey = 2

// AdminFrom returns the authenticated admin, or nil.
func AdminFrom(ctx context.Context) *store.Admin {
	v, _ := ctx.Value(adminContextKey).(*store.Admin)
	return v
}

// resolveSession parses + verifies the cookie, checks expiry, and confirms
// session_version still matches the DB row. Returns nil on any failure.
func (m *SessionManager) resolveSession(r *http.Request) *store.Admin {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}
	raw, err := m.cipher.Verify(c.Value)
	if err != nil {
		return nil
	}
	var p sessionPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil
	}
	if time.Now().Unix() > p.Expires {
		return nil
	}
	a, err := m.store.GetAdmin(r.Context(), p.AdminID)
	if err != nil {
		return nil
	}
	if a.SessionVersion != p.SessionVersion {
		return nil
	}
	return a
}

// Require returns middleware that 302-redirects to /admin/login when
// unauthenticated.
func (m *SessionManager) Require(loginPath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a := m.resolveSession(r)
			if a == nil {
				http.Redirect(w, r, loginPath, http.StatusSeeOther)
				return
			}
			ctx := context.WithValue(r.Context(), adminContextKey, a)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Optional returns middleware that attaches the admin to the context if
// present, but does not redirect on miss. Used for public-but-aware pages
// (e.g. /admin/login redirects to dashboard if already logged in).
func (m *SessionManager) Optional() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a := m.resolveSession(r); a != nil {
				ctx := context.WithValue(r.Context(), adminContextKey, a)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HashPassword bcrypts a plaintext password.
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}
