// Entry point: wires the login form, the language toggle, the tabs and the
// tab modules. Everything about a session (token, name) lives in auth.js.

import { mountAccounts } from './accounts.js';
import { api } from './api.js';
import { mountAudit } from './audit.js';
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
import { hideBanner, showBanner } from './ui.js';

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
  };
  let activeTab = 'accounts';
  let signingIn = false;

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
  }

  for (const tab of visibleTabs()) {
    doc.getElementById(`tab-${tab.id}`).addEventListener('click', () => activate(tab.id));
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
    activate('accounts');
  }

  onSessionChange((event) => {
    if (event.signedIn) showApp();
    else showLogin(event.reason === 'unauthorized' ? t('login.expired') : '');
  });

  loginForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    if (signingIn) return;
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
      showBanner(loginBanner, result.kind, () => loginForm.requestSubmit());
    }
  });

  doc.getElementById('sign-out').addEventListener('click', () => clearSession('signed_out'));

  showLogin('');
  return { activate, get activeTab() { return activeTab; } };
}

if (typeof document !== 'undefined') main();
