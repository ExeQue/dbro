// Data grid: selection, inline editing with a change queue, copy formats, side editor.
import { h, clear, cellText, rowsAs, icon } from './util';
import { menu, toast, type MenuItem } from './dialogs';
import { Go } from './api';
import { kindOf, enumValues, pickDate, pickSet, valueEditor, hexDump, type ValueEditor } from './editors';
import type { db } from '../wailsjs/go/models';

export interface SortKey {
  col: string;
  desc: boolean;
}

/** nextSort cycles a column through ascending, descending and off; add keeps the other columns. */
export function nextSort(sort: SortKey[], col: string, add: boolean): SortKey[] {
  const cur = sort.find((s) => s.col === col);
  const others = add ? sort.filter((s) => s.col !== col) : [];
  if (!cur) return [...others, { col, desc: false }];
  if (!cur.desc) return add ? sort.map((s) => (s.col === col ? { col, desc: true } : s)) : [{ col, desc: true }];
  return others;
}

export interface GridOptions {
  editable?: boolean;
  pk?: string[];
  fks?: db.ForeignKey[];
  table?: string;
  quote?: (s: string) => string;
  sort?: SortKey[];
  /** onSort is called on a header click; add is true with Ctrl or Shift held (secondary sort). */
  onSort?: (col: string, add: boolean) => void;
  /** onSortSet replaces the sort order (header menu) */
  onSortSet?: (sort: SortKey[]) => void;
  onFollow?: (fk: db.ForeignKey, values: unknown[]) => void;
  /** onFilter adds a condition for a cell's value to the table filter */
  onFilter?: (col: string, op: '=' | '<>' | 'IS NULL' | 'IS NOT NULL', value: unknown) => void;
  /** onSelect reports how many rows are selected */
  onSelect?: (count: number) => void;
  onChange?: () => void;
  /** full column types and nullability from the table definition (better than driver type names) */
  colTypes?: Record<string, string>;
  nullable?: Record<string, boolean>;
}

interface Edit {
  set: Record<string, unknown>;
}

const numericType = /int|dec|num|float|double|real|serial|money/;

export class Grid {
  el = h('div', { class: 'grid-wrap' });
  private scroll = h('div', { class: 'grid-scroll', tabIndex: 0 });
  private side = h('div', { class: 'side-editor', hidden: true });
  private columns: string[] = [];
  private types: string[] = [];
  private rows: unknown[][] = [];
  private edits = new Map<number, Edit>();
  private inserted = new Set<number>();
  private deleted = new Set<number>();
  private selected = new Set<number>();
  private cur = { r: -1, c: -1 };
  private anchor = -1;
  private table!: HTMLTableElement;
  private editing: HTMLInputElement | null = null;
  private sideEditor: ValueEditor | null = null;

  constructor(public opt: GridOptions = {}) {
    this.el.append(h('div', { class: 'split-h' }, this.scroll, this.side));
    this.scroll.addEventListener('keydown', (e) => this.onKey(e));
    this.scroll.addEventListener('copy', (e) => {
      if (this.editing) return;
      e.preventDefault();
      e.clipboardData?.setData('text/plain', this.copyText('tsv'));
    });
  }

  set(columns: string[], types: string[], rows: unknown[][]) {
    this.columns = columns ?? [];
    this.types = types ?? [];
    this.rows = rows ?? [];
    this.edits.clear();
    this.inserted.clear();
    this.deleted.clear();
    this.selected.clear();
    this.cur = { r: -1, c: -1 };
    this.side.hidden = true;
    this.render();
  }

  get rowCount() {
    return this.rows.length;
  }
  get data() {
    return { columns: this.columns, rows: this.rows };
  }

  get dirty() {
    return this.edits.size + this.inserted.size + this.deleted.size;
  }

  focus() {
    this.scroll.focus();
  }

