#!/usr/bin/env bash
# Tests for lightsoft/version.sh: bash lightsoft/version.test.sh
set -euo pipefail

src="$(cd "$(dirname "$0")" && pwd)/version.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

repo="$tmp/repo"
mkdir -p "$repo/lightsoft"
cp "$src" "$repo/lightsoft/version.sh"
git -C "$repo" init -q
git -C "$repo" -c user.email=t@example.com -c user.name=t -c commit.gpgsign=false commit -q --allow-empty -m root
git -C "$repo" tag v0.11.0
git -C "$repo" -c user.email=t@example.com -c user.name=t -c commit.gpgsign=false commit -q --allow-empty -m "team feature"
git -C "$repo" tag lightica-build-marker

fail=0
check() { # name expected actual
  if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: expected '$2', got '$3'"; fail=1; fi
}
run() { LIGHTSOFT_VERSION_DATE=20261119 bash "$repo/lightsoft/version.sh" "$@"; }

check "first build of the day" "0.11.0-20261119" "$(run)"
check "unrelated versions ignored" "0.11.0-20261119" "$(printf '0.11.0-20261118\n0.11.0-20261118.3\n0.10.0-20261119\n' | run --taken -)"
check "second build" "0.11.0-20261119.2" "$(printf '0.11.0-20261119\n' | run --taken -)"
check "after .2" "0.11.0-20261119.3" "$(printf '0.11.0-20261119\n0.11.0-20261119.2\n' | run --taken -)"
check "numeric, not lexical" "0.11.0-20261119.11" "$(printf '0.11.0-20261119.9\n0.11.0-20261119.10\n' | run --taken -)"
check "v prefix and blank lines" "0.11.0-20261119.2" "$(printf '\nv0.11.0-20261119\n\n' | run --taken -)"
printf '0.11.0-20261119\n' > "$tmp/taken"
check "taken file" "0.11.0-20261119.2" "$(run --taken "$tmp/taken")"

git -C "$repo" -c user.email=t@example.com -c user.name=t -c commit.gpgsign=false commit -q --allow-empty -m next
git -C "$repo" tag v0.12.0
check "follows the newest release tag" "0.12.0-20261119" "$(printf '0.11.0-20261119\n' | run --taken -)"

if LIGHTSOFT_VERSION_DATE=2026-11-19 bash "$repo/lightsoft/version.sh" >/dev/null 2>&1; then
  echo "FAIL malformed date accepted"; fail=1
else
  echo "ok   malformed date rejected"
fi

# Default date is Asia/Seoul.
check "Seoul date" "0.12.0-$(TZ=Asia/Seoul date +%Y%m%d)" "$(bash "$repo/lightsoft/version.sh")"

exit "$fail"
