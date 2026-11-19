# 구독 한도 (LIGH-113)

Mac Studio에서 에이전트가 쓰는 구독 계정(Claude Max, Codex)의 한도 소진율을 계속 기록하고, 팀 클라이언트(:3444)와 Lightica의 **팀 → 구독 한도** 화면에 보여 주는 서비스입니다.

```
브라우저 (:3444/{워크스페이스}/limits)   Lightica (팀 → 구독 한도)
  └─ tailscale serve :3444 /lightsoft-usage → 127.0.0.1:13500  lightsoft-usage-server (launchd)
                                                ├─ 인증: multica_auth 쿠키(웹)나 Bearer 세션(Lightica) → 운영 API(127.0.0.1:8080)로 구성원 확인
                                                ├─ 5분마다 읽기
                                                │   ├─ Codex: $CODEX_HOME/sessions/**/*.jsonl 의 최신 token_count.rate_limits
                                                │   └─ Claude: 키체인의 Claude Code 로그인 토큰 → api.anthropic.com/api/oauth/usage
                                                └─ ~/.multica/usage/history.jsonl  (스냅샷, 35일 보관)
```

- 운영 backend·DB·데몬은 바꾸지 않습니다. 서비스는 호출한 사람의 세션으로 `/api/me`와 워크스페이스 구성원 목록만 읽습니다.
- upstream v0.11.0에도 런타임 페이지에 한도를 보여 주는 기능이 있습니다(MUL-9427). 다만 그 값은 데몬이 작업 중에 보내야 쌓이고, 이 맥의 데몬(0.9.0)은 보내지 않습니다. 이 서비스는 데몬 버전과 상관없이 계정에서 직접 읽고, 추이를 남깁니다.
- 스냅샷은 upstream 런타임 한도와 같은 모양(`plan_type`, `windows[kind, used_percent, window_minutes, resets_at, status]`, `credits`, `observed_at`)입니다. 그래서 화면은 런타임 페이지의 한도 줄(`WindowRow`, `describeRateLimitWindow`)을 그대로 씁니다. 사용률 막대, 페이스 표시, 초기화까지 남은 시간이 런타임 페이지와 같습니다.
- 코드
  - 서비스: 이 폴더(별도 Go 모듈, 외부 의존성 없음)
  - 클라이언트: `packages/core/lightsoft/usage/`
  - 화면: `packages/views/lightsoft/usage/`
  - 경로: 웹은 `apps/web/app/[workspaceSlug]/(dashboard)/limits/`, Lightica는 `apps/desktop/src/renderer/src/routes.tsx`의 `limits`
- upstream 파일은 몇 줄만 고쳤습니다.
  - `packages/views/runtimes/components/rate-limit-section.tsx`: `WindowRow`에 `export` 추가
  - `packages/views/layout/panels/team-panel.tsx`: 메뉴 링크
  - `packages/core/paths/spaces.ts`: 한 줄
  - `packages/core/package.json`, `packages/views/package.json`: exports 한 줄씩
  - `apps/desktop/src/renderer/src/routes.tsx`: 경로 하나

## 규칙

| 항목 | 동작 |
|---|---|
| 조회 권한 | `accounts.json`의 `workspace_id` 워크스페이스 구성원이면 누구나 봅니다(역할 무관). 다른 워크스페이스 구성원에게는 계정 목록이 비어 있습니다. 세션 JWT만 받고 `mat_`·`mul_` 같은 기계용 토큰은 거부합니다. 읽기 전용 API라 쓰기 요청은 없습니다. |
| Codex | 계정의 `CODEX_HOME` 아래 최근 14일 폴더에서 가장 최근에 쓰인 세션 파일 5개의 끝부분을 읽어, 가장 늦은 `token_count` 이벤트의 `rate_limits`를 씁니다. 네트워크 호출이나 토큰 사용은 없습니다. upstream 데몬처럼 기본 버킷(`limit_id: codex`)만 보고, 창 길이로 종류를 정합니다(1일 이하 → 세션, 6~8일 → 주간). |
| Claude | 5분마다 키체인의 `Claude Code-credentials` 항목을 `/usr/bin/security`로 읽고, 그 토큰으로 Claude Code의 `/usage` 화면이 쓰는 비공개 엔드포인트를 한 번 부릅니다(LIGH-113에서 사용 승인). `five_hour` → 세션, `seven_day` → 주간, `seven_day_opus`/`_sonnet` → 모델별 주간. 값이 null인 창(그 기간에 쓰지 않음)은 표시하지 않습니다. |
| 토큰 | 읽기만 합니다. **갱신(refresh)하지 않습니다.** 갱신하면 refresh 토큰이 바뀌어 Claude Code가 로그아웃됩니다. 만료됐으면 요청을 보내지 않고 "토큰 만료"로 표시했다가, Claude Code가 다음에 실행돼 토큰을 갱신하면 다시 읽습니다. 토큰은 로그·응답·에러 문구에 나오지 않습니다. |
| 실패 | 읽기에 실패하면 마지막으로 읽은 값을 그대로 두고, 실패 이유를 그 계정 카드에 표시합니다. |
| 추이 | 새 스냅샷만 저장합니다(Codex 로그가 그대로면 건너뜀). 화면은 7일·30일을 고르고, 한 시간에 한 점(그 시간의 최댓값)을 그립니다. |

