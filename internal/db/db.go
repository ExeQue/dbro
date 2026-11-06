// Package db is dbro's database core: connecting (directly or through SSH),
// introspection, queries, row edits, schema changes and export for
// MySQL, MariaDB, PostgreSQL and SQLite.
package db

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/ExeQue/dbro/internal/config"
	"github.com/ExeQue/dbro/internal/sqllog"
	"github.com/ExeQue/dbro/internal/sshtunnel"
)

// Conn is an open connection to one server (or SQLite file), with one pool per database.
type Conn struct {
	Cfg    config.Connection
	Source string // gui | mcp, for the SQL log

	host     string
	port     int
	password string
	tunnel   *sshtunnel.Tunnel

	mu       sync.Mutex
	pools    map[string]*sql.DB
	sessions map[string]*sql.Conn
}

// Kind groups drivers by SQL dialect.
func (c *Conn) Kind() string {
	switch c.Cfg.Driver {
	case "mysql", "mariadb":
		return "mysql"
	case "postgres", "postgresql", "pgsql":
		return "postgres"
	case "sqlite", "sqlite3":
		return "sqlite"
	}
	return c.Cfg.Driver
}

// Open connects and pings the server.
func Open(ctx context.Context, cfg config.Connection, password, sshSecret, source string) (*Conn, error) {
	c := &Conn{Cfg: cfg, Source: source, password: password, host: cfg.Host, port: cfg.Port, pools: map[string]*sql.DB{}, sessions: map[string]*sql.Conn{}}
	switch c.Kind() {
	case "mysql", "postgres", "sqlite":
	default:
		return nil, fmt.Errorf("unknown driver %q", cfg.Driver)
	}
	if c.port == 0 {
		c.port = map[string]int{"mysql": 3306, "postgres": 5432}[c.Kind()]
	}
	if c.host == "" {
		c.host = "127.0.0.1"
	}
	if cfg.SSH != nil && cfg.SSH.Host != "" && c.Kind() != "sqlite" {
		t, err := sshtunnel.Open(*cfg.SSH, c.host, c.port, sshSecret)
		if err != nil {
			return nil, err
		}
		c.tunnel = t
		c.host, c.port = "127.0.0.1", t.Port
	}
	pool, err := c.pool(c.defaultDB())
	if err != nil {
		c.Close()
		return nil, err
	}
	if err := pool.PingContext(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Close closes all pools and the tunnel.
func (c *Conn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.sessions {
		s.Close()
	}
	for _, p := range c.pools {
		p.Close()
	}
	c.pools, c.sessions = map[string]*sql.DB{}, map[string]*sql.Conn{}
	if c.tunnel != nil {
		c.tunnel.Close()
		c.tunnel = nil
	}
}

func (c *Conn) defaultDB() string {
	switch c.Kind() {
	case "sqlite":
		return "main"
	case "postgres":
		if c.Cfg.Database == "" {
			return "postgres"
		}
	}
	return c.Cfg.Database
}

// pool returns the pool for a database, opening it on first use.
func (c *Conn) pool(database string) (*sql.DB, error) {
	if c.Kind() == "sqlite" {
		database = "main"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.pools[database]; ok {
		return p, nil
	}
	var p *sql.DB
	var err error
	switch c.Kind() {
	case "mysql":
		p, err = c.openMySQL(database)
	case "postgres":
		p, err = c.openPostgres(database)
	case "sqlite":
		path := config.ExpandHome(c.Cfg.File)
		if _, statErr := os.Stat(path); statErr != nil {
			return nil, fmt.Errorf("sqlite file: %w", statErr)
		}
		p, err = sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
		if err == nil {
			p.SetMaxOpenConns(4) // query tabs pin a connection each; busy_timeout covers write contention
		}
	}
	if err != nil {
		return nil, err
	}
	p.SetConnMaxIdleTime(5 * time.Minute)
	c.pools[database] = p
	return p, nil
}

func (c *Conn) openMySQL(database string) (*sql.DB, error) {
	mc := mysql.NewConfig()
	mc.User, mc.Passwd, mc.Net, mc.DBName = c.Cfg.User, c.password, "tcp", database
	mc.Addr = net.JoinHostPort(c.host, strconv.Itoa(c.port))
	mc.AllowNativePasswords = true
	mc.Collation = "utf8mb4_unicode_ci"
	mc.Timeout = 10 * time.Second
	switch c.Cfg.SSLMode {
	case "", "disable":
	case "require": // like libpq: encrypt, but verify nothing; verify-ca and verify-full check the certificate
		mc.TLS = &tls.Config{InsecureSkipVerify: true}
	default:
		tc, err := c.tlsConfig()
		if err != nil {
			return nil, err
		}
		mc.TLS = tc
	}
	conn, err := mysql.NewConnector(mc)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(conn), nil
}

func (c *Conn) tlsConfig() (*tls.Config, error) {
	tc := &tls.Config{ServerName: c.Cfg.Host}
	if c.Cfg.SSLCA != "" {
		pem, err := os.ReadFile(config.ExpandHome(c.Cfg.SSLCA))
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(pem)
		tc.RootCAs = pool
	}
	if c.Cfg.SSLCert != "" {
		cert, err := tls.LoadX509KeyPair(config.ExpandHome(c.Cfg.SSLCert), config.ExpandHome(c.Cfg.SSLKey))
		if err != nil {
			return nil, err
		}
		tc.Certificates = []tls.Certificate{cert}
	}
	if c.Cfg.SSLMode == "verify-ca" {
		tc.InsecureSkipVerify = true
		tc.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("no server certificate")
			}
			cert, err := x509.ParseCertificate(raw[0])
			if err != nil {
				return err
			}
			_, err = cert.Verify(x509.VerifyOptions{Roots: tc.RootCAs})
			return err
		}
	}
	return tc, nil
}

func (c *Conn) openPostgres(database string) (*sql.DB, error) {
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.host, strconv.Itoa(c.port)), Path: "/" + database}
	u.User = url.UserPassword(c.Cfg.User, c.password)
	q := url.Values{}
	mode := c.Cfg.SSLMode
	if mode == "" {
		mode = "prefer"
	}
	q.Set("sslmode", mode)
	if c.Cfg.SSLCA != "" {
		q.Set("sslrootcert", config.ExpandHome(c.Cfg.SSLCA))
	}
	if c.Cfg.SSLCert != "" {
		q.Set("sslcert", config.ExpandHome(c.Cfg.SSLCert))
		q.Set("sslkey", config.ExpandHome(c.Cfg.SSLKey))
	}
	q.Set("connect_timeout", "10")
	q.Set("application_name", "dbro")
	u.RawQuery = q.Encode()
	pc, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, err
	}
	if c.tunnel != nil && c.Cfg.Host != "" && pc.TLSConfig != nil {
		pc.TLSConfig.ServerName = c.Cfg.Host
	}
	return stdlib.OpenDB(*pc), nil
}

