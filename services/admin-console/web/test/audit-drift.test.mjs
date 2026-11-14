// Drift guard (owner 2026-11-14): every action string the academy, auth and
// notification services write to their audit logs (and every student account
// event type) has an Arabic and English label and a sentence verb, sits in
// the audit tab's action filter, and every target type has a label. The Go
// sources are read as text, so a new action fails here until it is labelled.

import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

import { SOURCE_ACTIONS } from '../js/audit.js';
import { MESSAGES } from '../js/i18n.js';

const servicesDir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');

function goFiles(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...goFiles(p));
    else if (name.endsWith('.go') && !name.endsWith('_test.go')) out.push(p);
  }
  return out;
}

const SERVICES = { academy: 'academy-service', auth: 'auth-service', notifications: 'notification-service' };
const sources = Object.fromEntries(Object.entries(SERVICES).map(([k, dir]) => [k, goFiles(join(servicesDir, dir, 'internal')).map((f) => [f, readFileSync(f, 'utf8')])]));

const STR = '"([a-z][a-z0-9_]*)"';
// Every way an audit action is written today. A call with a non-literal action
// must be one of the known indirections below, or the guard fails.
const PATTERNS = [
  new RegExp(`writeAdminAudit\\([^,]+,\\s*[^,]+,\\s*${STR}`, 'g'),
  new RegExp(`writeBroadcastAudit\\([^,]+,\\s*[^,]+,\\s*${STR}`, 'g'),
  new RegExp(`\\bAction:\\s*${STR}`, 'g'),
  new RegExp(`setSubjectStatus\\([^)]*,\\s*${STR}\\)`, 'g'),
  new RegExp(`summaryAction\\s*:?=\\s*${STR}`, 'g'),
];
const TARGET_PATTERNS = [
  new RegExp(`writeAdminAudit\\([^,]+,\\s*[^,]+,\\s*[^,]+,\\s*${STR}`, 'g'),
  new RegExp(`\\bTargetType:\\s*${STR}`, 'g'),
];
const KNOWN_INDIRECT = new Set(['action', 'summaryAction']);

function collect(files, patterns) {
  const found = new Set();
  for (const [, src] of files) for (const re of patterns) for (const m of src.matchAll(re)) found.add(m[1]);
  return found;
}

test('the scan finds the audit writes it is meant to guard', () => {
  const academy = collect(sources.academy, PATTERNS);
  for (const a of ['iap_purchase', 'iap_package_create', 'subject_publish', 'request_bulk_accept', 'settings_updated']) assert.ok(academy.has(a), a);
  assert.ok(collect(sources.auth, PATTERNS).has('account_suspend'));
  assert.ok(collect(sources.notifications, PATTERNS).has('broadcast_send'));
});

test('no audit write passes an action through an unknown variable', () => {
  for (const files of Object.values(sources)) {
    for (const [file, src] of files) {
      for (const m of src.matchAll(/write(?:Admin|Broadcast)Audit\([^,]+,\s*[^,]+,\s*([A-Za-z_][\w.]*)\s*,/g)) {
        assert.ok(KNOWN_INDIRECT.has(m[1]), `${file}: audit action passed as ${m[1]}; extend the drift patterns`);
      }
      for (const m of src.matchAll(/\bAction:\s*([A-Za-z_][\w.]*)\s*,/g)) {
        assert.ok(KNOWN_INDIRECT.has(m[1]), `${file}: Action: ${m[1]}; extend the drift patterns`);
      }
    }
  }
});

test('every audit action and student event type has ar+en labels, a verb, and a filter entry', () => {
  const written = {
    academy: collect(sources.academy, PATTERNS),
    auth: collect(sources.auth, PATTERNS),
    notifications: collect(sources.notifications, PATTERNS),
    student: new Set([...sources.auth.flatMap(([, src]) => [...src.matchAll(/\bAccountEvent\w+\s*=\s*"([a-z_]+)"/g)].map((m) => m[1]))]),
  };
  assert.ok(written.student.has('login') && written.student.has('account_deleted'));
  for (const [source, actions] of Object.entries(written)) {
    for (const a of actions) {
      for (const lang of ['ar', 'en']) {
        assert.ok(`audit.action.${a}` in MESSAGES[lang], `${lang} audit.action.${a} (${source})`);
        assert.ok(`audit.verb.${a}` in MESSAGES[lang], `${lang} audit.verb.${a} (${source})`);
      }
      assert.ok(SOURCE_ACTIONS[source].includes(a), `${a} is missing from the ${source} action filter`);
    }
  }
});

test('every audit target type has ar+en labels', () => {
  const targets = new Set(Object.values(sources).flatMap((files) => [...collect(files, TARGET_PATTERNS)]));
  assert.ok(targets.has('iap_package') && targets.has('user') && targets.has('broadcast'));
  for (const tt of targets) for (const lang of ['ar', 'en']) assert.ok(`audit.target.${tt}` in MESSAGES[lang], `${lang} audit.target.${tt}`);
});
