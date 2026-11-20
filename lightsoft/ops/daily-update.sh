#!/usr/bin/env bash
# Lightsoft — daily update of the self-hosted Multica on the Mac Studio (LIGH-99, LIGH-190).
#
#   1. Fork mirror  fast-forward lightsoft-dev/multica `main` to upstream `main`,
#                   and push upstream's release tags (v*) the fork does not have yet.
#   2. Production   follow the newest upstream *release* (pinned MULTICA_IMAGE_TAG):
#                   merge it into `lightsoft/dev` locally → build the fork backend
#                   image (fork backend only) → verified pg_dump → switch → push the
#                   merge once production is healthy. Rolls the images back if the new
#                   backend does not come up healthy (the DB is NOT restored
#                   automatically — the backup path is reported instead).
#                   Fork backend: a merge conflict or a failed build leaves production
#                   where it is, and a server-side change on `lightsoft/dev` redeploys
#                   the backend even without a new release.
#   3. Team client  rebuild/restart compose project `multica-team-web` when
#                   `lightsoft/dev` moved (or the container is not running).
#   4. Lightica     build, notarize and publish the desktop app to the team feed
#                   (lightsoft/desktop/release.sh) when `lightsoft/dev` changed what
#                   goes into it since the published build (LIGH-257). Installed
#                   apps find it within the hour and tell their users.
#   5. Report       comment on the tracking issue when something changed; open an
#                   issue for the project agent when something failed.
#
# The backend is the fork build when ~/.multica/server/.env sets
# MULTICA_BACKEND_IMAGE=$FORK_BACKEND_IMAGE; anything else is the upstream image.
# The web image (:3443) is always upstream's.
#
# Installed copy: ~/.multica/ops/daily-update.sh (run by launchd, see
# lightsoft/ops/README.md). Config: ~/.multica/ops/config.env.
# DRY_RUN=1 prints what would change without touching anything.
set -uo pipefail

export PATH="/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
# Never act with an agent run's identity, even if started from one.
for v in $(env | grep -oE '^MULTICA_[A-Z_]+' || true); do unset "$v"; done

OPS_HOME="${OPS_HOME:-$HOME/.multica/ops}"
# shellcheck disable=SC1091
[ -f "$OPS_HOME/config.env" ] && . "$OPS_HOME/config.env"

SERVER_DIR="${SERVER_DIR:-$HOME/.multica/server}"
COMPOSE_FILE="$SERVER_DIR/docker-compose.selfhost.yml"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-multica-postgres-1}"
BACKEND_CONTAINER="${BACKEND_CONTAINER:-multica-backend-1}"
REPO_DIR="${REPO_DIR:-$OPS_HOME/multica-custom}"
BACKUP_DIR="${BACKUP_DIR:-$HOME/.multica/backups}"
KEEP_BACKUPS="${KEEP_BACKUPS:-14}"
TEAM_BRANCH="${TEAM_BRANCH:-lightsoft/dev}"
UPSTREAM_REPO="${UPSTREAM_REPO:-multica-ai/multica}"
FORK_BACKEND_IMAGE="${FORK_BACKEND_IMAGE:-localhost/lightsoft/multica-backend}"
KEEP_FORK_BUILDS="${KEEP_FORK_BUILDS:-5}"
API="${API:-http://127.0.0.1:8080}"
TEAM_WEB="${TEAM_WEB:-http://127.0.0.1:13000}"
REPORT_ISSUE="${REPORT_ISSUE:-}"
FAILURE_PROJECT="${FAILURE_PROJECT:-}"
FAILURE_ASSIGNEE_ID="${FAILURE_ASSIGNEE_ID:-}"
DRY_RUN="${DRY_RUN:-0}"
DESKTOP_RELEASE="${DESKTOP_RELEASE:-1}"   # 0 turns the Lightica step off
LIGHTICA_FEED_DIR="${LIGHTICA_FEED_DIR:-$HOME/.multica/lightica/feed}"
# What goes into Lightica: the app, the shared packages, the bundled CLI
# (server/), and the dependency set. Web-only and ops changes do not ship it.
DESKTOP_PATHS=(apps/desktop packages server package.json pnpm-lock.yaml pnpm-workspace.yaml)

LOG_DIR="$OPS_HOME/logs"
STATE_DIR="$OPS_HOME/state"
FORK_FAILED_STATE="$STATE_DIR/fork-backend.failed"  # tree of the last fork backend that did not come up
mkdir -p "$LOG_DIR" "$STATE_DIR" "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"
TS="$(date +%Y%m%d-%H%M%S)"
LOG="$LOG_DIR/daily-update-$(date +%Y%m%d).log"
exec >>"$LOG" 2>&1
cd "$OPS_HOME" || exit 1

