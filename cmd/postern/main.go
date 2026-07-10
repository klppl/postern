// Postern — self-hosted email gateway.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexander/postern/internal/admin"
	"github.com/alexander/postern/internal/api"
	"github.com/alexander/postern/internal/auth"
	"github.com/alexander/postern/internal/config"
	"github.com/alexander/postern/internal/crypto"
	"github.com/alexander/postern/internal/queue"
	"github.com/alexander/postern/internal/ratelimit"
	"github.com/alexander/postern/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "postern:", err)
		os.Exit(1)
	}
}

func run() error {
	envFile := os.Getenv("POSTERN_ENV_FILE")
	if envFile == "" {
		envFile = ".env"
	}
	if err := config.LoadDotEnv(envFile); err != nil {
		return fmt.Errorf("load %s: %w", envFile, err)
	}

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()
	log.Info("store opened", "path", cfg.DatabasePath)

	cipher, err := crypto.New(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("init cipher: %w", err)
	}

	if err := bootstrapAdmin(context.Background(), st, cfg, log); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	limiter := ratelimit.New(st)
	worker := queue.NewWorker(st, cipher, log, cfg.WorkerInterval)
	retention := queue.NewRetentionWorker(st, log)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go worker.Run(ctx)
	go retention.Run(ctx)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// Only honor client-supplied X-Forwarded-For / X-Real-IP when we're
	// explicitly told we sit behind a trusted proxy. Otherwise these headers
	// are attacker-controlled and would poison audit logs.
	if cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(slogMiddleware(log))
	r.Use(middleware.Recoverer)

	apiSrv := api.NewServer(st, limiter, worker, log)
	r.Route("/api/v1", func(sub chi.Router) { apiSrv.Mount(sub) })

	useTLS := cfg.TLSCert != "" && cfg.TLSKey != ""
	// Mark cookies Secure when the app terminates TLS itself, or when the
	// operator declares TLS is terminated upstream (reverse proxy). Without
	// this, a proxy-terminated deployment would ship session cookies without
	// the Secure attribute.
	secureCookies := useTLS || cfg.SecureCookies
	sessions := auth.NewSessionManager(cipher, st, secureCookies)
	adminSrv, err := admin.NewServer(st, cipher, sessions, log, secureCookies, cfg.TrustProxy)
	if err != nil {
		return fmt.Errorf("init admin: %w", err)
	}
	r.Route("/admin", func(sub chi.Router) { adminSrv.Mount(sub) })

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusFound)
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if useTLS {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "tls", useTLS)
		var err error
		if useTLS {
			err = srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case err := <-errCh:
		log.Error("server error", "err", err)
		cancel()
	}

	shutdownCtx, sCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer sCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
	log.Info("stopped")
	return nil
}

// bootstrapAdmin creates the first admin user from env vars if no admin
// rows exist. After that the env vars are ignored.
func bootstrapAdmin(ctx context.Context, st *store.Store, cfg *config.Config, log *slog.Logger) error {
	n, err := st.CountAdmins(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if cfg.AdminUsername == "" || cfg.AdminPassword == "" {
		log.Warn("no admins exist and POSTERN_ADMIN_USERNAME/PASSWORD not set — admin UI will be inaccessible")
		return nil
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	if _, err := st.CreateAdmin(ctx, cfg.AdminUsername, hash); err != nil {
		return err
	}
	log.Info("bootstrapped admin user", "username", cfg.AdminUsername)
	return nil
}

// slogMiddleware logs each request at info level once it completes.
func slogMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"dur", time.Since(start).Round(time.Millisecond),
				"ip", r.RemoteAddr,
			)
		})
	}
}
