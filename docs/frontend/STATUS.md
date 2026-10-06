# Frontend Status

Per-file tracker for `frontend/lib/screens/` and `frontend/lib/widgets/`. Generated from
the tree and `scripts/frontend_gate_baseline.txt` at the commit that added this file;
refresh the numbers when the baseline is lowered. *(Refreshed 2026-10-06 from the tree of `origin/develop` at `79e1962`: line counts, the widget list and the "used by" column are recomputed from the files and from the imports under `lib/screens/`; the baseline is empty, so every entry is 0.)* Token and widget reference:
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
| `course_detail_screen.dart` | 122 | yes | 0 (0) | yes |
| `courses_screen.dart` | 312 | yes | 0 (0) | yes |
| `ebook_screen.dart` | 53 | yes | 0 (0) | yes |
| `forgot_password_screen.dart` | 251 | yes | 0 (0) | no |
| `home_screen.dart` | 200 | yes | 0 (0) | yes |
| `login_screen.dart` | 330 | yes | 0 (0) | no |
| `main_shell.dart` | 238 | yes | 0 (0) | no |
| `notifications_screen.dart` | 221 | yes | 0 (0) | no |
| `otp_screen.dart` | 168 | yes | 0 (0) | no |
| `settings_screen.dart` | 397 | yes | 0 (0) | no |
| `settings/delete_account_screen.dart` | 242 | yes | 0 (0) | no |
| `settings/devices_screen.dart` | 255 | yes | 0 (0) | no |
| `settings/email_change_screen.dart` | 156 | yes | 0 (0) | no |
| `settings/password_change_screen.dart` | 124 | yes | 0 (0) | no |
| `settings/profile_section.dart` | 338 | n/a (section) | 0 (0) | no |
| `signup_screen.dart` | 416 | yes | 0 (0) | no |
| `splash_screen.dart` | 117 | yes | 0 (0) | no |
| `update_gate_screen.dart` | 102 | yes | 0 (0) | no |
| `video_player_screen.dart` | 676 | yes | 0 (0) | no |
| **Total** | 4718 | 18 of 18 | 0 (0) | 4 of 18 |

## Widgets

The gate scans screens only, so widgets carry no baseline entries.

| File | Lines | Used by screens |
|------|------:|-----------------|
| `accent_title.dart` | 56 | subject content section, courses, home |
| `app_badge.dart` | 67 | subject content section, ebook |
| `app_bottom_nav.dart` | 108 | main shell |
| `app_shell.dart` | 116 | 13 screens |
| `brand_lockup.dart` | 44 | main shell |
| `catalog_file_tile.dart` | 118 | subject content section |
| `catalog_level_header.dart` | 76 | courses |
| `catalog_subject_card.dart` | 182 | courses |
| `catalog_video_tile.dart` | 84 | subject content section |
| `confirm_action_dialog.dart` | 103 | settings |
| `director_strip.dart` | 94 | course detail body |
| `file_details_sheet.dart` | 102 | subject content section |
| `framed_poster_card.dart` | 76 | login |
| `header_icon_button.dart` | 98 | main shell |
| `hero_backdrop.dart` | 85 | none |
| `home_hero_banner.dart` | 216 | home |
| `icon_tile.dart` | 41 | notifications, settings |
| `instructor_dossier_card.dart` | 155 | home |
| `language_toggle_chip.dart` | 71 | login |
| `moving_watermark.dart` | 109 | none |
| `otp_pin_input.dart` | 183 | forgot password, otp |
| `owned_subject_tile.dart` | 83 | course detail body, home |
| `password_rules.dart` | 68 | forgot password, signup |
| `pill_filter_bar.dart` | 88 | none |
| `player_controls.dart` | 153 | none |
| `primary_button.dart` | 89 | 5 screens |
| `profile_avatar.dart` | 52 | settings |
| `protected_video_surface.dart` | 214 | video player |
| `search_field.dart` | 75 | courses |
| `secondary_button.dart` | 90 | course detail body, home |
| `selectable_chip.dart` | 142 | subject content section, courses |
| `status_dot.dart` | 37 | notifications |
| `subject_hero_banner.dart` | 76 | course detail body |
| `themed_card.dart` | 55 | notifications, settings |
| `themed_empty_state.dart` | 64 | 5 screens |
| `themed_error_banner.dart` | 81 | 10 screens |
| `themed_loading_indicator.dart` | 49 | video player |
| `themed_panel.dart` | 53 | 7 screens |
| `themed_section_header.dart` | 48 | settings |
| `themed_skeleton.dart` | 96 | course detail, courses, home |
| `themed_text_field.dart` | 116 | forgot password, login, signup |

The F2 widgets are exercised by `test/widget_layer_test.dart` (English and Arabic) and
shown in the debug-only component library (`lib/debug/component_library_screen.dart`,
route `/components`, linked from the diagnostics screen). "Used by screens" is computed
from the imports in `lib/screens/`. Screens are migrated onto them one per commit (F3a).
