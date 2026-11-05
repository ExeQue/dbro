// Type-aware cell editors: a date picker, enum/set choosers and a CodeMirror value editor.
import { EditorView, keymap } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { basicSetup } from 'codemirror';
import { json, jsonParseLinter } from '@codemirror/lang-json';
import { linter, lintGutter } from '@codemirror/lint';
import { syntaxHighlighting } from '@codemirror/language';
import { h, clear } from './util';
import { highlight } from './highlight';

export type Kind = 'bool' | 'enum' | 'set' | 'date' | 'datetime' | 'time' | 'json' | 'number' | 'blob' | 'text';

/** kindOf maps a column type (full definition or driver type name) to an editor. */
export function kindOf(type = ''): Kind {
  const t = type.toLowerCase().trim();
  if (/^(tinyint\(1\)|bool|boolean|bit\(1\))$/.test(t)) return 'bool';
  if (t.startsWith('enum(')) return 'enum';
  if (t.startsWith('set(')) return 'set';
  if (/^(timestamp|datetime)/.test(t) || t === 'timestamptz') return 'datetime';
  if (t === 'date') return 'date';
  if (/^time(tz|\b|\(| with| without)/.test(t) || t === 'time') return 'time';
  if (t === 'json' || t === 'jsonb') return 'json';
  if (/blob|binary|bytea/.test(t)) return 'blob';
  if (/^(tiny|small|medium|big)?int|^dec|^numeric|^float|^double|^real|serial|^money|^year|^integer/.test(t)) return 'number';
  return 'text';
}

/** enumValues reads the options of enum('a','b') or set('a','b'). */
export function enumValues(type: string): string[] {
  const m = /^\w+\((.*)\)$/s.exec(type.trim());
  if (!m) return [];
  return [...m[1].matchAll(/'((?:[^']|'')*)'/g)].map((x) => x[1].replace(/''/g, "'"));
}

const pad = (n: number) => String(n).padStart(2, '0');

function parse(value: string | null): Date {
  const m = value ? /^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{2}):(\d{2})(?::(\d{2}))?)?|^(\d{2}):(\d{2})(?::(\d{2}))?/.exec(value) : null;
  if (!m) return new Date();
  if (m[7]) {
    const d = new Date();
    d.setHours(+m[7], +m[8], +(m[9] ?? 0), 0);
    return d;
  }
  return new Date(+m[1], +m[2] - 1, +m[3], +(m[4] ?? 0), +(m[5] ?? 0), +(m[6] ?? 0));
}

function format(d: Date, kind: Kind): string {
  const date = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  return kind === 'date' ? date : kind === 'time' ? time : `${date} ${time}`;
}

/** popover shows a small panel under an anchor; it resolves when closed. */
function popover<T>(anchor: HTMLElement, build: (done: (v: T | undefined) => void) => HTMLElement): Promise<T | undefined> {
  return new Promise((resolve) => {
    let el: HTMLElement;
    const done = (v: T | undefined) => {
      el.remove();
      document.removeEventListener('mousedown', outside, true);
      resolve(v);
    };
    const outside = (e: MouseEvent) => {
      if (!el.contains(e.target as Node)) done(undefined);
    };
    el = build(done);
    el.classList.add('popover');
    el.addEventListener('keydown', (e) => {
      e.stopPropagation();
      if (e.key === 'Escape') done(undefined);
    });
    document.body.appendChild(el);
    const r = anchor.getBoundingClientRect();
    const pr = el.getBoundingClientRect();
    el.style.left = Math.max(4, Math.min(r.left, window.innerWidth - pr.width - 4)) + 'px';
    el.style.top = (r.bottom + pr.height + 4 > window.innerHeight ? Math.max(4, r.top - pr.height - 4) : r.bottom + 2) + 'px';
    setTimeout(() => document.addEventListener('mousedown', outside, true), 0);
    (el.querySelector('input, button') as HTMLElement | null)?.focus();
  });
}

