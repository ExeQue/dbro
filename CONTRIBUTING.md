# Contributing to dbro

Thanks for helping out. Bug reports, ideas and pull requests are all welcome; for anything bigger than a fix, open an issue first so we can agree on the approach.

## Setup

You need Go (version in `go.mod`), Node 20 or newer, the Wails CLI and, on Linux, the GTK 3 and WebKitGTK 4.1 development packages.

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@latest
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev     # Ubuntu, Debian
sudo pacman -S --needed gtk3 webkit2gtk-4.1             # Arch, Omarchy

make dev       # the app with hot reload
make test      # Go tests
make install   # build and install to ~/.local
```

`dbro serve` runs the same UI in a browser, which is handy for testing with browser dev tools.

## Tests

`make test` runs everything that needs no database server, including SQLite. The MySQL and PostgreSQL tests run when `DBRO_TEST_LERD=1` is set. They expect the servers on Lerd's ports and passwords, which you get either from [Lerd](https://lerd.sh) or from two containers:

```sh
docker run -d --name dbro-mysql -e MYSQL_ROOT_PASSWORD=lerd -p 3307:3306 mysql:8.4
docker run -d --name dbro-pg -e POSTGRES_PASSWORD=lerd -p 5432:5432 postgres:17
DBRO_TEST_LERD=1 make test
```

The tests create and drop their own `dbro_selftest` databases. CI runs the same setup on every push and pull request.

## Layout

- `app.go`: the methods the frontend calls (Wails bindings); `serve.go` exposes the same methods over HTTP.
- `internal/db`: drivers, introspection, the schema editor's SQL planning, import/export.
- `internal/mcpserver`: the MCP tools for agents.
- `internal/sources`: connections found in Lerd and Laravel projects.
- `frontend/src`: the UI in plain TypeScript, with CodeMirror for SQL.

## Guidelines

- **Database behavior works on all four engines** (MySQL, MariaDB, PostgreSQL, SQLite), or says clearly where it does not. Add a test in `internal/db` that runs against each.
- **Schema changes go through the SQL preview.** Plan statements with `PlanSchema` and let the user see them before `ApplySchema` runs them; never run DDL behind the user's back.
- **Agents stay read-only by default.** MCP writes only happen on connections the user explicitly allowed.
- **No secrets on disk.** Passwords belong in the system keyring.
- **UI text is English with American spelling** (color, canceled). Keep things keyboard-friendly and quick to reach.
- **Code style**: `gofmt` and `go vet` for Go; `npx tsc --noEmit` must pass in `frontend`. Match the style of the surrounding code.

## Pull requests

- One change per pull request, with a short description of what and why. Screenshots help for UI changes.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org): `feat(grid): resize columns`, `fix(postgres): quote identifiers in EXPLAIN`.
- CI must be green.

Releases are cut by tagging `vX.Y.Z`; the release workflow builds and publishes everything.

## License

By contributing you agree that your contributions are licensed under the [MIT license](LICENSE).
