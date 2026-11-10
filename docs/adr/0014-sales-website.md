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

## Amendment 2026-11-04 (3): W2b built — static pages and the CSP tradeoff

Records how W2b carries out decision 7 (options A + C) and item 4 of the
amendment above. The text above is unchanged.

1. **Static and dynamic routes.** `connection()` is gone from both root
   layouts. Home, about, subjects, subject, terms, privacy and refund (both
   languages) prerender static: `○`, and `●` for the subject pages, which
   `generateStaticParams` prebuilds from the catalog with `revalidate = 300`.
   Only `/login`, `/signup`, `/en/login` and `/en/signup` call
   `connection()` and render per request (`ƒ`); the W4 buy page joins them.
   `scripts/web_gate.sh` prints the route table and fails on any other mix.
2. **Catalog data.** Next.js reads `GET /api/v1/academy/public/catalog` on
   the server only (`API_BASE_URL`, not `NEXT_PUBLIC_`), once per build
   stage or revalidation window for every catalog page (measured: 1-2
   requests per build, not one per subject). A build with the API down or
   unset renders "unavailable"; a failed refresh at run time keeps the last
   good page. `API_BASE_URL` is required on Vercel.
3. **CSP tradeoff (recorded as decision 7 asked).** The static pages carry
   one fixed policy from `next.config.ts` with `script-src 'self'
   'unsafe-inline'` and no nonce, because a prerendered page cannot carry a
   per-request nonce and Next.js's bootstrap scripts are inline. Accepted
   because those pages have no forms, no user input, no login state and no
   token in the page, and the rest of the policy is unchanged
   (`default-src 'self'`, no third-party script host, `object-src 'none'`,
   `base-uri 'self'`, `frame-ancestors 'none'`). An injected inline script
   on a static page would still run, so nothing on those pages may render
   untrusted HTML (they render text from the catalog through React, which
   escapes it). `style-src` stays `'self'` (no inline style exists).
   The form pages keep the strict nonce policy
   (`'nonce-…' 'strict-dynamic'`, no `'unsafe-inline'`), and links to them
   are plain `<a>` so that they always load with their own policy instead
   of inheriting the static one through a client-side navigation.
4. **Not covered here:** design, the buy form (W4), and the W6 security
   test pass. Vercel Deployment Protection for Previews is an owner step
   (`docs/web/README.md` "Deploy on Vercel" item 6).

## Amendment 2026-11-04 (4): owner decision — Vercel deploys the website from a mirror repo

Owner decision of 2026-11-04. The decision text above is unchanged; this
amendment supersedes the sentences it names.

1. **Vercel never gets access to `wael-app`.** The website is deployed by
   Vercel from a new private repository, `omarmaarouf18/wael-app-web`: a
   one-way mirror of `web/`, exactly like `wael-app-mobile` is of
   `frontend/`. Nobody commits to it directly; every commit in it is produced
   by the sync. Vercel imports `wael-app-web` with Root Directory `/` (the
   mirror's root is the website's root). The sentence in "Decision" above —
   "Vercel builds it with Root Directory `web`" — is superseded by this
   amendment. The "Alternatives Considered" entry "a separate repository for
   the website: rejected" still holds for the source of truth (the website is
   written, reviewed, gated and secret-scanned in `wael-app`); what changed is
   the deployment target.
2. **The sync.** A new workflow "Sync Website"
   (`.github/workflows/sync-website.yml`) runs `git subtree split --prefix=web`
   and pushes the result to `wael-app-web` on every push to `develop`
   (-> `develop`) and `main` (-> `main`) that changes `web/**`. It
   force-pushes only the split result (one branch, nothing else) and fails
   loudly on any error. Modeled on "Sync Mobile Frontend", with one
   difference: the mobile sync waits for the CI Gate on `main`, while the
   website sync runs on the push itself (owner decision) and runs its own
   checks before it pushes (items 3 and 4).
