#!/usr/bin/env bash
# Builds the Lightica release feed server and (re)starts it under launchd.
# Run from the repository root on the Mac Studio: bash lightsoft/desktop/feed/install.sh
# It does not change Tailscale serve; see lightsoft/desktop/README.md.
set -euo pipefail

LABEL=dev.lightsoft.lightica-feed
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_DIR="$HOME/.multica/lightica"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

mkdir -p "$APP_DIR/bin" "$APP_DIR/logs" "$APP_DIR/feed"
(cd "$SRC_DIR" && go build -trimpath -o "$APP_DIR/bin/lightica-feed.new" .)
mv "$APP_DIR/bin/lightica-feed.new" "$APP_DIR/bin/lightica-feed"

sed -e "s|__HOME__|$HOME|g" "$SRC_DIR/$LABEL.plist" > "$PLIST"
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"

for _ in $(seq 1 20); do
  if curl -fsS http://127.0.0.1:13400/lightica/health >/dev/null 2>&1; then
    echo "lightica-feed is up: http://127.0.0.1:13400/lightica/ (folder $APP_DIR/feed)"
    exit 0
  fi
  sleep 0.5
done
echo "lightica-feed did not become healthy; see $APP_DIR/logs/feed.log" >&2
exit 1
