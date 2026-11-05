// Prettify and minify SQL for the query editor.
import { format } from 'sql-formatter';

const languages = { mysql: 'mysql', postgres: 'postgresql', sqlite: 'sqlite' } as const;

/** prettify formats SQL in the dialect of the connection. */
export function prettify(sql: string, dialect: 'mysql' | 'postgres' | 'sqlite'): string {
  return format(sql, { language: languages[dialect], keywordCase: 'upper', tabWidth: 2, linesBetweenQueries: 1 });
}

/** minify drops comments and collapses whitespace outside strings and quoted names. */
export function minify(sql: string): string {
  let out = '';
  let space = false;
  for (let i = 0; i < sql.length; i++) {
    const c = sql[i];
    if (c === "'" || c === '"' || c === '`') {
      let j = i + 1;
      for (; j < sql.length; j++) {
        if (sql[j] === '\\' && c !== '`') {
          j++;
          continue;
        }
        if (sql[j] === c) {
          if (sql[j + 1] === c) {
            j++;
            continue;
          }
          break;
        }
      }
      if (space && out) out += ' ';
      space = false;
      out += sql.slice(i, j + 1);
      i = j;
      continue;
    }
    if ((c === '-' && sql[i + 1] === '-') || c === '#') {
      while (i < sql.length && sql[i] !== '\n') i++;
      space = true;
      continue;
    }
    if (c === '/' && sql[i + 1] === '*') {
      const end = sql.indexOf('*/', i + 2);
      i = end < 0 ? sql.length : end + 1;
      space = true;
      continue;
    }
    if (/\s/.test(c)) {
      space = true;
      continue;
    }
    // no space is needed around punctuation
    if (space && out && !/[(,;.]$/.test(out) && !/[),;.]/.test(c)) out += ' ';
    space = false;
    out += c;
  }
  return out.trim();
}

/** statementAt returns the [from, to) range of the statement containing pos, trimmed,
 * splitting on semicolons outside strings, comments and dollar quotes. */
export function statementAt(sql: string, pos: number): [number, number] {
  const bounds: number[] = [0];
  for (let i = 0; i < sql.length; i++) {
    const c = sql[i];
    if (c === "'" || c === '"' || c === '`') {
      for (i++; i < sql.length && sql[i] !== c; i++) if (sql[i] === '\\' && c !== '`') i++;
    } else if ((c === '-' && sql[i + 1] === '-') || c === '#') {
      while (i < sql.length && sql[i] !== '\n') i++;
    } else if (c === '/' && sql[i + 1] === '*') {
      const end = sql.indexOf('*/', i + 2);
      i = end < 0 ? sql.length : end + 1;
    } else if (c === '$') {
      const m = /^\$\w*\$/.exec(sql.slice(i));
      if (m) {
        const end = sql.indexOf(m[0], i + m[0].length);
        i = end < 0 ? sql.length : end + m[0].length - 1;
      }
    } else if (c === ';') {
      bounds.push(i + 1);
    }
  }
  bounds.push(sql.length);
  // a cursor right after a semicolon belongs to the statement it ends
  let k = bounds.findIndex((b, j) => j > 0 && pos < b);
  if (k < 0) k = bounds.length - 1;
  if (k > 1 && sql.slice(bounds[k - 1], pos).trim() === '' ) k--;
  let from = bounds[k - 1], to = bounds[k];
  while (from < to && /\s/.test(sql[from])) from++;
  while (to > from && /[\s;]/.test(sql[to - 1])) to--;
  return [from, to];
}
