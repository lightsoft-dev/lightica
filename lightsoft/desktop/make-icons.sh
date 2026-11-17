#!/usr/bin/env bash
# Regenerates Lightica's macOS app icon (apps/desktop/build/lightica/) from
# lightsoft/desktop/icon.svg. Upstream's icon files stay untouched so the daily
# release merge never hits a binary conflict; electron-builder.yml points
# `mac.icon` here. macOS only: Quick Look renders the SVG, ImageMagick cuts
# and resizes, iconutil packs the .icns.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
out="$here/../../apps/desktop/build/lightica"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

qlmanage -t -s 1024 -o "$work" "$here/icon.svg" >/dev/null
# Quick Look renders onto white; cut the icon back out with the SVG's own
# rounded rect (inset 1px so no white fringe survives on the edge).
magick "$work/icon.svg.png" \
  \( -size 1024x1024 xc:none -fill white -draw "roundrectangle 101,101 922,922 184,184" \) \
  -compose CopyOpacity -composite "$work/icon-1024.png"

mkdir -p "$out"
cp "$work/icon-1024.png" "$out/icon.png"

iconset="$work/icon.iconset"
mkdir "$iconset"
for s in 16 32 128 256 512; do
  magick "$work/icon-1024.png" -resize "${s}x${s}" "$iconset/icon_${s}x${s}.png"
  magick "$work/icon-1024.png" -resize "$((s * 2))x$((s * 2))" "$iconset/icon_${s}x${s}@2x.png"
done
iconutil -c icns -o "$out/icon.icns" "$iconset"
echo "icons written to $(cd "$out" && pwd)"
