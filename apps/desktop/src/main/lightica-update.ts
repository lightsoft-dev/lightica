// Lightica update decisions (LIGH-191). The feed is the team channel that
// lightsoft/desktop/release.sh publishes; see lightsoft/desktop/README.md.
//
// Squirrel.Mac replaces the app only when the downloaded bundle satisfies the
// running app's designated requirement. For a Developer ID build that pins the
// signing team, and an ad-hoc build can never be updated in place. So the app
// downloads and installs an update itself only when the feed says the new
// build is signed by its own team; otherwise it links to the dmg.
import { execFile } from "node:child_process";
import { dirname, resolve } from "node:path";
import { LIGHTICA_FEED_SIGNING_TEAM_KEY, LIGHTICA_UPDATE_FEED_URL } from "../shared/lightica";

const CODESIGN_TIMEOUT_MS = 10_000;

/** Team ID from `codesign -dv` output, or null unless it is a Developer ID signature. */
export function parseCodesignTeam(output: string): string | null {
  if (!/^Authority=Developer ID Application: /m.test(output)) return null;
  return /^TeamIdentifier=([A-Z0-9]{10})$/m.exec(output)?.[1] ?? null;
}

/** The Developer ID team that signed this running app (macOS), or null. */
export function readOwnSigningTeam(
  execPath: string = process.execPath,
  platform: NodeJS.Platform = process.platform,
): Promise<string | null> {
  if (platform !== "darwin") return Promise.resolve(null);
  // <bundle>.app/Contents/MacOS/<executable>
  const bundle = resolve(dirname(execPath), "..", "..");
  return new Promise((done) => {
    execFile(
      "/usr/bin/codesign",
      ["-dv", "--verbose=2", bundle],
      { timeout: CODESIGN_TIMEOUT_MS },
      (err, stdout, stderr) => done(err ? null : parseCodesignTeam(`${stdout}\n${stderr}`)),
    );
  });
}

/** True when the published build can replace this app in place. */
export function canInstallUpdate(ownTeam: string | null, updateInfo: object): boolean {
  if (!ownTeam) return false;
  return (updateInfo as Record<string, unknown>)[LIGHTICA_FEED_SIGNING_TEAM_KEY] === ownTeam;
}

/** The dmg of the published build, falling back to the release page. */
export function manualDownloadUrl(
  updateInfo: { files?: ReadonlyArray<{ url: string }> },
  feedUrl: string = LIGHTICA_UPDATE_FEED_URL,
): string {
  const dmg = updateInfo.files?.find((file) => file.url.endsWith(".dmg"));
  return new URL(dmg?.url ?? "", feedUrl).toString();
}
