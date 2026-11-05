// A query tab: CodeMirror SQL editor with autocomplete, results, history and snippets.
import { EditorView, keymap, placeholder } from '@codemirror/view';
import { EditorState, Compartment, Prec } from '@codemirror/state';
import { basicSetup } from 'codemirror';
import { sql, MySQL, PostgreSQL, SQLite, MariaSQL, keywordCompletionSource, schemaCompletionSource, type SQLDialect } from '@codemirror/lang-sql';
import { autocompletion, type CompletionContext, type CompletionSource } from '@codemirror/autocomplete';
import { syntaxHighlighting } from '@codemirror/language';
import { highlight } from './highlight';
import { prettify, minify } from './sqlformat';
import { h, clear, icon, nextId, formatCount, debounce } from './util';
import { Go, forConn } from './api';
import { Grid, nextSort } from './grid';
import { dropdown, toast, prompt, modal, errorMessage, type MenuItem } from './dialogs';
import { connInfo, dialect, quoteIdent, type Selection } from './state';
import type { db } from '../wailsjs/go/models';



export class QueryTab {
  id = nextId('q');
  el = h('div', { class: 'query' });
  title: string;
  conn = '';
  database = '';
  private view: EditorView;
  private lang = new Compartment();
  private complete = new Compartment();
  private columns: string[] = [];
  private results = h('div', { class: 'results' });
  private status = h('span', { class: 'muted' });
  private runBtn: HTMLButtonElement;
  private stopBtn: HTMLButtonElement;
  private target = h('span', { class: 'muted' });
  private running = false;
  private grids: Grid[] = [];
  private params = h('div', { class: 'params', hidden: true });
  private names: string[] = [];
  private values = new Map<string, string | null>();
  private bind = h('input', { type: 'checkbox', checked: true, 'aria-label': 'Bind parameters' });

  constructor(n: number, sel: Selection | null, text = "") {
    this.title = `Query ${n}`;
    this.runBtn = h('button', { type: 'button', class: 'btn primary', onclick: () => this.run() }, icon('play', 12), 'Run', h('span', { class: 'shortcut' }, 'Ctrl+Enter'));
    this.stopBtn = h('button', { type: 'button', class: 'btn', disabled: true, onclick: () => Go.CancelQuery(this.id) }, icon('stop', 12), 'Stop');
    this.view = new EditorView({
      parent: h('div'),
      state: EditorState.create({
        doc: text,
        extensions: [
          Prec.highest(keymap.of([
            { key: 'Mod-Enter', run: () => (this.run(), true) },
            { key: 'Shift-Mod-Enter', run: () => (this.explain(), true) },
            { key: 'Shift-Mod-f', run: () => (this.reformat(false), true) },
          ])),
          basicSetup,
          this.lang.of(sql()),
          Prec.highest(this.complete.of(autocompletion({ override: this.sources(MySQL, {}) }))),
          syntaxHighlighting(highlight),
          placeholder('SELECT … — Ctrl+Enter runs the selection, or everything when nothing is selected'),
          EditorView.lineWrapping,
          EditorView.updateListener.of((u) => u.docChanged && this.detect()),
        ],
      }),
    });
    const editorBox = h('div', { class: 'editor' }, this.view.dom);
    const split = h('div', { class: 'hsplit', role: 'separator', 'aria-orientation': 'horizontal' });
    resizeRows(split, editorBox);
    this.el.append(
      h('div', { class: 'pane-toolbar' }, this.runBtn, this.stopBtn,
        h('button', { type: 'button', class: 'btn', onclick: () => this.explain() }, 'Explain'),
        h('button', { type: 'button', class: 'btn', title: 'Prettify (Ctrl+Shift+F)', onclick: () => this.reformat(false) }, 'Prettify'),
        h('button', { type: 'button', class: 'btn', title: 'Minify', onclick: () => this.reformat(true) }, 'Minify'),
        h('button', { type: 'button', class: 'btn', onclick: () => this.showHistory() }, icon('history', 12), 'History'),
        dropdown('Snippets', () => this.snippetMenu()),
        h('span', { class: 'spacer' }), this.target),
      this.params, editorBox, split, this.results);
    this.bind.addEventListener('change', () => this.renderParams());
    this.detect();
    this.useSelection(sel);
    clear(this.results, h('div', { class: 'empty' }, 'Results appear here.'));
  }

