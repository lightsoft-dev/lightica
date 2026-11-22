# Lightsoft 팀 웹 클라이언트

이 브랜치(`lightsoft/dev`)는 팀 전용 기능을 넣는 작업 브랜치입니다. 기준은 **운영 Multica가 쓰는 upstream 릴리스 태그**입니다.
매일 05:00 자동 작업이 upstream 최신 릴리스를 이 브랜치에 merge하고, 이 브랜치로 운영 backend를 다시 빌드해 올립니다(`lightsoft/ops/README.md`). upstream `main`은 이 브랜치에 merge하지 마세요. 운영 API보다 앞서서 404가 납니다.
팀 웹 클라이언트는 따로 띄우고, **API와 DB는 Mac Studio에서 도는 운영 Multica를 씁니다.** 운영 backend 이미지는 이 브랜치로 빌드한 것입니다(아래 "포크 backend").

| | 주소 | 내용 |
|---|---|---|
| 운영 (원본) | https://mac-studio.taile83671.ts.net:3443 | self-host Multica (compose 프로젝트 `multica`) |
| **팀 클라이언트** | https://mac-studio.taile83671.ts.net:3444 | 이 브랜치로 빌드한 웹 (compose 프로젝트 `multica-team-web`) |
| 팀 클라이언트 (tailnet 밖) | 예: https://lightica.lightsoft.dev | 같은 팀 웹. Cloudflare Tunnel + Access 뒤, Access 계정으로 자동 로그인 (`lightsoft/access/README.md`, LIGH-253) |

쿠키가 같은 호스트에 저장되므로, 운영(:3443)에 로그인했다면 팀 클라이언트(:3444)에서도 로그인된 상태입니다.

## 구성

```
브라우저 ──https──▶ tailscale serve :3444
                    ├─ /                            → 127.0.0.1:13000  (multica-team-web-web-1, 이 브랜치의 Dockerfile.web 빌드)
                    ├─ /api /ws /v1 /health         → 127.0.0.1:8080   (운영 backend, 이 브랜치로 빌드한 이미지)
                    ├─ /lightsoft-term              → 127.0.0.1:13200  (프로젝트 터미널 게이트웨이, launchd)
                    ├─ /lightsoft-secrets           → 127.0.0.1:13300  (시크릿 매니저, launchd)
                    ├─ /lightsoft-usage             → 127.0.0.1:13500  (구독 한도, launchd. LIGH-113)
                    └─ /lightica                    → 127.0.0.1:13400  (Lightica 릴리스 피드, launchd. LIGH-191)
```

- tailnet 밖 접속(Cloudflare Tunnel + Access, Access 계정으로 자동 로그인)은 `lightsoft/access/README.md`에 설정 순서·로그인 규칙·끄는 법이 있습니다. 브라우저 전용이고, 터미널은 그 경로로 열지 않습니다.
- 프로젝트 터미널(owner/admin 전용)은 `lightsoft/terminal/README.md`에 설치·규칙이 있습니다. 팀 클라이언트 이미지와 별개로 `bash lightsoft/terminal/install.sh`로 갱신합니다.
- 팀 데스크톱 앱 **Lightica**(팀 클라이언트 서버에 붙는 서명·공증된 macOS 앱)의 빌드·배포는 `lightsoft/desktop/README.md`에 있습니다. 릴리스는 `lightsoft/desktop/release.sh`가 `/lightica/` 피드에 게시하고, 설치된 앱은 그 피드에서 새 버전을 찾습니다.
- 팀 빌드 버전은 `<upstream 릴리스>-<YYYYMMDD>[.N]`(예: `0.11.0-20261119`)이고 `lightsoft/version.sh` 한 곳에서 계산합니다. Lightica와 번들 CLI가 이 버전을 씁니다.
- 구독 한도(팀 → 구독 한도)는 `lightsoft/usage/README.md`에 설치·계정 설정·규칙이 있습니다. Claude Max와 Codex 계정의 세션·주간 한도 사용률과 추이를 보여 줍니다. 서비스는 `bash lightsoft/usage/install.sh`로 갱신하고, 화면은 팀 클라이언트 이미지에 들어 있습니다.
- 시크릿 매니저(팀 → 시크릿)는 `lightsoft/secrets/README.md`에 설치·규칙·에이전트 사용법이 있습니다. 서비스는 `bash lightsoft/secrets/install.sh`로 갱신하고, 화면은 팀 클라이언트 이미지에 들어 있습니다. 마스터 키(`~/.multica/secrets/master.key`)는 DB와 따로 오프라인에 보관합니다.

