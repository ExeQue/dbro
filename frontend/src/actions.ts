// Commands behind menus, toolbar buttons and shortcuts.
import { h, clear, icon } from './util';
import { Go, forConn, call } from './api';
import { modal, confirm, prompt, toast, showError, errorMessage, field, type MenuItem } from './dialogs';
import { state, emit, connInfo, dialect, quoteIdent, type Selection } from './state';
import { applyTheme, currentTheme, applyFont } from './theme';
import type { Tree } from './tree';
import { config, db } from '../wailsjs/go/models';

let tree: Tree;
export function bindTree(t: Tree) {
  tree = t;
}

// ---- menus ----

export function connectionMenu(name: string, t: Tree): MenuItem[] {
  const c = connInfo(name);
  const manual = c?.source !== 'lerd';
  return [
    state.open[name] ? { label: 'Disconnect', action: () => disconnect(name) } : { label: 'Connect', action: () => t.toggleConn(name, true) },
    { label: 'Refresh', shortcut: 'F5', action: () => t.refresh({ kind: 'connection', conn: name }) },
    { label: 'New query', shortcut: 'Ctrl+T', action: () => emit('newQuery', { kind: 'connection', conn: name }) },
    { separator: true, label: '' },
    { label: 'New database…', disabled: dialect(name) === 'sqlite', action: () => newDatabase(name) },
    { separator: true, label: '' },
    { label: 'Edit…', disabled: !manual, action: () => editConnection(name) },
    { label: 'Duplicate…', disabled: !manual, action: () => editConnection(name, true) },
    { label: 'Delete', disabled: !manual, danger: true, action: () => deleteConnection(name) },
  ];
}

export function databaseMenu(conn: string, dbName: string, t: Tree): MenuItem[] {
  const sel: Selection = { kind: 'database', conn, db: dbName };
  return [
    { label: 'Refresh', shortcut: 'F5', action: () => t.refresh(sel) },
    { label: 'New query', action: () => emit('newQuery', sel) },
    { label: 'New table…', action: () => newTable(sel) },
    { separator: true, label: '' },
    { label: 'Import SQL file…', action: () => importSQL(sel) },
    { label: 'Dump to SQL file…', action: () => dump(sel) },
    { label: 'Clone to…', action: () => cloneTo(sel) },
    { separator: true, label: '' },
    { label: 'Drop database…', danger: true, disabled: dialect(conn) === 'sqlite', action: () => dropDatabase(conn, dbName) },
  ];
}

export function tableMenu(sel: Selection, t: Tree): MenuItem[] {
  const isView = sel.kind === 'view';
  return [
    { label: 'Open data', action: () => emit('showTab', 'data') },
    { label: 'Open structure', action: () => emit('showTab', 'structure') },
    { label: 'Copy name', action: () => Go.CopyText(sel.table!) },
    { separator: true, label: '' },
    { label: 'Export', items: ['csv', 'json', 'sql'].map((f) => ({ label: f.toUpperCase() + '…', action: () => exportTable(sel, f) })) },
    { label: 'Import CSV…', disabled: isView, action: () => importCSV(sel) },
    { label: 'Clone to…', action: () => cloneTo(sel) },
    { separator: true, label: '' },
    { label: 'Rename…', disabled: isView, action: () => renameTable(sel, t) },
    { label: 'Truncate…', disabled: isView, danger: true, action: () => runSchema(sel, [{ kind: 'truncate_table', schema: sel.schema, table: sel.table } as db.SchemaOp], `Delete every row in ${sel.table}?`) },
    { label: isView ? 'Drop view…' : 'Drop table…', danger: true, action: () => runSchema(sel, [{ kind: 'drop_table', schema: sel.schema, table: sel.table } as db.SchemaOp], `Drop ${sel.table}? This cannot be undone.`) },
  ];
}

// ---- schema helpers ----

