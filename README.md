# Bifrost

A small self-hosted email gateway. Microservices POST to `/api/v1/send` over
HTTP; Bifrost queues the message, renders any template, and forwards it
through your SMTP relay (e.g. MXroute). Per-key rate limiting, retry with
exponential backoff, and a server-rendered admin UI.

Single binary. SQLite for state. No external broker. Pure Go (no CGO) so
you can cross-compile to a static binary that runs anywhere.

## Quick start

```bash
go build -o bifrost ./cmd/bifrost

cp .env.example .env
# edit .env: paste a real master key (openssl rand -hex 32) and set admin creds

./bifrost
```

Open `http://localhost:8080/admin/`, sign in, configure SMTP, create an API
key. The raw key is shown **once** at creation — copy it then.

### .env loading

On startup Bifrost reads `.env` from the working directory if present.
Override the path with `BIFROST_ENV_FILE=/path/to/file`. Variables already
set in the shell environment **always win** over `.env`, matching the
convention from `godotenv` / `docker-compose`. A missing file is fine.

The file format supports `KEY=value`, `export KEY=value`, single- and
double-quoted values, `#` comments, and trailing inline comments on
unquoted values.

## API

### `POST /api/v1/send`

Pass the raw API key (the value shown to you once when the key was created,
prefixed `bf_`) in the `Authorization` header. Replace
`bf_YOUR_KEY_HERE` in every example below with that value.

Inline content:

```http
POST /api/v1/send HTTP/1.1
Host: bifrost.example.com
Authorization: Bearer bf_YOUR_KEY_HERE
Content-Type: application/json

{
  "subject": "Welcome",
  "body": "Hi there.",
  "body_html": "<p>Hi there.</p>"
}
```

Or with a template:

```json
{
  "template_name": "welcome",
  "variables": { "name": "Alex" }
}
```

#### Recipients

By default `to`, `cc`, `bcc`, and `from` are bound to the API key — your
microservice never sees an address it could leak, and a stolen key can
only email the addresses the operator already configured.

When you need per-call recipients (e.g. transactional mail to end users),
enable **"Allow the API request to override recipients"** on the key. Then:

```json
{
  "subject": "Your invoice",
  "body": "See attached.",
  "to":  ["customer@example.com"],
  "cc":  ["billing@example.com"]
}
```

- If the body supplies any of `to`/`cc`/`bcc`, all three are taken from the
  body for that send (no field-level merge — keeps the privacy model
  unambiguous).
- If the body supplies none, the key's defaults are used.
- Capped at 50 recipients total across `to+cc+bcc`.
- Without the override flag, sending recipients in the body returns
  `400 invalid_recipients`.

`from` is always bound to the key — there is no per-request override.

Responses:

- `202 Accepted` — `{"message_id":"…","status":"queued"}`
- `400 Bad Request` — invalid JSON, missing subject/body
- `401 Unauthorized` — bad or missing key
- `429 Too Many Requests` — rate limit exceeded; honor `Retry-After`

The message is delivered asynchronously by the in-process worker. Inspect
status via the admin UI at `/admin/messages/`.

### Examples

In each snippet, replace `bf_YOUR_KEY_HERE` with the raw API key and
`https://bifrost.example.com` with your Bifrost host.

**curl**

```bash
curl -X POST https://bifrost.example.com/api/v1/send \
  -H "Authorization: Bearer bf_YOUR_KEY_HERE" \
  -H "Content-Type: application/json" \
  -d '{"subject":"Welcome","body":"Hi there."}'
```

**Go (stdlib)**

```go
import (
    "bytes"
    "encoding/json"
    "net/http"
)

func sendEmail(apiKey string, payload map[string]any) error {
    body, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST",
        "https://bifrost.example.com/api/v1/send",
        bytes.NewReader(body))
    req.Header.Set("Authorization", "Bearer "+apiKey) // bf_YOUR_KEY_HERE
    req.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusAccepted {
        return fmt.Errorf("bifrost: %s", resp.Status)
    }
    return nil
}
```

**Node (fetch)**

```js
await fetch("https://bifrost.example.com/api/v1/send", {
  method: "POST",
  headers: {
    "Authorization": `Bearer ${process.env.BIFROST_API_KEY}`, // bf_YOUR_KEY_HERE
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    template_name: "welcome",
    variables: { name: "Alex" },
  }),
});
```

**Python (requests)**

```python
import os, requests

requests.post(
    "https://bifrost.example.com/api/v1/send",
    headers={
        "Authorization": f"Bearer {os.environ['BIFROST_API_KEY']}",  # bf_YOUR_KEY_HERE
        "Content-Type": "application/json",
    },
    json={"subject": "Welcome", "body": "Hi there."},
).raise_for_status()
```

> **Tip:** never hardcode the key. Read it from an environment variable,
> a secret manager, or your existing config layer.

## Configuration

Bootstrap and secrets come from environment variables. Operational settings
(SMTP credentials, retention) live in SQLite and are edited via the UI.

