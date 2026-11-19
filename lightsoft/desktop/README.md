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
| 자동 업데이트 | multica-ai 릴리스 | **없음**. 새 dmg를 받아 다시 설치합니다. |

신원 값은 `apps/desktop/src/shared/lightica.ts`, `apps/desktop/electron-builder.yml`, `apps/desktop/package.json`(`productName`)에 있습니다. 바꿀 때는 세 곳을 같이 바꿉니다.
팀 웹(`apps/web/features/lightsoft/desktop-protocol.ts`)은 데스크톱 로그인과 "데스크톱에서 열기"를 `lightica://`로 넘깁니다. 그래서 운영 웹(:3443)은 공식 앱으로, 팀 웹(:3444)은 Lightica로 연결됩니다.

## 처음 한 번: 빌드 준비 (빌드하는 맥)

- Xcode Command Line Tools, Node 22 이상, pnpm, Go(`server/go.mod` toolchain)가 필요합니다.
- **Lightsoft 서명 인증서**(Developer ID Application: Lightsoft)가 서명용 키체인에 있어야 합니다. 확인: `lightsoft/desktop/signing.sh status`. 다른 Developer ID로 서명하려면 `CSC_NAME="<이름> (<TEAMID>)"`를 줍니다.

  처음 만들거나 갱신할 때(인증서 유효기간 5년):
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

결과물은 `apps/desktop/dist/lightica-<버전>-mac-<arch>.dmg`입니다. 15~30분쯤 걸리고, 대부분 Apple 공증 대기 시간입니다. 스크립트가 하는 일:

1. 서명용 키체인을 열고 `Developer ID Application: Lightsoft` 인증서를 찾습니다. 인증서의 팀 ID는 `APPLE_TEAM_ID`로 씁니다. `scripts/package.mjs`는 이 값이 없으면 공증을 건너뜁니다.
2. 공증 자격증명으로 `notarytool`에 로그인되는지 먼저 확인합니다.
3. upstream 릴리스 태그를 가져와 버전을 정합니다. `git describe` 기준이라 `0.10.0-8-gc0b61dd82`처럼 나옵니다.
4. `pnpm package -- --mac dmg`: 번들 CLI와 음성 엔진을 포함해 빌드하고, `.app`을 서명·공증·staple합니다.
5. dmg도 서명·공증·staple합니다. electron-builder는 `.app`만 공증합니다.
6. Gatekeeper 기준으로 검사합니다: `codesign --verify --deep --strict`, `spctl --assess`(app은 execute, dmg는 open), `stapler validate`.

## 설치와 첫 실행

1. dmg를 열어 Lightica를 Applications로 옮깁니다. 공증돼 있어 경고 없이 열립니다.
2. Tailscale에 접속한 상태에서 실행합니다. 로그인을 누르면 브라우저에서 팀 클라이언트(:3444) 로그인이 열리고, 끝나면 `lightica://`로 앱에 돌아옵니다.
3. 다른 서버에 붙이려면 `~/.lightica/desktop.json`을 만듭니다. `appUrl`, `wsUrl`을 생략하면 `apiUrl`에서 계산합니다.

   ```json
   { "schemaVersion": 1, "apiUrl": "https://mac-studio.taile83671.ts.net:3444" }
   ```

팀원에게 dmg를 나눠 줄 위치(GitHub Release, 사내 드라이브 등)는 아직 정하지 않았습니다. 외부에 올리기 전에 확인을 받습니다.

## 일일 업데이트 후 다시 빌드

매일 05:00 자동 작업이 운영과 같은 upstream 릴리스를 `lightsoft/dev`에 merge합니다(`lightsoft/ops/README.md`). Lightica는 스스로 업데이트하지 않습니다. 설정의 "업데이트 확인"을 누르면 수동 설치 안내가 나옵니다. 그래서 새 릴리스가 merge되면 사람이 다시 빌드해서 나눠 줍니다.

```bash
cd <lightsoft/dev 체크아웃> && git pull
lightsoft/desktop/build.sh
# dist/lightica-*.dmg 배포 → 팀원은 앱을 종료하고 Applications의 Lightica를 덮어씁니다.
```

- 리포트 이슈(LIGH-101)에 "`lightsoft/dev`에 upstream vX.Y.Z merge"가 올라오면 다시 빌드할 때입니다. 운영 backend와 같은 릴리스라서, 빌드하지 않아도 이전 Lightica가 바로 깨지지는 않습니다.
- merge가 충돌로 실패하면, 아래 파일에서 Lightica 값(이름·ID·스킴·경로·업데이트 차단)을 유지하는 쪽으로 풉니다.
  - `apps/desktop/electron-builder.yml`, `apps/desktop/package.json`
  - `apps/desktop/src/main/{index,runtime-config-loader,daemon-manager,daemon-profile,updater}.ts`
  - `apps/desktop/src/renderer/{index.html,src/App.tsx,src/pages/project-detail-page.tsx}`
  - `apps/web/app/(auth)/login/page.tsx`, `apps/web/app/auth/callback/page.tsx`, `apps/web/platform/navigation.tsx`
  - 팀 기능 경로: `apps/desktop/src/renderer/src/routes.tsx`의 `secrets`(시크릿 매니저, `lightsoft/secrets/README.md`)

## 공식 Multica 앱과 같이 쓸 때

설치·설정·딥링크는 겹치지 않습니다. 다만 두 앱이 각자 로컬 데몬을 띄우므로, 둘 다 같은 워크스페이스에 로그인하면 이 맥이 런타임 두 개로 등록됩니다. 한 앱에서만 데몬을 켜 두세요.

## 아이콘

원본은 `lightsoft/desktop/icon.svg`입니다. 바꾼 뒤 `lightsoft/desktop/make-icons.sh`(ImageMagick 필요)로 `apps/desktop/build/lightica/`를 다시 만듭니다. upstream 아이콘 파일(`apps/desktop/build/icon.*`)은 그대로 두고 `mac.icon`만 이쪽을 가리킵니다. 그래서 일일 merge에서 바이너리 충돌이 나지 않습니다.

## 범위

macOS만 Lightica로 바꿨습니다. Windows·Linux 설정(`win`, `linux` 섹션, 아이콘)은 upstream 그대로입니다.
