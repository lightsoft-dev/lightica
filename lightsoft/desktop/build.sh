#!/usr/bin/env bash
# Builds a signed, notarized and stapled Lightica dmg (plus the zip that
# in-app updates install) from this checkout and checks it the way Gatekeeper
# will (LIGH-110). macOS only. To publish the result to the team, use
# lightsoft/desktop/release.sh, which runs this.
#
#   lightsoft/desktop/build.sh            # Apple silicon (arm64)
#   lightsoft/desktop/build.sh --x64      # Intel
#
# Signs as "Developer ID Application: Lightsoft", kept in its own keychain by
# lightsoft/desktop/signing.sh (CSC_NAME="<name> (<TEAMID>)" picks another
# Developer ID). Notarization credentials come from the environment, first
# match wins:
#   APPLE_KEYCHAIN_PROFILE  notarytool keychain profile (default: lightica-notary),
#                           read from APPLE_KEYCHAIN (default: the signing keychain;
#                           APPLE_KEYCHAIN= reads notarytool's own keychain)
#   APPLE_API_KEY + APPLE_API_KEY_ID + APPLE_API_ISSUER   App Store Connect API key
#   APPLE_ID + APPLE_APP_SPECIFIC_PASSWORD                Apple ID
# Values are passed through and never printed. See lightsoft/desktop/README.md.
#
# Version: <upstream release>-<YYYYMMDD>[.N] from lightsoft/version.sh, with
# the versions already in the release feed (LIGHTICA_FEED_DIR) taken. Set
# DESKTOP_BUILD_VERSION to force one. The app and its bundled CLI both get it.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
desktop="$root/apps/desktop"
arch="${1:---arm64}"
case "$arch" in
  --arm64 | --x64) ;;
  *) echo "usage: $0 [--arm64|--x64]" >&2; exit 2 ;;
esac

# An agent run exports production MULTICA_* credentials; a build never needs them.
for v in $(env | grep -oE '^MULTICA_[A-Z_]+' || true); do unset "$v"; done

step() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------- signing ----
step "signing identity"
signing_kc="${LIGHTSOFT_SIGNING_KEYCHAIN:-$HOME/Library/Keychains/lightsoft-signing.keychain-db}"
if [ -f "$signing_kc" ]; then
  bash "$root/lightsoft/desktop/signing.sh" unlock
