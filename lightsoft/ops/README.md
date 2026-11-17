# 운영 Multica 일일 자동 업데이트

`daily-update.sh`는 Mac Studio에서 launchd로 매일 05:00에 실행됩니다. Mac이 잠들어 있었다면 깨어날 때 실행됩니다.

| 순서 | 대상 | 동작 |
|---|---|---|
| 1 | 운영 Multica (`~/.multica/server`) | upstream 최신 **릴리스**가 현재 `MULTICA_IMAGE_TAG`보다 새것이고 ghcr 이미지가 올라와 있으면 진행합니다. `pg_dump` 백업과 검증 → `.env` 백업 → 태그 고정 → backend·web pull 후 재생성 → `/health` commit 확인 순서입니다. 성공하면 현재·직전 버전 이미지만 남깁니다. 실패하면 이미지를 이전 태그로 되돌리고, DB는 자동 복구하지 않습니다. |
| 2 | 포크 `main` | upstream `main`으로 fast-forward합니다. 갈라졌으면 실패로 보고합니다. |
| 3 | `lightsoft/dev` | 운영이 쓰는 릴리스 태그를 `--no-ff` merge하고 push합니다. 충돌이 나면 abort하고 실패로 보고합니다. |
| 4 | 팀 클라이언트 | `lightsoft/dev` HEAD가 마지막 빌드와 다르거나 컨테이너가 멈춰 있으면 `docker compose -f lightsoft/docker-compose.team-web.yml up -d --build`를 실행합니다. |
| 5 | 리포트 | 변경이 있으면 리포트 이슈(LIGH-101)에 코멘트를 남깁니다. 실패하면 전담 에이전트에게 새 이슈를 할당합니다. 서버가 죽어 있으면 macOS 알림을 띄웁니다. |

운영과 팀 클라이언트는 같은 릴리스를 기준으로 합니다. 그래서 팀 클라이언트가 운영 API보다 앞서 404가 나는 일이 없습니다. upstream `main`의 미출시 기능은 다음 릴리스와 함께 들어옵니다.

## 파일 위치 (Mac Studio)

| 경로 | 내용 |
|---|---|
| `~/.multica/ops/daily-update.sh` | 실행본 (이 파일의 사본) |
| `~/.multica/ops/config.env` | `REPORT_ISSUE`, `FAILURE_PROJECT`, `FAILURE_ASSIGNEE_ID` 등 |
| `~/.multica/ops/multica-custom/` | 이 작업 전용 clone. 사람이 작업하는 폴더와 분리돼 있습니다. |
| `~/.multica/ops/logs/daily-update-YYYYMMDD.log` | 실행 로그 |
| `~/.multica/ops/state/team-web.built` | 마지막으로 빌드한 `lightsoft/dev` 커밋 |
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

## 수동 롤백

```bash
cd ~/.multica/server
sed -i '' 's|^MULTICA_IMAGE_TAG=.*|MULTICA_IMAGE_TAG=<이전 버전>|' .env
docker compose -f docker-compose.selfhost.yml up -d --no-deps backend frontend
# 스키마까지 되돌려야 하면 (운영 데이터 덮어씀 — 신중히):
# docker exec -i multica-postgres-1 sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists' < ~/.multica/backups/<dump>
```
