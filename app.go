package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/ExeQue/dbro/internal/config"
	"github.com/ExeQue/dbro/internal/db"
	"github.com/ExeQue/dbro/internal/history"
	"github.com/ExeQue/dbro/internal/manager"
	"github.com/ExeQue/dbro/internal/portal"
	"github.com/ExeQue/dbro/internal/secrets"
	"github.com/ExeQue/dbro/internal/sources"
	"github.com/ExeQue/dbro/internal/sqllog"
	"github.com/ExeQue/dbro/internal/sshtunnel"
	"github.com/ExeQue/dbro/internal/theme"
)

// App is bound to the frontend; every exported method is callable from TypeScript.
type App struct {
	ctx context.Context
	m   *manager.Manager

	mu           sync.Mutex
	cancels      map[string]context.CancelFunc
	themeCancel  context.CancelFunc
	unknownHosts map[string]*sshtunnel.UnknownHostError
	stop         chan struct{}
}

// NewApp creates the app.
func NewApp() *App {
	return &App{m: manager.New("gui"), cancels: map[string]context.CancelFunc{}, unknownHosts: map[string]*sshtunnel.UnknownHostError{}, stop: make(chan struct{})}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	sqllog.Listen(func(e sqllog.Entry) { runtime.EventsEmit(a.ctx, "log", e) })
	go sqllog.Tail("mcp", time.Second, a.stop, func(e sqllog.Entry) { runtime.EventsEmit(a.ctx, "log", e) })
	s, _ := config.LoadSettings()
	a.watchTheme(s.Theme)
}

func (a *App) shutdown(context.Context) {
	close(a.stop)
	a.m.CloseAll()
}

// encode turns errors the UI must react to into tagged messages.
func (a *App) encode(err error) error {
	if err == nil {
		return nil
	}
	var np *manager.NeedPasswordError
	if errors.As(err, &np) {
		b, _ := json.Marshal(map[string]string{"conn": np.Conn, "kind": np.Kind, "message": np.Error()})
		return errors.New("NEED_PASSWORD:" + string(b))
	}
	var uh *sshtunnel.UnknownHostError
	if errors.As(err, &uh) {
		return errors.New("UNKNOWN_HOST:" + uh.Host + "|" + uh.Fingerprint)
	}
	return err
}

func (a *App) conn(name string) (*db.Conn, error) {
	c, err := a.m.Conn(a.ctx, name)
	var uh *sshtunnel.UnknownHostError
	if errors.As(err, &uh) {
		a.mu.Lock()
		a.unknownHosts[name] = uh
		a.mu.Unlock()
	}
	return c, a.encode(err)
}

// ---- bootstrap, settings and themes ----

// Bootstrap is everything the UI needs on start.
type Bootstrap struct {
	Version    string          `json:"version"`
	Settings   config.Settings `json:"settings"`
	Theme      theme.Theme     `json:"theme"`
	Themes     []theme.Info    `json:"themes"`
	Omarchy    bool            `json:"omarchy"`
	AskOmarchy bool            `json:"askOmarchy"`
	ConfigDir  string          `json:"configDir"`
	Executable string          `json:"executable"`
}

// GetBootstrap loads settings and, on the first run, picks light or dark from the system.
func (a *App) GetBootstrap() (Bootstrap, error) {
	s, err := config.LoadSettings()
	if err != nil {
		return Bootstrap{}, err
	}
	if !s.FirstRunDone {
		s.Theme = "dbro-dark"
		if theme.SystemPrefersLight() {
			s.Theme = "dbro-light"
		}
		s.FirstRunDone = true
		config.SaveSettings(s)
		a.watchTheme(s.Theme)
	}
	t, err := theme.Load(s.Theme)
	if err != nil {
		t, _ = theme.Load("dbro-dark")
	}
	exe, _ := os.Executable()
	omarchy := theme.OmarchyPath() != ""
	return Bootstrap{
		Version: version, Settings: s, Theme: t, Themes: theme.List(),
		Omarchy: omarchy, AskOmarchy: omarchy && !s.OmarchyAsked && s.Theme != "omarchy",
		ConfigDir: config.Dir(), Executable: exe,
	}, nil
}

