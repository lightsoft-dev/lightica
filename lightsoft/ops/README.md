# 운영 Multica 일일 자동 업데이트

`daily-update.sh`는 Mac Studio에서 launchd로 매일 05:00에 실행됩니다. Mac이 잠들어 있었다면 깨어날 때 실행됩니다.

| 순서 | 대상 | 동작 |
|---|---|---|
| 1 | 포크 `main`·릴리스 태그 | upstream `main`으로 fast-forward합니다. 갈라졌으면 실패로 보고합니다. 이어서 포크에 없는 upstream 릴리스 태그(`v*`)를 push합니다. 포크에 이미 있는 태그는 옮기지 않습니다. |
| 2 | 운영 Multica (`~/.multica/server`) + `lightsoft/dev` | upstream 최신 **릴리스**가 현재 `MULTICA_IMAGE_TAG`보다 새것이고 ghcr 이미지(포크 backend면 web만)가 올라와 있으면 그 릴리스로 올립니다. 순서는 아래와 같습니다. |
| 3 | 팀 클라이언트 | `lightsoft/dev` HEAD가 마지막 빌드와 다르거나 컨테이너가 멈춰 있으면 `docker compose -f lightsoft/docker-compose.team-web.yml up -d --build`를 실행합니다. |
| 4 | 리포트 | 변경이 있으면 리포트 이슈(LIGH-101)에 코멘트를 남깁니다. 실패하면 전담 에이전트에게 새 이슈를 할당합니다. 서버가 죽어 있으면 macOS 알림을 띄웁니다. |

2단계는 운영 backend가 무엇이냐에 따라 다릅니다. `~/.multica/server/.env`의 `MULTICA_BACKEND_IMAGE`가 `localhost/lightsoft/multica-backend`면 포크 backend, 아니면 upstream backend입니다(`lightsoft/README.md` "포크 backend"). 운영 웹(:3443)은 어느 쪽이든 upstream 이미지입니다.

| 단계 | 포크 backend | upstream backend |
|---|---|---|
| a. merge | 릴리스를 `lightsoft/dev`에 `--no-ff` merge합니다. 아직 push하지 않습니다. **충돌하면 abort하고, 운영은 그대로 둔 채 실패로 보고합니다.** | 같습니다. 충돌하면 실패로 보고하지만 업그레이드는 계속합니다. |
| b. 빌드 | merge한 HEAD로 `lightsoft/backend/build.sh`를 실행합니다. 새 릴리스가 없어도 `server/`·`Dockerfile`·`docker/`가 실행 중인 커밋과 다르면 다시 빌드합니다. **빌드가 실패하면 운영은 그대로 둡니다.** | 없음 |
| c. 백업 | `pg_dump` 백업과 검증, `.env` 백업 | 같음 |
| d. 전환 | 태그를 고정하고 web만 pull한 뒤 backend·web을 다시 만듭니다. `/health`의 commit이 빌드한 HEAD인지 확인합니다. | backend·web을 pull한 뒤 다시 만들고, `/health` commit이 릴리스 커밋인지 확인합니다. |
| e. push | 운영이 healthy해지면 merge를 push합니다. | 같음 |
| 실패 시 | 이미지를 이전 태그로 되돌립니다(같은 릴리스 재빌드면 운영 태그를 이전 이미지로 다시 붙임). DB는 자동 복구하지 않습니다. merge는 push하지 않습니다. 그 트리는 다음 날 다시 배포하지 않습니다. | 이미지를 이전 태그로 되돌립니다. DB는 자동 복구하지 않습니다. |

성공하면 현재·직전 버전 이미지(포크는 최근 빌드 5개 포함)만 남깁니다.

운영과 팀 클라이언트는 같은 릴리스를 기준으로 합니다. merge는 운영이 그 릴리스로 올라간 뒤에 push하므로, 팀 클라이언트가 운영 API보다 앞서 404가 나는 일이 없습니다. upstream `main`의 미출시 기능은 다음 릴리스와 함께 들어옵니다.

`lightsoft/dev`가 merge하는 대상은 포크 `main`이 아니라 릴리스 태그입니다. 포크 `main`은 upstream에 무엇이 들어오는지 보는 참고용 미러입니다. 릴리스 태그를 포크에도 올려 두므로, 포크만 clone한 체크아웃에서도 upstream remote 없이 `git merge vX.Y.Z`로 수동 merge할 수 있습니다.

