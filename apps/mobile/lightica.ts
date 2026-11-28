// Lightica is Lightsoft's internal build of this mobile app (LIGH-258), the
// phone counterpart of the Lightica desktop app (apps/desktop/src/shared/lightica.ts).
// The build-time identity (display name, bundle ids, URL scheme, icon) is in
// app.config.ts; these are the values the app itself reads.
// Build and install steps: lightsoft/mobile/README.md.

export const LIGHTICA_APP_NAME = "Lightica";

// The team client origin. Tailscale serves the team web on `/` and the
// production API on `/api` and `/ws`, so one URL covers API, WebSocket and the
// "Open on web" links. The phone must be on the tailnet to reach it.
// .env.production carries it into the bundle; lightica.test.ts keeps the two
// equal.
export const LIGHTICA_SERVER_URL = "https://mac-studio.taile83671.ts.net:3444";
