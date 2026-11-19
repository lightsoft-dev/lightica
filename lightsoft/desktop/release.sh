#!/usr/bin/env bash
# Builds Lightica and publishes it to the team release feed (LIGH-191).
# Run on the Mac Studio from a clean lightsoft/dev checkout. macOS only.
#
#   lightsoft/desktop/release.sh              # Apple silicon
#   lightsoft/desktop/release.sh --x64        # Intel
#   lightsoft/desktop/release.sh --no-issue   # skip the install issue update
#
# 1. lightsoft/desktop/build.sh: version <upstream>-<YYYYMMDD>[.N], signed and
#    notarized dmg + zip, app and bundled CLI version checked.
# 2. Copies the build into the feed folder that lightica-feed serves at
#    https://mac-studio.taile83671.ts.net:3444/lightica/ and records it
#    (releases.json, index.html), then switches latest-mac.yml to it. From
#    that moment installed Lightica apps see the new version.
# 3. Rewrites the "현재 배포 파일" section of the install issue.
#
# Environment:
#   LIGHTICA_FEED_DIR       feed folder (default ~/.multica/lightica/feed)
#   LIGHTICA_FEED_URL       its public URL (default https://mac-studio.taile83671.ts.net:3444/lightica/)
#   LIGHTICA_INSTALL_ISSUE  install issue (default LIGH-116)
#   DESKTOP_BUILD_VERSION   force a version (e.g. the Intel build of a day's release)
# Signing and notarization variables: see build.sh.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
feed_tool="$root/lightsoft/desktop/release-feed.mjs"
feed_dir="${LIGHTICA_FEED_DIR:-$HOME/.multica/lightica/feed}"
base_url="${LIGHTICA_FEED_URL:-https://mac-studio.taile83671.ts.net:3444/lightica/}"
issue="${LIGHTICA_INSTALL_ISSUE:-LIGH-116}"
multica="${MULTICA_CLI_PATH:-multica}"

arch=--arm64
update_issue=1
for a in "$@"; do
  case "$a" in
    --arm64 | --x64) arch="$a" ;;
    --no-issue) update_issue=0 ;;
    *) echo "usage: $0 [--arm64|--x64] [--no-issue]" >&2; exit 2 ;;
  esac
done

step() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# --------------------------------------------------------------- preflight ----
step "preflight"
cd "$root"
[ -d "$feed_dir" ] || die "no feed folder $feed_dir (install it: bash lightsoft/desktop/feed/install.sh)"
# One release at a time: two runs pick the same version while the feed does not
# have it yet, and the later one replaces the earlier one's files and checksums.
# lightica-feed does not serve dot files.
lock="$feed_dir/.release.pid"
if ! (set -o noclobber; echo $$ > "$lock") 2>/dev/null; then
  holder="$(cat "$lock" 2>/dev/null || true)"
  if [ -n "$holder" ] && kill -0 "$holder" 2>/dev/null; then
    die "another release.sh (PID $holder) is publishing to $feed_dir"
  fi
  echo $$ > "$lock" # left behind by a run that was killed
fi
trap 'rm -f "$lock"' EXIT
# The release records its commit, so the build must be exactly that commit.
[ -z "$(git status --porcelain --untracked-files=no)" ] || die "the checkout has uncommitted changes"
if [ "$update_issue" = 1 ]; then
  "$multica" issue get "$issue" --output json >/dev/null \
    || die "cannot read $issue with '$multica' (log in, or pass --no-issue)"
fi
echo "feed $feed_dir → $base_url"

# ------------------------------------------------------------------- build ----
LIGHTICA_FEED_DIR="$feed_dir" bash "$root/lightsoft/desktop/build.sh" "$arch"

dist="$root/apps/desktop/dist"
app="$(ls -d "$dist"/mac*/Lightica.app | head -1)"
version="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist")"
commit="$(git rev-parse HEAD)"
upstream="$(git describe --tags --match 'v[0-9]*' --abbrev=0 HEAD)"
# Installed apps install the update themselves only when it is signed by
# their own Developer ID team (apps/desktop/src/main/lightica-update.ts).
signature="$(codesign -dv --verbose=2 "$app" 2>&1)"
team=""
if grep -q '^Authority=Developer ID Application: ' <<<"$signature"; then
  team="$(sed -n 's/^TeamIdentifier=//p' <<<"$signature")"
fi

# ----------------------------------------------------------------- publish ----
step "publish $version"
node "$feed_tool" publish "$feed_dir" --dist "$dist" --version "$version" --arch "${arch#--}" \
  --team "$team" --upstream "$upstream" --commit "$commit" --base-url "$base_url" >/dev/null
echo "$base_url"

# ------------------------------------------------------------ install issue ----
if [ "$update_issue" = 1 ]; then
  step "update $issue"
  work="$dist/release-issue"
  mkdir -p "$work"
  "$multica" issue get "$issue" --output json \
    | node -e 'let s="";process.stdin.on("data",(d)=>s+=d).on("end",()=>process.stdout.write(JSON.parse(s).description??""))' \
    > "$work/current.md"
  node "$feed_tool" issue-section "$feed_dir" --base-url "$base_url" > "$work/section.md"
  node "$feed_tool" issue-description --current "$work/current.md" --section "$work/section.md" > "$work/description.md"
  "$multica" issue update "$issue" --description-file "$work/description.md" --output json >/dev/null
  echo "$issue: 현재 배포 파일 → $version"
fi

step "done"
echo "Lightica $version (${arch#--}) is live at $base_url"
