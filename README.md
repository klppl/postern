# Postern

Self-hosted email gateway. Your microservices POST to `/api/v1/send`; Postern
queues the message in SQLite, renders any template, and delivers it through a
provider — a standard SMTP relay or MXroute's HTTP SMTP API.

Single Go binary, no CGO (`modernc.org/sqlite`), no external broker. Migrations,
admin templates, and static assets are embedded — only `.env` and the DB file
are read at runtime.

## Features

- **HTTP send API** with `pn_`-prefixed API keys (stored hashed, shown once).
- **Recipients bound to the key** by default; per-key opt-in for request-supplied
  `to`/`cc`/`bcc` (all-or-nothing, capped at 50). `from` is never overridable.
- **Queue + retry**: single worker drains the outbox, backoff 1m→5m→15m→1h→6h→24h,
  dead after 6 attempts. 5xx SMTP errors dead-letter immediately.
- **Per-key rate limits** (minute/hour/day), persisted in SQLite.
- **Handlebars templates** (`{{var}}`) with subject/text/HTML variants; public or
  per-key allow-listed.
- **Server-rendered admin UI** with signed-cookie sessions for SMTP config, keys,
  templates, and message inspection.
- **Encryption at rest**: one master key AES-256-GCM-encrypts the SMTP password
  and HMAC-signs cookies.

## Quick start

```bash
go build -o postern ./cmd/postern
cp .env.example .env      # set POSTERN_MASTER_KEY (openssl rand -hex 32) + admin creds
./postern                 # open http://localhost:8080/admin/
```

Sign in, pick a delivery mode and fill in provider config, then create an API
key (the raw key is shown once).

## API

`POST /api/v1/send`, `Authorization: Bearer pn_...`, JSON body:

```json
{ "subject": "Welcome", "body": "Hi.", "body_html": "<p>Hi.</p>" }
```

or with a template:

```json
{ "template_name": "welcome", "variables": { "name": "Alex" } }
```

Add `to`/`cc`/`bcc` when the key allows recipient override. Responses:
`202` queued, `400` bad request, `401` bad key, `429` rate limited (honor
`Retry-After`). Delivery is async; check status in `/admin/messages/`.
`GET /healthz` → `{"ok":true}`.

## Configuration

Bootstrap and secrets are env vars; provider config and retention live in SQLite
and are edited in the admin UI (re-read on every send, so rotating credentials or
switching providers needs no restart).

| Variable                  | Required | Default      | Notes                                      |
| ------------------------- | -------- | ------------ | ------------------------------------------ |
| `POSTERN_MASTER_KEY`      | yes      | —            | 32-byte hex/base64; encrypts secrets + signs cookies |
| `POSTERN_DB_PATH`         | no       | `postern.db` | SQLite file path                           |
| `POSTERN_LISTEN_ADDR`     | no       | `:8080`      | `host:port`                                |
| `POSTERN_ADMIN_USERNAME`  | no       | —            | Bootstraps the first admin user            |
| `POSTERN_ADMIN_PASSWORD`  | no       | —            | Same                                       |
| `POSTERN_TLS_CERT` / `_KEY`| no      | —            | Built-in TLS (set both or neither)         |
| `POSTERN_WORKER_INTERVAL` | no       | `1s`         | Outbox poll interval                       |
| `POSTERN_SHUTDOWN_GRACE`  | no       | `30s`        | HTTP shutdown grace                        |

**Back up the master key** — losing it makes the stored SMTP password
unrecoverable.

## Deploy

Run behind a reverse proxy that terminates TLS (set
`POSTERN_LISTEN_ADDR=127.0.0.1:8080`), or use built-in TLS via the cert/key vars.

```bash
# systemd
sudo ./deploy/install.sh && sudo systemctl start postern

# docker (distroless, ~25 MB)
docker run -d -p 8080:8080 -v postern-data:/data \
  -e POSTERN_MASTER_KEY=$(openssl rand -hex 32) \
  -e POSTERN_ADMIN_USERNAME=admin -e POSTERN_ADMIN_PASSWORD=changeme postern
```

## Build

```bash
go test ./...
go build -trimpath -ldflags="-s -w" -o postern ./cmd/postern
```

Requires Go 1.22+. No CGO.

## License

MIT.
