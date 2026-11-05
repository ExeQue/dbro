// Syntax colors for CodeMirror, taken from the active theme.
import { HighlightStyle } from '@codemirror/language';
import { tags as t } from '@lezer/highlight';

export const highlight = HighlightStyle.define([
  { tag: [t.keyword, t.operatorKeyword, t.modifier], color: 'var(--magenta)', fontWeight: '600' },
  { tag: [t.string, t.special(t.string)], color: 'var(--green)' },
  { tag: [t.number, t.bool, t.null], color: 'var(--orange)' },
  { tag: [t.function(t.variableName), t.standard(t.name), t.typeName], color: 'var(--cyan)' },
  { tag: [t.comment, t.lineComment, t.blockComment], color: 'var(--dark-foreground)', fontStyle: 'italic' },
  { tag: [t.special(t.name), t.quote], color: 'var(--yellow)' },
  { tag: t.punctuation, color: 'var(--light-foreground)' },
  { tag: [t.propertyName], color: 'var(--blue)' },
]);