3. **The guard: the mirror may only hold commits the sync produced.** The
   split is deterministic — `git subtree` copies the source commit's author
   and committer identity and dates, so the same source commit always yields
   the same split commit — so every commit a sync can ever produce is an
   ancestor of the split it computes. The workflow fetches the mirror's
   branches and requires each branch head to be an ancestor of the matching
   split: for the branch being synced, the split of the pushed commit; for
   any other branch, the split of the same-named source branch. A commit
   written by hand into the mirror is in no split history, so the sync stops
   with an error and pushes nothing. A branch in the mirror that has no
   source branch is refused the same way.
4. **The mirror holds only `web/` content and no secrets.** The workflow
   compares the split's tree with the source's `web/` tree (the two tree
   objects must be equal, so no path from outside `web/` can be in the
   mirror) and refuses a split that still carries paths under `web/`. It
   then scans the split with gitleaks (`v8.30.1`, the pinned version): over
   the split's whole history and over its tree. Any finding fails the sync
   before the push.
5. **Auth: the same GitHub App as the mobile sync.** The workflow uses the
   `wael-app` repository secrets `APP_ID` and `APP_PRIVATE_KEY` — the App
   `wael-app-sync-omarmaarouf18` (App ID 5137789) — and asks for a token
   scoped to the one repository `wael-app-web`. The App's permissions are
   unchanged (contents write, workflows write). The App must be installed on
   `wael-app-web`: it is installed on `wael-app-deploy` and `wael-app-mobile`
   only, so the owner either adds `wael-app-web` to that installation (GitHub
   → Settings → Installed GitHub Apps → `wael-app-sync-omarmaarouf18` →
   Configure → Repository access) or creates a dedicated App for the website
   and stores it as the secrets `WEB_APP_ID` and `WEB_APP_PRIVATE_KEY` (the
   workflow reads those when they are set and falls back to `APP_ID` /
   `APP_PRIVATE_KEY` otherwise; the mobile sync keeps its own secrets either
   way). Until the owner does one of these, the sync fails loudly at the
   token step and pushes nothing.
   *(Amended 2026-11-05, owner: a separate App for the website is allowed —
   the workflow reads the optional secrets `WEB_APP_ID` and
   `WEB_APP_PRIVATE_KEY` and uses them when they are set, and falls back to
   `APP_ID` / `APP_PRIVATE_KEY` otherwise; the mobile sync keeps its own
   secrets either way.)*
6. **The mirror's root README** is `web/README.md` in `wael-app`: "Mirror of
   `wael-app/web`, do not commit here."
7. **The mirror's branches.** `develop` appears at the first sync of `web/`;
   `main` appears when the owner fast-forwards `wael-app`'s `main` (the
   owner-only move, CLAUDE.md). Vercel's production branch is `main` in the
   mirror.

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

## Amendment 2026-11-05 (Part C / W3: Web auth and dynamic pages)

1. **Web client identification**: Web requests send header `X-Client: web`.
2. **Cookie refresh token**: For `X-Client: web`, `POST /api/v1/auth/login`, `POST /api/v1/auth/verify-otp`, and `POST /api/v1/auth/refresh` set/rotate the refresh token as an `HttpOnly; Secure; SameSite=Strict; Path=/api/v1/auth` cookie. The JSON response omits `refresh_token` and contains only `access_token` and `expires_in`. Mobile clients are unchanged (no cookie, refresh token in JSON response). `POST /api/v1/auth/logout` clears the cookie (`MaxAge: -1`).
3. **In-memory access token**: The website keeps the access token strictly in JavaScript memory; tokens are never persisted in localStorage or IndexedDB.
4. **Gateway CORS allowlist**: Exact match from env without wildcards (`https://elmetracademy.app`, `https://www.elmetracademy.app`, plus `http://localhost:3000` outside production). Credentials allowed only for allowlisted origins.
5. **CSRF protection**: On cookie-using routes (or whenever a refresh cookie is sent), the gateway verifies that the `Origin` header matches the exact allowlist and `X-Client: web` is present, else returns 403 `csrf_refused`.
6. **Web token restriction and device limits**: Tokens minted for web sessions carry `client: "web"`. `academy-service` refuses `POST /academy/videos/{id}/play` and `GET /academy/files/{id}/download` with 403 `web_access_forbidden`. Web sessions do not count toward the mobile 2-device cap and do not evict app sessions.
7. **Dynamic pages**: The form and authenticated user pages (`/login`, `/signup`, `/forgot-password`, `/account`, and their `/en/...` variants) render dynamically with per-request CSP nonces. All other pages remain static. `scripts/web_gate.sh` enforces this exact route list.

