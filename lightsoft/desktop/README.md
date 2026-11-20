# Lightica (팀 데스크톱 앱)

Lightica는 `lightsoft/dev`로 빌드한 Multica 데스크톱 앱입니다. 팀 클라이언트(:3444)와 같은 서버에 붙고, 팀 전용 기능이 들어 있으며, **Lightsoft** Developer ID로 서명·공증합니다. 공식 Multica 앱과 한 맥에 같이 설치해도 서로 겹치지 않습니다.

| | 공식 Multica | Lightica |
|---|---|---|
| 앱 이름 / 번들 ID | Multica / `ai.multica.desktop` | Lightica / `dev.lightsoft.lightica` |
| 딥링크 | `multica://` | `lightica://` |
| 기본 서버 | multica.ai | `https://mac-studio.taile83671.ts.net:3444` (팀 클라이언트, Tailscale 필요) |
| 서버 설정 파일 | `~/.multica/desktop.json` | `~/.lightica/desktop.json` |
| 데몬 설정 | `~/.multica/desktop_prefs.json` | `~/.lightica/desktop_prefs.json` |
| 데몬 프로필 | `~/.multica/profiles/desktop-<host>/` | `~/.multica/profiles/lightica-<host>/` |
| 앱 데이터 | `~/Library/Application Support/Multica` | `~/Library/Application Support/Lightica` |
| 업데이트 피드 | multica-ai GitHub 릴리스 | 팀 릴리스 피드 `https://mac-studio.taile83671.ts.net:3444/lightica/` (Tailscale 필요) |
| 버전 | `0.11.0` | `0.11.0-20261119` (upstream 릴리스-날짜[.N]) |

신원 값은 `apps/desktop/src/shared/lightica.ts`, `apps/desktop/electron-builder.yml`, `apps/desktop/package.json`(`productName`)에 있습니다. 바꿀 때는 세 곳을 같이 바꿉니다. 피드 주소는 `lightica.ts`의 `LIGHTICA_UPDATE_FEED_URL`과 `electron-builder.yml`의 `publish.url`이 같아야 합니다(테스트가 확인합니다). 이미 설치된 앱은 빌드에 들어간 주소만 보므로, 주소를 바꾸면 옛 앱은 새 버전을 찾지 못합니다.
팀 웹(`apps/web/features/lightsoft/desktop-protocol.ts`)은 데스크톱 로그인과 "데스크톱에서 열기"를 `lightica://`로 넘깁니다. 그래서 운영 웹(:3443)은 공식 앱으로, 팀 웹(:3444)은 Lightica로 연결됩니다.

## 처음 한 번: 빌드 준비 (빌드하는 맥)

- Xcode Command Line Tools, Node 22 이상, pnpm, Go(`server/go.mod` toolchain)가 필요합니다.
- **Lightsoft 서명 인증서**(Developer ID Application: Lightsoft)가 서명용 키체인에 있어야 합니다. 확인: `lightsoft/desktop/signing.sh status`. 다른 Developer ID로 서명하려면 `CSC_NAME="<이름> (<TEAMID>)"`를 줍니다.

  처음 만들거나 갱신할 때. 지금 인증서(팀 `H66C2M66DC`)는 **2027-12-19**까지 유효하니, 그 전에 같은 순서로 다시 발급합니다. 이미 서명·공증한 빌드는 만료 뒤에도 열립니다(서명에 타임스탬프가 있습니다). 팀 ID가 같으면 앱 안 자동 설치도 그대로 이어집니다.
  1. `lightsoft/desktop/signing.sh csr`: 이 맥에서 개인키와 CSR(`~/.lightsoft/signing/Lightsoft-DeveloperID.certSigningRequest`)을 만듭니다. 개인키는 이 맥 밖으로 나가지 않습니다.
  2. **Account Holder**가 developer.apple.com에서 인증서를 발급합니다. Developer ID 인증서는 Account Holder만 만들 수 있고, App Store Connect API 키로는 403이 납니다. 순서: Certificates, IDs & Profiles → Certificates → **+** → **Developer ID Application** → Profile Type **G2 Sub-CA** → 위 CSR 업로드 → Download(`developerID_application.cer`).
  3. `lightsoft/desktop/signing.sh install ~/Downloads/developerID_application.cer`: 받은 인증서와 개인키를 전용 키체인(`~/Library/Keychains/lightsoft-signing.keychain-db`)에 넣습니다. 키체인 비밀번호는 무작위로 만들어 로그인 키체인에 저장하므로, 빌드 중에 codesign 허용 창이 뜨지 않습니다. 암호화된 백업(`~/.lightsoft/signing/developer-id-application.p12`, 비밀번호도 로그인 키체인)을 남기고 평문 개인키는 지웁니다.
