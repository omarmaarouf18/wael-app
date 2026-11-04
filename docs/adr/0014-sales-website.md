# ADR-0014: Sales Website (El Metr Academy)

- **Status**: Accepted
- **Date**: 2026-11-04
- **Related Commit SHA**: none (decision only; the foundations scaffold in `web/` is built from this ADR, see `docs/web/README.md`)
- **Related Audit Finding**: n/a

## Context

The app sells nothing. A student opens a locked subject, the app creates an
access request, and the center activates the subject after the student pays
outside the app (ADR-0003 and its amendments, SPEC Section 1 decision 8 and
Section 2 D3). The app never shows a payment action.

App Store review is the pressure behind this record. Apple guideline 3.1.1
(unlocking digital content paid for outside the app without in-app purchase)
is the risk that deferred iOS on 2026-10-01 (`docs/frontend/IOS-RELEASE.md`
"Review risks"), and the owner's brief of 2026-11-04 cites guidelines 2.1(b)
and 3.1.1 from the App Store review. The model that fits both stores is the
reader app (Apple 3.1.3(a)): the app gives access to content the student
already bought elsewhere and never mentions or links to a way to buy it. That
model needs a place outside the apps where a student can see subjects and
prices and ask to buy one. Today that place is the center and WhatsApp only.

## Decision

Owner decision 2026-11-04.

1. El Metr Academy gets a website for selling subjects. It is a sales site,
   not a web version of the app.
2. Pages: home, about the academy, subjects catalog with prices, subject page,
   sign up / log in (same account as the app), buy, terms, privacy, refund
   policy.
3. After buying, the subject is activated on the student's account and the
   student studies in the mobile app. The website never plays videos or opens
   files.
4. Payment is a manual transfer to the owner's wallets (Vodafone Cash etc.)
   and InstaPay. No payment gateway. The student sends the transfer, submits a
   purchase request on the website with the transfer details, and an admin
   checks it and activates the subject in the console. This reuses the
   existing purchase request / payment record / grant flow.
5. Center activation from the admin console stays as today, in parallel.
6. Hosting: Vercel (Hobby plan to start). Backend and database: the existing
   server, shared with the app.
7. The mobile apps never mention or link to the website for buying (Apple
   3.1.3(a) reader app, Google Play payments policy). Store links from the
   website to the apps are fine.

Implementation placement (from the same brief): the website lives in the
`web/` folder of this repository (Next.js), so it shares one `CLAUDE.md`, one
SPEC/ADR set, one CI and one gitleaks scan with the rest of the product.
Vercel builds it with Root Directory `web`.

## Consequences

### Positive

- Shared auth and catalog: one account works on the website and in the app;
  the website reads the same subjects and prices the console manages. No
  second user store and no second catalog.
- Manual transfer payments verified by an admin, through the request, payment
  record and grant path the console already has (SPEC Phase 4.5-4.6). The
  website never activates anything; activation stays a server-side admin
  action.
- Wallet numbers and the InstaPay address are shown only on the website.
  The apps keep their store-safety rules unchanged (no payment action, no
  payment methods, no payment links).
- The apps never link to the site: the store-safety guard
  (`frontend/test/store_safety_l10n_test.dart`) fails if `frontend/` contains
  the website's address or a link to it (SPEC Section 2 D3 amendment
  2026-11-04).

### Negative and Tradeoffs

- Manual verification costs admin time per purchase and delays access until
  an admin checks the transfer; a payment gateway would not.
- A transfer can be claimed falsely or twice; W4 must refuse duplicates and
  the admin must check the wallet before approving.
- A second public surface to secure (CSP, auth cookies, CORS, rate limits),
  and the API gains its first unauthenticated endpoint (W2).
- Vercel Hobby is for non-commercial use. A sales site may be refused there;
  the fallback is Vercel Pro or Cloudflare Pages (static export from the same
  repo) — `docs/web/README.md` "Deploy on Vercel".
- Students cannot learn about the website from the apps; it must be promoted
  outside them.

## Alternatives Considered

- In-app purchase (Apple IAP / Google Play Billing): store commission on
  every subject and a different payment path per platform; not chosen.
- A payment gateway on the website: not chosen; manual transfers to the
  owner's wallets and InstaPay are what the owner uses today.