## Amendment 2026-11-05 (Part D / W4: Purchase request and payment methods)

1. **Console payment methods settings**: Configurable list `{kind: "wallet" | "instapay", label, number_or_address}` stored in `app_settings.payment_methods`, editable via admin console `POST /api/settings/update`, audit logged. Served via `GET /academy/payment-methods` only to authenticated web clients (`claims.Client == "web"`).
2. **Web purchase request endpoint**: `POST /academy/subjects/{id}/purchase-request` creates a `purchase_requests` row with `source: "web"`, `payment_method`, `transfer_reference`, `normalized_reference`, `sender_number`, and `amount`. Mobile access requests remain completely unchanged.
3. **Validation and business guards**: Enforces verified student, published unexpired subject, not already owned, one open pending request per student per subject, normalized transfer reference unique across all requests, length limits (reference 3-100, sender 3-50, amount > 0), `DisallowUnknownFields()`, per-user rate limit, and typed bilingual error codes (ar/en).
4. **Console review**: Requests tab displays source, method, reference, sender number, amount, and highlights price discrepancies (warning badge when amount differs from subject price). Accept and reject handlers operate unchanged; reject reasons are delivered to students.
5. **No payment reference field in payment_records**: `payment_records` schema remains strictly unchanged (`_id`, `user_id`, `subject_id`, `entitlement_id`, `request_id`, `amount`, `price_at_grant`, `source`, `recorded_by`, `recorded_at`). No payment reference field is added to `payment_records` (the transfer reference lives on the `purchase_requests` row only).
6. **IDOR protection**: `GET /academy/me/requests/{id}` returns 404 `not_found` if the request belongs to another student.
7. **App store safety**: `frontend/` contains no payment methods, wallet/InstaPay wording, or purchase forms (`frontend/test/store_safety_l10n_test.dart` passes).

## Amendment 2026-11-05 (Part E / W5: Request status and buy form)

1. **Account page requests**: `/account` (and `/en/account`) displays "My requests" (طلبات الاشتراك) showing subject title, formatted date, amount, and status badge (`pending` / `accepted` / `rejected` with rejection reason).
2. **Subject page client buy section**: The prebuilt subject page mounts client component `SubjectBuySection` which dynamically checks auth and ownership.
   - If owned: displays "you own this, open the app to study" with app store links.
   - If pending: displays pending request notification.
   - If unowned: loads payment methods and displays the transfer purchase request form.
3. **Instant app access**: Once accepted by the admin, the subject entitlement is active immediately in the mobile app with zero extra student action.

## Amendment 2026-11-05 (Part F: End-to-end stack verification)

