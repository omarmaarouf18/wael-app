import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createApi, kindForStatus } from '../js/api.js';
import { jsonResponse, makeFetch } from './fake-dom.mjs';

function setup(routes, { token = 'tok_123' } = {}) {
  const fetchFn = makeFetch(routes);
  const unauthorized = [];
  const api = createApi({
    fetchFn,
    getToken: () => token,
    onUnauthorized: () => unauthorized.push(true),
  });
  return { api, fetchFn, unauthorized };
}

test('kindForStatus maps the statuses the console shows banners for', () => {
  const expected = {
    0: 'unavailable',
    400: 'bad_request',
    401: 'unauthorized',
    403: 'forbidden',
    404: 'not_found',
    409: 'conflict',
    429: 'rate_limited',
    500: 'unavailable',
    502: 'unavailable',
    503: 'unavailable',
    418: 'unavailable',
  };
  for (const [status, kind] of Object.entries(expected)) {
    assert.equal(kindForStatus(Number(status)), kind, `status ${status}`);
  }
});

test('request sends the token in X-Admin-Token only, never in the URL', async () => {
  const { api, fetchFn } = setup({ 'GET /api/accounts': { body: { items: [] } } });
  const res = await api.get('/api/accounts', { search: 'علي', status: 'active', page: 2, limit: 15, empty: '', nothing: undefined });
  assert.equal(res.ok, true);
  const call = fetchFn.calls[0];
  assert.equal(call.init.headers['X-Admin-Token'], 'tok_123');
  assert.ok(!call.url.includes('tok_123'), 'token must not appear in the URL');
  assert.equal(call.url, '/api/accounts?search=%D8%B9%D9%84%D9%8A&status=active&page=2&limit=15');
  assert.equal(call.init.credentials, 'omit');
  assert.equal(call.init.cache, 'no-store');
  assert.equal(call.init.referrerPolicy, 'no-referrer');
});

test('POST sends a JSON body with a content type', async () => {
  const { api, fetchFn } = setup({ 'POST /api/accounts/suspend': { body: { status: 'ok' } } });
  const res = await api.post('/api/accounts/suspend', { id: 'x', reason: 'r' });
  assert.equal(res.ok, true);
  const { init } = fetchFn.calls[0];
  assert.equal(init.method, 'POST');
  assert.equal(init.headers['Content-Type'], 'application/json');
  assert.deepEqual(JSON.parse(init.body), { id: 'x', reason: 'r' });
});

test('without a token nothing is sent and the session is reported unauthorized', async () => {
  const { api, fetchFn, unauthorized } = setup({}, { token: '' });
  const res = await api.get('/api/audit');
  assert.deepEqual([res.ok, res.kind, res.status], [false, 'unauthorized', 401]);
  assert.equal(fetchFn.calls.length, 0);
  assert.equal(unauthorized.length, 1);
});

test('a 401 reports unauthorized exactly once and returns the unauthorized kind', async () => {
  const { api, unauthorized } = setup({ 'GET /api/audit': { status: 401, body: { code: 'unauthorized' } } });
  const res = await api.get('/api/audit');
  assert.deepEqual([res.ok, res.kind], [false, 'unauthorized']);
  assert.equal(unauthorized.length, 1);
});

test('other failures do not end the session', async () => {
  for (const status of [400, 403, 404, 409, 429, 500, 503]) {
    const { api, unauthorized } = setup({ 'GET /api/audit': { status, body: { code: 'x' } } });
    const res = await api.get('/api/audit');
    assert.equal(res.ok, false);
    assert.equal(res.kind, kindForStatus(status));
    assert.equal(unauthorized.length, 0, `status ${status} must not log out`);
  }
});

test('error responses never expose server text', async () => {
  const { api } = setup({
    'GET /api/audit': { status: 409, body: { error: 'SECRET internal detail', code: 'conflict', request_id: 'r1' } },
  });
  const res = await api.get('/api/audit');
  assert.equal(res.data, null);
  assert.ok(!JSON.stringify(res).includes('SECRET'));
});

test('network failures become the unavailable kind without throwing', async () => {
  const { api, unauthorized } = setup({ 'GET /api/audit': new Error('connect ECONNREFUSED 10.0.0.1:443') });
  const res = await api.get('/api/audit');
  assert.deepEqual([res.ok, res.status, res.kind, res.data], [false, 0, 'unavailable', null]);
  assert.ok(!JSON.stringify(res).includes('ECONNREFUSED'));
  assert.equal(unauthorized.length, 0);
});

test('a success response that is not JSON is treated as unavailable', async () => {
  const api = createApi({
    fetchFn: async () => ({ status: 200, ok: true, json: async () => { throw new SyntaxError('Unexpected token <'); } }),
    getToken: () => 't',
    onUnauthorized() {},
  });
  const res = await api.get('/api/audit');
  assert.deepEqual([res.ok, res.kind], [false, 'unavailable']);
});

test('whoami is GET /api/whoami', async () => {
  const { api, fetchFn } = setup({ 'GET /api/whoami': { body: { name: 'Wael' } } });
  const res = await api.whoami();
  assert.equal(res.data.name, 'Wael');
  assert.equal(fetchFn.calls[0].path, '/api/whoami');
});

test('jsonResponse helper sanity', () => {
  assert.equal(jsonResponse(204, {}).ok, true);
  assert.equal(jsonResponse(500, {}).ok, false);
});
