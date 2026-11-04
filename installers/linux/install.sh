#!/usr/bin/env bash
set -euo pipefail

BIN="${1:-dist/sml-signx-linux-amd64}"
SERVICE_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/sml-signx.service"

if [[ ! -f "$BIN" ]]; then
  echo "binary not found: $BIN" >&2
  echo "build it first with: make linux" >&2
  exit 1
fi

if [[ $EUID -ne 0 ]]; then
  echo "please run as root (sudo $0 $*)" >&2
  exit 1
fi

if ! id sml-signx >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/sml-signx --create-home --shell /usr/sbin/nologin sml-signx
fi

install -d -o sml-signx -g sml-signx /var/lib/sml-signx
install -d /opt/sml-signx
install -m 0755 "$BIN" /opt/sml-signx/sml-signx
install -m 0644 "$SERVICE_SRC" /etc/systemd/system/sml-signx.service

systemctl daemon-reload
systemctl enable --now sml-signx

echo "sml-signx installed and started."
echo "Check status: systemctl status sml-signx"
echo "API health:   curl http://127.0.0.1:7070/healthz"
