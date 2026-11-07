# Website design system (`web/`)

The website's design system, derived from the app's
(`docs/frontend/DESIGN_SYSTEM.md`, `frontend/lib/core/theme.dart`) so the
site and the app read as one brand: EL METR Academy — near-black canvas,
crimson accent, the gentleman logo, Cairo for Arabic.

- **Tokens live in one file:** `web/src/app/tokens.css` (imported by
  `globals.css`). This document is the spec; the file wins on a conflict
  and both change in the same commit.
- **Brand look: dark only.** The app ships one theme (`AppTheme.darkTheme`),
  so the site offers no light mode. `color-scheme: dark` is set on `:root`.
- Added 2026-11-07 (UI/UX brief, step 1). Pages were switched to the tokens
  without a redesign; the redesign comes in steps 2–3.

## Rules

1. **Tokens only.** No hex, `rgb()` or `hsl()` outside `tokens.css`; no
   inline `style=` in any component or page (the CSP blocks server-rendered
   style attributes, `docs/web/README.md` "Security headers").
   `web/test/design-tokens.test.ts` (`npm test`) enforces both, and that
   every `var(--…)` used is defined.
2. **Logical properties only.** `margin-inline-start`, `padding-block-end`,
   `border-inline-start`, `text-align: start`… never `left`/`right`
   (same test). Arabic (RTL) is the default; English (LTR) is the switch;
   every layout is checked in both.
3. **Crimson is a fill, not a text colour.** `--crimson` is 3.03:1 on
   `--surface-layer-1`: use it for buttons, borders, rules, badges and
   display text ≥ 24 px. Red body text and small icons use `--danger`.
4. **Static stays static.** Nothing in this system needs JavaScript; motion
   is CSS. No external fonts, scripts or embeds (CSP unchanged).
5. **Never invent facts.** Missing copy or photos are a visible TODO block
   (`.todo` + `.badge` in `placeholder-page.module.css`), shown only in
   non-production builds once the landing page exists (step 2).

## Colour

Values are the app's `AppColors` unless marked **web** (no app twin).
Contrast is WCAG 2.x, computed on the composited colour (translucent tints
blended over the surface they sit on), 2026-11-07.

### Roles

| Role | Token | Value | Use |
|---|---|---|---|
| Background | `--void-canvas` | `#080808` | Page background |
| Surface | `--surface-layer-1` | `#111111` | Cards, drawer, footer |
| Surface raised | `--surface-elevated` | `#181818` | List rows, nested cards |
| Surface high | `--surface-high` | `#202020` | Hover, chips, store buttons |
| Input fill | `--surface-container-low` | `#0e0e0e` | Text fields (app `inputDecorationTheme`) |
| Text | `--text-primary` | `#ffffff` | Headings, values |
| Body | `--text-secondary` | `#b8b8b8` | Paragraphs |
| Muted | `--text-muted` | `#9a9a9a` | Meta, helper text |
| Tertiary | `--text-tertiary` | `#8a8a8a` | Captions |
| Placeholder | `--text-placeholder` | `#808080` | Field placeholders |
| Accent | `--crimson` | `#c1121f` | Primary fill, active border, rules |
| Accent hover | `--crimson-hover` | `#a30f1a` | Primary hover |
| Accent deep | `--deep-crimson` | `#7f0d15` | Gradients, pressed |
| On accent | `--text-on-accent` | `#ffffff` | Text on crimson |
| Accent wash | `--crimson-tinted` / `--crimson-faint` (**web**) | 12% / 6% crimson | Selected rows, hero glow |
| Success | `--success` (+ `-bg`, `-border`) | `#10b981` | Accepted, owned |
| Warning | `--warning` (+ `-bg`, `-border`) | `#f59e0b` | Pending, TODO badge |
| Error | `--danger` (+ `-bg`, `-border`) | `#f67e86` | Errors, rejected, red text |
| Destructive fill | `--danger-fill` (**web**) | = `--crimson` | Delete-account button |
| Info | `--info` (+ `-bg`, `-border`) | `#60a5fa` | "Web" device badge |
| Decorative border | `--subtle-hairline` / `--prominent-border` | `#262626` / `#303030` | Dividers, card edges (decorative only) |
| Input border | `--input-border` (**web**) | `#6b6b6b` | Field and control boundaries |
| Header | `--header-glass` | 95% `#090909` | Sticky header |
| Scrim | `--scrim` / `--scrim-strong` (**web**) | 60% / 70% black | Drawer / dialog backdrop |

Tints (`-bg`) are 15% of the foreground; borders (`-border`, **web**) 35%.

### Text contrast