CHANGES=()
FAILURES=()
log()     { printf '%s %s\n' "$(date '+%F %T')" "$*" >&2; }  # stderr: keeps $(...) captures clean
changed() { log "CHANGED: $*"; CHANGES+=("$*"); }
failed()  { log "FAILED: $*"; FAILURES+=("$*"); }

if ! mkdir "$STATE_DIR/lock" 2>/dev/null; then
  log "another run holds $STATE_DIR/lock — exiting"
  exit 0
fi
trap 'rmdir "$STATE_DIR/lock" 2>/dev/null' EXIT

log "=== daily update start (dry_run=$DRY_RUN) ==="

env_get() { grep -E "^$1=" "$SERVER_DIR/.env" | tail -1 | cut -d= -f2-; }
env_set() { sed -i '' "s|^$1=.*|$1=$2|" "$SERVER_DIR/.env"; }
compose() { docker compose -f "$COMPOSE_FILE" "$@"; }
health_commit() {
  curl -sf --max-time 3 "$API/health" 2>/dev/null \
    | python3 -c 'import json,sys; print(json.load(sys.stdin).get("commit",""))' 2>/dev/null
}
wait_backend() {  # $1 = expected commit (empty: any healthy)
  local want=$1 got i
  for i in $(seq 1 150); do
    got="$(health_commit)"
    if [ -n "$got" ] && { [ -z "$want" ] || [ "$got" = "$want" ]; }; then return 0; fi
    sleep 2
  done
  return 1
}

# ------------------------------------------------------------------ repo ----
prepare_repo() {
  if [ ! -d "$REPO_DIR/.git" ]; then
    log "cloning ops checkout into $REPO_DIR"
    git clone -q https://github.com/lightsoft-dev/multica.git "$REPO_DIR" || return 1
    git -C "$REPO_DIR" remote add upstream "https://github.com/$UPSTREAM_REPO.git"
  fi
  git -C "$REPO_DIR" fetch -q --prune origin \
    && git -C "$REPO_DIR" fetch -q --prune --tags upstream
}

# ------------------------------------------------------------------ fork ----
mirror_fork_main() {
  cd "$REPO_DIR" || return
  if [ "$(git rev-parse origin/main)" = "$(git rev-parse upstream/main)" ]; then
    log "fork main: up to date"; cd "$OPS_HOME"; return
  fi
  if git merge-base --is-ancestor origin/main upstream/main; then
    if [ "$DRY_RUN" = 1 ]; then
      log "dry-run: would fast-forward fork main to $(git rev-parse --short upstream/main)"
    elif git push -q origin upstream/main:refs/heads/main; then
      changed "포크 main → upstream main $(git rev-parse --short upstream/main) (fast-forward)"
    else
      failed "fork main: push failed"
    fi
  else
    failed "포크 main이 upstream과 갈라짐 (fast-forward 불가) — 수동 확인 필요"
  fi
  cd "$OPS_HOME"
}

