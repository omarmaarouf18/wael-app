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
and License columns. UNCONFIRMED rows block the release review
(SPEC Section 11, Phase 8), not development.

## Brand art — `frontend/assets/branding/`

| Path | Used as (constants.dart) | Source | License |
|---|---|---|---|
| `el_metr_character_art.png` | `imgCharacterArt` | UNCONFIRMED | UNCONFIRMED |
| `el_metr_landscape.jpg` | `imgLandscape` | UNCONFIRMED | UNCONFIRMED |
| `el_metr_poster_007.jpg` | `imgPoster007` | UNCONFIRMED | UNCONFIRMED |
| `elmtr_card.jpg` | `imgCardFront` | UNCONFIRMED | UNCONFIRMED |
| `elmtr_card_back.jpg` | `imgCardBack` | UNCONFIRMED | UNCONFIRMED |

## Screen imagery — `frontend/assets/images/`

| Path | Used as (constants.dart / model default) | Source | License |
|---|---|---|---|
| `login_portrait.jpg` | `imgLoginPortrait` | UNCONFIRMED | UNCONFIRMED |
| `home_hero.png` | `imgHomeHero` | UNCONFIRMED | UNCONFIRMED |
| `profile_alexander_vane.jpg` | `imgProfileDefault`; `UserProfile.avatarUrl` default | UNCONFIRMED | UNCONFIRMED |
| `featured_architectural_discipline.jpg` | `imgFeaturedArchitectural` | UNCONFIRMED | UNCONFIRMED |
| `catalog_composure.jpg` | `imgCatalogComposure` | UNCONFIRMED | UNCONFIRMED |
| `catalog_sovereign_rhetoric.jpg` | `imgCatalogRhetoric` | UNCONFIRMED | UNCONFIRMED |
| `catalog_private_protocol.jpg` | `imgCatalogProtocol` | UNCONFIRMED | UNCONFIRMED |
| `course_strategic_rhetoric.jpg` | `imgCourseStrategic` | UNCONFIRMED | UNCONFIRMED |
| `course_psychology_composure.jpg` | `imgCoursePsychology` | UNCONFIRMED | UNCONFIRMED |
| `course_executive_protocol.jpg` | `imgCourseExecutive` | UNCONFIRMED | UNCONFIRMED |

## Launcher and platform icons

App icons for each platform. Source is expected to be the brand logo, but
that has not been recorded; treat all as UNCONFIRMED.

| Path (set) | Platform | Source | License |
|---|---|---|---|
| `frontend/android/app/src/main/res/mipmap-*/ic_launcher.png` (5 densities) | Android launcher | UNCONFIRMED | UNCONFIRMED |
| `frontend/ios/Runner/Assets.xcassets/AppIcon.appiconset/*` (15 icons) | iOS app icon | UNCONFIRMED | UNCONFIRMED |
| `frontend/ios/Runner/Assets.xcassets/LaunchImage.imageset/*` (3 images) | iOS launch image | UNCONFIRMED | UNCONFIRMED |
| `frontend/macos/Runner/Assets.xcassets/AppIcon.appiconset/*` (7 icons) | macOS app icon | UNCONFIRMED | UNCONFIRMED |
| `frontend/web/favicon.png`, `frontend/web/icons/*` (5 files) | Web | UNCONFIRMED | UNCONFIRMED |
| `frontend/windows/runner/resources/app_icon.ico` | Windows app icon | UNCONFIRMED | UNCONFIRMED |

## Known gaps

- `frontend/lib/models/lesson.dart` defaults `videoPoster` to
  `assets/images/placeholder_course.png`, which is **not committed**. Either
  commit it with a provenance row or change the default.
