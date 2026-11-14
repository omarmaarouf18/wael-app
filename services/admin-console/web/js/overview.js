// Overview tab (owner redesign 2026-11-14): the first tab after sign-in.
// The greeting and date are drawn here; the cards come in a later step.

import { getAdminName } from './auth.js';
import { formatLongDate, t } from './i18n.js';

export function mountOverview({ doc = document, now = () => new Date() } = {}) {
  const greeting = doc.getElementById('overview-greeting');
  const date = doc.getElementById('overview-date');

  function render() {
    greeting.textContent = t('overview.greeting', { name: getAdminName() || t('top.role') });
    date.textContent = t('overview.date', { date: formatLongDate(now()) });
  }

  return {
    load: render,
    rerender: render,
    reset() {
      greeting.textContent = '';
      date.textContent = '';
    },
  };
}