- 컨테이너 안에서 서버 측 API 요청은 `REMOTE_API_URL=http://host.docker.internal:8080`으로 보냅니다. 운영 compose 네트워크에는 붙지 않습니다.
- 운영 backend는 WebSocket Origin을 `~/.multica/server/.env`의 `CORS_ALLOWED_ORIGINS`로 검사합니다. 여기에 `https://mac-studio.taile83671.ts.net:3444`, `http://localhost:13000`, `http://localhost:13100`을 추가했습니다(LIGH-99).

## 변경 사항 배포 (팀 클라이언트 갱신)

`lightsoft/dev`에 push하면 다음 날 05:00 자동 작업이 다시 빌드합니다. 바로 반영하려면:

```bash
cd ~/lightsoft-projects/multica-custom      # lightsoft/dev 브랜치
git pull
TEAM_WEB_VERSION="lightsoft-dev-$(git rev-parse --short HEAD)" \
  docker compose -f lightsoft/docker-compose.team-web.yml up -d --build
```

- 멈추기: `docker compose -f lightsoft/docker-compose.team-web.yml down`
- 로그 보기: `docker logs -f multica-team-web-web-1`
- Tailscale 노출 해제: `tailscale serve --https=3444 off`

## 로컬 개발 (hot reload)

```bash
make web-dev    # http://localhost:13100, API는 운영 127.0.0.1:8080
```

- 이 설정은 로컬 전용 `.env`(git 제외)에 들어 있습니다: `REMOTE_API_URL=http://127.0.0.1:8080`, `FRONTEND_PORT=13100`.
- `pnpm dev:web`만 실행하면 3000번으로 떠서 운영 웹과 겹칩니다. 반드시 `make web-dev`로 실행하세요.

## 팀 기능

### 채팅 모델·effort 선택 (LIGH-104)

- 채팅 입력창에서 그 채팅에만 적용할 모델과 effort를 고릅니다. 에이전트 기본 설정은 바뀌지 않습니다. 대상 런타임은 Claude Code, Codex, pi입니다.
- 고른 값은 다음 메시지를 보낼 때 그 태스크의 `context`(`lightsoft_model_selection` 키)에 기록됩니다. 이후 메시지는 같은 세션의 마지막 값을 따릅니다. DB 스키마는 바꾸지 않습니다. 데몬도 바꾸지 않습니다. 서버가 claim 응답의 `model`/`thinking_level`을 이 값으로 바꿔 줍니다.
- **포크 backend가 있어야 동작합니다.** 운영 backend가 upstream 이미지면 `GET /api/lightsoft/capabilities`가 404를 돌려주고, 선택기가 숨겨집니다(아래 "포크 backend").
- 포크 코드는 새 파일에 모았습니다: `server/internal/{handler,service}/lightsoft_chat_model.go`, `server/pkg/db/queries/lightsoft_chat_model.sql`, `packages/core/chat/model-selection.ts`, `packages/core/api/lightsoft-chat-model-selection.ts`, `packages/views/chat/components/chat-model-selection-picker.tsx`.
- upstream 파일에는 연결 코드만 몇 줄 넣었습니다: `chat.go`, `task.go`, `daemon.go`, `router.go`, `client.ts`, `use-chat-controller.ts`, `chat-page.tsx`, `chat-window.tsx`. 릴리스 merge에서 충돌이 나면 이 지점부터 확인하세요.

### 외부 협업자 (LIGH-282)

