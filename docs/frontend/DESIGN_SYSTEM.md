# Frontend Design System

Token reference for `frontend/lib/core/theme.dart` and the shared widget layer in
`frontend/lib/widgets/`. **`theme.dart` wins on any conflict with this file.** Values
below were read from `theme.dart`; if you change a token, update this file in the same
commit.

Enforcement: `scripts/frontend_composition_gate.sh` (see `docs/frontend/STATUS.md` for
the per-file baseline).

## Rules

1. **No raw values in screens.** No `Color(0x...)`, `Colors.x` (except
   `Colors.transparent`), `TextStyle(`, `fontSize:`, `BoxDecoration(`, `Scaffold(`,
   `AppBar(` or `.toUpperCase()` in `lib/screens/`. Use the tokens below and the
   widgets in `lib/widgets/`. Uppercase labels go through
   `AppTypography.uppercaseLabel(text)`, the only sanctioned uppercase.
2. **Widgets first.** If a screen needs a pattern twice, add or extend a widget in
   `lib/widgets/` instead of copying markup between screens.
3. **Directional insets.** The app is Arabic-first and RTL. Use
   `EdgeInsetsDirectional` / `AlignmentDirectional` / `BorderRadiusDirectional`, not
   `EdgeInsets.only` / `EdgeInsets.fromLTRB`. `EdgeInsets.all` and `.symmetric` are
   direction-safe.
4. **Typography takes `isArabic`.** Every `AppTypography` style switches font family
   and metrics when `isArabic: true`; always pass the current locale.
5. **Baseline only goes down.** Never raise a count in
   `scripts/frontend_gate_baseline.txt`.

## AppColors

### Surfaces

| Token | Value |
|-------|-------|
| `voidCanvas` | `0xFF080808` |
| `surfaceLayer1` | `0xFF111111` |
| `surfaceElevated` | `0xFF181818` |
| `surfaceHigh` | `0xFF202020` |
| `surfaceBright` | `0xFF2A2A2A` |
| `surfaceContainerLowest` | `0xFF040404` |
| `surfaceContainerLow` | `0xFF0E0E0E` |
| `surfaceContainer` | `0xFF141414` |
| `surfaceContainerHighest` | `0xFF353534` |

### Borders and hairlines

| Token | Value |
|-------|-------|
| `subtleHairline` | `0xFF262626` |
| `prominentBorder` | `0xFF303030` |
| `activeBorder` | `0xFFC1121F` |

### Accents

| Token | Value |
|-------|-------|
| `crimson` | `0xFFC1121F` |
| `deepCrimson` | `0xFF7F0D15` |
| `crimsonHover` | `0xFFA30F1A` |
| `crimsonTinted` | `0x1FC1121F` |
| `crimsonGlow` | `0x38C1121F` |

### Text

| Token | Value |
|-------|-------|
| `textPrimary` | `0xFFFFFFFF` |
| `textSecondary` | `0xFFB8B8B8` |
| `textMuted` | `0xFF9A9A9A` |
| `textTertiary` | `0xFF666666` |
| `textPlaceholder` | `0xFF4A4A4A` |

### Status

| Token | Value |
|-------|-------|
| `statusPending` | `0xFFF59E0B` |
| `statusPendingBg` | `0x26F59E0B` |
| `statusApproved` | `0xFF10B981` |
| `statusApprovedBg` | `0x2610B981` |
| `statusRejected` | `0xFFC1121F` |
| `statusRejectedBg` | `0x26C1121F` |
| `starRating` | `0xFFF59E0B` |

### Semantic (feedback)

For snackbars, inline messages and banners. Each foreground is at least 4.5:1 (WCAG AA)
on `voidCanvas`, `surfaceLayer1`, `surfaceElevated` and `surfaceHigh`, and on its own
`*Bg` variant composited over those surfaces; `test/theme_tokens_test.dart` enforces this.