/** pickDate edits a date, datetime or time. Resolves to the new text, null for NULL, or undefined when canceled. */
export function pickDate(anchor: HTMLElement, value: string | null, kind: Kind, nullable: boolean): Promise<string | null | undefined> {
  return popover<string | null>(anchor, (done) => {
    let cur = parse(value);
    let month = new Date(cur.getFullYear(), cur.getMonth(), 1);
    const text = h('input', { class: 'input', value: value ?? '', 'aria-label': 'Value', spellcheck: 'false', style: { width: '100%' } });
    const cal = h('div', { class: 'cal' });
    const time = (n: number, max: number, label: string) => h('input', { class: 'input', type: 'number', min: 0, max, value: pad(n), 'aria-label': label });
    const hh = time(cur.getHours(), 23, 'Hours'), mm = time(cur.getMinutes(), 59, 'Minutes'), ss = time(cur.getSeconds(), 59, 'Seconds');
    const sync = () => (text.value = format(cur, kind));
    for (const [i, set] of [[hh, (v: number) => cur.setHours(v)], [mm, (v: number) => cur.setMinutes(v)], [ss, (v: number) => cur.setSeconds(v)]] as const) {
      i.addEventListener('input', () => { set(Number(i.value) || 0); sync(); });
    }
    text.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') done(text.value.trim() === '' && nullable ? null : text.value.trim());
    });
    text.addEventListener('change', () => {
      cur = parse(text.value);
      month = new Date(cur.getFullYear(), cur.getMonth(), 1);
      render();
    });
    const render = () => {
      const first = (month.getDay() + 6) % 7; // Monday first
      const days = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate();
      const cells: HTMLElement[] = ['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su'].map((d) => h('span', { class: 'cal-dow' }, d));
      for (let i = 0; i < first; i++) cells.push(h('span', {}));
      for (let d = 1; d <= days; d++) {
        const on = cur.getFullYear() === month.getFullYear() && cur.getMonth() === month.getMonth() && cur.getDate() === d;
        cells.push(h('button', { type: 'button', class: 'cal-day' + (on ? ' on' : ''), onclick: () => {
          cur = new Date(month.getFullYear(), month.getMonth(), d, cur.getHours(), cur.getMinutes(), cur.getSeconds());
          sync();
          render();
        } }, String(d)));
      }
      clear(cal,
        h('div', { class: 'cal-head' },
          h('button', { type: 'button', class: 'btn icon small', 'aria-label': 'Previous month', onclick: () => { month = new Date(month.getFullYear(), month.getMonth() - 1, 1); render(); } }, '‹'),
          h('b', {}, month.toLocaleDateString('en-US', { month: 'long', year: 'numeric' })),
          h('button', { type: 'button', class: 'btn icon small', 'aria-label': 'Next month', onclick: () => { month = new Date(month.getFullYear(), month.getMonth() + 1, 1); render(); } }, '›')),
        h('div', { class: 'cal-grid' }, cells));
    };
    render();
    return h('div', { class: 'date-picker', role: 'dialog', 'aria-label': 'Pick a date' },
      text,
      kind === 'time' ? null : cal,
      kind === 'date' ? null : h('div', { class: 'cal-time' }, hh, ':', mm, ':', ss),
      h('div', { class: 'cal-actions' },
        h('button', { type: 'button', class: 'btn small', onclick: () => { cur = new Date(); hh.value = pad(cur.getHours()); mm.value = pad(cur.getMinutes()); ss.value = pad(cur.getSeconds()); month = new Date(cur.getFullYear(), cur.getMonth(), 1); sync(); render(); } }, kind === 'date' ? 'Today' : 'Now'),
        nullable ? h('button', { type: 'button', class: 'btn small', onclick: () => done(null) }, 'NULL') : null,
        h('span', { class: 'spacer' }),
        h('button', { type: 'button', class: 'btn small', onclick: () => done(undefined) }, 'Cancel'),
        h('button', { type: 'button', class: 'btn small primary', onclick: () => done(text.value.trim() === '' && nullable ? null : text.value.trim()) }, 'OK')));
  });
}

/** pickSet edits a MySQL SET column with checkboxes. */
export function pickSet(anchor: HTMLElement, value: string | null, options: string[], nullable: boolean): Promise<string | null | undefined> {
  return popover<string | null>(anchor, (done) => {
    const chosen = new Set((value ?? '').split(',').filter(Boolean));
    const boxes = options.map((o) => {
      const cb = h('input', { type: 'checkbox', checked: chosen.has(o) });
      return [o, cb] as const;
    });
    const result = () => options.filter((_, i) => boxes[i][1].checked).join(',');
    return h('div', { class: 'set-picker', role: 'dialog', 'aria-label': 'Choose values' },
      boxes.map(([o, cb]) => h('label', { class: 'check' }, cb, ' ', o)),
      h('div', { class: 'cal-actions' },
        nullable ? h('button', { type: 'button', class: 'btn small', onclick: () => done(null) }, 'NULL') : null,
        h('span', { class: 'spacer' }),
        h('button', { type: 'button', class: 'btn small', onclick: () => done(undefined) }, 'Cancel'),
        h('button', { type: 'button', class: 'btn small primary', onclick: () => done(result()) }, 'OK')));
  });
}

export interface ValueEditor {
  el: HTMLElement;
  get(): string;
  set(text: string): void;
  error(): string | null;
  destroy(): void;
}

/** valueEditor is a CodeMirror editor for long text and JSON (highlighting, lint, folding). */
export function valueEditor(value: string, kind: Kind, readOnly: boolean, onChange: () => void): ValueEditor {
  const exts = [
    basicSetup,
    EditorView.lineWrapping,
    syntaxHighlighting(highlight),
    EditorState.readOnly.of(readOnly),
    EditorView.updateListener.of((u) => u.docChanged && onChange()),
    keymap.of([]),
  ];
  if (kind === 'json') exts.push(json(), linter(jsonParseLinter()), lintGutter());
  const view = new EditorView({ state: EditorState.create({ doc: value, extensions: exts }) });
  const el = h('div', { class: 'value-editor' }, view.dom);
  return {
    el,
    get: () => view.state.doc.toString(),
    set: (text) => view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } }),
    error: () => {
      if (kind !== 'json') return null;
      const t = view.state.doc.toString().trim();
      if (!t) return null;
      try {
        JSON.parse(t);
        return null;
      } catch (e) {
        return (e as Error).message;
      }
    },
    destroy: () => view.destroy(),
  };
}

/** hexDump shows binary values (0x… strings) in rows of 16 bytes. */
export function hexDump(value: string): string {
  const hex = value.replace(/^0x/i, '');
  const lines: string[] = [];
  for (let i = 0; i < hex.length; i += 32) {
    const chunk = hex.slice(i, i + 32);
    const bytes = chunk.match(/../g) ?? [];
    const ascii = bytes.map((b) => {
      const c = parseInt(b, 16);
      return c >= 32 && c < 127 ? String.fromCharCode(c) : '.';
    }).join('');
    lines.push(`${(i / 2).toString(16).padStart(8, '0')}  ${bytes.join(' ').padEnd(47)}  ${ascii}`);
  }
  return lines.join('\n');
}
