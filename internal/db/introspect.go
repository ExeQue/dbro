package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// TableInfo is one table or view in the tree.
type TableInfo struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Type   string `json:"type"` // table | view
	Rows   *int64 `json:"rows"`
	Size   *int64 `json:"size"`
}

// Routine is a function, procedure or trigger.
type Routine struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Type   string `json:"type"` // function | procedure | trigger
	Table  string `json:"table,omitempty"`
}

// Column describes one column.
type Column struct {
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Nullable      bool    `json:"nullable"`
	Default       *string `json:"default"`
	Key           string  `json:"key"` // PRI | UNI | MUL
	AutoIncrement bool    `json:"autoIncrement"`
	Comment       string  `json:"comment"`
}

// Index describes one index.
type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
	Primary bool     `json:"primary"`
}

// ForeignKey describes one foreign key.
type ForeignKey struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"refSchema"`
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`
	OnUpdate   string   `json:"onUpdate"`
	OnDelete   string   `json:"onDelete"`
}

// TableDef is a table's full structure.
type TableDef struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	IsView      bool         `json:"isView"`
	Columns     []Column     `json:"columns"`
	Indexes     []Index      `json:"indexes"`
	ForeignKeys []ForeignKey `json:"foreignKeys"`
	Create      string       `json:"create"`
}

// PrimaryKey returns the primary key columns, or nil.
func (t TableDef) PrimaryKey() []string {
	for _, ix := range t.Indexes {
		if ix.Primary {
			return ix.Columns
		}
	}
	return nil
}

func (c *Conn) rows(ctx context.Context, database, q string, args ...any) ([][]any, error) {
	p, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	r, err := c.run(ctx, p, database, q, 0, args...)
	return r.Rows, err
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func i64(v any) *int64 {
	switch x := v.(type) {
	case int64:
		return &x
	case float64:
		n := int64(x)
		return &n
	case string:
		var n int64
		if _, err := fmt.Sscan(x, &n); err == nil {
			return &n
		}
	}
	return nil
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case string:
		return x == "1" || strings.EqualFold(x, "true") || strings.EqualFold(x, "yes") || x == "t"
	}
	return false
}

// Databases lists databases (MySQL, PostgreSQL) or "main" for SQLite.
func (c *Conn) Databases(ctx context.Context) ([]string, error) {
	var q string
	switch c.Kind() {
	case "sqlite":
		return []string{"main"}, nil
	case "mysql":
		q = "SHOW DATABASES"
	case "postgres":
		q = "SELECT datname FROM pg_database WHERE NOT datistemplate AND datallowconn ORDER BY 1"
	}
	rows, err := c.rows(ctx, c.defaultDB(), q)
	if err != nil {
		return nil, err
	}
	// a lerd project only shows its own database and its variants (e.g. app_testing)
	own := c.Cfg.Source == "lerd-project" && c.Cfg.Database != ""
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		name := str(r[0])
		if own && name != c.Cfg.Database && !strings.HasPrefix(name, c.Cfg.Database+"_") {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// Tables lists tables and views with row counts and sizes where the server knows them.
func (c *Conn) Tables(ctx context.Context, database string) ([]TableInfo, error) {
	var out []TableInfo
	switch c.Kind() {
	case "mysql":
		rows, err := c.rows(ctx, database, "SELECT TABLE_NAME, TABLE_TYPE, TABLE_ROWS, DATA_LENGTH + INDEX_LENGTH FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME", database)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			t := TableInfo{Name: str(r[0]), Type: "table", Rows: i64(r[2]), Size: i64(r[3])}
			if strings.Contains(str(r[1]), "VIEW") {
				t.Type, t.Rows, t.Size = "view", nil, nil
			}
			out = append(out, t)
		}
	case "postgres":
		rows, err := c.rows(ctx, database, `SELECT n.nspname, c.relname, c.relkind::text, c.reltuples::bigint, pg_total_relation_size(c.oid)
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind IN ('r','v','m','p') AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND n.nspname NOT LIKE 'pg_temp%'
			ORDER BY 1, 2`)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			t := TableInfo{Schema: str(r[0]), Name: str(r[1]), Type: "table", Rows: i64(r[3]), Size: i64(r[4])}
			if k := str(r[2]); k == "v" || k == "m" {
				t.Type, t.Rows = "view", nil
			}
			if t.Rows != nil && *t.Rows < 0 {
				t.Rows = nil
			}
			out = append(out, t)
		}
	case "sqlite":
		rows, err := c.rows(ctx, database, "SELECT name, type FROM sqlite_master WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' ORDER BY name")
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			t := TableInfo{Schema: "main", Name: str(r[0]), Type: str(r[1])}
			if t.Type == "table" {
				if n, err := c.rows(ctx, database, "SELECT COUNT(*) FROM "+c.Q(t.Name)); err == nil && len(n) == 1 {
					t.Rows = i64(n[0][0])
				}
			}
			out = append(out, t)
		}
	}
	return out, nil
}