## 설치 / 갱신 (Mac Studio)

```bash
bash lightsoft/usage/install.sh     # 처음이면 accounts.json 예시를 만들고 멈춤 → 고친 뒤 다시 실행
# 처음 한 번: :3444에 경로 추가 (기존 /, /api, /lightsoft-secrets 등은 그대로)
tailscale serve --bg --https=3444 --set-path=/lightsoft-usage http://127.0.0.1:13500/lightsoft-usage
```

- **계정 설정**: `~/.multica/usage/accounts.json`(레포는 공개라 실제 값은 이 파일에만 둡니다. 형식은 `accounts.example.json`).
  - `workspace_id`: 이 계정들을 볼 워크스페이스
  - `accounts[]`: `id`(영문 소문자·숫자·`-`), `name`, `provider`(`claude`|`codex`), `codex_home`(codex만, `~/` 가능), `runtime_ids`(이 계정으로 도는 런타임. 화면에 이름이 나옵니다)
  - 데몬을 `CODEX_HOME`을 바꿔 하나 더 띄우면(예: `--profile`) 그 경로로 계정을 하나 더 적습니다. 같은 `~/.claude`를 쓰는 데몬들은 Claude 계정 하나에 런타임을 여러 개 적습니다.
  - 바꾼 뒤 `install.sh`를 다시 실행합니다(재시작).
- 다시 설치해도 기록(`history.jsonl`)은 그대로입니다.
- 로그: `~/.multica/usage/logs/service.log`(메서드·경로·상태와 읽기 실패 이유만 남고 토큰은 남지 않음)
- 중지: `launchctl bootout gui/$(id -u)/dev.lightsoft.multica-usage`
- 노출만 끄기: `tailscale serve --https=3444 --set-path=/lightsoft-usage off`
- tailnet 밖 주소(Cloudflare Tunnel)에도 열려면 `lightsoft/access/tunnel.yml.template`에 있는 `/lightsoft-usage` 경로를 반영해야 합니다(`lightsoft/access/README.md`의 터널 설치).

설정은 `LIGHTSOFT_USAGE_*` 환경 변수로 바꿉니다(`config.go`): `_ADDR`, `_API_URL`, `_ALLOWED_ORIGINS`, `_DATA_DIR`, `_ACCOUNTS`, `_HISTORY`, `_INTERVAL`(기본 5m, 최소 30s), `_RETENTION_DAYS`(기본 35).

## 알려진 한계

- **Codex 값은 그 계정으로 Codex가 마지막으로 실행된 시점 기준입니다.** 한동안 쓰지 않으면 값이 오래된 채로 남고, 초기화 시각이 지나면 화면이 "마지막 보고 이후 초기화됨"으로 표시합니다. 같은 계정으로 Multica 밖에서 Codex를 써도 같은 세션 폴더에 기록되므로 함께 반영됩니다.
- Claude 엔드포인트는 공개 API가 아닙니다. 응답 형식이 바뀌면 그 계정 카드에 "응답을 읽지 못했습니다"가 나옵니다. 이때는 `claude.go`의 필드 이름을 실제 응답에 맞춥니다.
- Grok, Kimi, Pi는 한도를 알려 주는 출처가 없어 다루지 않습니다. MacBook Air 등 다른 맥의 런타임은 그 맥의 파일과 키체인에 있으므로 범위 밖입니다(LIGH-113 합의).

## 개발

```bash
cd lightsoft/usage
go vet ./... && go test ./...    # 가짜 세션 로그·가짜 Claude 응답·가짜 Multica API로 확인
```

로컬 웹(`make web-dev`, :13100)에서 시험할 때는 서비스를 다른 포트와 임시 폴더로 띄우고(`LIGHTSOFT_USAGE_ADDR=127.0.0.1:13501`, `LIGHTSOFT_USAGE_DATA_DIR=$(mktemp -d)`, 그 폴더에 `accounts.json`), 웹은 `NEXT_PUBLIC_LIGHTSOFT_USAGE_URL=http://localhost:13501/lightsoft-usage`로 실행합니다. 기본 허용 Origin에 `http://localhost:13100`이 들어 있습니다.
