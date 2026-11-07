import localFont from "next/font/local";

// Same families as the app (docs/frontend/DESIGN_SYSTEM.md "AppTypography"),
// from frontend/assets/fonts/ (SIL OFL 1.1, licences beside them), subset to
// the scripts the site renders and converted to WOFF2 (2026-11-07,
// docs/asset-provenance.md "Website copies"): about 124 KB for all seven
// faces instead of 506 KB of TTF. Served from this site; nothing is fetched
// from Google.
//
// Every face is preloaded on every page. A per-layout split (Cairo only on
// Arabic pages) does not work with next/font: it merges identical font files
// and any preload wins. To be revisited with Lighthouse numbers (UI/UX brief
// step 4).

export const cairo = localFont({
  src: [
    { path: "../fonts/Cairo-Regular.woff2", weight: "400", style: "normal" },
    { path: "../fonts/Cairo-Bold.woff2", weight: "700", style: "normal" },
    { path: "../fonts/Cairo-ExtraBold.woff2", weight: "800", style: "normal" },
  ],
  variable: "--font-cairo",
  display: "swap",
});

export const syne = localFont({
  src: [
    { path: "../fonts/Syne-Bold.woff2", weight: "700", style: "normal" },
    { path: "../fonts/Syne-ExtraBold.woff2", weight: "800", style: "normal" },
  ],
  variable: "--font-syne",
  display: "swap",
});

export const plusJakartaSans = localFont({
  src: [
    { path: "../fonts/PlusJakartaSans-Regular.woff2", weight: "400", style: "normal" },
    { path: "../fonts/PlusJakartaSans-SemiBold.woff2", weight: "600", style: "normal" },
  ],
  variable: "--font-plus-jakarta-sans",
  display: "swap",
});

export const fontVariables = [cairo.variable, syne.variable, plusJakartaSans.variable].join(" ");