  private render() {
    const sort = this.opt.sort ?? [];
    const fkCols = new Set((this.opt.fks ?? []).flatMap((f) => f.columns));
    const head = h('tr', {}, h('th', { class: 'rownum' }, '#'), this.columns.map((c, i) => {
      const at = sort.findIndex((s) => s.col === c);
      const mark = at < 0 ? null : h('span', { class: 'sort' }, (sort[at].desc ? '▼' : '▲') + (sort.length > 1 ? String(at + 1) : ''));
      return h('th', {
        title: this.opt.onSort ? `Sort by ${c} (Ctrl+click adds a column)` : c,
        'aria-sort': at < 0 ? undefined : sort[at].desc ? 'descending' : 'ascending',
        onclick: (e: MouseEvent) => this.opt.onSort?.(c, e.ctrlKey || e.shiftKey || e.metaKey),
        oncontextmenu: (e: MouseEvent) => {
          e.preventDefault();
          const items: MenuItem[] = [{ label: 'Copy column name', action: () => { Go.CopyText(c); toast('Copied'); } }];
          if (this.opt.onSortSet) {
            items.push({ label: 'Sort ascending', action: () => this.opt.onSortSet!([{ col: c, desc: false }]) }, { label: 'Sort descending', action: () => this.opt.onSortSet!([{ col: c, desc: true }]) });
          }
          if (this.opt.onFilter) {
            const f = this.opt.onFilter;
            items.push({ separator: true, label: '' }, { label: `Filter ${c} IS NULL`, action: () => f(c, 'IS NULL', null) }, { label: `Filter ${c} IS NOT NULL`, action: () => f(c, 'IS NOT NULL', null) });
          }
          menu(e.clientX, e.clientY, items);
        },
      }, c, mark, this.types[i] ? h('span', { class: 'type' }, this.types[i]) : null);
    }));
    const body = h('tbody');
    const frag = document.createDocumentFragment();
    const isNum = this.types.map((t) => numericType.test(t ?? ''));
    this.rows.forEach((row, r) => {
      const tr = document.createElement('tr');
      tr.dataset.r = String(r);
      if (this.selected.has(r)) tr.className = 'selected';
      if (this.inserted.has(r)) tr.classList.add('inserted');
      if (this.deleted.has(r)) tr.classList.add('deleted');
      const num = document.createElement('td');
      num.className = 'rownum';
      num.textContent = this.inserted.has(r) ? '*' : String(r + 1);
      tr.appendChild(num);
      const edit = this.edits.get(r);
      this.columns.forEach((col, c) => {
        const td = document.createElement('td');
        const changed = edit && col in edit.set;
        const v = changed ? edit!.set[col] : row[c];
        const text = cellText(v);
        td.textContent = v === '' ? '(empty)' : text.length > 300 ? text.slice(0, 300) + '…' : text;
        if (text.length > 40) td.title = text.length > 2000 ? text.slice(0, 2000) + '…' : text;
        let cls = v === null || v === undefined ? 'null' : v === '' ? 'empty' : isNum[c] ? 'num' : '';
        if (changed) cls += ' changed';
        if (fkCols.has(col)) cls += ' fk';
        if (r === this.cur.r && c === this.cur.c) cls += ' cur';
        td.className = cls;
        tr.appendChild(td);
      });
      frag.appendChild(tr);
    });
    body.appendChild(frag);
    this.table = h('table', { class: 'grid', role: 'grid' }, h('thead', {}, head), body);
    body.addEventListener('mousedown', (e) => this.onMouse(e));
    body.addEventListener('dblclick', (e) => {
      const p = this.pos(e);
      if (p && p.c >= 0) this.startEdit();
    });
    body.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      const p = this.pos(e);
      if (p && !this.selected.has(p.r)) this.setCursor(p.r, Math.max(p.c, 0), false, false);
      menu(e.clientX, e.clientY, this.menuItems());
    });
    clear(this.scroll, this.columns.length ? this.table : h('div', { class: 'empty' }, 'No columns'));
  }

  private pos(e: Event): { r: number; c: number } | null {
    const td = (e.target as HTMLElement).closest('td');
    const tr = td?.parentElement as HTMLTableRowElement | undefined;
    if (!td || !tr) return null;
    return { r: Number(tr.dataset.r), c: td.cellIndex - 1 };
  }

  private onMouse(e: MouseEvent) {
    if (e.button !== 0) return;
    const p = this.pos(e);
    if (!p) return;
    if (this.editing) this.commitEdit();
    this.setCursor(p.r, Math.max(p.c, 0), e.shiftKey, e.ctrlKey || e.metaKey);
  }

  private setCursor(r: number, c: number, extend = false, toggle = false) {
    r = Math.max(0, Math.min(r, this.rows.length - 1));
    c = Math.max(0, Math.min(c, this.columns.length - 1));
    if (extend && this.anchor >= 0) {
      this.selected.clear();
      for (let i = Math.min(this.anchor, r); i <= Math.max(this.anchor, r); i++) this.selected.add(i);
    } else if (toggle) {
      this.selected.has(r) ? this.selected.delete(r) : this.selected.add(r);
      this.anchor = r;
    } else {
      this.selected = new Set([r]);
      this.anchor = r;
    }
    const prev = this.cur;
    this.cur = { r, c };
    this.opt.onSelect?.(this.selected.size);
    // cheap update instead of a full render
    this.table.querySelectorAll('tr.selected').forEach((tr) => tr.classList.remove('selected'));
    for (const i of this.selected) this.rowEl(i)?.classList.add('selected');
    this.cellEl(prev.r, prev.c)?.classList.remove('cur');
    const td = this.cellEl(r, c);
    td?.classList.add('cur');
    td?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    if (!this.side.hidden) this.openSide();
    this.scroll.focus({ preventScroll: true });
  }

  private rowEl(r: number) {
    return this.table.tBodies[0]?.rows[r];
  }
  private cellEl(r: number, c: number) {
    return r >= 0 && c >= 0 ? (this.rowEl(r)?.cells[c + 1] as HTMLTableCellElement | undefined) : undefined;
  }

  value(r: number, c: number): unknown {
    const e = this.edits.get(r);
    const col = this.columns[c];
    return e && col in e.set ? e.set[col] : this.rows[r]?.[c];
  }

  private onKey(e: KeyboardEvent) {
    if (this.editing) return;
    const { r, c } = this.cur;
    const page = Math.max(1, Math.floor(this.scroll.clientHeight / 24) - 1);
    const moves: Record<string, [number, number]> = { ArrowDown: [1, 0], ArrowUp: [-1, 0], ArrowLeft: [0, -1], ArrowRight: [0, 1], PageDown: [page, 0], PageUp: [-page, 0], Tab: [0, e.shiftKey ? -1 : 1] };
    if (moves[e.key] && this.rows.length) {
      e.preventDefault();
      const [dr, dc] = moves[e.key];
      this.setCursor(r < 0 ? 0 : r + dr, c < 0 ? 0 : c + dc, e.shiftKey && e.key !== 'Tab');
      return;
    }
    if (e.key === 'Home' && e.ctrlKey) return this.setCursor(0, c);
    if (e.key === 'End' && e.ctrlKey) return this.setCursor(this.rows.length - 1, c);
    if ((e.key === 'Enter' || e.key === 'F2') && r >= 0) {
      e.preventDefault();
      return this.startEdit();
    }
    if (e.key === 'Delete' && this.opt.editable && this.selected.size && !e.shiftKey) {
      e.preventDefault();
      return this.deleteRows();
    }
    if (e.key === 'a' && e.ctrlKey) {
      e.preventDefault();
      this.selected = new Set(this.rows.map((_, i) => i));
      return this.render();
    }
    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey && r >= 0 && this.opt.editable) {
      e.preventDefault();
      this.startEdit(e.key);
    }
  }

  private colType(c: number): string {
    return this.opt.colTypes?.[this.columns[c]] ?? this.types[c] ?? '';
  }

  private isNullable(c: number): boolean {
    return this.opt.nullable?.[this.columns[c]] ?? true;
  }

  private startEdit(initial?: string) {
    const { r, c } = this.cur;
    if (!this.opt.editable || r < 0 || this.deleted.has(r)) return;
    const td = this.cellEl(r, c);
    if (!td) return;
    const v = this.value(r, c);
    const s = v === null || v === undefined ? '' : String(v);
    const type = this.colType(c);
    let kind = kindOf(type);
    // SQLite stores dates as TEXT: offer the date picker when the value looks like one
    const dateLike = /^(\d{4}-\d{2}-\d{2})( \d{2}:\d{2}(:\d{2})?)?$/.exec(s);
    if (kind === 'text' && dateLike) kind = dateLike[2] ? 'datetime' : 'date';
    if (kind === 'text' && /^\s*[[{]/.test(s)) {
      try { JSON.parse(s); kind = 'json'; } catch { /* plain text */ }
    }
    const nullable = this.isNullable(c);
    const finish = (nv: string | null | undefined) => {
      if (nv !== undefined && nv !== (v ?? null)) this.setValue(r, c, nv);
      this.scroll.focus();
    };
    if (kind === 'blob' || kind === 'json' || s.includes('\n') || s.length > 200) return this.openSide();
    if (kind === 'date' || kind === 'datetime' || kind === 'time') {
      pickDate(td, v === null || v === undefined ? null : s, kind, nullable).then(finish);
      return;
    }
    if (kind === 'set') {
      pickSet(td, v === null || v === undefined ? null : s, enumValues(type), nullable).then(finish);
      return;
    }
    if (kind === 'bool' || kind === 'enum') {
      const boolText = typeof v === 'boolean' || /bool/i.test(type);
      const options = kind === 'enum' ? enumValues(type) : boolText ? ['true', 'false'] : ['1', '0'];
      const NULL = '\u0000null';
      const sel = h('select', { 'aria-label': this.columns[c] },
        nullable ? h('option', { value: NULL, selected: v === null || v === undefined }, 'NULL') : null,
        options.map((o) => h('option', { value: o, selected: s === o || (boolText && String(v) === o) }, o)));
      td.classList.add('editing');
      clear(td, sel);
      this.editing = sel as unknown as HTMLInputElement;
      sel.focus();
      const done = (commit: boolean) => {
        if (this.editing !== (sel as unknown as HTMLInputElement)) return;
        this.editing = null;
        if (commit) finish(sel.value === NULL ? null : sel.value);
        this.refreshCell(r, c);
      };
      sel.addEventListener('keydown', (e) => {
        e.stopPropagation();
        if (e.key === 'Enter') { e.preventDefault(); done(true); this.setCursor(r + 1, c); }
        else if (e.key === 'Escape') { e.preventDefault(); done(false); this.scroll.focus(); }
      });
      sel.addEventListener('change', () => done(true));
      sel.addEventListener('blur', () => done(false));
      return;
    }
    const input = h('input', { value: initial ?? s, 'aria-label': this.columns[c], inputmode: kind === 'number' ? 'decimal' : undefined });
    td.classList.add('editing');
    clear(td, input);
    this.editing = input;
    input.focus();
    if (initial === undefined) input.select();
    const valid = () => kind !== 'number' || input.value.trim() === '' || /^-?\d+(\.\d+)?([eE][-+]?\d+)?$/.test(input.value.trim());
    input.addEventListener('input', () => input.classList.toggle('invalid', !valid()));
    input.addEventListener('keydown', (e) => {
      e.stopPropagation();
      if ((e.key === 'Enter' || e.key === 'Tab') && !valid()) {
        e.preventDefault();
        toast(`${this.columns[c]} needs a number`, 'error');
        return;
      }
      if (e.key === 'Enter') {
        e.preventDefault();
        this.commitEdit();
        this.setCursor(r + 1, c);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        this.cancelEdit();
      } else if (e.key === 'Tab') {
        e.preventDefault();
        this.commitEdit();
        this.setCursor(r, c + (e.shiftKey ? -1 : 1));
      }
    });
    input.addEventListener('blur', () => this.editing === input && (valid() ? this.commitEdit() : this.cancelEdit()));
  }

  private commitEdit() {
    const input = this.editing;
    if (!input) return;
    this.editing = null;
    const { r, c } = this.cur;
    const old = this.value(r, c);
    const nv = input.value;
    if (!(old === null && nv === '') && String(old ?? '') !== nv) this.setValue(r, c, nv);
    else this.refreshCell(r, c);
  }

  private cancelEdit() {
    this.editing = null;
    this.refreshCell(this.cur.r, this.cur.c);
    this.scroll.focus();
  }

  setValue(r: number, c: number, v: unknown) {
    const col = this.columns[c];
    const e = this.edits.get(r) ?? { set: {} };
    if (!this.inserted.has(r) && this.rows[r][c] === v) delete e.set[col];
    else e.set[col] = v;
    if (Object.keys(e.set).length || this.inserted.has(r)) this.edits.set(r, e);
    else this.edits.delete(r);
    this.refreshCell(r, c);
    this.opt.onChange?.();
  }

  private refreshCell(r: number, c: number) {
    const td = this.cellEl(r, c);
    if (!td) return;
    const v = this.value(r, c);
    td.classList.remove('editing');
    td.textContent = v === '' ? '(empty)' : cellText(v);
    td.classList.toggle('null', v === null || v === undefined);
    td.classList.toggle('empty', v === '');
    td.classList.toggle('changed', !!this.edits.get(r) && this.columns[c] in this.edits.get(r)!.set);
    td.classList.toggle('cur', r === this.cur.r && c === this.cur.c);
  }

  insertRow() {
    if (!this.opt.editable) return;
    this.rows.push(this.columns.map(() => null));
    const r = this.rows.length - 1;
    this.inserted.add(r);
    this.edits.set(r, { set: {} });
    this.render();
    this.setCursor(r, 0);
    this.opt.onChange?.();
  }

  deleteRows() {
    for (const r of this.selected) {
      if (this.inserted.has(r)) continue;
      this.deleted.has(r) ? this.deleted.delete(r) : this.deleted.add(r);
    }
    this.render();
    this.opt.onChange?.();
  }

  discard() {
    const keep = this.rows.filter((_, i) => !this.inserted.has(i));
    this.set(this.columns, this.types, keep);
    this.opt.onChange?.();
  }

  /** changes returns the queued edits as row changes. */
  changes(): db.RowChange[] {
    const out: db.RowChange[] = [];
    const where = (r: number) => {
      const keys = this.opt.pk?.length ? this.opt.pk : this.columns;
      return Object.fromEntries(keys.map((k) => [k, this.rows[r][this.columns.indexOf(k)]]));
    };
    for (const r of [...this.deleted].sort((a, b) => a - b)) out.push({ op: 'delete', where: where(r), set: {}, order: [] } as any);
    for (const [r, e] of this.edits) {
      if (this.deleted.has(r)) continue;
      if (this.inserted.has(r)) out.push({ op: 'insert', where: {}, set: e.set, order: this.columns } as any);
      else if (Object.keys(e.set).length) out.push({ op: 'update', where: where(r), set: e.set, order: this.columns } as any);
    }
    return out;
  }

  selectedRows(): unknown[][] {
    const rows = this.selected.size ? [...this.selected].sort((a, b) => a - b) : this.cur.r >= 0 ? [this.cur.r] : [];
    return rows.map((r) => this.columns.map((_, c) => this.value(r, c)));
  }

  copyText(format: 'tsv' | 'csv' | 'json' | 'sql' | 'markdown' | 'php' | 'cell'): string {
    if (format === 'cell' || (format === 'tsv' && this.selected.size <= 1 && this.cur.c >= 0)) {
      return cellText(this.value(this.cur.r, this.cur.c));
    }
    return rowsAs(format, this.columns, this.selectedRows(), this.opt.table ?? 'table', this.opt.quote);
  }

  async copy(format: 'tsv' | 'csv' | 'json' | 'sql' | 'markdown' | 'php' | 'cell') {
    await Go.CopyText(this.copyText(format));
    toast('Copied');
  }

  /** copyAll copies every row in a format. */
  async copyAll(format: 'csv' | 'json' | 'sql' | 'markdown' | 'php') {
    await Go.CopyText(rowsAs(format, this.columns, this.rows.map((_, r) => this.columns.map((__, c) => this.value(r, c))), this.opt.table ?? 'table', this.opt.quote));
    toast(`Copied ${this.rows.length} rows as ${format.toUpperCase()}`);
  }

  private menuItems(): MenuItem[] {
    const { r, c } = this.cur;
    const col = this.columns[c];
    const fk = (this.opt.fks ?? []).find((f) => f.columns.includes(col));
    const items: MenuItem[] = [
      { label: 'Copy', shortcut: 'Ctrl+C', action: () => this.copy('tsv') },
      { label: 'Copy rows as', items: [
        { label: 'CSV', action: () => this.copy('csv') },
        { label: 'TSV', action: () => this.copy('tsv') },
        { label: 'JSON', action: () => this.copy('json') },
        { label: 'SQL INSERT', action: () => this.copy('sql') },
        { label: 'Markdown', action: () => this.copy('markdown') },
        { label: 'PHP array', action: () => this.copy('php') },
      ] },
      { label: 'Open in side editor', action: () => this.openSide() },
    ];
    if (fk && this.opt.onFollow) items.push({ label: `Follow ${fk.refTable}`, action: () => this.opt.onFollow!(fk, fk.columns.map((fc) => this.value(r, this.columns.indexOf(fc)))) });
    if (this.opt.onFilter && r >= 0 && c >= 0) {
      const v = this.value(r, c);
      const shown = v === null || v === undefined ? 'NULL' : String(v).length > 30 ? String(v).slice(0, 30) + '…' : String(v);
      const f = this.opt.onFilter;
      items.push({ label: 'Filter', items: v === null || v === undefined
        ? [{ label: `${col} IS NULL`, action: () => f(col, 'IS NULL', null) }, { label: `${col} IS NOT NULL`, action: () => f(col, 'IS NOT NULL', null) }]
        : [{ label: `${col} = ${shown}`, action: () => f(col, '=', v) }, { label: `${col} ≠ ${shown}`, action: () => f(col, '<>', v) }, { label: `${col} IS NULL`, action: () => f(col, 'IS NULL', null) }] });
    }
    if (this.opt.editable) {
      items.push(
        { separator: true, label: '' },
        { label: 'Edit cell', shortcut: 'F2', action: () => this.startEdit() },
        { label: 'Set NULL', action: () => this.setValue(r, c, null) },
        { label: 'Insert row', action: () => this.insertRow() },
        { label: this.selected.size > 1 ? `Delete ${this.selected.size} rows` : 'Delete row', shortcut: 'Del', danger: true, action: () => this.deleteRows() },
      );
    }
    return items;
  }

  /** openSide shows the current cell in a larger editor: CodeMirror for JSON and text, a hex dump for binary. */
  openSide() {
    const { r, c } = this.cur;
    if (r < 0 || c < 0) return;
    this.sideEditor?.destroy();
    this.sideEditor = null;
    const v = this.value(r, c);
    const text = v === null || v === undefined ? '' : String(v);
    let kind = kindOf(this.colType(c));
    if (kind === 'text' && /^\s*[[{]/.test(text)) {
      try { JSON.parse(text); kind = 'json'; } catch { /* plain text that starts with a bracket */ }
    }
    const editable = !!this.opt.editable && kind !== 'blob';
    const status = h('span', { class: 'muted' });
    const applyBtn = editable ? h('button', { type: 'button', class: 'btn small primary', onclick: () => apply() }, 'Apply') : null;
    let ed: ValueEditor | null = null;
    const refresh = () => {
      if (!ed) return;
      const err = ed.error();
      const len = ed.get().length;
      status.textContent = err ? 'Invalid JSON: ' + err : `${len.toLocaleString('en-US')} characters`;
      status.style.color = err ? 'var(--red)' : '';
      if (applyBtn) applyBtn.disabled = !!err;
    };
    const apply = () => {
      if (!ed || ed.error()) return;
      this.setValue(r, c, ed.get());
    };
    let body: HTMLElement;
    if (kind === 'blob') {
      body = h('pre', { class: 'hex-dump' }, text ? hexDump(text) : 'NULL');
    } else {
      ed = valueEditor(kind === 'json' && text ? prettyJSON(text) : text, kind, !editable, refresh);
      this.sideEditor = ed;
      body = ed.el;
    }
    clear(this.side,
      h('div', { class: 'pane-toolbar' }, h('b', {}, this.columns[c]), h('span', { class: 'muted' }, this.colType(c) || kind), h('span', { class: 'spacer' }),
        h('button', { type: 'button', class: 'btn icon', 'aria-label': 'Close side editor', onclick: () => { this.side.hidden = true; this.sideEditor?.destroy(); this.sideEditor = null; } }, icon('x'))),
      body,
      h('div', { class: 'pane-toolbar' },
        kind === 'json' && ed ? h('button', { type: 'button', class: 'btn small', onclick: () => { try { ed!.set(JSON.stringify(JSON.parse(ed!.get()), null, 2)); } catch { toast('Not valid JSON', 'error'); } } }, 'Format') : null,
        kind === 'json' && ed ? h('button', { type: 'button', class: 'btn small', onclick: () => { try { ed!.set(JSON.stringify(JSON.parse(ed!.get()))); } catch { toast('Not valid JSON', 'error'); } } }, 'Minify') : null,
        editable && this.isNullable(c) ? h('button', { type: 'button', class: 'btn small', onclick: () => this.setValue(r, c, null) }, 'Set NULL') : null,
        status,
        h('span', { class: 'spacer' }),
        applyBtn),
    );
    this.side.hidden = false;
    refresh();
  }
}

function prettyJSON(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}
