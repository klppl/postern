#!/usr/bin/env bash
# Install Postern as a systemd service on Linux.
#
# Run as root:
#   sudo ./deploy/install.sh
#
# Builds the binary in place, copies it to /usr/local/bin, drops a default
# environment file, creates the postern user, and enables the unit. Idempotent.

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
    echo "must be run as root" >&2
    exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DST=/usr/local/bin/postern
ENV_DIR=/etc/postern
UNIT_DST=/etc/systemd/system/postern.service
USER=postern

echo "==> building postern"
(cd "$ROOT" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BIN_DST" ./cmd/postern)
chmod 0755 "$BIN_DST"

echo "==> creating user $USER"
if ! id -u "$USER" >/dev/null 2>&1; then
    useradd --system --home /var/lib/postern --shell /usr/sbin/nologin "$USER"
fi

echo "==> installing env file"
mkdir -p "$ENV_DIR"
if [[ ! -f "$ENV_DIR/postern.env" ]]; then
    cp "$ROOT/deploy/postern.env.example" "$ENV_DIR/postern.env"
    KEY="$(openssl rand -hex 32)"
    sed -i "s|^POSTERN_MASTER_KEY=.*|POSTERN_MASTER_KEY=$KEY|" "$ENV_DIR/postern.env"
    echo "    wrote $ENV_DIR/postern.env (master key generated — back it up!)"
fi
chown -R root:"$USER" "$ENV_DIR"
chmod 0750 "$ENV_DIR"
chmod 0640 "$ENV_DIR/postern.env"

echo "==> installing systemd unit"
install -m 0644 "$ROOT/deploy/postern.service" "$UNIT_DST"
systemctl daemon-reload
systemctl enable postern.service

echo "==> done. Edit $ENV_DIR/postern.env then run: systemctl start postern"
