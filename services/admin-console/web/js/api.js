// All calls to the console's own /api/* routes. The admin token is attached
// as X-Admin-Token; any 401 ends the session. Failures are reduced to a small
// set of kinds so the page can show its own text: raw error text from the
// server is never displayed.

import { clearSession, getToken } from './auth.js';

/** Maps an HTTP status (0 for a network failure) to a message kind. */
export function kindForStatus(status) {
  switch (status) {
    case 400:
      return 'bad_request';
    case 401:
      return 'unauthorized';
    case 403:
      return 'forbidden';
    case 404:
      return 'not_found';
    case 409:
      return 'conflict';
    case 429:
      return 'rate_limited';
    default:
      return 'unavailable';
  }
}

export const RETRY_AFTER_MAX = 3600;

/**
 * Reads a Retry-After header as whole seconds. Only a plain number of seconds
 * is accepted (the console proxy relays digits only); a date, a negative or
 * zero value, or junk gives null. Longer waits are capped at an hour.
 */
export function parseRetryAfter(raw) {
  if (typeof raw !== 'string' && typeof raw !== 'number') return null;
  const text = String(raw).trim();
  if (!/^\d{1,7}$/.test(text)) return null;
  const seconds = Number(text);
  return seconds >= 1 ? Math.min(seconds, RETRY_AFTER_MAX) : null;
}

function withQuery(path, query) {
  const params = new URLSearchParams();
  for (const [name, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== null && value !== '') params.set(name, String(value));
  }
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}

/**
 * Reads the server's error code from a failed response. Only a plain
 * lowercase token is kept; anything else (including the human message) is
 * dropped so raw server text never reaches the page.
 */
async function errorCode(res) {
  try {
    const body = await res.json();
    const code = body && typeof body.code === 'string' ? body.code : null;
    return code && /^[a-z_]{1,64}$/.test(code) ? code : null;
  } catch {
    return null;
  }
}

/**
 * createApi builds the client. All three collaborators are injected so the
 * flows can be tested without a browser.
 */
export function createApi({ fetchFn, getToken: tokenOf, onUnauthorized }) {
  /**
   * Resolves { ok, status, kind, code, data }. data is set only when ok. code
   * is the server's error code when it is a plain token (a-z and _); raw
   * error text is never kept. A 429 also carries retryAfter (seconds, or null
   * when the server sent no usable Retry-After). Never throws on HTTP or
   * network errors.
   */
  async function request(method, path, { query, body } = {}) {
    const token = tokenOf();
    if (!token) {
      onUnauthorized();
      return { ok: false, status: 401, kind: 'unauthorized', code: 'unauthorized', data: null };
    }
    const headers = { Accept: 'application/json', 'X-Admin-Token': token };
    const init = {
      method,
      headers,
      credentials: 'omit',
      cache: 'no-store',
      referrerPolicy: 'no-referrer',
    };
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(body);
    }

    let res;
    try {
      res = await fetchFn(withQuery(path, query), init);
    } catch {
      return { ok: false, status: 0, kind: 'unavailable', code: null, data: null };
    }
    if (res.status === 401) {
      onUnauthorized();
      return { ok: false, status: 401, kind: 'unauthorized', code: 'unauthorized', data: null };
    }
    if (!res.ok) {
      const failure = { ok: false, status: res.status, kind: kindForStatus(res.status), code: await errorCode(res), data: null };
      if (res.status === 429) failure.retryAfter = parseRetryAfter(res.headers?.get?.('Retry-After'));
      return failure;
    }
    let data;
    try {
      data = await res.json();
    } catch {
      return { ok: false, status: res.status, kind: 'unavailable', code: null, data: null };
    }
    return { ok: true, status: res.status, kind: null, code: null, data };
  }

  return {
    request,
    get: (path, query) => request('GET', path, { query }),
    post: (path, body) => request('POST', path, { body }),
    whoami: () => request('GET', '/api/whoami'),
  };
}

export const api = createApi({
  fetchFn: (...args) => globalThis.fetch(...args),
  getToken,
  onUnauthorized: () => clearSession('unauthorized'),
});
