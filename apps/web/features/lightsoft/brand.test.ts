// @vitest-environment node
import { existsSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  LIGHTICA_APPLE_TOUCH_ICON,
  LIGHTICA_FAVICON,
  LIGHTICA_MANIFEST_ICONS,
} from "./brand";

// Path prefixes that tailscale serve :3444 and the Cloudflare tunnel
// (lightsoft/access/tunnel.yml.template) send somewhere other than the team web
// (lightsoft/README.md "구성"). A team-web asset under one of them never reaches
// the browser: LIGH-257 first shipped the icons under /lightica/, which is the
// Lightica release feed, and the favicon was a 404 on :3444.
const ROUTED_ELSEWHERE = [
  "/api",
  "/ws",
  "/v1",
  "/health",
  "/lightica",
  "/lightsoft-term",
  "/lightsoft-secrets",
  "/lightsoft-usage",
];

const iconPaths = [
  LIGHTICA_FAVICON,
  LIGHTICA_APPLE_TOUCH_ICON,
  ...(LIGHTICA_MANIFEST_ICONS ?? []).map((icon) => icon.src),
];

describe("Lightica web icons", () => {
  it.each(iconPaths)("%s is served by the team web", (src) => {
    for (const prefix of ROUTED_ELSEWHERE) {
      expect(src === prefix || src.startsWith(`${prefix}/`)).toBe(false);
    }
  });

  it.each(iconPaths)("%s exists in public/", (src) => {
    expect(existsSync(join(__dirname, "../../public", src))).toBe(true);
  });
});