# Release tags let a fork-only checkout merge the release production runs
# (`git merge v0.11.0`) without adding the upstream remote. Only tags the fork
# lacks are pushed; a tag already on the fork is never moved or forced.
mirror_release_tags() {
  local missing t tags=() remote
  cd "$REPO_DIR" || return
  if ! remote="$(git ls-remote --tags --refs origin 'refs/tags/v*')"; then
    failed "fork tags: could not list the fork's tags"; cd "$OPS_HOME"; return
  fi
  missing="$(comm -23 <(git tag -l 'v*' | sort) <(printf '%s\n' "$remote" | sed -n 's#.*refs/tags/##p' | sort))"
  while IFS= read -r t; do [ -n "$t" ] && tags+=("refs/tags/$t"); done <<<"$missing"
  if [ ${#tags[@]} -eq 0 ]; then log "fork tags: up to date"; cd "$OPS_HOME"; return; fi
  if [ "$DRY_RUN" = 1 ]; then
    log "dry-run: would push ${#tags[@]} release tag(s) to the fork"
  elif git push -q origin "${tags[@]}"; then
    changed "포크에 upstream 릴리스 태그 ${#tags[@]}개 push (최신 $(printf '%s\n' "$missing" | sort -V | tail -1))"
  else
    failed "fork tags: push failed"
  fi
  cd "$OPS_HOME"
}

# ------------------------------------------------------------ production ----
latest_release() {
  curl -sf --max-time 20 "https://api.github.com/repos/$UPSTREAM_REPO/releases/latest" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"])' 2>/dev/null
}
newer_than() {  # is $1 a newer version than $2?
  [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -1)" = "$1" ]
}
fork_backend() { [ "$(env_get MULTICA_BACKEND_IMAGE)" = "$FORK_BACKEND_IMAGE" ]; }
image_id() { docker image inspect -f '{{.Id}}' "$1" 2>/dev/null; }

published() {  # $1 = release; is every ghcr image production would pull for it up?
  local img imgs=(multica-web)
  fork_backend || imgs+=(multica-backend)
  for img in "${imgs[@]}"; do
    if ! docker manifest inspect "ghcr.io/multica-ai/$img:$1" >/dev/null 2>&1; then
      log "production: ghcr.io/multica-ai/$img:$1 not published yet — retry tomorrow"; return 1
    fi
  done
}

backup_db() {  # $1 = label
  local out="$BACKUP_DIR/multica-$1-$TS.dump"
  docker exec "$POSTGRES_CONTAINER" sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "$out" || return 1
  chmod 600 "$out"
  local tables
  tables="$(docker run --rm -v "$BACKUP_DIR:/b:ro" pgvector/pgvector:pg17 pg_restore -l "/b/$(basename "$out")" 2>/dev/null | grep -c 'TABLE DATA' || true)"
  [ "${tables:-0}" -gt 0 ] || { log "backup $out looks empty"; return 1; }
  cp -p "$SERVER_DIR/.env" "$BACKUP_DIR/server.env-$1-$TS" && chmod 600 "$BACKUP_DIR/server.env-$1-$TS"
  log "backup ok: $out ($tables tables)"
  printf '%s' "$out"
}

prune_backups() {
  # shellcheck disable=SC2012
  ls -1t "$BACKUP_DIR"/multica-*.dump 2>/dev/null | tail -n +"$((KEEP_BACKUPS + 1))" | while read -r f; do
    rm -f "$f"; log "pruned old backup $f"
  done
}

prune_old_images() {  # keep only $1 (current) and $2 (previous, the rollback target)
  local img tag
  for img in multica-backend multica-web; do
    docker images --format '{{.Tag}}' "ghcr.io/multica-ai/$img" | while read -r tag; do
      case "$tag" in
        "$1"|"$2") ;;
        *) docker rmi "ghcr.io/multica-ai/$img:$tag" >/dev/null 2>&1 && log "removed old image $img:$tag" ;;
      esac
    done
  done
}

# Fork images carry a release tag (what compose runs) and a team version tag per
# build. After a release upgrade keep the release tags $1 (current) and $2
# (previous, the rollback target); always keep the newest $KEEP_FORK_BUILDS builds.
# Untagging a build whose image a kept release tag still names frees nothing.
prune_fork_images() {
  local tag
  if [ "$1" != "$2" ]; then
    docker images --format '{{.Tag}}' "$FORK_BACKEND_IMAGE" | grep -E '^v[0-9]' | while read -r tag; do
      case "$tag" in
        "$1"|"$2") ;;
        *) docker rmi "$FORK_BACKEND_IMAGE:$tag" >/dev/null 2>&1 && log "removed fork image tag $tag" ;;
      esac
    done
  fi
  docker images --format '{{.Tag}}' "$FORK_BACKEND_IMAGE" | grep -E '^[0-9]+\.[0-9]+\.[0-9]+-[0-9]{8}' | sort -rV \
    | tail -n +"$((KEEP_FORK_BUILDS + 1))" | while read -r tag; do
      docker rmi "$FORK_BACKEND_IMAGE:$tag" >/dev/null 2>&1 && log "removed fork build $tag"
    done
}

