import './style.css';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { h, clear, icon } from './util';
import { Go } from './api';
import { state, on, emit, connInfo, displayName, tildify, type Selection } from './state';
import { applyTheme, applyFont } from './theme';
import { Tree } from './tree';
import { DataTab, welcome } from './datatab';
import { StructureTab } from './structure';
import { QueryTab } from './query';
import { dialogOpen, confirm, toast, showError } from './dialogs';
import * as actions from './actions';
import { logo } from './logo';
import type { theme } from '../wailsjs/go/models';

type TabId = 'data' | 'structure' | string;

const tree = new Tree();
actions.bindTree(tree);
const data = new DataTab();
const structure = new StructureTab();
const queries: QueryTab[] = [];
let queryCount = 0;
let active: TabId = 'data';

const tabbar = h('div', { class: 'tabbar', role: 'tablist' });
const breadcrumb = h('div', { class: 'breadcrumb' });
const content = h('div', { class: 'pane' });
const logBox = h('section', { class: 'log', 'aria-label': 'SQL log' });
const statusLeft = h('span');
const statusRight = h('span', { class: 'right' });

function layout() {
  const sidebar = h('div', { class: 'sidebar' }, tree.root);
  const resizer = h('div', { class: 'resizer', role: 'separator', 'aria-orientation': 'vertical' });
  resizer.addEventListener('mousedown', (e) => {
    e.preventDefault();
    resizer.classList.add('drag');
    const move = (ev: MouseEvent) => (sidebar.style.width = Math.max(160, ev.clientX) + 'px');
    const up = () => {
      resizer.classList.remove('drag');
      document.removeEventListener('mousemove', move);
      document.removeEventListener('mouseup', up);
    };
    document.addEventListener('mousemove', move);
    document.addEventListener('mouseup', up);
  });
  const toolbar = h('header', { class: 'toolbar' },
    h('span', { class: 'brand' }, logo(20, 'brand-logo'), 'd', h('b', {}, 'bro')),
    h('button', { type: 'button', class: 'btn', title: 'New connection (Ctrl+N)', 'aria-label': 'New connection', onclick: () => actions.editConnection() }, icon('plus'), h('span', { class: 'lbl' }, 'New connection')),
    h('button', { type: 'button', class: 'btn', title: 'Refresh (F5)', 'aria-label': 'Refresh', onclick: () => refresh() }, icon('refresh'), h('span', { class: 'lbl' }, 'Refresh'), h('span', { class: 'shortcut' }, 'F5')),
    h('button', { type: 'button', class: 'btn', title: 'New query (Ctrl+T)', 'aria-label': 'New query', onclick: () => openQuery(state.selection) }, icon('file'), h('span', { class: 'lbl' }, 'New query'), h('span', { class: 'shortcut' }, 'Ctrl+T')),
    h('span', { class: 'spacer' }),
    h('button', { type: 'button', class: 'btn', title: 'Commands, projects, databases and tables (Ctrl+K), tables only (Ctrl+P)', 'aria-label': 'Command palette', onclick: () => commandPalette() }, icon('search'), h('span', { class: 'lbl' }, 'Search'), h('span', { class: 'shortcut' }, 'Ctrl+K')),
    h('button', { type: 'button', class: 'btn icon', 'aria-label': 'Settings', title: 'Settings', onclick: () => actions.settings() }, icon('gear', 16)),
  );
  const main = h('main', { class: 'main' }, breadcrumb, tabbar, content);
  const logToggle = h('button', { type: 'button', class: 'btn small', onclick: () => logBox.classList.toggle('collapsed') }, 'Log');
  clear(document.getElementById('app')!,
    toolbar,
    h('div', { class: 'body' }, sidebar, resizer, main),
    logBox,
    h('footer', { class: 'statusbar' }, statusLeft, h('span', { class: 'right' }, statusRight, logToggle)));
}

