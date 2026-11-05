package db

import "strings"

// SplitStatements splits a script on semicolons, ignoring those inside quotes,
// comments and PostgreSQL dollar-quoted bodies. Empty statements are dropped.
func SplitStatements(sql string) []string {
	var out []string
	start := 0
	n := len(sql)
	for i := 0; i < n; i++ {
		c := sql[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			for i++; i < n; i++ {
				if sql[i] == '\\' && c != '`' {
					i++
					continue
				}
				if sql[i] == c {
					if i+1 < n && sql[i+1] == c { // doubled quote
						i++
						continue
					}
					break
				}
			}
		case c == '-' && i+1 < n && sql[i+1] == '-', c == '#':
			for i < n && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				i = n
			} else {
				i += end + 3
			}
		case c == '$':
			// $tag$ ... $tag$
			j := i + 1
			for j < n && (sql[j] == '_' || isAlnum(sql[j])) {
				j++
			}
			if j < n && sql[j] == '$' {
				tag := sql[i : j+1]
				end := strings.Index(sql[j+1:], tag)
				if end < 0 {
					i = n
				} else {
					i = j + end + len(tag)
				}
			}
		case c == ';':
			if s := strings.TrimSpace(sql[start:i]); s != "" {
				out = append(out, s)
			}
			start = i + 1
		}
	}
	if start < n {
		if s := strings.TrimSpace(sql[start:]); s != "" && !onlyComments(s) {
			out = append(out, s)
		}
	}
	return out
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func onlyComments(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		l := strings.TrimSpace(line)
		if l != "" && !strings.HasPrefix(l, "--") && !strings.HasPrefix(l, "#") {
			return false
		}
	}
	return true
}

// firstKeyword returns the upper-cased first word of a statement, skipping comments and parentheses.
func firstKeyword(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\r\n(")
		switch {
		case strings.HasPrefix(s, "--"), strings.HasPrefix(s, "#"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = s[i+2:]
				continue
			}
			return ""
		}
		break
	}
	end := strings.IndexFunc(s, func(r rune) bool { return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') })
	if end < 0 {
		end = len(s)
	}
	return strings.ToUpper(s[:end])
}

// returnsRows guesses whether a statement produces a result set.
func returnsRows(s string) bool {
	switch firstKeyword(s) {
	case "SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "WITH", "PRAGMA", "VALUES", "TABLE", "CHECKSUM", "ANALYZE":
		return true
	}
	return strings.Contains(strings.ToUpper(s), "RETURNING")
}