| Foreground | canvas `#080808` | layer-1 `#111` | elevated `#181818` | high `#202020` |
|---|--:|--:|--:|--:|
| `--text-primary` | 20.03 | 18.88 | 17.76 | 16.29 |
| `--text-secondary` | 10.10 | 9.52 | 8.95 | 8.21 |
| `--text-muted` | 7.12 | 6.71 | 6.31 | 5.79 |
| `--text-tertiary` | 5.80 | 5.47 | 5.14 | 4.72 |
| `--text-placeholder` | 5.07 | 4.78 | 4.50 | 4.13 ✗ |
| `--crimson` | 3.22 | 3.03 | 2.85 ✗ | 2.62 ✗ |
| `--danger` | 7.87 | 7.42 | 6.97 | 6.40 |
| `--success` | 7.90 | 7.44 | 7.00 | 6.42 |
| `--warning` | 9.33 | 8.79 | 8.27 | 7.59 |
| `--info` | 7.88 | 7.43 | 6.98 | 6.41 |

Body text needs 4.5:1, large text (≥ 24 px, or ≥ 18.66 px bold) 3:1.
✗ = below 3:1 or, for placeholder on `--surface-high`, below 4.5:1: do not
use that pair. Crimson text passes only as large text on canvas and layer-1.

Semantic foreground on its own 15% tint, over each surface:

| | canvas | layer-1 | elevated | high |
|---|--:|--:|--:|--:|
| `--danger` on `--danger-bg` | 6.61 | 6.07 | 5.62 | 5.10 |
| `--success` on `--success-bg` | 6.53 | 6.01 | 5.56 | 5.04 |
| `--warning` on `--warning-bg` | 7.48 | 6.86 | 6.33 | 5.72 |
| `--info` on `--info-bg` | 6.48 | 5.93 | 5.48 | 4.96 |

Other pairs: white on `--crimson` 6.22, on `--crimson-hover` 7.95, on
`--deep-crimson` 10.66; `--text-secondary` on a 12% crimson wash over canvas
9.63. Non-text (3:1, WCAG 1.4.11): `--input-border` 3.62 on the input fill,
3.33 on elevated, 3.06 on high; `--prominent-border` is only 1.35 on
elevated, so it may outline decoration but never a control. The focus ring
(`--crimson`) is 3.22 on canvas and 2.62 on `--surface-high` — see
"Focus".

### Known gaps in today's pages (fixed in step 3, not in step 1)

- Crimson used as small text: links (`.link`), cart prices
  (`.itemPrice`, `.totalAmount`), the active account tab. Step 3 moves them
  to `--text-primary`/`--danger` or large sizes.
- Inputs use `--prominent-border` (1.35:1); step 3 switches controls to
  `--input-border`.

*(Amended 2026-11-07, step 3: both fixed. Links, prices and the active tab
are `--text-primary` (crimson stays only as the tab underline and fills);
every input, select and outlined button uses `--input-border`.)*

## Typography

The app's three families, self-hosted from `web/src/fonts/` (SIL OFL 1.1,
copied from `frontend/assets/fonts/`, `docs/asset-provenance.md`
"Website copies"), loaded with `next/font/local`. Nothing comes from Google.

| Script | Display (headings) | Body and UI |
|---|---|---|
| Arabic (`:lang(ar)`) | Cairo 700 / 800 | Cairo 400 / 700 |
| Latin (`:lang(en)`) | Syne 700 / 800 | Plus Jakarta Sans 400 / 600 |

