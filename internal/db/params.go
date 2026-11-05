package db

import (
	"fmt"
	"strconv"
	"strings"
)

// walkParams calls visit for every placeholder outside strings, quoted names, comments and
// PostgreSQL dollar-quoted bodies: ":name", "?" and "$1". visit returns the replacement text.
// "::" casts, ":=" and Postgres' jsonb operators "?|" and "?&" are not placeholders.
func walkParams(sql string, visit func(tok string) string) string {
	var b strings.Builder
	n := len(sql)
	for i := 0; i < n; i++ {
		c := sql[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			j := i + 1
			for ; j < n; j++ {
				if sql[j] == '\\' && c != '`' {
					j++
					continue
				}
				if sql[j] == c {
					if j+1 < n && sql[j+1] == c {
						j++
						continue
					}
					break
				}
			}
			end := min(j+1, n)
			b.WriteString(sql[i:end])
			i = end - 1
		case c == '-' && i+1 < n && sql[i+1] == '-', c == '#':
			j := strings.IndexByte(sql[i:], '\n')
			if j < 0 {
				j = n - i
			}
			b.WriteString(sql[i : i+j])
			i += j - 1
		case c == '/' && i+1 < n && sql[i+1] == '*':
			j := strings.Index(sql[i+2:], "*/")
			end := n
			if j >= 0 {
				end = i + 2 + j + 2
			}
			b.WriteString(sql[i:end])
			i = end - 1
		case c == '$':
			j := i + 1
			for j < n && sql[j] >= '0' && sql[j] <= '9' {
				j++
			}
			if j > i+1 {
				b.WriteString(visit(sql[i:j]))
				i = j - 1
				continue
			}
			for j < n && (sql[j] == '_' || isAlnum(sql[j])) {
				j++
			}
			if j < n && sql[j] == '$' { // $tag$ … $tag$
				tag := sql[i : j+1]
				end := strings.Index(sql[j+1:], tag)
				stop := n
				if end >= 0 {
					stop = j + 1 + end + len(tag)
				}
				b.WriteString(sql[i:stop])
				i = stop - 1
				continue
			}
			b.WriteByte(c)
		case c == ':' && i+1 < n && (sql[i+1] == '_' || isLetter(sql[i+1])) && (i == 0 || sql[i-1] != ':'):
			j := i + 1
			for j < n && (sql[j] == '_' || isAlnum(sql[j])) {
				j++
			}
			b.WriteString(visit(sql[i:j]))
			i = j - 1
		case c == '?' && !(i+1 < n && (sql[i+1] == '|' || sql[i+1] == '&')):
			b.WriteString(visit("?"))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// Placeholders lists the parameters a script needs, in order of first use:
// names without the colon, and "1", "2", … for positional "?" markers.
func Placeholders(script string) []string {
	var out []string
	seen := map[string]bool{}
	for _, stmt := range SplitStatements(script) {
		pos := 0
		walkParams(stmt, func(tok string) string {
			name := strings.TrimPrefix(tok, ":")
			if tok == "?" {
				pos++
				name = strconv.Itoa(pos)
			}
			if strings.HasPrefix(tok, "$") || seen[name] {
				return tok
			}
			seen[name] = true
			out = append(out, name)
			return tok
		})
	}
	return out
}

// Bind rewrites :name and ? placeholders to the driver's markers and returns the arguments.
// Positional "?" markers count from 1 in each statement. A nil value binds NULL.
func (c *Conn) Bind(stmt string, values map[string]any) (string, []any, error) {
	var args []any
	var missing []string
	pos := 0
	out := walkParams(stmt, func(tok string) string {
		if strings.HasPrefix(tok, "$") {
			return tok
		}
		name := strings.TrimPrefix(tok, ":")
		if tok == "?" {
			pos++
			name = strconv.Itoa(pos)
		}
		v, ok := values[name]
		if !ok {
			missing = append(missing, tok)
			return tok
		}
		args = append(args, v)
		return c.ph(len(args))
	})
	if len(missing) > 0 {
		return "", nil, fmt.Errorf("no value for %s", strings.Join(missing, ", "))
	}
	return out, args, nil
}

// interpolate shows a statement with its arguments filled in, for the SQL log only.
func (c *Conn) interpolate(stmt string, args []any) string {
	if len(args) == 0 {
		return stmt
	}
	i := 0
	return walkParams(stmt, func(tok string) string {
		switch {
		case tok == "?" && i < len(args):
			i++
			return c.Literal(args[i-1])
		case strings.HasPrefix(tok, "$"):
			if n, err := strconv.Atoi(tok[1:]); err == nil && n >= 1 && n <= len(args) {
				return c.Literal(args[n-1])
			}
		}
		return tok
	})
}