function renderTabs() {
  const sel = state.selection;
  const tableish = sel && (sel.kind === 'table' || sel.kind === 'view');
  const tab = (id: TabId, label: string, closable = false) => {
    const b = h('button', { type: 'button', role: 'tab', class: 'tab' + (active === id ? ' active' : ''), 'aria-selected': String(active === id), onclick: () => showTab(id) }, label);
    if (closable) {
      const x = h('span', { class: 'close', role: 'button', 'aria-label': 'Close ' + label, onclick: (e: Event) => { e.stopPropagation(); closeQuery(id); } }, icon('x', 11));
      b.append(x);
      b.addEventListener('auxclick', (e) => e.button === 1 && closeQuery(id));
    }
    return b;
  };
  clear(tabbar,
    tab('data', sel?.kind === 'database' ? 'Database' : sel?.kind === 'connection' ? 'Connection' : tableish ? 'Data' : 'Home'),
    tab('structure', sel?.kind === 'routine' ? 'Source' : 'Structure'),
    queries.map((q) => tab(q.id, q.title, true)),
    h('button', { type: 'button', class: 'tab', 'aria-label': 'New query tab', title: 'New query (Ctrl+T)', onclick: () => openQuery(state.selection) }, icon('plus', 12)));
  const crumbs = sel ? [displayName(sel.conn), sel.db, sel.table ?? sel.routine?.name].filter(Boolean) as string[] : [];
  clear(breadcrumb, crumbs.flatMap((c, i) => [i ? '  ›  ' : '', i === crumbs.length - 1 ? h('b', {}, c) : c]));
}

function showTab(id: TabId) {
  active = id;
  const q = queries.find((x) => x.id === id);
  if (id === 'data') clear(content, data.el);
  else if (id === 'structure') {
    clear(content, structure.el);
    structure.show(state.selection);
  } else if (q) {
    clear(content, q.el);
    q.useSelection(state.selection);
    setTimeout(() => q.focus(), 0);
  }
  renderTabs();
}

export function openQuery(sel: Selection | null, text = '', show = true) {
  const q = new QueryTab(++queryCount, sel, text);
  queries.push(q);
  if (show) showTab(q.id);
  else renderTabs();
  saveQueries();
}

// Query tabs survive restarts: their text and target are kept in this browser profile.
const QUERIES_KEY = 'dbro.queryTabs';
function saveQueries() {
  try {
    localStorage.setItem(QUERIES_KEY, JSON.stringify(queries.map((q) => ({ conn: q.conn, db: q.database, text: q.content() }))));
  } catch {
    // storage unavailable: tabs just won't be restored
  }
}
function restoreQueries() {
  try {
    const saved = JSON.parse(localStorage.getItem(QUERIES_KEY) ?? '[]') as { conn: string; db: string; text: string }[];
    for (const t of saved) {
      if (!t.text.trim()) continue;
      const known = connInfo(t.conn);
      openQuery(known ? { kind: 'database', conn: t.conn, db: t.db } : null, t.text, false);
    }
  } catch {
    // ignore broken saved state
  }
}
on('queriesChanged', () => saveQueries());

async function closeQuery(id: TabId) {
  const i = queries.findIndex((q) => q.id === id);
  if (i < 0) return;
  queries[i].dispose();
  queries.splice(i, 1);
  saveQueries();
  if (active === id) showTab(queries[i - 1]?.id ?? queries[i]?.id ?? 'data');
  else renderTabs();
}

export function revealTable(sel: Selection) {
  tree.reveal(sel).then(() => showTab('structure'));
}

async function refresh() {
  await tree.refresh();
  if (active === 'data') data.show(state.selection, true);
  if (active === 'structure') structure.show(state.selection, true);
}