고객사·협력사 사람에게 **열어 준 프로젝트와 그 이슈만** 보여 주는 계정입니다. 같은 워크스페이스에 두고, 기존 공개 프로젝트든 새 프로젝트든 열 수 있습니다. 제한은 포크 backend가 겁니다(API·CLI·WebSocket·검색 모두). 화면에서 메뉴를 숨기는 것은 편의일 뿐입니다.

**외부 협업자가 할 수 있는 것**
- 열어 준 프로젝트(그 프로젝트의 멤버로 추가된 것)와 그 이슈를 보고, 이슈 생성·수정, 코멘트, 첨부, 반응, 구독, 인박스를 씁니다. 이슈는 그 프로젝트 안에서만 만들고 옮길 수 있습니다.
- 이름·아바타 수준으로 팀원과 에이전트가 보입니다(코멘트 작성자와 담당자 표시용). 이메일, 에이전트 지시문·설정은 보이지 않습니다.

**막히는 것**
- 열지 않은 프로젝트(공개 프로젝트 포함), 프로젝트가 없는 이슈, 그런 이슈의 알림·실시간 이벤트.
- 에이전트 실행: 할당·채팅·멘션·코멘트 트리거·워크플로 인계 단계로 상태 바꾸기가 모두 거부됩니다. 에이전트는 이 맥에서 팀원 권한으로 돌기 때문입니다.
- 에이전트·스쿼드 관리, 런타임, Autopilot, 스킬, 채팅, 대시보드, 워크스페이스 설정·초대, 프로젝트 생성·설정, 데몬 등록, 시크릿 매니저, 구독 한도, 터미널. 화면이 매번 불러오는 목록(런타임, 실행 현황 등)은 빈 결과로 응답합니다.
- 새로 생긴 API도 기본으로 막힙니다. 허용 목록은 `server/internal/handler/lightsoft_guest.go`의 `guestRoutes`입니다.

**추가 (owner/admin)**
1. 팀 클라이언트 **설정 → 외부 협업자**에서 이메일을 등록합니다. 등록과 함께 워크스페이스 초대가 나갑니다. 등록을 먼저 해야 그 계정이 첫 요청부터 외부 협업자입니다. 일반 초대로 먼저 들어온 멤버를 나중에 등록해도 되지만, 그 사이에는 일반 멤버 권한입니다.
2. tailnet 밖에서 들어오므로 Cloudflare Access의 Lightica 앱에 그 이메일을 허용하는 정책을 추가합니다(`lightsoft/access/README.md`). 팀원용 재사용 정책에는 넣지 않습니다.
3. 상대가 `https://lightica.lightsoft.dev`에서 로그인해 초대를 수락하면, 같은 화면의 **프로젝트** 버튼으로 열어 줄 프로젝트를 고릅니다. 공개 프로젝트는 공개 그대로 두고 그 사람만 프로젝트 멤버로 추가됩니다. 프로젝트의 접근 설정 화면(비공개 프로젝트의 멤버 목록)에서 추가해도 같습니다.

API로 할 때(owner/admin 토큰, `X-Workspace-ID` 헤더): `GET/POST /api/lightsoft/guests`(`{"email": ...}`), `DELETE /api/lightsoft/guests?email=...[&promote=1]`. 프로젝트는 `POST /api/projects/{id}/members`(`{"user_ids": [...]}`).

**회수**
- 프로젝트 하나만 닫기: 같은 **프로젝트** 버튼에서 체크를 끕니다. 바로 404가 됩니다.
- 전부 회수: 설정 → 멤버에서 워크스페이스에서 내보냅니다. 외부 협업자 목록에는 남겨 두므로 나중에 다시 초대해도 외부 협업자입니다. Cloudflare Access 정책에서도 이메일을 뺍니다.
- 목록에서 빼기: 워크스페이스 멤버가 아닐 때만 그냥 지워집니다. 멤버인 사람을 빼면 **정식 멤버로 전환**(모든 공개 프로젝트·에이전트 접근)이라 확인을 한 번 더 받습니다.

