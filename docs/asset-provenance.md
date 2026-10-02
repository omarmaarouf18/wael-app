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
| `el_metr_character_art.png` | `imgCharacterArt`; the director portrait; the source of the app icon | Owner-supplied | Owner-approved 2026-10-02 |
| `el_metr_poster.jpg` | `imgPoster`; shown on the login screen only | Owner-supplied (the EL METR poster, 548 x 871 JPEG) | Owner-approved 2026-10-02 |
| `store_icon_512.png` | none (Play Store listing icon, not bundled in the app) | Generated 2026-10-02 from `el_metr_character_art.png` by `scripts/make_app_icons.sh` | Owner-approved 2026-10-02 |
| `el_metr_landscape.jpg` | `imgLandscape` | UNCONFIRMED | UNCONFIRMED |

## Screen imagery — `frontend/assets/images/`

| Path | Used as (constants.dart) | Source | License |
|---|---|---|---|
| `home_hero.png` | `imgHomeHero` | A downscaled copy (341 x 512) of `el_metr_character_art.png` | Owner-approved 2026-10-02 (same picture as the character art) |
| `catalog_composure.jpg` | `imgCatalogComposure` | UNCONFIRMED | UNCONFIRMED |

## Launcher and platform icons

App icons for each platform. Source is expected to be the brand logo, but
that has not been recorded; treat all as UNCONFIRMED.

| Path (set) | Platform | Source | License |
|---|---|---|---|
| `frontend/android/app/src/main/res/mipmap-*/ic_launcher.png` (5 densities), `ic_launcher_foreground.png` (5 densities), `mipmap-anydpi-v26/ic_launcher.xml`, `values/ic_launcher_background.xml` | Android launcher (adaptive and legacy) | Generated 2026-10-02 from `el_metr_character_art.png` by `scripts/make_app_icons.sh` | Owner-approved 2026-10-02 |
| `frontend/ios/Runner/Assets.xcassets/AppIcon.appiconset/*` (15 icons) | iOS app icon | UNCONFIRMED | UNCONFIRMED |
| `frontend/ios/Runner/Assets.xcassets/LaunchImage.imageset/*` (3 images) | iOS launch image | UNCONFIRMED | UNCONFIRMED |
| `frontend/macos/Runner/Assets.xcassets/AppIcon.appiconset/*` (7 icons) | macOS app icon | UNCONFIRMED | UNCONFIRMED |
| `frontend/web/favicon.png`, `frontend/web/icons/*` (5 files) | Web | UNCONFIRMED | UNCONFIRMED |
| `frontend/windows/runner/resources/app_icon.ico` | Windows app icon | UNCONFIRMED | UNCONFIRMED |

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
  to supply per-subject images or keep a single banner.