| Token | Value | Notes |
|-------|-------|-------|
| `success` | `0xFF10B981` | Alias of `statusApproved` |
| `successBg` | `0x2610B981` | Alias of `statusApprovedBg` |
| `warning` | `0xFFF59E0B` | Alias of `statusPending` |
| `warningBg` | `0x26F59E0B` | Alias of `statusPendingBg` |
| `danger` | `0xFFF4707A` | Lighter than `crimson` on purpose |
| `dangerBg` | `0x26F4707A` | |
| `info` | `0xFF60A5FA` | |
| `infoBg` | `0x2660A5FA` | |

Contrast ratios (foreground / foreground on its `*Bg` over the surface):

| Token | `voidCanvas` | `surfaceLayer1` | `surfaceElevated` | `surfaceHigh` |
|-------|-------------:|----------------:|------------------:|--------------:|
| `success` | 7.90 / 6.59 | 7.44 / 6.01 | 7.00 / 5.56 | 6.42 / 5.04 |
| `warning` | 9.33 / 7.57 | 8.79 / 6.86 | 8.27 / 6.33 | 7.59 / 5.72 |
| `danger` | 7.10 / 6.01 | 6.69 / 5.48 | 6.30 / 5.07 | 5.78 / 4.61 |
| `info` | 7.88 / 6.53 | 7.43 / 5.93 | 6.98 / 5.48 | 6.41 / 4.96 |

`crimson` (and so `statusRejected`) is only 3.03:1 on `surfaceLayer1`. Use it for brand
fills, borders and large display text; use `danger` for red body text and small icons.

### Glass chrome and overlays

| Token | Value | Use |
|-------|-------|-----|
| `headerGlass` | `0xF2090909` | Translucent header background (also the theme AppBar) |
| `navBarGlass` | `0xF20B0B0B` | Translucent bottom bar background |
| `glassHairline` | `0x14FFFFFF` | 1px hairline on glass chrome and rings on dark surfaces |
| `scrimBlack` | `0xFF000000` | Scrims, text shadows, vignette gradients (apply alpha as needed) |

## AppSpacing

| Token | Value |
|-------|-------|
| `space2xs` | 2.0 |
| `spaceXs` | 4.0 |
| `spaceSm` | 8.0 |
| `spaceMd` | 12.0 |
| `spaceLg` | 16.0 |
| `spaceXl` | 24.0 |
| `space2xl` | 32.0 |
| `space3xl` | 40.0 |
| `gutterMobile` | 12.0 |
| `marginMobile` | 16.0 |
| `navBarHeight` | 68.0 |
| `headerHeight` | 56.0 |

## AppRadius

| Token | Value |
|-------|-------|
| `xs` | 2.0 |
| `sm` | 4.0 |
| `md` | 6.0 |
| `lg` | 8.0 |
| `xl` | 12.0 |
| `card` | 12.0 |
| `pill` | 9999.0 |

Each size also has a ready-made `BorderRadius`: `radiusXs`, `radiusSm`, `radiusMd`,
`radiusLg`, `radiusXl`, `radiusPill`.

## AppElevation

| Token | Shadow |
|-------|--------|
| `none` | no shadow |
| `card` | `0x80000000`, offset (0, 4), blur 12 |
| `crimsonGlow` | `0x40C1121F`, offset (0, 4), blur 20 |
| `bottomNav` | `0xCC000000`, offset (0, -4), blur 24 |
| `bottomNavSoft` | `0x99000000`, offset (0, -4), blur 24 (main shell bar) |

## AppMotion

| Token | Value |
|-------|-------|
| `durationFast` | 150 ms |
| `durationNormal` | 250 ms |
| `durationSlow` | 400 ms |
| `curveStandard` | `Curves.easeInOut` |
| `curveEmphasized` | `Curves.fastOutSlowIn` |

## AppIconSize

| Token | Value |
|-------|-------|
| `xs` | 14.0 |
| `sm` | 16.0 |
| `md` | 20.0 |
| `lg` | 24.0 |
| `xl` | 32.0 |

## AppTypography

Static methods taking `{bool isArabic = false}`. Latin uses Syne (display and
headings) and Plus Jakarta Sans (body and labels); Arabic uses Cairo throughout.
"LS" is letter spacing; "-" means the style does not set it.

