// Build-time switches for the static pages.
//
// TODO blocks (UI/UX brief 2026-11-07: "visible TODO block in non-production
// builds only") mark copy or photos the owner has not given yet. They are
// hidden only in Vercel's production build (VERCEL_ENV=production) and shown
// everywhere else: Vercel previews, CI and local builds. Showing a TODO is not
// a security relaxation, so an unknown value shows them rather than hiding
// them; production on Vercel always sets VERCEL_ENV. Read at build time: the
// pages that use it are static.
export function showTodoBlocks(): boolean {
  return process.env.VERCEL_ENV !== "production";
}
