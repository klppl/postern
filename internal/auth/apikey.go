// Package auth handles API-key authentication for /api/v1/* and admin
// session management for /admin/*.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/alexander/postern/internal/crypto"
	"github.com/alexander/postern/internal/store"
)

// rawKeyPrefix is the fixed prefix of every issued raw API key. Makes them
// trivially recognizable in logs and code.
const rawKeyPrefix = "pn_"

// IssueAPIKey returns (rawKey, hash, displayPrefix) for a new key.
// Raw key is pn_<32 random url-safe bytes>; only displayed at creation time.
func IssueAPIKey() (raw, hash, prefix string, err error) {
	tok, err := crypto.RandomToken(32)
	if err != nil {
		return "", "", "", err
	}
	raw = rawKeyPrefix + tok
	hash = crypto.HashAPIKey(raw)
	// First 8 chars after the prefix is enough to disambiguate visually.
	prefix = raw[:min(len(raw), len(rawKeyPrefix)+8)]
	return raw, hash, prefix, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type ctxKey int

const apiKeyContextKey ctxKey = 1

// APIKeyFrom extracts the authenticated key from the request context.
func APIKeyFrom(ctx context.Context) *store.APIKey {
	v, _ := ctx.Value(apiKeyContextKey).(*store.APIKey)
	return v
}

// APIKeyAuth returns middleware that validates `Authorization: Bearer <key>`
// against the api_keys table and stashes the resolved key in the context.
func APIKeyAuth(s *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerToken(r)
			if !ok || !strings.HasPrefix(raw, rawKeyPrefix) {
				writeAuthError(w, "missing or malformed Authorization header")
				return
			}
			hash := crypto.HashAPIKey(raw)
			key, err := s.GetAPIKeyByHash(r.Context(), hash)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					writeAuthError(w, "invalid API key")
					return
				}
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if key.Disabled {
				writeAuthError(w, "API key is disabled")
				return
			}
			ctx := context.WithValue(r.Context(), apiKeyContextKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(h[len(prefix):]), true
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="postern"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}