| Variable                  | Required | Default              | Notes                                            |
| ------------------------- | -------- | -------------------- | ------------------------------------------------ |
| `BIFROST_MASTER_KEY`      | yes      | —                    | 32-byte hex (`openssl rand -hex 32`) or base64   |
| `BIFROST_DB_PATH`         | no       | `bifrost.db`         | SQLite file path                                 |
| `BIFROST_LISTEN_ADDR`     | no       | `:8080`              | `host:port`                                      |
| `BIFROST_ADMIN_USERNAME`  | no       | —                    | Used only to bootstrap the first admin user      |
| `BIFROST_ADMIN_PASSWORD`  | no       | —                    | Same                                             |
| `BIFROST_TLS_CERT`        | no       | —                    | Path to TLS cert (set both or neither)           |
| `BIFROST_TLS_KEY`         | no       | —                    | Path to TLS key                                  |
| `BIFROST_WORKER_INTERVAL` | no       | `1s`                 | Outbox poll interval                             |
| `BIFROST_SHUTDOWN_GRACE`  | no       | `30s`                | HTTP shutdown grace period                       |

### Master key

The master key is used to:

- AES-256-GCM-encrypt the SMTP password at rest in the `settings` table
- HMAC-sign session and flash cookies

**Back up your master key.** Losing it means the stored SMTP password is
unrecoverable; you'll need to re-enter it via the UI.

## Deploying

### Behind a reverse proxy (recommended)

Bifrost listens in plain HTTP and expects a reverse proxy to terminate TLS.
A Caddy block:

```
emails.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

For nginx, the equivalent `proxy_pass` to `127.0.0.1:8080` is sufficient.
Set `BIFROST_LISTEN_ADDR=127.0.0.1:8080` so nothing on the public interface
hears Bifrost directly.

### Built-in TLS

If you'd rather skip the proxy:

```
export BIFROST_TLS_CERT=/etc/bifrost/cert.pem
export BIFROST_TLS_KEY=/etc/bifrost/key.pem
export BIFROST_LISTEN_ADDR=:443
```

### systemd

```bash
sudo ./deploy/install.sh
sudo $EDITOR /etc/bifrost/bifrost.env  # set admin credentials
sudo systemctl start bifrost
sudo journalctl -fu bifrost
```

`install.sh` builds the binary, creates the `bifrost` user, generates a
master key, and installs `deploy/bifrost.service` with a hardened
sandbox profile (read-only system, no new privileges, syscall filter, etc.).

### Docker

```bash
docker build -f deploy/Dockerfile -t bifrost .
docker run -d --name bifrost \
    -p 8080:8080 \
    -v bifrost-data:/data \
    -e BIFROST_MASTER_KEY=$(openssl rand -hex 32) \
    -e BIFROST_ADMIN_USERNAME=admin \
    -e BIFROST_ADMIN_PASSWORD=changeme \
    bifrost
```

Final image is `gcr.io/distroless/static-debian12:nonroot`, ~25 MB.

## MXroute setup

[MXroute](https://mxroute.com) provides authenticated SMTP from a domain
you own. After buying a slot:

1. Add a mailbox on your domain in the MXroute panel.
2. In Bifrost's **SMTP** page, fill in:
   - **Host**: your server's outbound host (e.g. `mail.example.com` —
     check the panel; it's *not* the same as your customer-facing relay).
   - **Port**: `587`
   - **Username**: the full email address of the mailbox
   - **Password**: the mailbox password
   - **TLS mode**: `STARTTLS`
3. Click **Send test**.

When creating an API key, the **From** address must be a domain MXroute is
authorized to send for (i.e. the domain whose DNS you've configured per
MXroute's instructions: SPF, DKIM, return-path).

## Templates

Handlebars-style: `{{variable}}` substitution. Each template has a name,
subject line, plain-text body, and optional HTML body. The same variable
expands in all three.

Templates can be **public** (any key can use them) or **restricted** (must
be in the allow-list on each API key that needs them).

Override the subject per-request by sending `subject` alongside
`template_name`/`template_id`.

## Operations

- **Retention**: deletes happen daily; default 90 days. Configure under
  **Settings**.
- **Stuck messages on crash**: any message left in `sending` state at
  startup is reset to `pending` so it gets a fresh attempt.
- **Backoff**: 1m → 5m → 15m → 1h → 6h → 24h. After the 6th failed attempt,
  status becomes `dead` and no further retries happen. Permanent SMTP errors
  (5xx) skip the schedule and dead-letter immediately.
- **Health**: `GET /healthz` returns `{"ok":true}`.

## Building from source

```bash
go test ./...
go build -trimpath -ldflags="-s -w" -o bifrost ./cmd/bifrost
```

Requires Go 1.22+. No CGO. The binary embeds migrations, HTML templates, and
static assets — nothing external is read at runtime.

## Project layout

```
cmd/bifrost/         entry point
internal/api/        /api/v1/* HTTP handlers
internal/admin/      /admin/* HTTP handlers + HTML templates + CSS
internal/auth/       API-key middleware + signed-cookie sessions
internal/crypto/     AES-GCM secrets, HMAC signing
internal/mailer/     go-mail wrapper, transient/permanent classification
internal/queue/      outbox worker + retention sweeper
internal/ratelimit/  fixed-window per-key rate limiter
internal/store/      SQLite layer + migrations (embedded)
internal/templates/  Handlebars rendering
internal/config/     env-var bootstrap config
deploy/              Dockerfile, systemd unit, install script
```

## License

MIT.
