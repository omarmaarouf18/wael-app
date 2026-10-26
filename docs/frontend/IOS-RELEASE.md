# iOS release pipeline (no Mac needed)

Owner decision 2026-10-09: prepare a production iOS build and App Store
Connect upload on GitHub's macOS runners. Everything below is done from a
browser, a Linux shell and an iPhone. SPEC Section 3 question 8 (App Store
review of content unlocked after an outside payment) is **not** re-decided
here; see "Review risks" before submitting.

| Piece | Where |
|---|---|
| Workflow | `frontend/.github/workflows/build-ios.yml` (runs in the `wael-app-mobile` mirror, manual only) |
| CI-only signing switch | `frontend/ios/ci/configure_signing.rb` (edits the runner's copy of the Xcode project; never committed) |
| Certificates on Linux | `scripts/ios_signing.sh` (OpenSSL only; writes to `~/ios-signing`, outside the repo) |
| App icons | `scripts/make_app_icons.sh ios` (same source as Android, `assets/branding/app_icon_1024.png`, no alpha) *(amended 2026-10-14: new owner icon; previously a crop of the character art)* |

What the workflow does: `macos-26` runner, Flutter 3.44.6 (same pin as CI),
temporary keychain with the distribution certificate, checks that the
profile is an unexpired App Store profile for the bundle id, `flutter build
ipa` (obfuscated, `API_BASE_URL` from the repo variable, build number =
run number + offset), `altool --validate-app` then `--upload-app` with an App
Store Connect API key, and keeps the IPA, Dart symbols and dSYMs for 30 days.
Without signing secrets it only proves the app compiles for iOS.

Cost: macOS minutes count 10x on private repositories, so it never runs on
push. One run is roughly 15-25 minutes.

## One-time setup

### 1. Apple Developer (developer.apple.com > Certificates, IDs & Profiles)

1. **Identifiers > +** > App IDs > App. Bundle ID (explicit):
   `com.wael.app` unless the owner picks another (ADR-0011 open question:
   it cannot change after the first upload; if Apple says it is taken,
   choose e.g. `app.elmetracademy.student` and set `IOS_BUNDLE_ID`).
   No extra capabilities are needed.
2. On Linux, in the repo:
   `scripts/ios_signing.sh csr "Your Name" you@example.com`
3. **Certificates > +** > Apple Distribution > upload
   `~/ios-signing/ios_distribution.csr` > download the `.cer`.
4. `scripts/ios_signing.sh p12 ~/Downloads/distribution.cer`
   (checks the certificate matches the key, writes the `.p12`, its random
   password and the base64 copy).
5. **Profiles > +** > Distribution > App Store Connect > the App ID >
   the certificate from step 3 > name it (e.g. `wael-app appstore`) >
   download. Then `scripts/ios_signing.sh profile ~/Downloads/<name>.mobileprovision`.
   The profile and certificate expire after one year: repeat 3-5 then and
   update the secrets.

### 2. App Store Connect (appstoreconnect.apple.com)

1. **Apps > + > New App**: iOS, name, primary language Arabic, the bundle
   id from step 1.1, SKU (any unique text).
2. **Users and Access > Integrations > App Store Connect API > +**: name
   `github-ci`, access **App Manager**. Download the `.p8` (only once), note
   the Key ID and the Issuer ID. Then
   `scripts/ios_signing.sh apikey ~/Downloads/AuthKey_<KEYID>.p8`.
3. Optional: App Information > **Apple ID** (a number) -> variable
   `IOS_APPLE_APP_ID`. Xcode 26.2's altool can fail with "Cannot determine
   the Apple ID from Bundle ID" without it.

### 3. GitHub (`wael-app-mobile` > Settings > Secrets and variables > Actions)

`scripts/ios_signing.sh secrets <owner>/wael-app-mobile` sets the secrets
with `gh`, or lists which file goes into which secret.

| Secret | Value |
|---|---|
| `IOS_DIST_CERT_P12_BASE64` | `~/ios-signing/IOS_DIST_CERT_P12_BASE64.txt` |
| `IOS_DIST_CERT_PASSWORD` | `~/ios-signing/IOS_DIST_CERT_PASSWORD.txt` |
| `IOS_PROFILE_BASE64` | `~/ios-signing/IOS_PROFILE_BASE64.txt` |
| `APPSTORE_API_KEY_ID` | Key ID (or `APPSTORE_API_KEY_ID.txt`) |
| `APPSTORE_API_ISSUER_ID` | Issuer ID |
| `APPSTORE_API_KEY_P8_BASE64` | `~/ios-signing/APPSTORE_API_KEY_P8_BASE64.txt` |

| Variable | Value |
|---|---|
| `API_BASE_URL` | already set for Android (https) |
| `IOS_BUNDLE_ID` | optional, default `com.wael.app` |
| `IOS_APPLE_APP_ID` | optional, see 2.3 |
| `IOS_BUILD_NUMBER_OFFSET` | optional, default `0`; raise it if a build was ever uploaded another way |
| `IOS_XCODE_VERSION` | optional, e.g. `26.1`, to pick a non-default Xcode on the image |

Keep `~/ios-signing` private (it is mode 700) and backed up offline: losing
the key means revoking the certificate and making a new one.

## Each release

1. `frontend/` reaches `wael-app-mobile` through the normal sync after the
   owner's fast-forward of `main` (ADR-0011).
2. `wael-app-mobile` > Actions > **Build iOS** > Run workflow (upload on).
3. After processing (5-30 min) the build is in App Store Connect >
   TestFlight. Add yourself as an internal tester and install it on the
   iPhone with the TestFlight app; test login, a video, settings and
   account deletion against production.
4. App Store submission is manual in App Store Connect: version page >
   choose the build > fill the listing > **Add for Review**.
   Raise `version:` in `frontend/pubspec.yaml` for each new store version;
   the build number grows by itself.

## Store listing checklist

- **Screenshots**: iPhone only (the app is iPhone-only:
  `TARGETED_DEVICE_FAMILY = 1`, so no iPad screenshots are required).
  App Store Connect requires the 6.9" size (1320 x 2868 portrait, or
  1290 x 2796); screenshots from a Pro Max / Plus iPhone fit directly.
  From a smaller iPhone they must be resized to an accepted size first.
  3 to 10 screenshots.
  *(Added 2026-10-13: the manual workflow **Store screenshots (iOS)**
  (`frontend/.github/workflows/screenshots-ios.yml`, runs in
  wael-app-mobile on `macos-26`) takes them with no Mac: it runs the real app
  on the newest 6.9-inch iPhone Simulator (1320 x 2868, status bar fixed at
  9:41) against production, signs in with a demo account, and captures login,
  home, courses, a course, files, settings and notifications in Arabic and
  English (`tool/screenshots/main.dart` is the tour; it never ships). It
  needs the secrets `SCREENSHOT_EMAIL` / `SCREENSHOT_PASSWORD` for a
  dedicated account that owns at least one subject; the tour signs out at
  the end, so it does not hold one of the account's two device slots. The
  PNGs come back as the workflow artifact `ios-screenshots-6.9-<run>`.)*