// Routines lists functions, procedures and triggers.
func (c *Conn) Routines(ctx context.Context, database string) ([]Routine, error) {
	var out []Routine
	add := func(rows [][]any, err error, f func(r []any) Routine) error {
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, f(r))
		}
		return nil
	}
	var err error
	switch c.Kind() {
	case "mysql":
		rows, e := c.rows(ctx, database, "SELECT ROUTINE_NAME, LOWER(ROUTINE_TYPE) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = ? ORDER BY 1", database)
		if err = add(rows, e, func(r []any) Routine { return Routine{Name: str(r[0]), Type: str(r[1])} }); err != nil {
			return nil, err
		}
		rows, e = c.rows(ctx, database, "SELECT TRIGGER_NAME, EVENT_OBJECT_TABLE FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = ? ORDER BY 1", database)
		err = add(rows, e, func(r []any) Routine { return Routine{Name: str(r[0]), Type: "trigger", Table: str(r[1])} })
	case "postgres":
		rows, e := c.rows(ctx, database, `SELECT n.nspname, p.proname, CASE p.prokind WHEN 'p' THEN 'procedure' ELSE 'function' END
			FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			LEFT JOIN pg_depend d ON d.objid = p.oid AND d.deptype = 'e'
			WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND p.prokind IN ('f','p') AND d.objid IS NULL
			ORDER BY 1, 2`)
		if err = add(rows, e, func(r []any) Routine { return Routine{Schema: str(r[0]), Name: str(r[1]), Type: str(r[2])} }); err != nil {
			return nil, err
		}
		rows, e = c.rows(ctx, database, `SELECT n.nspname, t.tgname, c.relname FROM pg_trigger t
			JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE NOT t.tgisinternal ORDER BY 1, 2`)
		err = add(rows, e, func(r []any) Routine {
			return Routine{Schema: str(r[0]), Name: str(r[1]), Type: "trigger", Table: str(r[2])}
		})
	case "sqlite":
		rows, e := c.rows(ctx, database, "SELECT name, tbl_name FROM sqlite_master WHERE type = 'trigger' ORDER BY name")
		err = add(rows, e, func(r []any) Routine {
			return Routine{Schema: "main", Name: str(r[0]), Type: "trigger", Table: str(r[1])}
		})
	}
	return out, err
}

// RoutineSource returns the CREATE statement of a routine.
func (c *Conn) RoutineSource(ctx context.Context, database string, r Routine) (string, error) {
	switch c.Kind() {
	case "mysql":
		kind := strings.ToUpper(r.Type)
		rows, err := c.rows(ctx, database, fmt.Sprintf("SHOW CREATE %s %s.%s", kind, c.Q(database), c.Q(r.Name)))
		if err != nil || len(rows) == 0 {
			return "", err
		}
		return str(rows[0][2]), nil
	case "postgres":
		if r.Type == "trigger" {
			rows, err := c.rows(ctx, database, `SELECT pg_get_triggerdef(t.oid, true) FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid
				JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND t.tgname = $2`, r.Schema, r.Name)
			if err != nil || len(rows) == 0 {
				return "", err
			}
			return str(rows[0][0]) + ";", nil
		}
		rows, err := c.rows(ctx, database, `SELECT pg_get_functiondef(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = $1 AND p.proname = $2`, r.Schema, r.Name)
		if err != nil {
			return "", err
		}
		var parts []string
		for _, row := range rows {
			parts = append(parts, strings.TrimSpace(str(row[0]))+";")
		}
		return strings.Join(parts, "\n\n"), nil
	default:
		rows, err := c.rows(ctx, database, "SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?", r.Name)
		if err != nil || len(rows) == 0 {
			return "", err
		}
		return str(rows[0][0]) + ";", nil
	}
}

var pgActions = map[string]string{"a": "NO ACTION", "r": "RESTRICT", "c": "CASCADE", "n": "SET NULL", "d": "SET DEFAULT"}

