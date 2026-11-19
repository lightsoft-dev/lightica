# 외부 접속: Cloudflare Access (LIGH-253)

tailnet 밖에서도 팀 클라이언트를 브라우저로 쓰게 하는 구성입니다. Cloudflare Tunnel로 공개 주소(예: `lightica.lightsoft.dev`)를 이 맥에 연결하고, 그 앞에 Cloudflare Access를 둡니다. Access에서 로그인하면 **같은 이메일의 Multica 계정으로 자동 로그인**됩니다. tailnet 주소(:3443/:3444)는 그대로입니다.

```
브라우저 ──▶ Cloudflare Access (허용 정책 통과 + Cf-Access-Jwt-Assertion 헤더)
         ──▶ cloudflared 터널 (launchd dev.lightsoft.lightica-tunnel) ──▶ 이 맥
              ├─ /api /ws /v1 /health  → 127.0.0.1:8080  (운영 backend)
              ├─ /lightsoft-secrets    → 127.0.0.1:13300
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
   - 정책: Allow. 예) Emails ending in `@lightsoft.dev`, 외부 협업자는 Emails에 따로 추가.
   - 로그인 방법: One-time PIN(기본) 또는 Google.
   - 세션 길이: 7일~1개월 정도로 넉넉히. 세션이 끝나면 열어 둔 탭은 새로고침해야 합니다.
   - 만든 뒤 앱의 **Application Audience (AUD) Tag**와 Zero Trust **팀 도메인**(`<팀>.cloudflareaccess.com`)을 적어 둡니다.
   - 로그인 화면을 Lightica로 꾸밉니다. 아래 "Access 로그인 화면"을 보세요.
2. **운영 backend에 설정을 넣습니다.** 운영 변경이므로 사용자 확인 후에 합니다.
   - `~/.multica/server/docker-compose.selfhost.yml`의 backend `environment:`는 목록에 있는 변수만 넘기므로 두 줄을 추가합니다. 일일 자동 업데이트는 이 파일을 다시 쓰지 않습니다.
     ```yaml
           LIGHTSOFT_CF_ACCESS_TEAM_DOMAIN: ${LIGHTSOFT_CF_ACCESS_TEAM_DOMAIN:-}
           LIGHTSOFT_CF_ACCESS_AUD: ${LIGHTSOFT_CF_ACCESS_AUD:-}
     ```
   - `~/.multica/server/.env`에 `LIGHTSOFT_CF_ACCESS_TEAM_DOMAIN=<팀>.cloudflareaccess.com`과 `LIGHTSOFT_CF_ACCESS_AUD=<AUD 태그>`를 넣습니다. 앱이 여러 개면 AUD를 쉼표로 이어 씁니다. `CORS_ALLOWED_ORIGINS`에는 `https://lightica.lightsoft.dev`를 추가합니다. 실시간 WebSocket이 이 Origin을 검사합니다.
   - backend만 다시 만듭니다: `cd ~/.multica/server && docker compose -f docker-compose.selfhost.yml up -d --no-deps backend`. 잠깐 끊깁니다.
3. **시크릿 서비스 Origin**: `lightsoft/secrets/dev.lightsoft.multica-secrets.plist`에 `LIGHTSOFT_SECRETS_ALLOWED_ORIGINS`(기존 :3444, localhost:13100 + 새 주소)를 넣고 `bash lightsoft/secrets/install.sh`를 실행합니다.
4. **터널**: `LIGHTICA_HOST=lightica.lightsoft.dev bash lightsoft/access/install.sh`
   - 첫 실행에서 이름 있는 터널 `lightica`와 DNS 레코드를 만들고, `~/.cloudflared/lightica.yml`과 launchd `dev.lightsoft.lightica-tunnel`을 둡니다. 기존 songtest·crawler 터널과는 별도 프로세스입니다.
   - 실행이 끝나면 바깥에서 `https://<주소>/health`를 불러 Access가 막는지 확인합니다. 200이 오면, 즉 Access 없이 origin이 열려 있으면 터널을 바로 내리고 실패합니다.
