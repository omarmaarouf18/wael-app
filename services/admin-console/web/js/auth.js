// The admin token lives in this module variable and nowhere else: not in
// localStorage, sessionStorage, a cookie or the URL (ADR-0008 section 4).
// Reloading the page therefore means signing in again.

let token = '';
let adminName = '';
const listeners = new Set();

export function getToken() {
  return token;
}

export function getAdminName() {
  return adminName;
}

export function isSignedIn() {
  return token !== '' && adminName !== '';
}

/** Subscribes to session changes; returns an unsubscribe function. */
export function onSessionChange(cb) {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

function notify(event) {
  for (const cb of listeners) cb(event);
}

export function normalizeToken(raw) {
  return typeof raw === 'string' ? raw.trim() : '';
}

/**
 * Ends the session. Listeners hear about it only if the admin was signed in,
 * so a failed sign-in attempt does not look like an expired session.
 */
export function clearSession(reason = 'signed_out') {
  const wasSignedIn = isSignedIn();
  token = '';
  adminName = '';
  if (wasSignedIn) notify({ signedIn: false, reason });
}

/**
 * Signs in with a candidate token. loadIdentity() must call GET /api/whoami
 * (which reads getToken()) and resolve to the api result. The token counts as
 * signed in only once the server has named the admin.
 *
 * Resolves { ok: true } or { ok: false, kind } where kind is 'empty' or an api
 * error kind such as 'unauthorized' or 'unavailable'. A rate-limited answer
 * also carries retryAfter (seconds).
 */
export async function signIn(rawToken, loadIdentity) {
  const candidate = normalizeToken(rawToken);
  if (!candidate) return { ok: false, kind: 'empty' };
  token = candidate;
  adminName = '';
  const res = await loadIdentity();
  if (token !== candidate) return { ok: false, kind: 'unauthorized' };
  const name = res.ok && res.data && typeof res.data.name === 'string' ? res.data.name.trim() : '';
  if (!name) {
    token = '';
    if (res.ok) return { ok: false, kind: 'unavailable' };
    return res.retryAfter ? { ok: false, kind: res.kind, retryAfter: res.retryAfter } : { ok: false, kind: res.kind };
  }
  adminName = name;
  notify({ signedIn: true, reason: 'signed_in' });
  return { ok: true };
}
