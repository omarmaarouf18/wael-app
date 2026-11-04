// Site configuration read from the environment. Every value here is public
// (NEXT_PUBLIC_*, inlined into the browser bundle at build time); secrets
// never belong in this site.

export const DEFAULT_API_BASE_URL = "https://api.elmetracademy.app";

const LOCAL_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"]);

/**
 * Parses an API base URL and returns its origin. Fails closed: anything that
 * is not an https origin (or http on a local host, for `next dev` against a
 * local stack) is refused, so a typo can never point the site, or its CSP,
 * at an unintended host.
 */
export function parseApiOrigin(raw: string | undefined): string {
  const value = raw === undefined || raw.trim() === "" ? DEFAULT_API_BASE_URL : raw.trim();
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`NEXT_PUBLIC_API_BASE_URL is not a URL: ${JSON.stringify(value)}`);
  }
  const local = LOCAL_HOSTS.has(url.hostname);
  if (url.protocol !== "https:" && !(url.protocol === "http:" && local)) {
    throw new Error("NEXT_PUBLIC_API_BASE_URL must use https (http only for localhost)");
  }
  if (url.username !== "" || url.password !== "") {
    throw new Error("NEXT_PUBLIC_API_BASE_URL must not carry credentials");
  }
  if ((url.pathname !== "/" && url.pathname !== "") || url.search !== "" || url.hash !== "") {
    throw new Error("NEXT_PUBLIC_API_BASE_URL must be an origin only (no path, query or fragment)");
  }
  return url.origin;
}

/** Origin of the shared backend (api-gateway). */
export const API_ORIGIN = parseApiOrigin(process.env.NEXT_PUBLIC_API_BASE_URL);
