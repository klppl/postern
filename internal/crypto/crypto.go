// Package crypto provides AES-256-GCM encryption for secrets at rest and
// HMAC-SHA256 signing for cookies. Both share the master key from env.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
)

type Cipher struct {
	gcm cipher.AEAD
	key []byte
}

func New(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{gcm: gcm, key: key}, nil
}

// Encrypt returns base64(nonce || ciphertext || tag).
func (c *Cipher) Encrypt(plaintext []byte) (string, error) {
	if plaintext == nil {
		return "", nil
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) Decrypt(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	ns := c.gcm.NonceSize()
	if len(raw) < ns+c.gcm.Overhead() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	return c.gcm.Open(nil, nonce, ct, nil)
}

// Sign returns base64(payload).base64(hmac-sha256(payload)).
// Used for session cookies.
func (c *Cipher) Sign(payload []byte) string {
	mac := hmac.New(sha256.New, c.key)
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// Verify checks the signed payload and returns the original bytes.
func (c *Cipher) Verify(signed string) ([]byte, error) {
	for i := 0; i < len(signed); i++ {
		if signed[i] == '.' {
			payload, err := base64.RawURLEncoding.DecodeString(signed[:i])
			if err != nil {
				return nil, err
			}
			gotSig, err := base64.RawURLEncoding.DecodeString(signed[i+1:])
			if err != nil {
				return nil, err
			}
			mac := hmac.New(sha256.New, c.key)
			mac.Write(payload)
			expected := mac.Sum(nil)
			if subtle.ConstantTimeCompare(expected, gotSig) != 1 {
				return nil, errors.New("signature mismatch")
			}
			return payload, nil
		}
	}
	return nil, errors.New("malformed signed value")
}

// HashAPIKey produces a deterministic SHA-256 of the raw key. We use SHA-256
// (not bcrypt) here because API keys are random 32-byte secrets, so brute
// force is infeasible and we need a deterministic lookup index. Bcrypt is
// reserved for the human admin password.
func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

// ConstantTimeEqual compares two strings in constant time.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// RandomBytes generates n cryptographically random bytes.
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// RandomToken returns a URL-safe base64 token of the given byte length.
func RandomToken(n int) (string, error) {
	b, err := RandomBytes(n)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
