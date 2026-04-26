// Package config loads bootstrap settings from the environment.
//
// Operational settings (SMTP credentials, retention) live in SQLite and are
// edited via the admin UI. Only secrets and things needed before the database
// is open belong here.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr     string
	DatabasePath   string
	MasterKey      []byte // 32 bytes, AES-256-GCM
	TLSCert        string
	TLSKey         string
	AdminUsername  string
	AdminPassword  string // plaintext, only used to bootstrap the first admin
	BaseURL        string
	ShutdownGrace  time.Duration
	WorkerInterval time.Duration
	TrustProxy     bool
}

func FromEnv() (*Config, error) {
	cfg := &Config{
		ListenAddr:     getenv("BIFROST_LISTEN_ADDR", ":8080"),
		DatabasePath:   getenv("BIFROST_DB_PATH", "bifrost.db"),
		TLSCert:        os.Getenv("BIFROST_TLS_CERT"),
		TLSKey:         os.Getenv("BIFROST_TLS_KEY"),
		AdminUsername:  os.Getenv("BIFROST_ADMIN_USERNAME"),
		AdminPassword:  os.Getenv("BIFROST_ADMIN_PASSWORD"),
		BaseURL:        getenv("BIFROST_BASE_URL", ""),
		ShutdownGrace:  getDuration("BIFROST_SHUTDOWN_GRACE", 30*time.Second),
		WorkerInterval: getDuration("BIFROST_WORKER_INTERVAL", 1*time.Second),
		TrustProxy:     getBool("BIFROST_TRUST_PROXY", false),
	}

	rawKey := os.Getenv("BIFROST_MASTER_KEY")
	if rawKey == "" {
		return nil, errors.New("BIFROST_MASTER_KEY is required (32-byte hex or base64)")
	}
	key, err := decodeKey(rawKey)
	if err != nil {
		return nil, fmt.Errorf("BIFROST_MASTER_KEY: %w", err)
	}
	cfg.MasterKey = key

	if (cfg.TLSCert == "") != (cfg.TLSKey == "") {
		return nil, errors.New("BIFROST_TLS_CERT and BIFROST_TLS_KEY must both be set or both empty")
	}

	return cfg, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func getBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
