// The connection tree on the left: connections, databases, tables, views and routines.
import { h, clear, icon, formatCount, formatBytes } from './util';
import { Go, forConn } from './api';
import { state, emit, connInfo, type Selection } from './state';
import { menu, toast, errorMessage, type MenuItem } from './dialogs';
import type { db } from '../wailsjs/go/models';
import * as actions from './actions';

interface DbNode {
  expanded: boolean;
  loading: boolean;
  tables?: db.TableInfo[];
  routines?: db.Routine[];
  groups: Record<string, boolean>;
}
interface ConnNode {
  expanded: boolean;
  loading: boolean;
  databases?: string[];
  labels?: Record<string, string>;
  dbs: Record<string, DbNode>;
}

const colors: Record<string, string> = { red: 'var(--red)', green: 'var(--green)', blue: 'var(--blue)', yellow: 'var(--yellow)', orange: 'var(--orange)', cyan: 'var(--cyan)', magenta: 'var(--magenta)' };

export class Tree {
  el = h('div', { class: 'tree', role: 'tree', 'aria-label': 'Connections' });
  filterInput = h('input', { class: 'input', type: 'search', placeholder: 'Filter…', 'aria-label': 'Filter tree' });
  thumb = h('div', { class: 'overlay-thumb', 'aria-hidden': 'true' });
  root = h('nav', { class: 'sidebar-inner', style: { display: 'contents' } }, h('div', { class: 'tree-filter' }, this.filterInput), h('div', { class: 'tree-wrap' }, this.el, this.thumb));
  private nodes: Record<string, ConnNode> = {};
  private selectedKey = '';

  constructor() {
    this.filterInput.addEventListener('input', () => this.render());
    this.el.addEventListener('keydown', (e) => this.onKey(e));
    overlayScrollbar(this.el, this.thumb);
  }

  async reload() {
    const list = await Go.ListConnections();
    state.connections = list.connections ?? [];
    state.open = list.open ?? {};
    for (const w of list.warnings ?? []) toast(w, 'error');
    for (const name of Object.keys(this.nodes)) if (!connInfo(name)) delete this.nodes[name];
    this.render();
  }

  private conn(name: string): ConnNode {
    return (this.nodes[name] ??= { expanded: false, loading: false, dbs: {} });
  }

  private dbn(conn: string, db: string): DbNode {
    return (this.conn(conn).dbs[db] ??= { expanded: false, loading: false, groups: { tables: true, views: true, routines: false } });
  }

  async toggleConn(name: string, open?: boolean, quiet = false) {
    const n = this.conn(name);
    n.expanded = open ?? !n.expanded;
    if (n.expanded && !n.databases) await this.loadConn(name, quiet);
    this.render();
  }

  /** autoConnect opens every lerd connection; a stopped lerd service is skipped silently. */
  async autoConnectLerd() {
    await Promise.all(state.connections.filter((c) => c.source === 'lerd').map((c) => this.toggleConn(c.name, true, true)));
  }

  async loadConn(name: string, quiet = false) {
    const n = this.conn(name);
    n.loading = true;
    this.render();
    try {
      n.databases = await forConn(name, () => Go.Databases(name));
      n.labels = await Go.DatabaseLabels(name).catch(() => ({}));
      state.open[name] = true;
      const def = connInfo(name)?.database;
      const only = n.databases.length === 1 ? n.databases[0] : def && n.databases.includes(def) ? def : '';
      if (only) await this.toggleDb(name, only, true);
    } catch (e) {
      n.expanded = false;
      if (!quiet && errorMessage(e) !== 'Canceled') toast(`${name}: ${errorMessage(e)}`, 'error');
    } finally {
      n.loading = false;
      this.render();
    }
  }

  async toggleDb(conn: string, db: string, open?: boolean) {
    const d = this.dbn(conn, db);
    d.expanded = open ?? !d.expanded;
    if (d.expanded && !d.tables) await this.loadDb(conn, db);
    this.render();
  }

  async loadDb(conn: string, db: string) {
    const d = this.dbn(conn, db);
    d.loading = true;
    this.render();
    try {
      const objs = await forConn(conn, () => Go.DatabaseObjects(conn, db));
      d.tables = objs.tables ?? [];
      d.routines = objs.routines ?? [];
    } catch (e) {
      toast(errorMessage(e), 'error');
    } finally {
      d.loading = false;
      this.render();
    }
  }

  /** refresh reloads what is under the current selection (F5). */
  async refresh(sel = state.selection) {
    if (!sel) return this.reload();
    if (sel.kind === 'connection') {
      delete this.conn(sel.conn).databases;
      this.conn(sel.conn).dbs = {};
      if (this.conn(sel.conn).expanded) await this.loadConn(sel.conn);
      return;
    }
    if (sel.db) await this.loadDb(sel.conn, sel.db);
  }

  disconnect(name: string) {
    delete this.nodes[name];
    state.open[name] = false;
    this.render();
  }

  tablesOf(conn: string, db: string): db.TableInfo[] {
    return this.nodes[conn]?.dbs[db]?.tables ?? [];
  }

