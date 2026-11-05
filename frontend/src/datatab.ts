// The Data tab: paged, sortable, filterable, editable rows of one table,
// or an overview when a database or connection is selected.
import { h, clear, icon, formatCount, formatBytes, debounce } from './util';
import { Go, forConn } from './api';
import { Grid, nextSort, type SortKey } from './grid';
import { dropdown, toast, errorMessage, confirm, showError, modal } from './dialogs';
import { state, connInfo, quoteIdent, dialect, type Selection } from './state';
import * as actions from './actions';
import { logo } from './logo';
import { whereField } from './wherefield';
import type { db } from '../wailsjs/go/models';

const PAGE = 1000;

export class DataTab {
  el = h('div', { class: 'pane' });
  private sel: Selection | null = null;
  private grid: Grid | null = null;
  private def: db.TableDef | null = null;
  private where = '';
  private sort: SortKey[] = [];
  private offset = 0;
  private total = 0;
  private pending = h('div', { class: 'pending-bar', hidden: true });
  private info = h('span', { class: 'pager' });
  private selInfo = h('span', { class: 'muted' });
  private whereEditor = whereField(() => this.applyFilter());
  private whereInput = this.whereEditor.proxy;
  private quickInput = h('input', { class: 'input', type: 'search', style: { flex: '1 1 180px' }, placeholder: 'Quick filter: search all columns', 'aria-label': 'Quick filter across all columns', spellcheck: 'false' });
  private token = 0;
  private sortBtn: HTMLButtonElement | null = null;