function updateStatus() {
  const sel = state.selection;
  const c = sel ? connInfo(sel.conn) : undefined;
  statusLeft.textContent = c ? `${displayName(c.name)} · ${c.driver}${c.driver === 'sqlite' ? ' · ' + tildify(c.file) : ` · ${c.host || '127.0.0.1'}${c.port ? ':' + c.port : ''}`}${c.ssh?.host ? ' via ' + c.ssh.host : ''}` : 'No connection selected';
  statusRight.textContent = `MCP: ${c?.hideFromAgents ? 'hidden' : c?.allowAgentWrites ? 'read/write' : 'read-only'} · ${state.boot?.theme?.name ?? ''}`;
}

// ---- log ----

interface LogEntry { time: string; source: string; conn: string; database?: string; sql: string; durationMs: number; rows: number; error?: string }
function addLog(e: LogEntry) {
  const t = new Date(e.time);
  const line = h('div', { class: 'log-line' + (e.error ? ' error' : ''), title: e.error ? e.sql + '\n\n' + e.error : e.sql },
    h('span', { class: 'time' }, t.toLocaleTimeString('en-GB')),
    h('span', { class: 'src ' + e.source }, `[${e.source}]`),
    h('span', { class: 'muted' }, displayName(e.conn)),
    h('span', { class: 'sql' }, e.sql.replace(/\s+/g, ' ') + (e.error ? '  -- ' + e.error : '')),
    h('span', { class: 'dur' }, `${e.durationMs.toFixed(1)} ms${e.rows ? ' · ' + e.rows + ' rows' : ''}`));
  const stick = logBox.scrollTop + logBox.clientHeight >= logBox.scrollHeight - 4;
  logBox.append(line);
  while (logBox.childElementCount > 500) logBox.firstElementChild!.remove();
  if (stick) logBox.scrollTop = logBox.scrollHeight;
}

// ---- events ----

on('select', (sel: Selection) => {
  if (active === 'data' || (active === 'structure' && sel.kind === 'routine')) data.show(sel);
  if (sel.kind === 'routine' && active === 'data') showTab('structure');
  if (active === 'structure') structure.show(sel);
  queries.find((q) => q.id === active)?.useSelection(sel);
  if (active !== 'data' && active !== 'structure') data.show(sel);
  renderTabs();
  updateStatus();
});
on('showTab', (id: TabId) => showTab(id));
on('newQuery', (sel: Selection) => openQuery(sel));
on('newTable', (sel: Selection) => {
  showTab('structure');
  structure.newTable(sel);
});
on('filterData', (where: string) => {
  showTab('data');
  data.filterBy(where);
});
on('reloadData', () => data.load());
on('schemaChanged', () => {
  if (state.selection?.table) data.show(state.selection, true);
});
on('themeChanged', (t: theme.Theme) => {
  state.boot.theme = t;
  updateStatus();
});

document.addEventListener('keydown', async (e) => {
  if (dialogOpen()) return;
  const k = (e.ctrlKey || e.metaKey ? 'Ctrl+' : '') + (e.shiftKey ? 'Shift+' : '') + e.key.toLowerCase();
  switch (k) {
    case 'f5':
    case 'Ctrl+r':
      e.preventDefault();
      return refresh();
    case 'Ctrl+t':
      e.preventDefault();
      return openQuery(state.selection);
    case 'Ctrl+p':
      e.preventDefault();
      return actions.quickSearch();
    case 'Ctrl+k':
      e.preventDefault();
      return commandPalette();
    case 'Ctrl+w':
      if (queries.some((q) => q.id === active)) {
        e.preventDefault();
        closeQuery(active);
      }
      return;
    case 'Ctrl+f':
      if (active === 'data' && state.selection?.table) {
        e.preventDefault();
        data.focusFilter();
      }
      return;
    case 'Ctrl+s':
      e.preventDefault();
      if (active === 'data') return data.save();
      if (active === 'structure') return structure.save();
      return;
    case 'Ctrl+,':
      e.preventDefault();
      return actions.settings();
    case 'Ctrl+n':
      e.preventDefault();
      return actions.editConnection();
  }
});

