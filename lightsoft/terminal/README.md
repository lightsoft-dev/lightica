# 프로젝트 터미널 (LIGH-105)

팀 클라이언트의 프로젝트 화면 메뉴에서 **터미널 열기**를 누르면 새 탭에 Mac Studio 셸이 열립니다. 개발 환경의 `.env`처럼 가끔 손봐야 하는 파일을 고칠 때 씁니다.

```
브라우저 (:3444/lightsoft-term/p/<프로젝트>?ws=<워크스페이스>)
  └─ tailscale serve :3444 /lightsoft-term → 127.0.0.1:13200  lightsoft-terminal (launchd, zimin 사용자)
                                               ├─ 인증: multica_auth 쿠키를 운영 API(127.0.0.1:8080)로 확인
                                               └─ PTY: 프로젝트 폴더에서 $SHELL -l
```

- 운영 backend와 DB는 바꾸지 않습니다. 게이트웨이는 로그인한 사람의 쿠키로 `/api/me`, `/api/workspaces/{id}/members`, `/api/projects/{id}`, `/api/projects/{id}/resources`를 **읽기만** 합니다.
- 게이트웨이는 upstream 코드와 섞이지 않게 이 폴더에 별도 Go 모듈로 있습니다. 화면(xterm.js 6.0.0, MIT, `web/vendor/`)도 바이너리에 들어 있어서 웹 빌드나 `pnpm-lock.yaml`에 영향이 없습니다. 팀 클라이언트 쪽 변경은 메뉴 항목(`apps/web/features/lightsoft/terminal-menu-item.tsx`)과 `ProjectDetail`의 `menuExtra` 슬롯뿐입니다.

## 규칙

| 항목 | 동작 |
|---|---|
| 권한 | 워크스페이스 **owner/admin**만 씁니다. 메뉴도 이들에게만 보입니다. 게이트웨이는 연결할 때마다 운영 API로 권한을 다시 확인합니다. |
| 출처 | WebSocket `Origin`이 `LIGHTSOFT_TERM_ALLOWED_ORIGINS`(기본 :3444)에 없으면 거부합니다. tailnet 밖으로는 열리지 않습니다(serve만, funnel 아님). |
| 시작 폴더 | ① 이 맥 데몬의 `local_directory` 리소스 → ② 프로젝트 설명에서 `로컬`로 시작하는 줄의 첫 경로(`로컬: ~/lightsoft-projects/foo — ...`) → ③ 홈 폴더 순서로 정합니다. 홈 밖의 경로나 없는 폴더는 건너뜁니다. |
| 셸 | `$SHELL -l`(zsh 로그인 셸), Mac 사용자 zimin 권한. `MULTICA_*`, `LIGHTSOFT_TERM_*` 변수는 지우고 `LIGHTSOFT_TERMINAL_USER=<이메일>`을 넣습니다. |
| 종료 | 탭을 닫거나 연결이 끊기면 바로, 15분 동안 입출력이 없으면, 4시간이 지나면 닫습니다. 터미널의 모든 프로세스에 SIGHUP을 보내고 5초 뒤 남은 프로세스는 SIGKILL합니다. `nohup ... & disown`이나 `setsid`로 일부러 떼어 낸 프로세스는 로컬 터미널을 닫을 때처럼 남습니다. |
| 동시 사용 | 사람당 3개, 전체 10개 |
| 감사 로그 | `~/.multica/terminal/audit.log`(JSON lines, 0600)에 열기·닫기·거부를 남깁니다. 누가, 어느 프로젝트에서, 어느 폴더로, 얼마나, 왜 닫혔는지와 `Tailscale-User-Login`. **입력과 출력은 기록하지 않습니다.** |

설정은 `LIGHTSOFT_TERM_*` 환경 변수로 바꿉니다(`config.go`): `_ADDR`, `_API_URL`, `_ALLOWED_ORIGINS`, `_ALLOWED_ROLES`, `_DAEMON_ID`, `_IDLE_TIMEOUT`, `_MAX_LIFETIME`, `_MAX_PER_USER`, `_MAX_TOTAL`, `_AUDIT_LOG`, `_SHELL`.

## 설치 / 갱신 (Mac Studio)

```bash
bash lightsoft/terminal/install.sh     # 빌드 → ~/.multica/terminal/bin, launchd 등록, health 확인
# 처음 한 번: :3444에 경로 추가 (기존 /, /api 등은 그대로)
tailscale serve --bg --https=3444 --set-path=/lightsoft-term http://127.0.0.1:13200/lightsoft-term
```

- 다시 설치하면 게이트웨이가 재시작되고 열린 터미널은 모두 닫힙니다.
- 로그: `~/.multica/terminal/logs/gateway.log`, 감사 로그: `~/.multica/terminal/audit.log`
- 중지: `launchctl bootout gui/$(id -u)/dev.lightsoft.multica-terminal`
- 노출만 끄기: `tailscale serve --https=3444 --set-path=/lightsoft-term off`

## 개발

```bash
cd lightsoft/terminal
go test ./...            # 가짜 Multica API + 실제 PTY로 권한·폴더·종료·정리를 확인
```

로컬 웹(`make web-dev`, :13100)에서 시험할 때는 게이트웨이를 `LIGHTSOFT_TERM_ALLOWED_ORIGINS=http://localhost:13200` 등으로 띄우고, 웹은 `NEXT_PUBLIC_LIGHTSOFT_TERMINAL_URL=http://localhost:13200/lightsoft-term`으로 실행합니다. 쿠키는 포트를 가리지 않으므로 localhost 로그인 쿠키가 게이트웨이에도 전달됩니다.