  /** allTables lists every loaded table for quick search. */
  allTables(): Selection[] {
    const out: Selection[] = [];
    for (const [conn, n] of Object.entries(this.nodes))
      for (const [db, d] of Object.entries(n.dbs))
        for (const t of d.tables ?? []) out.push({ kind: t.type === 'view' ? 'view' : 'table', conn, db, schema: t.schema, table: t.name });
    return out;
  }

  select(sel: Selection, key: string) {
    this.selectedKey = key;
    state.selection = sel;
    this.render();
    emit('select', sel);
  }

  /** reveal expands down to a table and selects it. */
  async reveal(sel: Selection) {
    await this.toggleConn(sel.conn, true);
    if (sel.db) await this.toggleDb(sel.conn, sel.db, true);
    this.select(sel, this.keyOf(sel));
    this.el.querySelector('.node.selected')?.scrollIntoView({ block: 'nearest' });
  }

  private keyOf(s: Selection) {
    return [s.conn, s.db, s.kind === 'routine' ? 'r:' + s.routine?.name : s.table ? s.schema + '.' + s.table : ''].filter(Boolean).join('/');
  }

  render() {
    const filter = this.filterInput.value.trim().toLowerCase();
    const rows: HTMLElement[] = [];
    let lastSource = '';
    for (const c of state.connections) {
      const heading = c.source === 'lerd' ? 'lerd services' : c.source === 'lerd-project' ? 'lerd projects' : '';
      if (heading && lastSource !== c.source) rows.push(h('div', { class: 'node group', style: { paddingLeft: '8px', marginTop: '6px' } }, h('span', { class: 'label' }, heading)));
      lastSource = c.source;
      const n = this.conn(c.name);
      const sel: Selection = { kind: 'connection', conn: c.name };
      rows.push(this.node(c.name, 0, {
        caret: n.loading ? 'spin' : n.expanded ? 'open' : 'closed',
        cls: 'conn' + (state.open[c.name] ? '' : ' closed'),
        lead: h('span', { class: 'dot', style: { background: colors[c.color] ?? 'var(--dark-foreground)' }, 'aria-hidden': 'true' }),
        label: c.source === 'lerd' ? c.name.replace(/^lerd · /, '') : c.source === 'lerd-project' ? c.name.replace(/^lerd project · /, '') : c.name,
        meta: c.missing ? 'missing' : c.label || c.driver,
        onToggle: () => this.toggleConn(c.name),
        onSelect: () => this.select(sel, c.name),
        menu: () => actions.connectionMenu(c.name, this),
      }));
      if (!n.expanded || !n.databases) continue;
      const renderDb = (dbName: string, depth: number) => {
        const d = this.dbn(c.name, dbName);
        const dkey = c.name + '/' + dbName;
        const dsel: Selection = { kind: 'database', conn: c.name, db: dbName };
        rows.push(this.node(dkey, depth, {
          caret: d.loading ? 'spin' : d.expanded ? 'open' : 'closed',
          lead: icon('db'),
          label: dbName,
          meta: n.labels?.[dbName],
          onToggle: () => this.toggleDb(c.name, dbName),
          onSelect: () => this.select(dsel, dkey),
          menu: () => actions.databaseMenu(c.name, dbName, this),
        }));
        if (!d.expanded || !d.tables) return;
        const match = (s: string) => !filter || s.toLowerCase().includes(filter);
        const tables = d.tables.filter((t) => t.type !== 'view' && match(t.name));
        const views = d.tables.filter((t) => t.type === 'view' && match(t.name));
        const routines = (d.routines ?? []).filter((r) => match(r.name));
        const multiSchema = new Set(d.tables.map((t) => t.schema)).size > 1;
        const group = (id: string, title: string, count: number, items: () => HTMLElement[]) => {
          if (!count && (id !== 'tables' || filter)) return;
          const open = filter ? true : d.groups[id];
          rows.push(this.node(dkey + '#' + id, depth + 1, {
            caret: open ? 'open' : 'closed', cls: 'group', label: title, meta: String(count),
            onToggle: () => { d.groups[id] = !d.groups[id]; this.render(); },
            onSelect: () => { d.groups[id] = !d.groups[id]; this.render(); },
          }));
          if (open) rows.push(...items());
        };
        const tableNode = (t: db.TableInfo) => {
          const s: Selection = { kind: t.type === 'view' ? 'view' : 'table', conn: c.name, db: dbName, schema: t.schema, table: t.name };
          const key = this.keyOf(s);
          return this.node(key, depth + 2, {
            lead: icon(t.type === 'view' ? 'view' : 'table'),
            label: multiSchema && t.schema !== 'public' ? `${t.schema}.${t.name}` : t.name,
            meta: t.type === 'view' ? '' : [formatCount(t.rows), formatBytes(t.size)].filter(Boolean).join(' · '),
            onSelect: () => this.select(s, key),
            menu: () => actions.tableMenu(s, this),
          });
        };
        group('tables', 'Tables', tables.length, () => tables.map(tableNode));
        group('views', 'Views', views.length, () => views.map(tableNode));
        group('routines', 'Functions, procedures, triggers', routines.length, () => routines.map((r) => {
          const s: Selection = { kind: 'routine', conn: c.name, db: dbName, schema: r.schema, routine: r };
          const key = this.keyOf(s);
          return this.node(key, depth + 2, {
            lead: icon(r.type === 'trigger' ? 'bolt' : 'fn'),
            label: r.name,
            meta: r.type + (r.table ? ' · ' + r.table : ''),
            onSelect: () => this.select(s, key),
          });
        }));
      };
      const filterDb = (name: string) => !filter || name.toLowerCase().includes(filter) || this.dbn(c.name, name).expanded;
      for (const dbName of n.databases) if (filterDb(dbName)) renderDb(dbName, 1);
    }
    if (!state.connections.length) {
      rows.push(h('div', { class: 'empty', style: { padding: '16px' } }, 'No connections yet.', h('button', { class: 'btn', type: 'button', onclick: () => actions.editConnection() }, icon('plus'), 'New connection')));
    }
    clear(this.el, rows);
  }