- A web version of the app (videos and files in the browser): rejected;
  content protection (watermark, device cap, recording protection) lives in
  the app.
- A separate repository for the website: rejected; one repo keeps one set of
  rules, decisions, CI and secret scanning.

## Planned phases (not built)

The foundations brief (2026-11-04) builds only docs, the `web/` scaffold with
placeholder pages, CI, security headers and deploy notes: no payment code, no
backend change, no real API calls. Later briefs:

- **W2 Public catalog**: an unauthenticated read-only endpoint for published
  subjects with price and description; rate-limited; cached.
  *(Amended 2026-11-04, owner decision 7: W2 also makes the website's pages
  static, options A and C of the 2026-11-04 static-pages report — see the
  amendment below — not as a separate fix.)*
  *(Amended 2026-11-04, W2a built on `develop` (W2-W5 brief, step 1): `GET /academy/public/catalog` (gateway `/api/v1/academy/public/catalog`)
and `GET /academy/public/subjects/{id}` in academy-service, through the existing
  `/api/v1/academy/` gateway route. No student JWT; the gateway secret is
  still required. Published subjects under published levels only (the app's
  non-owner rule, R8); each has id, level, term, title, description, price
  and `EGP` (always, owner decision 5; `show_prices` and
  `EXPOSE_PRICE_TO_STUDENTS` do not apply), instructor and counts of
  videos, books and notes, plus `access_expires_at`. Never a video, YouTube
  or file id, storage key, download or play path. A draft, unknown or
  hidden-level subject is 404 (`Cache-Control: no-store`); a 200 carries
  `Cache-Control: public, max-age=60` and a strong ETag (304 on
  `If-None-Match`). Per-IP limits: academy read tier (120/min) and the
  gateway edge limiter (100/min) in its own `gwpub:` bucket, separate from
  the auth bucket. The instructor is the platform's one teacher, served
  from a server constant equal to the app's director profile, since
  subjects have no instructor field. The site does not call it yet (W2b).
  The API reaches production only with the owner's fast-forward of `main`
  and a deploy.)*
- **W3 Web auth**: CORS for the website origin; refresh token in an httpOnly
  Secure SameSite cookie on the website, access token in memory; same
  accounts as the app.
- **W4 Manual payments**: the subject page shows the transfer details (from
  console settings); the student submits a purchase request (subject, method,
  transfer reference, sender number, amount, optional receipt image) through
  the API; the request appears in the console's existing requests flow; the
  admin approves (grant through the existing entitlement path + payment
  record) or rejects with a reason; the student sees the status on the
  website. Server checks: logged-in student, published subject, not already
  owned, one open request per subject, duplicate transfer reference refused,
  rate limit. A receipt image needs an upload path (today students upload
  nothing and the gateway caps request bodies at 1 MiB), so size/format and
  storage are decided in that brief. Activation never happens in the browser.
  *(Amended 2026-11-04, owner decision 3: no receipt image. The student
  submits the transfer reference, sender number and amount only; the
  "optional receipt image" and the upload path above are removed from W4,
  and SPEC Section 1 item 1 (students upload nothing) stays as it is.)*
- **W5 Website pages with real data**, and store links to the apps.

## Open Questions

Owner decisions, not decided here:

1. Website domain: `elmetracademy.app` + `www`, or a subdomain.
   *(Decided 2026-11-04: `elmetracademy.app` + `www`; see the amendment
   below.)*
2. Which wallet numbers and InstaPay address the website shows, and whether
   they are edited from the console (recommended in the brief) or fixed in the
   site.
   *(Decided 2026-11-04: edited from the console, served by the API.)*
3. What the student must submit as proof: transfer reference, sender number,
   amount, and whether a receipt screenshot is also required.
   *(Decided 2026-11-04: transfer reference, sender number, amount; no
   receipt image.)*
4. Refund policy text and terms text (the owner provides them).
   *(Decided 2026-11-04: the owner provides them; the placeholders stay until
   then.)*
5. Whether prices on the site always match the center's prices.
   *(Decided 2026-11-04: yes, the same prices.)*

No open question remains (2026-11-04).

## Amendment 2026-11-04: owner decisions

Owner decisions of 2026-11-04, answering the open questions above and the
foundations report of the same day. The decision text above is unchanged.

