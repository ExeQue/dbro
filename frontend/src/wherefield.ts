// HeidiSQL-style filter: a multi-line SQL field whose text goes after "SELECT * FROM … WHERE".
import { EditorView, keymap, placeholder } from '@codemirror/view';
import { EditorState, Compartment, Prec } from '@codemirror/state';
import { minimalSetup } from 'codemirror';
import { sql } from '@codemirror/lang-sql';
import { autocompletion } from '@codemirror/autocomplete';
import { syntaxHighlighting } from '@codemirror/language';
import { h } from './util';
import { highlight } from './highlight';

/** whereField returns the editor element, a value proxy (like an input's .value) and setColumns for autocomplete. */
export function whereField(onApply: () => void) {
  const lang = new Compartment();
  const view = new EditorView({
    state: EditorState.create({
      extensions: [
        Prec.highest(keymap.of([{ key: 'Mod-Enter', run: () => (onApply(), true) }])),
        minimalSetup,
        lang.of(sql()),
        autocompletion(),
        syntaxHighlighting(highlight),
        EditorView.lineWrapping,
        placeholder("email LIKE '%@example.com' AND created_at > '2026-01-01'   (Ctrl+Enter applies)"),
        EditorView.contentAttributes.of({ 'aria-label': 'Filter rows (WHERE clause)' }),
      ],
    }),
  });
  const el = h('div', { class: 'where-field' }, view.dom);
  const proxy = {
    get value() {
      return view.state.doc.toString();
    },
    set value(v: string) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v } });
    },
  };
  return {
    el,
    proxy,
    setColumns(table: string, columns: string[]) {
      view.dispatch({ effects: lang.reconfigure(sql({ schema: { [table]: columns }, defaultTable: table, upperCaseKeywords: true })) });
    },
  };
}
