# Frontend Status

Per-file tracker for `frontend/lib/screens/` and `frontend/lib/widgets/`. Generated from
the tree and `scripts/frontend_gate_baseline.txt` at the commit that added this file;
refresh the numbers when the baseline is lowered. *(Refreshed 2026-10-06 from the tree of `origin/develop` at `79e1962`: line counts, the widget list and the "used by" column are recomputed from the files and from the imports under `lib/screens/`; the baseline is empty, so every entry is 0.)* *(Refreshed 2026-10-11 from the tree of `origin/develop` at `b0de4d4`: line counts recomputed for the files that changed since (notes & books client, settings, signup, player fixes), the widget list gains `file_download_tile.dart` and `player_next_cards.dart`, and the "used by" column is recomputed from the imports under `lib/screens/`; the baseline is still empty, so every entry is 0.)* Token and widget reference:
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
| `course_detail_screen.dart` | 127 | yes | 0 (0) | yes |
| `courses_screen.dart` | 319 | yes | 0 (0) | yes |
| `ebook_screen.dart` | 278 | yes | 0 (0) | yes |
| `forgot_password_screen.dart` | 251 | yes | 0 (0) | no |
| `home_screen.dart` | 200 | yes | 0 (0) | yes |
| `login_screen.dart` | 330 | yes | 0 (0) | no |
| `main_shell.dart` | 256 | yes | 0 (0) | no |
| `notifications_screen.dart` | 265 | yes | 0 (0) | no |
| `otp_screen.dart` | 168 | yes | 0 (0) | no |
| `settings_screen.dart` | 475 | yes | 0 (0) | no |
| `settings/delete_account_screen.dart` | 277 | yes | 0 (0) | no |
| `settings/devices_screen.dart` | 255 | yes | 0 (0) | no |
| `settings/email_change_screen.dart` | 156 | yes | 0 (0) | no |
| `settings/password_change_screen.dart` | 124 | yes | 0 (0) | no |
| `settings/profile_section.dart` | 338 | n/a (section) | 0 (0) | no |
| `signup_screen.dart` | 452 | yes | 0 (0) | no |
| `splash_screen.dart` | 117 | yes | 0 (0) | no |
| `update_gate_screen.dart` | 102 | yes | 0 (0) | no |
| `video_player_screen.dart` | 676 | yes | 0 (0) | no |
| **Total** | 5166 | 18 of 18 | 0 (0) | 4 of 18 |

## Widgets

The gate scans screens only, so widgets carry no baseline entries.

| File | Lines | Used by screens |
|------|------:|-----------------|
| `accent_title.dart` | 56 | subject content section, courses, home |
| `app_badge.dart` | 68 | subject content section, ebook, devices |
| `app_bottom_nav.dart` | 119 | main shell |
| `app_shell.dart` | 116 | 18 screens |
| `brand_lockup.dart` | 44 | main shell |
| `catalog_file_tile.dart` | 119 | subject content section |
| `catalog_level_header.dart` | 77 | courses |
| `catalog_subject_card.dart` | 207 | courses |
| `catalog_video_tile.dart` | 85 | subject content section |
| `confirm_action_dialog.dart` | 103 | settings, devices |
| `director_strip.dart` | 96 | course detail body |
| `file_details_sheet.dart` | 102 | subject content section |
| `file_download_tile.dart` | 253 | subject content section, ebook |
| `framed_poster_card.dart` | 76 | login |
| `header_icon_button.dart` | 98 | main shell |
| `hero_backdrop.dart` | 85 | none |
| `home_hero_banner.dart` | 216 | home |
| `icon_tile.dart` | 41 | notifications, settings, devices, profile section, update gate |
| `instructor_dossier_card.dart` | 158 | home |
| `language_toggle_chip.dart` | 71 | login |
| `legal_summary_sheet.dart` | 161 | signup |
| `moving_watermark.dart` | 109 | none |
| `otp_pin_input.dart` | 183 | forgot password, otp, email change |
| `owned_subject_tile.dart` | 84 | course detail body, home |
| `password_rules.dart` | 68 | forgot password, signup, password change |
| `pill_filter_bar.dart` | 88 | none |
| `player_controls.dart` | 211 | none |
| `player_next_cards.dart` | 168 | video player |
| `primary_button.dart` | 89 | 8 screens |
| `profile_avatar.dart` | 52 | settings |
| `protected_video_surface.dart` | 245 | video player |
| `search_field.dart` | 75 | courses |
| `secondary_button.dart` | 90 | course detail body, home, delete account, devices |
| `selectable_chip.dart` | 143 | subject content section, courses |
| `status_dot.dart` | 38 | notifications |
| `subject_price.dart` | 73 | course detail body, courses (via card) |
| `subject_hero_banner.dart` | 76 | course detail body |
| `themed_card.dart` | 55 | notifications, settings, devices, profile section, update gate |
| `themed_empty_state.dart` | 64 | 6 screens |
| `themed_error_banner.dart` | 105 | 16 screens |
| `themed_loading_indicator.dart` | 49 | video player, devices |
| `themed_panel.dart` | 53 | 7 screens |
| `themed_section_header.dart` | 48 | ebook, settings, profile section |
| `themed_skeleton.dart` | 96 | course detail, courses, home, ebook |
| `themed_text_field.dart` | 120 | forgot password, login, signup, delete account, email change, password change, profile section |

The F2 widgets are exercised by `test/widget_layer_test.dart` (English and Arabic) and
shown in the debug-only component library (`lib/debug/component_library_screen.dart`,
route `/components`, linked from the diagnostics screen). "Used by screens" is computed
from the imports in `lib/screens/`. Screens are migrated onto them one per commit (F3a).
