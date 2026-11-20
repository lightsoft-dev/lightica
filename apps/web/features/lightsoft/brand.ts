import type { MetadataRoute } from "next";

// The team client shows up in browser tabs and on home screens as Lightica,
// with the same mark as the Lightica desktop app. The files live in
// public/assets/lightica/ (lightsoft/desktop/make-icons.sh) instead of replacing
// upstream's public/favicon.svg and public/icons/, which upstream rebrands
// often: the daily release merge then never conflicts on them, and upstream's
// brand sync check keeps passing.
//
// Not public/lightica/: on :3444 (tailscale serve) and the Cloudflare tunnel,
// /lightica goes to the Lightica release feed (127.0.0.1:13400), not to this
// app, so every icon there was a 404. brand.test.ts keeps the paths off the
// prefixes those proxies route elsewhere (lightsoft/README.md "구성").
export const LIGHTICA_NAME = "Lightica";

export const LIGHTICA_FAVICON = "/assets/lightica/favicon.svg";

export const LIGHTICA_APPLE_TOUCH_ICON = "/assets/lightica/apple-touch-icon.png";

export const LIGHTICA_MANIFEST_ICONS: MetadataRoute.Manifest["icons"] = [
  {
    src: "/assets/lightica/icon-192.png",
    sizes: "192x192",
    type: "image/png",
    purpose: "any",
  },
  {
    src: "/assets/lightica/icon-512.png",
    sizes: "512x512",
    type: "image/png",
    purpose: "any",
  },
  {
    src: "/assets/lightica/icon-maskable-512.png",
    sizes: "512x512",
    type: "image/png",
    purpose: "maskable",
  },
];
