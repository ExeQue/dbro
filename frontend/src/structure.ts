// The Structure tab: view and edit columns, indexes and foreign keys,
// create new tables, and show routine sources.
import { h, clear, icon } from './util';
import { Go, forConn } from './api';
import { toast, errorMessage, prompt, field, modal } from './dialogs';
import { dialect, type Selection } from './state';
import { runSchema } from './actions';
import { db } from '../wailsjs/go/models';

interface ColRow {
  orig?: db.Column; // undefined for new columns
  col: db.Column;
  dropped: boolean;
}

const commonTypes: Record<string, string[]> = {
  mysql: ['bigint unsigned', 'int', 'tinyint(1)', 'varchar(255)', 'text', 'longtext', 'decimal(10,2)', 'double', 'date', 'datetime', 'timestamp', 'json', 'blob'],
  postgres: ['bigint', 'integer', 'boolean', 'varchar(255)', 'text', 'numeric(10,2)', 'double precision', 'date', 'timestamp', 'timestamptz', 'jsonb', 'uuid', 'bytea'],
  sqlite: ['INTEGER', 'TEXT', 'REAL', 'NUMERIC', 'BLOB'],
};

export class StructureTab {
  el = h('div', { class: 'pane' });
  private sel: Selection | null = null;
  private def: db.TableDef | null = null;
  private rows: ColRow[] = [];
  private order: string[] = []; // original order
  private isNew = false;
  private info: db.InfoItem[] = [];
  private before: Selection | null = null; // selection to return to when a new table is canceled
  private newName = h('input', { class: 'input', placeholder: 'table_name', 'aria-label': 'New table name' });

  get dirty() {
    return this.ops().length > 0;
  }

  async show(sel: Selection | null, force = false) {
    if (!force && this.sel && sel && same(this.sel, sel) && !this.isNew) return;
    this.sel = sel;
    this.isNew = false;
    if (!sel) return clear(this.el, h('div', { class: 'empty' }, 'Select a table to see its structure.'));
    if (sel.kind === 'routine') return this.showRoutine(sel);
    if (sel.kind !== 'table' && sel.kind !== 'view') return clear(this.el, h('div', { class: 'empty' }, 'Select a table to see its structure.', sel.kind === 'database' ? h('button', { type: 'button', class: 'btn', onclick: () => this.newTable(sel) }, icon('plus'), 'New table') : null));
    clear(this.el, h('div', { class: 'empty' }, 'Loading…'));
    try {
      this.def = await forConn(sel.conn, () => Go.Describe(sel.conn, sel.db!, sel.schema ?? '', sel.table!));
      this.info = await Go.TableInfo(sel.conn, sel.db!, sel.schema ?? '', sel.table!).catch(() => []);
    } catch (e) {
      return clear(this.el, h('div', { class: 'result-error' }, errorMessage(e)));
    }
    this.reset();
  }

  /** newTable starts an empty definition with an id column. */
  newTable(sel: Selection) {
    this.before = sel;
    this.sel = { ...sel, kind: 'table', table: undefined };
    this.isNew = true;
    const d = dialect(sel.conn);
    this.def = db.TableDef.createFrom({ schema: sel.schema ?? (d === 'postgres' ? 'public' : ''), name: '', isView: false, columns: [], indexes: [], foreignKeys: [], create: '' });
    this.rows = [{ col: db.Column.createFrom({ name: 'id', type: d === 'mysql' ? 'bigint unsigned' : d === 'sqlite' ? 'INTEGER' : 'bigint', nullable: false, key: 'PRI', autoIncrement: true, comment: '' }), dropped: false }];
    this.order = [];
    this.newName.value = '';
    this.render();
    setTimeout(() => this.newName.focus(), 0);
  }

  private reset() {
    this.rows = (this.def?.columns ?? []).map((c) => ({ orig: c, col: db.Column.createFrom({ ...c }), dropped: false }));
    this.order = this.rows.map((r) => r.col.name);
    this.render();
  }

