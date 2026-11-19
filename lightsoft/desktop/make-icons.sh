#!/usr/bin/env bash
# Regenerates Lightica's macOS app icon (apps/desktop/build/lightica/) from
# lightsoft/desktop/icon-source.png (dark artwork on a light background).
# Upstream's icon files stay untouched so the daily release merge never hits a
# binary conflict; electron-builder.yml points `mac.icon` here. macOS only:
# ImageMagick cuts and places the artwork, iconutil packs the .icns.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
out="$here/../../apps/desktop/build/lightica"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Apple's macOS icon grid: a 824px rounded plate centred on a 1024px canvas.
# The artwork keeps roughly the template's inner margin (640px inside 824px).
art_size=640

# Artwork darkness becomes alpha, so only the shape (black) lands on the plate,
# whatever the source's background or padding.
magick "$here/icon-source.png" -alpha off -colorspace gray -fuzz 10% -trim +repage \
  -resize "${art_size}x${art_size}" -negate -background black -alpha shape "$work/art.png"
magick -size 1024x1024 xc:none -fill white -draw "roundrectangle 100,100 923,923 185,185" \
  "$work/art.png" -gravity center -composite "PNG32:$work/icon-1024.png"

mkdir -p "$out"
cp "$work/icon-1024.png" "$out/icon.png"

iconset="$work/icon.iconset"
mkdir "$iconset"
for s in 16 32 128 256 512; do
  magick "$work/icon-1024.png" -resize "${s}x${s}" "PNG32:$iconset/icon_${s}x${s}.png"
  magick "$work/icon-1024.png" -resize "$((s * 2))x$((s * 2))" "PNG32:$iconset/icon_${s}x${s}@2x.png"
done
iconutil -c icns -o "$out/icon.icns" "$iconset"
echo "icons written to $(cd "$out" && pwd)"
