#!/usr/bin/env bash
# Regenerates Lightica's iOS app icon (apps/mobile/assets/lightica/icon.png)
# from lightsoft/desktop/icon-source.png (dark artwork on a light background),
# the same source as the desktop icon. Upstream's apps/mobile/assets/icon.png
# stays untouched so the daily release merge never hits a binary conflict;
# app.config.ts points `icon` here.
#
# iOS wants a 1024px full-bleed square with no alpha channel: it masks the
# corners itself, so a padded or rounded source (like the macOS icon) would be
# masked twice. The artwork takes 616px, the same share of the canvas as
# upstream's mobile icon.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
out="$here/../../apps/mobile/assets/lightica"
art_size=616

mkdir -p "$out"
magick "$here/../desktop/icon-source.png" -alpha off -colorspace gray -fuzz 10% -trim +repage \
  -resize "${art_size}x${art_size}" -background white -gravity center -extent 1024x1024 \
  -colorspace sRGB -alpha off "PNG24:$out/icon.png"
echo "icon written to $(cd "$out" && pwd)/icon.png"
