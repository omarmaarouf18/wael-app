#!/usr/bin/env bash
# Builds the Android app icon set and the Play Store icon from the character
# art, with ImageMagick. Re-run it after changing the art or a crop box below.
#
#   scripts/make_app_icons.sh
#
# Source:  frontend/assets/branding/el_metr_character_art.png (1024 x 1536)
# Output:  Android adaptive icon (foreground + solid background colour),
#          legacy mipmap PNGs for every density, and the 512 x 512 store icon.
# Not generated: iOS, macOS, web and Windows icons (Android first, SPEC
# decision 20; those keep their current files).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ART="$REPO/frontend/assets/branding/el_metr_character_art.png"
RES="$REPO/frontend/android/app/src/main/res"
STORE="$REPO/frontend/assets/branding/store_icon_512.png"

command -v magick >/dev/null || { echo "ImageMagick (magick) is required" >&2; exit 1; }
[ -f "$ART" ] || { echo "missing $ART" >&2; exit 1; }

# Solid dark colour sampled from the art (mean of its dark corners and edges
# is 06 to 0D per channel); also the adaptive icon's background layer.
BG="#0A0A09"

# --- Crop boxes, in pixels of the 1024 x 1536 source -------------------------
# The hat spans x 350-665, y 140-380; the face ends near y 530; the red stripe
# runs x 700-805. Key content = x 350-805, y 140-530, centre (577, 335).
#
# Legacy and store icon: a 700 x 700 square at x 227, y 0, so the hat, face,
# tie and the red stripe all fit on a dark background.
SQ_W=700; SQ_X=227; SQ_Y=0
#
# Adaptive foreground: the 108 dp layer, of which only the central 66 dp
# circle (61%) is guaranteed visible. The key content needs a circle of about
# 600 px, so the layer covers 980 x 980 source pixels centred on (577, 335):
# x 87-1067, y -155-825. The part outside the art (right edge and top) is
# filled with BG, which is the same dark as the art's edges.
FG_W=980; FG_X=87; FG_Y=-155

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Square crop for the legacy and store icons.
magick "$ART" -crop "${SQ_W}x${SQ_W}+${SQ_X}+${SQ_Y}" +repage \
  -background "$BG" -alpha remove -alpha off "$tmp/square.png"

# Foreground layer: a BG canvas with the art placed at (-FG_X, -FG_Y).
magick -size "${FG_W}x${FG_W}" "xc:$BG" "$ART" \
  -geometry "$(printf '%+d%+d' "$((-FG_X))" "$((-FG_Y))")" -composite \
  -alpha off "$tmp/foreground.png"

# Densities: name:launcher px (48 dp) : foreground px (108 dp).
for spec in mdpi:48:108 hdpi:72:162 xhdpi:96:216 xxhdpi:144:324 xxxhdpi:192:432; do
  IFS=: read -r dpi legacy fg <<<"$spec"
  dir="$RES/mipmap-$dpi"
  mkdir -p "$dir"
  magick "$tmp/square.png" -resize "${legacy}x${legacy}" "$dir/ic_launcher.png"
  magick "$tmp/foreground.png" -resize "${fg}x${fg}" "$dir/ic_launcher_foreground.png"
done

# Adaptive icon definition (Android 8.0+) and its background colour.
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
magick "$tmp/square.png" -resize 512x512 "$STORE"

echo "icons written under $RES and $STORE"
