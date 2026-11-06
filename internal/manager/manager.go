// Package manager resolves connections from every source and keeps them open.
// The GUI and the MCP server share it.
package manager

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/ExeQue/dbro/internal/config"
	"github.com/ExeQue/dbro/internal/db"
	"github.com/ExeQue/dbro/internal/secrets"
	"github.com/ExeQue/dbro/internal/sources"
)

// NeedPasswordError asks the caller for a password (Kind "" for the database, "ssh" for SSH).
type NeedPasswordError struct {
	Conn string
	Kind string
	Err  error
}

func (e *NeedPasswordError) Error() string {
	if e.Kind == "ssh" {
		return fmt.Sprintf("%s needs an SSH password or key passphrase", e.Conn)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Conn, e.Err)
	}
	return e.Conn + " needs a password"
}

// Manager owns open connections.
type Manager struct {
	Source string // gui | mcp

	mu    sync.Mutex
	conns map[string]*db.Conn
}

// New creates a manager that logs as source.
func New(source string) *Manager {
	return &Manager{Source: source, conns: map[string]*db.Conn{}}
}

// Connections returns saved connections (Laravel ones resolved) plus lerd's instances.
// Problems with a source are returned as warnings, not failures.
func (m *Manager) Connections() ([]config.Connection, []string) {
	var warn []string
	saved, err := config.LoadConnections()
	if err != nil {
		warn = append(warn, err.Error())
	}
	out := make([]config.Connection, 0, len(saved))
	for _, c := range saved {
		if c.Laravel != "" {
			r, err := sources.ResolveLaravel(c)
			if err != nil {
				warn = append(warn, err.Error())
			}
			r.Name, r.Color, r.SSH, r.AllowAgentWrites, r.HideFromAgents, r.Laravel = c.Name, c.Color, c.SSH, c.AllowAgentWrites, c.HideFromAgents, c.Laravel
			c = r
		}
		if c.Driver == "sqlite" && c.File != "" {
			c.File = config.ExpandHome(c.File)
		}
		out = append(out, c)
	}
	if s, _ := config.LoadSettings(); s.Lerd {
		projects, err := sources.LerdProjects()
		if err != nil {
			warn = append(warn, "lerd sites: "+err.Error())
		}
		out = append(out, projects...)
		lerd, err := sources.LerdConnections()
		if err != nil {
			warn = append(warn, "lerd: "+err.Error())
		}
		out = append(out, lerd...)
		for i := range out {
			if out[i].Source == "lerd" || out[i].Source == "lerd-project" {
				out[i].AllowAgentWrites = slices.Contains(s.AgentWrites, out[i].Name)
			}
		}
	}
	rank := map[string]int{"manual": 0, "laravel": 0, "lerd-project": 1, "lerd": 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Source] < rank[out[j].Source] })
	return out, warn
}

// Find returns one connection by name.
func (m *Manager) Find(name string) (config.Connection, error) {
	conns, _ := m.Connections()
	for _, c := range conns {
		if c.Name == name {
			return c, nil
		}
	}
	return config.Connection{}, fmt.Errorf("no connection named %q", name)
}

// Conn returns an open connection, opening it on first use.
func (m *Manager) Conn(ctx context.Context, name string) (*db.Conn, error) {
	m.mu.Lock()
	c := m.conns[name]
	m.mu.Unlock()
	if c != nil {
		return c, nil
	}
	cfg, err := m.Find(name)
	if err != nil {
		return nil, err
	}
	pass := cfg.Password
	stored := pass != ""
	if !stored && cfg.Driver != "sqlite" {
		if p, err := secrets.Get(secrets.Key(name, "")); err == nil {
			pass, stored = p, true
		}
	}
	sshSecret, _ := secrets.Get(secrets.Key(name, "ssh"))
	c, err = db.Open(ctx, cfg, pass, sshSecret, m.Source)
	if err != nil {
		if isAuthError(err) {
			return nil, &NeedPasswordError{Conn: name, Err: err}
		}
		if cfg.SSH != nil && strings.Contains(err.Error(), "unable to authenticate") {
			return nil, &NeedPasswordError{Conn: name, Kind: "ssh", Err: err}
		}
		return nil, err
	}
	m.mu.Lock()
	if existing := m.conns[name]; existing != nil {
		m.mu.Unlock()
		c.Close()
		return existing, nil
	}
	m.conns[name] = c
	m.mu.Unlock()
	return c, nil
}

func isAuthError(err error) bool {
	s := err.Error()
	for _, k := range []string{"Error 1045", "Access denied", "28P01", "password authentication failed", "no password supplied", "28000"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// Disconnect closes one connection.
func (m *Manager) Disconnect(name string) {
	m.mu.Lock()
	c := m.conns[name]
	delete(m.conns, name)
	m.mu.Unlock()
	if c != nil {
		c.Close()
	}
}

// CloseAll closes every connection.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	cs := m.conns
	m.conns = map[string]*db.Conn{}
	m.mu.Unlock()
	for _, c := range cs {
		c.Close()
	}
}

// IsOpen reports whether a connection is open.
func (m *Manager) IsOpen(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conns[name] != nil
}

// ErrHidden is returned to agents for connections marked hide_from_agents.
var ErrHidden = errors.New("this connection is hidden from agents")
