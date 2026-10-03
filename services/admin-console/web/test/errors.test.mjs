// Error-code coverage: every code the academy admin handlers can return
// (the checked-in list in internal/proxy/catalog_codes_test.go, extracted
// from the handlers with the grep command quoted there) needs an ar and en
// message, plus the console's own kinds. Fails closed: an unknown code falls
// back to the status kind, never to raw server text.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { MESSAGES, errorText } from '../js/i18n.js';

// Keep in sync with academyErrorCodes in
// services/admin-console/internal/proxy/catalog_codes_test.go.
const ACADEMY_CODES = [
  'unauthorized',
  'locked_out',
  'service_unavailable',
  'invalid_json',
  'method_not_allowed',
  'not_found',
  'invalid_level_id',
  'invalid_study_type',
  'invalid_name',
  'level_not_found',
  'level_not_deletable',
  'level_has_subjects',
  'invalid_subject_id',
  'subject_not_found',
  'invalid_title',
  'invalid_description',
  'invalid_term',
  'invalid_price',
  'invalid_expires_at',
  'invalid_published',
  'subject_has_no_videos',
  'invalid_video_id',
  'video_not_found',
  'invalid_youtube_id',
  'invalid_duration_seconds',
  'invalid_video_order',
  'last_video_of_published_subject',
];

test('every academy admin code has an ar and an en message', () => {
  assert.equal(ACADEMY_CODES.length, 27);
  for (const code of ACADEMY_CODES) {
    assert.ok(MESSAGES.ar[`err.${code}`], `ar err.${code}`);
    assert.ok(MESSAGES.en[`err.${code}`], `en err.${code}`);
  }
});

test('the console kinds still resolve in both languages', () => {
  for (const kind of ['unauthorized', 'forbidden', 'not_found', 'conflict', 'rate_limited', 'bad_request', 'unavailable']) {
    assert.ok(MESSAGES.ar[`err.${kind}`], `ar err.${kind}`);
    assert.ok(MESSAGES.en[`err.${kind}`], `en err.${kind}`);
  }
});

test('errorText prefers the code, falls back to the kind, never to raw text', () => {
  assert.equal(errorText('level_not_found', 'not_found'), MESSAGES.ar['err.level_not_found']);
  assert.equal(errorText('level_has_subjects', 'conflict'), MESSAGES.ar['err.level_has_subjects']);
  assert.equal(errorText('something_new', 'conflict'), MESSAGES.ar['err.conflict']);
  assert.equal(errorText(null, 'unavailable'), MESSAGES.ar['err.unavailable']);
  assert.equal(errorText('', 'bad_request'), MESSAGES.ar['err.bad_request']);
  assert.equal(errorText(42, 'not_found'), MESSAGES.ar['err.not_found']);
  // A hostile code shaped like a message key cannot escape the map.
  assert.equal(errorText('constructor', 'unavailable'), MESSAGES.ar['err.unavailable']);
});
