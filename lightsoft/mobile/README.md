# Lightica (팀 모바일 앱, iOS)

Lightica 모바일은 `lightsoft/dev`로 빌드한 Multica iOS 앱(`apps/mobile`, Expo)입니다. 데스크톱 Lightica(`lightsoft/desktop/README.md`)처럼 팀 클라이언트(:3444)와 같은 서버에 붙습니다. 공식 Multica 앱과 한 iPhone에 같이 설치해도 서로 겹치지 않습니다(LIGH-258).

| | 공식 Multica | Lightica |
|---|---|---|
| 앱 이름 | Multica / Multica (Staging) / Multica (Dev) | Lightica / Lightica (Staging) / Lightica (Dev) |
| 번들 ID | `ai.multica.mobile[.staging\|.dev]` | `dev.lightsoft.lightica.mobile[.staging\|.dev]` |
| 딥링크 | `multica://` | `lightica://` |
| 서버 (release 빌드) | `api.multica.ai` | `https://mac-studio.taile83671.ts.net:3444` (팀 클라이언트, Tailscale 필요) |
| 아이콘 | `apps/mobile/assets/icon.png` | `apps/mobile/assets/lightica/icon.png` |
| 로그인 화면 | Multica 워드마크, "Sign in to Multica" | Lightica 마크, "Sign in to Lightica" |

- 데스크톱 Lightica의 번들 ID는 `dev.lightsoft.lightica`입니다. 모바일은 upstream이 `ai.multica.desktop`과 `ai.multica.mobile`을 나누듯 `.mobile`을 붙입니다.
- 빌드 신원(이름·번들 ID·스킴·아이콘·사진 권한 문구)은 `apps/mobile/app.config.ts`의 `LIGHTICA` 상수에 있습니다. Expo 설정 로더가 다른 TS 파일을 읽지 못해서 거기에 둡니다. 앱이 실행 중에 읽는 값(이름, 서버 주소)은 `apps/mobile/lightica.ts`에 있습니다.
- 서버 주소는 `apps/mobile/.env.production`의 `EXPO_PUBLIC_API_URL`·`EXPO_PUBLIC_WEB_URL`입니다. 빌드할 때 JS 번들에 들어갑니다. `lightica.ts`의 `LIGHTICA_SERVER_URL`과 같아야 하고, `apps/mobile/lightica.test.ts`가 확인합니다.
- 로그인 화면의 마크는 팀 웹의 `packages/ui/components/common/lightica-icon.tsx`와 같은 도형입니다(`apps/mobile/components/brand/lightica-logo.tsx`). 테스트가 두 경로가 같은지 봅니다.
- 앱의 나머지 화면과 문구는 upstream 그대로입니다. 다국어 파일(`apps/mobile/locales/`)은 바꾸지 않았고, 로그인 제목만 호출하는 곳에서 바꿉니다. 팀 웹과 같은 방식입니다.

## 서버와 로그인

- 앱은 `https://mac-studio.taile83671.ts.net:3444`의 `/api`와 `/ws`를 씁니다. Tailscale serve가 이 경로를 운영 backend(127.0.0.1:8080)로 보냅니다. **iPhone에 Tailscale 앱이 켜져 있어야** 합니다. tailnet 밖(Cloudflare Access, `lightica.lightsoft.dev`)은 브라우저 전용이라 앱이 쓸 수 없습니다.
- 로그인은 이메일 인증 코드입니다. 운영 서버가 Resend로 코드를 보냅니다. 팀 웹과 같은 계정을 씁니다.
- WebSocket은 운영 backend의 Origin 검사를 통과합니다. iOS WebSocket은 Origin을 접속 대상(`https://mac-studio.taile83671.ts.net:3444`)으로 채우고, 이 값은 `CORS_ALLOWED_ORIGINS`에 있습니다. 운영 설정을 바꿀 필요가 없습니다.

## 빌드와 설치 (빌드하는 맥 + USB iPhone)