1. **Local Compose E2E Flow**: Verified against local docker compose stack with API gateway, auth-service, academy-service, and admin-console:
   - Web student signup (`POST /api/v1/auth/signup`) with `X-Client: web` -> 201 Created.
   - Web OTP verification (`POST /api/v1/auth/verify-otp`) -> 200 OK with HttpOnly `refresh_token` cookie and in-memory `access_token` (`refresh_token` omitted from body).
   - Cookie refresh with CSRF check (`POST /api/v1/auth/refresh`) -> 403 when CSRF headers absent; 200 OK with rotated cookie when allowlisted Origin and `X-Client: web` present.
   - Payment methods fetch (`GET /api/v1/academy/payment-methods`) with web token -> 200 OK.
   - Purchase request submission (`POST /api/v1/academy/subjects/{id}/purchase-request`) -> 201 Created with `source: "web"`, `status: "pending"`.
   - Admin console review (`GET /api/requests?status=pending`) -> displays web fields (instapay, ref, sender, amount).
   - Admin accept (`POST /api/requests/accept`) -> 200 OK. Grants entitlement and appends `payment_records` row without reference field.
   - Web ownership verification: `GET /api/v1/academy/me/entitlements` returns subject ID; `GET /api/v1/academy/me/requests` shows status `accepted`. Re-requesting returns 409 `already_owned`.
   - Web token restrictions: `POST /api/v1/academy/videos/{id}/play` and `GET /api/v1/academy/subjects/{id}/files/{id}/download` return 403 `web_access_forbidden`.
2. **Production-only items**: Production Caddy edge routing on public catalog, Vercel mirror repo sync via GitHub App access, production DNS, and real Resend email delivery.

## Amendment 2026-11-05 (Orders architecture: W4, W5, C2)

- **W4 Cart Checkout**: Cart checkout is one `POST /academy/orders` per cart. The server creates one order and one purchase request per subject, all-or-none; proof fields live on the order; the amount-mismatch flag compares `amount_paid` with the sum of the order's subject prices.
- **W5 My Orders**: Reads `GET /academy/me/orders` (and `/{id}`).
- **C2 App Order Grouping**: App access requests are grouped by the server into one open `app` order per student: the app still has no cart, sends and receives nothing new, an order closes when its last pending request is decided, and a new app request starts a new order. Web and app orders never mix. Existing pending app requests are migrated into one open order per student; accepted/rejected history gets no order.

## Confirmation Note 2026-11-05 (Decision 5: Public catalog pricing)

Decision 5 stands: the website always displays prices for published subjects in the catalog and subject detail pages. The per-subject `price_visibility` switch added for the mobile app in Part D does not affect the website public endpoints (`GET /academy/public/catalog`, `GET /academy/public/subjects/{id}`).

## Amendment 2026-11-05 (Admin Console Requests Tab & Bulk Operations)

- **Requests Tab Rebuild**: The admin console Requests tab reflects the order grouping introduced in ADR-0014 amendment 2026-11-05 (Part C2 & Part F). Students with multiple requests appear as one consolidated row.
- **Web Order Payment Validation**: For web checkout orders, `amount_paid` is validated against the sum of subject prices. If `amount_mismatch` is `true`, the console displays an amount warning badge (`⚠️`) with the recorded transfer details (method, reference, sender number).
- **Bulk Actions**: Administrators can accept or reject multiple requests across orders simultaneously with confirmation dialogues and explicit per-item failure reporting.

## Amendment 2026-11-05 (Website Cart, AuthProvider, Account Management & Legal Pages)

- **Decision 4 Amendment (Multi-subject Cart & Orders)**: In addition to single-subject purchase requests, the website provides a client-side shopping cart enabling multi-subject checkout in a single payment transaction.
  - Cart storage: Stored in browser `localStorage` under `elmetr_academy_cart_v1` with JSON validation and graceful fallback. Cleared upon successful checkout.
  - Checkout submission: Calls `POST /api/v1/academy/orders` with `claims.client == "web"`, passing `subject_ids` (1–50 items), `payment_method` (`"wallet"` | `"instapay"`), `transfer_reference`, `sender_number`, and `amount_paid`.
  - Error handling: Displays typed bilingual error messages returned by academy-service (`already_owned`, `pending_request_exists`, `duplicate_reference`, `subject_expired`, `invalid_amount`) naming the specific subject where applicable.
  - Order history: Exposes `GET /api/v1/academy/me/orders` on the `/account` hub, showing order date, proof fields, total, and individual per-subject statuses (`pending`, `accepted`, `rejected` with `reject_reason`).
