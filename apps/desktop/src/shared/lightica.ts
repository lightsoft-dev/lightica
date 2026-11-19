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

// Lightica never polls an update feed: a new build is installed by hand after
// `lightsoft/dev` moves, so upstream releases can never replace it. Typed as
// boolean so the updater keeps upstream's code path compiled and testable.
export const LIGHTICA_AUTO_UPDATE: boolean = false;
// Shown by the manual "check for updates" action.
export const LIGHTICA_UPDATES_DISABLED_MESSAGE =
  "Lightica는 자동 업데이트를 쓰지 않습니다. 새 빌드(dmg)를 받아 다시 설치하세요.";
