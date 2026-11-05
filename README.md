<p align="center"><img src="assets/banner.svg" alt="dbro: your database bro" width="100%"></p>

**dbro** is a simple SQL browser for Linux, in the spirit of HeidiSQL: a tree on the left, Data, Structure and Query tabs on the right, the SQL log at the bottom. It talks to **MySQL, MariaDB, PostgreSQL and SQLite**, follows your **Omarchy** theme, and ships an **MCP server** so agents can read your databases too.

## Features

- **Connections** to servers, SQLite files and Laravel projects (read from `.env` on every connect), with SSL/TLS and SSH tunnels (`~/.ssh/config`, ssh-agent, `ProxyJump`, `known_hosts`). Passwords live in the system keyring, never on disk.
- **lerd** database services show up by themselves, each database labeled with the lerd site that uses it.
- **Data grid** with paging, sorting, a WHERE filter, inline editing with a change queue, NULL handling, a side editor for JSON and long text, foreign-key following, and copy as CSV, TSV, JSON, SQL or Markdown.
- **Schema editor**: create, rename, truncate and drop databases and tables; add, change, move and drop columns, indexes and foreign keys, always with a SQL preview first. SQLite tables are rebuilt where `ALTER TABLE` falls short.
- **Query tabs** with CodeMirror, autocomplete for tables and columns, several result sets, EXPLAIN, a per-connection history and saved snippets.
- **Import and export**: tables and results to CSV, JSON or SQL; full SQL dumps; SQL and CSV import; **Clone to** another database or connection, across drivers.
- **Themes**: dbro Dark and Light (picked from your system preference on first start), Tokyo Night, Catppuccin, Gruvbox, Nord, your own in `~/.config/dbro/themes/*.toml`, or live sync with Omarchy.
- **MCP server** for agents: read-only by default, writes only on connections you allow, connections can be hidden.

## Install

Needs Go, Node, the Wails CLI and `webkit2gtk-4.1` (already on Omarchy).

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@latest
make install        # builds and installs to ~/.local (binary, .desktop file, icon)
```

## MCP for agents

```sh
claude mcp add dbro -- dbro mcp
```

Tools: `list_connections`, `list_databases`, `list_tables`, `describe_table`, `query` (always in a read-only transaction) and `execute` (only where *Allow agents to change data* is on). Agent statements appear in the app's SQL log marked `[mcp]`.

## Files

| Path | What |
| --- | --- |
| `~/.config/dbro/connections.toml` | saved connections (no passwords) |
| `~/.config/dbro/config.toml` | settings |
| `~/.config/dbro/themes/*.toml` | your themes, Omarchy `colors.toml` format |
| `~/.config/dbro/snippets.json` | saved queries |
| `~/.local/state/dbro/` | SQL log and query history |

## Development

```sh
make dev     # wails dev with hot reload
make test    # go test ./... (set DBRO_TEST_LERD=1 to also test against lerd's MySQL and PostgreSQL)
```