/** runSchema previews the SQL for schema ops and runs it after confirmation. */
export async function runSchema(sel: Selection, ops: db.SchemaOp[], question?: string): Promise<boolean> {
  try {
    const stmts = await forConn(sel.conn, () => Go.PlanSchema(sel.conn, sel.db!, ops as any));
    const red = connInfo(sel.conn)?.color === 'red';
    const r = await modal(question ? 'Confirm' : 'Preview SQL', h('div', { class: 'form' },
      question ? h('p', {}, question) : null,
      red ? h('p', { style: { color: 'var(--red)' } }, `${sel.conn} is marked red.`) : null,
      h('pre', { class: 'sql-preview' }, stmts.join('\n')),
    ), [{ label: 'Cancel' }, { label: 'Copy SQL', action: () => { Go.CopyText(stmts.join('\n')); return false; } }, {
      label: 'Run', primary: true, danger: !!question || red,
      action: () => forConn(sel.conn, () => Go.ApplySchema(sel.conn, sel.db!, stmts)),
    }], { wide: true });
    if (r !== 2) return false;
    toast('Done');
    await tree.refresh({ kind: 'database', conn: sel.conn, db: sel.db });
    emit('schemaChanged', sel);
    return true;
  } catch (e) {
    showError(e);
    return false;
  }
}

async function renameTable(sel: Selection, t: Tree) {
  const name = await prompt('Rename table', 'New name', sel.table);
  if (!name || name === sel.table) return;
  if (await runSchema(sel, [{ kind: 'rename_table', schema: sel.schema, table: sel.table, name } as db.SchemaOp])) {
    t.reveal({ ...sel, table: name });
  }
}

export async function newDatabase(conn: string) {
  const name = await prompt('New database', 'Name');
  if (!name) return;
  if (await runSchema({ kind: 'connection', conn, db: '' }, [{ kind: 'create_database', name } as db.SchemaOp])) tree.refresh({ kind: 'connection', conn });
}

async function dropDatabase(conn: string, dbName: string) {
  const typed = await prompt('Drop database', `Type ${dbName} to drop it and all its tables`);
  if (typed !== dbName) return;
  if (await runSchema({ kind: 'connection', conn, db: '' }, [{ kind: 'drop_database', name: dbName } as db.SchemaOp])) tree.refresh({ kind: 'connection', conn });
}

export function newTable(sel: Selection) {
  emit('newTable', sel);
}

