// Package mcpserver exposes dbro's saved connections to agents over MCP (stdio).
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ExeQue/dbro/internal/config"
	"github.com/ExeQue/dbro/internal/db"
	"github.com/ExeQue/dbro/internal/manager"
)

type connectionInfo struct {
	Name             string `json:"name"`
	Driver           string `json:"driver"`
	Source           string `json:"source"`
	Database         string `json:"database,omitempty"`
	AllowAgentWrites bool   `json:"allow_agent_writes"`
}

type connectionsOut struct {
	Connections []connectionInfo `json:"connections"`
}

type connIn struct {
	Connection string `json:"connection" jsonschema:"name of a connection from list_connections"`
}

type databasesOut struct {
	Databases []string `json:"databases"`
}

type dbIn struct {
	Connection string `json:"connection" jsonschema:"name of a connection from list_connections"`
	Database   string `json:"database" jsonschema:"database name; for SQLite use main"`
}

type tablesOut struct {
	Tables []db.TableInfo `json:"tables"`
}

type tableIn struct {
	Connection string `json:"connection" jsonschema:"name of a connection from list_connections"`
	Database   string `json:"database" jsonschema:"database name; for SQLite use main"`
	Schema     string `json:"schema,omitempty" jsonschema:"PostgreSQL schema, default public"`
	Table      string `json:"table" jsonschema:"table or view name"`
}

type queryIn struct {
	Connection string `json:"connection" jsonschema:"name of a connection from list_connections"`
	Database   string `json:"database" jsonschema:"database name; for SQLite use main"`
	SQL        string `json:"sql" jsonschema:"one or more SQL statements separated by semicolons"`
	Limit      int    `json:"limit,omitempty" jsonschema:"maximum rows per result set, default 100, max 1000"`
}

type resultsOut struct {
	Results []db.Result `json:"results"`
}

// New builds the server with dbro's tools.
func New(version string) *mcp.Server {
	m := manager.New("mcp")
	s := mcp.NewServer(&mcp.Implementation{Name: "dbro", Version: version}, &mcp.ServerOptions{
		Instructions: "dbro gives read access to the user's saved database connections (MySQL, MariaDB, PostgreSQL, SQLite). " +
			"Start with list_connections, then list_databases, list_tables and describe_table before writing queries. " +
			"query always runs in a read-only transaction. execute only works on connections where the user allowed agent writes.",
	})

	visible := func(name string) error {
		c, err := m.Find(name)
		if err != nil {
			return err
		}
		if c.HideFromAgents {
			return manager.ErrHidden
		}
		return nil
	}
	timeout := func(ctx context.Context) (context.Context, context.CancelFunc) {
		st, _ := config.LoadSettings()
		return context.WithTimeout(ctx, time.Duration(st.QueryTimeout)*time.Second)
	}
	open := func(ctx context.Context, name string) (*db.Conn, error) {
		if err := visible(name); err != nil {
			return nil, err
		}
		c, err := m.Conn(ctx, name)
		var np *manager.NeedPasswordError
		if errors.As(err, &np) {
			return nil, fmt.Errorf("%w. Open the connection once in the dbro app and save its password to the keyring", err)
		}
		return c, err
	}

	mcp.AddTool(s, &mcp.Tool{Name: "list_connections", Description: "List the database connections saved in dbro. Passwords are never returned."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, connectionsOut, error) {
			conns, _ := m.Connections()
			out := connectionsOut{Connections: []connectionInfo{}}
			for _, c := range conns {
				if c.HideFromAgents {
					continue
				}
				out.Connections = append(out.Connections, connectionInfo{Name: c.Name, Driver: c.Driver, Source: c.Source, Database: c.Database, AllowAgentWrites: c.AllowAgentWrites})
			}
			return nil, out, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_databases", Description: "List the databases on a connection (PostgreSQL: databases; SQLite: main)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in connIn) (*mcp.CallToolResult, databasesOut, error) {
			ctx, cancel := timeout(ctx)
			defer cancel()
			c, err := open(ctx, in.Connection)
			if err != nil {
				return nil, databasesOut{}, err
			}
			dbs, err := c.Databases(ctx)
			return nil, databasesOut{Databases: dbs}, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_tables", Description: "List tables and views in a database with row counts and sizes."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in dbIn) (*mcp.CallToolResult, tablesOut, error) {
			ctx, cancel := timeout(ctx)
			defer cancel()
			c, err := open(ctx, in.Connection)
			if err != nil {
				return nil, tablesOut{}, err
			}
			ts, err := c.Tables(ctx, in.Database)
			return nil, tablesOut{Tables: ts}, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "describe_table", Description: "Show a table's columns, indexes, foreign keys and CREATE statement."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in tableIn) (*mcp.CallToolResult, db.TableDef, error) {
			ctx, cancel := timeout(ctx)
			defer cancel()
			c, err := open(ctx, in.Connection)
			if err != nil {
				return nil, db.TableDef{}, err
			}
			def, err := c.Describe(ctx, in.Database, in.Schema, in.Table)
			return nil, def, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "query", Description: "Run read-only SQL (SELECT, SHOW, EXPLAIN, ...) inside a read-only transaction. Rows are returned as JSON; results are cut at limit rows."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in queryIn) (*mcp.CallToolResult, resultsOut, error) {
			ctx, cancel := timeout(ctx)
			defer cancel()
			c, err := open(ctx, in.Connection)
			if err != nil {
				return nil, resultsOut{}, err
			}
			limit := in.Limit
			if limit <= 0 {
				limit = 100
			}
			if limit > 1000 {
				limit = 1000
			}
			rs, err := c.QueryReadOnly(ctx, in.Database, in.SQL, limit)
			if rs == nil {
				rs = []db.Result{}
			}
			return nil, resultsOut{Results: rs}, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "execute", Description: "Run SQL that changes data or schema. Only allowed on connections where allow_agent_writes is true (see list_connections)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in queryIn) (*mcp.CallToolResult, resultsOut, error) {
			ctx, cancel := timeout(ctx)
			defer cancel()
			cfg, err := m.Find(in.Connection)
			if err != nil {
				return nil, resultsOut{}, err
			}
			if !cfg.AllowAgentWrites {
				return nil, resultsOut{}, errors.New("writes are not allowed on this connection; the user can enable allow_agent_writes in dbro")
			}
			c, err := open(ctx, in.Connection)
			if err != nil {
				return nil, resultsOut{}, err
			}
			rs, err := c.Exec(ctx, in.Database, in.SQL)
			if rs == nil {
				rs = []db.Result{}
			}
			return nil, resultsOut{Results: rs}, err
		})
	return s
}

// Run serves MCP over stdio until the client disconnects.
func Run(ctx context.Context, version string) error {
	return New(version).Run(ctx, &mcp.StdioTransport{})
}
