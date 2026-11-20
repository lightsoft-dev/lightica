// @vitest-environment node
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { LIGHTICA_FEED_SIGNING_TEAM_KEY, LIGHTICA_UPDATE_FEED_URL } from "../shared/lightica";
import {
  canInstallUpdate,
  manualDownloadUrl,
  parseCodesignTeam,
  readOwnSigningTeam,
  updateNotificationContent,
} from "./lightica-update";

const DEVELOPER_ID = `Executable=/Applications/Lightica.app/Contents/MacOS/Lightica
Identifier=dev.lightsoft.lightica
Format=app bundle with Mach-O thin (arm64)
Signature size=9012
Authority=Developer ID Application: Lightsoft (ABCDE12345)
Authority=Developer ID Certification Authority
Authority=Apple Root CA
Notarization Ticket=stapled
TeamIdentifier=ABCDE12345
`;

describe("parseCodesignTeam", () => {
  it("reads the team of a Developer ID signature", () => {
    expect(parseCodesignTeam(DEVELOPER_ID)).toBe("ABCDE12345");
  });

  it("returns null for an ad-hoc signature", () => {
    expect(parseCodesignTeam("Identifier=dev.lightsoft.lightica\nSignature=adhoc\nTeamIdentifier=not set\n")).toBeNull();
  });

  it("returns null for a development certificate", () => {
    const development = DEVELOPER_ID.replace(
      "Developer ID Application: Lightsoft (ABCDE12345)",
      "Apple Development: Someone (QWERTY1234)",
    );
    expect(parseCodesignTeam(development)).toBeNull();
  });
});

describe("readOwnSigningTeam", () => {
  it("does not look for a signature off macOS", async () => {
    await expect(readOwnSigningTeam("/opt/Lightica/lightica", "linux")).resolves.toBeNull();
  });
});

describe("canInstallUpdate", () => {
  const signedBy = (team: string) => ({ version: "0.11.0-20261120", [LIGHTICA_FEED_SIGNING_TEAM_KEY]: team });

  it("installs only a build signed by its own team", () => {
    expect(canInstallUpdate("ABCDE12345", signedBy("ABCDE12345"))).toBe(true);
    expect(canInstallUpdate("ABCDE12345", signedBy("ZZZZZ99999"))).toBe(false);
    expect(canInstallUpdate("ABCDE12345", { version: "0.11.0-20261120" })).toBe(false);
    expect(canInstallUpdate(null, signedBy("ABCDE12345"))).toBe(false);
  });
});

describe("manualDownloadUrl", () => {
  it("points at the published dmg", () => {
    expect(
      manualDownloadUrl({
        files: [
          { url: "lightica-0.11.0-20261120-mac-arm64.zip" },
          { url: "lightica-0.11.0-20261120-mac-arm64.dmg" },
        ],
      }),
    ).toBe(`${LIGHTICA_UPDATE_FEED_URL}lightica-0.11.0-20261120-mac-arm64.dmg`);
  });

  it("falls back to the release page", () => {
    expect(manualDownloadUrl({ files: [{ url: "lightica-x.zip" }] })).toBe(LIGHTICA_UPDATE_FEED_URL);
    expect(manualDownloadUrl({})).toBe(LIGHTICA_UPDATE_FEED_URL);
  });
});

it("packages the same feed the app reads, never upstream releases", () => {
  // electron-builder writes publish.url into the bundled app-update.yml.
  const config = readFileSync(resolve(__dirname, "../../electron-builder.yml"), "utf-8");
  expect(config).not.toMatch(/provider:\s*github/);
  expect(config).not.toMatch(/^\s*owner:\s*multica-ai/m);
  const publish = config.slice(config.indexOf("\npublish:"));
  expect(publish).toMatch(/^\s+provider: generic$/m);
  expect(publish).toMatch(new RegExp(`^\\s+url: ${LIGHTICA_UPDATE_FEED_URL.replace(/[.]/g, "\\.")}$`, "m"));
});

describe("updateNotificationContent", () => {
  it("names the version that is ready to install", () => {
    expect(updateNotificationContent({ kind: "ready", version: "0.11.1-20261121" })).toEqual({
      title: "Lightica 업데이트 준비됨",
      body: 'v0.11.1-20261121이(가) 준비됐습니다. 창에서 "지금 업데이트"를 누르면 적용됩니다.',
    });
  });

  it("points at the dmg when the app cannot install it itself", () => {
    expect(updateNotificationContent({ kind: "manual", version: "0.11.1-20261121" })).toEqual({
      title: "Lightica v0.11.1-20261121",
      body: "새 버전이 있습니다. dmg를 받아 Applications의 Lightica를 덮어쓰세요.",
    });
  });
});