  /** useSelection points the tab at the selected connection and database (until it has run). */
  useSelection(sel: Selection | null) {
    if (this.running || !sel) return;
    const db = sel.db || connInfo(sel.conn)?.database || (dialect(sel.conn) === 'sqlite' ? 'main' : '');
    if (sel.conn === this.conn && db === this.database) return;
    if (this.conn && (this.conn !== sel.conn || this.database !== db)) Go.CloseQueryTab(this.conn, this.database, this.id);
    this.conn = sel.conn;
    this.database = db ?? '';
    const d = dialect(sel.conn);
    const driver = connInfo(sel.conn)?.driver;
    const dial = d === 'postgres' ? PostgreSQL : d === 'sqlite' ? SQLite : driver === 'mariadb' ? MariaSQL : MySQL;
    this.target.textContent = `${this.conn}${this.database ? ' / ' + this.database : ''}`;
    this.view.dispatch({ effects: [this.lang.reconfigure(sql({ dialect: dial, upperCaseKeywords: true })), this.complete.reconfigure(autocompletion({ override: this.sources(dial, {}) }))] });
    if (this.database) {
      forConn(this.conn, () => Go.Completions(this.conn, this.database)).then((schema) => {
        this.columns = [...new Set(Object.values(schema).flat())].sort();
        this.view.dispatch({ effects: [this.lang.reconfigure(sql({ dialect: dial, schema, upperCaseKeywords: true })), this.complete.reconfigure(autocompletion({ override: this.sources(dial, schema) }))] });
      }).catch(() => {});
    }
  }

  focus() {
    this.view.focus();
  }

  /** text returns the selection, or everything. */
  private text() {
    const s = this.view.state.selection.main;
    return s.empty ? this.view.state.doc.toString() : this.view.state.sliceDoc(s.from, s.to);
  }

  /** sources completes :parameters after a colon and SQL keywords and schema everywhere else. */
  private sources(dial: SQLDialect, schema: Record<string, string[]>): CompletionSource[] {
    const param = (ctx: CompletionContext) => ctx.matchBefore(/(^|[^:\w]):\w*$/);
    const guard = (src: CompletionSource): CompletionSource => (ctx) => (param(ctx) ? null : src(ctx));
    const params: CompletionSource = (ctx) => {
      const m = param(ctx);
      if (!m) return null;
      const from = m.from + m.text.indexOf(':');
      const typed = ctx.state.sliceDoc(from + 1, ctx.pos);
      const used = this.names.filter((n) => !/^\d+$/.test(n) && n !== typed);
      const options = [
        ...used.map((n) => ({ label: ':' + n, type: 'variable', detail: 'parameter', boost: 2 })),
        ...this.columns.filter((c) => !used.includes(c)).map((c) => ({ label: ':' + c, type: 'property', detail: 'column' })),
      ];
      return { from, options, validFor: /^:\w*$/ };
    };
    return [params, guard(keywordCompletionSource(dial, true)), guard(schemaCompletionSource({ dialect: dial, schema }))];
  }

  /** detect asks the backend which :name and ? parameters the script uses. */
  private detect = debounce(async () => {
    const names = await Go.Placeholders(this.view.state.doc.toString()).catch(() => []);
    if (names.join('\u0000') === this.names.join('\u0000')) return;
    this.names = names;
    this.renderParams();
  }, 300);

