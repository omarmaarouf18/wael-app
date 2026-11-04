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
- **W5 Website pages with real data**, and store links to the apps.

## Open Questions

Owner decisions, not decided here:

1. Website domain: `elmetracademy.app` + `www`, or a subdomain.
2. Which wallet numbers and InstaPay address the website shows, and whether
   they are edited from the console (recommended in the brief) or fixed in the
   site.
3. What the student must submit as proof: transfer reference, sender number,
   amount, and whether a receipt screenshot is also required.
4. Refund policy text and terms text (the owner provides them).
5. Whether prices on the site always match the center's prices.

## To verify

- Vercel accepts the site on the Hobby plan (owner, when the project is
  created).
- Once open question 1 is decided, the store-safety guard covers the chosen
  host (the apex, any non-app subdomain and Vercel hosts are refused today;
  a domain outside `elmetracademy.app` must be added to `_extraWebsiteHosts`).
- Before W4: an optional receipt image is a student upload, and SPEC
  Section 1 item 1 (locked) says students upload nothing. The W4 brief needs
  an owner amendment of that item, or no receipt image.
