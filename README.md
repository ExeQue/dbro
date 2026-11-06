<p align="center"><img src="assets/banner.svg" alt="dbro: your database bro" width="100%"></p>

**dbro** is a fast, keyboard-friendly database client for developers, built for Linux and available for macOS and Windows too. It talks to **MySQL, MariaDB, PostgreSQL and SQLite**, finds your local project databases by itself, follows your **Omarchy** theme, and ships an **MCP server** so your agents can work with the same databases you do.

## Features

- **Connections** to servers, SQLite files and Laravel projects (read from `.env` on every connect), with SSL/TLS and SSH tunnels (`~/.ssh/config`, ssh-agent, `ProxyJump`, `known_hosts`). Passwords live in the system keyring, never on disk.
- **lerd**: every lerd project shows up with its own database, found the way lerd does it, from the project's framework definition (Laravel, Symfony, WordPress, Drupal, Craft and the rest lerd knows, including git worktrees). lerd's MySQL and PostgreSQL services are listed too and connect on startup.
- **Data grid** with paging, sorting, a WHERE filter, inline editing with a change queue, NULL handling, a side editor for JSON and long text, foreign-key following, and copy as CSV, TSV, JSON, SQL, Markdown or a PHP array.
- **Schema editor**: create, rename, truncate and drop databases and tables; add, change, move and drop columns, indexes and foreign keys, always with a SQL preview first. SQLite tables are rebuilt where `ALTER TABLE` falls short.
- **Quick access**: a command palette (Ctrl+K) that jumps to any project, connection, database or table and runs actions, run the statement under the cursor (Alt+Enter), filter from any cell, and generated SELECT/INSERT/UPDATE/DELETE statements.
- **Query tabs** with CodeMirror, named and positional parameters, autocomplete for tables and columns, several result sets, EXPLAIN, a per-connection history and saved snippets.
- **Import and export**: tables and results to CSV, JSON or SQL; full SQL dumps; SQL and CSV import; **Clone to** another database or connection, across drivers.
- **Themes**: dbro Dark and Light (picked from your system preference on first start), Tokyo Night, Catppuccin, Gruvbox, Nord, your own in `~/.config/dbro/themes/*.toml`, or live sync with Omarchy.
- **MCP server** for agents (stdio or streamable HTTP): read-only by default, writes only on connections you allow, connections can be hidden.
- **Headless mode**: `dbro serve` puts the app in a web browser on a port, embeddable in other applications.

## Install

Download a build from [Releases](https://github.com/ExeQue/dbro/releases):

- **Linux** (amd64, arm64): needs `webkit2gtk-4.1` (already on Omarchy). Unpack, then
  ```sh
  install -Dm755 dbro ~/.local/bin/dbro
  install -Dm644 dbro.desktop ~/.local/share/applications/dbro.desktop
  install -Dm644 dbro.png ~/.local/share/icons/hicolor/1024x1024/apps/dbro.png
  ```
- **macOS** (universal): move `dbro.app` to Applications. The build is not notarized, so clear the quarantine flag once: `xattr -dr com.apple.quarantine /Applications/dbro.app`.
- **Windows** (amd64): run `dbro.exe`. It needs the WebView2 runtime (part of Windows 11). The build is not signed, so SmartScreen asks once.

lerd and Omarchy integration only apply on Linux; everything else works on all three.

### From source

Needs Go, Node, the Wails CLI and, on Linux, `webkit2gtk-4.1`.

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@latest
make install        # builds and installs to ~/.local (binary, .desktop file, icon)
```

## Themes

A theme is an Omarchy `colors.toml`: `mode`, `accent`, `background`, `foreground`, `selection`, the ANSI colors and so on. Missing keys come from dbro Dark or dbro Light. The logo follows `accent`; a theme can recolor it with optional keys:

```toml
logo_body = "#a3be8c"   # the cylinder
logo_lid = "#c8dcb4"    # its top
logo_band = "#6f8a5a"   # the disk band and rim
logo_glint = "#ebcb8b"  # the sunglasses' glints
logo_ink = "#20242c"    # sunglasses and smile
```

## MCP for agents

```sh
claude mcp add dbro -- dbro mcp                                   # stdio
dbro mcp --http                                                   # streamable HTTP on 127.0.0.1:7811
claude mcp add --transport http dbro http://127.0.0.1:7811/mcp
```

Tools: `list_connections`, `list_databases`, `list_tables`, `describe_table`, `query` (always in a read-only transaction) and `execute` (only where *Allow agents to change data* is on). Agent statements appear in the app's SQL log marked `[mcp]`.

## Headless mode

`dbro serve` runs the app without a window: open the printed URL in any browser. MCP is served at `/mcp` on the same port.

```sh
dbro serve                                  # http://127.0.0.1:7810/
DBRO_TOKEN=secret dbro serve --addr 0.0.0.0:7810
```

- On loopback without a token, only `localhost`/`127.0.0.1` host names are accepted and the UI cannot be framed by other sites.
- With a token (set `DBRO_TOKEN`; generated automatically beyond loopback), the API, events and MCP need `Authorization: Bearer <token>` or `?token=`. Any application that knows the token can embed the UI: `<iframe src="http://127.0.0.1:7810/?token=secret">`.
- Plain HTTP: put a TLS proxy in front when you listen beyond your own machine.
- File pickers (import, export, dump) use the desktop portal of the machine dbro runs on, so they only work when `dbro serve` runs in a desktop session.

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
