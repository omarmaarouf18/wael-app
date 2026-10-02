import assert from 'node:assert/strict';
import { afterEach, test } from 'node:test';

// Any use of browser storage or cookies fails the test (and is counted).
const storageHits = [];
function trap(name) {
  return new Proxy({}, {
    get(_t, prop) { storageHits.push(`${name}.${String(prop)}`); throw new Error(`${name} must not be used`); },
    set(_t, prop) { storageHits.push(`${name}.${String(prop)}`); throw new Error(`${name} must not be used`); },
  });
}
globalThis.localStorage = trap('localStorage');
globalThis.sessionStorage = trap('sessionStorage');
globalThis.indexedDB = trap('indexedDB');
globalThis.document = {
  get cookie() { storageHits.push('document.cookie'); throw new Error('cookies must not be used'); },
  set cookie(_v) { storageHits.push('document.cookie'); throw new Error('cookies must not be used'); },
};

const { api } = await import('../js/api.js');
const { clearSession, getAdminName, getToken, isSignedIn, normalizeToken, onSessionChange, signIn } = await import('../js/auth.js');
const { makeFetch } = await import('./fake-dom.mjs');

afterEach(() => {
  clearSession('signed_out');
  assert.deepEqual(storageHits, [], 'browser storage or cookies were touched');
});

function events() {
  const seen = [];
  const off = onSessionChange((e) => seen.push(e));
  return { seen, off };
}

test('normalizeToken trims and rejects non-strings', () => {
  assert.equal(normalizeToken('  abc \n'), 'abc');
  assert.equal(normalizeToken(''), '');
  assert.equal(normalizeToken(undefined), '');
  assert.equal(normalizeToken(42), '');
});

test('login flow: the token is accepted only once the server names the admin', async () => {
  globalThis.fetch = makeFetch({ 'GET /api/whoami': { body: { name: 'Wael El Saeed' } } });
  const { seen, off } = events();
  assert.equal(isSignedIn(), false);
  const res = await signIn('  my-admin-token  ', () => api.whoami());
  off();
  assert.deepEqual(res, { ok: true });
  assert.equal(getToken(), 'my-admin-token');
  assert.equal(getAdminName(), 'Wael El Saeed');
  assert.equal(isSignedIn(), true);
  assert.deepEqual(seen, [{ signedIn: true, reason: 'signed_in' }]);
  const sent = globalThis.fetch.calls[0];
  assert.equal(sent.init.headers['X-Admin-Token'], 'my-admin-token');
  assert.ok(!sent.url.includes('my-admin-token'));
});

test('login flow: an empty token is refused without any request', async () => {
  globalThis.fetch = makeFetch({});
  const res = await signIn('   ', () => api.whoami());
  assert.deepEqual(res, { ok: false, kind: 'empty' });
  assert.equal(globalThis.fetch.calls.length, 0);
  assert.equal(getToken(), '');
});

test('login flow: a 401 clears the token and reports unauthorized, without a logout event', async () => {
  globalThis.fetch = makeFetch({ 'GET /api/whoami': { status: 401, body: { code: 'unauthorized' } } });
  const { seen, off } = events();
  const res = await signIn('wrong-token', () => api.whoami());
  off();
  assert.deepEqual(res, { ok: false, kind: 'unauthorized' });
  assert.equal(getToken(), '');
  assert.equal(isSignedIn(), false);
  assert.deepEqual(seen, [], 'a failed sign-in is not an expired session');
});

test('login flow: server and network failures clear the token and report unavailable', async () => {
  for (const fetchSpec of [{ status: 503, body: {} }, new Error('network down')]) {
    globalThis.fetch = makeFetch({ 'GET /api/whoami': fetchSpec });
    const res = await signIn('tok', () => api.whoami());
    assert.deepEqual(res, { ok: false, kind: 'unavailable' });
    assert.equal(getToken(), '');
  }
});

test('login flow: a reply without a name does not sign in', async () => {
  for (const body of [{}, { name: '' }, { name: '  ' }, { name: 7 }]) {
    globalThis.fetch = makeFetch({ 'GET /api/whoami': { body } });
    const res = await signIn('tok', () => api.whoami());
    assert.deepEqual(res, { ok: false, kind: 'unavailable' });
    assert.equal(isSignedIn(), false);
    assert.equal(getToken(), '');
  }
});

test('401 anywhere logs out: token and name are cleared and listeners hear why', async () => {
  globalThis.fetch = makeFetch({
    'GET /api/whoami': { body: { name: 'Wael' } },
    'GET /api/accounts': { status: 401, body: { code: 'unauthorized' } },
  });
  await signIn('tok', () => api.whoami());
  const { seen, off } = events();
  const res = await api.get('/api/accounts');
  off();
  assert.deepEqual([res.ok, res.kind], [false, 'unauthorized']);
  assert.equal(getToken(), '');
  assert.equal(getAdminName(), '');
  assert.equal(isSignedIn(), false);
  assert.deepEqual(seen, [{ signedIn: false, reason: 'unauthorized' }]);

  // The next call sends nothing: there is no token any more.
  const before = globalThis.fetch.calls.length;
  const again = await api.get('/api/audit');
  assert.equal(again.kind, 'unauthorized');
  assert.equal(globalThis.fetch.calls.length, before);
});

test('two 401s at once produce one logout', async () => {
  globalThis.fetch = makeFetch({
    'GET /api/whoami': { body: { name: 'Wael' } },
    'GET /api/accounts': { status: 401, body: {} },
    'GET /api/audit': { status: 401, body: {} },
  });
  await signIn('tok', () => api.whoami());
  const { seen, off } = events();
  await Promise.all([api.get('/api/accounts'), api.get('/api/audit')]);
  off();
  assert.equal(seen.length, 1);
});

test('sign out clears the session and tells listeners', async () => {
  globalThis.fetch = makeFetch({ 'GET /api/whoami': { body: { name: 'Wael' } } });
  await signIn('tok', () => api.whoami());
  const { seen, off } = events();
  clearSession('signed_out');
  off();
  assert.deepEqual(seen, [{ signedIn: false, reason: 'signed_out' }]);
  assert.equal(getToken(), '');
});

test('clearing a session that does not exist is silent', () => {
  const { seen, off } = events();
  clearSession('signed_out');
  off();
  assert.deepEqual(seen, []);
});

test('the token is not reachable from the global object', async () => {
  globalThis.fetch = makeFetch({ 'GET /api/whoami': { body: { name: 'Wael' } } });
  await signIn('very-secret-token-value', () => api.whoami());
  for (const key of Object.getOwnPropertyNames(globalThis)) {
    let value;
    try { value = globalThis[key]; } catch { continue; }
    if (typeof value === 'string') assert.ok(!value.includes('very-secret-token-value'), key);
  }
});
