// Store listings the website may link to (ADR-0014 decision 7: the website
// may link to the stores; the apps never link to the website).
//
// Owner decisions 2026-11-07: the Google Play listing is
// https://play.google.com/store/apps/details?id=com.wael.app (the app's
// applicationId, frontend/android/app/build.gradle.kts).
// Owner decision 2026-11-23: the iOS app is live on the App Store, so the
// site links to its listing with the official App Store badge. Every App
// Store link on the site comes from this constant (web/test/store-links.test.ts).
export const STORE_URLS = {
  android: "https://play.google.com/store/apps/details?id=com.wael.app",
  ios: "https://apps.apple.com/eg/app/el-metr-academy/id6820829162",
} as const;
