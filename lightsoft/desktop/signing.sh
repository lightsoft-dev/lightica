#!/usr/bin/env bash
# Lightsoft "Developer ID Application" identity for Lightica builds (LIGH-110).
#
#   lightsoft/desktop/signing.sh csr            create the private key + CSR (once per certificate)
#   lightsoft/desktop/signing.sh install <cer>  load the certificate Apple issued for that CSR
#   lightsoft/desktop/signing.sh unlock         unlock the signing keychain (build.sh calls this)
#   lightsoft/desktop/signing.sh status         show the identity build.sh will use
#
# The private key is generated on this Mac and never leaves it. It ends up in a
# dedicated keychain whose random password is kept in the login keychain, so
# codesign runs without GUI prompts. An encrypted .p12 backup stays next to it.
# Only the Account Holder can issue a Developer ID certificate (Apple's
# portal; the App Store Connect API refuses), see lightsoft/desktop/README.md.
set -euo pipefail

DIR="${LIGHTSOFT_SIGNING_DIR:-$HOME/.lightsoft/signing}"
KC="${LIGHTSOFT_SIGNING_KEYCHAIN:-$HOME/Library/Keychains/lightsoft-signing.keychain-db}"
KC_PW_SERVICE="${LIGHTSOFT_SIGNING_SERVICE:-dev.lightsoft.signing}-keychain"
P12_PW_SERVICE="${LIGHTSOFT_SIGNING_SERVICE:-dev.lightsoft.signing}-p12"
KEY="$DIR/developer-id.key.pem"
CSR="$DIR/Lightsoft-DeveloperID.certSigningRequest"
P12="$DIR/developer-id-application.p12"

die() { printf 'error: %s\n' "$*" >&2; exit 1; }
secret() { security find-generic-password -s "$1" -w 2>/dev/null; }
store_secret() { security add-generic-password -a lightsoft -s "$1" -w "$2" -U >/dev/null; }

unlock() {
  [ -f "$KC" ] || die "no signing keychain at $KC (run: $0 install <cer>)"
  local pw
  pw="$(secret "$KC_PW_SERVICE")" || die "signing keychain password missing from the login keychain"
  security unlock-keychain -p "$pw" "$KC"
}

ensure_in_search_list() {
  local current
  current="$(security list-keychains -d user | tr -d '"' | xargs)"
  case " $current " in
    *" $KC "*) ;;
    # shellcheck disable=SC2086
    *) security list-keychains -d user -s $current "$KC" ;;
  esac
}

case "${1:-}" in
  csr)
    umask 077
    mkdir -p "$DIR" && chmod 700 "$DIR"
    [ -f "$KEY" ] && die "a pending key already exists: $KEY (its CSR: $CSR)"
    openssl req -new -newkey rsa:2048 -nodes -keyout "$KEY" -out "$CSR" \
      -subj "/CN=Lightsoft Developer ID/O=Lightsoft/C=KR" 2>/dev/null
    echo "$CSR"
    ;;

  install)
    cer="${2:-}"
    [ -f "$cer" ] || die "usage: $0 install <developerID_application.cer>"
    [ -f "$KEY" ] || die "no pending key at $KEY: the certificate must be issued for this Mac's CSR"
    umask 077
    work="$(mktemp -d)"
    trap 'rm -rf "$work"' EXIT
    openssl x509 -inform DER -in "$cer" -out "$work/cert.pem" 2>/dev/null \
      || openssl x509 -in "$cer" -out "$work/cert.pem" \
      || die "$cer is not a certificate"
    [ "$(openssl x509 -noout -pubkey -in "$work/cert.pem" | openssl sha256)" = \
      "$(openssl pkey -pubout -in "$KEY" | openssl sha256)" ] \
      || die "$cer was not issued for $CSR"
    openssl x509 -noout -subject -enddate -in "$work/cert.pem"

    # Encrypted backup; its password goes to the login keychain, never to disk.
    P12_PASS="$(openssl rand -base64 24)"
    export P12_PASS
    openssl pkcs12 -export -legacy -inkey "$KEY" -in "$work/cert.pem" \
      -name "Lightsoft Developer ID Application" -out "$P12" -passout env:P12_PASS
    store_secret "$P12_PW_SERVICE" "$P12_PASS"

    if [ ! -f "$KC" ]; then
      store_secret "$KC_PW_SERVICE" "$(openssl rand -base64 24)"
      security create-keychain -p "$(secret "$KC_PW_SERVICE")" "$KC"
    fi
    security set-keychain-settings "$KC" # no auto-lock timeout
    unlock
    security import "$P12" -k "$KC" -f pkcs12 -P "$P12_PASS" -T /usr/bin/codesign -T /usr/bin/security >/dev/null
    # Lets codesign use the key without a GUI "allow access" prompt.
    security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$(secret "$KC_PW_SERVICE")" "$KC" >/dev/null
    ensure_in_search_list
    rm -f "$KEY"
    security find-identity -v -p codesigning "$KC"
    ;;

  unlock)
    unlock
    ;;

  status)
    [ -f "$KC" ] && unlock && security find-identity -v -p codesigning "$KC" || echo "no signing keychain yet"
    [ -f "$KEY" ] && echo "pending CSR (waiting for the certificate): $CSR"
    exit 0
    ;;

  *)
    sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
