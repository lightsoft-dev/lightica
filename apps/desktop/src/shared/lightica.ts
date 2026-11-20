// Lightica is Lightsoft's internal build of this desktop app (LIGH-110).
// Every identity below differs from the official Multica app so both can be
// installed on one Mac without sharing a bundle, deep-link scheme, userData,
// runtime config, or daemon profile.
//
// Keep in sync with the packaging side: `appId`, `productName`, `protocols`
// and `publish` in electron-builder.yml, and `productName` in package.json
// (the packaged ASAR's productName is what names userData).
// Build and release steps: lightsoft/desktop/README.md.

export const LIGHTICA_APP_NAME = "Lightica";
export const LIGHTICA_APP_ID = "dev.lightsoft.lightica";
export const LIGHTICA_PROTOCOL = "lightica";

// Desktop-owned files that upstream keeps directly under ~/.multica
// (desktop.json, desktop_prefs.json) live here instead.
export const LIGHTICA_HOME_DIR = ".lightica";

// Daemon profile name prefix. The Go CLI owns ~/.multica/profiles/, so the
// profile directory stays there: ~/.multica/profiles/lightica-<host>/.
export const LIGHTICA_PROFILE_PREFIX = "lightica";

// The team client origin. Tailscale serves the team web on `/` and the
// production API on `/api` and `/ws`, so one URL covers api, ws and app.
export const LIGHTICA_DEFAULT_API_URL = "https://mac-studio.taile83671.ts.net:3444";

// The team release channel (LIGH-191): lightsoft/desktop/release.sh publishes
// each build here and Tailscale serves it on the team client origin, so only
// the tailnet can reach it. electron-builder.yml `publish.url` must match.
export const LIGHTICA_UPDATE_FEED_URL = `${LIGHTICA_DEFAULT_API_URL}/lightica/`;
// The release page (version history and downloads) is the feed's index.html.
export const LIGHTICA_RELEASES_URL = LIGHTICA_UPDATE_FEED_URL;

// Lightica takes no update from upstream (LIGH-191). The app reads only the
// team feed above, and the daemon runs only the CLI bundled in this build:
// upstream's fallbacks — installing multica-ai's latest release into userData,
// or borrowing a `multica` found on PATH — are off. (The daemon itself already
// refuses GitHub and Runtimes-page self-updates when Desktop launches it.)
// Typed as boolean so upstream's fallback stays compiled and tested.
export const LIGHTICA_BUNDLED_CLI_ONLY: boolean = true;
export const LIGHTICA_CLI_MISSING_MESSAGE =
  "Lightica에 들어 있는 CLI를 실행할 수 없습니다. 릴리스 페이지에서 Lightica를 다시 설치하세요.";

// latest-mac.yml key that release.sh adds: the code-signing team ID of the
// published build. Squirrel.Mac only installs a bundle signed by the running
// app's own Developer ID team, so the app installs an update itself only when
// this matches its own signature and otherwise links to the dmg.
export const LIGHTICA_FEED_SIGNING_TEAM_KEY = "lighticaSigningTeam";

// Copy for the update prompts. Lightica's own strings stay here (Korean, for
// the team) instead of upstream's locale files.
export const LIGHTICA_UPDATE_COPY = {
  // Downloaded and installable in place (LIGH-268): one click quits, installs
  // and reopens the app.
  readyTitle: (version: string) => `Lightica v${version}`,
  readyBody: "업데이트가 준비됐습니다. 앱이 종료된 뒤 새 버전으로 다시 열립니다.",
  installNow: "지금 업데이트",
  // The "install it yourself" path.
  manualTitle: (version: string) => `Lightica v${version}`,
  manualBody: "새 버전이 있습니다. dmg를 받아 Applications의 Lightica를 덮어쓰세요.",
  manualStatus: (version: string) => `새 버전 v${version}을(를) 받을 수 있습니다.`,
  download: "다운로드",
  releases: "릴리스 기록",
};