func (a *App) watchTheme(id string) {
	a.mu.Lock()
	if a.themeCancel != nil {
		a.themeCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.themeCancel = cancel
	a.mu.Unlock()
	theme.Watch(ctx, id, func(t theme.Theme) { runtime.EventsEmit(a.ctx, "theme", t) })
}

// SetTheme switches theme, saves the choice and starts live sync for it.
func (a *App) SetTheme(id string) (theme.Theme, error) {
	t, err := theme.Load(id)
	if err != nil {
		return t, err
	}
	s, _ := config.LoadSettings()
	s.Theme = id
	if err := config.SaveSettings(s); err != nil {
		return t, err
	}
	a.watchTheme(id)
	return t, nil
}

// PreviewTheme loads a theme without saving it.
func (a *App) PreviewTheme(id string) (theme.Theme, error) { return theme.Load(id) }

// AnswerOmarchy records the answer to the first-run sync question.
func (a *App) AnswerOmarchy(sync bool) (theme.Theme, error) {
	s, _ := config.LoadSettings()
	s.OmarchyAsked = true
	config.SaveSettings(s)
	if sync {
		return a.SetTheme("omarchy")
	}
	return theme.Load(s.Theme)
}

// ListThemes returns every theme.
func (a *App) ListThemes() []theme.Info { return theme.List() }

// SaveThemeAs copies the active theme's colors into a custom theme file.
func (a *App) SaveThemeAs(name string, colors map[string]string) (string, error) {
	return theme.SaveCustom(name, colors)
}

// SaveSettings stores settings (theme changes go through SetTheme).
func (a *App) SaveSettings(s config.Settings) error {
	cur, _ := config.LoadSettings()
	s.Theme, s.FirstRunDone, s.OmarchyAsked = cur.Theme, cur.FirstRunDone, cur.OmarchyAsked
	return config.SaveSettings(s)
}

// ---- connections ----

// ConnectionList is the tree's top level.
type ConnectionList struct {
	Connections []config.Connection `json:"connections"`
	Warnings    []string            `json:"warnings"`
	Open        map[string]bool     `json:"open"`
}

// ListConnections returns every connection with its open state.
func (a *App) ListConnections() ConnectionList {
	conns, warn := a.m.Connections()
	open := map[string]bool{}
	for _, c := range conns {
		open[c.Name] = a.m.IsOpen(c.Name)
	}
	if conns == nil {
		conns = []config.Connection{}
	}
	return ConnectionList{Connections: conns, Warnings: warn, Open: open}
}

// GetConnection returns a saved connection as stored (for the editor).
func (a *App) GetConnection(name string) (config.Connection, error) {
	saved, err := config.LoadConnections()
	if err != nil {
		return config.Connection{}, err
	}
	for _, c := range saved {
		if c.Name == name {
			return c, nil
		}
	}
	return config.Connection{}, fmt.Errorf("no saved connection named %q", name)
}

// SaveConnection adds or updates a saved connection. oldName is "" for a new one.
// An empty password keeps the stored one; clearPassword removes it.
func (a *App) SaveConnection(c config.Connection, oldName, password string, clearPassword bool, sshSecret string) error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return errors.New("name is required")
	}
	if strings.HasPrefix(c.Name, "lerd · ") || strings.HasPrefix(c.Name, sources.LerdProjectPrefix) {
		return errors.New("names starting with \"lerd\" and a dot are reserved for lerd")
	}
	saved, err := config.LoadConnections()
	if err != nil {
		return err
	}
	var out []config.Connection
	for _, s := range saved {
		if s.Name == c.Name && s.Name != oldName {
			return fmt.Errorf("a connection named %q already exists", c.Name)
		}
		if s.Name != oldName {
			out = append(out, s)
		}
	}
	if c.SSH != nil && c.SSH.Host == "" {
		c.SSH = nil
	}
	out = append(out, c)
	if err := config.SaveConnections(out); err != nil {
		return err
	}
	if oldName != "" && oldName != c.Name {
		if p, err := secrets.Get(secrets.Key(oldName, "")); err == nil && password == "" {
			password = p
		}
		secrets.Delete(secrets.Key(oldName, ""))
		secrets.Delete(secrets.Key(oldName, "ssh"))
		a.m.Disconnect(oldName)
	}
	switch {
	case clearPassword:
		secrets.Delete(secrets.Key(c.Name, ""))
	case password != "":
		secrets.Set(secrets.Key(c.Name, ""), password, true)
	}
	if sshSecret != "" {
		secrets.Set(secrets.Key(c.Name, "ssh"), sshSecret, true)
	}
	a.m.Disconnect(c.Name)
	return nil
}

