package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
)

// decodeKey accepts a 32-byte master key as either hex (64 chars) or
// base64 (with or without padding). Both are common ways to ship secrets
// through env-var managers.
func decodeKey(s string) ([]byte, error) {
	if len(s) == 64 {
		b, err := hex.DecodeString(s)
		if err == nil && len(b) == 32 {
			return b, nil
		}
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, errors.New("must decode to exactly 32 bytes (hex or base64)")
}
