// Site configuration read from the environment. NEXT_PUBLIC_* values are
// public (inlined into the browser bundle at build time); API_BASE_URL is
// server-only (read by Next.js on the server, never in a browser bundle).
// Secrets never belong in this site.

export const DEFAULT_API_BASE_URL = "https://api.elmetracademy.app";

const LOCAL_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"]);

/**
 * Parses an API base URL and returns its origin. Fails closed: anything that
 * is not an https origin (or http on a local host, for `next dev` against a
 * local stack) is refused, so a typo can never point the site, or its CSP,
 * at an unintended host.
 */
export function parseApiOrigin(raw: string | undefined, name = "NEXT_PUBLIC_API_BASE_URL"): string {
  const value = raw === undefined || raw.trim() === "" ? DEFAULT_API_BASE_URL : raw.trim();
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`${name} is not a URL: ${JSON.stringify(value)}`);
  }
  const local = LOCAL_HOSTS.has(url.hostname);
  if (url.protocol !== "https:" && !(url.protocol === "http:" && local)) {
    throw new Error(`${name} must use https (http only for localhost)`);
  }
  if (url.username !== "" || url.password !== "") {
    throw new Error(`${name} must not carry credentials`);
  }
  if ((url.pathname !== "/" && url.pathname !== "") || url.search !== "" || url.hash !== "") {
    throw new Error(`${name} must be an origin only (no path, query or fragment)`);
  }
  return url.origin;
}

/** Origin of the shared backend (api-gateway). */
export const API_ORIGIN = parseApiOrigin(process.env.NEXT_PUBLIC_API_BASE_URL);

/**
 * Server-side API origin for the catalog (API_BASE_URL, ADR-0014 W2b), or
 * null when it is not set. Unset is allowed off Vercel (CI, local builds):
 * the catalog pages then show "unavailable" and make no request. On Vercel
 * next.config.ts refuses to build without it. Same rules as
 * NEXT_PUBLIC_API_BASE_URL when set.
 */
export function serverApiOrigin(raw: string | undefined = process.env.API_BASE_URL): string | null {
  if (raw === undefined || raw.trim() === "") return null;
  return parseApiOrigin(raw, "API_BASE_URL");
}
