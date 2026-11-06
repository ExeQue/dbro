package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ExeQue/dbro/internal/config"
	"github.com/ExeQue/dbro/internal/sshtunnel"
	"github.com/ExeQue/dbro/internal/sshtunnel/sshtest"
)

func ptr(s string) *string { return &s }

func TestSplitStatements(t *testing.T) {
	got := SplitStatements("SELECT 'a;b'; -- c;\nSELECT \"x;\"; CREATE FUNCTION f() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql; /* ; */ SELECT 2")
	if len(got) != 4 || !strings.Contains(got[2], "$$ SELECT 1; $$") {
		t.Fatalf("%q", got)
	}
}

func openSQLite(t *testing.T) *Conn {
	t.Helper()
	t.Setenv("DBRO_STATE_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "t.sqlite")
	os.WriteFile(path, nil, 0o600)
	c, err := Open(context.Background(), config.Connection{Name: "t", Driver: "sqlite", File: path}, "", "", "gui")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// exercise runs the same scenario against any dialect.
func exercise(t *testing.T, c *Conn, database, schema string) {
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	idType := "integer"
	if c.Kind() == "mysql" {
		idType = "bigint unsigned"
	}
	ops := []SchemaOp{
		{Kind: "create_table", Schema: schema, Def: &TableDef{Name: "users", Columns: []Column{
			{Name: "id", Type: idType, AutoIncrement: true},
			{Name: "name", Type: "varchar(100)"},
			{Name: "email", Type: "varchar(200)", Nullable: true},
		}, Indexes: []Index{{Name: "PRIMARY", Columns: []string{"id"}, Primary: true, Unique: true}, {Name: "users_email_unique", Columns: []string{"email"}, Unique: true}}}},
		{Kind: "create_table", Schema: schema, Def: &TableDef{Name: "orders", Columns: []Column{
			{Name: "id", Type: idType, AutoIncrement: true},
			{Name: "user_id", Type: idType},
			{Name: "total", Type: "decimal(10,2)", Default: ptr("0")},
		}, Indexes: []Index{{Name: "PRIMARY", Columns: []string{"id"}, Primary: true, Unique: true}}}},
	}
	stmts, err := c.PlanSchema(ctx, database, ops)
	must(err)
	must(c.ApplySchema(ctx, database, stmts))

	_, err = c.ApplyRowChanges(ctx, database, schema, "users", []RowChange{
		{Op: "insert", Set: map[string]any{"name": "Anna", "email": "anna@example.com"}},
		{Op: "insert", Set: map[string]any{"name": "Jonas", "email": nil}},
	})
	must(err)
	_, err = c.ApplyRowChanges(ctx, database, schema, "orders", []RowChange{
		{Op: "insert", Set: map[string]any{"user_id": "1", "total": "12.50"}},
		{Op: "insert", Set: map[string]any{"user_id": "2"}},
	})
	must(err)
	_, err = c.ApplyRowChanges(ctx, database, schema, "users", []RowChange{{Op: "update", Where: map[string]any{"id": int64(2)}, Set: map[string]any{"name": "Jonas B"}}})
	must(err)

	res, total, err := c.Browse(ctx, database, schema, "users", "", []SortKey{{Col: "id"}}, 0, 1000)
	must(err)
	if total != 2 || res.Rows[1][1] != "Jonas B" || res.Rows[1][2] != nil {
		t.Fatalf("browse: %d %v", total, res.Rows)
	}

	// schema editing: add, modify (rename + move), index, fk, drop
	stmts, err = c.PlanSchema(ctx, database, []SchemaOp{
		{Kind: "add_column", Schema: schema, Table: "users", Column: &Column{Name: "age", Type: "integer", Nullable: true}},
		{Kind: "modify_column", Schema: schema, Table: "users", Name: "name", Column: &Column{Name: "full_name", Type: "varchar(150)"}},
		{Kind: "add_index", Schema: schema, Table: "users", Index: &Index{Columns: []string{"full_name"}}},
		{Kind: "add_fk", Schema: schema, Table: "orders", FK: &ForeignKey{Name: "orders_user_id_foreign", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}, OnDelete: "CASCADE"}},
	})
	must(err)
	must(c.ApplySchema(ctx, database, stmts))
	def, err := c.Describe(ctx, database, schema, "users")
	must(err)
	if _, i := findColumn(def.Columns, "full_name"); i < 0 {
		t.Fatalf("rename failed: %+v", def.Columns)
	}
	if def.Columns[0].Key != "PRI" || len(def.Indexes) < 3 {
		t.Fatalf("indexes: %+v", def.Indexes)
	}
	od, err := c.Describe(ctx, database, schema, "orders")
	must(err)
	if len(od.ForeignKeys) != 1 || od.ForeignKeys[0].RefTable != "users" || od.ForeignKeys[0].OnDelete != "CASCADE" {
		t.Fatalf("fk: %+v", od.ForeignKeys)
	}
	res, _, err = c.Browse(ctx, database, schema, "users", "", []SortKey{{Col: "id"}}, 0, 10)
	must(err)
	if res.Rows[0][1] != "Anna" {
		t.Fatalf("data lost after rebuild: %v", res.Rows)
	}

	stmts, err = c.PlanSchema(ctx, database, []SchemaOp{
		{Kind: "drop_fk", Schema: schema, Table: "orders", Name: od.ForeignKeys[0].Name},
		{Kind: "drop_column", Schema: schema, Table: "users", Name: "age"},
		{Kind: "rename_table", Schema: schema, Table: "orders", Name: "purchases"},
	})
	must(err)
	must(c.ApplySchema(ctx, database, stmts))

	tables, err := c.Tables(ctx, database)
	must(err)
	var names []string
	for _, tb := range tables {
		if tb.Schema == schema || c.Kind() == "mysql" {
			names = append(names, tb.Name)
		}
	}
	if strings.Join(names, ",") != "purchases,users" {
		t.Fatalf("tables: %v", names)
	}

	info, err := c.TableInfo(ctx, database, schema, "users")
	must(err)
	facts := map[string]string{}
	for _, it := range info {
		facts[it.Label] = it.Value
	}
	next := facts["Auto increment (next)"] + facts["Next value of id"]
	if next != "3" {
		t.Fatalf("next auto increment = %q in %v", next, facts)
	}

	dbInfo, err := c.DatabaseInfo(ctx, database)
	must(err)
	dbFacts := map[string]string{}
	for _, it := range dbInfo {
		dbFacts[it.Label] = it.Value
	}
	if dbFacts["Tables"] != "2" || dbFacts["Collation"]+dbFacts["Encoding"] == "" {
		t.Fatalf("database info: %v", dbFacts)
	}
	if c.Kind() == "mysql" {
		cols, err := c.Collations(ctx)
		must(err)
		found := false
		for _, co := range cols {
			found = found || co.Name == "utf8mb4_bin" && co.Charset == "utf8mb4"
		}
		if !found {
			t.Fatalf("utf8mb4_bin missing from %d collations", len(cols))
		}
		stmts, err := c.PlanSchema(ctx, database, []SchemaOp{
			{Kind: "alter_database", Name: database, Charset: "utf8mb4", Collation: "utf8mb4_bin"},
			{Kind: "convert_table", Table: "users", Charset: "utf8mb4", Collation: "utf8mb4_bin"},
		})
		must(err)
		must(c.ApplySchema(ctx, database, stmts))
		after, err := c.DatabaseInfo(ctx, database)
		must(err)
		got := map[string]string{}
		for _, it := range after {
			got[it.Label] = it.Value
		}
		tinfo, err := c.TableInfo(ctx, database, schema, "users")
		must(err)
		tc := ""
		for _, it := range tinfo {
			if it.Label == "Collation" {
				tc = it.Value
			}
		}
		if got["Collation"] != "utf8mb4_bin" || tc != "utf8mb4_bin" {
			t.Fatalf("after alter: database %q, table %q", got["Collation"], tc)
		}
		if _, err := c.PlanSchema(ctx, database, []SchemaOp{{Kind: "alter_database", Name: database, Charset: "utf8mb4", Collation: "x; DROP"}}); err == nil {
			t.Fatal("collation not validated")
		}
	}

	results, err := c.Query(ctx, database, "tab1", "SELECT COUNT(*) AS n FROM "+c.Table(schema, "users")+"; SELECT 1", 100)
	must(err)
	if len(results) != 2 || results[0].Columns[0] != "n" {
		t.Fatalf("query: %+v", results)
	}
	if _, err := c.QueryReadOnly(ctx, database, "DELETE FROM "+c.Table(schema, "users"), 10); err == nil {
		t.Fatal("read-only query allowed a DELETE")
	}
	if _, err := c.Explain(ctx, database, "SELECT * FROM "+c.Table(schema, "users")); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for _, f := range []string{"csv", "json", "sql"} {
		if _, err := c.ExportTable(ctx, database, schema, "users", "", f, filepath.Join(dir, "users."+f)); err != nil {
			t.Fatal(f, err)
		}
	}
	must(c.Dump(ctx, database, filepath.Join(dir, "dump.sql"), true, nil))

	// clone everything into a fresh SQLite file and compare
	dst := openSQLite(t)
	must(Clone(ctx, c, database, tables, dst, "main", "", CloneOptions{WithData: true}, nil))
	res, _, err = dst.Browse(ctx, "main", "main", "users", "", []SortKey{{Col: "id"}}, 0, 10)
	must(err)
	if len(res.Rows) != 2 || res.Rows[1][1] != "Jonas B" {
		t.Fatalf("clone: %v", res.Rows)
	}

	// CSV import back into the source
	csvPath := filepath.Join(dir, "in.csv")
	os.WriteFile(csvPath, []byte("full_name,email\nPeter,peter@example.com\nSofie,\\N\n"), 0o600)
	n, err := c.ImportCSV(ctx, database, schema, "users", csvPath, ',', true, []string{"full_name", "email"}, "", nil)
	must(err)
	if n != 2 {
		t.Fatalf("imported %d", n)
	}
}

func TestSQLite(t *testing.T) {
	c := openSQLite(t)
	exercise(t, c, "main", "main")

	// dump of a SQLite db imports into another SQLite db
	dir := t.TempDir()
	ctx := context.Background()
	if err := c.Dump(ctx, "main", filepath.Join(dir, "d.sql"), true, nil); err != nil {
		t.Fatal(err)
	}
	dst := openSQLite(t)
	if _, _, err := dst.ImportSQL(ctx, "main", filepath.Join(dir, "d.sql"), false, nil); err != nil {
		t.Fatal(err)
	}
	tables, _ := dst.Tables(ctx, "main")
	if len(tables) != 2 || *tables[1].Rows != 4 {
		t.Fatalf("imported tables: %+v", tables)
	}
}

// lerdConn connects to the local lerd services when DBRO_TEST_LERD=1.
func lerdConn(t *testing.T, driver string, port int, user string) (*Conn, string) {
	if os.Getenv("DBRO_TEST_LERD") == "" {
		t.Skip("set DBRO_TEST_LERD=1 to run against lerd's MySQL and PostgreSQL")
	}
	t.Setenv("DBRO_STATE_DIR", t.TempDir())
	ctx := context.Background()
	cfg := config.Connection{Name: "lerd-" + driver, Driver: driver, Host: "127.0.0.1", Port: port, User: user}
	c, err := Open(ctx, cfg, "lerd", "", "gui")
	if err != nil {
		t.Fatal(err)
	}
	const name = "dbro_selftest"
	stmts := []string{"DROP DATABASE IF EXISTS " + c.Q(name), "CREATE DATABASE " + c.Q(name)}
	p, _ := c.pool(c.defaultDB())
	for _, s := range stmts {
		if _, err := p.ExecContext(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		c.Close()
		c2, err := Open(context.Background(), cfg, "lerd", "", "gui")
		if err == nil {
			p, _ := c2.pool(c2.defaultDB())
			p.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+c2.Q(name))
			c2.Close()
		}
	})
	return c, name
}

