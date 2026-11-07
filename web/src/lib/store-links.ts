// Store listings the website may link to (ADR-0014 decision 7: the website
// may link to the stores; the apps never link to the website).
//
// Owner decisions 2026-11-07: the Google Play listing is
// https://play.google.com/store/apps/details?id=com.wael.app (the app's
// applicationId, frontend/android/app/build.gradle.kts). The iOS app is not
// live yet (in App Review): the site shows no App Store link, badge or
// "coming soon" anywhere until the owner says it is live — so there is
// deliberately no iOS URL here.
export const STORE_URLS = {
  android: "https://play.google.com/store/apps/details?id=com.wael.app",
} as const;