주의: upstream의 `.github/workflows/release.yml`은 `v*.*.*` 태그 push로 실행되고, 일부 job(Docker 이미지 빌드·ghcr push)은 저장소 소유자를 가리지 않습니다. 지금 포크는 GitHub Actions가 한 번도 실행되지 않은 상태(포크 워크플로 미활성)라 태그를 push해도 아무것도 돌지 않습니다. 포크에서 Actions를 켜게 되면 먼저 `gh workflow disable release.yml --repo lightsoft-dev/multica`로 Release 워크플로를 꺼 두세요.

## 파일 위치 (Mac Studio)

| 경로 | 내용 |
|---|---|
| `~/.multica/ops/daily-update.sh` | 실행본 (이 파일의 사본) |
| `~/.multica/ops/config.env` | `REPORT_ISSUE`, `FAILURE_PROJECT`, `FAILURE_ASSIGNEE_ID` 등 |
| `~/.multica/ops/multica-custom/` | 이 작업 전용 clone. 사람이 작업하는 폴더와 분리돼 있습니다. |
| `~/.multica/ops/logs/daily-update-YYYYMMDD.log` | 실행 로그 |
| `~/.multica/ops/state/team-web.built` | 마지막으로 빌드한 `lightsoft/dev` 커밋 |
| `~/.multica/ops/state/fork-backend.failed` | 헬스 확인에 실패해 롤백된 포크 backend 트리. 같은 트리는 다시 배포하지 않습니다. 지우면 다음 실행에서 다시 시도합니다. |
| `localhost/lightsoft/multica-backend:*` (docker 이미지) | 포크 backend 빌드. `vX.Y.Z` = 운영이 쓰는 태그, `X.Y.Z-YYYYMMDD[.N]` = 빌드별 |
| `~/.multica/backups/multica-<버전>-<시각>.dump` | 업그레이드 직전 DB 백업 (최근 14개 보관) |
| `~/Library/LaunchAgents/dev.lightsoft.multica-daily-update.plist` | launchd 등록 |

## 설치 / 갱신

```bash
cp lightsoft/ops/daily-update.sh ~/.multica/ops/daily-update.sh && chmod +x ~/.multica/ops/daily-update.sh
sed "s|__HOME__|$HOME|g" lightsoft/ops/dev.lightsoft.multica-daily-update.plist \
  > ~/Library/LaunchAgents/dev.lightsoft.multica-daily-update.plist
launchctl bootout gui/$(id -u)/dev.lightsoft.multica-daily-update 2>/dev/null
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.lightsoft.multica-daily-update.plist
```

- 지금 바로 한 번 실행: `launchctl kickstart gui/$(id -u)/dev.lightsoft.multica-daily-update`
- 실제로 바꾸지 않고 확인만: `DRY_RUN=1 bash ~/.multica/ops/daily-update.sh; tail -30 ~/.multica/ops/logs/daily-update-$(date +%Y%m%d).log`
- 중지: `launchctl bootout gui/$(id -u)/dev.lightsoft.multica-daily-update`

스크립트가 `lightsoft/backend/build.sh`를 ops checkout에서 실행하므로, 설치본을 갱신하기 전에 그 스크립트가 들어 있는 `lightsoft/dev`가 push돼 있어야 합니다.

## 수동 롤백

릴리스 버전을 되돌릴 때입니다. 포크 backend를 직전 빌드나 upstream 이미지로 되돌리는 방법은 `lightsoft/README.md` "포크 backend"에 있습니다.

```bash
cd ~/.multica/server
sed -i '' 's|^MULTICA_IMAGE_TAG=.*|MULTICA_IMAGE_TAG=<이전 버전>|' .env
# 포크 backend면 localhost/lightsoft/multica-backend:<이전 버전> 이미지가 남아 있어야 합니다 (docker images로 확인)
docker compose -f docker-compose.selfhost.yml up -d --no-deps backend frontend
# 스키마까지 되돌려야 하면 (운영 데이터 덮어씀 — 신중히):
# docker exec -i multica-postgres-1 sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists' < ~/.multica/backups/<dump>
```