export async function followForeignKey(sel: Selection, fk: db.ForeignKey, values: unknown[]) {
  const q = quoteIdent(sel.conn);
  const lit = (v: unknown) => (v === null || v === undefined ? 'NULL' : typeof v === 'number' ? String(v) : "'" + String(v).replace(/'/g, "''") + "'");
  const where = fk.refColumns.map((c, i) => (values[i] === null ? `${q(c)} IS NULL` : `${q(c)} = ${lit(values[i])}`)).join(' AND ');
  const target: Selection = { kind: 'table', conn: sel.conn, db: sel.db, schema: fk.refSchema || sel.schema, table: fk.refTable };
  await tree.reveal(target);
  emit('filterData', where);
}

// ---- files ----

export async function exportTable(sel: Selection, format: string, where = '') {
  try {
    const msg = await forConn(sel.conn, () => Go.ExportTable(sel.conn, sel.db!, sel.schema ?? '', sel.table!, where, format));
    if (msg) toast(msg);
  } catch (e) {
    showError(e, 'Export failed');
  }
}

export async function dump(sel: Selection) {
  const withData = h('input', { type: 'checkbox', checked: true });
  if ((await modal(`Dump ${sel.db}`, h('div', { class: 'form' }, h('p', {}, 'Write every table and view as SQL.'), h('label', { class: 'check' }, withData, ' Include data')), [{ label: 'Cancel' }, { label: 'Choose file…', primary: true }])) !== 1) return;
  try {
    const msg = await forConn(sel.conn, () => Go.DumpDatabase(sel.conn, sel.db!, withData.checked));
    if (msg) toast(msg);
  } catch (e) {
    showError(e, 'Dump failed');
  }
}

export async function importSQL(sel: Selection) {
  const cont = h('input', { type: 'checkbox' });
  if ((await modal(`Import into ${sel.db}`, h('div', { class: 'form' }, h('p', {}, 'Run every statement in a SQL file.'), h('label', { class: 'check' }, cont, ' Continue after errors')), [{ label: 'Cancel' }, { label: 'Choose file…', primary: true }])) !== 1) return;
  try {
    const msg = await forConn(sel.conn, () => Go.ImportSQL(sel.conn, sel.db!, cont.checked));
    if (msg) {
      toast(msg);
      tree.refresh({ kind: 'database', conn: sel.conn, db: sel.db });
    }
  } catch (e) {
    showError(e, 'Import failed');
  }
}

export async function importCSV(sel: Selection) {
  const delimiter = h('select', { class: 'input' }, [[',', 'Comma'], [';', 'Semicolon'], ['\\t', 'Tab'], ['|', 'Pipe']].map(([v, l]) => h('option', { value: v }, l)));
  const header = h('input', { type: 'checkbox', checked: true });
  const nullText = h('input', { class: 'input', placeholder: 'e.g. NULL (\\N always works)' });
  let file: { path: string; columns: string[]; rows: string[][] } | null = null;
  const def = await forConn(sel.conn, () => Go.Describe(sel.conn, sel.db!, sel.schema ?? '', sel.table!)).catch((e) => { showError(e); return null; });
  if (!def) return;
  const mapBox = h('div', {});
  const selects: HTMLSelectElement[] = [];
  const renderMap = () => {
    if (!file) return clear(mapBox, h('p', { class: 'muted' }, 'No file chosen.'));
    selects.length = 0;
    const cols = def.columns.map((c) => c.name);
    clear(mapBox, h('p', { class: 'muted' }, file.path),
      h('table', { class: 'sgrid' }, h('tr', {}, h('th', {}, 'CSV column'), h('th', {}, 'Sample'), h('th', {}, 'Table column')),
        file.columns.map((c, i) => {
          const s = h('select', { class: 'input', 'aria-label': 'Table column for ' + c }, h('option', { value: '' }, '(skip)'), cols.map((tc) => h('option', { value: tc, selected: tc.toLowerCase() === c.toLowerCase() }, tc)));
          selects.push(s);
          return h('tr', {}, h('td', {}, c), h('td', { class: 'muted' }, file!.rows[0]?.[i] ?? ''), h('td', {}, s));
        })));
  };
  const pick = async () => {
    const f = await Go.PickCSV(delimiter.value, header.checked);
    if (f.path) file = f;
    renderMap();
  };
  const reread = async () => {
    if (file) file = await Go.PreviewCSV(file.path, delimiter.value, header.checked);
    renderMap();
  };
  delimiter.addEventListener('change', reread);
  header.addEventListener('change', reread);
  renderMap();
  await modal(`Import CSV into ${sel.table}`, h('div', { class: 'form' },
    h('div', { class: 'form-row' }, field('Delimiter', delimiter), field('NULL text', nullText)),
    h('label', { class: 'check' }, header, ' First row is a header'),
    h('button', { type: 'button', class: 'btn', onclick: pick }, 'Choose file…'),
    mapBox,
  ), [{ label: 'Cancel' }, {
    label: 'Import', primary: true,
    action: async () => {
      if (!file) throw new Error('Choose a file first');
      const msg = await forConn(sel.conn, () => Go.ImportCSV(sel.conn, sel.db!, sel.schema ?? '', sel.table!, file!.path, delimiter.value, header.checked, selects.map((s) => s.value), nullText.value));
      toast(msg);
      emit('reloadData');
      tree.refresh({ kind: 'database', conn: sel.conn, db: sel.db });
    },
  }], { wide: true });
}

/** cloneTo copies a table, or every table in a database, to another database or connection. */
export async function cloneTo(sel: Selection) {
  const conns = state.connections.map((c) => c.name);
  const target = h('select', { class: 'input' }, conns.map((n) => h('option', { value: n, selected: n === sel.conn }, n)));
  const dbSel = h('select', { class: 'input' });
  const newDb = h('input', { class: 'input', placeholder: 'or a new database name' });
  const schema = h('input', { class: 'input', value: 'public' });
  const withData = h('input', { type: 'checkbox', checked: true });
  const drop = h('input', { type: 'checkbox' });
  const schemaField = field('PostgreSQL schema', schema);
  const loadDbs = async () => {
    clear(dbSel, h('option', {}, 'Loading…'));
    try {
      const dbs = await forConn(target.value, () => Go.Databases(target.value));
      clear(dbSel, dbs.map((d) => h('option', { value: d, selected: d === sel.db && target.value !== sel.conn }, d)));
    } catch (e) {
      clear(dbSel, h('option', { value: '' }, errorMessage(e)));
    }
    schemaField.hidden = dialect(target.value) !== 'postgres';
  };
  target.addEventListener('change', loadDbs);
  loadDbs();
  const what = sel.table ? `table ${sel.table}` : `all tables in ${sel.db}`;
  await modal(`Clone ${what}`, h('div', { class: 'form' },
    field('Target connection', target),
    h('div', { class: 'form-row' }, field('Target database', dbSel), field('New database', newDb)),
    schemaField,
    h('label', { class: 'check' }, withData, ' Copy data'),
    h('label', { class: 'check' }, drop, ' Replace tables that already exist'),
    h('p', { class: 'muted' }, 'Types are translated between drivers. Views, triggers and routines are not copied.'),
  ), [{ label: 'Cancel' }, {
    label: 'Clone', primary: true,
    action: async () => {
      let dst = dbSel.value;
      if (newDb.value.trim()) {
        dst = newDb.value.trim();
        if (dialect(target.value) !== 'sqlite') {
          const stmts = await forConn(target.value, () => Go.PlanSchema(target.value, '', [{ kind: 'create_database', name: dst } as any]));
          await Go.ApplySchema(target.value, '', stmts);
        }
      }
      if (!dst) throw new Error('Pick a target database');
      const tables = sel.table ? [{ schema: sel.schema ?? '', name: sel.table, type: sel.kind }] : tree.tablesOf(sel.conn, sel.db!).filter((t) => t.type !== 'view');
      if (!tables.length) throw new Error('No tables loaded; expand the database first');
      if (target.value === sel.conn && dst === sel.db && (dialect(sel.conn) !== 'postgres' || schema.value === (sel.schema || 'public'))) throw new Error('Pick a different target');
      const msg = await forConn(target.value, () => Go.Clone(sel.conn, sel.db!, tables as any, target.value, dst, schema.value, { withData: withData.checked, dropExisting: drop.checked } as any));
      toast(msg);
      tree.refresh({ kind: 'connection', conn: target.value });
    },
  }]);
}

// ---- connections ----

export async function disconnect(name: string) {
  await Go.Disconnect(name);
  tree.disconnect(name);
}

async function deleteConnection(name: string) {
  if (!(await confirm('Delete connection', `Delete "${name}" and its stored passwords?`, 'Delete', true))) return;
  await Go.DeleteConnection(name);
  await tree.reload();
}

const drivers = [['mysql', 'MySQL'], ['mariadb', 'MariaDB'], ['postgres', 'PostgreSQL'], ['sqlite', 'SQLite']];
const colorNames = ['', 'green', 'blue', 'yellow', 'orange', 'red', 'magenta', 'cyan'];

/** editConnection opens the connection editor: new (no name), edit, or duplicate. */
export async function editConnection(name?: string, duplicate = false) {
  let c: config.Connection = config.Connection.createFrom({ name: '', driver: 'mysql', host: '127.0.0.1', port: 0, user: '', database: '', file: '', laravel: '', color: '', sslMode: '', sslCA: '', sslCert: '', sslKey: '', allowAgentWrites: false, hideFromAgents: false });
  if (name) {
    try {
      c = await Go.GetConnection(name);
    } catch (e) {
      showError(e);
      return;
    }
  }
  const oldName = name && !duplicate ? name : '';
  if (duplicate) c.name = c.name + ' copy';
  let mode: 'server' | 'sqlite' | 'laravel' = c.laravel ? 'laravel' : c.driver === 'sqlite' ? 'sqlite' : 'server';

  const i = (value: any, attrs: Record<string, any> = {}) => h('input', { class: 'input', value: value ?? '', ...attrs });
  const nameI = i(c.name, { placeholder: 'e.g. app local' });
  const driverS = h('select', { class: 'input' }, drivers.filter(([d]) => d !== 'sqlite').map(([v, l]) => h('option', { value: v, selected: v === c.driver }, l)));
  const hostI = i(c.host), portI = i(c.port || '', { inputmode: 'numeric', placeholder: 'default' });
  const userI = i(c.user), passI = i('', { type: 'password', placeholder: oldName ? 'unchanged' : '', autocomplete: 'off' });
  const clearPass = h('input', { type: 'checkbox' });
  const dbI = i(c.database, { placeholder: 'optional' });
  const fileI = i(c.file, { placeholder: '/path/to/database.sqlite' });
  const laravelI = i(c.laravel, { placeholder: '~/Work/my-app' });
  const laravelInfo = h('p', { class: 'muted' });
  const colorS = h('select', { class: 'input' }, colorNames.map((n) => h('option', { value: n, selected: n === (c.color ?? '') }, n || 'none')));
  const sslS = h('select', { class: 'input' }, ['', 'disable', 'require', 'verify-ca', 'verify-full'].map((m) => h('option', { value: m, selected: m === (c.sslMode ?? '') }, m || 'default')));
  const caI = i(c.sslCA, { placeholder: 'CA certificate file' }), certI = i(c.sslCert, { placeholder: 'client certificate' }), keyI = i(c.sslKey, { placeholder: 'client key' });
  const sshOn = h('input', { type: 'checkbox', checked: !!c.ssh?.host });
  const sshHost = i(c.ssh?.host, { placeholder: 'host or ~/.ssh/config alias' }), sshPort = i(c.ssh?.port || '', { placeholder: '22' }), sshUser = i(c.ssh?.user, { placeholder: 'from ~/.ssh/config' }), sshKey = i(c.ssh?.key, { placeholder: 'ssh-agent or default keys' });
  const sshPass = i('', { type: 'password', placeholder: 'password or key passphrase (optional)', autocomplete: 'off' });
  const allowWrites = h('input', { type: 'checkbox', checked: c.allowAgentWrites }), hide = h('input', { type: 'checkbox', checked: c.hideFromAgents });
  const testOut = h('span', { class: 'muted' });

  const browse = (input: HTMLInputElement, f: () => Promise<string>) => h('button', { type: 'button', class: 'btn', onclick: async () => { const p = await f(); if (p) { input.value = p; input.dispatchEvent(new Event('change')); } } }, 'Browse…');
  const serverBox = h('div', { class: 'form' },
    h('div', { class: 'form-row' }, field('Driver', driverS), field('Host', hostI), field('Port', portI)),
    h('div', { class: 'form-row' }, field('User', userI), field('Password', passI), field('Default database', dbI)),
    oldName ? h('label', { class: 'check' }, clearPass, ' Remove the stored password') : null,
    h('fieldset', {}, h('legend', {}, 'SSL / TLS'), h('div', { class: 'form-row' }, field('Mode', sslS), field('CA file', caI)), h('div', { class: 'form-row' }, field('Client cert', certI), field('Client key', keyI))),
  );
  const sqliteBox = h('div', { class: 'form' }, field('Database file', h('div', { style: { display: 'flex', gap: '6px' } }, fileI, browse(fileI, () => Go.PickFile('SQLite database', 'sqlite')), h('button', { type: 'button', class: 'btn', onclick: async () => { const p = await Go.PickNewFile('New SQLite database'); if (p) fileI.value = p; } }, 'New…'))));
  const laravelBox = h('div', { class: 'form' }, field('Laravel project', h('div', { style: { display: 'flex', gap: '6px' } }, laravelI, browse(laravelI, () => Go.PickDirectory('Laravel project'))), 'dbro reads DB_* from .env every time it connects.'), laravelInfo);
  const previewLaravel = async () => {
    if (!laravelI.value) return clear(laravelInfo);
    try {
      const r = await Go.ResolveLaravel(laravelI.value);
      laravelInfo.textContent = r.driver === 'sqlite' ? `sqlite: ${r.file}${r.missing ? ' (missing)' : ''}` : `${r.driver}: ${r.user}@${r.host}:${r.port || 'default'}/${r.database}`;
      if (!nameI.value) nameI.value = r.label;
    } catch (e) {
      laravelInfo.textContent = errorMessage(e);
    }
  };
  laravelI.addEventListener('change', previewLaravel);
  if (mode === 'laravel') previewLaravel();
  const sshBox = h('div', { class: 'form' }, h('div', { class: 'form-row' }, field('SSH host', sshHost), field('Port', sshPort), field('User', sshUser)), h('div', { class: 'form-row' }, field('Key file', sshKey), field('Secret', sshPass)));
  const sshField = h('fieldset', {}, h('legend', {}, h('label', { class: 'check' }, sshOn, ' SSH tunnel')), sshBox);
  const seg = h('div', { class: 'seg', role: 'tablist' });
  const setMode = (m: typeof mode) => {
    mode = m;
    clear(seg, ([['server', 'Server'], ['sqlite', 'SQLite file'], ['laravel', 'Laravel project']] as const).map(([v, l]) => h('button', { type: 'button', role: 'tab', class: v === mode ? 'on' : '', 'aria-selected': String(v === mode), onclick: () => setMode(v) }, l)));
    serverBox.hidden = mode !== 'server';
    sqliteBox.hidden = mode !== 'sqlite';
    laravelBox.hidden = mode !== 'laravel';
    sshField.hidden = mode === 'sqlite';
  };
  const syncSSH = () => (sshBox.hidden = !sshOn.checked);
  sshOn.addEventListener('change', syncSSH);
  syncSSH();
  setMode(mode);

  const collect = (): config.Connection => {
    const out = config.Connection.createFrom({
      ...c, name: nameI.value.trim(), color: colorS.value, allowAgentWrites: allowWrites.checked, hideFromAgents: hide.checked,
      laravel: '', file: '', driver: driverS.value, host: '', port: 0, user: '', database: '', sslMode: '', sslCA: '', sslCert: '', sslKey: '',
    });
    if (mode === 'sqlite') Object.assign(out, { driver: 'sqlite', file: fileI.value.trim() });
    if (mode === 'laravel') Object.assign(out, { laravel: laravelI.value.trim(), driver: '' });
    if (mode === 'server') Object.assign(out, { host: hostI.value.trim(), port: Number(portI.value) || 0, user: userI.value.trim(), database: dbI.value.trim(), sslMode: sslS.value, sslCA: caI.value.trim(), sslCert: certI.value.trim(), sslKey: keyI.value.trim() });
    out.ssh = mode !== 'sqlite' && sshOn.checked && sshHost.value.trim() ? config.SSH.createFrom({ host: sshHost.value.trim(), port: Number(sshPort.value) || 0, user: sshUser.value.trim(), key: sshKey.value.trim() }) : undefined;
    return out;
  };

  const body = h('div', { class: 'form' },
    h('div', { class: 'form-row' }, field('Name', nameI), field('Colour', colorS, 'Red asks before saving changes.')),
    seg, serverBox, sqliteBox, laravelBox, sshField,
    h('fieldset', {}, h('legend', {}, 'Agents (MCP)'),
      h('label', { class: 'check' }, allowWrites, ' Allow agents to change data and schema'),
      h('label', { class: 'check' }, hide, ' Hide this connection from agents')),
    testOut,
  );
  const r = await modal(oldName ? `Edit ${oldName}` : 'New connection', body, [
    { label: 'Cancel' },
    { label: 'Test', action: async () => {
      testOut.textContent = 'Testing…';
      try {
        const v = await call(() => Go.TestConnection(collect(), passI.value, sshPass.value));
        testOut.textContent = 'Connected: ' + v;
      } catch (e) {
        testOut.textContent = '';
        throw e;
      }
      return false;
    } },
    { label: 'Save', primary: true, action: () => Go.SaveConnection(collect(), oldName, passI.value, clearPass.checked, sshPass.value) },
  ], { wide: true });
  if (r === 2) {
    await tree.reload();
    if (oldName) tree.disconnect(oldName);
  }
}

// ---- quick search, settings ----

export async function quickSearch() {
  const all = tree.allTables();
  const input = h('input', { class: 'input', placeholder: all.length ? 'Table name…' : 'Expand a database first', style: { width: '100%' }, 'aria-label': 'Search tables' });
  const list = h('div', { class: 'list', role: 'listbox' });
  let hits: Selection[] = [];
  let idx = 0;
  let close: (() => void) | null = null;
  const render = () => {
    const q = input.value.toLowerCase();
    hits = all.filter((t) => fuzzy(q, t.table!.toLowerCase())).slice(0, 50);
    idx = Math.min(idx, Math.max(hits.length - 1, 0));
    clear(list, hits.map((t, i) => h('button', { type: 'button', role: 'option', class: 'list-item' + (i === idx ? ' on' : ''), onclick: () => go(t) }, icon(t.kind === 'view' ? 'view' : 'table'), t.table, h('span', { class: 'sub' }, `${t.conn} / ${t.db}`))));
  };
  const go = (t: Selection) => {
    close?.();
    tree.reveal(t);
  };
  input.addEventListener('input', () => { idx = 0; render(); });
  input.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') idx = Math.min(idx + 1, hits.length - 1);
    else if (e.key === 'ArrowUp') idx = Math.max(idx - 1, 0);
    else if (e.key === 'Enter' && hits[idx]) { e.preventDefault(); e.stopPropagation(); go(hits[idx]); return; }
    else return;
    e.preventDefault();
    render();
  });
  render();
  const p = modal('Quick search', h('div', { class: 'form' }, input, list), [{ label: 'Close' }]);
  close = () => {
    const ev = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true });
    document.dispatchEvent(ev);
  };
  await p;
}

