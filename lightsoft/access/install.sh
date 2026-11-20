#!/usr/bin/env bash
# Puts the team client on a public hostname through Cloudflare Tunnel, behind
# Cloudflare Access (LIGH-253). Run on the Mac Studio from the repository root,
# only AFTER the Access application for the hostname exists:
#
#   LIGHTICA_HOST=lightica.lightsoft.dev bash lightsoft/access/install.sh
#
# First run creates the named tunnel and its DNS record. Every run renders
# ~/.cloudflared/lightica.yml, (re)starts the tunnel under launchd, and then
# checks from outside that the hostname answers with Cloudflare Access. If the
# origin answers directly, or Access cannot be confirmed, the tunnel is stopped.
# It never touches the production Multica stack or Tailscale serve.
set -euo pipefail

LABEL=dev.lightsoft.lightica-tunnel
HOST="${LIGHTICA_HOST:?set LIGHTICA_HOST, e.g. lightica.lightsoft.dev}"
TUNNEL_NAME="${LIGHTICA_TUNNEL:-lightica}"
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG="$HOME/.cloudflared/lightica.yml"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
DOMAIN="gui/$(id -u)"
CF="$(command -v cloudflared)" || { echo "install.sh: cloudflared not found (brew install cloudflared)" >&2; exit 1; }
[ -s "$HOME/.cloudflared/cert.pem" ] || { echo "install.sh: run 'cloudflared tunnel login' once first" >&2; exit 1; }

# A live tunnel reports deleted_at as the zero time (0001-01-01T00:00:00Z).
tunnel_id() {
  "$CF" tunnel list --output json --name "$TUNNEL_NAME" 2>/dev/null \
    | python3 -I -c 'import json,sys; t=[x for x in json.load(sys.stdin) or [] if (x.get("deleted_at") or "0001-").startswith("0001-")]; print(t[0]["id"] if t else "")'
}

id="$(tunnel_id)"
if [ -z "$id" ]; then
  "$CF" tunnel create "$TUNNEL_NAME" >/dev/null
  id="$(tunnel_id)"
fi
[ -n "$id" ] || { echo "install.sh: could not find or create tunnel $TUNNEL_NAME" >&2; exit 1; }
[ -s "$HOME/.cloudflared/$id.json" ] || { echo "install.sh: credentials for tunnel $TUNNEL_NAME are not on this Mac" >&2; exit 1; }

umask 022
sed -e "s|__TUNNEL_ID__|$id|g" -e "s|__HOST__|$HOST|g" -e "s|__HOME__|$HOME|g" \
  "$SRC_DIR/tunnel.yml.template" > "$CONFIG.new"
"$CF" tunnel --config "$CONFIG.new" ingress validate >/dev/null
mv "$CONFIG.new" "$CONFIG"

# Route with this tunnel's own config and UUID. Without --config, cloudflared
# reads ~/.cloudflared/config.yml, and when that names another tunnel the record
# is pointed at that tunnel instead (LIGH-253: the hostname answered with a
# Cloudflare Tunnel error behind Access). An existing record is left alone
# unless LIGHTICA_OVERWRITE_DNS=1, so a mistyped host cannot take over another
# service's record.
dns_flags=()
[ "${LIGHTICA_OVERWRITE_DNS:-0}" = 1 ] && dns_flags=(--overwrite-dns)
if ! out="$("$CF" tunnel --config "$CONFIG" route dns ${dns_flags[@]+"${dns_flags[@]}"} "$id" "$HOST" 2>&1)"; then
  case "$out" in
    *already*)
      echo "DNS record for $HOST already exists; leaving it as is." >&2
      echo "  If it does not point at tunnel $TUNNEL_NAME ($id), rerun with LIGHTICA_OVERWRITE_DNS=1." >&2 ;;
    *) echo "$out" >&2; exit 1 ;;
  esac
fi

sed -e "s|__HOME__|$HOME|g" -e "s|__CLOUDFLARED__|$CF|g" "$SRC_DIR/$LABEL.plist" > "$PLIST"
stop_tunnel() { launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true; }
stop_tunnel
for _ in $(seq 1 20); do
  launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1 || break
  sleep 0.5
done
launchctl bootstrap "$DOMAIN" "$PLIST" 2>/dev/null || { sleep 2; launchctl bootstrap "$DOMAIN" "$PLIST"; }

# Access answers an anonymous request with a redirect to its login page (or a
# 401/403); the origin would answer /health with 200. Resolve over DoH so a
# negative answer cached before the record existed does not stall the check.
for _ in $(seq 1 90); do
  read -r code location < <(curl -s -o /dev/null --max-time 5 --doh-url https://cloudflare-dns.com/dns-query \
    -w '%{http_code} %{redirect_url}\n' "https://$HOST/health" || echo "000 -")
  case "$code" in
    2??)
      stop_tunnel
      echo "install.sh: https://$HOST answered without Cloudflare Access; tunnel stopped." >&2
      echo "  Create the Access application for $HOST, then run this again." >&2
      exit 1 ;;
    30?)
      case "$location" in
        https://*.cloudflareaccess.com/*) echo "https://$HOST is up behind Cloudflare Access"; exit 0 ;;
      esac ;;
    401|403) echo "https://$HOST is up behind Cloudflare Access"; exit 0 ;;
  esac
  sleep 2
done
stop_tunnel
echo "install.sh: could not confirm Cloudflare Access on https://$HOST (last status $code); tunnel stopped." >&2
echo "  Logs: ~/Library/Logs/lightica-tunnel.log" >&2
exit 1