func TestMySQL(t *testing.T) {
	c, name := lerdConn(t, "mysql", 3307, "root")
	exercise(t, c, name, "")
}

func TestPostgres(t *testing.T) {
	c, name := lerdConn(t, "postgres", 5432, "postgres")
	exercise(t, c, name, "public")
}

func TestSQLiteRebuildKeepsUnique(t *testing.T) {
	c := openSQLite(t)
	ctx := context.Background()
	p, _ := c.pool("main")
	p.Exec(`CREATE TABLE u (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT UNIQUE, name TEXT)`)
	stmts, err := c.PlanSchema(ctx, "main", []SchemaOp{{Kind: "modify_column", Table: "u", Name: "name", Column: &Column{Name: "full_name", Type: "TEXT", Nullable: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplySchema(ctx, "main", stmts); err != nil {
		t.Fatal(err)
	}
	def, _ := c.Describe(ctx, "main", "", "u")
	if def.Columns[1].Key != "UNI" {
		t.Fatalf("unique lost: %+v", def.Indexes)
	}
	stmts, err = c.PlanSchema(ctx, "main", []SchemaOp{{Kind: "drop_index", Table: "u", Name: def.Indexes[1].Name}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplySchema(ctx, "main", stmts); err != nil {
		t.Fatal(err)
	}
}

func TestCloneSQLiteToMySQL(t *testing.T) {
	dst, name := lerdConn(t, "mysql", 3307, "root")
	src := openSQLite(t)
	ctx := context.Background()
	p, _ := src.pool("main")
	if _, err := p.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT UNIQUE, status TEXT DEFAULT 'pending', meta TEXT, created_at TEXT DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE orders (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id), total NUMERIC DEFAULT 0);
		INSERT INTO users (email, meta) VALUES ('a@example.com', '{"x":1}'); INSERT INTO orders (user_id, total) VALUES (1, 12.5);`); err != nil {
		t.Fatal(err)
	}
	tables, _ := src.Tables(ctx, "main")
	if err := Clone(ctx, src, "main", tables, dst, name, "", CloneOptions{WithData: true}, nil); err != nil {
		t.Fatal(err)
	}
	res, _, err := dst.Browse(ctx, name, "", "users", "", []SortKey{{Col: "id"}}, 0, 10)
	if err != nil || len(res.Rows) != 1 || res.Rows[0][2] != "pending" {
		t.Fatalf("%v %v", res.Rows, err)
	}
}

func TestThroughSSHAndTLS(t *testing.T) {
	if os.Getenv("DBRO_TEST_LERD") == "" {
		t.Skip("set DBRO_TEST_LERD=1")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("DBRO_STATE_DIR", t.TempDir())
	port := sshtest.Server(t, "pw", nil)
	ctx := context.Background()
	s := &config.SSH{Host: "127.0.0.1", Port: port, User: "dev"}
	uh, err := sshtunnel.FetchHostKey(*s)
	if err != nil {
		t.Fatal(err)
	}
	sshtunnel.Trust(uh)
	for _, cfg := range []config.Connection{
		{Name: "pg", Driver: "postgres", Host: "127.0.0.1", Port: 5432, User: "postgres", SSH: s},
		{Name: "my", Driver: "mysql", Host: "127.0.0.1", Port: 3307, User: "root", SSH: s},
		{Name: "my-tls", Driver: "mysql", Host: "127.0.0.1", Port: 3307, User: "root", SSLMode: "require"},
	} {
		c, err := Open(ctx, cfg, "lerd", "pw", "gui")
		if err != nil {
			t.Fatalf("%s: %v", cfg.Name, err)
		}
		if _, err := c.Databases(ctx); err != nil {
			t.Fatalf("%s: %v", cfg.Name, err)
		}
		if cfg.SSLMode == "require" {
			rs, err := c.QueryReadOnly(ctx, "", "SHOW SESSION STATUS LIKE 'Ssl_cipher'", 1)
			if err != nil || len(rs) == 0 || len(rs[0].Rows) == 0 || rs[0].Rows[0][1] == "" {
				t.Fatalf("no TLS cipher: %+v %v", rs, err)
			}
		}
		c.Close()
	}
}

func TestProjectDatabasesOnlyOwn(t *testing.T) {
	c, name := lerdConn(t, "mysql", 3307, "root")
	ctx := context.Background()
	p, _ := c.pool(c.defaultDB())
	p.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+c.Q(name+"_testing"))
	t.Cleanup(func() { p.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+c.Q(name+"_testing")) })
	c.Cfg.Source, c.Cfg.Database = "lerd-project", name
	dbs, err := c.Databases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(dbs, ",") != name+","+name+"_testing" {
		t.Fatalf("%v", dbs)
	}
}