**저장 방식과 주의**
- DB 스키마는 바꾸지 않았습니다. 목록은 `workspace.settings.lightsoft_guests`(소문자 이메일 배열)에 있고, member의 role은 `member` 그대로입니다. 워크스페이스 설정 저장(`PATCH /api/workspaces/{id}`)은 이 키를 바꾸지 못하고 응답에서도 빠집니다. 바꾸는 길은 위 API 하나입니다.
- **upstream backend로 롤백하면 외부 협업자는 일반 멤버가 됩니다.** 롤백 전에 외부 협업자를 워크스페이스에서 먼저 내보냅니다.
- 시크릿 매니저와 구독 한도 서비스는 role `guest`를 거부하도록 고쳤습니다. 이 버전을 설치(`bash lightsoft/secrets/install.sh`, `bash lightsoft/usage/install.sh`)하기 전에는 외부 협업자를 지정하지 않습니다.
- 외부 협업자 계정은 어느 워크스페이스에서든 데몬을 등록하거나 메신저 봇을 연결할 수 없습니다(자기 워크스페이스 포함).
- 외부 협업자가 직접 시작하는 실행은 모두 막지만, **팀원이 걸어 둔 자동화는 그 팀원 권한으로 돕니다.** 예를 들어 깨우기 조건이나 "하위 이슈가 끝나면" 규칙이 있는 이슈에서 외부 협업자가 하위 이슈를 닫으면 에이전트가 실행될 수 있습니다. 외부 협업자에게 연 프로젝트에는 에이전트 자동화를 걸지 않는 것을 권합니다.
- 기존 멤버를 외부 협업자로 바꾸면, 그 사람이 이미 만든 메신저 연결·Autopilot·에이전트·데몬은 남습니다. 외부 협업자는 새로 초대한 계정으로 쓰고, 바꿔야 한다면 그런 연결을 먼저 정리합니다.
- 회귀 시나리오: 격리 backend(dev 로그인 코드 사용)에 대고 `node lightsoft/guest/scenario.mjs http://127.0.0.1:<port>`. 운영 주소에는 실행되지 않게 막혀 있습니다.

## 포크 backend (운영)

운영 backend 컨테이너(`multica-backend-1`)는 이 브랜치로 빌드한 이미지 `localhost/lightsoft/multica-backend`를 씁니다(LIGH-190). 운영 웹(:3443), DB, Tailscale 설정은 upstream 그대로입니다. 포크 backend는 upstream API에 팀 기능만 더한 것이라, 운영 웹과 모바일도 지금처럼 동작합니다.

- `~/.multica/server/.env`의 `MULTICA_BACKEND_IMAGE=localhost/lightsoft/multica-backend`가 스위치입니다. 일일 자동 작업도 이 값을 보고 포크 backend를 빌드할지 정합니다.
- 이미지에는 태그가 두 개 붙습니다.
  - `v0.11.0`: 운영 compose가 실제로 쓰는 이름입니다. compose는 backend와 web에 같은 `MULTICA_IMAGE_TAG`를 쓰므로, 포크 이미지도 upstream 릴리스 태그를 달아야 합니다. 다시 빌드하면 이 태그가 새 이미지로 옮겨 갑니다.
  - `0.11.0-YYYYMMDD[.N]`: 빌드마다 하나씩 붙고 옮겨지지 않습니다. 형식은 Lightica와 같고 `lightsoft/version.sh`가 계산합니다. 도움말 메뉴의 "서버 버전"과 `/api/config`의 `server_version`에 이 값이 보입니다. `/health`의 `commit`은 빌드한 이 브랜치 커밋입니다.
- 이름이 `localhost/`로 시작하는 것은 레지스트리에 올리지 않는 로컬 이미지라서입니다. `compose pull`로 받을 수 없으므로 실패하고, 남의 이미지를 받아 올 일도 없습니다. backend는 pull하지 말고 빌드합니다.
- 빌드 목록: `docker images localhost/lightsoft/multica-backend`. 일일 작업이 최근 5개 빌드와 현재·직전 릴리스 태그만 남깁니다.

```bash
bash lightsoft/backend/build.sh       # HEAD로 빌드 (server/ 변경이 커밋돼 있어야 함). 팀 버전을 출력
bash lightsoft/backend/verify.sh      # 격리 Postgres(127.0.0.1:5434) + backend(18080)에 띄워 API 스모크 후 정리
```

