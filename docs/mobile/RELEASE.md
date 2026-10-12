# Mobile release (Android)

How the student app ships. Sources of truth: `frontend/android/app/build.gradle.kts`,
`frontend/pubspec.yaml`, `frontend/.github/workflows/build-apk.yml`
(which runs only in wael-app-mobile, where `frontend/` is the repo root),
and `frontend/lib/core/app_config.dart`.

## Identity

- `applicationId = com.wael.app` (`build.gradle.kts`). It is permanent after
  the first Play upload.
- TODO(owner): confirm `com.wael.app` BEFORE the first Play upload; after that
  it can never change.

## Version

- Current: `1.0.0+1` (`pubspec.yaml`): `versionName 1.0.0`, `versionCode 1`.
  *(Amended 2026-10-12, owner decision: the next store build is `1.0.1+2` —
  `versionName 1.0.1`, `versionCode 2`, iOS `CFBundleShortVersionString 1.0.1`,
  `CFBundleVersion 2` — and `pubspec.yaml` now says so. The update gate
  compares the version name only (`PackageInfo.version`), so `1.0.1` is the
  first build an app-config `min_version` can tell apart from the `1.0.0`
  installs.)*
- Bump rule: raise the build number (`+N`, Android `versionCode`) on EVERY
  release build — Android refuses an upload whose `versionCode` is not higher
  than the last one. Raise the version name (`1.0.0`) for user-visible
  changes. Both may be overridden at build time (`--build-name` /
  `--build-number`); the default source is `pubspec.yaml`.
- The CI tags releases `v<version>-<short-sha>` from `pubspec.yaml` and the
  commit sha.

## Signing

- Release signing reads `android/key.properties` (git-ignored, never
  committed). Without it, release builds are debug-signed and must NOT be
  distributed — the CI labels them so.
- In wael-app-mobile the CI writes `key.properties` and
  `android/app/upload-keystore.jks` from four secrets, then deletes both after
  the build (`Remove signing material` step, always runs):
  `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`,
  `ANDROID_KEY_PASSWORD`.
- Release builds enable R8 (`isMinifyEnabled`, `isShrinkResources`) with
  `proguard-rules.pro`.
- TODO(owner): Play App Signing enrollment (upload key vs Play-held signing
  key) and where the upload keystore backup lives. No keystore or password is
  ever committed.

## API base URL

- `API_BASE_URL` (wael-app-mobile repo variable) is REQUIRED and must start
  with `https://` — the workflow fails otherwise. Production:
  `https://api.elmetracademy.app`.
- Warning: without it a release build would fall back to the compiled-in
  default (`https://10.0.2.2:8080` on Android, the emulator host loopback),
  which is unreachable on real phones. Never ship a build that used the
  fallback.

## Build (build-apk workflow)

```bash
# Runs in wael-app-mobile on push to main, or via workflow_dispatch.
flutter build apk --release --obfuscate --split-debug-info=build/app/outputs/symbols --dart-define=API_BASE_URL="$API_BASE_URL"
flutter build appbundle --release --obfuscate --split-debug-info=build/app/outputs/symbols --dart-define=API_BASE_URL="$API_BASE_URL"  # signed builds only
```

- With the four signing secrets: builds a signed APK + AAB and creates a
  GitHub Release holding both. Upload the AAB to Play (see
  `docs/mobile/PLAY-STORE.md`).
- Without them: builds a debug-signed APK as a workflow artifact only,
  clearly labelled NOT for distribution.
- Toolchain pinned in the workflow: Java 17 (Temurin), Flutter `3.44.6`
  (same pin as wael-app CI).

## Update gate values (per platform)

*(Added 2026-10-12, owner decision: the update link is set per platform; iOS
must never show an Android/APK link.)* The app reads these app-config fields
(server env in brackets, `infrastructure/deploy/SERVER-MANUAL.md` §5):

| Platform | Link (first non-empty wins) | Minimum / latest version |
|----------|-----------------------------|--------------------------|
| Android | `update_url_android` (`UPDATE_URL_ANDROID`), else `update_url` (`UPDATE_URL`) | `min_version` / `latest_version` (`MIN_VERSION` / `LATEST_VERSION`) |
| iOS | `update_url_ios` (`UPDATE_URL_IOS`), else `update_url` — either one **only** when it is an `https://apps.apple.com/...` link | `min_version_ios` / `latest_version_ios` (`MIN_VERSION_IOS` / `LATEST_VERSION_IOS`), else the shared ones |

- With no usable link, a required update shows the support WhatsApp button
  (from app-config) instead of a dead end, and the Settings "Update available"
  tile opens the same chat; with neither, the gate shows the contact-support
  text and the tile is hidden.
- Installed `1.0.0` apps read only the shared fields. Keep `UPDATE_URL` an
  Android link (they show it on iOS too) until no `1.0.0` iOS install is left,
  or leave it empty.
- Code: `AppConfigProvider.updateUrlFor` / `updateState`
  (`frontend/lib/providers/app_config_provider.dart`); tests:
  `frontend/test/update_per_platform_test.dart`.
