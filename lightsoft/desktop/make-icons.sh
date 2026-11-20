#!/usr/bin/env bash
# Regenerates Lightica's icons from lightsoft/desktop/icon-source.png (dark
# artwork on a light background): the macOS app icon (apps/desktop/build/lightica/)
# and the team web's home-screen icons (apps/web/public/lightica/). Upstream's
# icon files stay untouched so the daily release merge never hits a binary
# conflict; electron-builder.yml (`mac.icon`) and apps/web/features/lightsoft/brand.ts
# point here. The web favicon (apps/web/public/lightica/favicon.svg) is vector
# and drawn by hand from the same mark as packages/ui/components/common/lightica-icon.tsx.
# macOS only: ImageMagick cuts and places the artwork, iconutil packs the .icns.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
out="$here/../../apps/desktop/build/lightica"
web="$here/../../apps/web/public/lightica"
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

# Team web. Same canvases as upstream's apps/web/public/icons: icon-192/512 are a
# white plate inside a transparent margin with the artwork at the macOS ratio
# (640/824); apple-touch and maskable are opaque and full-bleed, since iOS and
# Android mask them themselves, with the artwork at 60% (inside the maskable
# safe zone).
magick "$here/icon-source.png" -alpha off -colorspace gray -fuzz 10% -trim +repage \
  -negate -background black -alpha shape "$work/art-full.png"
mkdir -p "$web"
plate() { # <canvas px> <plate px> <margin px>
  local s=$1 px=$2 off=$3 art=$(($2 * 640 / 824)) r=$(($2 * 225 / 1000))
  magick "$work/art-full.png" -resize "${art}x${art}" "$work/art-$s.png"
  magick -size "${s}x${s}" xc:none -fill white \
    -draw "roundrectangle $off,$off $((off + px - 1)),$((off + px - 1)) $r,$r" \
    "$work/art-$s.png" -gravity center -composite "PNG32:$web/icon-$s.png"
}
bleed() { # <file> <canvas px>
  local s=$2 art=$(($2 * 60 / 100))
  magick "$work/art-full.png" -resize "${art}x${art}" "$work/bleed-$s.png"
  magick -size "${s}x${s}" xc:white "$work/bleed-$s.png" -gravity center -composite \
    -background white -alpha remove -alpha off "PNG24:$web/$1"
}
plate 192 156 18
plate 512 412 50
bleed apple-touch-icon.png 180
bleed icon-maskable-512.png 512
echo "web icons written to $(cd "$web" && pwd)"