준비물은 upstream과 같습니다: Xcode, Xcode → Settings → Accounts에 추가한 Apple ID, USB로 연결하고 [개발자 모드](https://docs.expo.dev/guides/ios-developer-mode/)를 켠 iPhone. 자세한 내용은 `apps/mobile/README.md`의 "Just want to use it on your phone?" 절을 따릅니다.

```bash
git checkout lightsoft/dev && git pull && pnpm install
pnpm ios:mobile:device:prod:release      # release 빌드를 USB iPhone에 설치 (Lightica, 팀 서버)
pnpm ios:mobile:prod --configuration Release      # 같은 빌드를 iOS 시뮬레이터에
```

- 첫 빌드는 CocoaPods와 React Native 소스 빌드 때문에 10~20분 걸립니다. 그다음은 Xcode 캐시를 씁니다.
- **서명.** Lightsoft Apple Developer 팀(`H66C2M66DC`)에 속한 Apple ID면 `EXPO_APPLE_TEAM_ID=H66C2M66DC`를 주고 빌드합니다. 기본 번들 ID를 그대로 씁니다. 팀에 없는 Apple ID(개인 팀)는 기본 번들 ID를 서명할 수 없으니, 자기 도메인으로 바꿉니다.

  ```bash
  export EXPO_BUNDLE_IDENTIFIER_PROD=com.<이름>.lightica
  pnpm ios:mobile:device:prod:release
  ```

  값은 `apps/mobile/.env.production.local`(git 제외)에 넣어 둬도 됩니다.
- 무료 Apple ID(개인 팀)로 서명한 빌드는 7일 뒤에 열리지 않습니다. 다시 연결하고 같은 명령을 실행합니다. Lightsoft 팀으로 서명하면 1년입니다.
- `pnpm ios:mobile:device:prod`(Debug)는 Metro에서 JS를 받는 개발용입니다. 맥 없이 쓰려면 `:release`를 씁니다.
- 다른 서버에 붙이려면 `apps/mobile/.env.production.local`에 `EXPO_PUBLIC_API_URL`·`EXPO_PUBLIC_WEB_URL`을 넣고 다시 빌드합니다. release 빌드는 빌드할 때의 값만 씁니다.

## 변형(variant)

`APP_ENV`로 고르는 변형은 upstream과 같습니다. 이름과 번들 ID만 Lightica로 바뀝니다.

- production(`*:prod`): 팀 서버. 팀원이 쓰는 빌드입니다.
- development(접미사 없음): `apps/mobile/.env.development.local`의 서버(맥의 LAN IP). 격리 환경에 띄운 포크 backend를 시험할 때 씁니다. 운영 DB에 개발 backend를 붙이지 않는 규칙은 그대로입니다.
- staging(`*:staging`): `apps/mobile/.env.staging`은 upstream 내부 staging 서버를 가리킵니다. 팀은 쓰지 않습니다.

## 아이콘

원본은 데스크톱과 같은 `lightsoft/desktop/icon-source.png`입니다. 바꾼 뒤 `lightsoft/mobile/make-icon.sh`(ImageMagick 필요)로 `apps/mobile/assets/lightica/icon.png`를 다시 만듭니다. iOS 규격대로 1024px 정사각형 전체를 흰 배경으로 채우고 알파 채널을 두지 않습니다(iOS가 모서리를 직접 깎습니다). 도형 크기는 upstream 모바일 아이콘과 같은 616px입니다. upstream 아이콘(`apps/mobile/assets/icon.png`)은 그대로 둬서 일일 merge에서 바이너리 충돌이 나지 않습니다.

## 일일 업데이트 후

매일 05:00 자동 작업이 upstream 릴리스를 `lightsoft/dev`에 merge합니다(`lightsoft/ops/README.md`). 모바일 앱은 자동으로 다시 빌드되지 않습니다. 새 릴리스가 들어오면 위 명령으로 다시 설치합니다. 운영 backend와 같은 릴리스라서, 다시 설치하지 않아도 이전 빌드가 바로 깨지지는 않습니다.

merge가 충돌하면 아래 파일에서 Lightica 값을 유지합니다.

- `apps/mobile/app.config.ts`(`LIGHTICA` 상수와 그것을 쓰는 `name`·`scheme`·`icon`·`bundleIdentifier`·`photosPermission`), `apps/mobile/app.config.test.ts`
- `apps/mobile/.env.production`, `apps/mobile/.env.production.example`
- `apps/mobile/app/(auth)/login.tsx`, `apps/mobile/app/(auth)/verify.tsx`(Lightica 마크와 제목)
- `apps/mobile/vitest.config.ts`(`lightica.test.ts`)

## 범위

- iOS만 있습니다. upstream 모바일 앱이 iOS 전용이라 Android 빌드 설정이 없습니다.
- TestFlight·App Store 배포는 아직 없습니다. 팀원마다 위 명령으로 직접 설치합니다.
