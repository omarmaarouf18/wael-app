# Website (`web/`)

El Metr Academy's sales website. Decision record: ADR-0014
(`docs/adr/0014-sales-website.md`); SPEC Section 16. State: **foundations**
(2026-11-04) — scaffold, placeholder pages, CI, security headers and deploy
notes. No page calls the API yet; no payment code exists.
*(Amended 2026-11-04: W2a, the public catalog API, is built on `develop`
(`GET /api/v1/academy/public/catalog` and
`GET /api/v1/academy/public/subjects/{id}`, no login, price always included,
`Cache-Control: public, max-age=60` + ETag, per-IP limits; shape in SPEC
Section 6, amendment 2026-11-04). No page calls it yet: the site reads it in
W2b, on the server side. It reaches production only after the owner
fast-forwards `main` and deploys. Per-IP limits: about 100 requests a minute
from one IP at the gateway, so a build that fetches every subject page
separately must stay under that, or read the subjects from the one catalog
call.)*
*(Amended 2026-11-04, W2b: "No page calls it yet" no longer holds. The
`/subjects` and `/subjects/[id]` pages read the catalog on the server
(`API_BASE_URL`; "Catalog data" below); the browser still calls no API. No
payment code exists.)*
*(Amended 2026-11-04, owner decision: Vercel deploys the website from the
private mirror repo `omarmaarouf18/wael-app-web` — a one-way mirror of
`web/` — so Vercel never gets access to wael-app. The "Sync Website"
workflow keeps the mirror in step; see "Mirror repo" below and ADR-0014
"Amendment 2026-11-04 (4)".)*

## Purpose

