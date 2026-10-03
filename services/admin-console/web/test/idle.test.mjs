import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createIdleLock } from '../js/idle.js';
import { clearSession, signIn } from '../js/auth.js';
import { formatNumber } from '../js/i18n.js';
import { FakeDocument, FakeElement } from './fake-dom.mjs';

function makeMockWindow() {
  let now = 1000;
  const timers = new Map();
  const intervals = new Map();
  const listeners = new Map();
  let nextId = 1;

  return {
    now() {
      return now;
    },
    addEventListener(type, fn) {
      if (!listeners.has(type)) listeners.set(type, []);
      listeners.get(type).push(fn);
    },
    removeEventListener(type, fn) {
      const list = listeners.get(type) ?? [];
      const idx = list.indexOf(fn);
      if (idx >= 0) list.splice(idx, 1);
    },
    fire(type, event = {}) {
      for (const fn of listeners.get(type) ?? []) fn(event);
    },
    setTimeout(fn, delay) {
      const id = nextId++;
      timers.set(id, { fn, at: now + delay });
      return id;
    },
    clearTimeout(id) {
      timers.delete(id);
    },
    setInterval(fn, delay) {
      const id = nextId++;
      intervals.set(id, { fn, delay, at: now + delay });
      return id;
    },
    clearInterval(id) {
      intervals.delete(id);
    },
    advanceTime(ms) {
      now += ms;
      // Fire any expired timers
      for (const [id, t] of [...timers.entries()]) {
        if (t.at <= now) {
          timers.delete(id);
          t.fn();
        }
      }
      // Fire intervals
      for (const [, iv] of [...intervals.entries()]) {
        while (iv.at <= now) {
          iv.at += iv.delay;
          iv.fn();
        }
      }
    },
  };
}

test('idle lock warns at 19 minutes and locks at 20 minutes', async () => {
  const doc = new FakeDocument();
  const dialog = doc.getElementById('idle-dialog');
  const message = doc.getElementById('idle-message');
  const stayBtn = doc.getElementById('idle-stay');
  const win = makeMockWindow();

  await signIn('test-token', async () => ({ ok: true, data: { name: 'Admin' } }));

  let locked = false;
  const lock = createIdleLock({
    doc,
    win,
    idleMs: 19 * 60 * 1000,
    warnMs: 60 * 1000,
    onLock: () => {
      locked = true;
    },
  });

  lock.start();

  // At 18 minutes: no warning yet
  win.advanceTime(18 * 60 * 1000);
  assert.equal(lock.isWarning(), false);
  assert.equal(dialog.open, false);
  assert.equal(locked, false);

  // At 19 minutes: warning triggered
  win.advanceTime(1 * 60 * 1000);
  assert.equal(lock.isWarning(), true);
  assert.equal(dialog.open, true);
  assert.ok(message.textContent.includes(formatNumber(60)));

  // Advance 30 seconds into warning
  win.advanceTime(30 * 1000);
  assert.equal(lock.isWarning(), true);
  assert.equal(locked, false);

  // Advance remaining 30 seconds -> locks
  win.advanceTime(30 * 1000);
  assert.equal(locked, true);

  clearSession();
});

test('activity during warning dismisses warning and resets idle timer', async () => {
  const doc = new FakeDocument();
  const dialog = doc.getElementById('idle-dialog');
  const win = makeMockWindow();

  await signIn('test-token', async () => ({ ok: true, data: { name: 'Admin' } }));

  let locked = false;
  const lock = createIdleLock({
    doc,
    win,
    idleMs: 1000,
    warnMs: 500,
    onLock: () => {
      locked = true;
    },
  });

  lock.start();

  // Advance to warning
  win.advanceTime(1000);
  assert.equal(lock.isWarning(), true);
  assert.equal(dialog.open, true);

  // User moves mouse or presses key during warning
  win.advanceTime(200);
  win.fire('mousemove');
  assert.equal(lock.isWarning(), false);
  assert.equal(dialog.open, false);
  assert.equal(locked, false);

  // Should not lock after previous warn timeout
  win.advanceTime(500);
  assert.equal(locked, false);

  lock.stop();
  clearSession();
});

test('activity before warning resets idle timer', async () => {
  const doc = new FakeDocument();
  const win = makeMockWindow();

  await signIn('test-token', async () => ({ ok: true, data: { name: 'Admin' } }));

  const lock = createIdleLock({
    doc,
    win,
    idleMs: 1000,
    warnMs: 500,
  });

  lock.start();

  // Activity at 600ms
  win.advanceTime(600);
  win.fire('keydown');

  // At 1100ms (500ms after activity): still not warning
  win.advanceTime(500);
  assert.equal(lock.isWarning(), false);

  // At 1700ms (1100ms after activity): warning triggers
  win.advanceTime(600);
  assert.equal(lock.isWarning(), true);

  lock.stop();
  clearSession();
});