  private node(key: string, depth: number, o: { caret?: 'open' | 'closed' | 'spin'; cls?: string; lead?: Node; label: string; meta?: string; onToggle?: () => void; onSelect: () => void; menu?: () => MenuItem[] }) {
    const caret = h('span', { class: 'caret' }, o.caret === 'spin' ? h('span', { class: 'spinner' }) : o.caret ? icon(o.caret === 'open' ? 'caretD' : 'caretR', 12) : null);
    caret.addEventListener('click', (e) => {
      e.stopPropagation();
      o.onToggle?.();
    });
    const b = h('button', {
      type: 'button', role: 'treeitem', class: 'node ' + (o.cls ?? '') + (key === this.selectedKey ? ' selected' : ''),
      style: { paddingLeft: 8 + depth * 12 + 'px' }, 'data-key': key,
      'aria-expanded': o.caret ? String(o.caret === 'open') : undefined, 'aria-selected': String(key === this.selectedKey),
      onclick: () => o.onSelect(),
      ondblclick: () => o.onToggle?.(),
    }, o.lead ?? null, h('span', { class: 'label' }, o.label), h('span', { class: 'meta' }, o.meta ?? ''), caret);
    (b as any)._toggle = o.onToggle;
    if (o.menu) b.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      o.onSelect();
      menu(e.clientX, e.clientY, o.menu!());
    });
    return b;
  }

  private onKey(e: KeyboardEvent) {
    const nodes = [...this.el.querySelectorAll<HTMLButtonElement>('button.node')];
    const i = nodes.indexOf(document.activeElement as HTMLButtonElement);
    if (i < 0) return;
    const cur = nodes[i] as any;
    switch (e.key) {
      case 'ArrowDown':
        nodes[Math.min(i + 1, nodes.length - 1)].focus();
        break;
      case 'ArrowUp':
        nodes[Math.max(i - 1, 0)].focus();
        break;
      case 'ArrowRight':
        if (cur.getAttribute('aria-expanded') === 'false') cur._toggle?.();
        else nodes[i + 1]?.focus();
        break;
      case 'ArrowLeft':
        if (cur.getAttribute('aria-expanded') === 'true') cur._toggle?.();
        break;
      case 'ContextMenu': {
        const r = cur.getBoundingClientRect();
        cur.dispatchEvent(new MouseEvent('contextmenu', { clientX: r.left + 40, clientY: r.bottom, bubbles: true }));
        break;
      }
      default:
        return;
    }
    e.preventDefault();
    const key = cur.dataset.key;
    requestAnimationFrame(() => {
      if (document.activeElement === document.body) this.el.querySelector<HTMLElement>(`[data-key="${CSS.escape(key)}"]`)?.focus();
    });
  }
}

/** overlayScrollbar keeps a thin draggable thumb in sync with a scroller whose native bar is hidden. */
function overlayScrollbar(scroller: HTMLElement, thumb: HTMLElement) {
  const update = () => {
    const { scrollHeight: total, clientHeight: view, scrollTop: top } = scroller;
    thumb.hidden = total <= view + 1;
    if (thumb.hidden) return;
    const h = Math.max(24, (view / total) * view);
    thumb.style.height = h + 'px';
    thumb.style.top = (top / (total - view)) * (view - h) + 'px';
  };
  scroller.addEventListener('scroll', update, { passive: true });
  new ResizeObserver(update).observe(scroller);
  new MutationObserver(update).observe(scroller, { childList: true });
  thumb.addEventListener('mousedown', (e) => {
    e.preventDefault();
    const startY = e.clientY, startTop = scroller.scrollTop;
    const ratio = (scroller.scrollHeight - scroller.clientHeight) / (scroller.clientHeight - thumb.offsetHeight);
    thumb.classList.add('drag');
    const move = (ev: MouseEvent) => (scroller.scrollTop = startTop + (ev.clientY - startY) * ratio);
    const up = () => {
      thumb.classList.remove('drag');
      document.removeEventListener('mousemove', move);
      document.removeEventListener('mouseup', up);
    };
    document.addEventListener('mousemove', move);
    document.addEventListener('mouseup', up);
  });
}
