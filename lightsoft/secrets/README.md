# 시크릿 매니저 (LIGH-112)

구성원이 쓰는 시크릿(API 키, 토큰, 비밀번호)을 저장해 두고, 권한을 받은 사람과 에이전트만 꺼내 쓰게 하는 서비스입니다. 팀 클라이언트의 **팀 → 시크릿** 메뉴에서 관리합니다.

```
브라우저 (:3444/{워크스페이스}/secrets)          에이전트 (lightsoft-secrets CLI)
  └─ tailscale serve :3444 /lightsoft-secrets → 127.0.0.1:13300  lightsoft-secrets-server (launchd)
                                                  ├─ 사람: multica_auth 쿠키를 운영 API(127.0.0.1:8080)로 확인
                                                  ├─ 에이전트: 연결 토큰(lss_) + 태스크 토큰(MULTICA_TOKEN)
                                                  └─ ~/.multica/secrets/secrets.db  (값은 AES-256-GCM 암호문)
```

- 운영 backend·DB·`~/.multica/server/.env`는 바꾸지 않습니다. 서비스는 호출한 사람이나 에이전트의 자격증명으로 운영 API를 읽고, 에이전트를 연결할 때만 upstream `PUT /api/agents/{id}/env`로 그 에이전트의 custom_env에 토큰을 씁니다.
- upstream 코드와 섞이지 않게 이 폴더에 별도 Go 모듈로 있습니다. 화면은 `apps/web/features/lightsoft/secrets/`, 경로는 `apps/web/app/[workspaceSlug]/(dashboard)/secrets/`입니다. upstream 파일은 `packages/core/paths/spaces.ts`(한 줄)와 `packages/views/layout/panels/team-panel.tsx`(메뉴 링크)만 고쳤습니다.

## 규칙

| 항목 | 동작 |
|---|---|
| 이름 | 워크스페이스 안에서 유일합니다. 영문 대문자로 시작하고 대문자·숫자·`_`만 씁니다(예: `ZIMIN_GITHUB_TOKEN`). |
| 관리 | 값·설명 수정, 권한 변경, 삭제는 만든 사람과 워크스페이스 owner/admin이 할 수 있습니다. 시크릿은 워크스페이스 구성원 누구나 만들 수 있습니다. |
| 읽기 | 만든 사람, 그리고 권한을 받은 구성원·에이전트·스쿼드(스쿼드에 속한 사람·에이전트)만 읽을 수 있습니다. owner/admin도 권한이 없으면 값을 볼 수 없습니다. 자기 자신에게 권한을 줄 수는 있고, 이 변경은 기록에 남습니다. |
| 에이전트 연결 | 에이전트 owner나 owner/admin이 **에이전트 연결** 탭에서 연결합니다. 서비스가 에이전트 전용 토큰을 만들어 custom_env에 `LIGHTSOFT_SECRETS_TOKEN`·`LIGHTSOFT_SECRETS_URL`로 넣습니다(다른 키는 그대로 둡니다). 다음 실행부터 적용됩니다. 재발급하면 이전 토큰은 바로 막히고, 연결을 해제하면 토큰을 폐기하고 두 키를 지웁니다. |
| 에이전트 인증 | 연결 토큰과 함께 그 태스크의 `MULTICA_TOKEN`이 있어야 합니다. 서비스는 이 토큰으로 운영 API를 읽어, 살아 있는 태스크이고 같은 워크스페이스인지 확인합니다. 사람용 화면 API는 로그인 쿠키(JWT)만 받고 `mat_`·`mul_` 토큰은 거부합니다. |
| 암호화 | 값마다 nonce를 새로 만들어 AES-256-GCM으로 암호화합니다. 추가 데이터(AAD)에 워크스페이스·시크릿 ID·버전을 넣어서, 행을 바꿔치기하면 복호화가 실패합니다. 목록 응답·로그에는 값이 나오지 않습니다. |
| 기록 | 생성·수정·삭제·권한 변경·값 보기(사람)·에이전트 조회·거부·연결·해제를 `audit` 테이블에 남깁니다. 값은 기록하지 않습니다. 값을 내주기 전에 기록부터 쓰고, 기록에 실패하면 값을 내주지 않습니다. |
| 기록 보기 | **에이전트 접근 기록** 탭에서 봅니다. owner/admin은 워크스페이스 전체를 볼 수 있습니다. 다른 구성원은 자기가 만든 시크릿에 대한 기록, 자기 행동, 자기 에이전트의 행동만 볼 수 있습니다. |

## 에이전트에서 쓰기

