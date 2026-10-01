# Frontend Status

Per-file tracker for `frontend/lib/screens/` and `frontend/lib/widgets/`. Generated from
the tree and `scripts/frontend_gate_baseline.txt` at the commit that added this file;
refresh the numbers when the baseline is lowered. Token and widget reference:
`docs/frontend/DESIGN_SYSTEM.md`.

- **Uses shared shell**: the screen builds on `DashboardScreenTemplate`. No screen does yet;
  every screen declares its own `Scaffold(`.
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
| `forgot_password_screen.dart` | 163 | no | 4 (6) | no |
| `home_screen.dart` | 1083 | no | 6 (38) | yes |
| `login_screen.dart` | 449 | no | 8 (16) | no |
| `main_shell.dart` | 304 | no | 6 (18) | no |
| `notifications_screen.dart` | 226 | no | 8 (13) | no |
| `otp_screen.dart` | 115 | no | 6 (6) | no |
| `payment_screen.dart` | 672 | no | 8 (36) | yes |
| `settings_screen.dart` | 653 | no | 8 (27) | no |
| `signup_screen.dart` | 349 | no | 7 (7) | no |
| `splash_screen.dart` | 59 | no | 1 (1) | no |
| **Total** | 7821 | 0 of 13 | 83 (392) | 5 of 13 |

`main_shell.dart` hosts the bottom navigation, but it is a screen with its own
`Scaffold(`, not the shared template.

## Widgets

The gate scans screens only, so widgets carry no baseline entries.

| File | Lines | Used by screens |
|------|------:|-----------------|
| `dashboard_screen_template.dart` | 293 | none |
| `pill_filter_bar.dart` | 88 | ebook |
| `primary_button.dart` | 99 | 8 screens |
| `status_badge.dart` | 76 | payment |
| `themed_card.dart` | 55 | 7 screens |
| `themed_text_field.dart` | 104 | 7 screens |
