package db

import (
	"context"
	"reflect"
	"testing"
)

func TestPlaceholders(t *testing.T) {
	got := Placeholders(`SELECT * FROM users WHERE email = :email AND id > :id -- :comment
		AND note = ':not' AND created::date = :day AND tags ?| array['x'] AND @n := 1 AND name = :email;
		SELECT ? , ?`)
	want := []string{"email", "id", "day", "1", "2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestBindAndRun(t *testing.T) {
	c := openSQLite(t)
	ctx := context.Background()
	p, _ := c.pool("main")
	p.Exec("CREATE TABLE u (id INTEGER PRIMARY KEY, email TEXT, note TEXT); INSERT INTO u VALUES (1, 'a@x', 'x:y'), (2, 'b@x', NULL)")

	rs, err := c.QueryParams(ctx, "main", "t", "SELECT id FROM u WHERE email = :email OR id = :id OR email = :email; SELECT note FROM u WHERE id = ? AND note IS NOT ?", 0,
		map[string]any{"email": "a@x", "id": "2", "1": "1", "2": nil})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || len(rs[0].Rows) != 2 || rs[1].Rows[0][0] != "x:y" {
		t.Fatalf("%+v", rs)
	}
	if _, err := c.QueryParams(ctx, "main", "t", "SELECT :missing", 0, map[string]any{}); err == nil {
		t.Fatal("missing value accepted")
	}

	pg := &Conn{Cfg: c.Cfg}
	pg.Cfg.Driver = "postgres"
	sql, args, _ := pg.Bind("SELECT :a, :b, :a, $$:x$$, '?' ", map[string]any{"a": 1, "b": "z"})
	if sql != "SELECT $1, $2, $3, $$:x$$, '?' " || len(args) != 3 {
		t.Fatalf("%q %v", sql, args)
	}
	if got := pg.interpolate(sql, args); got != "SELECT 1, 'z', 1, $$:x$$, '?' " {
		t.Fatalf("interpolate: %q", got)
	}
}

func TestPostgresParams(t *testing.T) {
	c, name := lerdConn(t, "postgres", 5432, "postgres")
	rs, err := c.QueryParams(context.Background(), name, "t", "SELECT :n::int + 1, :t, :x IS NULL, ?, 5 > :m", 0,
		map[string]any{"n": "41", "t": "o'hara", "x": nil, "1": "pos", "m": "3"})
	if err != nil {
		t.Fatal(err)
	}
	row := rs[0].Rows[0]
	if row[0] != int64(42) || row[1] != "o'hara" || row[2] != true || row[3] != "pos" || row[4] != true {
		t.Fatalf("%v", row)
	}
	if rs[0].SQL != "SELECT '41'::int + 1, 'o''hara', NULL IS NULL, 'pos', 5 > '3'" {
		t.Fatalf("logged %q", rs[0].SQL)
	}
}
