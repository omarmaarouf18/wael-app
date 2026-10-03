// Catalog tab: levels, then the subjects of one level, then the videos of
// one subject. The breadcrumb walks back; the subjects module keeps its own
// filter and page while its view is left and re-entered. Every mutation
// reloads its list from the server.

import { clear, h } from './dom.js';
import { t } from './i18n.js';
import { createConfirm } from './confirm.js';
import { mountLevels } from './levels.js';
import { createSubjectDialog } from './subject-dialog.js';
import { mountSubjects } from './subjects.js';
import { createVideoDialog } from './video-dialog.js';
import { mountVideos } from './videos.js';
import { hideToast, toast } from './ui.js';

export function mountCatalog({ api, doc = document }) {
  const crumb = doc.getElementById('catalog-crumb');

  const confirm = createConfirm({ api, doc });
  const toastDone = (key) => toast(doc, t(key));
  // Dialogs reload their list after a mutation (toast first, then refresh).
  const subjectDialog = createSubjectDialog({
    api, doc,
    onDone: (key) => {
      toastDone(key);
      subjects.reload();
    },
  });
  const videoDialog = createVideoDialog({
    api, doc,
    onDone: (key) => {
      toastDone(key);
      videos.reload();
    },
  });

  const videos = mountVideos({
    api, doc,
    dialog: videoDialog,
    confirm,
    onChanged: () => subjects.reload(),
  });
  const subjects = mountSubjects({
    api, doc,
    dialog: subjectDialog,
    confirm,
    levelsOf: () => levels.items,
    onOpen: (subject) => enterVideos(subject),
  });
  const levels = mountLevels({
    api, doc,
    confirm,
    onOpen: (level) => enterSubjects(level),
  });

  // View stack: [{ view: 'levels' }] |
  // [{ view: 'levels' }, { view: 'subjects', level }] |
  // [..., { view: 'videos', subject (with .level) }].
  let stack = [{ view: 'levels' }];

  function current() {
    return stack[stack.length - 1];
  }

  function levelOf(entry) {
    if (!entry) return null;
    if (entry.view === 'subjects') return entry.level;
    if (entry.view === 'videos') return entry.subject?.level ?? null;
    return null;
  }

  function renderCrumb() {
    clear(crumb);
    crumb.append(h('button', {
      class: 'btn link', text: t('catalog.title'), attrs: { type: 'button' },
      on: { click: () => enterLevels() },
    }));
    const top = current();
    const level = levelOf(top);
    if (level) {
      crumb.append(h('span', { class: 'muted', text: ' › ' }));
      crumb.append(h('button', {
        class: 'btn link', text: level.name_ar || level.key, attrs: { type: 'button' },
        on: { click: () => backToSubjects() },
      }));
    }
    if (top.view === 'videos' && top.subject) {
      crumb.append(h('span', { class: 'muted', text: ' › ' }));
      crumb.append(h('span', { text: top.subject.title_ar || top.subject.id }));
    }
  }

  function enterLevels() {
    stack = [{ view: 'levels' }];
    renderCrumb();
    levels.load();
  }

  function enterSubjects(level) {
    stack = [{ view: 'levels' }, { view: 'subjects', level }];
    renderCrumb();
    subjects.open(level, false);
  }

  function enterVideos(subject) {
    const subjectsEntry = stack.find((e) => e.view === 'subjects');
    const withLevel = { ...subject, level: subjectsEntry?.level ?? null };
    stack = [{ view: 'levels' }, ...(subjectsEntry ? [subjectsEntry] : []), { view: 'videos', subject: withLevel }];
    renderCrumb();
    videos.open(withLevel);
  }

  function backToSubjects() {
    const entry = stack.find((e) => e.view === 'subjects');
    if (!entry) {
      enterLevels();
      return;
    }
    stack = stack.slice(0, stack.indexOf(entry) + 1);
    renderCrumb();
    // The subjects module kept its filter and page; just reload the list.
    subjects.reload();
  }

  return {
    load() {
      enterLevels();
    },
    rerender() {
      renderCrumb();
      levels.rerender();
      subjects.rerender();
      videos.rerender();
      subjectDialog.rerender();
      videoDialog.rerender();
      confirm.rerender();
    },
    reset() {
      stack = [{ view: 'levels' }];
      levels.reset();
      subjects.reset();
      videos.reset();
      confirm.close();
      hideToast(doc);
    },
  };
}