**배포.** `lightsoft/dev`의 `server/`·`Dockerfile`·`docker/`가 바뀌면 다음 05:00 작업이 다시 빌드해 올립니다. 프런트만 바뀌었으면 backend는 재시작하지 않습니다. 바로 올리려면 일일 작업을 한 번 실행합니다. 빌드, DB 백업, 전환, 헬스 확인, 실패 시 롤백까지 같은 절차로 진행됩니다.

```bash
launchctl kickstart gui/$(id -u)/dev.lightsoft.multica-daily-update
tail -f ~/.multica/ops/logs/daily-update-$(date +%Y%m%d).log
```

**롤백.**

```bash
cd ~/.multica/server
# 직전 포크 빌드로: 그 빌드의 팀 버전 태그를 운영 태그로 다시 붙인다
docker tag localhost/lightsoft/multica-backend:<이전 팀 버전> localhost/lightsoft/multica-backend:$(grep '^MULTICA_IMAGE_TAG=' .env | cut -d= -f2)
docker compose -f docker-compose.selfhost.yml up -d --no-deps backend
# upstream 이미지로: 스위치를 되돌린다 (스키마가 같아 DB 복원은 필요 없음)
sed -i '' 's|^MULTICA_BACKEND_IMAGE=.*|MULTICA_BACKEND_IMAGE=ghcr.io/multica-ai/multica-backend|' .env
docker compose -f docker-compose.selfhost.yml up -d --no-deps backend
curl -s http://127.0.0.1:8080/health   # commit 확인
```

upstream으로 되돌리면 팀 기능(모델·effort 선택기 등)이 숨겨지고, 일일 작업도 upstream 이미지를 받는 예전 방식으로 돌아갑니다. 다시 포크로 가려면 스위치를 `localhost/lightsoft/multica-backend`로 바꾸고 위 배포 명령을 실행합니다. 처음 전환할 때처럼 `build.sh`로 먼저 빌드해 두면 `up -d --no-deps backend`만으로도 됩니다.

**주의.**

- 서버 코드를 바꾸는 PR은 merge하면 다음 날 05:00에 운영 backend로 나갑니다. 운영 웹(:3443)과 모든 데몬이 이 backend를 씁니다.
- 포크에 DB 마이그레이션을 추가하지 않습니다. upstream 번호와 겹치고, upstream 이미지로 되돌리기 어려워집니다. 팀 기능은 LIGH-104처럼 스키마를 바꾸지 않고 만듭니다. 꼭 필요하면 구현 전에 설계를 올려 확인받습니다.
- 헬스 확인에 실패해 롤백된 트리는 다음 날 다시 배포하지 않습니다. 새 커밋을 올리거나 `~/.multica/ops/state/fork-backend.failed`를 지우면 다시 시도합니다.

## ⚠️ 주의

- **버전 맞춤**: 운영 backend와 이 브랜치는 같은 릴리스(현재 v0.11.0)를 기준으로 합니다. 운영 버전은 `~/.multica/server/.env`의 `MULTICA_IMAGE_TAG`에 고정돼 있고, 자동 작업이 함께 올립니다.
- **운영 DB에는 운영 backend 컨테이너만 붙입니다.** 그 이미지는 위 절차로만 만들고 바꿉니다. 개발용 backend(`go run ./cmd/server`, `cmd/migrate`)는 운영 DB에 붙이지 않습니다. 마이그레이션과 백그라운드 작업이 겹칩니다.
- 레포의 `docker-compose.yml`도 이름이 `multica`라서 운영 stack과 겹칩니다. `make dev`, `make setup*`, `make db-*`를 기본값으로 실행하지 마세요. 로컬 `.env`와 `docker-compose.override.yml`이 격리된 `multica-custom` 프로젝트(Postgres 5434)로 우회시키는 안전장치입니다.
- `localhost:8080`은 IPv6로 다른 프로세스가 잡고 있습니다. 운영 API는 항상 `127.0.0.1:8080`으로 지정하세요.
