import localFont from "next/font/local";

// English pages: Syne (display) and Plus Jakarta Sans (body), the app's Latin
// families (docs/frontend/DESIGN_SYSTEM.md), subset WOFF2, SIL OFL 1.1, served
// from this site. The few Arabic words on English pages (the language switch,
// the Arabic eyebrow, an Arabic-only level title) use the system Arabic font;
// see fonts-ar.ts for why the layouts do not share a font module.
//
// Not preloaded. next/font (Turbopack) merges preloads app-wide, so a
// preloaded face here was preloaded on every Arabic page too. Lighthouse
// mobile medians of 3 runs, 2026-11-07: preloading Cairo only took LCP on /
// from 3.31 s to 2.86 s and on /subjects from 2.94 s to 2.71 s; English pages
// stayed about the same (3.24 s vs 3.31 s on /en) with first paint 0.9 s ->
// 1.36 s and CLS up to 0.033 (budget 0.1). Arabic is the default site.
export const syne = localFont({
  src: [
    { path: "../fonts/Syne-Bold.woff2", weight: "700", style: "normal" },
    { path: "../fonts/Syne-ExtraBold.woff2", weight: "800", style: "normal" },
  ],
  variable: "--font-syne",
  display: "swap",
  preload: false,
});

export const plusJakartaSans = localFont({
  src: [
    { path: "../fonts/PlusJakartaSans-Regular.woff2", weight: "400", style: "normal" },
    { path: "../fonts/PlusJakartaSans-SemiBold.woff2", weight: "600", style: "normal" },
  ],
  variable: "--font-plus-jakarta-sans",
  display: "swap",
  preload: false,
});

export const fontVariablesEn = [syne.variable, plusJakartaSans.variable].join(" ");