  private renderParams() {
    this.params.hidden = this.names.length === 0;
    if (!this.names.length) return;
    clear(this.params,
      h('label', { class: 'check params-toggle', title: 'Send the values as prepared statement parameters' }, this.bind, ' Parameters'),
      this.bind.checked ? this.names.map((n) => {
        const label = /^\d+$/.test(n) ? '?' + n : ':' + n;
        const v = this.values.get(n);
        const input = h('input', { class: 'input', value: v ?? '', placeholder: v === null ? 'NULL' : '', 'aria-label': 'Value for ' + label, spellcheck: 'false', disabled: v === null });
        input.addEventListener('input', () => this.values.set(n, input.value));
        input.addEventListener('keydown', (e) => {
          if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) this.run();
        });
        const nul = h('button', { type: 'button', class: 'btn small' + (v === null ? ' on' : ''), 'aria-pressed': String(v === null), title: 'Bind NULL', onclick: () => {
          this.values.set(n, this.values.get(n) === null ? '' : null);
          this.renderParams();
        } }, 'NULL');
        return h('div', { class: 'param' }, h('code', {}, label), input, nul);
      }) : h('span', { class: 'muted' }, 'Off: the SQL runs exactly as written.'),
    );
  }

  /** paramValues returns the values to bind, or null when binding is off or not needed. */
  private paramValues(): Record<string, string | null> | null {
    if (!this.bind.checked || !this.names.length) return null;
    return Object.fromEntries(this.names.map((n) => [n, this.values.has(n) ? this.values.get(n)! : '']));
  }

  /** reformat prettifies or minifies the selection, or everything when nothing is selected. */
  private reformat(min: boolean) {
    const sel = this.view.state.selection.main;
    const from = sel.empty ? 0 : sel.from, to = sel.empty ? this.view.state.doc.length : sel.to;
    const text = this.view.state.sliceDoc(from, to);
    if (!text.trim()) return;
    try {
      const out = min ? minify(text) : prettify(text, this.conn ? dialect(this.conn) : 'mysql');
      this.view.dispatch({ changes: { from, to, insert: out } });
    } catch (e) {
      toast('Could not format: ' + errorMessage(e), 'error');
    }
  }

  setText(text: string) {
    this.view.dispatch({ changes: { from: 0, to: this.view.state.doc.length, insert: text } });
  }

  async run() {
    const script = this.text().trim();
    if (!script || this.running) return;
    if (!this.conn) return toast('Select a connection in the tree first', 'error');
    this.setRunning(true);
    const started = performance.now();
    try {
      const run = await forConn(this.conn, () => Go.RunQuery(this.conn, this.database, this.id, script, 0, this.paramValues() as any));
      this.showResults(run.results ?? [], run.error, performance.now() - started);
    } catch (e) {
      this.showResults([], errorMessage(e), performance.now() - started);
    } finally {
      this.setRunning(false);
    }
  }

  async explain() {
    const script = this.text().trim();
    if (!script || !this.conn) return;
    try {
      const r = await forConn(this.conn, () => Go.Explain(this.conn, this.database, script));
      this.showResults([r], '', r.durationMs);
    } catch (e) {
      this.showResults([], errorMessage(e), 0);
    }
  }

  private setRunning(on: boolean) {
    this.running = on;
    this.runBtn.disabled = on;
    this.stopBtn.disabled = !on;
    this.status.textContent = on ? 'Running…' : '';
  }

  private showResults(results: db.Result[], error: string, ms: number) {
    const sets = results.filter((r) => r.isQuery);
    const tabbar = h('div', { class: 'tabbar' });
    const body = h('div', { class: 'grid-wrap' });
    this.grids = [];
    const summary = results.filter((r) => !r.isQuery).map((r) => `${formatCount(r.affected)} rows affected`).join(', ');
    const info = h('div', { class: 'result-info' }, `${results.length} statement${results.length === 1 ? '' : 's'} in ${ms.toFixed(0)} ms`, summary ? ' · ' + summary : '');
    const show = (i: number) => {
      tabbar.querySelectorAll('.tab').forEach((t, j) => t.classList.toggle('active', i === j));
      const r = sets[i];
      const g = this.grids[i];
      clear(body,
        h('div', { class: 'pane-toolbar' },
          h('span', { class: 'muted' }, `${formatCount(r.rows?.length ?? 0)} rows${r.truncated ? ' (cut)' : ''} · ${r.durationMs.toFixed(1)} ms`),
          h('span', { class: 'spacer' }),
          dropdown('Copy as', () => (['csv', 'json', 'sql', 'markdown'] as const).map((f) => ({ label: f === 'sql' ? 'SQL INSERT' : f.toUpperCase(), action: () => g.copyAll(f) }))),
          dropdown('Export', () => ['csv', 'json', 'sql'].map((f) => ({ label: f.toUpperCase() + '…', action: async () => {
            try {
              const msg = await Go.ExportRows(this.conn, r.columns, r.rows as any, f, 'result');
              if (msg) toast(msg);
            } catch (e) { toast(errorMessage(e), 'error'); }
          } })))),
        g.el);
    };
    sets.forEach((r, i) => {
      const g = new Grid({ quote: quoteIdent(this.conn), table: 'result', onSort: (col, add) => sortLocal(g, r, col, add) });
      g.set(r.columns, r.types, r.rows ?? []);
      this.grids.push(g);
      tabbar.append(h('button', { type: 'button', class: 'tab', onclick: () => show(i) }, `Result ${i + 1}`));
    });
    clear(this.results, info, error ? h('div', { class: 'result-error' }, error) : null, sets.length > 1 ? tabbar : null, sets.length ? body : null);
    if (sets.length) show(sets.length - 1);
  }

  private async showHistory() {
    const hist = await Go.History(this.conn).catch(() => []);
    let picked = '';
    const list = h('div', { class: 'list' }, hist.length ? hist.map((e) => h('button', {
      type: 'button', class: 'list-item',
      onclick: () => { picked = e.sql; document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })); },
      title: minify(e.sql),
    }, h('span', { class: 'history-sql' }, minify(e.sql)), h('span', { class: 'sub' }, `${e.database} · ${new Date(e.time).toLocaleString()}`))) : h('p', { class: 'muted', style: { padding: '10px' } }, 'No history yet.'));
    await modal(`History for ${this.conn || 'this connection'}`, list, [{ label: 'Close' }], { wide: true });
    if (picked) this.setText(picked);
  }

  private async snippetMenu(): Promise<MenuItem[]> {
    const list = await Go.Snippets().catch(() => []);
    return [
      { label: 'Save as snippet…', action: async () => {
        const name = await prompt('Save snippet', 'Name');
        if (!name) return;
        await Go.SaveSnippet({ name, sql: this.view.state.doc.toString() } as any);
        toast('Snippet saved');
      } },
      ...(list.length ? [{ separator: true, label: '' }] : []),
      ...list.map((s) => ({
        label: s.name, items: [
          { label: 'Insert', action: () => this.setText(s.sql) },
          { label: 'Delete', danger: true, action: () => { Go.DeleteSnippet(s.name).then(() => toast('Deleted')); } },
        ],
      })),
    ];
  }

  dispose() {
    if (this.conn) Go.CloseQueryTab(this.conn, this.database, this.id);
    this.view.destroy();
  }
}

