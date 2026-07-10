# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Postern is a self-hosted email gateway: microservices POST to `/api/v1/send`, the message is queued in a SQLite outbox, and an in-process worker delivers it through a configured SMTP relay. Single Go binary, no CGO (`modernc.org/sqlite`), no external broker. Everything (migrations, admin HTML templates, static assets) is embedded via `go:embed` — nothing external is read at runtime except `.env` and the DB file.

## Commands

```bash
go build -o postern ./cmd/postern      # build
go test ./...                          # all tests
go test ./internal/queue/ -run TestBackoff   # single package / single test
go vet ./...
```

Run locally: copy `.env.example` to `.env`, set `POSTERN_MASTER_KEY` (`openssl rand -hex 32`) and admin creds, then `./postern` and open `http://localhost:8080/admin/`. `.env` is loaded at startup by `internal/config/dotenv.go` (custom parser, not godotenv); real environment variables always override it.

Release build: `go build -trimpath -ldflags="-s -w" -o postern ./cmd/postern`.

## Architecture

Request path: `cmd/postern/main.go` wires everything and mounts two chi sub-routers — `internal/api` (`/api/v1/*`, JSON, API-key auth) and `internal/admin` (`/admin/*`, server-rendered HTML, signed-cookie sessions). Both talk only to `internal/store`; nothing else touches `*sql.DB` directly.

Send flow: `api/send.go` authenticates the key → rate-limits → resolves recipients and template content → inserts a row into the `outbox` table → calls `worker.Notify()`. The queue worker (`internal/queue/worker.go`) is a single goroutine that drains due messages one at a time (SMTP to one relay doesn't benefit from fan-out), waking on the notify channel or a fallback ticker (`POSTERN_WORKER_INTERVAL`) for retries. Message states: `pending → sending → sent | failed (retry) | dead`. `mailer` classifies SMTP errors: 4xx/network = transient (retried on backoff schedule 1m→5m→15m→1h→6h→24h, dead after 6 attempts), 5xx = permanent (dead-letters immediately). Messages stuck in `sending` at startup are reset to `pending`.

Key cross-cutting pieces:

- **`internal/store`** — all persistence; typed query methods per entity file (`outbox.go`, `apikeys.go`, `settings.go`, …). Migrations are embedded `.sql` files in `store/migrations/`, applied in filename order and tracked in `schema_migrations`; add a new numbered file to change schema. The pool is deliberately capped at 1 connection (WAL mode) to avoid SQLite write-lock errors — don't "fix" this.
- **`internal/crypto`** — one master key (env `POSTERN_MASTER_KEY`) drives both AES-256-GCM encryption of the SMTP password at rest and HMAC signing of session/flash cookies.
- **API keys** — raw keys are `pn_`-prefixed, shown once at creation, stored only as a hash. Recipients (`to`/`cc`/`bcc`/`from`) are bound to the key by default; per-request recipients require the key's override flag and are all-or-nothing (no field merge), capped at 50. `from` is never overridable.
- **SMTP config lives in the DB** (settings table, edited via admin UI), not env — the worker re-reads it on every send so rotation needs no restart. Env vars are only bootstrap/secrets (see `.env.example` for the full list).
- **`internal/ratelimit`** — fixed-window per-key limits (minute/hour/day buckets) stored in SQLite, so they survive restarts.
- **Templates** (`internal/templates`) — Handlebars (`{{var}}`) via raymond; each template has subject/text/HTML variants. Templates are public or restricted to an allow-list per API key.
- **Admin UI** — each page in `internal/admin/templates/*.html` is parsed together with `base.html`; adding a page means a handler + template + registration in `server.go`'s template set.
