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
