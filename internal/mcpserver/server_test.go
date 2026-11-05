package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"
)

func TestTools(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DBRO_CONFIG_DIR", dir)
	t.Setenv("DBRO_STATE_DIR", dir)
	t.Setenv("DBRO_LERD_CONFIG", filepath.Join(dir, "none.yaml"))
	dbPath := filepath.Join(dir, "app.sqlite")
	raw, _ := sql.Open("sqlite", dbPath)
	raw.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT); INSERT INTO users (name) VALUES ('Anna'), ('Jonas')")
	raw.Close()
	os.WriteFile(filepath.Join(dir, "connections.toml"), []byte(`
[[connection]]
name = "app"
driver = "sqlite"
file = "`+dbPath+`"

[[connection]]
name = "secret"
driver = "sqlite"
file = "`+dbPath+`"
hide_from_agents = true
`), 0o600)

	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	go New("test").Run(ctx, st)
	client := mcp.NewClient(&mcp.Implementation{Name: "t"}, nil)
	sess, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	call := func(name string, args map[string]any) (string, bool) {
		r, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(r.StructuredContent)
		text := string(b)
		if r.IsError && len(r.Content) > 0 {
			text = r.Content[0].(*mcp.TextContent).Text
		}
		return text, r.IsError
	}

	out, _ := call("list_connections", nil)
	if !strings.Contains(out, `"app"`) || strings.Contains(out, "secret") {
		t.Fatalf("list_connections: %s", out)
	}
	out, isErr := call("query", map[string]any{"connection": "app", "database": "main", "sql": "SELECT name FROM users ORDER BY id"})
	if isErr || !strings.Contains(out, "Jonas") {
		t.Fatalf("query: %s", out)
	}
	if out, isErr = call("query", map[string]any{"connection": "app", "database": "main", "sql": "DELETE FROM users"}); !isErr {
		t.Fatalf("read-only query allowed DELETE: %s", out)
	}
	if out, isErr = call("execute", map[string]any{"connection": "app", "database": "main", "sql": "DELETE FROM users"}); !isErr || !strings.Contains(out, "not allowed") {
		t.Fatalf("execute without permission: %s", out)
	}
	if _, isErr = call("list_tables", map[string]any{"connection": "secret", "database": "main"}); !isErr {
		t.Fatal("hidden connection reachable")
	}
	out, _ = call("describe_table", map[string]any{"connection": "app", "database": "main", "table": "users"})
	if !strings.Contains(out, `"PRI"`) {
		t.Fatalf("describe: %s", out)
	}
}