Cairo also carries Latin and digits, so mixed Arabic text with numbers or
"EL METR" stays in one face. Step 2 converts the shipped TTFs to WOFF2 and
loads Cairo only where it is needed, for the LCP budget.
*(Amended 2026-11-07, step 4: done in step 2 (subset WOFF2). Each layout
now declares only its own families — Cairo on Arabic pages
(`src/lib/fonts-ar.ts`), Syne and Plus Jakarta Sans on English pages
(`src/lib/fonts-en.ts`) — and only Cairo is preloaded: `next/font` merges
preloads app-wide, and preloading the Latin faces too cost Arabic pages
0.2–0.45 s of LCP in Lighthouse (`docs/web/README.md` "Performance"). On
English pages the few Arabic words (language switch, Arabic eyebrow, an
Arabic-only level title) use the system Arabic font; on Arabic pages the
Latin bits ("EL METR ACADEMY", store names) use Cairo's Latin.)*

### Scale (roles, fluid 375 → 1440 px)

| Role | Token | Latin | Arabic | Weight | Line height en / ar |
|---|---|---|---|---|---|
| Display (hero h1) | `--fs-display` | 32 → 52 | 34 → 52 | 800 | 1.15 / 1.3 (`--lh-display`) |
| Page title (h1) | `--fs-h1` | 28 → 38 | 28 → 36 | 800 | 1.2 / 1.35 (`--lh-heading`) |
| Section (h2) | `--fs-h2` | 24 → 32 | 24 → 32 | 800 | 1.2 / 1.35 |
| Card title (h3) | `--fs-h3` | 18 → 20 | 18 → 20 | 700 | 1.2 / 1.35 |
| Lead | `--fs-lead` | 17 → 19 | 17 → 19 | 400 | 1.55 / 1.75 (`--lh-body`) |
| Body | `--fs-body` | 16 | 16 | 400 | 1.55 / 1.75 |
| Small | `--fs-small` | 14 | 14 | 400 | 1.5 / 1.7 |
| Caption, label | `--fs-caption` | 13 | 13 | 600 | 1.4 |
| Eyebrow | `--fs-eyebrow` | 12, `0.2em` tracking, uppercase | 12, no tracking | 700 | 1.4 |

Arabic sits one step smaller at display sizes and gets taller lines, as in
the app (`AppTypography` Arabic metrics); Arabic never gets letter spacing
or `text-transform`. Body text is never below 14 px on the web (the app goes
to 11–13 px on phones; a website at arm's length should not).
Fixed steps (`--fs-2xs` 10 … `--fs-6xl` 48, `--lh-tight` 1.2 … `--lh-relaxed`
1.6) exist for the pages switched over in step 1 and for fine tuning.

## Spacing

The app's `AppSpacing` (4-point) plus three section steps (**web**):

| Token | px | | Token | px |
|---|--:|---|---|--:|
| `--space-2xs` | 2 | | `--space-2xl` | 32 |
| `--space-xs` | 4 | | `--space-3xl` | 40 |
| `--space-sm` | 8 | | `--space-4xl` | 48 |
| `--space-md` | 12 | | `--space-5xl` | 64 (section, mobile) |
| `--space-lg` | 16 | | `--space-6xl` | 96 (section, desktop) |
| `--space-xl` | 24 | | `--margin-mobile` | 16 (page gutter) |

Layout widths: content `1080px` (`--content-width`), forms 520 / 480,
account 720, drawer 480, dialog 500.

## Breakpoints

Mobile first; media queries use literal values (custom properties cannot
appear in `@media`):

| Name | Min width | Layout |
|---|---|---|
| base | — (designed at 375) | One column, 16 px gutters, header wraps to two rows |
| tablet | `768px` | Two-column grids, header in one row |
| desktop | `1440px` design width (layouts switch at `1024px`) | Three-column grids, content capped at 1080 px, centred |

## Radius

`--radius-xs` 2 · `--radius-sm` 4 · `--radius-md` 6 · `--radius-lg` 8 ·
`--radius-xl` / `--radius-card` 12 · `--radius-pill` 9999.
Buttons and inputs use 12 (the app's `PrimaryButton` and text field);
badges and filter chips use the pill; cards 12.

## Elevation

| Token | Shadow | Use |
|---|---|---|
| `--elevation-card` | `0 4px 12px` black 50% | Cards on canvas |
| `--elevation-crimson-glow` | `0 4px 20px` crimson 25% | Primary button, featured card |
| `--elevation-modal` (**web**) | `0 10px 25px` black 50% | Drawer, dialog |

On a near-black canvas shadows barely read; depth comes from surface steps
and hairlines first, shadows second.

## Components

Each is a CSS class set in a module today; step 3 gathers them into shared
components. *(Amended 2026-11-07, step 3: kept as module classes instead —
`auth.module.css` (forms, account, buy section), `cart.module.css` (drawer,
checkout), `site-shell.module.css` (header, footer), `catalog-pages.module.css`
and `home-page.module.css` — restyled to the specs below with the views'
class names unchanged, so no view changed behaviour.)* Every interactive target is at least 44 × 44 px
(`--tap-target`).

- **Primary button.** `--crimson` fill, `--text-on-accent`, 600 weight,
  height 52 (`--control-height`), radius 12, `--elevation-crimson-glow`;
  hover `--crimson-hover`; disabled 60% opacity, no glow; loading keeps its
  width and shows the text "…" state from i18n. One per view.
- **Secondary button.** Transparent, 1 px `--input-border`,
  `--text-primary`; hover `--surface-high`. Same height and radius.
- **Danger button.** `--danger-fill`; only inside the delete-account flow,
  always after a warning box.
- **Text link.** `--text-primary`, underlined, offset 3 px; hover
  `--text-secondary`. Not crimson (rule 3).
- **Input / select.** Fill `--surface-container-low`, 1 px
  `--input-border`, radius 12, height 52, 16 px text (no iOS zoom);
  focus: border `--crimson` + the focus ring; error: border `--danger` and
  a message under the field linked with `aria-describedby`. A visible
  `<label>` on every field; placeholder never replaces it. Phone, e-mail,
  reference and OTP fields are `dir="ltr"` in both languages.
- **Card.** `--surface-layer-1`, 1 px `--subtle-hairline`, radius 12,
  padding 24 (16 on mobile). Interactive cards: hover border
  `--crimson-tinted`, the whole card is the link.
- **Subject card.** Term badge, title (h3), price (`--text-primary`, bold,
  Latin digits as the API sends them), "view subject"; mirrors the app's
  `CatalogSubjectCard`.
- **Badge.** Pill, 12 px, 700, tint + border + foreground of one semantic
  colour (`-bg`, `-border`, fg). Status is never colour only: the word is
  always in the badge.
- **Eyebrow.** The app's brand eyebrow: 12 px, muted, a 24 px crimson rule
  before it (`--crimson`, 2 px). Latin uppercase with tracking; Arabic plain.
- **Header.** Sticky, `--header-glass`, bottom `--glass-hairline`, 56 px.
  Start edge: logo (36 px, radius 8) + wordmark "أكاديمية المتر" with the
  "EL METR ACADEMY" eyebrow. End edge: Subjects, About, cart (count badge),
  account state (Log in / Sign up, or My account / Log out), language
  switch (a pill naming the other language, `hreflang`). Under 768 px the
  links fold into a menu button (step 3). *(Amended 2026-11-07, step 3:
  under 1024 px, not 768 — at 768 the English links wrapped. The menu is a
  `<details>` disclosure (`header-menu.tsx`) that closes on navigation and
  Escape; under 480 px the cart and menu buttons show their icon only, the
  word kept for screen readers.)*
- **Footer.** `--surface-layer-1`, top hairline. Brand line, store badges,
  legal links (terms, privacy, refund), language switch, rights line.
  *(Amended 2026-11-07: store links are text buttons until the owner sends
  the official badges.)*
- **Banner / alert.** Tint background, 35% border, semantic foreground,
  radius 6; errors are `role="alert"`, success `role="status"`; never
  auto-dismiss.
- **Empty state, skeleton.** Centred muted text with one action; skeletons
  are static `--surface-elevated` blocks (no shimmer under reduced motion).

## Focus

Every interactive element shows `:focus-visible`: 2 px
`--focus-ring-color` outline, 2 px offset. Today that colour is
`--crimson` (3.22:1 on canvas, passes). On `--surface-high` crimson is only
2.62:1, so step 3 sets `--focus-ring-color` to `--text-primary` (16:1+ on
every surface) with the crimson border kept as a second cue on inputs.
A skip link ("تخطَّ إلى المحتوى" / "Skip to content") is the first focusable
element on every page (step 3).
*(Amended 2026-11-07: done — `--focus-ring-color` is `--text-primary`; the
skip link shipped in step 2. Network failures show an error with "Try again"
(account hub, checkout payment methods); loading shows static skeletons
(account hub, buy section).)*

## Motion

| Token | Value | App twin |
|---|---|---|
| `--duration-fast` | 150 ms | `durationFast` — hover, colour |
| `--duration-normal` | 250 ms | `durationNormal` — drawer, dialog |
| `--duration-slow` | 400 ms | `durationSlow` — section reveal (max) |
| `--ease-standard` | `cubic-bezier(0.42, 0, 0.58, 1)` | `Curves.easeInOut` |
| `--ease-emphasized` | `cubic-bezier(0.4, 0, 0.2, 1)` | `Curves.fastOutSlowIn` |
| `--ease-out` (**web**) | `cubic-bezier(0, 0, 0.2, 1)` | things entering |

CSS only; animate `opacity` and `transform` only. Content is never hidden
waiting for an animation (a reveal starts from visible-but-offset or is
skipped). No parallax, no scroll-jacking, no autoplay video.
`prefers-reduced-motion: reduce` turns every transition and animation off
(`globals.css`). The drawer's slide direction follows `dir`.

## Imagery

- Brand art from the repo only (`docs/asset-provenance.md`); every new photo
  is owner-supplied with a consent note before it is committed.
- Shipped as pre-sized AVIF + WebP (JPEG fallback) with explicit `width` and
  `height`; hero preloaded, everything else `loading="lazy"`. Plain `<img>`
  / `<picture>` (no `next/image`: its inline style breaks the CSP, and
  Vercel image optimisation is not used).
- Photos sit on the canvas with a dark gradient toward the text side,
  like the app's `HeroBackdrop`; text never sits on an unshaded photo.
- Every image has alt text; decorative ones `alt=""`.