A site for selling subjects, not a web version of the app. A student sees
the subjects and prices, signs up or logs in with the same account as the
app, sends a manual transfer (owner's wallets or InstaPay), and submits a
purchase request with the transfer details. An admin checks it and activates
the subject in the console; the student then studies in the mobile app. The
website never plays videos or opens files.

The apps never mention or link to the website (SPEC Section 2 D3 amendment
2026-11-04). `frontend/test/store_safety_l10n_test.dart` ("Website address
guard") fails if `frontend/` contains the website's address or a link to it
(the apex `elmetracademy.app`, `www.elmetracademy.app` or a `*.vercel.app`
deployment; owner decision 2026-11-04).
Links from the website to the store listings are fine (W5).

## Pages

Arabic is the default (right to left) at the root paths; English is under
`/en` and every page has a language switch.

| Arabic | English | Page | State |
|---|---|---|---|
| `/` | `/en` | Home | Placeholder (W5) |
| `/about` | `/en/about` | About the academy | Placeholder (W5) |
| `/subjects` | `/en/subjects` | Subjects catalog with prices | Real catalog page (W2) |
| `/subjects/[id]` | `/en/subjects/[id]` | Subject page, buy | Real subject page (W2, buy in W4) |
| `/login` | `/en/login` | Log in (same account as the app) | Real login page (W3) |
| `/signup` | `/en/signup` | Sign up with email OTP | Real signup page (W3) |
| `/forgot-password` | `/en/forgot-password` | Forgot password reset | Real reset flow (W3) |
| `/account` | `/en/account` | My account (owned subjects, app links) | Real account page (W3) |
| `/terms` | `/en/terms` | Terms | Placeholder (owner text) |
| `/privacy` | `/en/privacy` | Privacy | Placeholder (owner text) |
| `/refund` | `/en/refund` | Refund policy | Placeholder (owner text) |

Every placeholder shows a visible "TODO" note. No price, subject or policy
text is made up. All pages send `noindex, nofollow` until W5 puts real pages
up.

*(Amended 2026-11-04, W2b: `/subjects` and `/subjects/[id]` (both
languages) are real pages now, plain and functional (design comes in a later
brief). `/subjects` lists the published subjects grouped by study type and
level, each with its term and price; `/subjects/[id]` shows the level,
title, price, description, instructor, counts, "available until" date, a
"study in the app" line and a TODO for the W4 buy form. An unknown,
draft or expired subject is a 404 with a localized "not found" page. If the
catalog cannot be read, the page says so ("unavailable") and shows no
subject. Both are built from one catalog request — see "Catalog data". The
other pages are unchanged placeholders.)*

*(Amended 2026-11-05, W3: `/login`, `/signup`, `/forgot-password`, and
`/account` (both languages) are real functional pages now. Web auth uses
`X-Client: web`, with refresh tokens kept in HttpOnly cookies and access tokens
in JS memory only. Web sessions do not count toward the mobile 2-device limit.
Web tokens are restricted from `/play` and file downloads in academy-service.
These four pages are dynamic (ƒ) with per-request nonce CSP; all others are
static (○/●).)*

*(Amended 2026-11-05, W4 & W5: `/account` displays "My requests" (طلبات الاشتراك) showing real request history, status badges (`pending` / `accepted` / `rejected`), rejection reason, amount, and formatted date. The subject page mounts client buy component `SubjectBuySection`: if owned, informs the student with mobile app links; if pending, informs the student; if unowned, loads payment methods from `GET /api/v1/academy/payment-methods` and presents the manual transfer buy form (`POST /api/v1/academy/subjects/{id}/purchase-request`). The subject page remains prebuilt/static (●) without making the route dynamic, maintaining the strict static CSP on `/subjects/[id]`. Only form pages remain dynamic (ƒ).)*

## Catalog data

*(Added 2026-11-04, W2b.)* `web/src/lib/catalog.ts` reads
`GET /api/v1/academy/public/catalog` on the server (build time, then every
300 s via `revalidate`), never in the browser, so there is no CORS and
Vercel Previews work. `/subjects`, `generateStaticParams` of
`/subjects/[id]` and every subject page read the same URL with the same
revalidate value, so they share one response (owner decision 2026-11-04,
ADR-0014 "Amendment 2026-11-04 (2)" item 4). Measured against a local mock
API (2026-11-04): a full `next build` with 4 subjects (8 subject pages and
both `/subjects` pages) made 1 or 2 catalog requests (one per build stage),
never one per subject.

- `web/src/lib/catalog-data.ts` parses the response strictly and keeps only
  the fields the pages render; a malformed subject or level is dropped on its
  own, an id that is not `[A-Za-z0-9_-]{1,64}` is dropped (ids go into
  URLs), and any extra field (a content id, for example) never reaches a page.
  Unit tests: `web/test/catalog-data.test.ts` (`npm test`, `node --test`).
- When the API is unreachable or answers an error during `next build`, the
  pages build as "unavailable" (CI has no API; the production API has the
  route only after the owner's deploy). At run time a failed refresh throws,
  so Next.js keeps serving the last good page.
- A subject published after the build has no prebuilt page: its first
  request renders it (and caches it the same way). It appears within about
  one to two revalidation windows (5-10 minutes): the catalog response
  is cached for 300 s, and a 404 rendered before it appeared is cached for
  300 s too.

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
  *(Amended 2026-11-07, UI/UX brief step 1: the tokens moved to
  `web/src/app/tokens.css` (imported by `globals.css`), the one file with raw
  design values; the website design system is `web/DESIGN.md`, and
  `web/test/design-tokens.test.ts` fails on a colour literal outside
  `tokens.css`, a physical left/right property, or an inline `style=`.)*
- Logo and art: copies of repo assets listed in `docs/asset-provenance.md`
  ("Website copies").

## Folder layout

```text
web/
  package.json, package-lock.json   pinned dependencies (npm ci only)
  .npmrc                            ignore-scripts, save-exact
  README.md                         mirror notice (the mirror repo's root README)
  vercel.json                       Vercel install (npm ci) and build commands
  next.config.ts                    static security headers; validates the API URL at build
  eslint.config.mjs, tsconfig.json
  .env.example                      every variable, commented
  public/brand/                     logo-512.png, banner.jpg (repo asset copies)
  src/proxy.ts                      per-request CSP nonce (Next.js "proxy", formerly middleware)
  src/lib/config.ts                 NEXT_PUBLIC_API_BASE_URL and API_BASE_URL parsing (fail closed)
  src/lib/csp.ts                    the two Content-Security-Policies (static, nonce)
  src/lib/catalog.ts                server-side catalog fetch (one request, revalidate 300 s)
  src/lib/catalog-data.ts           catalog types and strict parser (pure, unit-tested)
  src/lib/api.ts                    typed API client stub (not called yet)
  src/lib/i18n.ts                   Arabic and English wording, localePath()
  src/lib/fonts.ts                  local fonts
  src/fonts/                        font files and OFL licences
  src/components/                   site shell (header, nav, footer), placeholder page, catalog pages
  DESIGN.md                         website design system (added 2026-11-07)
  src/app/tokens.css                design tokens (added 2026-11-07)
  test/                             unit tests (node --test)
  src/app/(ar)/...                  Arabic pages, root layout lang="ar" dir="rtl"
  src/app/(en)/en/...               English pages, root layout lang="en" dir="ltr"
```

The two route groups are two root layouts, so switching language is a full
page load; navigation inside one language is client-side.

## Environment variables

| Name | Default | Meaning |
|---|---|---|
| `NEXT_PUBLIC_API_BASE_URL` | `https://api.elmetracademy.app` | Shared backend origin (api-gateway). Origin only, `https` (http only for `localhost`/`127.0.0.1`/`[::1]`); anything else fails `next build`. Also the only extra host in CSP `connect-src`. Public: compiled into the browser bundle. |
| `API_BASE_URL` | none | *(Added 2026-11-04, W2b.)* Server-side API origin for the catalog pages; never in the browser bundle (not `NEXT_PUBLIC_`). Same rules as above. Required on Vercel (`next build` fails without it there); unset elsewhere (CI, local) builds the catalog pages as "unavailable" with no request. |

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
`npm run typecheck` (`next typegen` + `tsc --noEmit`), `npm test` (added
2026-11-04: `node --test` unit tests) and `npm run build`, and
fails if the build prints any warning, with one exception: Next.js's
"⚠ No build cache found…" line, which only means `web/.next/cache` is empty
(a fresh CI runner) and says nothing about the code. That exact whole line is
allowed; any other warning, or that text changed, still fails.
*(Amended 2026-11-04, W2b: the gate then prints the route table and fails
unless exactly `/login`, `/signup`, `/en/login` and `/en/signup` are dynamic
(ƒ) and every other route is static (○) or prebuilt (●); see "Security
headers".)*
`scripts/web_gate_test.sh` tests both checks on sample logs. CI job "Website
(lint, typecheck, build)" runs the self-test every time, and the gate when
`web/`, `scripts/web_gate.sh`, `scripts/web_gate_test.sh` or `ci.yml` changed
(the job passes without running otherwise, so `CI OK` can require it); it
restores `web/.next/cache` with `actions/cache` (key: OS, lockfile hash, hash
of `web/**/*.ts(x)`; fallback on the lockfile hash) for speed only, and a
cache miss still passes. The pre-push hook and `make ci` run both every time
`web/package.json` exists.
Dependabot updates `web/` weekly (npm, minor and patch grouped, target
`develop`). ESLint stays on 9 (`9.39.5`, the last 9.x, deprecated by npm)
until `eslint-plugin-react`, `eslint-plugin-import` and
`eslint-plugin-jsx-a11y` (used by `eslint-config-next`) support ESLint 10; no
npm `overrides` (owner decision 2026-11-04).

## Mirror repo (`wael-app-web`)

*(Added 2026-11-04, owner decision; ADR-0014 "Amendment 2026-11-04 (4)".)*
Vercel deploys the website from the private repo `omarmaarouf18/wael-app-web`
— a one-way mirror of `web/`, exactly like `wael-app-mobile` is of
`frontend/` — so Vercel never gets access to `wael-app`. Nobody commits to
the mirror directly.

- **The sync.** The workflow "Sync Website"
  (`.github/workflows/sync-website.yml`) runs
  `git subtree split --prefix=web` and pushes the result to the mirror on
  every push to `develop` (-> `develop`) and `main` (-> `main`) that changes
  `web/**`. It force-pushes only the split result (one branch, nothing else)
  and fails loudly on any error. Manual re-sync: the "Sync Website" workflow
  → Run workflow (on `develop` or `main`).
- **What lands in the mirror.** Only `web/` content: the split's tree must
  be the source's `web/` tree (the two tree objects are compared), and a
  split that still carries paths under `web/` is refused. gitleaks
  (`v8.30.1`, the pinned version) scans the split's whole history and its
  tree; any finding fails the sync before the push.
- **The guard.** The sync refuses to push if the mirror holds commits it did
  not produce. The split is deterministic (`git subtree` keeps the source
  commit's author and committer identity and dates, so the same source
  commit always gives the same split commit), so every commit a sync can
  produce is an ancestor of the split it computes. The workflow fetches the
  mirror's branches and requires each branch head to be an ancestor of the
  matching split: for the branch being synced, the split of the pushed
  commit; for any other branch, the split of the same-named source branch.
  A commit written by hand into the mirror is in no split history, so the
  sync stops with an error and pushes nothing. A mirror branch with no
  source branch is refused the same way.
- **Auth.** The same GitHub App as the mobile sync: the `wael-app`
  repository secrets `APP_ID` and `APP_PRIVATE_KEY` (the App
  `wael-app-sync-omarmaarouf18`, App ID 5137789), with the token scoped to
  `wael-app-web` only. The App's permissions are unchanged (contents write,
  workflows write). If the owner prefers a separate App for the website, the
  workflow also reads the optional secrets `WEB_APP_ID` and
  `WEB_APP_PRIVATE_KEY` and uses them instead; the mobile sync keeps
  `APP_ID` / `APP_PRIVATE_KEY` either way.
- **Owner step (once).** The App must be installed on `wael-app-web`; it is
  not yet. Either route works:
  1. *Add the repo to the existing App* (the App the mobile sync uses,
     `wael-app-sync-omarmaarouf18`, App ID 5137789): GitHub → Settings →
     Installed GitHub Apps → `wael-app-sync-omarmaarouf18` → Configure →
     Repository access → add `wael-app-web` → save. The App's permissions
     (Contents read/write, Workflows read/write) already cover the push.
  2. *A dedicated App for the website* (what the owner chose on 2026-11-05):
     GitHub → Settings → Applications → Developer settings → GitHub Apps →
     New GitHub App; name it (e.g. `wael-app-web-sync-omarmaarouf18`); set
     Repository permissions → Contents: Read and write (Metadata: Read-only
     is added automatically); leave "Expire user access tokens" empty; create
     it, then Install it on the account and select **only**
     `omarmaarouf18/wael-app-web`; generate a private key and download the
     `.pem`. Put both in the `wael-app` repository secrets:
     ```bash
     gh secret set WEB_APP_ID --repo omarmaarouf18/wael-app --body "<numeric-app-id>"
     gh secret set WEB_APP_PRIVATE_KEY --repo omarmaarouf18/wael-app < private-key.pem
     ```
     The workflow uses `WEB_APP_ID` / `WEB_APP_PRIVATE_KEY` when they are set
     and falls back to `APP_ID` / `APP_PRIVATE_KEY` otherwise, so the mobile
     sync keeps its own secrets either way.

  Without one of these the sync fails loudly at the token step and pushes
  nothing; the mirror keeps the commits of the last successful sync.
  Re-run it after the App is installed: the "Sync Website" workflow → Run
  workflow (on `develop` or `main`).
- **The mirror's README** is `web/README.md` in `wael-app`: "Mirror of
  `wael-app/web`, do not commit here."
- **The mirror's branches.** `develop` appears at the first sync; `main`
  appears when the owner fast-forwards `wael-app`'s `main` (the owner-only
  move, CLAUDE.md). Vercel's production branch is `main` in the mirror.

## Deploy on Vercel (owner steps)

The agent only documents these; the owner does them in Vercel.

1. Vercel → Add New Project → import `omarmaarouf18/wael-app-web`.
   *(Amended 2026-11-04, owner decision: Vercel imports the mirror repo
   `wael-app-web`, not `wael-app`, so Vercel never gets access to wael-app —
   see "Mirror repo".)*
2. Root Directory `/`, framework preset Next.js, production branch `main`.
   Leave the build and install commands empty in the dashboard:
   `vercel.json` (the mirror's root; `web/vercel.json` in wael-app) sets
   them (`npm ci`, `npm run build`), and `.npmrc` turns off install scripts.
   *(Amended 2026-11-04, owner decision: the Root Directory is `/`, not
   `web` — the mirror's root is the website's root. The mirror's `main`
   branch appears when the owner fast-forwards wael-app's `main`; until then
   only `develop` exists and there is nothing for a production build.)*
3. Environment variables from `.env.example` (the mirror's root;
   `web/.env.example` in wael-app — today only `NEXT_PUBLIC_API_BASE_URL`),
   for Production and Preview.
   *(Amended 2026-11-04, W2b: also `API_BASE_URL`
   (`https://api.elmetracademy.app`), for Production and Preview; the build
   fails on Vercel without it. Neither value is a secret.)*
4. Domain: the owner's choice (ADR-0014 open question 1), added in Vercel
   (Project → Settings → Domains) and in DNS as Vercel shows.
   *(Amended 2026-11-04, owner decision: add `elmetracademy.app` and
   `www.elmetracademy.app`; leave `api.` and `legal.` on the server.)*
5. The Hobby plan is for non-commercial use. If Vercel objects to a sales
   site on Hobby, move to Pro, or to Cloudflare Pages (static export from the
   same mirror repo; the per-request CSP nonce would then have to become a
   static policy, see "Security headers").

6. *(Added 2026-11-04.)* Deployment Protection: in Vercel → Project →
   Settings → Deployment Protection, turn on Vercel Authentication for
   Preview deployments, so previews of unfinished steps are not public. An
   owner step; nothing in the repo can set it.

The mirror's `main` moves only when the owner fast-forwards `wael-app`'s
`main` (CLAUDE.md), so production deploys follow it. Vercel also builds
previews of the mirror's other branches (`develop`); their `*.vercel.app`
addresses must never appear in the app (the guard refuses them).

## Domain

Not decided (ADR-0014 open question 1): `elmetracademy.app` + `www`, or a
subdomain of it. `legal.elmetracademy.app` (legal pages) and
`api.elmetracademy.app` (API) are taken. Whatever is chosen, the app's guard
already refuses the apex domain, every subdomain other than `legal` and
`api`, and Vercel hosts; a domain outside `elmetracademy.app` must be added
to `_extraWebsiteHosts` in the guard.

*(Amended 2026-11-04, owner decision: the website is `elmetracademy.app` and
`www.elmetracademy.app`; the API stays `api.elmetracademy.app` and the legal
pages `legal.elmetracademy.app`. The app's guard now refuses exactly the
website's hosts — the apex, `www.` and `*.vercel.app` — and allows `legal.`,
`api.` and every other subdomain; `_extraWebsiteHosts` is removed.)*

W3 will need CORS on the API for the chosen website origin; nothing allows
it today.

## Security headers

Set on every response. `web/next.config.ts` sets the static headers;
`web/src/proxy.ts` sets the Content-Security-Policy per request.
*(Amended 2026-11-04, W2b: the proxy sets the CSP on the four form pages
only; `web/next.config.ts` sets a fixed CSP on every other page. See the
amendment after the CSP table.)*

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

*(Amended 2026-11-04, W2b, ADR-0014 decision 7 options A + C: the policy
above is now the **form pages' policy** only — `/login`, `/signup`,
`/en/login`, `/en/signup` (and the W4 buy page when it exists), set per
request by `web/src/proxy.ts`, whose matcher lists exactly those paths;
those pages call `connection()` and render per request (ƒ). Every other
page is static (○, or ● for the subject pages prebuilt from the catalog)
and gets one fixed policy from `web/next.config.ts` (`buildStaticCsp` in
`web/src/lib/csp.ts`), sent on every path except the four form pages:*

| Directive | Static pages | Why |
|---|---|---|
| `script-src` | `'self' 'unsafe-inline'` (plus `'unsafe-eval'` under `next dev` only) | A prerendered page has no per-request nonce; Next.js's inline bootstrap scripts need `'unsafe-inline'`. Tradeoff recorded in ADR-0014 "Amendment 2026-11-04 (3)". |
| `style-src` | `'self'` | No static page has an inline `<style>` or `style=` attribute (checked in the built HTML), and a browser check with client-side navigation reported no violation. |
| everything else | as in the table above | Same `default-src`, `img-src`, `font-src`, `connect-src`, `object-src`, `base-uri`, `form-action`, `frame-ancestors`, `frame-src`, `manifest-src`, `worker-src`, `upgrade-insecure-requests`. |

*Each response carries exactly one `Content-Security-Policy` header (checked
with `next start`, 2026-11-04). Links to the form pages are plain `<a>`
(a full page load), not `next/link`: a client-side navigation keeps the
current document's policy, so it would run a form page under the static
policy. The web gate checks which routes are static and which are dynamic
(see "Local run and build"). Verified locally on 2026-11-04 with
`next start` and a mock catalog API: all 11 script tags on `/login` carry
the nonce, the static pages and the localized 404 render with no CSP
violation, and `/subjects` and a subject page are readable at 375 px wide
with no horizontal scroll.)*

The nonce makes every page render per request (`connection()` in both root
layouts); Next.js applies a nonce only to dynamically rendered pages. On
Vercel that means a function call per page view instead of a cached static
file — fine for a small site, and the price of a strict CSP without
`'unsafe-inline'`. A static host (Cloudflare Pages export) cannot use a
per-request nonce.

*(Amended 2026-11-04, owner decision: W2 makes the site mostly static —
the fixed pages (home, about, catalog, terms, privacy, refund) prerendered
with a static CSP in `next.config`, subject pages prebuilt with
`generateStaticParams` and `revalidate`, and the per-request nonce kept only
on the pages with forms (log in, sign up, buy). The proxy itself does not
make pages dynamic; `connection()` in the root layouts does. See ADR-0014
"Amendment 2026-11-04", item 7.)*
*(Amended 2026-11-04: done in W2b. `connection()` is gone from both root
layouts and is called only by the form pages; the per-request render and
the function call per page view now apply only to those four pages.)*

Verified locally on 2026-11-04 with `next start`: every header above is on
the response, all 11 script tags on the home page carry the nonce, the page
hydrates, client navigation between Arabic pages works, and no CSP violation
was reported.

## Amendments 2026-11-05 (Orders, Pricing, Console Groups, and Web Account/Cart)

- **Orders backend**: The API side of W4/W5 is available: `POST /academy/orders` for cart checkout and `GET /academy/me/orders` / `GET /academy/me/orders/{id}` for order history per `docs/contracts/orders-api.md`.
- **Subject price visibility**: Public catalog APIs used by the website always return `price` and `currency` for published subjects. The per-subject `price_visibility` switch (`follow_global`, `show`, `hide`) applies exclusively to the mobile application student API responses (`GET /academy/subjects`, `GET /academy/subjects/{id}`) and the admin console editor.
- **Admin console integration**: Web orders submitted through the website cart checkout flow (`POST /academy/orders`) appear in the admin console Requests tab under the corresponding student row. Web orders display the payment transfer reference, payment method (InstaPay or Wallet), sender phone number, and student-entered amount paid alongside individual course items. If the amount paid does not equal the order's subject prices total, an amount check warning badge (`⚠️`) is displayed to alert the reviewer.
- **Cart, Account Hub & Legal Pages**:
  - `CartProvider` (`web/src/components/cart-provider.tsx`): Wraps localized layout trees (`(ar)` and `(en)`). Provides `items`, `addItem`, `removeItem`, `clearCart`, `isInCart`, `totalPrice`, and drawer visibility toggles.
  - `CartDrawer` (`web/src/components/cart-drawer.tsx`): Slide-over drawer presenting cart items, total calculation, and checkout form. Integrated directly into `SiteShell` so that static catalog pages remain fully static without adding dynamic routes.
  - `AccountPageView` (`web/src/components/account-view.tsx`): Tabbed account management hub on `/account` mirroring the mobile settings experience (profile edit with 30-day rate limit, password change, device sessions, notifications inbox, orders history, account deletion).
  - Legal pages: `/terms` and `/privacy` (and `/en/...` variants) 308 redirect to the mobile app's hosted legal pages; `/refund` remains an owner TODO placeholder.
  - Route verification expectations: Dynamic routes (`ƒ`) remain strictly limited to form pages: `/account`, `/en/account`, `/forgot-password`, `/en/forgot-password`, `/login`, `/en/login`, `/signup`, `/en/signup`. All catalog, about, terms, privacy, and refund pages remain static (`○`) or SSG (`●`).

