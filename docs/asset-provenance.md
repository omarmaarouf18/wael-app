# Committed asset provenance

This repository is public. Every binary asset (image, icon, font, audio,
video) committed to git must be listed here with its provenance, in the same
commit that adds it. An asset with no recorded provenance must not be
committed; an asset whose provenance cannot be established must not be
published. *(Phase 0.0, owner review 2026-09-30.)*

## Rules

1. One row per asset (or per asset set), added in the same commit as the
   asset.
2. Source must name the origin: owner-supplied, commissioned, stock (site
   and URL), or generated (tool and date).
3. License must state the right to publish it in a public repository.
4. Never commit third-party assets of unknown origin.
5. Do not put content hashes in this file; the pre-push hook validates
   40-char hex strings in markdown as commit SHAs.

## Status

All assets below were introduced with the frontend wiring commit
`b900b17` (2026-09), without a provenance
record. Every row is **UNCONFIRMED** until the owner fills in the Source
and License columns, except the rows the owner confirmed on 2026-10-02 (the
character art, the EL METR poster, the Home hero copy of the art, and the
Android app icon set), which say so. UNCONFIRMED rows block the release review
(SPEC Section 11, Phase 8), not development.

## Brand art — `frontend/assets/branding/`

| Path | Used as (constants.dart) | Source | License |
|---|---|---|---|
| `el_metr_character_art.png` | `imgCharacterArt`; the director portrait; the source of the app icon *(amended 2026-10-08: no longer the director portrait, replaced by `director_portrait.jpg`; still the Home hero art, the subject-image fallback and the icon source)* | Owner-supplied | Owner-approved 2026-10-02 |
| `el_metr_character_art.jpg` | `imgCharacterArt` *(added 2026-10-12: the bundled copy; the PNG above is no longer bundled and stays the icon source)* | A JPEG copy (1024 x 1536, quality 88, metadata stripped, PSNR 41.4 dB against the PNG, 190 KB instead of 2 MB) of `el_metr_character_art.png` | Same picture as the owner-approved character art (2026-10-02) |
| `el_metr_poster.jpg` | `imgPoster`; shown on the login screen only | Owner-supplied (the EL METR poster, 548 x 871 JPEG) | Owner-approved 2026-10-02 |
| `store_icon_512.png` | none (Play Store listing icon, not bundled in the app) | Generated 2026-10-02 from `el_metr_character_art.png` by `scripts/make_app_icons.sh` *(amended 2026-10-14: now generated from `app_icon_1024.png`)* | Owner-approved 2026-10-02 |
| `app_icon_1024.png`, `app_icon_adaptive_foreground.png` | none (icon sources for `scripts/make_app_icons.sh`, not bundled) | Owner-supplied EL METR icon (180 x 180), upscaled 4x with Real-ESRGAN (anime 6B model) to 1024, rounded corners filled with the art's background, and the small text lines (EL METR / What is the law..? / 0122 27 007 27) replaced by the same text taken from `el_metr_poster.jpg` (same layout, upscaled 4x the same way) so it stays legible; the adaptive foreground places it at 60/108 dp on the extended background | Owner-supplied 2026-10-14 |
| `el_metr_landscape.jpg` | `imgLandscape` | UNCONFIRMED | UNCONFIRMED |

## Screen imagery — `frontend/assets/images/`

| Path | Used as (constants.dart) | Source | License |
|---|---|---|---|
| `home_hero.png` | `imgHomeHero` | A downscaled copy (341 x 512) of `el_metr_character_art.png` | Owner-approved 2026-10-02 (same picture as the character art) |
| `elmetr_banner.jpg` | `imgSubjectBanner`; the banner at the top of every subject screen | Owner-supplied EL METR banner (1242 x 649 JPEG; the art's own text includes a phone number line, as supplied), 2026-10-08. Replaces `catalog_composure.jpg` (source never confirmed, deleted) | Owner-supplied 2026-10-08 |
| `director_portrait.jpg` | `imgDirectorPortrait`; the director portrait (Home director card, subject strip) | Owner-supplied photo of Ustaz Wael Al-Saeed (800 x 800 JPEG, head and shoulders), used with his consent, 2026-10-08. *(Amended 2026-10-08: replaced in place by the owner's v2, `director_portrait_v2.jpg` from "Claude outputs/director/", 800 x 800 JPEG, black-and-white close-up; same path and constant, titles unchanged.)* *(Amended 2026-10-12, owner decision: replaced in place by v3 — source: owner-supplied studio photo (1280 x 1920, photographer credit Hazem El layub Photography, the mark on the original); cropped only (square head-and-shoulders crop 720 x 720 at x 180, y 17; the photographer's mark is outside the crop; no retouching, no colour, contrast or sharpening change), metadata stripped, 720 x 720 progressive JPEG, 51 506 bytes; used with the director's consent; photographer's permission to use it in the app: confirmed by the owner (2026-10-12). The original is not in the repository.)* | CONFIRMED by owner 2026-10-08; v3 CONFIRMED by owner 2026-10-12 |

