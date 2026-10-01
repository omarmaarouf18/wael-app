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
| `courses_screen.dart` | 755 | no | 6 (37) | yes |
| `ebook_screen.dart` | 954 | no | 7 (60) | yes |
| `forgot_password_screen.dart` | 165 | yes | 0 (0) | no |
| `home_screen.dart` | 1083 | no | 6 (38) | yes |
| `login_screen.dart` | 343 | yes | 0 (0) | no |
| `main_shell.dart` | 300 | no | 3 (10) | no |
| `notifications_screen.dart` | 226 | no | 6 (10) | no |
| `otp_screen.dart` | 111 | yes | 0 (0) | no |
| `payment_screen.dart` | 672 | no | 8 (36) | yes |
| `settings_screen.dart` | 660 | no | 5 (17) | no |
| `signup_screen.dart` | 349 | no | 5 (5) | no |
| `splash_screen.dart` | 61 | yes | 0 (0) | no |
| **Total** | 7718 | 4 of 13 | 54 (340) | 5 of 13 |

`main_shell.dart` hosts the bottom navigation, but it is a screen with its own
`Scaffold(`, not the shared template.

## Widgets

The gate scans screens only, so widgets carry no baseline entries.

| File | Lines | Used by screens |
|------|------:|-----------------|
| `app_shell.dart` | 109 | 4 screens |
| `confirm_action_dialog.dart` | 103 | none |
| `hero_backdrop.dart` | 85 | login |
| `language_toggle_chip.dart` | 58 | login |
| `otp_pin_input.dart` | 178 | forgot password, otp |
| `pill_filter_bar.dart` | 88 | ebook |
| `primary_button.dart` | 89 | 8 screens |
| `secondary_button.dart` | 90 | none |
| `status_badge.dart` | 76 | payment |
| `themed_card.dart` | 55 | 7 screens |
| `themed_empty_state.dart` | 64 | none |
| `themed_error_banner.dart` | 81 | forgot password, login, otp |
| `themed_loading_indicator.dart` | 49 | none |
| `themed_panel.dart` | 51 | otp |
| `themed_section_header.dart` | 48 | none |
| `themed_text_field.dart` | 104 | 6 screens |

The F2 widgets are exercised by `test/widget_layer_test.dart` (English and Arabic) and
shown in the debug-only component library (`lib/debug/component_library_screen.dart`,
route `/components`, linked from the diagnostics screen). "Used by screens" is computed
from the imports in `lib/screens/`. Screens are migrated onto them one per commit (F3a).
