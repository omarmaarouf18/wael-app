// Idle lock (UI/UX audit A1): after 20 minutes with no user activity, wipe
// the admin token from memory and return to the login screen. A warning dialog
// is displayed for the final 60 seconds with a live countdown.

import { clearSession, isSignedIn } from './auth.js';
import { formatNumber, t } from './i18n.js';

export const DEFAULT_IDLE_MS = 19 * 60 * 1000; // 19 minutes
export const DEFAULT_WARN_MS = 60 * 1000;      // 60 seconds (total 20 min)

const ACTIVITY_EVENTS = ['mousemove', 'mousedown', 'keydown', 'touchstart', 'scroll'];

export function createIdleLock({
  doc = document,
  win = window,
  idleMs = DEFAULT_IDLE_MS,
  warnMs = DEFAULT_WARN_MS,
  now = () => (win && typeof win.now === 'function' ? win.now() : Date.now()),
  onLock,
} = {}) {
  const dialog = doc.getElementById('idle-dialog');
  const message = doc.getElementById('idle-message');
  const stayBtn = doc.getElementById('idle-stay');

  const setTimeoutFn = (win && typeof win.setTimeout === 'function') ? win.setTimeout.bind(win) : setTimeout;
  const clearTimeoutFn = (win && typeof win.clearTimeout === 'function') ? win.clearTimeout.bind(win) : clearTimeout;
  const setIntervalFn = (win && typeof win.setInterval === 'function') ? win.setInterval.bind(win) : setInterval;
  const clearIntervalFn = (win && typeof win.clearInterval === 'function') ? win.clearInterval.bind(win) : clearInterval;

  let idleTimer = null;
  let warnTimer = null;
  let tickInterval = null;
  let warningActive = false;
  let secondsLeft = 0;
  let lastActivity = 0;

  function updateWarningText() {
    if (message) {
      message.textContent = t('idle.warnBody', { seconds: formatNumber(secondsLeft) });
    }
  }

  function showWarning() {
    warningActive = true;
    secondsLeft = Math.ceil(warnMs / 1000);
    updateWarningText();
    if (dialog && typeof dialog.showModal === 'function') {
      try {
        dialog.showModal();
      } catch {
        // In case dialog is already open
      }
    }
    tickInterval = setIntervalFn(() => {
      secondsLeft -= 1;
      if (secondsLeft <= 0) {
        lock();
      } else {
        updateWarningText();
      }
    }, 1000);
    if (tickInterval && typeof tickInterval.unref === 'function') {
      tickInterval.unref();
    }

    warnTimer = setTimeoutFn(() => {
      lock();
    }, warnMs);
    if (warnTimer && typeof warnTimer.unref === 'function') {
      warnTimer.unref();
    }
  }

  function dismissWarning() {
    if (!warningActive) return;
    warningActive = false;
    if (tickInterval) {
      win.clearInterval(tickInterval);
      tickInterval = null;
    }
    if (warnTimer) {
      win.clearTimeout(warnTimer);
      warnTimer = null;
    }
    if (dialog && typeof dialog.close === 'function') {
      try {
        dialog.close();
      } catch {
        // Ignored
      }
    }
    startIdleTimer();
  }

  function lock() {
    stop();
    if (dialog && typeof dialog.close === 'function') {
      try {
        dialog.close();
      } catch {
        // Ignored
      }
    }
    if (typeof onLock === 'function') {
      onLock();
    } else {
      clearSession('idle_lock');
    }
  }

  function startIdleTimer() {
    if (idleTimer) {
      clearTimeoutFn(idleTimer);
    }
    idleTimer = setTimeoutFn(() => {
      showWarning();
    }, idleMs);
    if (idleTimer && typeof idleTimer.unref === 'function') {
      idleTimer.unref();
    }
  }

  function onActivity() {
    if (!isSignedIn()) return;
    const currentTime = now();
    // Throttle activity handling to once per 500ms
    if (currentTime - lastActivity < 500) return;
    lastActivity = currentTime;

    if (warningActive) {
      dismissWarning();
    } else {
      startIdleTimer();
    }
  }

  function start() {
    stop();
    lastActivity = now();
    if (win && typeof win.addEventListener === 'function') {
      for (const evt of ACTIVITY_EVENTS) {
        win.addEventListener(evt, onActivity, { passive: true });
      }
    }
    if (stayBtn) {
      stayBtn.addEventListener('click', dismissWarning);
    }
    startIdleTimer();
  }

  function stop() {
    warningActive = false;
    if (idleTimer) {
      clearTimeoutFn(idleTimer);
      idleTimer = null;
    }
    if (warnTimer) {
      clearTimeoutFn(warnTimer);
      warnTimer = null;
    }
    if (tickInterval) {
      clearIntervalFn(tickInterval);
      tickInterval = null;
    }
    if (win && typeof win.removeEventListener === 'function') {
      for (const evt of ACTIVITY_EVENTS) {
        win.removeEventListener(evt, onActivity);
      }
    }
  }

  return {
    start,
    stop,
    dismissWarning,
    lock,
    isWarning: () => warningActive,
  };
}