- Privacy Policy URL (`PRIVACY_URL`), Support URL, description, keywords,
  category Education, age rating questionnaire.
- **App Privacy**: name, email, phone number and user ID are collected and
  linked to the user for app functionality; no tracking; no third-party
  analytics in the app.
  *(Amended 2026-10-12: the app now ships an app-level privacy manifest,
  `frontend/ios/Runner/PrivacyInfo.xcprivacy` (in the Runner target's
  resources), declaring no tracking and these collected types: name, email,
  phone number, user ID **and device ID** — the per-install id the app sends
  for the two-device session cap and the "My devices" list. Add "Device ID"
  (linked, app functionality, no tracking) to the App Store Connect answers so
  both match.)*
  *(Amended 2026-10-26, store compliance review 2026-10-14: the manifest also
  declares **Product Interaction** (the server's `video_plays` log) and
  **Purchase History** (access requests, entitlements and `payment_records`),
  both linked, app functionality, no tracking, as the privacy policy already
  says. App Store Connect > App Privacy must list all seven types: name,
  email address, phone number, user ID, device ID, product interaction,
  purchase history. `CFBundleName` is now "EL METR ACADEMY" (was
  `wael_app`), matching `CFBundleDisplayName`.)*
- *(Added 2026-10-12, owner decision: the app display name is
  **"EL METR ACADEMY"** on both platforms — iOS `CFBundleDisplayName` (was
  "El Metr Academy") now matches Android's `android:label` and the app title.
  `CFBundleLocalizations` lists `ar` and `en`, so iOS shows system UI —
  share sheet, Quick Look, permission prompts — in the app's languages.)*
- **Sign-in for review**: the app needs an account, so App Review needs a
  demo account (email + password) with at least one active subject, and a
  note explaining that access is granted by the center.
- Export compliance is answered in the build (`ITSAppUsesNonExemptEncryption`
  = `false`: HTTPS only).

## Review risks (owner's call; SPEC Section 3 question 8)

- Guideline 3.1.1: unlocking digital content (the video courses) that was
  paid for outside the app, without in-app purchase, is the reason iOS was
  deferred on 2026-10-01. The app shows no payment action, but a reviewer
  can still reject it outside the US storefront.
- Showing subject prices (D3, `show_prices`) raises that risk on iOS. The
  switch is server-side and applies to both platforms; consider keeping
  `show_prices` off while the iOS build is in review.
- Deciding between accepting the risk, in-app purchase, or Android only
  stays with the owner.

## Verified

- 2026-10-09: first run in `wael-app-mobile` (run 37936859267) built the
  signed IPA and uploaded it to App Store Connect (1.0.0, build 1).
- App Store Connect warned ITMS-90068 (iOS 13.0 deployment target; 15.0 is
  required for uploads from April 2027). The minimum iOS version is now
  15.0 (`IPHONEOS_DEPLOYMENT_TARGET` in all three Runner configurations).
- 2026-10-13: run 38353982883 on `macos-26` built `17d1c0d` (1.0.1, iOS
  15.0 target, the 2026-10-09 TestFlight fixes, iOS screen-capture cover)
  and uploaded build 4 to App Store Connect. The run before it
  (38265421984) had failed on Xcode 26.6: `ActorIsolatedCall` errors for
  `registerForTraitChanges` / `unregisterForTraitChanges` in
  `AppDelegate.swift`, fixed in `17d1c0d`. Flutter only prints those lines
  as `... (Xcode): ...` after "Failed to build iOS app", so search a failed
  log for `(Xcode):`.
- 2026-10-14: run 38524255616 built `6d1b2b3` (1.0.1, new app icon,
  bottom-nav inset fix `ccc914c`) from wael-app-mobile `main` and uploaded
  build 6 to App Store Connect. Not verified here: processing, TestFlight
  install, and the icon and bottom bar on an iPhone.

## Not verified

*(Amended 2026-10-13: the paragraph below is superseded. The 15.0 target and
the 2026-10-09 fixes have now been built on a macOS runner (see Verified).
Still not verified: App Store Connect processing of build 4, installing it
from TestFlight, and every iPhone row of `docs/frontend/RELEASE-QA-1.0.1.md`,
including the screen-recording cover. The "Store screenshots (iOS)" workflow
has not run yet.)*

The 15.0 deployment target has not been built on a macOS runner yet; the
next "Build iOS" run after the sync is the verification. *(Added 2026-10-11:
the same unbuilt range carries the 2026-10-09 TestFlight fixes — the YouTube
frame on iOS (`32bc53d`) and iOS PDF open/share via Quick Look and the share
sheet (`b0de4d4`) — so that run re-tests all three; `origin/main` is now at
`b0de4d4`.)*
