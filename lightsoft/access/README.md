# 외부 접속: Cloudflare Access (LIGH-253)

tailnet 밖에서도 팀 클라이언트를 브라우저로 쓰게 하는 구성입니다. Cloudflare Tunnel로 공개 주소(예: `lightica.lightsoft.dev`)를 이 맥에 연결하고, 그 앞에 Cloudflare Access를 둡니다. Access에서 로그인하면 **같은 이메일의 Multica 계정으로 자동 로그인**됩니다. tailnet 주소(:3443/:3444)는 그대로입니다.

```
브라우저 ──▶ Cloudflare Access (허용 정책 통과 + Cf-Access-Jwt-Assertion 헤더)
         ──▶ cloudflared 터널 (launchd dev.lightsoft.lightica-tunnel) ──▶ 이 맥
              ├─ /api /ws /v1 /health  → 127.0.0.1:8080  (운영 backend)
              ├─ /lightsoft-secrets    → 127.0.0.1:13300
              ├─ /lightsoft-usage      → 127.0.0.1:13500
              ├─ /lightica             → 127.0.0.1:13400
              ├─ /lightsoft-term       → 404 (터미널은 tailnet 전용)
              └─ /                     → 127.0.0.1:13000 (팀 웹)
```

## 로그인 규칙

- 팀 웹 로그인 화면이 `POST /api/lightsoft/auth/cloudflare-access`를 한 번 부릅니다. backend는 Access가 붙인 JWT(`Cf-Access-Jwt-Assertion`)를 팀 서명 키(`https://<팀 도메인>/cdn-cgi/access/certs`)로 검증하고, `iss`가 팀 도메인인지, `aud`가 우리 Access 앱인지 확인합니다. 그다음 그 이메일로 세션을 발급합니다.
- 계정 규칙은 이메일 코드 로그인과 같습니다. 이미 있는 사용자는 로그인됩니다. 새 이메일은 대기 중인 워크스페이스 초대가 있을 때만 계정이 생깁니다(`ALLOW_SIGNUP=false`). 초대가 없으면 로그인 화면에 "초대되지 않은 계정"이라고 나오고, 일반 로그인 폼은 그대로 쓸 수 있습니다.
- 서명 검증을 하므로, tailnet이나 127.0.0.1로 들어온 요청에 헤더를 위조해 붙여도 로그인되지 않습니다.
- 사용자가 직접 로그아웃하면 그 탭에서는 자동으로 다시 로그인하지 않습니다. 로그인 화면의 "Cloudflare Access 계정으로 로그인" 버튼으로 다시 들어옵니다. 서버가 세션을 끝낸 경우(만료)에는 Access로 다시 자동 로그인합니다.
- 설정이 없으면 엔드포인트가 404를 돌려주고, 로그인 화면은 예전과 똑같이 동작합니다. tailnet으로 접속하면 Access 헤더가 없어 조용히 일반 폼이 나옵니다.

코드: `server/internal/auth/lightsoft_cf_access.go`(JWT 검증), `server/internal/handler/lightsoft_cf_access.go`(세션 발급), `apps/web/features/lightsoft/cloudflare-access-login.tsx`(로그인 화면). upstream 파일에는 연결 코드만 넣었습니다: `router.go`, `client.ts`, `auth/store.ts`, `login/page.tsx`, `web-providers.tsx`.

## 처음 설정 (순서대로)

1. **Access 앱을 먼저 만듭니다** (Cloudflare 대시보드 → Zero Trust → Access → Applications → Add → Self-hosted).
   - 도메인: `lightica.lightsoft.dev`(경로 비움, 호스트 전체).
   - 정책: 재사용 정책 "lightsoft.dev 팀원"(Allow, Emails ending in `@lightsoft.dev`)을 연결합니다. 다른 Access 앱도 같이 쓰는 정책이라 여기에 외부 이메일을 넣지 않습니다. 외부 협업자는 Lightica 앱에 별도 Allow 정책(Emails 목록)을 추가합니다. 정책끼리는 OR입니다.
   - 로그인 방법: **이메일 One-time PIN 하나만** 둡니다. 외부 협업자도 회사 계정 없이 이메일로 들어올 수 있습니다. IdP가 하나뿐이므로 Instant Auth를 켜서 IdP 선택 화면을 건너뜁니다.
   - 세션 길이: 7일. 세션이 끝나면 열어 둔 탭은 새로고침해야 합니다.
   - 만든 뒤 앱의 **Application Audience (AUD) Tag**와 Zero Trust **팀 도메인**(`<팀>.cloudflareaccess.com`)을 적어 둡니다.
   - 로그인 화면은 `login-page.html` 템플릿을 이 앱에만 지정합니다. 아래 "Access 로그인 화면"을 보세요.