# The team branch gets the release merged locally first; the merge is pushed
# only once production runs it, so the team client is never ahead of the API.
MERGED=""
merge_release() {  # $1 = release; 0 when HEAD contains it
  MERGED=""
  if ! { git checkout -q -B "$TEAM_BRANCH" "origin/$TEAM_BRANCH" && git reset -q --hard "origin/$TEAM_BRANCH" && git clean -qfd; }; then
    failed "team branch: could not check out origin/$TEAM_BRANCH"; return 1
  fi
  git merge-base --is-ancestor "$1" HEAD && return 0
  if git merge -q --no-ff --no-edit -m "chore(lightsoft): sync with upstream $1" "$1"; then
    MERGED="$1"; return 0
  fi
  git merge --abort
  failed "$TEAM_BRANCH 에 $1 merge 중 충돌 — 수동 merge 필요"
  return 1
}
drop_local_merge() { [ -z "$MERGED" ] || git reset -q --hard "origin/$TEAM_BRANCH"; MERGED=""; }
push_team_branch() {
  [ -n "$MERGED" ] || return 0
  if git push -q origin "$TEAM_BRANCH"; then
    changed "$TEAM_BRANCH 에 upstream $MERGED merge"
  else
    failed "team branch: push failed (운영은 로컬 merge $(git rev-parse --short HEAD)로 실행 중)"
  fi
}

# Does production need a fresh fork image for $2? Only server-side changes count,
# so a frontend-only change to the team branch does not restart the backend.
fork_build_needed() {  # $1 = target release, $2 = rev
  local running
  [ "$1" != "$(env_get MULTICA_IMAGE_TAG)" ] && return 0
  [ -n "$(image_id "$FORK_BACKEND_IMAGE:$1")" ] || return 0
  running="$(health_commit)"
  [ -n "$running" ] || return 0
  ! git diff --quiet "$running" "$2" -- server Dockerfile docker 2>/dev/null
}

# A same-release rebuild moved the tag production runs; put it back on failure.
restore_alias() {  # $1 = built version, $2 = target, $3 = current release, $4 = image the tag named before
  if [ -n "$1" ] && [ "$2" = "$3" ] && [ -n "$4" ]; then docker tag "$4" "$FORK_BACKEND_IMAGE:$3"; fi
}

