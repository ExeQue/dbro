# dbro

A simple SQL browser for Linux, inspired by HeidiSQL. Supports MySQL, MariaDB, PostgreSQL and SQLite, and ships an MCP server for agents.

## Development

```sh
mise install
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails dev -tags webkit2_41
wails build -tags webkit2_41
```
