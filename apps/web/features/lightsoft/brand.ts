import type { MetadataRoute } from "next";

// The team client shows up in browser tabs and on home screens as Lightica,
// with the same mark as the Lightica desktop app. The files live in
// public/lightica/ (lightsoft/desktop/make-icons.sh) instead of replacing
// upstream's public/favicon.svg and public/icons/, which upstream rebrands
// often: the daily release merge then never conflicts on them, and upstream's
// brand sync check keeps passing.
export const LIGHTICA_NAME = "Lightica";

export const LIGHTICA_FAVICON = "/lightica/favicon.svg";

export const LIGHTICA_APPLE_TOUCH_ICON = "/lightica/apple-touch-icon.png";

export const LIGHTICA_MANIFEST_ICONS: MetadataRoute.Manifest["icons"] = [
  {
    src: "/lightica/icon-192.png",
    sizes: "192x192",
    type: "image/png",
    purpose: "any",
  },
  {
    src: "/lightica/icon-512.png",
    sizes: "512x512",
    type: "image/png",
    purpose: "any",
  },
  {
    src: "/lightica/icon-maskable-512.png",
    sizes: "512x512",
    type: "image/png",
    purpose: "maskable",
  },
];