  private render() {
    const sel = this.sel!;
    const def = this.def!;
    const d = dialect(sel.conn);
    const ro = def.isView;
    const types = h('datalist', { id: 'coltypes' }, commonTypes[d].map((t) => h('option', { value: t })));
    const pk = new Set(def.indexes?.find((i) => i.primary)?.columns ?? []);
    const colTable = h('table', { class: 'sgrid' },
      h('tr', {}, ['#', 'Name', 'Type', 'Null', 'Default', 'Auto', 'Comment', ''].map((t) => h('th', {}, t))),
      this.rows.map((r, i) => {
        const changed = r.orig && JSON.stringify(strip(r.orig)) !== JSON.stringify(strip(r.col));
        const moved = r.orig && this.position(r) !== null;
        const tr = h('tr', { class: !r.orig ? 'new' : r.dropped ? 'dropped' : changed || moved ? 'dirty' : '' });
        const inp = (key: 'name' | 'type' | 'comment', attrs: Record<string, any> = {}) => {
          const el = h('input', { class: 'input', value: r.col[key] ?? '', readOnly: ro || r.dropped, 'aria-label': key + ' of ' + r.col.name, spellcheck: 'false', ...attrs });
          el.addEventListener('input', () => { (r.col as any)[key] = el.value; this.mark(tr, r); });
          return el;
        };
        const def = h('input', { class: 'input', value: r.col.default ?? '', placeholder: r.col.default === null || r.col.default === undefined ? 'none' : '', readOnly: ro || r.dropped, 'aria-label': 'default of ' + r.col.name, spellcheck: 'false', title: "SQL expression: 'text', 0, CURRENT_TIMESTAMP, NULL" });
        def.addEventListener('input', () => { r.col.default = def.value === '' ? undefined : def.value; this.mark(tr, r); });
        const chk = (key: 'nullable' | 'autoIncrement') => {
          const el = h('input', { type: 'checkbox', checked: !!r.col[key], disabled: ro || r.dropped, 'aria-label': key + ' ' + r.col.name });
          el.addEventListener('change', () => { (r.col as any)[key] = el.checked; this.mark(tr, r); });
          return el;
        };
        append(tr,
          h('td', { class: 'muted' }, String(i + 1), pk.has(r.orig?.name ?? '') ? h('span', { class: 'key', title: 'primary key' }, ' ', icon('key', 11)) : null),
          h('td', {}, inp('name')),
          h('td', {}, inp('type', { list: 'coltypes', class: 'input type' })),
          h('td', {}, chk('nullable')),
          h('td', {}, def),
          h('td', {}, chk('autoIncrement')),
          h('td', {}, inp('comment')),
          h('td', { style: { whiteSpace: 'nowrap' } }, ro ? null : [
            d !== 'postgres' || !r.orig ? h('button', { type: 'button', class: 'btn icon small', 'aria-label': 'Move up', disabled: i === 0, onclick: () => this.move(i, -1) }, icon('up', 12)) : null,
            d !== 'postgres' || !r.orig ? h('button', { type: 'button', class: 'btn icon small', 'aria-label': 'Move down', disabled: i === this.rows.length - 1, onclick: () => this.move(i, 1) }, icon('down', 12)) : null,
            h('button', { type: 'button', class: 'btn icon small danger', 'aria-label': r.dropped ? 'Keep column' : 'Drop column', title: r.dropped ? 'Keep' : 'Drop', onclick: () => {
              if (!r.orig) this.rows.splice(i, 1);
              else r.dropped = !r.dropped;
              this.render();
            } }, r.dropped ? '↺' : icon('x', 12)),
          ]),
        );
        return tr;
      }),
    );
    const dirty = this.ops().length;
    const head = this.isNew
      ? h('div', { class: 'pane-toolbar' }, field('New table', this.newName), h('span', { class: 'spacer' }),
          h('button', { type: 'button', class: 'btn', onclick: () => this.show(this.before, true) }, 'Cancel'),
          h('button', { type: 'button', class: 'btn primary', onclick: () => this.save() }, 'Preview SQL and create'))
      : h('div', { class: 'pane-toolbar' }, h('b', {}, def.name), def.isView ? h('span', { class: 'muted' }, 'view (read-only)') : null, h('span', { class: 'spacer' }),
          ro ? null : h('button', { type: 'button', class: 'btn', onclick: () => this.addColumn() }, icon('plus'), 'Column'),
          ro ? null : h('button', { type: 'button', class: 'btn', onclick: () => this.addIndex() }, icon('plus'), 'Index'),
          ro ? null : h('button', { type: 'button', class: 'btn', onclick: () => this.addFK() }, icon('plus'), 'Foreign key'),
          h('button', { type: 'button', class: 'btn', onclick: () => this.showCreate() }, 'Show CREATE'),
          ro ? null : h('button', { type: 'button', class: 'btn', disabled: !dirty, onclick: () => this.reset() }, 'Reset'),
          ro ? null : h('button', { type: 'button', class: 'btn primary', disabled: !dirty, onclick: () => this.save() }, 'Preview SQL and save', h('span', { class: 'shortcut' }, 'Ctrl+S')));
    const infoBlock = !this.isNew && this.info.length
      ? h('details', { class: 'table-info', open: true },
          h('summary', {}, 'Table info'),
          h('dl', {}, this.info.map((it) => h('div', {}, h('dt', {}, it.label), h('dd', {}, it.value)))))
      : null;
    const body = h('div', { class: 'structure' }, types, infoBlock, h('h3', {}, 'Columns'), colTable);
    if (this.isNew) {
      body.append(h('p', { class: 'muted' }, 'Add columns, then create. Indexes and foreign keys can be added once the table exists.'), h('button', { type: 'button', class: 'btn', onclick: () => this.addColumn() }, icon('plus'), 'Column'));
    } else {
      body.append(
        h('h3', {}, 'Indexes'),
        h('table', { class: 'sgrid' }, h('tr', {}, ['Name', 'Columns', 'Type', ''].map((t) => h('th', {}, t))),
          (def.indexes ?? []).map((ix) => h('tr', {}, h('td', {}, ix.name), h('td', {}, ix.columns.join(', ')), h('td', { class: 'key' }, ix.primary ? 'PRIMARY' : ix.unique ? 'UNIQUE' : 'INDEX'),
            h('td', {}, ro ? null : h('button', { type: 'button', class: 'btn small danger', onclick: () => this.drop({ kind: 'drop_index', name: ix.name }, `Drop index ${ix.name}?`) }, 'Drop'))))),
        h('h3', {}, 'Foreign keys'),
        (def.foreignKeys ?? []).length ? h('table', { class: 'sgrid' }, h('tr', {}, ['Name', 'Columns', 'References', 'On update', 'On delete', ''].map((t) => h('th', {}, t))),
          def.foreignKeys.map((fk) => h('tr', {}, h('td', {}, fk.name), h('td', {}, fk.columns.join(', ')), h('td', {}, `${fk.refSchema ? fk.refSchema + '.' : ''}${fk.refTable} (${fk.refColumns.join(', ')})`), h('td', {}, fk.onUpdate), h('td', {}, fk.onDelete),
            h('td', {}, ro ? null : h('button', { type: 'button', class: 'btn small danger', onclick: () => this.drop({ kind: 'drop_fk', name: fk.name }, `Drop foreign key ${fk.name}?`) }, 'Drop'))))) : h('p', { class: 'muted' }, 'None.'),
      );
    }
    clear(this.el, head, body);
  }