2. **운영 backend에 설정을 넣습니다.** 운영 변경이므로 사용자 확인 후에 합니다.
   - `~/.multica/server/docker-compose.selfhost.yml`의 backend `environment:`는 목록에 있는 변수만 넘기므로 두 줄을 추가합니다. 일일 자동 업데이트는 이 파일을 다시 쓰지 않습니다.
     ```yaml
           LIGHTSOFT_CF_ACCESS_TEAM_DOMAIN: ${LIGHTSOFT_CF_ACCESS_TEAM_DOMAIN:-}
           LIGHTSOFT_CF_ACCESS_AUD: ${LIGHTSOFT_CF_ACCESS_AUD:-}
     ```
   - `~/.multica/server/.env`에 `LIGHTSOFT_CF_ACCESS_TEAM_DOMAIN=<팀>.cloudflareaccess.com`과 `LIGHTSOFT_CF_ACCESS_AUD=<AUD 태그>`를 넣습니다. 앱이 여러 개면 AUD를 쉼표로 이어 씁니다. `CORS_ALLOWED_ORIGINS`에는 `https://lightica.lightsoft.dev`를 추가합니다. 실시간 WebSocket이 이 Origin을 검사합니다.
   - backend만 다시 만듭니다: `cd ~/.multica/server && docker compose -f docker-compose.selfhost.yml up -d --no-deps backend`. 잠깐 끊깁니다.
3. **시크릿 서비스 Origin**: `lightsoft/secrets/dev.lightsoft.multica-secrets.plist`의 `LIGHTSOFT_SECRETS_ALLOWED_ORIGINS`에 새 주소가 들어 있습니다(:3444, localhost:13100, `https://lightica.lightsoft.dev`). 주소를 바꾸면 이 값도 바꾸고 `bash lightsoft/secrets/install.sh`를 실행합니다. 구독 한도 서비스도 같습니다(`lightsoft/usage/dev.lightsoft.multica-usage.plist`의 `LIGHTSOFT_USAGE_ALLOWED_ORIGINS`, `bash lightsoft/usage/install.sh`).
4. **터널**: `LIGHTICA_HOST=lightica.lightsoft.dev bash lightsoft/access/install.sh`
   - 첫 실행에서 이름 있는 터널 `lightica`와 DNS 레코드를 만들고, `~/.cloudflared/lightica.yml`과 launchd `dev.lightsoft.lightica-tunnel`을 둡니다. 기존 songtest·crawler 터널과는 별도 프로세스입니다.
   - 실행이 끝나면 바깥에서 `https://<주소>/health`를 불러 Access가 막는지 확인합니다. 200이 오면, 즉 Access 없이 origin이 열려 있으면 터널을 바로 내리고 실패합니다.
5. **확인**: tailnet을 끈 기기(휴대폰 LTE 등)에서 주소를 엽니다. Access 로그인 → 자동으로 Multica 로그인 → 이슈 목록, 실시간 갱신, 첨부 업로드를 확인합니다.

## Access 로그인 화면

현재 구성(2026-11-20): Zero Trust 팀 `lightsoft-crew.cloudflareaccess.com`, Access 앱 "Lightica"(`lightica.lightsoft.dev`, 이메일 OTP만, Instant Auth, 세션 7일), 터널 `lightica`. Zero Trust 플랜은 Free("Teams Free Base")라서 템플릿 없이 Cloudflare 기본 화면("Log in to Lightica")이 나옵니다.

tailnet 밖 사용자가 처음 보는 화면은 Cloudflare가 띄우는 Access 로그인 화면(`<팀>.cloudflareaccess.com`)입니다. Access를 통과하면 Lightica 로그인 화면을 거치지 않고 바로 자동 로그인됩니다. Lightica 로그인 화면은 tailnet으로 접속할 때, 그리고 초대되지 않은 이메일이거나 직접 로그아웃한 뒤에만 보입니다. 그래서 Lightica처럼 보여야 하는 쪽은 Access 화면입니다.