- **Decision 7 Amendment (Client AuthProvider & Session Persistence)**: The website maintains session persistence across navigations and page reloads via a client-side `AuthProvider`:
  - On initial mount, `AuthProvider` calls `POST /api/v1/auth/refresh` with `credentials: "include"` and header `X-Client: web`. Concurrent refresh requests are deduplicated into a single in-flight promise.
  - The short-lived access token is stored strictly in JavaScript memory (`inMemoryAccessToken`) and passed via `Authorization: Bearer <token>` on authenticated API requests.
  - A `401 Unauthorized` response on refresh silently clears state to unauthenticated without displaying error banners to visitors.
  - Web sessions are labeled `"web"` in device session listings and are exempt from the mobile 2-device cap.
- **Legal Pages & Store Safety**: Replaced website legal placeholder text with permanent 308 redirects for `/terms`, `/privacy`, `/en/terms`, and `/en/privacy` to `https://legal.elmetracademy.app/terms` and `https://legal.elmetracademy.app/privacy`. `/refund` and `/en/refund` remain owner TODO placeholders.

## Amendment 2026-11-07 (UI/UX brief: owner decisions)

Owner decisions of 2026-11-07, after the UI/UX brief's step 4 report. The
text above is unchanged.

1. **LCP accepted as measured.** Lighthouse mobile LCP 2.72-3.60 s
   (budget 2.5 s, `docs/web/README.md` "Performance") is accepted; fonts and
   the framework are not changed for it. The owner checks real-user numbers
   on Vercel after launch.
2. **`noindex` stays until the owner says "launch".** Then, in one commit:
   remove `noindex`, and add `sitemap.xml` and `robots.txt` for the public
   pages (home, catalog, subject pages, legal), with the account and form
   pages (`/login`, `/signup`, `/forgot-password`, `/account` and their
   `/en` versions) excluded.
3. **Store links.** The Google Play listing is
   `https://play.google.com/store/apps/details?id=com.wael.app`. The iOS app
   is not live (in App Review): the website shows no App Store link, badge
   or "coming soon" anywhere until the owner says it is live
   (`web/test/store-links.test.ts` fails on any App Store link).
4. **Landing assets.** The teacher portrait is the app's director portrait
   (v3, 2026-10-12); the app section uses the existing Google Play store
   screenshots (`docs/asset-provenance.md`).

## Amendment 2026-11-10 (Compensating controls for static-page CSP: H1 / 5.3)

Owner decision of 2026-11-10 on review finding H1 / 5.3:

1. **Evolution of static pages**: Originally (decision 7), static pages carried no login state, no token, and no forms. Following the client-side cart, `AuthProvider` (Amendment 2026-11-05), and `SubjectBuySection` additions, prerendered static catalog and detail pages now execute client components that mount the shopping cart, manage in-memory session access tokens, and render checkout forms.
2. **Why `'unsafe-inline'` is retained on static pages**: Next.js static prerendering generates inline bootstrap scripts on build without access to a per-request CSP nonce. Turning all catalog and subject pages dynamic solely to inject per-request nonces would defeat static site caching and degrade mobile LCP. Therefore, the static CSP policy (`script-src 'self' 'unsafe-inline'`) is retained on static pages.
3. **Compensating controls**: To strictly mitigate XSS risks on pages with `'unsafe-inline'`, automated build-time ESLint rules (`web/eslint.config.mjs`, verified by `web/test/csp-compensating-controls.test.ts` and `scripts/web_gate.sh`) forbid:
   - `dangerouslySetInnerHTML` in JSX attributes and property accesses
   - Direct HTML assignments or calls: `innerHTML`, `outerHTML`, `insertAdjacentHTML`
   - Document writes: `document.write`, `document.writeln`
   - Dynamic code evaluation: `eval()`, `new Function()`, and `Function()`
   The site renders all dynamic API data strictly via React standard text bindings, which escape HTML characters.
