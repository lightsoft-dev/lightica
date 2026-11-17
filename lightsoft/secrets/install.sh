#!/usr/bin/env bash
# Builds the secrets service and its agent CLI, creates the master key on the
# first run, and (re)starts the service under launchd.
# Run from the repository root on the Mac Studio: bash lightsoft/secrets/install.sh
# It does not change Tailscale serve; see lightsoft/secrets/README.md.
set -euo pipefail

LABEL=dev.lightsoft.multica-secrets
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_DIR="$HOME/.multica/secrets"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
CLI_DIR="${LIGHTSOFT_SECRETS_CLI_DIR:-$HOME/.local/bin}"

umask 077
mkdir -p "$APP_DIR/bin" "$APP_DIR/logs"
chmod 700 "$APP_DIR"

if [ ! -s "$APP_DIR/master.key" ]; then
  openssl rand -base64 32 > "$APP_DIR/master.key.new"
  chmod 600 "$APP_DIR/master.key.new"
  mv "$APP_DIR/master.key.new" "$APP_DIR/master.key"
  echo "created $APP_DIR/master.key"
  echo "  back it up offline now (e.g. print it). Without it no stored secret can be recovered."
fi

(cd "$SRC_DIR" && go build -trimpath -o "$APP_DIR/bin/lightsoft-secrets-server.new" .)
(cd "$SRC_DIR" && go build -trimpath -o "$APP_DIR/bin/lightsoft-secrets.new" ./cmd/lightsoft-secrets)
mv "$APP_DIR/bin/lightsoft-secrets-server.new" "$APP_DIR/bin/lightsoft-secrets-server"
mv "$APP_DIR/bin/lightsoft-secrets.new" "$APP_DIR/bin/lightsoft-secrets"
# Agents on this Mac find the CLI on PATH.
mkdir -p "$CLI_DIR"
install -m 755 "$APP_DIR/bin/lightsoft-secrets" "$CLI_DIR/lightsoft-secrets"

sed -e "s|__HOME__|$HOME|g" "$SRC_DIR/$LABEL.plist" > "$PLIST"
chmod 644 "$PLIST"
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"

for _ in $(seq 1 20); do
  if curl -fsS http://127.0.0.1:13300/lightsoft-secrets/health >/dev/null 2>&1; then
    echo "lightsoft-secrets is up (CLI: $CLI_DIR/lightsoft-secrets)"
    exit 0
  fi
  sleep 0.5
done
echo "lightsoft-secrets did not become healthy; see $APP_DIR/logs/service.log" >&2
exit 1