1. **Domain.** The website is `elmetracademy.app` and `www.elmetracademy.app`.
   The API stays `api.elmetracademy.app`; the legal pages stay
   `legal.elmetracademy.app`. The app guard
   (`frontend/test/store_safety_l10n_test.dart`) is narrowed to the website's
   hosts: the apex, `www.` and `*.vercel.app` (Vercel deployments). `legal.`,
   `api.` and every other subdomain are allowed; `vercel.com` is not a website
   host and is allowed too.
2. **Wallet numbers and InstaPay address** are edited by an admin in the
   console (settings) and served to the website by the API (W2/W4). The app
   never shows them.
3. **Proof of transfer**: transfer reference, sender number and amount. No
   receipt image, so SPEC Section 1 item 1 (students upload nothing) stays
   as it is and W4 has no upload path.
4. **Terms and refund text**: the owner provides it; the placeholder pages
   stay until then.
5. **Prices**: the website shows the same prices as the center (the
   subject's price in the console; no separate website price).
6. **Root `.dockerignore`** (approved): excludes `web/`, `node_modules` and
   `.next` from the repository-root build context used by the prod image
   builds.
   *(Amended 2026-11-04, owner approval in the W2-W5 brief, step 0: it also
   excludes `frontend/build/` and `frontend/.dart_tool/`. No image copies
   `frontend/`: the prod stages copy only `shared/infra` and
   `services/<svc>`, and the compose dev builds use the service directory as
   context.)*
7. **Static pages**: options A and C of the static-pages report, built inside
   W2, not as a separate fix.
   - A: the fixed pages (home, about, catalog, terms, privacy, refund) are
     prerendered static with a static CSP set in `next.config`; only the
     pages with forms (log in, sign up, buy) keep the per-request nonce and
     render per request (the proxy and `connection()` limited to them).
   - C: subject pages get `generateStaticParams` from the W2 catalog plus
     `revalidate`, so they are prebuilt and refreshed on a timer.
   - The report found that the proxy does not make pages dynamic;
     `connection()` in the root layouts does (16 of 18 routes build static
     without it), and that a static page under today's nonce CSP is blocked
     (no script carries the nonce). The static CSP of option A needs
     `script-src 'unsafe-inline'` for Next.js's inline scripts; W2 records
     that tradeoff for those pages.
   - ESLint stays on 9 (`9.39.5`, the last 9.x) until `eslint-plugin-react`,
     `eslint-plugin-import` and `eslint-plugin-jsx-a11y` support ESLint 10
     (their peer ranges end at 9 on 2026-11-04). No npm `overrides`.

## Amendment 2026-11-04 (2): owner decisions after the W2a report

Owner decisions of 2026-11-04, answering the W2a (step 1) report. The text
above is unchanged.

1. **Instructor name**: the public catalog keeps serving the platform's one
   teacher from the server constant (`models.AcademyInstructor`), equal to
   the app's director profile. No console setting for now.
2. **Expired subjects**: a published subject whose `access_expires_at` has
   passed is hidden from `GET /academy/public/catalog` and is 404 on
   `GET /academy/public/subjects/{id}`. The app is unchanged (it still lists
   such a subject). Built as a small backend fix before the W2b web work.
3. **Auth bypass**: the StudentAuth bypass for exactly the two public
   catalog paths is confirmed by the owner under SPEC Section 12 rule 6
   (access-control change held from `main` until the owner confirms).
4. **W2b builds every subject page from the one catalog call**: the site
   fetches `GET /api/v1/academy/public/catalog` once per build or
   revalidation and renders `/subjects` and every `/subjects/[id]` from it,
   so a build never makes one API call per subject (the per-IP limits stay
   as they are).

## To verify

- Vercel accepts the site on the Hobby plan (owner, when the project is
  created).
- Once open question 1 is decided, the store-safety guard covers the chosen
  host (the apex, any non-app subdomain and Vercel hosts are refused today;
  a domain outside `elmetracademy.app` must be added to `_extraWebsiteHosts`).
  *(Done 2026-11-04: the guard refuses the apex, `www.` and `*.vercel.app`;
  `_extraWebsiteHosts` is removed.)*
- Before W4: an optional receipt image is a student upload, and SPEC
  Section 1 item 1 (locked) says students upload nothing. The W4 brief needs
  an owner amendment of that item, or no receipt image.
  *(Resolved 2026-11-04: no receipt image, owner decision 3.)*