fi
signer="${CSC_NAME:-Lightsoft}"
signer="${signer#Developer ID Application: }"
identity="$(security find-identity -v -p codesigning \
  | sed -nE 's/.*"(Developer ID Application: [^"]+)".*/\1/p' \
  | grep -F "Developer ID Application: $signer" | head -1 || true)"
[ -n "$identity" ] \
  || die "no valid 'Developer ID Application: $signer' identity (lightsoft/desktop/signing.sh, README)"
team_id="$(printf '%s' "$identity" | sed -nE 's/.*\(([A-Z0-9]{10})\)$/\1/p')"
[ -n "$team_id" ] || die "could not read the team ID from '$identity'"
export CSC_NAME="${identity#Developer ID Application: }"
# scripts/package.mjs turns notarization off unless APPLE_TEAM_ID is set.
export APPLE_TEAM_ID="${APPLE_TEAM_ID:-$team_id}"
echo "$identity"

# ----------------------------------------------------------- notarization ----
step "notarization credentials"
notary=()
if [ -n "${APPLE_API_KEY:-}${APPLE_API_KEY_ID:-}${APPLE_API_ISSUER:-}" ]; then
  [ -n "${APPLE_API_KEY:-}" ] && [ -n "${APPLE_API_KEY_ID:-}" ] && [ -n "${APPLE_API_ISSUER:-}" ] \
    || die "set all of APPLE_API_KEY, APPLE_API_KEY_ID and APPLE_API_ISSUER"
  [ -f "$APPLE_API_KEY" ] || die "APPLE_API_KEY must be the path to the .p8 file"
  notary=(--key "$APPLE_API_KEY" --key-id "$APPLE_API_KEY_ID" --issuer "$APPLE_API_ISSUER")
  echo "App Store Connect API key"
elif [ -n "${APPLE_ID:-}${APPLE_APP_SPECIFIC_PASSWORD:-}" ]; then
  [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_APP_SPECIFIC_PASSWORD:-}" ] \
    || die "set both APPLE_ID and APPLE_APP_SPECIFIC_PASSWORD"
  notary=(--apple-id "$APPLE_ID" --password "$APPLE_APP_SPECIFIC_PASSWORD" --team-id "$APPLE_TEAM_ID")
  echo "Apple ID"
else
  export APPLE_KEYCHAIN_PROFILE="${APPLE_KEYCHAIN_PROFILE:-lightica-notary}"
  # Without --keychain, notarytool keeps profiles in the data protection keychain,
  # which is unreadable while the screen is locked, as it is for the 05:00 daily
  # run. The signing keychain is a file keychain this script has just unlocked.
  # electron-builder reads APPLE_KEYCHAIN too.
  if [ -z "${APPLE_KEYCHAIN+set}" ] && [ -f "$signing_kc" ]; then
    export APPLE_KEYCHAIN="$signing_kc"
  fi
  notary=(--keychain-profile "$APPLE_KEYCHAIN_PROFILE")
  if [ -n "${APPLE_KEYCHAIN:-}" ]; then
    notary+=(--keychain "$APPLE_KEYCHAIN")
    echo "keychain profile '$APPLE_KEYCHAIN_PROFILE' in $APPLE_KEYCHAIN"
  else
    echo "keychain profile '$APPLE_KEYCHAIN_PROFILE'"
  fi
fi
xcrun notarytool history "${notary[@]}" >/dev/null 2>&1 \
  || die "notarytool could not sign in with these credentials (save the profile with lightsoft/desktop/signing.sh notary, see lightsoft/desktop/README.md)"

# ----------------------------------------------------------------- source ----
step "version"
cd "$root"
feed_dir="${LIGHTICA_FEED_DIR:-$HOME/.multica/lightica/feed}"
if [ -z "${DESKTOP_BUILD_VERSION:-}" ]; then
  DESKTOP_BUILD_VERSION="$(node "$root/lightsoft/desktop/release-feed.mjs" versions "$feed_dir" --arch "${arch#--}" \
    | bash "$root/lightsoft/version.sh" --taken -)"
fi
# scripts/package.mjs (app) and scripts/bundle-cli.mjs (CLI) read it.
export DESKTOP_BUILD_VERSION
echo "$DESKTOP_BUILD_VERSION ($(git describe --tags --match 'v[0-9]*' --always --dirty))"

# ------------------------------------------------------------------ build ----
step "build"
pnpm install --frozen-lockfile
# The zip (and latest-mac.yml) is what in-app updates download and install.
(cd "$desktop" && pnpm package -- --mac dmg zip "$arch")

dmg="$desktop/dist/lightica-$DESKTOP_BUILD_VERSION-mac-${arch#--}.dmg"
zip="$desktop/dist/lightica-$DESKTOP_BUILD_VERSION-mac-${arch#--}.zip"
app="$(ls -d "$desktop"/dist/mac*/Lightica.app | head -1)"
[ -f "$dmg" ] || die "no $(basename "$dmg") under $desktop/dist"
[ -f "$zip" ] || die "no $(basename "$zip") under $desktop/dist"
[ -d "$app" ] || die "no Lightica.app under $desktop/dist"

# electron-builder notarizes and staples the .app only; do the dmg as well so
# the download itself passes Gatekeeper.
step "notarize dmg"
codesign --force --sign "$identity" --timestamp "$dmg"
xcrun notarytool submit "$dmg" "${notary[@]}" --wait
xcrun stapler staple "$dmg"

# ----------------------------------------------------------------- verify ----
step "verify"
codesign --verify --deep --strict --verbose=2 "$app"
spctl --assess --type execute --verbose=4 "$app"
xcrun stapler validate "$app"
spctl --assess --type open --context context:primary-signature --verbose=4 "$dmg"
xcrun stapler validate "$dmg"
/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist"
app_version="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist")"
[ "$app_version" = "$DESKTOP_BUILD_VERSION" ] || die "app version is $app_version, expected $DESKTOP_BUILD_VERSION"
# The daemon reports this as its runtime version (설정 → 런타임).
cli="$app/Contents/Resources/app.asar.unpacked/resources/bin/multica"
cli_version="$("$cli" version --output json | node -e 'let s="";process.stdin.on("data",(d)=>s+=d).on("end",()=>console.log(JSON.parse(s).version))')"
[ "$cli_version" = "$DESKTOP_BUILD_VERSION" ] || die "bundled CLI version is $cli_version, expected $DESKTOP_BUILD_VERSION"
echo "bundled CLI $cli_version"

step "done"
echo "$dmg"
echo "$zip"
