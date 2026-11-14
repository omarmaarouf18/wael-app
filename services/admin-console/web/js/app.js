// Entry point: wires the login form, the language toggle, the tabs and the
// tab modules. Everything about a session (token, name) lives in auth.js.

import { mountAccounts } from './accounts.js';
import { api } from './api.js';
import { mountAudit } from './audit.js';
import { mountCatalog } from './catalog.js';
import { mountFiles } from './files.js';
import { mountIAP } from './iap.js';
import { mountNotifications } from './notifications.js';
import { mountOverview } from './overview.js';
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
import { DEFAULT_TAB, TABS, isTabEnabled, nextTabIndex, visibleTabs } from './tabs.js';
import { createCooldown, hideBanner, initialOf, showError } from './ui.js';
import { closeDiscardDialog, installUnloadGuard, leaveIfClean } from './unsaved.js';

export function main(doc = document, win = window) {
  const loginView = doc.getElementById('login-view');
  const appView = doc.getElementById('app-view');
  const loginForm = doc.getElementById('login-form');
  const loginInput = doc.getElementById('login-token');
  const loginSubmit = doc.getElementById('login-submit');
  const loginBanner = doc.getElementById('login-banner');
  const adminName = doc.getElementById('admin-name');
  const adminInitial = doc.getElementById('admin-initial');
  const tabList = doc.getElementById('tabs');
  const sidenav = doc.getElementById('sidenav');
  const navOpen = doc.getElementById('nav-open');
  const navClose = doc.getElementById('nav-close');
  const navScrim = doc.getElementById('nav-scrim');

  // The audit log links student and package names to their tabs.
  function openFromAudit(link) {
    if (!link) return;
    if (link.tab === 'accounts' && link.user) modules.accounts.showUser(link.user);
    else if (link.tab === 'iap' && link.packageId) modules.iap.focusPackage(link.packageId);
    else return;
    switchTab(link.tab);
  }

  const modules = {
    overview: mountOverview({
      api, doc,
      decide: (action, requests) => modules.requests.decide(action, requests),
      openTab: (id) => switchTab(id),
      navigate: openFromAudit,
    }),
    accounts: mountAccounts({ api, doc }),
    audit: mountAudit({ api, doc, win, navigate: openFromAudit }),
    requests: mountRequests({ api, doc, win, onDecided: afterDecision }),
    catalog: mountCatalog({ api, doc }),
    iap: mountIAP({ api, doc }),
    settings: mountSettings({ api, doc }),
    files: mountFiles({ api, doc }),
    notifications: mountNotifications({ api, doc }),
  };
  // An accept/reject started from the overview reloads it; a partial failure
  // is shown there too (the Requests tab keeps its own banner).
  function afterDecision(result = {}) {
    if (activeTab !== 'overview') return;
    if (result.partial) modules.overview.showNotice(result.partial);
    modules.overview.load({ keepNotice: Boolean(result.partial) });
  }
  const idleLock = createIdleLock({ doc, win });
  let badgePollTimer = null;
  let activeTab = DEFAULT_TAB;
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

  // --- navigation drawer (below 900px) ----------------------------------
  // On a wide screen the side navigation is always shown and these controls
  // are hidden by the stylesheet; the open state then changes nothing visible.
  function setNav(open) {
    appView.setAttribute('data-nav', open ? 'open' : 'closed');
    navOpen.setAttribute('aria-expanded', String(open));
    navScrim.hidden = !open;
    if (open) doc.getElementById(`tab-${activeTab}`)?.focus();
  }
  const navIsOpen = () => appView.getAttribute('data-nav') === 'open';
  function closeNav({ returnFocus = false } = {}) {
    if (!navIsOpen()) return;
    setNav(false);
    if (returnFocus) navOpen.focus();
  }
  navOpen.addEventListener('click', () => setNav(!navIsOpen()));
  navClose.addEventListener('click', () => closeNav({ returnFocus: true }));
  navScrim.addEventListener('click', () => closeNav({ returnFocus: true }));
  sidenav.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && navIsOpen()) {
      event.preventDefault();
      closeNav({ returnFocus: true });
    }
  });

  // A tab switch with unsaved edits asks first; Stay keeps the current tab.
  async function switchTab(id) {
    closeNav();
    const moved = await leaveIfClean(doc, () => activate(id), () => {
      modules.catalog.discard();
      if (modules.settings?.discard) modules.settings.discard();
      if (modules.files?.discard) modules.files.discard();
      if (modules.notifications?.discard) modules.notifications.discard();
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
    setNav(false);
    adminName.textContent = '';
    adminInitial.textContent = '';
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
    adminInitial.textContent = initialOf(getAdminName());
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
    activate(DEFAULT_TAB);
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
  return { activate, switchTab, get activeTab() { return activeTab; } };
}

if (typeof document !== 'undefined') main();