```bash
lightsoft-secrets list                                   # 이 에이전트가 읽을 수 있는 이름
lightsoft-secrets exec GITHUB_TOKEN -- gh repo list      # 값을 출력하지 않고 env로만 넣어 실행 (권장)
lightsoft-secrets exec --env GITHUB_TOKEN=ZIMIN_GITHUB_TOKEN -- gh pr list   # env 이름 바꿔 넣기
lightsoft-secrets get OPENAI_API_KEY                     # 값을 stdout으로 (로그에 남지 않게 주의)
```

- Mac Studio에서는 `install.sh`가 CLI를 `~/.local/bin/lightsoft-secrets`에 설치합니다.
- 다른 맥의 데몬에서 도는 에이전트는 CLI 없이 HTTP로 부릅니다.
  ```bash
  curl -fsS -H "Authorization: Bearer $LIGHTSOFT_SECRETS_TOKEN" -H "X-Multica-Token: $MULTICA_TOKEN" \
    -H "X-Task-ID: $MULTICA_TASK_ID" "$LIGHTSOFT_SECRETS_URL/v1/secrets/NAME"   # → {"name","value"}
  ```
- 권한이 없는 이름과 없는 이름은 똑같이 404로 답합니다. 에이전트가 이름을 하나씩 대 보며 확인할 수 없게 하려는 것입니다. 거부된 조회도 기록에 남습니다.

## 설치 / 갱신 (Mac Studio)

```bash
bash lightsoft/secrets/install.sh     # 처음이면 마스터 키 생성 → 빌드 → launchd 등록 → CLI 설치 → health 확인
# 처음 한 번: :3444에 경로 추가 (기존 /, /api, /lightsoft-term 등은 그대로)
tailscale serve --bg --https=3444 --set-path=/lightsoft-secrets http://127.0.0.1:13300/lightsoft-secrets
```

- **마스터 키**: `~/.multica/secrets/master.key`(0600, base64 32바이트)입니다. 처음 설치한 뒤 **사람이 직접** `cat ~/.multica/secrets/master.key`로 열어 인쇄하거나 오프라인에 보관하세요. 키를 잃으면 저장된 값을 복구할 수 없습니다. 키 파일 권한이 0600보다 넓으면 서비스가 시작하지 않습니다.
- **백업**: `~/.multica/secrets/secrets.db`(암호문)를 복사해 두면 됩니다. 키는 DB 백업과 따로 보관합니다. 복원할 때는 DB와 키를 같은 자리에 두고 `install.sh`를 다시 실행합니다.
- 다시 설치해도 저장된 값과 기록은 그대로입니다. 키 파일이 이미 있으면 새로 만들지 않습니다.
- 로그: `~/.multica/secrets/logs/service.log`(메서드·경로·상태만 남고 값·토큰은 남지 않음)
- 중지: `launchctl bootout gui/$(id -u)/dev.lightsoft.multica-secrets`
- 노출만 끄기: `tailscale serve --https=3444 --set-path=/lightsoft-secrets off`

설정은 `LIGHTSOFT_SECRETS_*` 환경 변수로 바꿉니다(`config.go`): `_ADDR`, `_API_URL`, `_ALLOWED_ORIGINS`, `_DATA_DIR`, `_DB`, `_KEY_FILE`, `_PUBLIC_URL`.

## 알려진 한계

- Mac Studio의 에이전트는 모두 사용자 zimin의 셸 권한으로 돕니다. 그래서 `docker exec`로 운영 DB(에이전트 custom_env 포함)를 읽거나 `~/.multica/secrets`의 파일을 직접 읽을 수 있습니다. 이 서비스가 막는 것은 정상 경로(화면, API, CLI)로 권한 없는 사람이나 에이전트가 값을 얻는 경우까지입니다. 같은 맥에서 악의적으로 동작하는 프로세스는 막지 못합니다. (LIGH-112에서 합의한 범위)
- 연결 토큰은 운영 DB의 custom_env에 평문으로 저장됩니다. 그래서 그 에이전트의 owner와 워크스페이스 owner/admin은 upstream `agent env`로 토큰을 꺼내 볼 수 있습니다. 이때 upstream 감사 기록이 남습니다.

## 개발

```bash
cd lightsoft/secrets
go vet ./... && go test ./...    # 가짜 Multica API로 권한·암호화·기록·연결·실패 시 차단을 확인
```

로컬 웹(`make web-dev`, :13100)에서 시험할 때는 서비스를 임시 폴더와 다른 포트로 띄우고(`LIGHTSOFT_SECRETS_DATA_DIR=$(mktemp -d)`, `LIGHTSOFT_SECRETS_ADDR=127.0.0.1:13301`, 키 파일은 `openssl rand -base64 32`로 만들고 0600), 웹은 `NEXT_PUBLIC_LIGHTSOFT_SECRETS_URL=http://localhost:13301/lightsoft-secrets`로 실행합니다. 기본 허용 Origin에 `http://localhost:13100`이 들어 있습니다.