  private mark(tr: HTMLElement, r: ColRow) {
    const changed = r.orig && (JSON.stringify(strip(r.orig)) !== JSON.stringify(strip(r.col)) || this.position(r) !== null);
    tr.className = !r.orig ? 'new' : r.dropped ? 'dropped' : changed ? 'dirty' : '';
    const save = this.el.querySelector<HTMLButtonElement>('.pane-toolbar .btn.primary');
    if (save && !this.isNew) save.disabled = !this.ops().length;
    const reset = [...this.el.querySelectorAll<HTMLButtonElement>('.pane-toolbar .btn')].find((b) => b.textContent === 'Reset');
    if (reset) reset.disabled = !this.ops().length;
  }

  private move(i: number, dir: number) {
    const j = i + dir;
    [this.rows[i], this.rows[j]] = [this.rows[j], this.rows[i]];
    this.render();
  }

  private addColumn() {
    const d = dialect(this.sel!.conn);
    this.rows.push({ col: db.Column.createFrom({ name: '', type: d === 'mysql' ? 'varchar(255)' : d === 'sqlite' ? 'TEXT' : 'text', nullable: true, key: '', autoIncrement: false, comment: '' }), dropped: false });
    this.render();
    const inputs = this.el.querySelectorAll<HTMLInputElement>('.sgrid tr:last-child input.input');
    inputs[0]?.focus();
  }

  /** position returns the AFTER target if a kept column moved relative to the original order, else null. */
  private position(r: ColRow): string | null {
    const kept = this.rows.filter((x) => !x.dropped);
    const i = kept.indexOf(r);
    const prevNow = i > 0 ? kept[i - 1].orig?.name ?? kept[i - 1].col.name : '';
    const origKept = this.order.filter((n) => kept.some((x) => x.orig?.name === n));
    const oi = origKept.indexOf(r.orig!.name);
    const prevOrig = oi > 0 ? origKept[oi - 1] : '';
    return prevNow === prevOrig ? null : prevNow;
  }