/** commandPalette (Ctrl+K) lists commands for the current selection, then every loaded table. */
// themes are fetched ahead, so the palette opens at once and no keystroke is lost
let themes: Awaited<ReturnType<typeof Go.ListThemes>> = [];
const loadThemes = () => Go.ListThemes().then((t) => (themes = t ?? [])).catch(() => {});

async function commandPalette() {
  loadThemes();
  const sel = state.selection;
  const cmds: actions.PaletteItem[] = [
    { label: 'New query', icon: 'file', shortcut: 'Ctrl+T', run: () => openQuery(state.selection) },
    { label: 'New connection', icon: 'plus', shortcut: 'Ctrl+N', run: () => actions.editConnection() },
    { label: 'Refresh', icon: 'refresh', shortcut: 'F5', run: () => refresh() },
    { label: 'Settings', icon: 'gear', shortcut: 'Ctrl+,', run: () => actions.settings() },
    { label: 'Toggle SQL log', run: () => logBox.classList.toggle('collapsed') },
  ];
  if (sel && (sel.kind === 'table' || sel.kind === 'view')) {
    const t = sel.table!;
    cmds.unshift(
      { label: `Open data: ${t}`, icon: 'table', run: () => showTab('data') },
      { label: `Open structure: ${t}`, icon: 'table', run: () => showTab('structure') },
      ...(['SELECT', 'INSERT', 'UPDATE', 'DELETE'] as const).filter((k) => sel.kind === 'table' || k === 'SELECT').map((k) => ({ label: `Generate ${k}: ${t}`, icon: 'file', run: () => actions.generateSQL(sel, k) })),
      ...['csv', 'json', 'sql'].map((f) => ({ label: `Export ${t} as ${f.toUpperCase()}`, run: () => actions.exportTable(sel, f) })),
      { label: `Import CSV into ${t}`, run: () => actions.importCSV(sel) },
      { label: `Clone ${t} to…`, run: () => actions.cloneTo(sel) },
      { label: `Copy name: ${t}`, run: () => Go.CopyText(t) },
    );
  }
  if (sel?.db && sel.kind !== 'connection') {
    const dbSel: Selection = { kind: 'database', conn: sel.conn, db: sel.db };
    cmds.push(
      { label: `New table in ${sel.db}`, icon: 'plus', run: () => actions.newTable(dbSel) },
      { label: `Dump ${sel.db}…`, run: () => actions.dump(dbSel) },
      { label: `Import SQL into ${sel.db}…`, run: () => actions.importSQL(dbSel) },
      { label: `Clone ${sel.db} to…`, run: () => actions.cloneTo(dbSel) },
    );
  }
  for (const t of themes) {
    cmds.push({ label: `Theme: ${t.name}`, run: async () => {
      const th = await Go.SetTheme(t.id);
      applyTheme(th);
      state.boot.settings.theme = t.id;
      state.boot.theme = th;
      updateStatus();
    } });
  }
  await actions.palette('Command palette', [...cmds, ...actions.placeItems(), ...actions.tableItems()], 'Type a command, project, database or table…');
}

window.addEventListener('beforeunload', (e) => {
  if (data.dirty) e.preventDefault();
});

async function start() {
  layout();
  try {
    state.boot = await Go.GetBootstrap();
    loadThemes();
  } catch (e) {
    showError(e, 'Could not load settings');
    return;
  }
  applyTheme(state.boot.theme);
  applyFont(state.boot.settings.font ?? '');
  EventsOn('theme', (t: theme.Theme) => {
    applyTheme(t);
    state.boot.theme = t;
    updateStatus();
  });
  EventsOn('log', addLog);
  EventsOn('progress', (p: { task: string; text: string }) => {
    if (p.text) statusLeft.textContent = p.text;
    else updateStatus();
  });
  await tree.reload();
  if (state.boot.settings.lerdAutoConnect) tree.autoConnectLerd();
  restoreQueries();
  clear(data.el, welcome());
  showTab('data');
  updateStatus();
  if (state.boot.askOmarchy) actions.askOmarchy();
}

start();

export { emit, confirm, toast };
