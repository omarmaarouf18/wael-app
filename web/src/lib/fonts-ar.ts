import localFont from "next/font/local";

// Arabic pages: Cairo only (its Latin glyphs cover "EL METR ACADEMY" and the
// store names). Same family as the app (docs/frontend/DESIGN_SYSTEM.md),
// subset WOFF2, SIL OFL 1.1, served from this site.
//
// Cairo is the only preloaded family (78 KB). next/font (Turbopack) merges
// font preloads across the whole app, whatever module or layout declares
// them, so the Latin faces in fonts-en.ts are not preloaded at all (numbers
// there and in docs/web/README.md "Performance"). Two modules keep each
// layout's CSS to its own families.
export const cairo = localFont({
  src: [
    { path: "../fonts/Cairo-Regular.woff2", weight: "400", style: "normal" },
    { path: "../fonts/Cairo-Bold.woff2", weight: "700", style: "normal" },
    { path: "../fonts/Cairo-ExtraBold.woff2", weight: "800", style: "normal" },
  ],
  variable: "--font-cairo",
  display: "swap",
});

export const fontVariablesAr = cairo.variable;