  /** ops turns the edited column list into schema operations. */
  private ops(): db.SchemaOp[] {
    if (!this.sel || !this.def || this.isNew) return [];
    const base = { schema: this.def.schema, table: this.def.name };
    const out: db.SchemaOp[] = [];
    const kept = this.rows.filter((r) => !r.dropped);
    for (const r of this.rows) if (r.dropped && r.orig) out.push({ ...base, kind: 'drop_column', name: r.orig.name } as db.SchemaOp);
    kept.forEach((r, i) => {
      const prev = i > 0 ? kept[i - 1].col.name : '';
      if (!r.orig) {
        if (!r.col.name.trim()) return;
        const last = i === kept.length - 1;
        out.push({ ...base, kind: 'add_column', column: clean(r.col), after: last ? undefined : prev } as db.SchemaOp);
        return;
      }
      const moved = this.position(r) !== null;
      if (moved || JSON.stringify(strip(r.orig)) !== JSON.stringify(strip(r.col))) {
        out.push({ ...base, kind: 'modify_column', name: r.orig.name, column: clean(r.col), after: moved ? prev : undefined } as db.SchemaOp);
      }
    });
    return out;
  }

  async save() {
    const sel = this.sel;
    if (!sel || !this.def) return;
    if (this.isNew) {
      const name = this.newName.value.trim();
      if (!name) return toast('Give the table a name', 'error');
      const cols = this.rows.filter((r) => r.col.name.trim()).map((r) => clean(r.col));
      const pk = this.rows.filter((r) => r.col.key === 'PRI').map((r) => r.col.name);
      const def = { ...this.def, name, columns: cols, indexes: pk.length ? [{ name: 'PRIMARY', columns: pk, unique: true, primary: true }] : [] };
      if (await runSchema(sel, [{ kind: 'create_table', schema: this.def.schema, def } as any])) {
        this.isNew = false;
        const target = { ...sel, kind: 'table' as const, table: name };
        const { revealTable } = await import('./main');
        revealTable(target);
      }
      return;
    }
    const ops = this.ops();
    if (!ops.length) return;
    if (await runSchema(sel, ops)) this.show(sel, true);
  }

  private async drop(op: Partial<db.SchemaOp>, question: string) {
    if (await runSchema(this.sel!, [{ ...op, schema: this.def!.schema, table: this.def!.name } as db.SchemaOp], question)) this.show(this.sel, true);
  }

  private async addIndex() {
    const cols = this.def!.columns.map((c) => c.name);
    const checks = cols.map((c) => h('input', { type: 'checkbox', 'data-col': c }));
    const kind = h('select', { class: 'input' }, h('option', { value: 'index' }, 'INDEX'), h('option', { value: 'unique' }, 'UNIQUE'), h('option', { value: 'primary' }, 'PRIMARY KEY'));
    const name = h('input', { class: 'input', placeholder: 'generated if empty' });
    const order: string[] = [];
    checks.forEach((c) => c.addEventListener('change', () => {
      const col = c.dataset.col!;
      if (c.checked) order.push(col);
      else order.splice(order.indexOf(col), 1);
    }));
    const r = await modal('New index', h('div', { class: 'form' }, h('div', { class: 'form-row' }, field('Type', kind), field('Name', name)),
      h('div', {}, h('span', { class: 'muted' }, 'Columns, in the order you tick them:'), h('div', { style: { display: 'grid', gap: '4px', marginTop: '6px' } }, checks.map((c, i) => h('label', { class: 'check' }, c, cols[i]))))),
      [{ label: 'Cancel' }, { label: 'Next', primary: true, action: () => { if (!order.length) throw new Error('Pick at least one column'); } }]);
    if (r !== 1) return;
    const ix = { name: name.value.trim(), columns: order, unique: kind.value !== 'index', primary: kind.value === 'primary' };
    if (await runSchema(this.sel!, [{ kind: 'add_index', schema: this.def!.schema, table: this.def!.name, index: ix } as any])) this.show(this.sel, true);
  }

