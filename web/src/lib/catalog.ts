// Server-side catalog fetch (ADR-0014 W2b). Next.js reads the public catalog
// on the server, at build time and on revalidation, never in the browser: no
// CORS, and Vercel Previews work too.
//
// One request feeds every catalog page (owner decision 2026-11-04, ADR-0014
// "Amendment 2026-11-04 (2)" item 4): /subjects, generateStaticParams and
// each /subjects/[id] read the same URL with the same revalidate value, so
// Next.js's data cache answers them from one response per revalidation
// window, and React's cache() dedupes within one render.

import { cache } from "react";

import { parseCatalog, type Catalog } from "./catalog-data";
import { serverApiOrigin } from "./config";

/** Seconds between catalog refreshes; keep equal to `revalidate` in the pages. */
export const CATALOG_REVALIDATE_SECONDS = 300;

export const CATALOG_PATH = "/api/v1/academy/public/catalog";

export type CatalogResult = { ok: true; catalog: Catalog } | { ok: false };

function isBuildPhase(): boolean {
  return process.env.NEXT_PHASE === "phase-production-build";
}

/**
 * Failure handling. During `next build` a failure renders the "unavailable"
 * state, so a build never depends on the API being up (CI has no API; the
 * production API gets the route only after the owner's deploy). At run time
 * (revalidation) a failure throws, so Next.js keeps serving the last good
 * page instead of replacing it with "unavailable".
 */
function failed(reason: string): CatalogResult {
  if (!isBuildPhase()) {
    throw new Error(`catalog: ${reason}`);
  }
  console.error(`[catalog] ${reason}; building the catalog pages as unavailable`);
  return { ok: false };
}

export const getCatalog = cache(async (): Promise<CatalogResult> => {
  const origin = serverApiOrigin();
  if (origin === null) {
    // No API configured (CI and local builds): nothing to fetch.
    return { ok: false };
  }
  let res: Response;
  try {
    res = await fetch(`${origin}${CATALOG_PATH}`, {
      headers: { Accept: "application/json" },
      redirect: "error",
      next: { revalidate: CATALOG_REVALIDATE_SECONDS },
    });
  } catch {
    return failed("API unreachable");
  }
  if (!res.ok) {
    return failed(`API answered ${res.status}`);
  }
  let body: unknown;
  try {
    body = await res.json();
  } catch {
    return failed("API answered a body that is not JSON");
  }
  const catalog = parseCatalog(body);
  if (catalog === null) {
    return failed("API answered an unexpected catalog shape");
  }
  return { ok: true, catalog };
});
