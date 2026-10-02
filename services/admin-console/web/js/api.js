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

function withQuery(path, query) {
  const params = new URLSearchParams();
  for (const [name, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== null && value !== '') params.set(name, String(value));
  }
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}

/**
 * createApi builds the client. All three collaborators are injected so the
 * flows can be tested without a browser.
 */
export function createApi({ fetchFn, getToken: tokenOf, onUnauthorized }) {
  /** Resolves { ok, status, kind, data }. data is set only when ok. Never throws on HTTP or network errors. */
  async function request(method, path, { query, body } = {}) {
    const token = tokenOf();
    if (!token) {
      onUnauthorized();
      return { ok: false, status: 401, kind: 'unauthorized', data: null };
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
      return { ok: false, status: 0, kind: 'unavailable', data: null };
    }
    if (res.status === 401) {
      onUnauthorized();
      return { ok: false, status: 401, kind: 'unauthorized', data: null };
    }
    if (!res.ok) {
      return { ok: false, status: res.status, kind: kindForStatus(res.status), data: null };
    }
    let data;
    try {
      data = await res.json();
    } catch {
      return { ok: false, status: res.status, kind: 'unavailable', data: null };
    }
    return { ok: true, status: res.status, kind: null, data };
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
