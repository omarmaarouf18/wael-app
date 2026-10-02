import assert from 'node:assert/strict';
import { test } from 'node:test';

import { ACTIONS, REASON_MAX, buildActionRequest, cleanReason, validateReason } from '../js/account-dialog.js';
import { actionsFor, listQuery } from '../js/accounts.js';
import { actionLabelKey } from '../js/audit.js';

const account = { id: '0f8fad5b-d9cb-469f-a165-70867728950e', full_name: 'علي أحمد', email: 'ali@example.com' };

// These cases mirror internal/proxy TestSuspend_ReasonIsCleanedBeforeForwarding:
// the page and the server must clean a reason the same way.
test('cleanReason matches the server: control characters become spaces, then trim', () => {
  const cases = [
    ['line one\r\nline two', 'line one  line two'],
    ['a\nb', 'a b'],
    ['  padded  ', 'padded'],
    ['a\u0000b\u0007c', 'a b c'],
    ['a\tb', 'a b'],
    ['سبام', 'سبام'],
    ['say "hi" <b>x</b>', 'say "hi" <b>x</b>'],
    ['\r\n\r\n', ''],
    ['', ''],
  ];
  for (const [input, want] of cases) assert.equal(cleanReason(input), want, JSON.stringify(input));
  assert.equal(cleanReason(undefined), '');
  assert.equal(cleanReason(null), '');
});

test('validateReason requires 1 to 1000 characters after cleaning', () => {
  assert.deepEqual(validateReason(''), { ok: false, error: 'required', value: '' });
  assert.equal(validateReason('   ').error, 'required');
  assert.equal(validateReason('\r\n').error, 'required');
  assert.equal(validateReason('\u0000\u0007').error, 'required');
  assert.deepEqual(validateReason(' spam '), { ok: true, error: null, value: 'spam' });
  assert.equal(validateReason('x'.repeat(REASON_MAX)).ok, true);
  assert.equal(validateReason('x'.repeat(REASON_MAX + 1)).error, 'too_long');
});

test('validateReason counts characters, not UTF-16 units or bytes', () => {
  assert.equal(validateReason('س'.repeat(REASON_MAX)).ok, true);
  assert.equal(validateReason('س'.repeat(REASON_MAX + 1)).error, 'too_long');
  // Astral characters are two UTF-16 units but one character.
  assert.equal(validateReason('😀'.repeat(REASON_MAX)).ok, true);
  assert.equal(validateReason('😀'.repeat(REASON_MAX + 1)).error, 'too_long');
});

test('reason validation: suspend and delete need a reason, no request is built without one', () => {
  for (const action of ['suspend', 'delete']) {
    for (const bad of ['', '  ', '\n', undefined, 'x'.repeat(REASON_MAX + 1)]) {
      const req = buildActionRequest(action, account, bad);
      assert.ok(req.error, `${action} with ${JSON.stringify(bad)?.slice(0, 20)} must be refused`);
      assert.equal(req.path, undefined);
      assert.equal(req.body, undefined);
    }
  }
});

test('buildActionRequest: suspend and delete send id and the cleaned reason', () => {
  assert.deepEqual(buildActionRequest('suspend', account, ' spam\n'), {
    path: '/api/accounts/suspend',
    body: { id: account.id, reason: 'spam' },
  });
  assert.deepEqual(buildActionRequest('delete', account, 'fraud'), {
    path: '/api/accounts/delete',
    body: { id: account.id, reason: 'fraud' },
  });
});

test('buildActionRequest: reactivate sends the id only, whatever reason text is around', () => {
  assert.deepEqual(buildActionRequest('reactivate', account, 'ignored'), {
    path: '/api/accounts/reactivate',
    body: { id: account.id },
  });
  assert.deepEqual(buildActionRequest('reactivate', account, ''), {
    path: '/api/accounts/reactivate',
    body: { id: account.id },
  });
});

test('buildActionRequest refuses unknown actions and accounts without an id', () => {
  assert.ok(buildActionRequest('promote', account, 'x').error);
  assert.ok(buildActionRequest('suspend', null, 'x').error);
  assert.ok(buildActionRequest('suspend', {}, 'x').error);
  assert.ok(buildActionRequest('suspend', { id: '' }, 'x').error);
  assert.ok(buildActionRequest('suspend', { id: 5 }, 'x').error);
});

test('every action targets one of the console routes the server allows', () => {
  assert.deepEqual(Object.keys(ACTIONS).sort(), ['delete', 'reactivate', 'suspend']);
  for (const spec of Object.values(ACTIONS)) assert.match(spec.path, /^\/api\/accounts\/(suspend|reactivate|delete)$/);
  assert.equal(ACTIONS.reactivate.needsReason, false);
  assert.equal(ACTIONS.suspend.needsReason, true);
  assert.equal(ACTIONS.delete.needsReason, true);
});

test('actionsFor offers only what the account status allows', () => {
  assert.deepEqual(actionsFor('active'), ['suspend', 'delete']);
  assert.deepEqual(actionsFor('suspended'), ['reactivate', 'delete']);
  assert.deepEqual(actionsFor('deleted'), []);
  assert.deepEqual(actionsFor(''), []);
  assert.deepEqual(actionsFor(undefined), []);
});

test('listQuery leaves out empty filters', () => {
  assert.deepEqual(listQuery({ search: '  ', status: '', page: 1, limit: 15 }), { page: 1, limit: 15 });
  assert.deepEqual(listQuery({ search: ' علي ', status: 'suspended', page: 3, limit: 15 }), {
    page: 3,
    limit: 15,
    search: 'علي',
    status: 'suspended',
  });
});

test('audit action labels exist only for known actions', () => {
  assert.equal(actionLabelKey('account_suspend'), 'audit.action.account_suspend');
  assert.equal(actionLabelKey('account_reactivate'), 'audit.action.account_reactivate');
  assert.equal(actionLabelKey('account_delete'), 'audit.action.account_delete');
  assert.equal(actionLabelKey('subject_publish'), null);
  assert.equal(actionLabelKey(undefined), null);
});