update_production() {
  local cur new target fork=0 want backup version="" prev_alias="" pull=(frontend) what
  cur="$(env_get MULTICA_IMAGE_TAG)"
  fork_backend && fork=1
  target="$cur"
  new="$(latest_release)"
  if [ -z "$new" ]; then
    failed "production: could not read the latest upstream release"
  else
    log "production: running $cur ($([ $fork = 1 ] && echo fork || echo upstream) backend), latest release $new"
    if { [ "$cur" = latest ] || newer_than "$new" "$cur"; } && published "$new"; then target="$new"; fi
  fi
  if [[ ! "$target" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log "production: '$target' is not a release tag — nothing to do"; return
  fi
  cd "$REPO_DIR" || return

  if [ "$DRY_RUN" = 1 ]; then
    [ "$target" = "$cur" ] || log "dry-run: would upgrade production $cur -> $target"
    git merge-base --is-ancestor "$target" "origin/$TEAM_BRANCH" || log "dry-run: would merge $target into $TEAM_BRANCH"
    if [ $fork = 1 ] && fork_build_needed "$target" "origin/$TEAM_BRANCH"; then
      log "dry-run: would build the fork backend from $TEAM_BRANCH and redeploy it"
    fi
    cd "$OPS_HOME"; return
  fi

  # 1. Merge. With the fork backend the image is built from this merge, so a conflict stops here.
  if ! merge_release "$target" && [ $fork = 1 ]; then
    failed "포크 backend: $target merge 실패 → 운영은 $cur 그대로"; cd "$OPS_HOME"; return
  fi

  # 2. Build (fork backend only). A tree that already failed to come up is not
  # redeployed every morning; a new commit (or deleting the state file) retries.
  if [ $fork = 1 ]; then
    if [ "$(cat "$FORK_FAILED_STATE" 2>/dev/null)" = "$(git rev-parse 'HEAD^{tree}')" ]; then
      log "fork backend: $TEAM_BRANCH $(git rev-parse --short HEAD) already failed to come up — not retrying (rm $FORK_FAILED_STATE to retry)"
      drop_local_merge; cd "$OPS_HOME"; return
    fi
    if fork_build_needed "$target" HEAD; then
      prev_alias="$(image_id "$FORK_BACKEND_IMAGE:$target")"
      if ! version="$(RELEASE="$target" IMAGE="$FORK_BACKEND_IMAGE" bash lightsoft/backend/build.sh)"; then
        failed "포크 backend 이미지 빌드 실패 ($TEAM_BRANCH $(git rev-parse --short HEAD)) → 운영은 $cur 그대로"
        drop_local_merge; cd "$OPS_HOME"; return
      fi
      log "fork backend: built $FORK_BACKEND_IMAGE:$version"
    fi
    want="$(git rev-parse HEAD)"
  else
    pull+=(backend)
    want="$(git rev-list -n1 "$target" 2>/dev/null)"
  fi

  if [ "$target" = "$cur" ] && [ -z "$version" ]; then
    log "production: up to date"; push_team_branch; cd "$OPS_HOME"; return
  fi

  # 3. Back up, 4. switch.
  if ! backup="$(backup_db "$cur")"; then
    failed "production: DB backup failed — $target 배포 건너뜀"
    restore_alias "$version" "$target" "$cur" "$prev_alias"; drop_local_merge; cd "$OPS_HOME"; return
  fi
  if [ "$target" != "$cur" ]; then
    env_set MULTICA_IMAGE_TAG "$target"
    if ! compose pull -q "${pull[@]}"; then
      env_set MULTICA_IMAGE_TAG "$cur"; failed "production: pulling $target failed — stayed on $cur"
      drop_local_merge; cd "$OPS_HOME"; return
    fi
  fi
  compose up -d --no-deps backend frontend
  what="$cur → $target"; [ "$target" = "$cur" ] && what="$cur backend 갱신"
  [ -n "$version" ] && what="$what (포크 backend \`$version\`, $(git rev-parse --short HEAD))"
  if wait_backend "$want"; then
    changed "운영 Multica $what (DB 백업: \`$backup\`)"
    rm -f "$FORK_FAILED_STATE"
    prune_backups
    [ "$target" = "$cur" ] || prune_old_images "$target" "$cur"
    [ $fork = 1 ] && prune_fork_images "$target" "$cur"
    push_team_branch
  else
    log "production: $target did not become healthy — rolling back to $cur"
    [ $fork = 1 ] && git rev-parse 'HEAD^{tree}' > "$FORK_FAILED_STATE"
    env_set MULTICA_IMAGE_TAG "$cur"
    restore_alias "$version" "$target" "$cur" "$prev_alias"
    compose up -d --no-deps backend frontend
    if wait_backend ""; then
      failed "운영 $what 실패 → $cur 로 롤백함. 마이그레이션이 적용됐을 수 있으니 확인 필요 (DB 백업: \`$backup\`)"
    else
      failed "운영 $what 실패, $cur 롤백 후에도 backend가 healthy 아님 — 즉시 확인 필요 (DB 백업: \`$backup\`)"
    fi
    drop_local_merge
  fi
  cd "$OPS_HOME"
}

# ----------------------------------------------------------- team client ----
update_team_client() {
  local head built running
  cd "$REPO_DIR" || return
  head="$(git rev-parse --short HEAD)"
  built="$(cat "$STATE_DIR/team-web.built" 2>/dev/null || true)"
  running="$(docker inspect -f '{{.State.Running}}' multica-team-web-web-1 2>/dev/null || echo false)"
  if [ "$head" = "$built" ] && [ "$running" = true ]; then
    log "team client: up to date ($head)"; cd "$OPS_HOME"; return
  fi
  if [ "$DRY_RUN" = 1 ]; then log "dry-run: would rebuild team client at $head"; cd "$OPS_HOME"; return; fi
  if TEAM_WEB_VERSION="lightsoft-dev-$head" docker compose -f lightsoft/docker-compose.team-web.yml up -d --build; then
    local i code=000
    for i in $(seq 1 60); do
      code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$TEAM_WEB/" || true)"
      [ "$code" = 200 ] && break; sleep 2
    done
    if [ "$code" = 200 ]; then
      echo "$head" > "$STATE_DIR/team-web.built"
      changed "팀 클라이언트(:3444) 재빌드 → $TEAM_BRANCH $head"
      docker image prune -f >/dev/null 2>&1 || true
    else
      failed "팀 클라이언트 재빌드 후 응답 없음 (HTTP $code)"
    fi
  else
    failed "팀 클라이언트 빌드 실패"
  fi
  cd "$OPS_HOME"
}

# --------------------------------------------------------------- Lightica ----
feed_latest() {  # $1 = field of the newest Apple silicon release in the feed
  python3 -c 'import json,sys
r=[x for x in json.load(open(sys.argv[1])).get("releases",[]) if x.get("arch")=="arm64"]
print(r[0].get(sys.argv[2],"") if r else "")' "$LIGHTICA_FEED_DIR/releases.json" "$1" 2>/dev/null
}

# Only commits production already serves reach the feed: this runs after the
# release merge is pushed, from the commit the team client was just built from.
update_desktop() {
  local head published rc
  [ "$DESKTOP_RELEASE" = 1 ] || { log "Lightica: release step off (DESKTOP_RELEASE=$DESKTOP_RELEASE)"; return; }
  cd "$REPO_DIR" || return
  head="$(git rev-parse "origin/$TEAM_BRANCH")"
  published="$(feed_latest commit)"
  if [ -n "$published" ] && git cat-file -e "$published^{commit}" 2>/dev/null \
    && git diff --quiet "$published" "$head" -- "${DESKTOP_PATHS[@]}"; then
    log "Lightica: \`$(feed_latest version)\` is up to date with $TEAM_BRANCH $(git rev-parse --short "$head")"
    cd "$OPS_HOME"; return
  fi
  if [ "$DRY_RUN" = 1 ]; then
    log "dry-run: would publish Lightica from $TEAM_BRANCH $(git rev-parse --short "$head") (feed: ${published:0:9})"
    cd "$OPS_HOME"; return
  fi
  if [ "$(git rev-parse HEAD)" != "$head" ]; then
    log "Lightica: ops checkout is at $(git rev-parse --short HEAD), not origin/$TEAM_BRANCH — not publishing"
    cd "$OPS_HOME"; return
  fi
  log "Lightica: publishing from $TEAM_BRANCH $(git rev-parse --short "$head") (feed: ${published:0:9})"
  LIGHTICA_FEED_DIR="$LIGHTICA_FEED_DIR" bash lightsoft/desktop/release.sh; rc=$?
  if [ "$(feed_latest commit)" = "$head" ]; then
    changed "Lightica \`$(feed_latest version)\` 게시 ($TEAM_BRANCH $(git rev-parse --short "$head")) — 설치된 앱이 1시간 안에 받아서 업데이트 알림을 띄움"
    [ $rc = 0 ] || failed "Lightica 게시는 됐지만 설치 안내 이슈 갱신 실패 — release.sh 로그 확인"
  else
    failed "Lightica 빌드·게시 실패 ($TEAM_BRANCH $(git rev-parse --short "$head")) — 피드는 \`$(feed_latest version)\` 그대로"
  fi
  cd "$OPS_HOME"
}

# ---------------------------------------------------------------- report ----
backend_desc() {
  if fork_backend; then
    printf '포크 backend `%s`' "$(docker inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$BACKEND_CONTAINER" 2>/dev/null || echo '?')"
  else
    printf 'upstream backend'
  fi
}

report() {
  local body="$STATE_DIR/report-$TS.md" prod
  prod="$(env_get MULTICA_IMAGE_TAG)"
  {
    if [ ${#FAILURES[@]} -gt 0 ]; then echo "## ⚠️ 운영 Multica 일일 업데이트 — 실패 ($(date '+%F %H:%M'))"; else echo "## 운영 Multica 일일 업데이트 ($(date '+%F %H:%M'))"; fi
    echo
    for c in ${CHANGES[@]+"${CHANGES[@]}"}; do echo "- ✅ $c"; done
    for f in ${FAILURES[@]+"${FAILURES[@]}"}; do echo "- ❌ $f"; done
    echo
    echo "현재 운영: \`$prod\` ($(backend_desc)) · 로그: \`$LOG\`"
  } > "$body"
  if [ ${#FAILURES[@]} -gt 0 ] && [ -n "$FAILURE_PROJECT" ]; then
    local args=(issue create --title "운영 Multica 일일 업데이트 실패 ($(date +%F))" --project "$FAILURE_PROJECT" --description-file "$body" --priority high)
    [ -n "$FAILURE_ASSIGNEE_ID" ] && args+=(--assignee-id "$FAILURE_ASSIGNEE_ID" --status todo)
    multica "${args[@]}" --output table || osascript -e 'display notification "운영 Multica 일일 업데이트 실패 — ~/.multica/ops/logs 확인" with title "Multica"' || true
  elif [ ${#CHANGES[@]} -gt 0 ] && [ -n "$REPORT_ISSUE" ]; then
    multica issue comment add "$REPORT_ISSUE" --content-file "$body" --output table || true
  fi
  rm -f "$body"
}

if prepare_repo; then
  mirror_fork_main
  mirror_release_tags
  update_production
  update_team_client
  update_desktop
else
  failed "ops checkout ($REPO_DIR) fetch/clone failed"
fi
[ "$DRY_RUN" = 1 ] || report
log "=== done: ${#CHANGES[@]} changed, ${#FAILURES[@]} failed ==="
[ ${#FAILURES[@]} -eq 0 ]