5. **확인**: tailnet을 끈 기기(휴대폰 LTE 등)에서 주소를 엽니다. Access 로그인 → 자동으로 Multica 로그인 → 이슈 목록, 실시간 갱신, 첨부 업로드를 확인합니다.

## Access 로그인 화면

tailnet 밖 사용자가 처음 보는 화면은 Cloudflare가 띄우는 Access 로그인 화면(`<팀>.cloudflareaccess.com`)입니다. Access를 통과하면 Lightica 로그인 화면을 거치지 않고 바로 자동 로그인됩니다. Lightica 로그인 화면은 tailnet으로 접속할 때, 그리고 초대되지 않은 이메일이거나 직접 로그아웃한 뒤에만 보입니다. 그래서 Lightica처럼 보여야 하는 쪽은 Access 화면입니다.

- **Free 플랜에서 꾸미기**: Zero Trust → Reusable components → Custom pages → Access login page → Manage에서 바꿉니다.
  - 이름: `Lightica`
  - 로고 URL: `https://raw.githubusercontent.com/lightsoft-dev/multica/lightsoft/dev/lightsoft/access/lightica-login-logo.png`. 로고는 로그인 전에 불러야 하므로 Access 뒤의 Lightica 주소에 두면 안 됩니다. 공개 레포의 이 파일을 씁니다.
  - 배경색: `#fafafa`(Lightica 라이트 배경)
  - 머리글·바닥글 문구

  이 설정은 그 계정의 모든 Access 앱에 함께 적용됩니다(songtest 등에도 같은 화면이 나옵니다).
- **화면을 통째로 바꾸기**(HTML·Liquid 템플릿): Pay-as-you-go 이상 플랜에서만 됩니다.
- **Cloudflare 화면 없이 바로 Google로**: Access 앱의 로그인 방법을 Google(또는 Google Workspace) 하나만 두고 **Instant Auth**(API `auto_redirect_to_identity`)를 켭니다. 그러면 주소를 열 때 Google 계정 선택 → Lightica 순서로 가고, Cloudflare 화면은 나오지 않습니다. Google OAuth 클라이언트를 Cloudflare에 등록해야 합니다. 이메일 OTP를 쓰면 이메일을 입력하는 Cloudflare 화면이 남습니다.

## 끄기

- 터널만 내리기: `launchctl bootout gui/$(id -u)/dev.lightsoft.lightica-tunnel`. 그 주소는 530이 납니다.
- 완전히 지우기: 위 명령 + `cloudflared tunnel route`로 만든 DNS 레코드를 대시보드에서 삭제 + `cloudflared tunnel delete lightica` + `~/Library/LaunchAgents/dev.lightsoft.lightica-tunnel.plist` 삭제.
- 자동 로그인만 끄기: `.env`의 `LIGHTSOFT_CF_ACCESS_*`를 비우고 backend를 다시 만듭니다. Access 뒤에서도 이메일 코드 로그인은 계속 됩니다.

## 제약

- **브라우저 전용입니다.** CLI, 데몬, Lightica 데스크톱 앱, 모바일 앱은 Bearer 토큰으로 API를 부릅니다. Access 쿠키가 없으니 이 주소에서는 막힙니다. 이 클라이언트들은 tailnet 주소를 씁니다. Lightica가 브라우저로 로그인을 넘기는 흐름(`platform=desktop`)은 이 주소에서도 되지만, 앱 자체의 API 연결은 tailnet이 필요합니다.
- 업로드는 Cloudflare 무료 플랜 기준 요청당 100MB까지입니다.
- 웹훅(GitHub 등)도 Access에 막힙니다. 지금도 tailnet 전용이라 달라지지 않습니다.
- 이메일·초대 링크는 계속 운영 주소(`MULTICA_APP_URL`, :3443)로 나갑니다.