| Style | Latin: font, size, weight, height, LS | Arabic (Cairo): size, weight, height, LS | Colour |
|-------|---------------------------------------|------------------------------------------|--------|
| `displayHero` | Syne, 36, w800, 1.15, -0.5 | 34, w800, 1.2, - | `textPrimary` |
| `headlineLg` | Syne, 26, w700, 1.2, -0.2 | 24, w800, 1.25, - | `textPrimary` |
| `headlineMd` | Syne, 22, w600, 1.25, -0.2 | 20, w700, 1.3, - | `textPrimary` |
| `headlineSm` | Syne, 17, w600, 1.3, - | 16, w700, 1.35, - | `textPrimary` |
| `academyEyebrow` | Syne, 10, w700, -, 3.2 | 10, w700, -, 1.5 | `textMuted` |
| `bodyLg` | Plus Jakarta Sans, 16, w400, 1.45, - | 15, w400, 1.5, - | `textSecondary` |
| `bodyMd` | Plus Jakarta Sans, 14, w400, 1.4, - | 13, w400, 1.45, - | `textSecondary` |
| `bodySm` | Plus Jakarta Sans, 12, w400, 1.35, - | 11, w400, 1.4, - | `textMuted` |
| `labelMd` | Plus Jakarta Sans, 13, w600, -, 0.8 | 12, w600, -, 0.5 | `textPrimary` |
| `labelSm` | Plus Jakarta Sans, 10, w700, -, 1.5 | 10, w700, -, 1.0 | `textSecondary` |

`AppTypography.uppercaseLabel(String text)` is not a style: it returns `text` upper-cased
(Arabic has no letter case and passes through unchanged). Screens use it instead of
`.toUpperCase()`.

## AppTheme.darkTheme

Brightness dark. `scaffoldBackgroundColor` and `canvasColor` are `voidCanvas`;
`primaryColor` is `crimson`. `ColorScheme.dark`: primary `crimson`, surface
`voidCanvas`, error `crimson`, `onPrimary`/`onSurface`/`onError` `textPrimary`.
AppBar, divider, card and input decoration themes are set in `theme.dart`; read the
file for their exact values.

## Shared widgets (`frontend/lib/widgets/`)

| Widget | File | Purpose |
|--------|------|---------|
| `DashboardScreenTemplate` | `dashboard_screen_template.dart` | Screen shell: optional header (title, back button, custom actions), body, loading / error (with retry) / empty states, optional floating action button. |
| `PillFilterBar` (+ `FilterPillItem`) | `pill_filter_bar.dart` | Horizontal pill filter with optional per-item count. |
| `PrimaryButton` | `primary_button.dart` | Primary and secondary button with loading state and leading/trailing icon. |
| `StatusBadge` | `status_badge.dart` | Badge for a `PaymentStatus` (pending / approved / rejected) using the status colour tokens. |
| `ThemedCard` | `themed_card.dart` | Bordered card surface with optional tap and glow. |
| `ThemedTextField` | `themed_text_field.dart` | Themed form field with label, hint, validator and prefix/suffix. |

## Known gaps

- The gate's failure message names `AppShell` and `ThemedPanel`, which do not exist yet.
  Today the shell is `DashboardScreenTemplate` and the card is `ThemedCard`.
- `DashboardScreenTemplate` is not used by any screen yet (see `STATUS.md`).
- `theme.dart` itself still has a raw value that is not a named token: the input hint
  style size (`fontSize: 14`).
- Raw colours and `.toUpperCase()` are gone from `lib/widgets/` and the non-catalog
  screens (splash, OTP, forgot password, login, signup, notifications, settings, main
  shell). The catalog screens (home, courses, course detail, ebook, payment) still carry
  them and move with SPEC Phase 2-3.
- The gate matches text, so `Color.fromARGB(`, `Color.fromRGBO(` and a pattern split
  across lines (`Scaffold\n(`) are not detected, and a pattern inside a comment is.