function fuzzy(q: string, s: string) {
  let i = 0;
  for (const ch of s) if (ch === q[i]) i++;
  return i === q.length;
}

export async function settings() {
  const boot = state.boot;
  let chosen = boot.settings.theme;
  const original = currentTheme();
  const themes = await Go.ListThemes();
  const list = h('div', { class: 'theme-list' });
  const renderThemes = async () => {
    const cards = await Promise.all(themes.map(async (t) => {
      const full = await Go.PreviewTheme(t.id).catch(() => null);
      const sw = ['background', 'lighter_background', 'foreground', 'accent', 'red', 'green', 'yellow', 'blue'].map((k) => h('span', { style: { background: full?.colors?.[k] ?? 'transparent' } }));
      return h('button', { type: 'button', class: 'theme-card' + (t.id === chosen ? ' on' : ''), 'aria-pressed': String(t.id === chosen), onclick: async () => {
        chosen = t.id;
        if (full) applyTheme(full);
        renderThemes();
      } }, h('b', {}, t.name), h('span', { class: 'muted' }, t.source === 'omarchy' ? 'follows Omarchy live' : `${t.source} · ${t.mode}`), h('div', { class: 'swatches' }, sw));
    }));
    const system = h('button', { type: 'button', class: 'theme-card' + (chosen === 'system' ? ' on' : ''), onclick: async () => { chosen = 'system'; applyTheme(await Go.PreviewTheme('system')); renderThemes(); } }, h('b', {}, 'System'), h('span', { class: 'muted' }, 'dbro Light or Dark, following the system'));
    clear(list, system, cards);
  };
  renderThemes();
  const lerd = h('input', { type: 'checkbox', checked: boot.settings.lerd });
  const font = h('input', { class: 'input', value: boot.settings.font ?? '', placeholder: 'monospace (from fontconfig)' });
  const timeout = h('input', { class: 'input', value: boot.settings.queryTimeout, inputmode: 'numeric' });
  const exe = boot.executable || 'dbro';
  const mcpCmd = `claude mcp add dbro -- ${exe} mcp`;
  const r = await modal('Settings', h('div', { class: 'form' },
    h('fieldset', {}, h('legend', {}, 'Theme'), list,
      h('div', {}, h('button', { type: 'button', class: 'btn small', onclick: async () => {
        const n = await prompt('Save current theme', 'Name for the new theme');
        if (!n) return;
        const id = await Go.SaveThemeAs(n, currentTheme()!.colors);
        toast(`Saved to themes/${id}.toml`);
        themes.splice(0, themes.length, ...(await Go.ListThemes()));
        renderThemes();
      } }, 'Save current as theme…'), h('span', { class: 'muted' }, `  Own themes: ${boot.configDir}/themes/*.toml`))),
    h('div', { class: 'form-row' }, field('Font', font), field('Agent query timeout (seconds)', timeout)),
    h('label', { class: 'check' }, lerd, ' Show lerd database services in the tree'),
    h('fieldset', {}, h('legend', {}, 'MCP server for agents'),
      h('p', { class: 'muted' }, 'Agents get read-only access to your saved connections. Writes need "Allow agents to change data" on the connection.'),
      h('div', { style: { display: 'flex', gap: '6px' } }, h('code', { style: { flex: '1', userSelect: 'all' } }, mcpCmd), h('button', { type: 'button', class: 'btn small', onclick: () => { Go.CopyText(mcpCmd); toast('Copied'); } }, 'Copy'))),
  ), [{ label: 'Cancel' }, { label: 'Save', primary: true, action: async () => {
    const s = { ...boot.settings, lerd: lerd.checked, font: font.value.trim(), queryTimeout: Number(timeout.value) || 30 };
    await Go.SaveSettings(s as any);
    const t = await Go.SetTheme(chosen);
    applyTheme(t);
    applyFont(s.font);
    boot.settings = { ...s, theme: chosen } as any;
    emit('themeChanged', t);
  } }], { wide: true });
  if (r !== 1 && original) applyTheme(original);
  if (r === 1) tree.reload();
}

/** askOmarchy is the first-run question when Omarchy is installed. */
export async function askOmarchy() {
  const r = await modal('Omarchy detected', h('div', {}, h('p', {}, "Sync dbro's theme with Omarchy?"), h('p', { class: 'muted' }, 'dbro will follow your Omarchy theme and change with it. You can switch this in Settings at any time.')), [{ label: 'No' }, { label: 'Yes', primary: true }]);
  try {
    const t = await Go.AnswerOmarchy(r === 1);
    applyTheme(t);
    state.boot.settings.theme = r === 1 ? 'omarchy' : state.boot.settings.theme;
    emit('themeChanged', t);
  } catch (e) {
    showError(e);
  }
}