// DeleteConnection removes a saved connection and its stored passwords.
func (a *App) DeleteConnection(name string) error {
	saved, err := config.LoadConnections()
	if err != nil {
		return err
	}
	var out []config.Connection
	for _, s := range saved {
		if s.Name != name {
			out = append(out, s)
		}
	}
	a.m.Disconnect(name)
	secrets.Delete(secrets.Key(name, ""))
	secrets.Delete(secrets.Key(name, "ssh"))
	return config.SaveConnections(out)
}

// TestConnection opens an unsaved connection, reports the server version and closes it.
func (a *App) TestConnection(c config.Connection, password, sshSecret string) (string, error) {
	if c.Laravel != "" {
		r, err := resolveLaravel(c.Laravel)
		if err != nil {
			return "", err
		}
		r.SSH, r.Name = c.SSH, c.Name
		if password == "" {
			password = r.Password
		}
		c = r
	}
	if password == "" && c.Name != "" {
		password, _ = secrets.Get(secrets.Key(c.Name, ""))
	}
	if sshSecret == "" && c.Name != "" {
		sshSecret, _ = secrets.Get(secrets.Key(c.Name, "ssh"))
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	conn, err := db.Open(ctx, c, password, sshSecret, "gui")
	if err != nil {
		var uh *sshtunnel.UnknownHostError
		if errors.As(err, &uh) {
			a.mu.Lock()
			a.unknownHosts[c.Name] = uh
			a.mu.Unlock()
		}
		return "", a.encode(err)
	}
	defer conn.Close()
	q := map[string]string{"mysql": "SELECT VERSION()", "postgres": "SHOW server_version", "sqlite": "SELECT sqlite_version()"}[conn.Kind()]
	rs, err := conn.QueryReadOnly(ctx, conn.Defaults(), q, 1)
	if err != nil || len(rs) == 0 || len(rs[0].Rows) == 0 {
		return "connected", nil
	}
	return fmt.Sprint(rs[0].Rows[0][0]), nil
}

// ResolveLaravel previews what a Laravel project's .env resolves to (without the password).
func (a *App) ResolveLaravel(path string) (config.Connection, error) {
	c, err := resolveLaravel(path)
	c.Password = ""
	return c, err
}

func resolveLaravel(path string) (config.Connection, error) {
	return sources.ResolveLaravel(config.Connection{Laravel: path})
}

// Connect opens a connection (the tree calls this when expanding it).
func (a *App) Connect(name string) error {
	_, err := a.conn(name)
	return err
}

// ProvidePassword stores a password (in the keyring when remember) and connects.
func (a *App) ProvidePassword(name, kind, password string, remember bool) error {
	secrets.Set(secrets.Key(name, kind), password, remember)
	a.m.Disconnect(name)
	_, err := a.conn(name)
	return err
}

// TrustHost adds the pending SSH host key for a connection to known_hosts and reconnects.
func (a *App) TrustHost(name string) error {
	a.mu.Lock()
	uh := a.unknownHosts[name]
	a.mu.Unlock()
	if uh == nil {
		return errors.New("no pending host key")
	}
	if err := sshtunnel.Trust(uh); err != nil {
		return err
	}
	return nil
}

// Disconnect closes a connection.
func (a *App) Disconnect(name string) { a.m.Disconnect(name) }

// ---- browsing ----

// Databases lists databases on a connection.
func (a *App) Databases(name string) ([]string, error) {
	c, err := a.conn(name)
	if err != nil {
		return nil, err
	}
	return c.Databases(a.ctx)
}

// DatabaseLabels names the lerd site behind each database of a lerd connection.
func (a *App) DatabaseLabels(name string) map[string]string {
	out := map[string]string{}
	c, err := a.m.Find(name)
	if err != nil || c.Source != "lerd" {
		return out
	}
	svc := strings.TrimPrefix(c.Name, "lerd · ")
	for k, v := range sources.LerdSiteDatabases() {
		if s, db, ok := strings.Cut(k, "/"); ok && s == svc {
			out[db] = v
		}
	}
	return out
}

// TreeDatabase is a database node's children.
type TreeDatabase struct {
	Tables   []db.TableInfo `json:"tables"`
	Routines []db.Routine   `json:"routines"`
}

// DatabaseObjects lists tables, views and routines in a database.
func (a *App) DatabaseObjects(name, database string) (TreeDatabase, error) {
	c, err := a.conn(name)
	if err != nil {
		return TreeDatabase{}, err
	}
	ts, err := c.Tables(a.ctx, database)
	if err != nil {
		return TreeDatabase{}, err
	}
	rs, _ := c.Routines(a.ctx, database)
	if ts == nil {
		ts = []db.TableInfo{}
	}
	if rs == nil {
		rs = []db.Routine{}
	}
	return TreeDatabase{Tables: ts, Routines: rs}, nil
}

// RoutineSource returns a routine's CREATE statement.
func (a *App) RoutineSource(name, database string, r db.Routine) (string, error) {
	c, err := a.conn(name)
	if err != nil {
		return "", err
	}
	return c.RoutineSource(a.ctx, database, r)
}

// Describe returns a table's structure.
func (a *App) Describe(name, database, schema, table string) (db.TableDef, error) {
	c, err := a.conn(name)
	if err != nil {
		return db.TableDef{}, err
	}
	return c.Describe(a.ctx, database, schema, table)
}

// BrowseResult is one page of a table.
type BrowseResult struct {
	Result db.Result `json:"result"`
	Total  int64     `json:"total"`
}

// Browse returns a page of rows.
func (a *App) Browse(name, database, schema, table, where, orderBy string, desc bool, offset, limit int) (BrowseResult, error) {
	c, err := a.conn(name)
	if err != nil {
		return BrowseResult{}, err
	}
	r, total, err := c.Browse(a.ctx, database, schema, table, where, orderBy, desc, offset, limit)
	return BrowseResult{Result: r, Total: total}, err
}

// ApplyRowChanges saves grid edits.
func (a *App) ApplyRowChanges(name, database, schema, table string, changes []db.RowChange) ([]string, error) {
	c, err := a.conn(name)
	if err != nil {
		return nil, err
	}
	return c.ApplyRowChanges(a.ctx, database, schema, table, changes)
}

// ---- queries ----

// QueryRun is a query tab's outcome.
type QueryRun struct {
	Results []db.Result `json:"results"`
	Error   string      `json:"error"`
}

// RunQuery runs a script on the tab's own session. Cancel it with CancelQuery(tabID).
func (a *App) RunQuery(name, database, tabID, script string, limit int) (QueryRun, error) {
	c, err := a.conn(name)
	if err != nil {
		return QueryRun{}, err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.cancels[tabID] = cancel
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		delete(a.cancels, tabID)
		a.mu.Unlock()
	}()
	history.Add(name, database, script)
	rs, err := c.Query(ctx, database, tabID, script, limit)
	run := QueryRun{Results: rs}
	if run.Results == nil {
		run.Results = []db.Result{}
	}
	if err != nil {
		run.Error = err.Error()
		if ctx.Err() != nil {
			run.Error = "query canceled"
			c.CloseSession(database, tabID) // the session may still be busy on the server
		}
	}
	return run, nil
}

// CancelQuery stops a running query.
func (a *App) CancelQuery(tabID string) {
	a.mu.Lock()
	if c := a.cancels[tabID]; c != nil {
		c()
	}
	a.mu.Unlock()
}

// CloseQueryTab releases a query tab's session.
func (a *App) CloseQueryTab(name, database, tabID string) {
	if c, err := a.m.Conn(a.ctx, name); err == nil && a.m.IsOpen(name) {
		c.CloseSession(database, tabID)
	}
}

// Explain returns a statement's plan.
func (a *App) Explain(name, database, sql string) (db.Result, error) {
	c, err := a.conn(name)
	if err != nil {
		return db.Result{}, err
	}
	return c.Explain(a.ctx, database, sql)
}

// History returns a connection's query history.
func (a *App) History(name string) []history.Entry {
	h := history.List(name)
	if h == nil {
		h = []history.Entry{}
	}
	return h
}

// Snippets returns saved snippets.
func (a *App) Snippets() []history.Snippet { return history.Snippets() }

// SaveSnippet stores a snippet.
func (a *App) SaveSnippet(s history.Snippet) error { return history.SaveSnippet(s) }

// DeleteSnippet removes a snippet.
func (a *App) DeleteSnippet(name string) error { return history.DeleteSnippet(name) }

// Completions returns table and column names for autocomplete.
func (a *App) Completions(name, database string) (map[string][]string, error) {
	c, err := a.conn(name)
	if err != nil {
		return nil, err
	}
	return c.Completions(a.ctx, database)
}

// ---- schema ----

// PlanSchema returns the SQL a set of schema changes will run.
func (a *App) PlanSchema(name, database string, ops []db.SchemaOp) ([]string, error) {
	c, err := a.conn(name)
	if err != nil {
		return nil, err
	}
	return c.PlanSchema(a.ctx, database, ops)
}

// ApplySchema runs planned statements.
func (a *App) ApplySchema(name, database string, stmts []string) error {
	c, err := a.conn(name)
	if err != nil {
		return err
	}
	return c.ApplySchema(a.ctx, database, stmts)
}

// ---- files: export, import, dump, clone ----

func (a *App) progress(task, text string) {
	runtime.EventsEmit(a.ctx, "progress", map[string]string{"task": task, "text": text})
}

func filters(format string) []runtime.FileFilter {
	return []runtime.FileFilter{{DisplayName: strings.ToUpper(format), Pattern: "*." + format}}
}

// ExportTable asks for a file and writes a table to it. Returns "" when canceled.
func (a *App) ExportTable(name, database, schema, table, where, format string) (string, error) {
	c, err := a.conn(name)
	if err != nil {
		return "", err
	}
	path, err := a.saveDialog(runtime.SaveDialogOptions{Title: "Export " + table, DefaultFilename: table + "." + format, Filters: filters(format)})
	if err != nil || path == "" {
		return "", err
	}
	n, err := c.ExportTable(a.ctx, database, schema, table, where, format, path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Exported %d rows to %s", n, path), nil
}

// ExportRows asks for a file and writes a query result to it.
func (a *App) ExportRows(name string, columns []string, rows [][]any, format, table string) (string, error) {
	c, err := a.conn(name)
	if err != nil {
		return "", err
	}
	path, err := a.saveDialog(runtime.SaveDialogOptions{Title: "Export result", DefaultFilename: "result." + format, Filters: filters(format)})
	if err != nil || path == "" {
		return "", err
	}
	if err := c.ExportRows(columns, rows, format, table, path); err != nil {
		return "", err
	}
	return fmt.Sprintf("Exported %d rows to %s", len(rows), path), nil
}

// DumpDatabase asks for a file and writes a SQL dump.
func (a *App) DumpDatabase(name, database string, withData bool) (string, error) {
	c, err := a.conn(name)
	if err != nil {
		return "", err
	}
	path, err := a.saveDialog(runtime.SaveDialogOptions{Title: "Dump " + database, DefaultFilename: database + ".sql", Filters: filters("sql")})
	if err != nil || path == "" {
		return "", err
	}
	err = c.Dump(a.ctx, database, path, withData, func(t string) { a.progress("dump", "Dumping "+t) })
	a.progress("dump", "")
	if err != nil {
		return "", err
	}
	return "Dumped " + database + " to " + path, nil
}

// ImportSQL asks for a SQL file and runs it.
func (a *App) ImportSQL(name, database string, continueOnError bool) (string, error) {
	c, err := a.conn(name)
	if err != nil {
		return "", err
	}
	path, err := a.openDialog(false, runtime.OpenDialogOptions{Title: "Import SQL file", Filters: filters("sql")})
	if err != nil || path == "" {
		return "", err
	}
	done, failed, err := c.ImportSQL(a.ctx, database, path, continueOnError, func(d, t int) { a.progress("import", fmt.Sprintf("Statement %d of %d", d, t)) })
	a.progress("import", "")
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("Ran %d statements from %s", done, filepath.Base(path))
	if len(failed) > 0 {
		msg += fmt.Sprintf("; %d failed: %s", len(failed), strings.Join(failed[:min(len(failed), 5)], "; "))
	}
	return msg, nil
}

// CSVFile is a picked CSV file with a preview.
type CSVFile struct {
	Path    string     `json:"path"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// PickCSV asks for a CSV file and previews it.
func (a *App) PickCSV(delimiter string, header bool) (CSVFile, error) {
	path, err := a.openDialog(false, runtime.OpenDialogOptions{Title: "Import CSV file", Filters: []runtime.FileFilter{{DisplayName: "CSV", Pattern: "*.csv;*.tsv;*.txt"}}})
	if err != nil || path == "" {
		return CSVFile{}, err
	}
	return a.PreviewCSV(path, delimiter, header)
}

// PreviewCSV re-reads a CSV file with other options.
func (a *App) PreviewCSV(path, delimiter string, header bool) (CSVFile, error) {
	cols, rows, err := db.CSVPreview(path, delim(delimiter), header)
	return CSVFile{Path: path, Columns: cols, Rows: rows}, err
}

func delim(s string) rune {
	if s == "\\t" || s == "tab" {
		return '\t'
	}
	if s == "" {
		return ','
	}
	return []rune(s)[0]
}

// ImportCSV loads a CSV file into a table.
func (a *App) ImportCSV(name, database, schema, table, path, delimiter string, header bool, mapping []string, nullText string) (string, error) {
	c, err := a.conn(name)
	if err != nil {
		return "", err
	}
	n, err := c.ImportCSV(a.ctx, database, schema, table, path, delim(delimiter), header, mapping, nullText, func(r int64) { a.progress("import", fmt.Sprintf("%d rows", r)) })
	a.progress("import", "")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Imported %d rows into %s", n, table), nil
}

// Clone copies tables to another database or connection.
func (a *App) Clone(srcName, srcDB string, tables []db.TableInfo, dstName, dstDB, dstSchema string, opt db.CloneOptions) (string, error) {
	src, err := a.conn(srcName)
	if err != nil {
		return "", err
	}
	dst, err := a.conn(dstName)
	if err != nil {
		return "", err
	}
	err = db.Clone(a.ctx, src, srcDB, tables, dst, dstDB, dstSchema, opt, func(t string, n int64) { a.progress("clone", fmt.Sprintf("%s: %d rows", t, n)) })
	a.progress("clone", "")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Cloned %d tables to %s / %s", len(tables), dstName, dstDB), nil
}

// ---- pickers and clipboard ----

// PickFile asks for an existing file. kind is sqlite | any.
func (a *App) PickFile(title, kind string) (string, error) {
	opts := runtime.OpenDialogOptions{Title: title}
	if kind == "sqlite" {
		opts.Filters = []runtime.FileFilter{{DisplayName: "SQLite", Pattern: "*.sqlite;*.sqlite3;*.db;*.db3"}, {DisplayName: "All files", Pattern: "*"}}
	}
	return a.openDialog(false, opts)
}

// PickNewFile asks for a new file path (to create a SQLite database).
func (a *App) PickNewFile(title string) (string, error) {
	p, err := a.saveDialog(runtime.SaveDialogOptions{Title: title, DefaultFilename: "database.sqlite"})
	if err != nil || p == "" {
		return p, err
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			return "", err
		}
	}
	return p, nil
}

// PickDirectory asks for a directory (a Laravel project).
func (a *App) PickDirectory(title string) (string, error) {
	return a.openDialog(true, runtime.OpenDialogOptions{Title: title})
}

// CopyText puts text on the clipboard.
func (a *App) CopyText(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

func portalFilters(fs []runtime.FileFilter) []portal.Filter {
	out := make([]portal.Filter, 0, len(fs))
	for _, f := range fs {
		out = append(out, portal.Filter{Name: f.DisplayName, Patterns: strings.Split(f.Pattern, ";")})
	}
	return out
}

// openDialog uses the desktop's file chooser portal, falling back to Wails' GTK dialog.
func (a *App) openDialog(dir bool, o runtime.OpenDialogOptions) (string, error) {
	p, err := portal.OpenFile(o.Title, portalFilters(o.Filters), dir)
	if !errors.Is(err, portal.ErrUnavailable) {
		return p, err
	}
	if dir {
		return runtime.OpenDirectoryDialog(a.ctx, o)
	}
	return runtime.OpenFileDialog(a.ctx, o)
}

// saveDialog uses the desktop's file chooser portal, falling back to Wails' GTK dialog.
func (a *App) saveDialog(o runtime.SaveDialogOptions) (string, error) {
	p, err := portal.SaveFile(o.Title, o.DefaultFilename, portalFilters(o.Filters))
	if !errors.Is(err, portal.ErrUnavailable) {
		return p, err
	}
	return runtime.SaveFileDialog(a.ctx, o)
}