  private async addFK() {
    const sel = this.sel!;
    const tables = await forConn(sel.conn, () => Go.DatabaseObjects(sel.conn, sel.db!)).then((o) => o.tables.filter((t) => t.type !== 'view')).catch(() => []);
    const col = h('select', { class: 'input' }, this.def!.columns.map((c) => h('option', { value: c.name }, c.name)));
    const ref = h('select', { class: 'input' }, tables.map((t) => h('option', { value: JSON.stringify([t.schema, t.name]) }, t.name)));
    const refCol = h('select', { class: 'input' });
    const loadRef = async () => {
      const [schema, table] = JSON.parse(ref.value || '["",""]');
      const d = await forConn(sel.conn, () => Go.Describe(sel.conn, sel.db!, schema, table)).catch(() => null);
      const pk = d?.indexes?.find((i) => i.primary)?.columns?.[0];
      clear(refCol, (d?.columns ?? []).map((c) => h('option', { value: c.name, selected: c.name === pk }, c.name)));
    };
    ref.addEventListener('change', loadRef);
    const guess = this.def!.columns.find((c) => c.name.endsWith('_id'));
    if (guess) {
      col.value = guess.name;
      const t = tables.find((t) => t.name === guess.name.slice(0, -3) + 's' || t.name === guess.name.slice(0, -3));
      if (t) ref.value = JSON.stringify([t.schema, t.name]);
    }
    await loadRef();
    const actions = ['NO ACTION', 'CASCADE', 'SET NULL', 'RESTRICT'];
    const onDelete = h('select', { class: 'input' }, actions.map((a) => h('option', {}, a)));
    const onUpdate = h('select', { class: 'input' }, actions.map((a) => h('option', {}, a)));
    const name = h('input', { class: 'input', placeholder: 'generated if empty' });
    const r = await modal('New foreign key', h('div', { class: 'form' },
      h('div', { class: 'form-row' }, field('Column', col), field('References table', ref), field('Column', refCol)),
      h('div', { class: 'form-row' }, field('On delete', onDelete), field('On update', onUpdate), field('Name', name))),
      [{ label: 'Cancel' }, { label: 'Next', primary: true }], { wide: true });
    if (r !== 1) return;
    const [refSchema, refTable] = JSON.parse(ref.value);
    const fk = { name: name.value.trim(), columns: [col.value], refSchema: refSchema === this.def!.schema ? '' : refSchema, refTable, refColumns: [refCol.value], onDelete: onDelete.value, onUpdate: onUpdate.value };
    if (await runSchema(sel, [{ kind: 'add_fk', schema: this.def!.schema, table: this.def!.name, fk } as any])) this.show(sel, true);
  }

  private async showCreate() {
    const sql = this.def?.create ?? '';
    await modal(`CREATE ${this.def?.name}`, h('pre', { class: 'sql-preview' }, sql), [{ label: 'Copy', action: () => { Go.CopyText(sql); toast('Copied'); } }, { label: 'Close', primary: true }], { wide: true });
  }

  private async showRoutine(sel: Selection) {
    clear(this.el, h('div', { class: 'empty' }, 'Loading…'));
    try {
      const src = await forConn(sel.conn, () => Go.RoutineSource(sel.conn, sel.db!, sel.routine as any));
      clear(this.el,
        h('div', { class: 'pane-toolbar' }, h('b', {}, sel.routine!.name), h('span', { class: 'muted' }, sel.routine!.type), h('span', { class: 'spacer' }),
          h('button', { type: 'button', class: 'btn', onclick: () => { Go.CopyText(src); toast('Copied'); } }, 'Copy'),
          h('button', { type: 'button', class: 'btn', onclick: async () => { const { openQuery } = await import('./main'); openQuery(sel, src); } }, 'Open in query tab')),
        h('div', { class: 'structure' }, h('pre', { class: 'create-sql' }, src)));
    } catch (e) {
      clear(this.el, h('div', { class: 'result-error' }, errorMessage(e)));
    }
  }
}

function strip(c: db.Column) {
  return { name: c.name, type: c.type, nullable: !!c.nullable, default: c.default ?? null, autoIncrement: !!c.autoIncrement, comment: c.comment ?? '' };
}

function clean(c: db.Column): db.Column {
  return db.Column.createFrom({ ...strip(c), default: c.default === '' ? undefined : c.default ?? undefined, key: c.key ?? '' });
}

function same(a: Selection, b: Selection) {
  return a.conn === b.conn && a.db === b.db && a.schema === b.schema && a.table === b.table && a.kind === b.kind && a.routine?.name === b.routine?.name;
}

function append(el: HTMLElement, ...children: (Node | null | (Node | null)[])[]) {
  for (const c of children.flat()) if (c) el.appendChild(c);
}

export { prompt };