// session returns a pinned connection for a query tab, so SET, temporary tables
// and transactions survive between runs.
func (c *Conn) session(ctx context.Context, database, id string) (*sql.Conn, error) {
	key := database + "\x00" + id
	c.mu.Lock()
	s := c.sessions[key]
	c.mu.Unlock()
	if s != nil {
		if s.PingContext(ctx) == nil {
			return s, nil
		}
		s.Close()
	}
	p, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	s, err = p.Conn(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.sessions[key] = s
	c.mu.Unlock()
	return s, nil
}

// CloseSession releases a query tab's pinned connection.
func (c *Conn) CloseSession(database, id string) {
	key := database + "\x00" + id
	c.mu.Lock()
	if s := c.sessions[key]; s != nil {
		s.Close()
		delete(c.sessions, key)
	}
	c.mu.Unlock()
}

// Q quotes an identifier for this dialect.
func (c *Conn) Q(name string) string {
	if c.Kind() == "mysql" {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Table returns the qualified name of a table.
func (c *Conn) Table(schema, table string) string {
	if schema == "" || c.Kind() == "sqlite" && schema == "main" {
		return c.Q(table)
	}
	return c.Q(schema) + "." + c.Q(table)
}

// ph returns the n-th (1-based) bind placeholder.
func (c *Conn) ph(n int) string {
	if c.Kind() == "postgres" {
		return "$" + strconv.Itoa(n)
	}
	return "?"
}

func (c *Conn) log(database, query string, start time.Time, rows int64, err error) {
	e := sqllog.Entry{Source: c.Source, Conn: c.Cfg.Name, Database: database, SQL: query, DurationMs: float64(time.Since(start).Microseconds()) / 1000, Rows: rows}
	if err != nil {
		e.Error = err.Error()
	}
	sqllog.Write(e)
}

// querier is satisfied by *sql.DB, *sql.Conn and *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Result is one statement's outcome.
type Result struct {
	SQL        string   `json:"sql"`
	Columns    []string `json:"columns"`
	Types      []string `json:"types"`
	Rows       [][]any  `json:"rows"`
	IsQuery    bool     `json:"isQuery"`
	Affected   int64    `json:"affected"`
	DurationMs float64  `json:"durationMs"`
	Truncated  bool     `json:"truncated"`
	Error      string   `json:"error,omitempty"`
}

// run executes one statement and collects up to limit rows (0 = no limit).
func (c *Conn) run(ctx context.Context, q querier, database, stmt string, limit int, args ...any) (Result, error) {
	start := time.Now()
	shown := c.interpolate(stmt, args)
	res := Result{SQL: shown}
	if !returnsRows(stmt) {
		r, err := q.ExecContext(ctx, stmt, args...)
		res.DurationMs = ms(start)
		if err == nil {
			res.Affected, _ = r.RowsAffected()
		}
		c.log(database, shown, start, res.Affected, err)
		return res, err
	}
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		c.log(database, shown, start, 0, err)
		return res, err
	}
	defer rows.Close()
	res.IsQuery = true
	res.Columns, _ = rows.Columns()
	if cts, err := rows.ColumnTypes(); err == nil {
		for _, ct := range cts {
			res.Types = append(res.Types, strings.ToLower(ct.DatabaseTypeName()))
		}
	}
	res.Rows = [][]any{}
	vals := make([]any, len(res.Columns))
	ptrs := make([]any, len(vals))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if limit > 0 && len(res.Rows) >= limit {
			res.Truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			c.log(database, shown, start, int64(len(res.Rows)), err)
			return res, err
		}
		row := make([]any, len(vals))
		for i, v := range vals {
			row[i] = Value(v)
		}
		res.Rows = append(res.Rows, row)
	}
	err = rows.Err()
	res.DurationMs = ms(start)
	res.Affected = int64(len(res.Rows))
	c.log(database, shown, start, res.Affected, err)
	return res, err
}

func ms(start time.Time) float64 { return float64(time.Since(start).Microseconds()) / 1000 }

// Value converts a scanned driver value to something JSON-friendly:
// nil, bool, int64, float64 or string.
func Value(v any) any {
	switch x := v.(type) {
	case nil, bool, int64, float64, string:
		return x
	case int32:
		return int64(x)
	case int16:
		return int64(x)
	case int:
		return int64(x)
	case float32:
		return float64(x)
	case []byte:
		if utf8.Valid(x) {
			return string(x)
		}
		return "0x" + hex.EncodeToString(x)
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 && x.Location() == time.UTC {
			return x.Format("2006-01-02")
		}
		if x.Nanosecond() != 0 {
			return x.Format("2006-01-02 15:04:05.999999")
		}
		return x.Format("2006-01-02 15:04:05")
	case [16]byte: // uuid
		return fmt.Sprintf("%x-%x-%x-%x-%x", x[0:4], x[4:6], x[6:8], x[8:10], x[10:16])
	case pgtype.Numeric:
		if !x.Valid {
			return nil
		}
		b, _ := x.MarshalJSON()
		return strings.Trim(string(b), `"`)
	case *big.Int:
		return x.String()
	case fmt.Stringer:
		return x.String()
	}
	return fmt.Sprint(v)
}

// Query runs a script statement by statement on the query tab's session.
// It stops at the first error; results so far are returned with it.
func (c *Conn) Query(ctx context.Context, database, sessionID, script string, limit int) ([]Result, error) {
	s, err := c.session(ctx, database, sessionID)
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, stmt := range SplitStatements(script) {
		r, err := c.run(ctx, s, database, stmt, limit)
		if err != nil {
			r.Error = err.Error()
			out = append(out, r)
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// QueryParams is Query with :name and ? placeholders bound as real statement parameters.
// A nil map runs the script as it is.
func (c *Conn) QueryParams(ctx context.Context, database, sessionID, script string, limit int, values map[string]any) ([]Result, error) {
	if values == nil {
		return c.Query(ctx, database, sessionID, script, limit)
	}
	s, err := c.session(ctx, database, sessionID)
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, stmt := range SplitStatements(script) {
		bound, args, err := c.Bind(stmt, values)
		if err != nil {
			return out, err
		}
		if c.Kind() == "postgres" && len(args) > 0 {
			// Untyped parameters ("SELECT :x IS NULL") fail type inference in the extended
			// protocol, and pgx's simple protocol quotes numbers ('1.25' * 2 is then an integer
			// error). Inline typed literals instead: numbers bare, booleans TRUE/FALSE, strings
			// quoted with '' escaping (standard_conforming_strings is on by default since 9.1).
			bound, args = c.interpolate(bound, args), nil
		}
		r, err := c.run(ctx, s, database, bound, limit, args...)
		if err != nil {
			r.Error = err.Error()
			out = append(out, r)
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// QueryReadOnly runs one or more statements inside a read-only transaction
// (query_only for SQLite). Used by the MCP server.
func (c *Conn) QueryReadOnly(ctx context.Context, database, script string, limit int) ([]Result, error) {
	p, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	conn, err := p.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var q querier = conn
	if c.Kind() == "sqlite" {
		if _, err := conn.ExecContext(ctx, "PRAGMA query_only = 1"); err != nil {
			return nil, err
		}
		defer conn.ExecContext(context.Background(), "PRAGMA query_only = 0")
	} else {
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		q = tx
	}
	var out []Result
	for _, stmt := range SplitStatements(script) {
		r, err := c.run(ctx, q, database, stmt, limit)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Exec runs statements outside the read-only guard. Used by the MCP execute tool.
func (c *Conn) Exec(ctx context.Context, database, script string) ([]Result, error) {
	p, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	conn, err := p.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var out []Result
	for _, stmt := range SplitStatements(script) {
		r, err := c.run(ctx, conn, database, stmt, 1000)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Explain shows the plan for a statement.
func (c *Conn) Explain(ctx context.Context, database, stmt string) (Result, error) {
	p, err := c.pool(database)
	if err != nil {
		return Result{}, err
	}
	prefix := "EXPLAIN "
	if c.Kind() == "sqlite" {
		prefix = "EXPLAIN QUERY PLAN "
	}
	stmts := SplitStatements(stmt)
	if len(stmts) == 0 {
		return Result{}, errors.New("nothing to explain")
	}
	return c.run(ctx, p, database, prefix+stmts[0], 0)
}