## Launcher and platform icons

App icons for each platform. Source is expected to be the brand logo, but
that has not been recorded; treat all as UNCONFIRMED.

| Path (set) | Platform | Source | License |
|---|---|---|---|
| `frontend/android/app/src/main/res/mipmap-*/ic_launcher.png` (5 densities), `ic_launcher_foreground.png` (5 densities), `mipmap-anydpi-v26/ic_launcher.xml`, `values/ic_launcher_background.xml` | Android launcher (adaptive and legacy) | Generated 2026-10-02 from `el_metr_character_art.png` by `scripts/make_app_icons.sh` | Owner-approved 2026-10-02 |
| `frontend/ios/Runner/Assets.xcassets/AppIcon.appiconset/*` (15 icons) | iOS app icon | Generated 2026-10-09 from `el_metr_character_art.png` by `scripts/make_app_icons.sh ios` (same square crop as the Android legacy icon, alpha removed) | Same source as the owner-approved Android icon (2026-10-02) |
| `frontend/ios/Runner/Assets.xcassets/LaunchImage.imageset/*` (3 images) | iOS launch image | UNCONFIRMED | UNCONFIRMED |
| `frontend/macos/Runner/Assets.xcassets/AppIcon.appiconset/*` (7 icons) | macOS app icon | UNCONFIRMED | UNCONFIRMED |
| `frontend/web/favicon.png`, `frontend/web/icons/*` (5 files) | Web | UNCONFIRMED | UNCONFIRMED |
| `frontend/windows/runner/resources/app_icon.ico` | Windows app icon | UNCONFIRMED | UNCONFIRMED |

## Bundled fonts — `frontend/assets/fonts/` (F-UX4 Commit 2, 2026-10-05)

Only the weights the app uses (see `frontend/pubspec.yaml` `fonts:`).
Runtime fetching is disabled (`GoogleFonts.config.allowRuntimeFetching =
false` in `lib/main.dart`), so nothing is requested from Google at runtime.

| Path (set) | Source | License |
|---|---|---|
| `Cairo-Regular.ttf` (400), `Cairo-SemiBold.ttf` (600), `Cairo-Bold.ttf` (700), `Cairo-ExtraBold.ttf` (800); `OFL-Cairo.txt` | Google Fonts (`fonts.google.com/specimen/Cairo`), static TTF instances via the `fonts.googleapis.com/css2` API | SIL Open Font License 1.1 (`OFL-Cairo.txt`) |
| `Syne-SemiBold.ttf` (600), `Syne-Bold.ttf` (700), `Syne-ExtraBold.ttf` (800); `OFL-Syne.txt` | Google Fonts (`fonts.google.com/specimen/Syne`), static TTF instances via the `fonts.googleapis.com/css2` API | SIL Open Font License 1.1 (`OFL-Syne.txt`) |
| `PlusJakartaSans-Regular.ttf` (400), `PlusJakartaSans-SemiBold.ttf` (600), `PlusJakartaSans-Bold.ttf` (700); `OFL-PlusJakartaSans.txt` | Google Fonts (`fonts.google.com/specimen/Plus+Jakarta+Sans`), static TTF instances via the `fonts.googleapis.com/css2` API | SIL Open Font License 1.1 (`OFL-PlusJakartaSans.txt`) |

## Removed assets

Deleted on 2026-10-02 because nothing referenced them any more (they belonged
to the removed mock library, mock catalog and stock profile photo; see
`docs/frontend/CONTENT-GAPS.md`): `el_metr_poster_007.jpg`, `elmtr_card.jpg`,
`elmtr_card_back.jpg`, `profile_alexander_vane.jpg`,
`featured_architectural_discipline.jpg`, `catalog_sovereign_rhetoric.jpg`,
`catalog_private_protocol.jpg`, `course_strategic_rhetoric.jpg`,
`course_psychology_composure.jpg` and `course_executive_protocol.jpg`. They
remain in git history.

Also deleted 2026-10-02: `login_portrait.jpg` (the login screen now shows the
poster; it was the same picture as `home_hero.png`, which stays). The poster
`el_metr_poster_007.jpg` listed above is byte-identical to today's
`el_metr_poster.jpg`: the owner asked for it back on 2026-10-02, under the new
name, for the login screen.

## Known gaps

- `el_metr_landscape.jpg` (`imgLandscape`) is committed but referenced by no
  screen. It is kept until the owner decides whether it is wanted.
- `catalog_composure.jpg` is the one decorative banner shown at the top of every
  subject screen. Subject images are not in the API; the owner decides whether
  to supply per-subject images or keep a single banner. *(Amended 2026-10-08:
  replaced by the owner's `elmetr_banner.jpg`, still one banner for every
  subject; `catalog_composure.jpg` is deleted.)*
