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
