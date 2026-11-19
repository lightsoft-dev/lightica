#!/usr/bin/env bash
# Builds the subscription-limit service and (re)starts it under launchd.
# Run from the repository root on the Mac Studio: bash lightsoft/usage/install.sh
# The first run copies accounts.example.json to ~/.multica/usage/accounts.json;
# edit that file (workspace and runtime IDs) and run this again.
# It does not change Tailscale serve; see lightsoft/usage/README.md.
set -euo pipefail

LABEL=dev.lightsoft.multica-usage
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_DIR="$HOME/.multica/usage"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

umask 077
mkdir -p "$APP_DIR/bin" "$APP_DIR/logs"
chmod 700 "$APP_DIR"

if [ ! -s "$APP_DIR/accounts.json" ]; then
  cp "$SRC_DIR/accounts.example.json" "$APP_DIR/accounts.json"
  echo "created $APP_DIR/accounts.json from the example: set workspace_id, codex_home and runtime_ids, then run this again" >&2
  exit 1
fi

(cd "$SRC_DIR" && go build -trimpath -o "$APP_DIR/bin/lightsoft-usage-server.new" .)
mv "$APP_DIR/bin/lightsoft-usage-server.new" "$APP_DIR/bin/lightsoft-usage-server"

sed -e "s|__HOME__|$HOME|g" "$SRC_DIR/$LABEL.plist" > "$PLIST"
chmod 644 "$PLIST"
DOMAIN="gui/$(id -u)"
launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
# bootout returns before the old job is fully removed; bootstrapping too early
# fails with "5: Input/output error". Wait for it to go, then retry once.
for _ in $(seq 1 20); do
  launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1 || break
  sleep 0.5
done
launchctl bootstrap "$DOMAIN" "$PLIST" 2>/dev/null || { sleep 2; launchctl bootstrap "$DOMAIN" "$PLIST"; }

for _ in $(seq 1 20); do
  if curl -fsS http://127.0.0.1:13500/lightsoft-usage/health >/dev/null 2>&1; then
    echo "lightsoft-usage is up"
    exit 0
  fi
  sleep 0.5
done
echo "lightsoft-usage did not become healthy; see $APP_DIR/logs/service.log" >&2
exit 1