function sortLocal(g: Grid, r: db.Result, col: string, add: boolean) {
  const sort = nextSort(g.opt.sort ?? [], col, add);
  const idx = sort.map((k) => ({ i: r.columns.indexOf(k.col), desc: k.desc }));
  const cmp = (x: any, y: any) => {
    if (x === y) return 0;
    if (x === null) return 1;
    if (y === null) return -1;
    return typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y), undefined, { numeric: true });
  };
  const rows = [...(r.rows ?? [])].sort((a: any, b: any) => {
    for (const k of idx) {
      const c = cmp(a[k.i], b[k.i]);
      if (c) return k.desc ? -c : c;
    }
    return 0;
  });
  g.opt.sort = sort;
  g.set(r.columns, r.types, sort.length ? rows : r.rows ?? []);
}

function resizeRows(handle: HTMLElement, box: HTMLElement) {
  handle.addEventListener('mousedown', (e) => {
    e.preventDefault();
    const startY = e.clientY;
    const startH = box.getBoundingClientRect().height;
    handle.classList.add('drag');
    const move = (ev: MouseEvent) => (box.style.height = Math.max(60, startH + ev.clientY - startY) + 'px');
    const up = () => {
      handle.classList.remove('drag');
      document.removeEventListener('mousemove', move);
      document.removeEventListener('mouseup', up);
    };
    document.addEventListener('mousemove', move);
    document.addEventListener('mouseup', up);
  });
}