- 공증 자격증명을 키체인 프로필 `lightica-notary`로 한 번 저장합니다. 값은 셸 기록에 남지 않게 입력합니다.

  ```bash
  xcrun notarytool store-credentials lightica-notary \
    --key ~/.appstoreconnect/private_keys/AuthKey_<KEY_ID>.p8 --key-id <KEY_ID> --issuer <ISSUER_ID>
  ```

  키체인 프로필 대신 환경변수도 됩니다: `APPLE_API_KEY`(.p8 경로)·`APPLE_API_KEY_ID`·`APPLE_API_ISSUER`, 또는 `APPLE_ID`·`APPLE_APP_SPECIFIC_PASSWORD`. 값은 로그나 이슈에 남기지 않습니다.

## 빌드

```bash
git checkout lightsoft/dev && git pull
lightsoft/desktop/build.sh            # Apple silicon
lightsoft/desktop/build.sh --x64      # Intel 맥용
```

팀에 나눠 줄 빌드는 아래 [릴리스](#릴리스-팀-배포)의 `release.sh`로 만듭니다. `build.sh`만 실행하면 빌드와 검사만 하고 게시하지 않습니다.

결과물은 `apps/desktop/dist/lightica-<버전>-mac-<arch>.dmg`와 같은 이름의 `.zip`(앱 안 업데이트용), `latest-mac.yml`입니다. 15~30분쯤 걸리고, 대부분 Apple 공증 대기 시간입니다. 스크립트가 하는 일:

1. 서명용 키체인을 열고 `Developer ID Application: Lightsoft` 인증서를 찾습니다. 인증서의 팀 ID는 `APPLE_TEAM_ID`로 씁니다. `scripts/package.mjs`는 이 값이 없으면 공증을 건너뜁니다.
2. 공증 자격증명으로 `notarytool`에 로그인되는지 먼저 확인합니다.
3. 버전을 정합니다: `lightsoft/version.sh`가 `<upstream 릴리스>-<YYYYMMDD>`(서울 날짜)를 만들고, 릴리스 피드에 이미 있는 버전이면 `.2`, `.3`을 붙입니다. `DESKTOP_BUILD_VERSION`을 주면 그 값을 씁니다.
4. `pnpm package -- --mac dmg zip`: 번들 CLI와 음성 엔진을 포함해 빌드하고, `.app`을 서명·공증·staple합니다. 앱과 번들 CLI 모두 3의 버전을 받습니다(`scripts/package.mjs`의 `DESKTOP_BUILD_VERSION`).
5. dmg도 서명·공증·staple합니다. electron-builder는 `.app`만 공증합니다.
6. Gatekeeper 기준으로 검사합니다: `codesign --verify --deep --strict`, `spctl --assess`(app은 execute, dmg는 open), `stapler validate`. 앱 버전(`CFBundleShortVersionString`)과 번들 CLI의 `multica version`이 3의 버전과 같은지도 봅니다.

### 버전 체계

- 형식: `<upstream 릴리스>-<YYYYMMDD>[.N]`. 예: `0.11.0-20261119`, 같은 날 두 번째 빌드는 `0.11.0-20261119.2`. 날짜는 Asia/Seoul입니다.
- 계산은 `lightsoft/version.sh` 한 곳에서 합니다. 이미 쓴 버전 목록을 `--taken`으로 받습니다. 포크 backend 이미지(LIGH-190)도 같은 스크립트를 씁니다: `docker image ls --format '{{.Tag}}' <이미지> | lightsoft/version.sh --taken -`.
- semver로는 prerelease라서 `0.11.0-20261119`은 `0.11.0`보다 낮습니다. 피드에는 날짜 버전만 올리므로 상관없습니다. 날짜와 N은 숫자로 비교되어 `.10`이 `.9`보다 높습니다.
- 데몬은 번들 CLI 버전을 런타임 버전으로 보고합니다. 설정 → 런타임(또는 `multica runtime list`)에서 누가 어느 빌드를 쓰는지 보입니다.
  - 서버의 CLI 최소 버전 검사는 앞의 `X.Y.Z`만 봅니다. 그래서 `0.11.0-20261119`은 `0.11.0`으로 검사됩니다. 예전 `git describe` 버전은 검사를 건너뛰었지만, 이제는 upstream 같은 버전의 데몬과 똑같이 검사됩니다.
  - 번들 CLI가 upstream 릴리스로 바뀌지 않는 이유는 버전 꼴이 아니라 아래 [upstream 업데이트를 받지 않는 설계](#upstream-업데이트를-받지-않는-설계)입니다.
- `electron-builder.yml`의 `detectUpdateChannel: false`는 꼭 둡니다. 없으면 electron-builder가 날짜를 prerelease 채널로 읽어 `20261119-mac.yml`을 만들고 앱에도 그 채널을 넣어서, 빌드마다 피드가 따로 생깁니다.

## 설치와 첫 실행

1. dmg를 열어 Lightica를 Applications로 옮깁니다. 공증돼 있어 경고 없이 열립니다.
2. Tailscale에 접속한 상태에서 실행합니다. 로그인을 누르면 브라우저에서 팀 클라이언트(:3444) 로그인이 열리고, 끝나면 `lightica://`로 앱에 돌아옵니다.
3. 다른 서버에 붙이려면 `~/.lightica/desktop.json`을 만듭니다. `appUrl`, `wsUrl`을 생략하면 `apiUrl`에서 계산합니다.

   ```json
   { "schemaVersion": 1, "apiUrl": "https://mac-studio.taile83671.ts.net:3444" }
   ```

## 릴리스 (팀 배포)

포크 레포는 public이라 GitHub Release에 올리면 외부에 공개됩니다. 그래서 Lightica는 **Mac Studio의 릴리스 피드**로 나눠 줍니다.

```
설치된 Lightica / 브라우저 ──https──▶ tailscale serve :3444 /lightica
                                      → 127.0.0.1:13400 lightica-feed (launchd)
                                      → ~/.multica/lightica/feed/
                                         latest-mac.yml, lightica-<버전>-mac-arm64.{dmg,zip}[.blockmap],
                                         releases.json, index.html
```

- `https://mac-studio.taile83671.ts.net:3444/lightica/`가 릴리스 페이지입니다. 지금 버전의 dmg, SHA-256, 기록이 있습니다. Tailscale 밖에서는 열리지 않습니다(serve만, funnel 아님).
- 피드 파일은 버전이 이름에 들어 있어 바뀌지 않습니다. `latest-mac.yml`, `releases.json`, `index.html`만 `no-cache`로 내보냅니다.
- Tailscale macOS 앱(Standalone)은 샌드박스라서 `tailscale serve`로 폴더를 바로 내보내지 못합니다. 그래서 작은 정적 서버(`lightsoft/desktop/feed/`)를 둡니다.

### 처음 한 번: 피드 열기 (Mac Studio)

```bash
bash lightsoft/desktop/feed/install.sh      # 빌드 → ~/.multica/lightica/bin, launchd 등록, health 확인
tailscale serve --bg --https=3444 --set-path=/lightica http://127.0.0.1:13400/lightica
curl -fsS https://mac-studio.taile83671.ts.net:3444/lightica/health
```

- 노출만 끄기: `tailscale serve --https=3444 --set-path=/lightica off`
- 서버 멈추기: `launchctl bootout gui/$(id -u)/dev.lightsoft.lightica-feed`
- 로그: `~/.multica/lightica/logs/feed.log`

### 게시

```bash
cd <lightsoft/dev 체크아웃> && git pull       # 커밋되지 않은 변경이 있으면 멈춥니다
lightsoft/desktop/release.sh                   # Apple silicon
DESKTOP_BUILD_VERSION=<그날 버전> lightsoft/desktop/release.sh --x64   # Intel이 필요할 때
```

`release.sh`가 하는 일:

1. `build.sh`로 빌드하고 검사합니다(위 "빌드").
2. dmg, zip, blockmap을 피드 폴더에 복사하고 `releases.json`과 `index.html`을 갱신합니다. 오래된 릴리스는 최근 5개 버전만 파일을 남기고 기록에 "정리됨"으로 표시합니다.
3. 마지막에 `latest-mac.yml`을 바꿉니다. 이때부터 설치된 앱이 새 버전을 봅니다. `latest-mac.yml`에는 빌드의 서명 팀 ID(`lighticaSigningTeam`)를 덧붙입니다. 파일의 sha512와 size는 게시한 파일로 다시 씁니다. `build.sh`가 electron-builder 다음에 dmg를 서명·staple해서, electron-builder가 적은 dmg 값은 맞지 않기 때문입니다.
4. 설치 안내 이슈(LIGH-116)의 "현재 배포 파일" 절을 새 버전, 파일, SHA-256으로 바꿉니다. `multica` CLI 로그인이 필요합니다. `--no-issue`로 건너뜁니다.

한 피드에는 `release.sh`를 한 번에 하나만 실행합니다. 이미 실행 중이면 시작 단계에서 멈춥니다(피드 폴더의 `.release.pid`). 둘이 같이 돌면 둘 다 같은 버전을 고르고, 나중 것이 먼저 게시된 파일과 SHA-256을 덮어씁니다.

환경변수: `LIGHTICA_FEED_DIR`(피드 폴더), `LIGHTICA_FEED_URL`, `LIGHTICA_INSTALL_ISSUE`, `DESKTOP_BUILD_VERSION`. 서명·공증 변수는 `build.sh`와 같습니다.

되돌리기: `latest-mac.yml`이 가리키는 버전을 낮추면 설치된 앱은 따라 내려가지 않습니다(downgrade 안 함). 문제가 있는 빌드는 고친 빌드를 새 버전으로 다시 게시합니다.

### 앱 안 업데이트

설치된 Lightica는 설정 → 업데이트의 자동 업데이트가 켜져 있으면(기본값) 시작 5초 뒤와 1시간마다 피드의 `latest-mac.yml`을 확인합니다. 같은 화면의 "지금 확인"으로도 확인합니다. 새 버전이 있으면:

- **자동 설치**: 실행 중인 앱과 새 빌드가 **같은 Developer ID 팀**으로 서명돼 있으면 zip을 백그라운드에서 받은 뒤 "Lightica vX — 업데이트가 준비됐습니다 → **지금 업데이트**" 알림을 띄웁니다. 같은 버튼은 설정 → 업데이트에도 있습니다. 누르면 앱이 종료되고, Squirrel.Mac이 Applications의 앱을 바꾼 뒤 새 버전을 다시 엽니다(LIGH-268). 누르지 않아도 앱을 종료할 때 설치됩니다. Squirrel.Mac은 같은 팀 서명이 아니면 설치를 거부하므로, 앱이 자기 서명(`codesign`)과 `latest-mac.yml`의 `lighticaSigningTeam`을 먼저 비교합니다(`apps/desktop/src/main/lightica-update.ts`).
  - 설정 → 데몬의 "앱 종료 시 자동 중지"가 꺼져 있으면(기본값) 데몬은 앱과 함께 멈추지 않습니다. 새 앱이 뜨면 번들 CLI 버전이 바뀐 것을 보고, 돌던 태스크가 끝난 뒤 데몬을 새 CLI로 다시 띄웁니다(`daemon-manager.ts`의 `ensureRunningDaemonVersionMatches`). 켜져 있으면 종료 전에 데몬을 멈추고, 돌던 태스크가 있으면 먼저 묻습니다.
- **다운로드 안내**: 그 밖의 경우(ad-hoc 빌드, 다른 팀으로 서명된 빌드)는 "Lightica vX — 다운로드" 알림만 띄웁니다. 다운로드를 누르면 브라우저로 dmg를 엽니다.
- 업데이트 다운로드 캐시는 `~/Library/Caches/lightica-updater`입니다(`extraMetadata.name: lightica`). 공식 Multica 앱의 캐시와 겹치지 않습니다.
- 이 기능은 이 버전 체계가 들어간 빌드부터 동작합니다. 그 전 빌드(`0.10.0-…`, `0.11.0-26-g…`)는 업데이트 확인이 막혀 있으니 한 번은 손으로 설치합니다.
- `0.11.0-20261119`까지는 ad-hoc 서명(`codesign -dv`에 `TeamIdentifier=not set`)이라 새 버전이 있어도 "다운로드" 알림만 뜹니다. Squirrel.Mac은 ad-hoc 앱을 제자리에서 바꿀 수 없습니다. **Lightsoft Developer ID로 서명한 첫 빌드 `0.11.0-20261120`**을 한 번 손으로 설치하면, 그다음 게시부터 "지금 업데이트"로 설치됩니다.

### upstream 업데이트를 받지 않는 설계

Lightica는 multica-ai 릴리스로 바뀌지 않습니다. 업데이트는 팀 릴리스 피드에서만 받습니다. 경로마다 막는 곳은 이렇습니다.

| 경로 | upstream이면 | Lightica |
|---|---|---|
| 앱 업데이트 | GitHub `multica-ai/multica` 릴리스 | 팀 피드만 읽습니다. 실행 중에 `setFeedURL`(`updater.ts`)로 고정하고, 빌드에 들어가는 `app-update.yml`도 팀 피드입니다(`electron-builder.yml`의 `publish`). 테스트가 `multica-ai`/`github` provider가 없는지 봅니다. |
| 번들 CLI를 못 쓸 때 | GitHub 최신 CLI를 userData에 받거나 PATH의 `multica`를 씁니다 | 대체하지 않습니다. 번들 CLI만 실행합니다(`LIGHTICA_BUNDLED_CLI_ONLY`, `daemon-manager.ts`). 설정 → 데몬에 "Lightica를 다시 설치" 안내와 릴리스 페이지 버튼이 나옵니다. |
| 데몬 자동 업데이트 | GitHub 최신 CLI로 스스로 교체 | 데스크톱이 띄운 데몬(`MULTICA_LAUNCHED_BY=desktop`)은 건너뜁니다(upstream `auto_update.go`). |
| 런타임 화면의 "업데이트" | 서버가 데몬에 upstream 버전 설치를 보냅니다 | 데스크톱이 띄운 데몬은 거부하고 실패로 보고합니다(upstream `handleUpdate`, 포크 테스트 `server/internal/daemon/lightsoft_desktop_update_test.go`). |

- 일일 05:00 작업이 upstream 릴리스를 `lightsoft/dev`에 merge해도, 팀원 앱은 사람이 `release.sh`로 게시한 빌드만 받습니다.
- upstream merge가 충돌하면 이 표의 파일에서 Lightica 쪽을 유지합니다.

### 팀원 업데이트 방법

- 알림의 **지금 업데이트**를 누릅니다. 앱이 종료되고 새 버전이 설치된 뒤 다시 열립니다. 앱을 종료했다가 다시 켜도 설치됩니다.
- "다운로드" 알림이면: dmg를 받아 열고, Lightica를 종료한 뒤 Applications의 Lightica를 덮어씁니다. 로그인과 설정은 그대로 남습니다.
- 직접 받으려면 릴리스 페이지(`https://mac-studio.taile83671.ts.net:3444/lightica/`)를 엽니다.

## 일일 업데이트 후 다시 빌드

매일 05:00 자동 작업이 운영과 같은 upstream 릴리스를 `lightsoft/dev`에 merge합니다(`lightsoft/ops/README.md`). Lightica는 자동으로 다시 빌드되지 않습니다. 새 릴리스가 merge되면 사람이 `lightsoft/desktop/release.sh`로 게시하고, 팀원의 앱이 그 버전을 알려 줍니다.

- 리포트 이슈(LIGH-101)에 "`lightsoft/dev`에 upstream vX.Y.Z merge"가 올라오면 다시 게시할 때입니다. 운영 backend와 같은 릴리스라서, 게시하지 않아도 이전 Lightica가 바로 깨지지는 않습니다.
- merge가 충돌로 실패하면, 아래 파일에서 Lightica 값(이름·ID·스킴·경로·팀 피드와 설치 판단)을 유지하는 쪽으로 풉니다.
  - `apps/desktop/electron-builder.yml`(`publish`, `detectUpdateChannel`, `extraMetadata`), `apps/desktop/package.json`
  - `apps/desktop/scripts/package.mjs`(`DESKTOP_BUILD_VERSION`)
  - `apps/desktop/src/main/{index,runtime-config-loader,daemon-manager,daemon-profile,updater}.ts`(`daemon-manager`는 번들 CLI 전용 게이트), `apps/desktop/src/shared/updater-types.ts`(`manual` 상태)
  - `apps/desktop/src/renderer/src/components/daemon-settings-tab.tsx`(CLI 없음 안내)
  - `apps/desktop/src/renderer/{index.html,src/App.tsx,src/pages/project-detail-page.tsx}`, `apps/desktop/src/renderer/src/components/{update-notification,updates-settings-tab}.tsx`
  - `apps/web/app/(auth)/login/page.tsx`, `apps/web/app/auth/callback/page.tsx`, `apps/web/platform/navigation.tsx`
  - 팀 기능 경로: `apps/desktop/src/renderer/src/routes.tsx`의 `secrets`(시크릿 매니저, `lightsoft/secrets/README.md`), `limits`(구독 한도, `lightsoft/usage/README.md`)

## 공식 Multica 앱과 같이 쓸 때

설치·설정·딥링크는 겹치지 않습니다. 다만 두 앱이 각자 로컬 데몬을 띄우므로, 둘 다 같은 워크스페이스에 로그인하면 이 맥이 런타임 두 개로 등록됩니다. 한 앱에서만 데몬을 켜 두세요.

## 아이콘

원본은 `lightsoft/desktop/icon-source.png`(밝은 배경 위 어두운 도형, 1024px)입니다. 바꾼 뒤 `lightsoft/desktop/make-icons.sh`(ImageMagick 필요)로 `apps/desktop/build/lightica/`를 다시 만듭니다. 스크립트는 원본의 어두운 부분만 잘라 macOS 아이콘 규격(1024px 캔버스 가운데 824px 흰 둥근 사각형)에 640px로 얹습니다. 원본 배경색과 여백은 결과에 남지 않습니다.

워크스페이스 아이콘(사이드바에 보이는 워크스페이스 이미지)은 앱이 아니라 서버 데이터입니다. 설정 → 워크스페이스에서 같은 원본을 올립니다. upstream 아이콘 파일(`apps/desktop/build/icon.*`)은 그대로 두고 `mac.icon`만 이쪽을 가리킵니다. 그래서 일일 merge에서 바이너리 충돌이 나지 않습니다.

## 범위

macOS만 Lightica로 바꿨습니다. Windows·Linux 설정(`win`, `linux` 섹션, 아이콘)은 upstream 그대로입니다.
