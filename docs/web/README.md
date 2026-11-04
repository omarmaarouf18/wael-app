# Website (`web/`)

El Metr Academy's sales website. Decision record: ADR-0014
(`docs/adr/0014-sales-website.md`); SPEC Section 16. State: **foundations**
(2026-11-04) — scaffold, placeholder pages, CI, security headers and deploy
notes. No page calls the API yet; no payment code exists.

## Purpose

A site for selling subjects, not a web version of the app. A student sees
the subjects and prices, signs up or logs in with the same account as the
app, sends a manual transfer (owner's wallets or InstaPay), and submits a
purchase request with the transfer details. An admin checks it and activates
the subject in the console; the student then studies in the mobile app. The
website never plays videos or opens files.

The apps never mention or link to the website (SPEC Section 2 D3 amendment
2026-11-04). `frontend/test/store_safety_l10n_test.dart` ("Website address
guard") fails if `frontend/` contains the website's address or a link to it.
Links from the website to the store listings are fine (W5).

## Pages

Arabic is the default (right to left) at the root paths; English is under
`/en` and every page has a language switch.

| Arabic | English | Page | State |
|---|---|---|---|
| `/` | `/en` | Home | Placeholder (W5) |
| `/about` | `/en/about` | About the academy | Placeholder (W5) |
| `/subjects` | `/en/subjects` | Subjects catalog with prices | Placeholder, no prices (W2) |
| `/subjects/[id]` | `/en/subjects/[id]` | Subject page, buy | Placeholder (W2, W4) |
| `/login` | `/en/login` | Log in (same account as the app) | Placeholder (W3) |
| `/signup` | `/en/signup` | Sign up | Placeholder (W3) |
| `/terms` | `/en/terms` | Terms | Placeholder (owner text) |
| `/privacy` | `/en/privacy` | Privacy | Placeholder (owner text) |
| `/refund` | `/en/refund` | Refund policy | Placeholder (owner text) |

Every placeholder shows a visible "TODO" note. No price, subject or policy
text is made up. All pages send `noindex, nofollow` until W5 puts real pages
up.

## Stack

- Next.js 16 (App Router), React 19, TypeScript (strict, plus
  `noUncheckedIndexedAccess`), ESLint (`eslint-config-next`, zero warnings).
- Exact versions, each published at least 14 days before it was added
  (`npm install --before=<date>`); `package-lock.json` is committed and only
  `npm ci` installs. `web/.npmrc` sets `ignore-scripts=true` (no dependency
  install scripts anywhere) and `save-exact=true`.
- Node 24 (`engines.node` = `24.x`; Vercel follows it).
- Fonts: Cairo (Arabic), Syne and Plus Jakarta Sans (Latin) — the app's
  families (`docs/frontend/DESIGN_SYSTEM.md` "AppTypography"), copied from
  `frontend/assets/fonts/` with their OFL licences and served by the site
  (`next/font/local`). Nothing is loaded from Google.
- Colors, spacing, radius, elevation and motion are CSS variables named after
  the app's tokens (`web/src/app/globals.css`).
- Logo and art: copies of repo assets listed in `docs/asset-provenance.md`
  ("Website copies").

## Folder layout

```text
web/
  package.json, package-lock.json   pinned dependencies (npm ci only)
  .npmrc                            ignore-scripts, save-exact
  vercel.json                       Vercel install (npm ci) and build commands
  next.config.ts                    static security headers; validates the API URL at build
  eslint.config.mjs, tsconfig.json
  .env.example                      every variable, commented
  public/brand/                     logo-512.png, banner.jpg (repo asset copies)
  src/proxy.ts                      per-request CSP nonce (Next.js "proxy", formerly middleware)
  src/lib/config.ts                 NEXT_PUBLIC_API_BASE_URL parsing (fail closed)
  src/lib/csp.ts                    the Content-Security-Policy
  src/lib/api.ts                    typed API client stub (not called yet)
  src/lib/i18n.ts                   Arabic and English wording, localePath()
  src/lib/fonts.ts                  local fonts
  src/fonts/                        font files and OFL licences
  src/components/                   site shell (header, nav, footer), placeholder page
  src/app/(ar)/...                  Arabic pages, root layout lang="ar" dir="rtl"
  src/app/(en)/en/...               English pages, root layout lang="en" dir="ltr"
```

The two route groups are two root layouts, so switching language is a full
page load; navigation inside one language is client-side.

## Environment variables

| Name | Default | Meaning |
|---|---|---|
| `NEXT_PUBLIC_API_BASE_URL` | `https://api.elmetracademy.app` | Shared backend origin (api-gateway). Origin only, `https` (http only for `localhost`/`127.0.0.1`/`[::1]`); anything else fails `next build`. Also the only extra host in CSP `connect-src`. Public: compiled into the browser bundle. |

No secrets exist for the website. Every `NEXT_PUBLIC_` value is public; never
put a secret in one. `web/.env.example` is the list; `.env*` files other than
it are git-ignored.

## Local run and build

```bash
cd web
npm ci
npm run dev        # http://localhost:3000 (Arabic), /en for English
```

```bash
cd web
npm run lint && npm run typecheck && npm run build && npm start
```

The full gate, as CI and the pre-push hook run it:

```bash
bash scripts/web_gate.sh
```

It runs `npm ci`, `npm run lint` (ESLint, `--max-warnings=0`),
`npm run typecheck` (`next typegen` + `tsc --noEmit`) and `npm run build`, and
fails if the build prints any warning, with one exception: Next.js's
"⚠ No build cache found…" line, which only means `web/.next/cache` is empty
(a fresh CI runner) and says nothing about the code. That exact whole line is
allowed; any other warning, or that text changed, still fails.
`scripts/web_gate_test.sh` tests this check on sample logs. CI job "Website
(lint, typecheck, build)" runs the self-test every time, and the gate when
`web/`, `scripts/web_gate.sh`, `scripts/web_gate_test.sh` or `ci.yml` changed
(the job passes without running otherwise, so `CI OK` can require it); it
restores `web/.next/cache` with `actions/cache` (key: OS, lockfile hash, hash
of `web/**/*.ts(x)`; fallback on the lockfile hash) for speed only, and a
cache miss still passes. The pre-push hook and `make ci` run both every time
`web/package.json` exists.
Dependabot updates `web/` weekly (npm, minor and patch grouped, target
`develop`).

## Deploy on Vercel (owner steps)

The agent only documents these; the owner does them in Vercel.

1. Vercel → Add New Project → import `omarmaarouf18/wael-app`.
2. Root Directory `web`, framework preset Next.js, production branch `main`.
   Leave the build and install commands empty in the dashboard:
   `web/vercel.json` sets them (`npm ci`, `npm run build`), and `web/.npmrc`
   turns off install scripts.
3. Environment variables from `web/.env.example` (today only
   `NEXT_PUBLIC_API_BASE_URL`), for Production and Preview.
4. Domain: the owner's choice (ADR-0014 open question 1), added in Vercel
   (Project → Settings → Domains) and in DNS as Vercel shows.
5. The Hobby plan is for non-commercial use. If Vercel objects to a sales
   site on Hobby, move to Pro, or to Cloudflare Pages (static export from the
   same repo; the per-request CSP nonce would then have to become a static
   policy, see "Security headers").

`main` moves only by the owner's fast-forward (CLAUDE.md), so production
deploys follow it. Vercel also builds previews of other branches; their
`*.vercel.app` addresses must never appear in the app (the guard refuses
them).

## Domain

Not decided (ADR-0014 open question 1): `elmetracademy.app` + `www`, or a
subdomain of it. `legal.elmetracademy.app` (legal pages) and
`api.elmetracademy.app` (API) are taken. Whatever is chosen, the app's guard
already refuses the apex domain, every subdomain other than `legal` and
`api`, and Vercel hosts; a domain outside `elmetracademy.app` must be added
to `_extraWebsiteHosts` in the guard.

W3 will need CORS on the API for the chosen website origin; nothing allows
it today.

## Security headers

Set on every response. `web/next.config.ts` sets the static headers;
`web/src/proxy.ts` sets the Content-Security-Policy per request.

| Header | Value | Why |
|---|---|---|
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` | HTTPS only for a year, subdomains included; same value as the API and admin sites (owner decision 2026-10-05, S3). No `preload`. |
| `Content-Security-Policy` | see below | Only the site's own code runs; the browser talks only to the site and the API. |
| `X-Content-Type-Options` | `nosniff` | The browser never guesses a content type. |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | Other sites see at most the origin, never a path. |
| `Permissions-Policy` | camera, microphone, geolocation, payment, USB, serial, sensors, display capture, autoplay, topics… all `()` | The site needs no device feature; none can be turned on by injected code. |
| `X-Frame-Options` | `DENY` | Old-browser twin of `frame-ancestors 'none'`. |
| `Cross-Origin-Opener-Policy` | `same-origin` | Another site's window cannot keep a handle on ours. |

Next.js's `X-Powered-By` header is off.

Content-Security-Policy (`web/src/lib/csp.ts`):

| Directive | Value | Why |
|---|---|---|
| `default-src` | `'self'` | Anything not listed below: own origin only. |
| `script-src` | `'self' 'nonce-<per request>' 'strict-dynamic'` | Next.js renders inline bootstrap scripts; a fresh nonce per request allows exactly those, with no `'unsafe-inline'`. `'unsafe-eval'` is added only under `next dev` (React's development stack traces); `next build`/`next start` never add it. |
| `style-src` | `'self' 'nonce-<per request>'` | Own stylesheets and Next.js's nonced inline styles; no inline `style=` attributes (so pages use `<img>`, not `next/image`, which adds one). |
| `img-src` | `'self'` | Images are local files. |
| `font-src` | `'self'` | Fonts are served by the site; no Google Fonts. |
| `connect-src` | `'self' <API origin>` | The API origin from `NEXT_PUBLIC_API_BASE_URL` is the only other host; `'self'` is Next.js's own page data requests during navigation. |
| `object-src` | `'none'` | No plugins. |
| `base-uri` | `'self'` | No `<base>` hijacking. |
| `form-action` | `'self'` | Forms post only to the site. |
| `frame-ancestors` | `'none'` | The site cannot be framed (clickjacking). |
| `frame-src` | `'none'` | The site frames nothing. |
| `manifest-src`, `worker-src` | `'self'` | Own origin only. |
| `upgrade-insecure-requests` | (production only) | Any stray http subresource is fetched over https. |

The nonce makes every page render per request (`connection()` in both root
layouts); Next.js applies a nonce only to dynamically rendered pages. On
Vercel that means a function call per page view instead of a cached static
file — fine for a small site, and the price of a strict CSP without
`'unsafe-inline'`. A static host (Cloudflare Pages export) cannot use a
per-request nonce.

Verified locally on 2026-11-04 with `next start`: every header above is on
the response, all 11 script tags on the home page carry the nonce, the page
hydrates, client navigation between Arabic pages works, and no CSP violation
was reported.