// Describe returns a table's columns, indexes, foreign keys and CREATE statement.
func (c *Conn) Describe(ctx context.Context, database, schema, table string) (TableDef, error) {
	def := TableDef{Schema: schema, Name: table}
	var err error
	switch c.Kind() {
	case "mysql":
		err = c.describeMySQL(ctx, database, &def)
	case "postgres":
		if def.Schema == "" {
			def.Schema = "public"
		}
		err = c.describePostgres(ctx, database, &def)
	case "sqlite":
		def.Schema = "main"
		err = c.describeSQLite(ctx, database, &def)
	}
	if err != nil {
		return def, err
	}
	if len(def.Columns) == 0 {
		return def, fmt.Errorf("table %s not found", table)
	}
	markKeys(&def)
	return def, nil
}

func markKeys(def *TableDef) {
	key := map[string]string{}
	for _, ix := range def.Indexes {
		for i, col := range ix.Columns {
			switch {
			case ix.Primary:
				key[col] = "PRI"
			case ix.Unique && len(ix.Columns) == 1 && key[col] != "PRI":
				key[col] = "UNI"
			case i == 0 && key[col] == "":
				key[col] = "MUL"
			}
		}
	}
	for i := range def.Columns {
		if k := key[def.Columns[i].Name]; k != "" {
			def.Columns[i].Key = k
		}
	}
}

func groupIndexes(rows [][]any, name, col, unique, primary func(r []any) any) []Index {
	var out []Index
	pos := map[string]int{}
	for _, r := range rows {
		n := str(name(r))
		i, ok := pos[n]
		if !ok {
			i = len(out)
			pos[n] = i
			out = append(out, Index{Name: n, Unique: truthy(unique(r)), Primary: truthy(primary(r))})
		}
		out[i].Columns = append(out[i].Columns, str(col(r)))
	}
	return out
}

