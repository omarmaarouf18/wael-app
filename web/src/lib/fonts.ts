import localFont from "next/font/local";

// Same families as the app (docs/frontend/DESIGN_SYSTEM.md "AppTypography"),
// copied from frontend/assets/fonts/ (SIL OFL 1.1, licences beside them).
// Served from this site; nothing is fetched from Google.

export const cairo = localFont({
  src: [
    { path: "../fonts/Cairo-Regular.ttf", weight: "400", style: "normal" },
    { path: "../fonts/Cairo-Bold.ttf", weight: "700", style: "normal" },
    { path: "../fonts/Cairo-ExtraBold.ttf", weight: "800", style: "normal" },
  ],
  variable: "--font-cairo",
  display: "swap",
});

export const syne = localFont({
  src: [
    { path: "../fonts/Syne-Bold.ttf", weight: "700", style: "normal" },
    { path: "../fonts/Syne-ExtraBold.ttf", weight: "800", style: "normal" },
  ],
  variable: "--font-syne",
  display: "swap",
});

export const plusJakartaSans = localFont({
  src: [
    { path: "../fonts/PlusJakartaSans-Regular.ttf", weight: "400", style: "normal" },
    { path: "../fonts/PlusJakartaSans-SemiBold.ttf", weight: "600", style: "normal" },
  ],
  variable: "--font-plus-jakarta-sans",
  display: "swap",
});

export const fontVariables = [cairo.variable, syne.variable, plusJakartaSans.variable].join(" ");
