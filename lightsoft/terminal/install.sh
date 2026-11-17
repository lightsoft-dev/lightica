#!/usr/bin/env bash
# Builds the terminal gateway and (re)starts it under launchd.
# Run from the repository root on the Mac Studio: bash lightsoft/terminal/install.sh
# It does not change Tailscale serve; see lightsoft/terminal/README.md.
set -euo pipefail

LABEL=dev.lightsoft.multica-terminal
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_DIR="$HOME/.multica/terminal"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

# The daemon id scopes local_directory resources to this machine. Optional:
# without it, any local_directory whose folder exists here is used.
DAEMON_ID="${LIGHTSOFT_TERM_DAEMON_ID:-}"
if [ -z "$DAEMON_ID" ] && command -v multica >/dev/null 2>&1; then
  DAEMON_ID="$(multica daemon status --output json 2>/dev/null \
    | python3 -c 'import json,sys; print(json.load(sys.stdin).get("daemon_id") or "")' 2>/dev/null || true)"
fi

mkdir -p "$APP_DIR/bin" "$APP_DIR/logs"
chmod 700 "$APP_DIR"
(cd "$SRC_DIR" && go build -trimpath -o "$APP_DIR/bin/lightsoft-terminal.new" .)
mv "$APP_DIR/bin/lightsoft-terminal.new" "$APP_DIR/bin/lightsoft-terminal"

sed -e "s|__HOME__|$HOME|g" -e "s|__DAEMON_ID__|$DAEMON_ID|g" "$SRC_DIR/$LABEL.plist" > "$PLIST"
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"

for _ in $(seq 1 20); do
  if curl -fsS http://127.0.0.1:13200/lightsoft-term/health >/dev/null 2>&1; then
    echo "lightsoft-terminal is up (daemon id: ${DAEMON_ID:-none})"
    exit 0
  fi
  sleep 0.5
done
echo "lightsoft-terminal did not become healthy; see $APP_DIR/logs/gateway.log" >&2
exit 1