func (c *Conn) describeMySQL(ctx context.Context, database string, def *TableDef) error {
	rows, err := c.rows(ctx, database, `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, EXTRA, COLUMN_COMMENT
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, database, def.Name)
	if err != nil {
		return err
	}
	for _, r := range rows {
		col := Column{Name: str(r[0]), Type: str(r[1]), Nullable: str(r[2]) == "YES", Comment: str(r[5])}
		if r[3] != nil {
			d := str(r[3])
			col.Default = &d
		}
		extra := strings.ToLower(str(r[4]))
		col.AutoIncrement = strings.Contains(extra, "auto_increment")
		out := col
		def.Columns = append(def.Columns, out)
	}
	rows, err = c.rows(ctx, database, `SELECT INDEX_NAME, COLUMN_NAME, NON_UNIQUE FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY INDEX_NAME = 'PRIMARY' DESC, INDEX_NAME, SEQ_IN_INDEX`, database, def.Name)
	if err != nil {
		return err
	}
	def.Indexes = groupIndexes(rows, func(r []any) any { return r[0] }, func(r []any) any { return r[1] },
		func(r []any) any { return !truthy(r[2]) }, func(r []any) any { return str(r[0]) == "PRIMARY" })
	rows, err = c.rows(ctx, database, `SELECT k.CONSTRAINT_NAME, k.COLUMN_NAME, k.REFERENCED_TABLE_SCHEMA, k.REFERENCED_TABLE_NAME, k.REFERENCED_COLUMN_NAME, r.UPDATE_RULE, r.DELETE_RULE
		FROM information_schema.KEY_COLUMN_USAGE k
		JOIN information_schema.REFERENTIAL_CONSTRAINTS r ON r.CONSTRAINT_SCHEMA = k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME = k.CONSTRAINT_NAME AND r.TABLE_NAME = k.TABLE_NAME
		WHERE k.TABLE_SCHEMA = ? AND k.TABLE_NAME = ? AND k.REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY k.CONSTRAINT_NAME, k.ORDINAL_POSITION`, database, def.Name)
	if err != nil {
		return err
	}
	def.ForeignKeys = groupFKs(rows, database)
	create, err := c.rows(ctx, database, "SHOW CREATE TABLE "+c.Q(database)+"."+c.Q(def.Name))
	if err != nil {
		return err
	}
	if len(create) > 0 {
		def.Create = str(create[0][1]) + ";"
		def.IsView = strings.Contains(strings.ToUpper(str(create[0][1])), " VIEW ")
	}
	return nil
}

func groupFKs(rows [][]any, sameSchema string) []ForeignKey {
	var out []ForeignKey
	pos := map[string]int{}
	for _, r := range rows {
		n := str(r[0])
		i, ok := pos[n]
		if !ok {
			i = len(out)
			pos[n] = i
			ref := str(r[2])
			if ref == sameSchema {
				ref = ""
			}
			out = append(out, ForeignKey{Name: n, RefSchema: ref, RefTable: str(r[3]), OnUpdate: str(r[5]), OnDelete: str(r[6])})
		}
		out[i].Columns = append(out[i].Columns, str(r[1]))
		out[i].RefColumns = append(out[i].RefColumns, str(r[4]))
	}
	return out
}

func (c *Conn) describePostgres(ctx context.Context, database string, def *TableDef) error {
	rows, err := c.rows(ctx, database, `SELECT a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
			pg_get_expr(d.adbin, d.adrelid), a.attidentity::text, coalesce(col_description(c.oid, a.attnum), ''), c.relkind::text
		FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`, def.Schema, def.Name)
	if err != nil {
		return err
	}
	for _, r := range rows {
		col := Column{Name: str(r[0]), Type: str(r[1]), Nullable: truthy(r[2]), Comment: str(r[5])}
		if r[3] != nil {
			d := str(r[3])
			if strings.HasPrefix(d, "nextval(") {
				col.AutoIncrement = true
			} else {
				col.Default = &d
			}
		}
		if id := str(r[4]); id == "a" || id == "d" {
			col.AutoIncrement = true
		}
		def.IsView = str(r[6]) == "v" || str(r[6]) == "m"
		def.Columns = append(def.Columns, col)
	}
	rows, err = c.rows(ctx, database, `SELECT i.relname, ix.indisunique, ix.indisprimary, a.attname
		FROM pg_index ix JOIN pg_class i ON i.oid = ix.indexrelid JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		CROSS JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = ix.indrelid AND a.attnum = k.attnum
		WHERE n.nspname = $1 AND t.relname = $2 ORDER BY ix.indisprimary DESC, i.relname, k.ord`, def.Schema, def.Name)
	if err != nil {
		return err
	}
	def.Indexes = groupIndexes(rows, func(r []any) any { return r[0] }, func(r []any) any { return r[3] },
		func(r []any) any { return r[1] }, func(r []any) any { return r[2] })
	rows, err = c.rows(ctx, database, `SELECT con.conname, a.attname, rn.nspname, rc.relname, ra.attname, con.confupdtype::text, con.confdeltype::text
		FROM pg_constraint con
		JOIN pg_class t ON t.oid = con.conrelid JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_class rc ON rc.oid = con.confrelid JOIN pg_namespace rn ON rn.oid = rc.relnamespace
		CROSS JOIN LATERAL unnest(con.conkey, con.confkey) WITH ORDINALITY k(col, refcol, ord)
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.col
		JOIN pg_attribute ra ON ra.attrelid = con.confrelid AND ra.attnum = k.refcol
		WHERE con.contype = 'f' AND n.nspname = $1 AND t.relname = $2 ORDER BY con.conname, k.ord`, def.Schema, def.Name)
	if err != nil {
		return err
	}
	for _, r := range rows {
		r[5], r[6] = pgActions[str(r[5])], pgActions[str(r[6])]
	}
	def.ForeignKeys = groupFKs(rows, def.Schema)
	if def.IsView {
		v, err := c.rows(ctx, database, "SELECT pg_get_viewdef(($1 || '.' || $2)::regclass, true)", c.Q(def.Schema), c.Q(def.Name))
		if err == nil && len(v) > 0 {
			def.Create = fmt.Sprintf("CREATE VIEW %s AS\n%s", c.Table(def.Schema, def.Name), str(v[0][0]))
		}
		return nil
	}
	def.Create = strings.Join(c.createTableSQL(*def, true), "\n")
	return nil
}

func (c *Conn) describeSQLite(ctx context.Context, database string, def *TableDef) error {
	rows, err := c.rows(ctx, database, "SELECT cid, name, type, \"notnull\", dflt_value, pk FROM pragma_table_info(?)", def.Name)
	if err != nil {
		return err
	}
	var pk []struct {
		pos  int64
		name string
	}
	for _, r := range rows {
		col := Column{Name: str(r[1]), Type: str(r[2]), Nullable: !truthy(r[3])}
		if r[4] != nil {
			d := str(r[4])
			col.Default = &d
		}
		if p := i64(r[5]); p != nil && *p > 0 {
			pk = append(pk, struct {
				pos  int64
				name string
			}{*p, col.Name})
		}
		def.Columns = append(def.Columns, col)
	}
	sort.Slice(pk, func(i, j int) bool { return pk[i].pos < pk[j].pos })
	if len(pk) > 0 {
		ix := Index{Name: "PRIMARY", Primary: true, Unique: true}
		for _, p := range pk {
			ix.Columns = append(ix.Columns, p.name)
		}
		def.Indexes = append(def.Indexes, ix)
	}
	master, err := c.rows(ctx, database, "SELECT type, sql FROM sqlite_master WHERE name = ?", def.Name)
	if err != nil {
		return err
	}
	if len(master) > 0 {
		def.IsView = str(master[0][0]) == "view"
		def.Create = str(master[0][1]) + ";"
		if len(pk) == 1 && strings.Contains(strings.ToUpper(def.Create), "AUTOINCREMENT") {
			for i := range def.Columns {
				if def.Columns[i].Name == pk[0].name {
					def.Columns[i].AutoIncrement = true
				}
			}
		}
	}
	idx, err := c.rows(ctx, database, "SELECT name, \"unique\", origin FROM pragma_index_list(?) ORDER BY name", def.Name)
	if err != nil {
		return err
	}
	for _, r := range idx {
		if str(r[2]) == "pk" {
			continue
		}
		ix := Index{Name: str(r[0]), Unique: truthy(r[1])}
		cols, err := c.rows(ctx, database, "SELECT name FROM pragma_index_info(?) ORDER BY seqno", ix.Name)
		if err != nil {
			return err
		}
		for _, cr := range cols {
			ix.Columns = append(ix.Columns, str(cr[0]))
		}
		def.Indexes = append(def.Indexes, ix)
		if s, err := c.rows(ctx, database, "SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ? AND sql IS NOT NULL", ix.Name); err == nil && len(s) > 0 {
			def.Create += "\n" + str(s[0][0]) + ";"
		}
	}
	fks, err := c.rows(ctx, database, "SELECT id, \"from\", \"table\", \"to\", on_update, on_delete FROM pragma_foreign_key_list(?) ORDER BY id, seq", def.Name)
	if err != nil {
		return err
	}
	pos := map[string]int{}
	for _, r := range fks {
		id := str(r[0])
		i, ok := pos[id]
		if !ok {
			i = len(def.ForeignKeys)
			pos[id] = i
			def.ForeignKeys = append(def.ForeignKeys, ForeignKey{Name: fmt.Sprintf("fk_%s_%s", def.Name, id), RefTable: str(r[2]), OnUpdate: str(r[4]), OnDelete: str(r[5])})
		}
		def.ForeignKeys[i].Columns = append(def.ForeignKeys[i].Columns, str(r[1]))
		def.ForeignKeys[i].RefColumns = append(def.ForeignKeys[i].RefColumns, str(r[3]))
	}
	return nil
}

// SortKey is one ORDER BY column.
type SortKey struct {
	Col  string `json:"col"`
	Desc bool   `json:"desc"`
}

// Browse returns one page of a table, with the total count for the filter.
func (c *Conn) Browse(ctx context.Context, database, schema, table, where string, order []SortKey, offset, limit int) (Result, int64, error) {
	p, err := c.pool(database)
	if err != nil {
		return Result{}, 0, err
	}
	from := c.Table(schema, table)
	cond := ""
	if strings.TrimSpace(where) != "" {
		cond = " WHERE " + where
	}
	var total int64
	cr, err := c.run(ctx, p, database, "SELECT COUNT(*) FROM "+from+cond, 0)
	if err != nil {
		return Result{}, 0, err
	}
	if len(cr.Rows) == 1 {
		if n := i64(cr.Rows[0][0]); n != nil {
			total = *n
		}
	}
	q := "SELECT * FROM " + from + cond
	if len(order) > 0 {
		parts := make([]string, len(order))
		for i, o := range order {
			parts[i] = c.Q(o.Col)
			if o.Desc {
				parts[i] += " DESC"
			}
		}
		q += " ORDER BY " + strings.Join(parts, ", ")
	}
	q += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	r, err := c.run(ctx, p, database, q, 0)
	return r, total, err
}

// RowChange is one pending edit from the data grid.
type RowChange struct {
	Op    string         `json:"op"`    // insert | update | delete
	Where map[string]any `json:"where"` // identifying values (primary key, or every original column)
	Set   map[string]any `json:"set"`   // new values for insert and update
	Order []string       `json:"order"` // column order for Set, so generated SQL is stable
}

// ApplyRowChanges runs grid edits in one transaction and returns the statements it ran.
func (c *Conn) ApplyRowChanges(ctx context.Context, database, schema, table string, changes []RowChange) ([]string, error) {
	p, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	tx, err := p.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	from := c.Table(schema, table)
	var done []string
	for _, ch := range changes {
		var q string
		var args []any
		where := func() string {
			var parts []string
			for _, k := range sortedKeys(ch.Where) {
				v := ch.Where[k]
				if v == nil {
					parts = append(parts, c.Q(k)+" IS NULL")
					continue
				}
				args = append(args, v)
				parts = append(parts, c.Q(k)+" = "+c.ph(len(args)))
			}
			return strings.Join(parts, " AND ")
		}
		cols := ch.Order
		if len(cols) == 0 {
			cols = sortedKeys(ch.Set)
		}
		switch ch.Op {
		case "insert":
			var names, phs []string
			for _, k := range cols {
				if _, ok := ch.Set[k]; !ok {
					continue
				}
				args = append(args, ch.Set[k])
				names = append(names, c.Q(k))
				phs = append(phs, c.ph(len(args)))
			}
			if len(names) == 0 {
				q = "INSERT INTO " + from + " DEFAULT VALUES"
				if c.Kind() == "mysql" {
					q = "INSERT INTO " + from + " () VALUES ()"
				}
			} else {
				q = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", from, strings.Join(names, ", "), strings.Join(phs, ", "))
			}
		case "update":
			var sets []string
			for _, k := range cols {
				v, ok := ch.Set[k]
				if !ok {
					continue
				}
				args = append(args, v)
				sets = append(sets, c.Q(k)+" = "+c.ph(len(args)))
			}
			if len(sets) == 0 {
				continue
			}
			q = fmt.Sprintf("UPDATE %s SET %s WHERE %s", from, strings.Join(sets, ", "), where())
		case "delete":
			q = fmt.Sprintf("DELETE FROM %s WHERE %s", from, where())
		default:
			return done, fmt.Errorf("unknown change %q", ch.Op)
		}
		if (ch.Op == "update" || ch.Op == "delete") && len(ch.Where) == 0 {
			return done, fmt.Errorf("refusing %s without a WHERE", ch.Op)
		}
		r, err := c.run(ctx, tx, database, q, 0, args...)
		if err != nil {
			return done, err
		}
		if ch.Op != "insert" && r.Affected > 1 {
			return done, fmt.Errorf("%s would change %d rows; the table needs a primary key to edit single rows", ch.Op, r.Affected)
		}
		done = append(done, q)
	}
	return done, tx.Commit()
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var _ = sql.ErrNoRows

// Defaults returns the database a connection opens by default.
func (c *Conn) Defaults() string { return c.defaultDB() }

// Completions maps table names to their column names, for SQL autocomplete.
func (c *Conn) Completions(ctx context.Context, database string) (map[string][]string, error) {
	out := map[string][]string{}
	var rows [][]any
	var err error
	switch c.Kind() {
	case "mysql":
		rows, err = c.rows(ctx, database, "SELECT TABLE_NAME, COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, ORDINAL_POSITION", database)
	case "postgres":
		rows, err = c.rows(ctx, database, `SELECT CASE WHEN table_schema = 'public' THEN table_name ELSE table_schema || '.' || table_name END, column_name
			FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog','information_schema') ORDER BY 1, ordinal_position`)
	case "sqlite":
		rows, err = c.rows(ctx, database, "SELECT m.name, p.name FROM sqlite_master m JOIN pragma_table_info(m.name) p WHERE m.type IN ('table','view') AND m.name NOT LIKE 'sqlite_%' ORDER BY m.name, p.cid")
	}
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		t := str(r[0])
		out[t] = append(out[t], str(r[1]))
	}
	return out, nil
}

