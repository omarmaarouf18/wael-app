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
| App icons | `scripts/make_app_icons.sh ios` (same crop as Android, no alpha) |

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
- Privacy Policy URL (`PRIVACY_URL`), Support URL, description, keywords,
  category Education, age rating questionnaire.
- **App Privacy**: name, email, phone number and user ID are collected and
  linked to the user for app functionality; no tracking; no third-party
  analytics in the app.
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

## Not verified

The workflow has not run on a macOS runner yet (actionlint and shellcheck
pass; the signing script was run against the real project file on Linux).
The first run in `wael-app-mobile` is the verification.
