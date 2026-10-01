# Frontend Status

Per-file tracker for `frontend/lib/screens/` and `frontend/lib/widgets/`. Generated from
the tree and `scripts/frontend_gate_baseline.txt` at the commit that added this file;
refresh the numbers when the baseline is lowered. Token and widget reference:
`docs/frontend/DESIGN_SYSTEM.md`.

- **Uses shared shell**: the screen builds on `AppShell`. Screens are migrated one per commit
  (F3a); until a screen is migrated it declares its own `Scaffold(`.
- **Baseline entries**: file/rule pairs in the gate baseline (violation count in
  parentheses). The baseline only goes down.
- **Catalog screen**: course catalog and payment screens. They move with SPEC Phase 2-3,
  so avoid polishing them before the academy API lands.

## Screens

| File | Lines | Uses shared shell | Baseline entries (violations) | Catalog screen |
|------|------:|-------------------|------------------------------:|----------------|
| `course_detail_screen.dart` | 2039 | no | 8 (127) | yes |
| `courses_screen.dart` | 291 | yes | 0 (0) | yes |
| `ebook_screen.dart` | 954 | no | 7 (60) | yes |
| `forgot_password_screen.dart` | 165 | yes | 0 (0) | no |
| `home_screen.dart` | 198 | yes | 0 (0) | yes |
| `login_screen.dart` | 343 | yes | 0 (0) | no |
| `main_shell.dart` | 103 | yes | 0 (0) | no |
| `notifications_screen.dart` | 189 | yes | 0 (0) | no |
| `otp_screen.dart` | 111 | yes | 0 (0) | no |
| `payment_screen.dart` | 672 | no | 8 (36) | yes |
| `settings_screen.dart` | 550 | yes | 0 (0) | no |
| `signup_screen.dart` | 332 | yes | 0 (0) | no |
| `splash_screen.dart` | 61 | yes | 0 (0) | no |
| **Total** | 6008 | 10 of 13 | 23 (223) | 5 of 13 |

`main_shell.dart` hosts the bottom navigation, but it is a screen with its own
`Scaffold(`, not the shared template.

## Widgets

The gate scans screens only, so widgets carry no baseline entries.

| File | Lines | Used by screens |
|------|------:|-----------------|
| `accent_title.dart` | 56 | courses, home |
| `app_badge.dart` | 67 | none |
| `app_bottom_nav.dart` | 108 | main shell |
| `app_shell.dart` | 116 | 10 screens |
| `brand_lockup.dart` | 44 | main shell |
| `catalog_level_header.dart` | 76 | courses |
| `catalog_subject_card.dart` | 179 | courses |
| `confirm_action_dialog.dart` | 103 | none |
| `header_icon_button.dart` | 52 | main shell |
| `hero_backdrop.dart` | 85 | login |
| `home_hero_banner.dart` | 207 | home |
| `icon_tile.dart` | 41 | notifications, settings |
| `instructor_dossier_card.dart` | 163 | home |
| `language_toggle_chip.dart` | 58 | login |
| `otp_pin_input.dart` | 178 | forgot password, otp |
| `owned_subject_tile.dart` | 83 | home |
| `pill_filter_bar.dart` | 88 | ebook |
| `primary_button.dart` | 89 | 8 screens |
| `profile_avatar.dart` | 42 | settings |
| `search_field.dart` | 75 | courses |
| `secondary_button.dart` | 90 | home |
| `selectable_chip.dart` | 125 | courses |
| `status_badge.dart` | 76 | payment |
| `status_dot.dart` | 37 | notifications, settings |
| `themed_card.dart` | 55 | 5 screens |
| `themed_empty_state.dart` | 64 | courses, home, notifications |
| `themed_error_banner.dart` | 81 | 7 screens |
| `themed_loading_indicator.dart` | 49 | courses, home |
| `themed_panel.dart` | 53 | courses, home, otp |
| `themed_section_header.dart` | 48 | settings |
| `themed_text_field.dart` | 104 | 6 screens |

The F2 widgets are exercised by `test/widget_layer_test.dart` (English and Arabic) and
shown in the debug-only component library (`lib/debug/component_library_screen.dart`,
route `/components`, linked from the diagnostics screen). "Used by screens" is computed
from the imports in `lib/screens/`. Screens are migrated onto them one per commit (F3a).