// InfoItem is one line of table information (HeidiSQL's Options tab).
type InfoItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// TableInfo returns engine, sizes, auto-increment and similar facts about a table.
func (c *Conn) TableInfo(ctx context.Context, database, schema, table string) ([]InfoItem, error) {
	p, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	conn, err := p.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var out []InfoItem
	add := func(label string, v any) {
		if s := str(v); s != "" {
			out = append(out, InfoItem{label, s})
		}
	}
	switch c.Kind() {
	case "mysql":
		// MySQL 8 caches these statistics for a day unless told otherwise; MariaDB has no such variable.
		conn.ExecContext(ctx, "SET SESSION information_schema_stats_expiry = 0")
		r, err := c.run(ctx, conn, database, `SELECT ENGINE, TABLE_COLLATION, AUTO_INCREMENT, ROW_FORMAT, TABLE_ROWS, AVG_ROW_LENGTH,
			DATA_LENGTH, INDEX_LENGTH, DATA_FREE, CREATE_TIME, UPDATE_TIME, TABLE_COMMENT, TABLE_TYPE
			FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, 0, database, table)
		if err != nil || len(r.Rows) == 0 {
			return nil, err
		}
		v := r.Rows[0]
		add("Type", v[12])
		add("Engine", v[0])
		add("Collation", v[1])
		add("Auto increment (next)", v[2])
		add("Row format", v[3])
		add("Rows (estimate)", v[4])
		add("Average row length", bytes(v[5]))
		add("Data size", bytes(v[6]))
		add("Index size", bytes(v[7]))
		add("Free space", bytes(v[8]))
		add("Created", v[9])
		add("Updated", v[10])
		add("Comment", v[11])
	case "postgres":
		if schema == "" {
			schema = "public"
		}
		r, err := c.run(ctx, conn, database, `SELECT pg_get_userbyid(c.relowner), coalesce(t.spcname, 'default'), c.reltuples::bigint,
				pg_table_size(c.oid), pg_indexes_size(c.oid), pg_total_relation_size(c.oid),
				CASE c.relpersistence WHEN 'u' THEN 'unlogged' WHEN 't' THEN 'temporary' ELSE 'permanent' END,
				obj_description(c.oid, 'pg_class'), s.last_vacuum, s.last_autovacuum, s.last_analyze, s.last_autoanalyze, s.n_live_tup, s.n_dead_tup
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_tablespace t ON t.oid = c.reltablespace
			LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
			WHERE n.nspname = $1 AND c.relname = $2`, 0, schema, table)
		if err != nil || len(r.Rows) == 0 {
			return nil, err
		}
		v := r.Rows[0]
		add("Owner", v[0])
		add("Tablespace", v[1])
		add("Persistence", v[6])
		if n := i64(v[2]); n != nil && *n >= 0 {
			add("Rows (estimate)", *n)
		}
		add("Live / dead rows", fmt.Sprintf("%s / %s", str(v[12]), str(v[13])))
		add("Table size", bytes(v[3]))
		add("Index size", bytes(v[4]))
		add("Total size", bytes(v[5]))
		add("Last vacuum", later(str(v[8]), str(v[9])))
		add("Last analyze", later(str(v[10]), str(v[11])))
		add("Comment", v[7])
		// next value of every serial or identity column
		seqs, err := c.run(ctx, conn, database, `SELECT a.attname, pg_get_serial_sequence(quote_ident($1) || '.' || quote_ident($2), a.attname)
			FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
			AND pg_get_serial_sequence(quote_ident($1) || '.' || quote_ident($2), a.attname) IS NOT NULL`, 0, schema, table)
		if err == nil {
			for _, s := range seqs.Rows {
				nv, err := c.run(ctx, conn, database, "SELECT CASE WHEN is_called THEN last_value + 1 ELSE last_value END FROM "+str(s[1]), 0)
				if err == nil && len(nv.Rows) > 0 {
					add("Next value of "+str(s[0]), nv.Rows[0][0])
				}
			}
		}
	case "sqlite":
		r, err := c.run(ctx, conn, database, "SELECT COUNT(*) FROM "+c.Q(table), 0)
		if err == nil && len(r.Rows) > 0 {
			add("Rows", r.Rows[0][0])
		}
		if seq, err := c.run(ctx, conn, database, "SELECT seq + 1 FROM sqlite_sequence WHERE name = ?", 0, table); err == nil && len(seq.Rows) > 0 {
			add("Auto increment (next)", seq.Rows[0][0])
		}
		if m, err := c.run(ctx, conn, database, "SELECT type, sql FROM sqlite_master WHERE name = ?", 0, table); err == nil && len(m.Rows) > 0 {
			up := strings.ToUpper(str(m.Rows[0][1]))
			add("Type", m.Rows[0][0])
			add("Without rowid", map[bool]string{true: "yes", false: "no"}[strings.Contains(up, "WITHOUT ROWID")])
			add("Strict", map[bool]string{true: "yes", false: "no"}[strings.Contains(up, "STRICT")])
		}
		if sz, err := c.run(ctx, conn, database, "SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()", 0); err == nil && len(sz.Rows) > 0 {
			add("Database file size", bytes(sz.Rows[0][0]))
		}
	}
	return out, nil
}

// bytes formats a byte count like 1.5 MiB; "" when v is not a number.
func bytes(v any) string {
	n := i64(v)
	if n == nil {
		return ""
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	f, i := float64(*n), 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", *n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// DatabaseInfo returns facts about a database: character set and collation, sizes, object counts.
func (c *Conn) DatabaseInfo(ctx context.Context, database string) ([]InfoItem, error) {
	p, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	conn, err := p.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var out []InfoItem
	add := func(label string, v any) {
		if s := str(v); s != "" {
			out = append(out, InfoItem{label, s})
		}
	}
	// row runs a one-row query and returns its values, or nil
	row := func(q string, args ...any) []any {
		r, err := c.run(ctx, conn, database, q, 0, args...)
		if err != nil || len(r.Rows) == 0 {
			return nil
		}
		return r.Rows[0]
	}
	switch c.Kind() {
	case "mysql":
		conn.ExecContext(ctx, "SET SESSION information_schema_stats_expiry = 0")
		v := row("SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", database)
		if v == nil {
			return nil, fmt.Errorf("database %s not found", database)
		}
		add("Character set", v[0])
		add("Collation", v[1])
		if t := row(`SELECT SUM(TABLE_TYPE = 'BASE TABLE'), SUM(TABLE_TYPE = 'VIEW'), SUM(DATA_LENGTH), SUM(INDEX_LENGTH), SUM(DATA_LENGTH + INDEX_LENGTH)
			FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?`, database); t != nil {
			add("Tables", t[0])
			add("Views", t[1])
			add("Data size", bytes(t[2]))
			add("Index size", bytes(t[3]))
			add("Total size", bytes(t[4]))
		}
		if r := row("SELECT COUNT(*) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = ?", database); r != nil {
			add("Routines", r[0])
		}
		if r := row("SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = ?", database); r != nil {
			add("Triggers", r[0])
		}
	case "postgres":
		v := row(`SELECT pg_get_userbyid(d.datdba), pg_encoding_to_char(d.encoding), d.datcollate, d.datctype, t.spcname,
				CASE WHEN d.datconnlimit < 0 THEN 'unlimited' ELSE d.datconnlimit::text END, pg_database_size(d.oid),
				shobj_description(d.oid, 'pg_database')
			FROM pg_database d JOIN pg_tablespace t ON t.oid = d.dattablespace WHERE d.datname = $1`, database)
		if v == nil {
			return nil, fmt.Errorf("database %s not found", database)
		}
		add("Owner", v[0])
		add("Encoding", v[1])
		add("Collation", v[2])
		add("Character type", v[3])
		add("Tablespace", v[4])
		add("Connection limit", v[5])
		add("Size", bytes(v[6]))
		if t := row(`SELECT COUNT(DISTINCT n.nspname), COUNT(*) FILTER (WHERE c.relkind IN ('r', 'p')), COUNT(*) FILTER (WHERE c.relkind IN ('v', 'm'))
			FROM pg_namespace n LEFT JOIN pg_class c ON c.relnamespace = n.oid
			WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND n.nspname NOT LIKE 'pg_temp%'`); t != nil {
			add("Schemas", t[0])
			add("Tables", t[1])
			add("Views", t[2])
		}
		add("Comment", v[7])
	case "sqlite":
		for _, p := range []struct{ label, pragma string }{
			{"Encoding", "encoding"}, {"Journal mode", "journal_mode"}, {"Page size", "page_size"}, {"Pages", "page_count"},
			{"Free pages", "freelist_count"}, {"Auto vacuum", "auto_vacuum"}, {"Foreign keys", "foreign_keys"}, {"User version", "user_version"},
		} {
			if v := row("PRAGMA " + p.pragma); v != nil {
				add(p.label, v[0])
			}
		}
		if v := row("SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()"); v != nil {
			add("Size", bytes(v[0]))
		}
		if v := row("SELECT SUM(type = 'table' AND name NOT LIKE 'sqlite_%'), SUM(type = 'view'), SUM(type = 'index'), SUM(type = 'trigger') FROM sqlite_master"); v != nil {
			add("Tables", v[0])
			add("Views", v[1])
			add("Indexes", v[2])
			add("Triggers", v[3])
		}
	}
	return out, nil
}

// later returns the later of two timestamps formatted the same way (either may be empty).
func later(a, b string) string {
	if a > b {
		return a
	}
	return b
}
