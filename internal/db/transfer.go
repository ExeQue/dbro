package db

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
)

// Literal renders a value as a SQL literal for this dialect.
func (c *Conn) Literal(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if x {
			return "TRUE"
		}
		return "FALSE"
	case int64, float64, int, int32:
		return fmt.Sprint(x)
	case []byte:
		if !utf8.Valid(x) {
			h := hex.EncodeToString(x)
			switch c.Kind() {
			case "postgres":
				return `'\x` + h + `'::bytea`
			case "sqlite":
				return "X'" + h + "'"
			}
			return "0x" + h
		}
		return c.Literal(string(x))
	case time.Time:
		return c.Literal(Value(x))
	case string:
		s := strings.ReplaceAll(x, "'", "''")
		if c.Kind() == "mysql" {
			s = strings.ReplaceAll(s, `\`, `\\`)
		}
		return "'" + s + "'"
	}
	return c.Literal(fmt.Sprint(Value(v)))
}

// streamRows runs a query and calls f for every raw row (driver values, not converted).
func (c *Conn) streamRows(ctx context.Context, q querier, database, query string, f func(cols []string, row []any) error) (int64, error) {
	start := time.Now()
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		c.log(database, query, start, 0, err)
		return 0, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var n int64
	for rows.Next() {
		if err = rows.Scan(ptrs...); err != nil {
			break
		}
		if err = f(cols, vals); err != nil {
			break
		}
		n++
	}
	if err == nil {
		err = rows.Err()
	}
	c.log(database, query, start, n, err)
	return n, err
}

// RowWriter writes rows in one export format.
type RowWriter struct {
	format string // csv | json | sql
	table  string
	conn   *Conn
	w      *bufio.Writer
	csv    *csv.Writer
	cols   []string
	n      int
}

// NewRowWriter starts an export to w. table and conn are used by the sql format.
func NewRowWriter(w io.Writer, format, table string, conn *Conn) (*RowWriter, error) {
	switch format {
	case "csv", "json", "sql":
	default:
		return nil, fmt.Errorf("unknown format %q", format)
	}
	rw := &RowWriter{format: format, table: table, conn: conn, w: bufio.NewWriter(w)}
	if format == "csv" {
		rw.csv = csv.NewWriter(rw.w)
	}
	if format == "json" {
		rw.w.WriteString("[\n")
	}
	return rw, nil
}

// Write adds one row.
func (rw *RowWriter) Write(cols []string, row []any) error {
	if rw.cols == nil {
		rw.cols = cols
		if rw.csv != nil {
			rw.csv.Write(cols)
		}
	}
	defer func() { rw.n++ }()
	switch rw.format {
	case "csv":
		rec := make([]string, len(row))
		for i, v := range row {
			if v == nil {
				rec[i] = `\N`
				continue
			}
			rec[i] = fmt.Sprint(Value(v))
		}
		return rw.csv.Write(rec)
	case "json":
		obj := make([]string, len(row))
		for i, v := range row {
			k, _ := json.Marshal(cols[i])
			val, _ := json.Marshal(Value(v))
			obj[i] = string(k) + ": " + string(val)
		}
		if rw.n > 0 {
			rw.w.WriteString(",\n")
		}
		_, err := rw.w.WriteString("  {" + strings.Join(obj, ", ") + "}")
		return err
	default:
		vals := make([]string, len(row))
		for i, v := range row {
			vals[i] = rw.conn.Literal(v)
		}
		_, err := fmt.Fprintf(rw.w, "INSERT INTO %s (%s) VALUES (%s);\n", rw.table, rw.conn.quoteList(cols), strings.Join(vals, ", "))
		return err
	}
}

// Close flushes the export.
func (rw *RowWriter) Close() error {
	if rw.csv != nil {
		rw.csv.Flush()
		if err := rw.csv.Error(); err != nil {
			return err
		}
	}
	if rw.format == "json" {
		if rw.n > 0 {
			rw.w.WriteString("\n")
		}
		rw.w.WriteString("]\n")
	}
	return rw.w.Flush()
}

// ExportTable writes a whole table (optionally filtered) to a file.
func (c *Conn) ExportTable(ctx context.Context, database, schema, table, where, format, path string) (int64, error) {
	p, err := c.pool(database)
	if err != nil {
		return 0, err
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	rw, err := NewRowWriter(f, format, c.Table(schema, table), c)
	if err != nil {
		return 0, err
	}
	q := "SELECT * FROM " + c.Table(schema, table)
	if strings.TrimSpace(where) != "" {
		q += " WHERE " + where
	}
	n, err := c.streamRows(ctx, p, database, q, rw.Write)
	if err != nil {
		return n, err
	}
	return n, rw.Close()
}

// ExportRows writes rows already in memory (a query result) to a file.
func (c *Conn) ExportRows(cols []string, rows [][]any, format, table, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if table == "" {
		table = "result"
	}
	rw, err := NewRowWriter(f, format, c.Q(table), c)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := rw.Write(cols, r); err != nil {
			return err
		}
	}
	return rw.Close()
}

// Dump writes the structure (and optionally data) of every table and view in a database as SQL.
func (c *Conn) Dump(ctx context.Context, database, path string, withData bool, progress func(table string)) error {
	p, err := c.pool(database)
	if err != nil {
		return err
	}
	tables, err := c.Tables(ctx, database)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "-- dbro dump of %s (%s), %s\n\n", database, c.Cfg.Driver, time.Now().Format(time.RFC3339))
	switch c.Kind() {
	case "mysql":
		w.WriteString("SET NAMES utf8mb4;\nSET FOREIGN_KEY_CHECKS = 0;\n\n")
	case "sqlite":
		w.WriteString("PRAGMA foreign_keys = OFF;\nBEGIN;\n\n")
	}
	var fks []string
	var views []TableDef
	for _, t := range tables {
		if progress != nil {
			progress(t.Name)
		}
		def, err := c.Describe(ctx, database, t.Schema, t.Name)
		if err != nil {
			return err
		}
		if def.IsView {
			views = append(views, def)
			continue
		}
		name := c.Table(t.Schema, t.Name)
		fmt.Fprintf(w, "DROP TABLE IF EXISTS %s;\n", name)
		switch c.Kind() {
		case "postgres":
			for _, s := range c.createTable(def, true, false) {
				w.WriteString(s + "\n")
			}
			for _, fk := range def.ForeignKeys {
				fks = append(fks, fmt.Sprintf("ALTER TABLE %s ADD %s;", name, c.fkClause(fk, def.Schema)))
			}
		default:
			w.WriteString(def.Create + "\n")
		}
		w.WriteString("\n")
		if withData {
			rw, _ := NewRowWriter(w, "sql", name, c)
			if _, err := c.streamRows(ctx, p, database, "SELECT * FROM "+name, rw.Write); err != nil {
				return err
			}
			rw.w.Flush()
			w.WriteString("\n")
		}
	}
	for _, v := range views {
		fmt.Fprintf(w, "DROP VIEW IF EXISTS %s;\n%s\n\n", c.Table(v.Schema, v.Name), strings.TrimSuffix(v.Create, ";")+";")
	}
	for _, s := range fks {
		w.WriteString(s + "\n")
	}
	switch c.Kind() {
	case "mysql":
		w.WriteString("\nSET FOREIGN_KEY_CHECKS = 1;\n")
	case "sqlite":
		w.WriteString("\nCOMMIT;\nPRAGMA foreign_keys = ON;\n")
	}
	return w.Flush()
}

// ImportSQL runs every statement in a SQL file on one connection.
// With continueOnError, failing statements are counted and skipped.
func (c *Conn) ImportSQL(ctx context.Context, database, path string, continueOnError bool, progress func(done, total int)) (int, []string, error) {
	b, err := os.ReadFile(path) // ponytail: whole file in memory; stream if multi-GB dumps matter
	if err != nil {
		return 0, nil, err
	}
	stmts := SplitStatements(string(b))
	p, err := c.pool(database)
	if err != nil {
		return 0, nil, err
	}
	conn, err := p.Conn(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer conn.Close()
	var failed []string
	done := 0
	for i, s := range stmts {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			if !continueOnError {
				c.log(database, s, time.Now(), 0, err)
				return done, failed, fmt.Errorf("statement %d: %w", i+1, err)
			}
			failed = append(failed, fmt.Sprintf("statement %d: %v", i+1, err))
			continue
		}
		done++
		if progress != nil && (i%200 == 0 || i == len(stmts)-1) {
			progress(i+1, len(stmts))
		}
	}
	c.log(database, fmt.Sprintf("-- imported %s: %d statements, %d failed", path, done, len(failed)), time.Now(), int64(done), nil)
	return done, failed, nil
}

// CSVPreview returns the header (or generated names) and the first rows of a CSV file.
func CSVPreview(path string, delimiter rune, header bool) ([]string, [][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = delimiter
	r.FieldsPerRecord = -1
	var cols []string
	var rows [][]string
	for len(rows) < 10 {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if cols == nil {
			if header {
				cols = rec
				continue
			}
			for i := range rec {
				cols = append(cols, "column"+strconv.Itoa(i+1))
			}
		}
		rows = append(rows, rec)
	}
	return cols, rows, nil
}

// ImportCSV inserts a CSV file into a table. mapping[i] names the table column for CSV column i ("" skips it).
// Cells equal to nullText (and \N) become NULL.
func (c *Conn) ImportCSV(ctx context.Context, database, schema, table, path string, delimiter rune, header bool, mapping []string, nullText string, progress func(rows int64)) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := csv.NewReader(bufio.NewReader(f))
	r.Comma = delimiter
	r.FieldsPerRecord = -1
	if header {
		if _, err := r.Read(); err != nil {
			return 0, err
		}
	}
	var cols []string
	var idx []int
	for i, m := range mapping {
		if m != "" {
			cols = append(cols, m)
			idx = append(idx, i)
		}
	}
	if len(cols) == 0 {
		return 0, fmt.Errorf("map at least one column")
	}
	p, err := c.pool(database)
	if err != nil {
		return 0, err
	}
	tx, err := p.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	ins := newInserter(c, tx, database, c.Table(schema, table), cols)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ins.n, err
		}
		row := make([]any, len(idx))
		for j, i := range idx {
			if i >= len(rec) || rec[i] == `\N` || (nullText != "" && rec[i] == nullText) {
				continue
			}
			row[j] = rec[i]
		}
		if err := ins.add(ctx, row); err != nil {
			return ins.n, err
		}
		if progress != nil && ins.n%5000 == 0 {
			progress(ins.n)
		}
	}
	if err := ins.flush(ctx); err != nil {
		return ins.n, err
	}
	return ins.n, tx.Commit()
}

// inserter batches multi-row INSERTs.
type inserter struct {
	c        *Conn
	q        querier
	database string
	table    string
	cols     []string
	batch    int
	pending  [][]any
	n        int64
}

func newInserter(c *Conn, q querier, database, table string, cols []string) *inserter {
	batch := 60000 / max(len(cols), 1)
	if batch > 500 {
		batch = 500
	}
	return &inserter{c: c, q: q, database: database, table: table, cols: cols, batch: batch}
}

func (in *inserter) add(ctx context.Context, row []any) error {
	in.pending = append(in.pending, row)
	if len(in.pending) >= in.batch {
		return in.flush(ctx)
	}
	return nil
}

func (in *inserter) flush(ctx context.Context) error {
	if len(in.pending) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "INSERT INTO %s (%s) VALUES ", in.table, in.c.quoteList(in.cols))
	args := make([]any, 0, len(in.pending)*len(in.cols))
	for i, row := range in.pending {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(")
		for j, v := range row {
			if j > 0 {
				b.WriteString(", ")
			}
			args = append(args, v)
			b.WriteString(in.c.ph(len(args)))
		}
		b.WriteString(")")
	}
	start := time.Now()
	_, err := in.q.ExecContext(ctx, b.String(), args...)
	in.c.log(in.database, fmt.Sprintf("INSERT INTO %s (%s) VALUES … -- %d rows", in.table, in.c.quoteList(in.cols), len(in.pending)), start, int64(len(in.pending)), err)
	if err != nil {
		return err
	}
	in.n += int64(len(in.pending))
	in.pending = in.pending[:0]
	return nil
}

// CloneOptions controls Clone.
type CloneOptions struct {
	DropExisting bool `json:"dropExisting"`
	WithData     bool `json:"withData"`
}

// Clone copies tables (structure and data) from src to dst, translating types between drivers.
func Clone(ctx context.Context, src *Conn, srcDB string, tables []TableInfo, dst *Conn, dstDB, dstSchema string, opt CloneOptions, progress func(table string, rows int64)) error {
	if dst.Kind() == "postgres" && dstSchema == "" {
		dstSchema = "public"
	}
	if dst.Kind() != "postgres" {
		dstSchema = ""
	}
	dp, err := dst.pool(dstDB)
	if err != nil {
		return err
	}
	conn, err := dp.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	switch dst.Kind() {
	case "mysql":
		conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0")
		defer conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 1")
	case "sqlite":
		conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF")
		defer conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
	}
	sp, err := src.pool(srcDB)
	if err != nil {
		return err
	}
	var laterFKs []string
	for _, t := range tables {
		def, err := src.Describe(ctx, srcDB, t.Schema, t.Name)
		if err != nil {
			return err
		}
		if def.IsView {
			continue
		}
		mapped := MapTableDef(def, src.Kind(), dst.Kind())
		mapped.Schema = dstSchema
		name := dst.Table(dstSchema, mapped.Name)
		if opt.DropExisting {
			if _, err := dst.run(ctx, conn, dstDB, "DROP TABLE IF EXISTS "+name, 0); err != nil {
				return err
			}
		}
		withFKs := dst.Kind() == "sqlite"
		for _, s := range dst.createTable(mapped, true, withFKs) {
			if _, err := dst.run(ctx, conn, dstDB, strings.TrimSuffix(s, ";"), 0); err != nil {
				return fmt.Errorf("create %s: %w", mapped.Name, err)
			}
		}
		if !withFKs {
			for _, fk := range mapped.ForeignKeys {
				fk.RefSchema = ""
				laterFKs = append(laterFKs, fmt.Sprintf("ALTER TABLE %s ADD %s", name, dst.fkClause(fk, dstSchema)))
			}
		}
		if !opt.WithData {
			continue
		}
		cols := make([]string, len(mapped.Columns))
		blob := make([]bool, len(mapped.Columns))
		for i, col := range mapped.Columns {
			cols[i] = col.Name
			blob[i] = isBinaryType(col.Type)
		}
		ins := newInserter(dst, conn, dstDB, name, cols)
		_, err = src.streamRows(ctx, sp, srcDB, "SELECT * FROM "+src.Table(t.Schema, t.Name), func(_ []string, row []any) error {
			out := make([]any, len(row))
			for i, v := range row {
				out[i] = cloneValue(v, blob[i])
			}
			if err := ins.add(ctx, out); err != nil {
				return err
			}
			if progress != nil && ins.n%5000 == 0 && ins.n > 0 {
				progress(t.Name, ins.n)
			}
			return nil
		})
		if err == nil {
			err = ins.flush(ctx)
		}
		if err != nil {
			return fmt.Errorf("copy %s: %w", t.Name, err)
		}
		if progress != nil {
			progress(t.Name, ins.n)
		}
		if dst.Kind() == "postgres" {
			// move identity sequences past the copied ids
			for _, col := range mapped.Columns {
				if col.AutoIncrement {
					dst.run(ctx, conn, dstDB, fmt.Sprintf("SELECT setval(pg_get_serial_sequence('%s', '%s'), COALESCE(MAX(%s), 0) + 1, false) FROM %s",
						strings.ReplaceAll(name, "'", "''"), strings.ReplaceAll(col.Name, "'", "''"), dst.Q(col.Name), name), 0)
				}
			}
		}
	}
	for _, s := range laterFKs {
		if _, err := dst.run(ctx, conn, dstDB, s, 0); err != nil {
			return err
		}
	}
	return nil
}

func cloneValue(v any, binary bool) any {
	switch x := v.(type) {
	case []byte:
		if binary {
			return append([]byte{}, x...)
		}
		return string(x)
	case time.Time:
		return Value(x)
	case [16]byte, pgtype.Numeric:
		return Value(x)
	case nil, bool, int64, float64, string:
		return x
	case int32, int16, int, float32:
		return Value(x)
	}
	if b, err := json.Marshal(v); err == nil && (strings.HasPrefix(string(b), "{") || strings.HasPrefix(string(b), "[")) {
		return string(b) // json/jsonb and arrays from pgx
	}
	return fmt.Sprint(v)
}

func isBinaryType(t string) bool {
	t = strings.ToLower(t)
	return strings.Contains(t, "blob") || strings.Contains(t, "binary") || t == "bytea"
}

var _ = sql.ErrNoRows
