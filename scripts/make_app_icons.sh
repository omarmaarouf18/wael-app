#!/usr/bin/env bash
# Builds the Android and iOS app icon sets and the Play Store icon, with
# ImageMagick. Re-run it after changing either source image below.
#
#   scripts/make_app_icons.sh            # android and ios
#   scripts/make_app_icons.sh android    # android + Play Store icon only
#   scripts/make_app_icons.sh ios        # iOS AppIcon set only
#
# Sources (owner's EL METR icon, 2026-10-14; AI-upscaled 4x from the owner's
# 180 x 180 original with Real-ESRGAN, rounded corners filled with the art's
# own background so the stores' masks apply cleanly; the text block is the
# same text from el_metr_poster.jpg, upscaled the same way, so it is legible):
#   frontend/assets/branding/app_icon_1024.png                 (1024 x 1024)
#     full-bleed square: legacy mipmaps, Play Store icon, iOS set
#   frontend/assets/branding/app_icon_adaptive_foreground.png  (1080 x 1080)
#     Android adaptive foreground (108 dp): the icon at 60 dp in the centre,
#     inside the 66 dp safe circle, on its own background extended to the edge
# No alpha anywhere (App Store Connect rejects a 1024 icon with alpha).
# Not generated: macOS, web and Windows icons.
# *(Amended 2026-10-14, owner decision: new icon source; previously cropped
# from el_metr_character_art.png.)*
# *(Amended 2026-10-14, later, owner decision: new source art supplied by the
# owner (1254 x 1254, black background, full text block kept as requested).
# app_icon_1024.png = the art at 900 px centred on black; the adaptive
# foreground = the art at 600 px centred on a black 1080 square, so the text
# stays inside the launcher masks. The upscaling note above describes the
# previous source.)*
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SQ="$REPO/frontend/assets/branding/app_icon_1024.png"
FG="$REPO/frontend/assets/branding/app_icon_adaptive_foreground.png"
RES="$REPO/frontend/android/app/src/main/res"
STORE="$REPO/frontend/assets/branding/store_icon_512.png"
IOS_SET="$REPO/frontend/ios/Runner/Assets.xcassets/AppIcon.appiconset"

TARGET="${1:-all}"
case "$TARGET" in
  all|android|ios) ;;
  *) echo "usage: $0 [all|android|ios]" >&2; exit 2 ;;
esac

if command -v magick >/dev/null; then IM=magick
elif command -v convert >/dev/null; then IM=convert
else echo "ImageMagick (magick or convert) is required" >&2; exit 1; fi
for f in "$SQ" "$FG"; do [ -f "$f" ] || { echo "missing $f" >&2; exit 1; }; done

# Adaptive icon background colour (hidden behind the opaque foreground).
BG="#0A0A09"

resize() { "$IM" "$1" -filter Lanczos -resize "${2}x${2}" -background "$BG" \
  -alpha remove -alpha off "$3"; }

if [ "$TARGET" = all ] || [ "$TARGET" = ios ]; then
  [ -d "$IOS_SET" ] || { echo "missing $IOS_SET" >&2; exit 1; }
  for spec in 20x20@1x:20 20x20@2x:40 20x20@3x:60 29x29@1x:29 29x29@2x:58 \
    29x29@3x:87 40x40@1x:40 40x40@2x:80 40x40@3x:120 60x60@2x:120 60x60@3x:180 \
    76x76@1x:76 76x76@2x:152 83.5x83.5@2x:167 1024x1024@1x:1024; do
    IFS=: read -r name px <<<"$spec"
    resize "$SQ" "$px" "$IOS_SET/Icon-App-$name.png"
  done
  echo "iOS icons written under $IOS_SET"
fi

[ "$TARGET" = ios ] && exit 0

# Densities: name:launcher px (48 dp) : foreground px (108 dp).
for spec in mdpi:48:108 hdpi:72:162 xhdpi:96:216 xxhdpi:144:324 xxxhdpi:192:432; do
  IFS=: read -r dpi legacy fg <<<"$spec"
  dir="$RES/mipmap-$dpi"
  mkdir -p "$dir"
  resize "$SQ" "$legacy" "$dir/ic_launcher.png"
  resize "$FG" "$fg" "$dir/ic_launcher_foreground.png"
done

mkdir -p "$RES/mipmap-anydpi-v26" "$RES/values"
cat >"$RES/mipmap-anydpi-v26/ic_launcher.xml" <<'XML'
<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background"/>
    <foreground android:drawable="@mipmap/ic_launcher_foreground"/>
</adaptive-icon>
XML
cat >"$RES/values/ic_launcher_background.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<resources>
    <color name="ic_launcher_background">$BG</color>
</resources>
XML

# Play Store icon: 512 x 512 PNG, full bleed (Play applies its own mask).
resize "$SQ" 512 "$STORE"

echo "icons written under $RES and $STORE"
