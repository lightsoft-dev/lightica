// @vitest-environment node
// Lightsoft fork (LIGH-258): checks that keep the Lightica build's identity
// consistent across the files that cannot import lightica.ts.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";
import type { ConfigContext } from "expo/config";
import createConfig from "./app.config";
import { LIGHTICA_APP_NAME, LIGHTICA_SERVER_URL } from "./lightica";

const here = __dirname;
const read = (path: string) => readFileSync(join(here, path), "utf8");

function envValue(file: string, key: string): string | undefined {
  const line = read(file)
    .split("\n")
    .find((l) => l.startsWith(`${key}=`));
  return line?.slice(key.length + 1).trim();
}

function markPath(source: string): string {
  const match = /const LIGHTICA_MARK_PATH =([\s\S]*?);/.exec(source);
  if (!match?.[1]) throw new Error("LIGHTICA_MARK_PATH not found");
  return [...match[1].matchAll(/"([^"]*)"/g)].map((m) => m[1]).join("");
}

describe("Lightica mobile identity", () => {
  it("builds the release app under the name the sign-in screens show", () => {
    vi.stubEnv("APP_ENV", "production");
    try {
      expect(createConfig({ config: {} } as ConfigContext).name).toBe(LIGHTICA_APP_NAME);
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it.each(["EXPO_PUBLIC_API_URL", "EXPO_PUBLIC_WEB_URL"])(
    "points the release build's %s at the team client",
    (key) => {
      expect(envValue(".env.production", key)).toBe(LIGHTICA_SERVER_URL);
    },
  );

  it("draws the same mark as the team web", () => {
    const mobile = markPath(read("components/brand/lightica-logo.tsx"));
    const web = markPath(read("../../packages/ui/components/common/lightica-icon.tsx"));
    expect(mobile).toBe(web);
  });

  it("ships a 1024px app icon without an alpha channel", () => {
    // PNG IHDR: width and height at bytes 16-23, colour type at byte 25.
    // iOS rejects an icon with alpha; types 0 (grey) and 2 (RGB) have none.
    const png = readFileSync(join(here, "assets/lightica/icon.png"));
    expect(png.readUInt32BE(16)).toBe(1024);
    expect(png.readUInt32BE(20)).toBe(1024);
    expect([0, 2]).toContain(png[25]);
  });
});
