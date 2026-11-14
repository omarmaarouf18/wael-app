# ADR-0015: In-App Purchase Packages on iOS

- **Status**: Accepted
- **Date**: 2026-11-14
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

On 2026-11-13, Apple App Review rejected iOS build 1.0.1 (10) under App Store Review
Guideline 3.1.1 (In-App Purchase). The rejection cited that digital educational
content bought outside the app (academy manual payment transfer and administrator
activation) was not purchasable via In-App Purchase inside the app, disqualifying
it from the multiplatform services exception under Guideline 3.1.3(b).

The previous attempt on 2026-11-12 (commit fdb83e3) configured the iOS app as a
content-only "reader app" under Guideline 3.1.3(a). On 2026-11-13, Apple App Review
rejected the build under Guideline 3.1.1 / 3.1.3(b), stating that digital content bought
outside the app is not available to purchase with In-App Purchase.

To unblock iOS App Store distribution while preserving existing business operations,
the academy will sell packages of subjects on iOS using Apple In-App Purchase
(StoreKit 2 Non-Renewing Subscriptions). All existing channels (manual transfers
via the website and wallets/InstaPay, academy activations, and the Android mobile app)
remain completely unchanged and operational in parallel.

## Decision

Owner decisions (2026-11-13), locked:

1. **iOS only**: Android app, website (`web/`), public catalog, and every existing
   API response stay byte-for-byte the same. The app decides purchase presentation
   by platform (`StorePolicy`, `Theme.of(context).platform` in widgets), never by
   a server flag. Server work is purely additive: new MongoDB collections and new
   endpoints.
2. **Product type: Non-Renewing Subscription**: App Store Connect holds only
   Product ID, name, and price tier. Price changes are configured exclusively in
   App Store Connect, never by our backend. The iOS app always displays the localized
   price returned by StoreKit 2; our API never carries or returns an IAP price.
3. **Products (created in App Store Connect, status "Prepare for Submission")**:
   | Product ID | App Store Name (ar / en) |
   |---|---|
   | `com.wael.app.pkg.year1` | الفرقة الأولى / Year 1 |
   | `com.wael.app.pkg.year2` | الفرقة الثانية / Year 2 |
   | `com.wael.app.pkg.year3` | الفرقة الثالثة / Year 3 |
   | `com.wael.app.pkg.year4` | الفرقة الرابعة / Year 4 |
   Bundle ID is `com.wael.app`. No other product IDs are created or referenced.
   Historical `...yearN.term1` IDs were deleted and must never appear anywhere.
4. **A package is managed only in the admin console**: Fixed Product ID (immutable
   after creation), titles (Arabic and English), optional descriptions (Arabic and
   English), a hand-picked list of subject IDs (add, remove, or reorder at any time;
   may be empty), optional `available_until` timestamp, `published` toggle, and
   display `position`. There is no automatic mapping from academic levels or terms.
5. **Buying grants immediately and automatically**: With no manual admin review or
   approval step, buying a package immediately grants every subject in that package
   at the time of purchase. Entitlements already granted remain with the student;
   subsequent edits to package subject membership apply only to future buyers.
6. **Entitlement expiry equals subject `access_expires_at`**: Exactly like an admin
   activation, the existing `Store.Grant` sets entitlement expiration to the subject's
   configured `access_expires_at` (the core grant method remains unchanged). The
   package `available_until` date governs only the app display ("متاحة حتى ...") and
   whether the client displays the purchase button.
7. **A valid Apple transaction is always honoured**: If Apple charged the student,
   the server grants the package's current subjects even if the package is unpublished
   or past `available_until` (since Apple may deliver transactions regardless of server
   flags). The `on_sale` flag is UI presentation only.
8. **Single subjects unchanged**: Opening a locked subject on iOS still sends an
   automatic access request; administrators can still manually activate students who
   paid through outside channels. On iOS, locked subject text directs the student to
   the corresponding package rather than showing the neutral reader copy from fdb83e3.
   *(Amended 2026-11-14, owner decision: the previous sentence is superseded. On
   iOS a locked subject never mentions or links to a package, whether or not one
   lists it: it shows only the neutral "المادة دي مش متاحة ليك حاليًا" / "This
   subject isn't available to you right now", with no button. The automatic access
   request still sends; the packages entry tile and packages screen are unchanged;
   Android is unchanged.)*
9. **Subjects in no package**: The admin console displays an informative warning for
   published subjects that do not belong to any published package; nothing is hidden
   automatically.
10. **Real content and real prices only**: No demo, test, or placeholder content;
    nothing hidden from App Review; no behavior or response differs during App Review.
11. **Sandbox safety**: Because App Review and TestFlight operate in the Apple Sandbox
    environment where purchases are free, and TestFlight is publicly accessible,
    Sandbox transactions are accepted only for allowlisted user IDs via server
    configuration. All other Sandbox transactions receive HTTP 403 `sandbox_not_allowed`.

## Consequences

### Positive

- Unblocks iOS App Store approval under Guideline 3.1.1 and 3.1.3(b) multiplatform
  services rules.
- Streamlined 1-tap purchase and immediate activation for iOS students using StoreKit 2.
- Zero breaking changes to existing Android clients, website checkout, or admin
  manual activation workflows.
- Clean additive backend architecture with dedicated collections (`iap_packages` and
  `iap_purchases`) and endpoints.
- Deterministic `app_account_token` ties transactions securely to specific student
  accounts without server storage.

### Negative and Tradeoffs

- Apple collects its standard App Store commission (15% for Small Business Program)
  on all iOS in-app purchases.
- Administrators must maintain package subject lists in the admin console in addition
  to individual subject management.
- Sandbox allowlist configuration must be kept up to date for internal QA and App
  Review test accounts.

## Alternatives Considered

- **Reader-app only (Guideline 3.1.3(a))**: Rejected by Apple App Review on 2026-11-13
  under Guideline 3.1.1 / 3.1.3(b) because content bought outside the app was not
  available to purchase via In-App Purchase.
- **Per-subject IAP**: Rejected because curriculum subjects change, expire, and rotate
  every academic semester. Creating individual IAP products for every subject would
  require maintaining dozens of App Store Connect products and price matrices. Selling
  four stable academic year packages matches university enrollment and limits App Store
  Connect configuration to 4 permanent IDs.
- **Hide content during review**: Rejected categorically under Guideline 2.3.1 (hidden
  or undocumented features). All features, packages, and pricing must remain honest,
  transparent, and identical between App Review and production.

## Open Questions

- Handling refunds and revocations via App Store Server Notifications V2 (tracking
  issue #61).
- App Store Connect screenshot and metadata review requirements for Non-Renewing
  Subscription packages.

## To verify

- Check whether Apple App Review approves Non-Renewing Subscription packages without
  mandating individual subject in-app purchases.
- Verify that StoreKit 2 JWS transaction payloads in both Sandbox and Production
  consistently include `appAccountToken`.
- Verify certificate chain validation against pinned Apple Root CA - G3 and WWDR
  intermediate marker OIDs during live Sandbox testing.