- **템플릿**: `login-page.html`. Lightica 로그인 카드와 같은 모양이고(로고, "Sign in to Lightica", 라이트·다크), 가운데 `<cf-access-authbox>`에 Cloudflare가 이메일 OTP 입력을 채웁니다. 이 요소는 정확히 한 번, Liquid 조건 밖에 있어야 합니다. 로그인 오류 문구(`message.text`)도 지우지 않습니다. 스크립트와 직접 만든 폼은 쓸 수 없습니다.
- **지정**: Zero Trust → Reusable components → Custom pages에서 Login 유형으로 등록(API `POST /accounts/{id}/access/custom_pages`, `type: login`, `contract_version: 1`)하고, Lightica 앱의 Login page에만 지정합니다(앱의 `custom_pages`). **계정 기본값으로 두지 않습니다.** 기본값으로 두면 songtest 같은 다른 Access 앱도 "Sign in to Lightica"가 됩니다.
- **로고**: 로그인 전에 불러야 하므로 Access 뒤의 Lightica 주소에는 둘 수 없습니다. 공개 레포의 `lightica-login-logo.png`를 `https://raw.githubusercontent.com/lightsoft-dev/multica/lightsoft/dev/lightsoft/access/lightica-login-logo.png`로 씁니다. 이 PR이 `lightsoft/dev`에 merge된 뒤에 보입니다.
- **고칠 때**: 이 파일을 고친 뒤 같은 custom page를 갱신합니다(`PUT /accounts/{id}/access/custom_pages/{uid}`). 템플릿에 문제가 있어 렌더링이 안 되면 Access는 기본 화면으로 돌아가고, 정책 판단은 바뀌지 않습니다.
- 템플릿은 Zero Trust Pay-as-you-go 이상에서만 됩니다. Workers·R2 유료 구독과는 별개입니다. 템플릿 HTML을 대시보드 세션에서 API로 직접 보내면 대시보드 WAF가 XSS로 보고 막습니다(403 "Sorry, you have been blocked"). 대시보드 화면의 편집기나 API 토큰으로 넣습니다. Free 플랜이면 Custom pages → Access login page에서 이름·로고 URL·배경색만 바꿀 수 있고, 이 설정은 모든 Access 앱에 적용됩니다.

## 문제 해결

- **Access를 통과한 뒤 "Cloudflare Tunnel error"(1033)**: DNS 레코드가 다른 터널을 가리키는 경우입니다. `~/.cloudflared/config.yml`에 기본 터널(buildover/staged)이 지정돼 있으면, `--config` 없이 실행한 `cloudflared tunnel route dns`는 인자로 준 터널이 아니라 그 기본 터널로 레코드를 만듭니다. `install.sh`는 이제 Lightica 설정 파일과 터널 UUID로 연결합니다. 이미 잘못된 레코드가 있으면 `LIGHTICA_OVERWRITE_DNS=1`을 붙여 다시 실행합니다. 대시보드 DNS에서 `lightica` CNAME의 대상이 `<lightica 터널 UUID>.cfargotunnel.com`인지 확인할 수 있습니다.
- **설치 스크립트의 외부 확인으로는 이 경우를 못 잡습니다.** 로그인하지 않은 요청에는 Access가 먼저 응답하므로, 레코드가 엉뚱한 곳을 가리켜도 302가 옵니다. 터널까지 가는 경로 전체를 보려면 임시 서비스 토큰을 만들고, Lightica 앱에 그 토큰만 허용하는 Service Auth(`non_identity`) 정책을 붙인 뒤 `CF-Access-Client-Id`/`CF-Access-Client-Secret` 헤더로 `/health`와 `/login`을 요청합니다. 반영까지 1분쯤 걸립니다. 끝나면 정책과 토큰을 지웁니다. 이때 `POST /api/lightsoft/auth/cloudflare-access`는 403(`cf_access_invalid_assertion`)이 정상입니다. 서비스 토큰에는 이메일이 없어서, backend 로그에는 "has no email"로 남습니다.
- 터널 처리량: `curl -s 127.0.0.1:20242/metrics | grep cloudflared_tunnel_total_requests`. 사용자가 접속했는데 0이면 요청이 터널까지 오지 않은 것입니다.

## 끄기

- 터널만 내리기: `launchctl bootout gui/$(id -u)/dev.lightsoft.lightica-tunnel`. 그 주소는 530이 납니다.
- 완전히 지우기: 위 명령 + `cloudflared tunnel route`로 만든 DNS 레코드를 대시보드에서 삭제 + `cloudflared tunnel delete lightica` + `~/Library/LaunchAgents/dev.lightsoft.lightica-tunnel.plist` 삭제.
- 자동 로그인만 끄기: `.env`의 `LIGHTSOFT_CF_ACCESS_*`를 비우고 backend를 다시 만듭니다. Access 뒤에서도 이메일 코드 로그인은 계속 됩니다.

## 제약

- **브라우저 전용입니다.** CLI, 데몬, Lightica 데스크톱 앱, 모바일 앱은 Bearer 토큰으로 API를 부릅니다. Access 쿠키가 없으니 이 주소에서는 막힙니다. 이 클라이언트들은 tailnet 주소를 씁니다. Lightica가 브라우저로 로그인을 넘기는 흐름(`platform=desktop`)은 이 주소에서도 되지만, 앱 자체의 API 연결은 tailnet이 필요합니다.
- 업로드는 Cloudflare 무료 플랜 기준 요청당 100MB까지입니다.
- 웹훅(GitHub 등)도 Access에 막힙니다. 지금도 tailnet 전용이라 달라지지 않습니다.
- 이메일·초대 링크는 계속 운영 주소(`MULTICA_APP_URL`, :3443)로 나갑니다.
