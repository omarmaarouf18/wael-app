// Store listings the website may link to (ADR-0014 decision 7: the website
// may link to the stores; the apps never link to the website). Same defaults
// as account-view.tsx and subject-buy-view.tsx, which can also take them from
// the app config at run time; the static landing page uses these. Owner to
// confirm both are live (docs/web/landing-plan.md item 7).
export const STORE_URLS = {
  android: "https://play.google.com/store/apps/details?id=app.elmetracademy.elmetr",
  ios: "https://apps.apple.com/app/el-metr-academy/id6737155342",
} as const;
