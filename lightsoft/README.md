# Lightsoft 팀 웹 클라이언트

이 브랜치(`lightsoft/dev`)는 팀 전용 기능을 넣는 작업 브랜치입니다. 기준은 **운영 Multica가 쓰는 upstream 릴리스 태그**입니다.
매일 05:00 자동 작업이 운영을 upstream 최신 릴리스로 올리고, 같은 태그를 이 브랜치에 merge합니다(`lightsoft/ops/README.md`). upstream `main`은 이 브랜치에 merge하지 마세요. 운영 API보다 앞서서 404가 납니다.
웹 클라이언트만 따로 띄우고, **API와 DB는 Mac Studio에서 도는 운영 Multica를 그대로 씁니다.**

| | 주소 | 내용 |
|---|---|---|
| 운영 (원본) | https://mac-studio.taile83671.ts.net:3443 | self-host Multica (compose 프로젝트 `multica`) |
| **팀 클라이언트** | https://mac-studio.taile83671.ts.net:3444 | 이 브랜치로 빌드한 웹 (compose 프로젝트 `multica-team-web`) |

쿠키가 같은 호스트에 저장되므로, 운영(:3443)에 로그인했다면 팀 클라이언트(:3444)에서도 로그인된 상태입니다.

## 구성

```
브라우저 ──https──▶ tailscale serve :3444
                    ├─ /                            → 127.0.0.1:13000  (multica-team-web-web-1, 이 브랜치의 Dockerfile.web 빌드)
                    ├─ /api /ws /v1 /health         → 127.0.0.1:8080   (운영 backend, 고정 릴리스 태그)
                    ├─ /lightsoft-term              → 127.0.0.1:13200  (프로젝트 터미널 게이트웨이, launchd)
                    └─ /lightsoft-secrets           → 127.0.0.1:13300  (시크릿 매니저, launchd)
```

- 프로젝트 터미널(owner/admin 전용)은 `lightsoft/terminal/README.md`에 설치·규칙이 있습니다. 팀 클라이언트 이미지와 별개로 `bash lightsoft/terminal/install.sh`로 갱신합니다.
- 팀 데스크톱 앱 **Lightica**(팀 클라이언트 서버에 붙는 서명·공증된 macOS 앱)의 빌드·배포는 `lightsoft/desktop/README.md`에 있습니다.
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

## ⚠️ 주의

- **버전 맞춤**: 운영 backend와 이 브랜치는 같은 릴리스(현재 v0.10.0)를 기준으로 합니다. 운영 버전은 `~/.multica/server/.env`의 `MULTICA_IMAGE_TAG`에 고정돼 있고, 자동 작업이 함께 올립니다.
- **포크 backend를 운영 DB에 붙이지 않습니다.** 마이그레이션과 백그라운드 작업이 겹칩니다. 새 API나 DB가 필요한 기능은 운영 backend를 교체하는 일이므로 따로 계획합니다.
- 레포의 `docker-compose.yml`도 이름이 `multica`라서 운영 stack과 겹칩니다. `make dev`, `make setup*`, `make db-*`를 기본값으로 실행하지 마세요. 로컬 `.env`와 `docker-compose.override.yml`이 격리된 `multica-custom` 프로젝트(Postgres 5434)로 우회시키는 안전장치입니다.
- `localhost:8080`은 IPv6로 다른 프로세스가 잡고 있습니다. 운영 API는 항상 `127.0.0.1:8080`으로 지정하세요.