  constructor() {
    const live = debounce(() => this.applyFilter(), 400);
    this.quickInput.addEventListener('input', live);
    this.quickInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') this.applyFilter();
    });
  }

  /** applyFilter combines the quick filter and the WHERE field, like HeidiSQL's filter panel. */
  private applyFilter() {
    this.where = this.whereInput.value.trim();
    this.offset = 0;
    if (this.where) remember(this.sel!, this.where);
    this.load();
  }

  /** effectiveWhere is the WHERE sent to the server: the typed condition AND the quick filter. */
  private effectiveWhere(): string {
    const parts: string[] = [];
    if (this.where) parts.push(`(${this.where})`);
    const q = this.quickInput.value.trim();
    if (q && this.def?.columns.length && this.sel) {
      const d = dialect(this.sel.conn);
      const quote = quoteIdent(this.sel.conn);
      const lit = "'%" + q.replace(/'/g, "''") + "%'";
      const one = (c: string) =>
        d === 'postgres' ? `${quote(c)}::text ILIKE ${lit}` : d === 'mysql' ? `CAST(${quote(c)} AS CHAR) LIKE ${lit}` : `CAST(${quote(c)} AS TEXT) LIKE ${lit}`;
      parts.push('(' + this.def.columns.map((c) => one(c.name)).join(' OR ') + ')');
    }
    return parts.join(' AND ');
  }

  get dirty() {
    return this.grid?.dirty ?? 0;
  }

  async show(sel: Selection | null, force = false) {
    if (!force && this.sel && sel && sameTable(this.sel, sel)) return;
    if (this.dirty && !(await confirm('Unsaved changes', `Discard ${this.dirty} unsaved changes?`, 'Discard', true))) return;
    this.sel = sel;
    this.where = '';
    this.whereInput.value = '';
    this.quickInput.value = '';
    this.sort = [];
    this.offset = 0;
    this.def = null;
    if (!sel) return clear(this.el, welcome());
    if (sel.kind === 'table' || sel.kind === 'view') return this.showTable(sel);
    if (sel.kind === 'database') return this.showDatabase(sel);
    if (sel.kind === 'connection') return this.showConnection(sel);
    clear(this.el, h('div', { class: 'empty' }, 'Open the Structure tab to see this routine.'));
  }

  private async showTable(sel: Selection) {
    const q = quoteIdent(sel.conn);
    const isView = sel.kind === 'view';
    this.grid = new Grid({
      editable: !isView,
      table: sel.table,
      quote: q,
      onSort: (col, add) => {
        this.sort = nextSort(this.sort, col, add);
        this.offset = 0;
        this.load();
      },
      onFollow: (fk, values) => actions.followForeignKey(sel, fk, values),
      onFilter: (col, op, value) => {
        const cond = op === 'IS NULL' || op === 'IS NOT NULL' ? `${q(col)} ${op}` : `${q(col)} ${op} ${typeof value === 'number' ? value : "'" + String(value).replace(/'/g, "''") + "'"}`;
        const cur = this.whereInput.value.trim();
        this.whereInput.value = cur ? `${cur}\n  AND ${cond}` : cond;
        this.applyFilter();
      },
      onSelect: (n) => {
        this.selInfo.textContent = n > 1 ? `· ${n} selected` : '';
      },
      onChange: () => this.updatePending(),
    });
    const filters = h('div', { class: 'pane-toolbar filter-bar' },
      h('label', { style: { display: 'contents' } }, icon('search'), this.quickInput),
      h('b', { class: 'where-label' }, 'WHERE'), this.whereEditor.el,
      dropdown('Recent', () => {
        const list = recent(sel);
        return list.length ? list.map((w) => ({ label: w.length > 70 ? w.slice(0, 70) + '…' : w, action: () => { this.whereInput.value = w; this.applyFilter(); } })) : [{ label: 'No recent filters', disabled: true }];
      }),
      h('button', { type: 'button', class: 'btn', onclick: () => this.applyFilter() }, 'Apply'),
      h('button', { type: 'button', class: 'btn', onclick: () => { this.whereInput.value = ''; this.quickInput.value = ''; this.applyFilter(); } }, 'Clear'),
    );
    this.sortBtn = h('button', { type: 'button', class: 'btn', onclick: () => this.editSort() }, 'Sort');
    const toolbar = h('div', { class: 'pane-toolbar' },
      this.sortBtn,
      this.info,
      this.selInfo,
      h('button', { type: 'button', class: 'btn icon', 'aria-label': 'Previous page', onclick: () => this.page(-1) }, '‹'),
      h('button', { type: 'button', class: 'btn icon', 'aria-label': 'Next page', onclick: () => this.page(1) }, '›'),
      dropdown('Copy as', () => [
        { label: 'CSV', action: () => this.grid!.copyAll('csv') },
        { label: 'JSON', action: () => this.grid!.copyAll('json') },
        { label: 'SQL INSERT', action: () => this.grid!.copyAll('sql') },
        { label: 'Markdown', action: () => this.grid!.copyAll('markdown') },
      ]),
      dropdown('Export', () => ['csv', 'json', 'sql'].map((f) => ({ label: `Whole table as ${f.toUpperCase()}`, action: () => actions.exportTable(sel, f, this.effectiveWhere()) }))),
      isView ? null : h('button', { type: 'button', class: 'btn', onclick: () => this.grid!.insertRow() }, icon('plus'), 'Row'),
      h('button', { type: 'button', class: 'btn icon', 'aria-label': 'Refresh', title: 'Refresh (F5)', onclick: () => this.load() }, icon('refresh')),
    );
    clear(this.el, filters, toolbar, this.grid.el, this.pending);
    try {
      this.def = await forConn(sel.conn, () => Go.Describe(sel.conn, sel.db!, sel.schema ?? '', sel.table!));
      this.grid.opt.pk = this.def.columns.length ? pkOf(this.def) : [];
      this.grid.opt.colTypes = Object.fromEntries(this.def.columns.map((c) => [c.name, c.type]));
      this.grid.opt.nullable = Object.fromEntries(this.def.columns.map((c) => [c.name, c.nullable]));
      this.whereEditor.setColumns(sel.table!, this.def.columns.map((c) => c.name));
      this.grid.opt.fks = this.def.foreignKeys ?? [];
    } catch (e) {
      toast(errorMessage(e), 'error');
    }
    await this.load();
  }

  async load() {
    const sel = this.sel;
    if (!sel || !this.grid || !sel.table) return;
    if (this.dirty && !(await confirm('Unsaved changes', `Discard ${this.dirty} unsaved changes?`, 'Discard', true))) return;
    const token = ++this.token;
    this.info.textContent = 'Loading…';
    try {
      const r = await forConn(sel.conn, () => Go.Browse(sel.conn, sel.db!, sel.schema ?? '', sel.table!, this.effectiveWhere(), this.sort as any, this.offset, PAGE));
      if (token !== this.token) return;
      this.total = r.total;
      this.grid.opt.sort = this.sort;
      if (this.sortBtn) this.sortBtn.textContent = this.sort.length ? 'Sort: ' + this.sort.map((k) => k.col + (k.desc ? ' ↓' : ' ↑')).join(', ') : 'Sort';
      this.grid.set(r.result.columns, r.result.types, r.result.rows);
      const n = r.result.rows?.length ?? 0;
      this.info.textContent = n ? `Rows ${formatCount(this.offset + 1)}–${formatCount(this.offset + n)} of ${formatCount(this.total)}` : `0 of ${formatCount(this.total)} rows`;
      this.updatePending();
    } catch (e) {
      if (token !== this.token) return;
      this.info.textContent = '';
      this.grid.set([], [], []);
      toast(errorMessage(e), 'error');
    }
  }

  /** editSort shows HeidiSQL's sorting dialog: an ordered list of columns with a direction each. */
  private async editSort() {
    const cols = this.def?.columns.map((c) => c.name) ?? [];
    let keys = this.sort.map((k) => ({ ...k }));
    const list = h('div', { class: 'form' });
    const render = () => {
      clear(list,
        keys.length ? null : h('p', { class: 'muted' }, 'Not sorted. Add a column below.'),
        keys.map((k, i) => {
          const col = h('select', { class: 'input', 'aria-label': `Sort column ${i + 1}` }, cols.map((c) => h('option', { value: c, selected: c === k.col }, c)));
          col.addEventListener('change', () => (k.col = col.value));
          const dir = h('select', { class: 'input', 'aria-label': `Direction ${i + 1}` }, h('option', { value: 'asc', selected: !k.desc }, 'Ascending'), h('option', { value: 'desc', selected: k.desc }, 'Descending'));
          dir.addEventListener('change', () => (k.desc = dir.value === 'desc'));
          return h('div', { class: 'form-row', style: { gridTemplateColumns: '2fr 1fr auto' } }, col, dir,
            h('button', { type: 'button', class: 'btn icon danger', 'aria-label': 'Remove', onclick: () => { keys.splice(i, 1); render(); } }, icon('x', 12)));
        }),
        h('div', {}, h('button', { type: 'button', class: 'btn', disabled: !cols.length, onclick: () => { keys.push({ col: cols.find((c) => !keys.some((k) => k.col === c)) ?? cols[0], desc: false }); render(); } }, icon('plus'), 'Add column')));
    };
    render();
    const r = await modal('Sorting', list, [{ label: 'Clear', action: () => { keys = []; } }, { label: 'Cancel' }, { label: 'Apply', primary: true }]);
    if (r === 1 || r < 0) return;
    this.sort = keys.filter((k, i) => keys.findIndex((x) => x.col === k.col) === i);
    this.offset = 0;
    this.load();
  }

  private page(dir: number) {
    const next = this.offset + dir * PAGE;
    if (next < 0 || next >= this.total) return;
    this.offset = next;
    this.load();
  }

  /** focusFilter puts the cursor in the quick filter (Ctrl+F). */
  focusFilter() {
    this.quickInput.focus();
    this.quickInput.select();
  }

  /** filterBy shows rows matching column values (follow foreign key). */
  async filterBy(where: string) {
    this.where = where;
    this.whereInput.value = where;
    this.quickInput.value = '';
    this.offset = 0;
    await this.load();
  }

  private updatePending() {
    const n = this.dirty;
    this.pending.hidden = n === 0;
    if (!n) return;
    const color = connInfo(this.sel!.conn)?.color;
    clear(this.pending, h('span', {}, `${n} pending change${n === 1 ? '' : 's'}`), h('span', { class: 'spacer' }),
      h('button', { type: 'button', class: 'btn', onclick: () => this.grid!.discard() }, 'Discard'),
      h('button', { type: 'button', class: 'btn primary', onclick: () => this.save(color === 'red') }, icon('save'), 'Save', h('span', { class: 'shortcut' }, 'Ctrl+S')));
  }

  /** save writes the queued changes; red (production) connections ask first. */
  async save(confirmFirst = connInfo(this.sel?.conn ?? '')?.color === 'red') {
    const sel = this.sel;
    if (!sel || !this.grid || !this.dirty) return;
    const changes = this.grid.changes();
    if (confirmFirst && !(await confirm('Save to a red connection', `${sel.conn} is marked red. Save ${changes.length} changes to ${sel.table}?`, 'Save', true))) return;
    try {
      await forConn(sel.conn, () => Go.ApplyRowChanges(sel.conn, sel.db!, sel.schema ?? '', sel.table!, changes as any));
      toast(`Saved ${changes.length} change${changes.length === 1 ? '' : 's'}`);
      this.grid.discard();
      await this.load();
    } catch (e) {
      showError(e, 'Could not save');
    }
  }

  private async showDatabase(sel: Selection) {
    this.grid = null;
    const body = h('div', { class: 'grid-wrap' });
    clear(this.el,
      h('div', { class: 'pane-toolbar' },
        h('b', {}, sel.db),
        h('span', { class: 'spacer' }),
        h('button', { type: 'button', class: 'btn', onclick: () => actions.newTable(sel) }, icon('plus'), 'Table'),
        h('button', { type: 'button', class: 'btn', onclick: () => actions.importSQL(sel) }, 'Import SQL'),
        h('button', { type: 'button', class: 'btn', onclick: () => actions.dump(sel) }, 'Dump'),
        h('button', { type: 'button', class: 'btn', onclick: () => actions.cloneTo(sel) }, 'Clone to…'),
      ),
      body);
    try {
      const objs = await forConn(sel.conn, () => Go.DatabaseObjects(sel.conn, sel.db!));
      const g = new Grid({});
      g.set(['name', 'schema', 'type', 'rows', 'size'], ['', '', '', 'bigint', ''], (objs.tables ?? []).map((t) => [t.name, t.schema, t.type, t.rows ?? null, t.size === undefined || t.size === null ? null : formatBytes(t.size)]));
      clear(body, g.el);
    } catch (e) {
      clear(body, h('div', { class: 'result-error' }, errorMessage(e)));
    }
  }

  private showConnection(sel: Selection) {
    this.grid = null;
    const c = connInfo(sel.conn);
    if (!c) return;
    const rows: [string, string][] = [
      ['Driver', c.driver], ['Source', c.source + (c.label ? ` (${c.label})` : '')],
      c.driver === 'sqlite' ? ['File', c.file] : ['Server', `${c.host || '127.0.0.1'}:${c.port || ''}`],
      ['User', c.user ?? ''], ['Default database', c.database ?? ''],
      ['SSH tunnel', c.ssh?.host ? `${c.ssh.user ? c.ssh.user + '@' : ''}${c.ssh.host}` : 'no'],
      ['Agents (MCP)', c.hideFromAgents ? 'hidden' : c.allowAgentWrites ? 'read and write' : 'read-only'],
    ];
    clear(this.el,
      h('div', { class: 'pane-toolbar' }, h('b', {}, c.name), h('span', { class: 'spacer' }),
        c.source === 'lerd' || c.source === 'lerd-project' ? null : h('button', { type: 'button', class: 'btn', onclick: () => actions.editConnection(c.name) }, 'Edit'),
        c.driver === 'sqlite' ? null : h('button', { type: 'button', class: 'btn', onclick: () => actions.newDatabase(c.name) }, icon('plus'), 'Database'),
      ),
      h('div', { class: 'structure' }, h('table', { class: 'sgrid', style: { maxWidth: '640px' } }, rows.filter((r) => r[1]).map(([k, v]) => h('tr', {}, h('th', {}, k), h('td', {}, v))))),
    );
  }
}

