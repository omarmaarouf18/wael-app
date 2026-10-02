# Frontend Status

Per-file tracker for `frontend/lib/screens/` and `frontend/lib/widgets/`. Generated from
the tree and `scripts/frontend_gate_baseline.txt` at the commit that added this file;
refresh the numbers when the baseline is lowered. Token and widget reference:
`docs/frontend/DESIGN_SYSTEM.md`.

- **Uses shared shell**: the screen builds on `AppShell`. Screens are migrated one per commit
  (F3a); until a screen is migrated it declares its own `Scaffold(`.
- **Baseline entries**: file/rule pairs in the gate baseline (violation count in
  parentheses). The baseline only goes down.
- **Catalog screen**: course catalog screens. They move with SPEC Phase 2-3,
  so avoid polishing them before the academy API lands.

## Screens

| File | Lines | Uses shared shell | Baseline entries (violations) | Catalog screen |
|------|------:|-------------------|------------------------------:|----------------|
| `course_detail_screen.dart` | 521 | yes | 0 (0) | yes |
| `courses_screen.dart` | 291 | yes | 0 (0) | yes |
| `ebook_screen.dart` | 27 | yes | 0 (0) | yes |
| `forgot_password_screen.dart` | 165 | yes | 0 (0) | no |
| `home_screen.dart` | 198 | yes | 0 (0) | yes |
| `login_screen.dart` | 343 | yes | 0 (0) | no |
| `main_shell.dart` | 103 | yes | 0 (0) | no |
| `notifications_screen.dart` | 189 | yes | 0 (0) | no |
| `otp_screen.dart` | 111 | yes | 0 (0) | no |
| `settings_screen.dart` | 550 | yes | 0 (0) | no |
| `signup_screen.dart` | 332 | yes | 0 (0) | no |
| `splash_screen.dart` | 61 | yes | 0 (0) | no |
| `video_player_screen.dart` | 436 | yes | 0 (0) | no |
| **Total** | 3327 | 13 of 13 | 0 (0) | 4 of 13 |

`main_shell.dart` hosts the bottom navigation, but it is a screen with its own
`Scaffold(`, not the shared template.

## Widgets

The gate scans screens only, so widgets carry no baseline entries.

| File | Lines | Used by screens |
|------|------:|-----------------|
| `accent_title.dart` | 56 | courses, home, subject content section |
| `app_badge.dart` | 67 | subject content section |
| `app_bottom_nav.dart` | 108 | main shell |
| `app_shell.dart` | 116 | 12 screens |
| `brand_lockup.dart` | 44 | main shell |
| `catalog_file_tile.dart` | 118 | subject content section |
| `catalog_level_header.dart` | 76 | courses |
| `catalog_subject_card.dart` | 179 | courses |
| `catalog_video_tile.dart` | 84 | subject content section |
| `confirm_action_dialog.dart` | 103 | none |
| `director_strip.dart` | 78 | course detail body |
| `file_details_sheet.dart` | 102 | subject content section |
| `header_icon_button.dart` | 52 | main shell |
| `hero_backdrop.dart` | 85 | login |
| `home_hero_banner.dart` | 207 | home |
| `icon_tile.dart` | 41 | notifications, settings |
| `instructor_dossier_card.dart` | 163 | home |
| `language_toggle_chip.dart` | 58 | login |
| `moving_watermark.dart` | 109 | none |
| `otp_pin_input.dart` | 178 | forgot password, otp |
| `owned_subject_tile.dart` | 83 | course detail body, home |
| `pill_filter_bar.dart` | 88 | none |
| `player_controls.dart` | 153 | none |
| `primary_button.dart` | 89 | 8 screens |
| `profile_avatar.dart` | 42 | settings |
| `protected_video_surface.dart` | 131 | video player |
| `search_field.dart` | 75 | courses |
| `secondary_button.dart` | 90 | home |
| `selectable_chip.dart` | 125 | courses, subject content section |
| `status_dot.dart` | 37 | notifications, settings |
| `subject_hero_banner.dart` | 76 | course detail body |
| `themed_card.dart` | 55 | 5 screens |
| `themed_empty_state.dart` | 64 | 4 screens |
| `themed_error_banner.dart` | 81 | 9 screens |
| `themed_loading_indicator.dart` | 49 | 4 screens |
| `themed_panel.dart` | 53 | 6 screens |
| `themed_section_header.dart` | 48 | settings |
| `themed_text_field.dart` | 104 | 6 screens |

The F2 widgets are exercised by `test/widget_layer_test.dart` (English and Arabic) and
shown in the debug-only component library (`lib/debug/component_library_screen.dart`,
route `/components`, linked from the diagnostics screen). "Used by screens" is computed
from the imports in `lib/screens/`. Screens are migrated onto them one per commit (F3a).
