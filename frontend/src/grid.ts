// Data grid: selection, inline editing with a change queue, copy formats, side editor.
import { h, clear, cellText, rowsAs, icon } from './util';
import { menu, toast, type MenuItem } from './dialogs';
import { Go } from './api';
import type { db } from '../wailsjs/go/models';

export interface GridOptions {
  editable?: boolean;
  pk?: string[];
  fks?: db.ForeignKey[];
  table?: string;
  quote?: (s: string) => string;
  sort?: { col: string; desc: boolean } | null;
  onSort?: (col: string) => void;
  onFollow?: (fk: db.ForeignKey, values: unknown[]) => void;
  onChange?: () => void;
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
    const sort = this.opt.sort;
    const fkCols = new Set((this.opt.fks ?? []).flatMap((f) => f.columns));
    const head = h('tr', {}, h('th', { class: 'rownum' }, '#'), this.columns.map((c, i) => h('th', {
      title: this.opt.onSort ? 'Sort by ' + c : c,
      onclick: () => this.opt.onSort?.(c),
    }, c, sort?.col === c ? h('span', { class: 'sort' }, sort.desc ? '▼' : '▲') : null, this.types[i] ? h('span', { class: 'type' }, this.types[i]) : null)));
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
        td.textContent = cellText(v).length > 300 ? cellText(v).slice(0, 300) + '…' : cellText(v);
        let cls = v === null || v === undefined ? 'null' : isNum[c] ? 'num' : '';
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

  private startEdit(initial?: string) {
    const { r, c } = this.cur;
    if (!this.opt.editable || r < 0 || this.deleted.has(r)) return;
    const td = this.cellEl(r, c);
    if (!td) return;
    const v = this.value(r, c);
    const s = v === null || v === undefined ? '' : String(v);
    if (s.includes('\n') || s.length > 200) return this.openSide();
    const input = h('input', { value: initial ?? s, 'aria-label': this.columns[c] });
    td.classList.add('editing');
    clear(td, input);
    this.editing = input;
    input.focus();
    if (initial === undefined) input.select();
    input.addEventListener('keydown', (e) => {
      e.stopPropagation();
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
    input.addEventListener('blur', () => this.editing === input && this.commitEdit());
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
    td.textContent = cellText(v);
    td.classList.toggle('null', v === null || v === undefined);
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

  copyText(format: 'tsv' | 'csv' | 'json' | 'sql' | 'markdown' | 'cell'): string {
    if (format === 'cell' || (format === 'tsv' && this.selected.size <= 1 && this.cur.c >= 0)) {
      return cellText(this.value(this.cur.r, this.cur.c));
    }
    return rowsAs(format, this.columns, this.selectedRows(), this.opt.table ?? 'table', this.opt.quote);
  }

  async copy(format: 'tsv' | 'csv' | 'json' | 'sql' | 'markdown' | 'cell') {
    await Go.CopyText(this.copyText(format));
    toast('Copied');
  }

  /** copyAll copies every row in a format. */
  async copyAll(format: 'csv' | 'json' | 'sql' | 'markdown') {
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
      ] },
      { label: 'Open in side editor', action: () => this.openSide() },
    ];
    if (fk && this.opt.onFollow) items.push({ label: `Follow ${fk.refTable}`, action: () => this.opt.onFollow!(fk, fk.columns.map((fc) => this.value(r, this.columns.indexOf(fc)))) });
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

  /** openSide shows the current cell in a larger editor (JSON and long text). */
  openSide() {
    const { r, c } = this.cur;
    if (r < 0 || c < 0) return;
    const v = this.value(r, c);
    const ta = h('textarea', { class: 'input', readOnly: !this.opt.editable, spellcheck: 'false', 'aria-label': 'Value of ' + this.columns[c] });
    ta.value = v === null || v === undefined ? '' : String(v);
    const isJSON = /^\s*[[{]/.test(ta.value);
    const apply = () => this.setValue(r, c, ta.value);
    clear(this.side,
      h('div', { class: 'pane-toolbar' }, h('b', {}, this.columns[c]), h('span', { class: 'spacer' }),
        h('button', { type: 'button', class: 'btn icon', 'aria-label': 'Close side editor', onclick: () => { this.side.hidden = true; } }, icon('x'))),
      ta,
      h('div', { class: 'pane-toolbar' },
        isJSON ? h('button', { type: 'button', class: 'btn small', onclick: () => {
          try { ta.value = JSON.stringify(JSON.parse(ta.value), null, 2); } catch { toast('Not valid JSON', 'error'); }
        } }, 'Format JSON') : null,
        this.opt.editable ? h('button', { type: 'button', class: 'btn small', onclick: () => this.setValue(r, c, null) }, 'Set NULL') : null,
        h('span', { class: 'spacer' }),
        this.opt.editable ? h('button', { type: 'button', class: 'btn small primary', onclick: apply }, 'Apply') : null),
    );
    this.side.hidden = false;
  }
}
