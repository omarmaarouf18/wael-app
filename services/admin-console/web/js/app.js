// Entry point: wires the login form, the language toggle, the tabs and the
// tab modules. Everything about a session (token, name) lives in auth.js.

import { mountAccounts } from './accounts.js';
import { api } from './api.js';
import { mountAudit } from './audit.js';
import { mountCatalog } from './catalog.js';
import { mountFiles } from './files.js';
import { mountRequests } from './requests.js';
import { mountSettings } from './settings.js';
import { createIdleLock } from './idle.js';
import { clearSession, getAdminName, onSessionChange, signIn } from './auth.js';
import {
  applyTranslations,
  dirOf,
  getLang,
  langFromHash,
  onLangChange,
  otherLang,
  setLang,
  t,
} from './i18n.js';
import { TABS, isTabEnabled, nextTabIndex, visibleTabs } from './tabs.js';
import { createCooldown, hideBanner, showError } from './ui.js';
import { closeDiscardDialog, installUnloadGuard, leaveIfClean } from './unsaved.js';

export function main(doc = document, win = window) {
  const loginView = doc.getElementById('login-view');
  const appView = doc.getElementById('app-view');
  const loginForm = doc.getElementById('login-form');
  const loginInput = doc.getElementById('login-token');
  const loginSubmit = doc.getElementById('login-submit');
  const loginBanner = doc.getElementById('login-banner');
  const adminName = doc.getElementById('admin-name');
  const tabList = doc.getElementById('tabs');

  const modules = {
    accounts: mountAccounts({ api, doc }),
    audit: mountAudit({ api, doc }),
    requests: mountRequests({ api, doc, win }),
    catalog: mountCatalog({ api, doc }),
    settings: mountSettings({ api, doc }),
    files: mountFiles({ api, doc }),
  };
  const idleLock = createIdleLock({ doc, win });
  let badgePollTimer = null;
  let activeTab = 'accounts';
  let signingIn = false;
  // A 429 with Retry-After on sign-in keeps the button off until it ends.
  const loginCooldown = createCooldown();
  loginCooldown.subscribe((left) => {
    if (left === 0 && !signingIn) loginSubmit.disabled = false;
  });

  // Closing or reloading the page with unsaved catalog edits asks first.
  if (win && typeof win.addEventListener === 'function') installUnloadGuard(win);

  // --- language ----------------------------------------------------------
  setLang(langFromHash(win.location.hash));
  applyTranslations(doc);
  onLangChange(() => {
    applyTranslations(doc);
    for (const m of Object.values(modules)) m.rerender();
    renderSigningIn();
  });
  for (const id of ['lang-toggle', 'login-lang']) {
    doc.getElementById(id).addEventListener('click', () => {
      setLang(otherLang(getLang()));
      win.history.replaceState(null, '', `#${getLang()}`);
    });
  }

  // --- tabs --------------------------------------------------------------
  for (const tab of TABS) {
    const button = doc.getElementById(`tab-${tab.id}`);
    if (button) button.hidden = !tab.enabled;
  }
  const tabButtons = () => visibleTabs().map((tab) => doc.getElementById(`tab-${tab.id}`));

  function activate(id) {
    if (!isTabEnabled(id)) return;
    activeTab = id;
    for (const tab of TABS) {
      const selected = tab.id === id;
      const button = doc.getElementById(`tab-${tab.id}`);
      const panel = doc.getElementById(`panel-${tab.id}`);
      if (button) {
        button.setAttribute('aria-selected', String(selected));
        button.tabIndex = selected ? 0 : -1;
      }
      if (panel) panel.hidden = !selected;
    }
    modules[id].load();
    if (modules.requests && typeof modules.requests.refreshBadge === 'function') {
      modules.requests.refreshBadge();
    }
  }

  // A tab switch with unsaved edits asks first; Stay keeps the current tab.
  async function switchTab(id) {
    const moved = await leaveIfClean(doc, () => activate(id), () => {
      modules.catalog.discard();
      if (modules.settings?.discard) modules.settings.discard();
      if (modules.files?.discard) modules.files.discard();
    });
    if (!moved) doc.getElementById(`tab-${activeTab}`)?.focus();
  }

  for (const tab of visibleTabs()) {
    doc.getElementById(`tab-${tab.id}`).addEventListener('click', () => switchTab(tab.id));
  }
  tabList.addEventListener('keydown', (event) => {
    const buttons = tabButtons();
    const current = buttons.indexOf(doc.activeElement);
    if (current < 0) return;
    const next = nextTabIndex(current, buttons.length, event.key, dirOf(getLang()));
    if (next !== current) {
      event.preventDefault();
      buttons[next].focus();
      buttons[next].click();
    }
  });

  // --- session -----------------------------------------------------------
  function renderSigningIn() {
    loginSubmit.textContent = t(signingIn ? 'login.verifying' : 'login.submit');
  }

  function showLogin(message) {
    idleLock.stop();
    closeDiscardDialog();
    if (badgePollTimer) {
      win.clearInterval(badgePollTimer);
      badgePollTimer = null;
    }
    appView.hidden = true;
    loginView.hidden = false;
    adminName.textContent = '';
    for (const m of Object.values(modules)) m.reset();
    if (message) {
      loginBanner.textContent = message;
      loginBanner.hidden = false;
    } else {
      hideBanner(loginBanner);
    }
    loginInput.focus();
  }

  function showApp() {
    loginView.hidden = true;
    appView.hidden = false;
    adminName.textContent = getAdminName();
    hideBanner(loginBanner);
    idleLock.start();
    if (modules.requests && typeof modules.requests.refreshBadge === 'function') {
      modules.requests.refreshBadge();
    }
    if (!badgePollTimer && win && typeof win.setInterval === 'function') {
      badgePollTimer = win.setInterval(() => {
        if (modules.requests && typeof modules.requests.refreshBadge === 'function') {
          modules.requests.refreshBadge();
        }
      }, 60 * 1000);
      if (badgePollTimer && typeof badgePollTimer.unref === 'function') {
        badgePollTimer.unref();
      }
    }
    activate('accounts');
  }

  onSessionChange((event) => {
    if (event.signedIn) {
      showApp();
    } else {
      let msg = '';
      if (event.reason === 'unauthorized') msg = t('login.expired');
      else if (event.reason === 'idle_lock') msg = t('login.idleLocked');
      showLogin(msg);
    }
  });

  loginForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    if (signingIn || loginCooldown.active) return;
    signingIn = true;
    loginSubmit.disabled = true;
    renderSigningIn();
    hideBanner(loginBanner);
    const result = await signIn(loginInput.value, () => api.whoami());
    signingIn = false;
    loginSubmit.disabled = false;
    renderSigningIn();
    if (result.ok) {
      loginInput.value = '';
      return;
    }
    if (result.kind === 'empty') {
      loginBanner.textContent = t('login.empty');
      loginBanner.hidden = false;
    } else if (result.kind === 'unauthorized') {
      loginInput.value = '';
      loginBanner.textContent = t('login.expired');
      loginBanner.hidden = false;
    } else {
      loginCooldown.arm(result);
      loginSubmit.disabled = loginCooldown.active;
      showError(loginBanner, result, () => loginForm.requestSubmit(), loginCooldown);
    }
  });

  doc.getElementById('sign-out').addEventListener('click', () => clearSession('signed_out'));

  showLogin('');
  return { activate, get activeTab() { return activeTab; } };
}

if (typeof document !== 'undefined') main();