function pkOf(def: db.TableDef): string[] {
  return def.indexes?.find((i) => i.primary)?.columns ?? [];
}

function sameTable(a: Selection, b: Selection) {
  return a.kind === b.kind && a.conn === b.conn && a.db === b.db && a.schema === b.schema && a.table === b.table && a.routine?.name === b.routine?.name;
}

export function welcome() {
  return h('div', { class: 'empty' },
    logo(),
    h('div', {}, h('b', {}, 'dbro'), ' · your database bro'),
    state.connections.length ? h('div', {}, 'Pick a table in the tree, or press ', h('span', { class: 'kbd' }, 'Ctrl+P'), ' to search.') : h('button', { type: 'button', class: 'btn primary', onclick: () => actions.editConnection() }, icon('plus'), 'New connection'),
  );
}

// Recent WHERE filters per table, kept in this browser profile only.
function recentKey(sel: Selection) {
  return `dbro.filters.${sel.conn}/${sel.db}/${sel.schema ?? ''}/${sel.table}`;
}

function recent(sel: Selection): string[] {
  try {
    return JSON.parse(localStorage.getItem(recentKey(sel)) ?? '[]');
  } catch {
    return [];
  }
}

function remember(sel: Selection, where: string) {
  try {
    const list = [where, ...recent(sel).filter((w) => w !== where)].slice(0, 10);
    localStorage.setItem(recentKey(sel), JSON.stringify(list));
  } catch {
    // storage unavailable: recent filters are a convenience only
  }
}
