// Package config loads and saves dbro's settings and saved connections.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// SSH describes an optional SSH tunnel in front of a connection.
type SSH struct {
	Host string `toml:"host" json:"host"`
	Port int    `toml:"port,omitempty" json:"port"`
	User string `toml:"user,omitempty" json:"user"`
	Key  string `toml:"key,omitempty" json:"key"`
}

// Connection is one saved database connection.
type Connection struct {
	Name     string `toml:"name" json:"name"`
	Driver   string `toml:"driver" json:"driver"` // mysql | mariadb | postgres | sqlite
	Host     string `toml:"host,omitempty" json:"host"`
	Port     int    `toml:"port,omitempty" json:"port"`
	User     string `toml:"user,omitempty" json:"user"`
	Database string `toml:"database,omitempty" json:"database"`
	File     string `toml:"file,omitempty" json:"file"`       // sqlite
	Laravel  string `toml:"laravel,omitempty" json:"laravel"` // read DB_* from this project's .env
	Color    string `toml:"color,omitempty" json:"color"`

	SSLMode string `toml:"ssl_mode,omitempty" json:"sslMode"` // disable | require | verify-ca | verify-full
	SSLCA   string `toml:"ssl_ca,omitempty" json:"sslCA"`
	SSLCert string `toml:"ssl_cert,omitempty" json:"sslCert"`
	SSLKey  string `toml:"ssl_key,omitempty" json:"sslKey"`

	SSH *SSH `toml:"ssh,omitempty" json:"ssh"`

	AllowAgentWrites bool `toml:"allow_agent_writes,omitempty" json:"allowAgentWrites"`
	HideFromAgents   bool `toml:"hide_from_agents,omitempty" json:"hideFromAgents"`

	// Set at runtime, never saved.
	Source   string `toml:"-" json:"source"`  // manual | lerd | laravel
	Label    string `toml:"-" json:"label"`   // extra text shown in the tree
	Password string `toml:"-" json:"-"`       // known password (lerd defaults, .env)
	Missing  bool   `toml:"-" json:"missing"` // laravel project or sqlite file not found
}

// Settings holds app-wide preferences.
type Settings struct {
	Theme        string `toml:"theme" json:"theme"` // dbro-dark | dbro-light | system | omarchy | <custom name>
	Font         string `toml:"font,omitempty" json:"font"`
	OmarchyAsked bool   `toml:"omarchy_asked,omitempty" json:"omarchyAsked"`
	Lerd         bool   `toml:"lerd" json:"lerd"`                         // show lerd instances in the tree
	LerdConnect  bool   `toml:"lerd_auto_connect" json:"lerdAutoConnect"` // connect to them on startup
	FirstRunDone bool   `toml:"first_run_done,omitempty" json:"firstRunDone"`
	QueryTimeout int    `toml:"query_timeout,omitempty" json:"queryTimeout"` // seconds
	CheckUpdates bool   `toml:"check_updates" json:"checkUpdates"`           // look for a new release on startup
}

// Dir returns the config directory, honoring DBRO_CONFIG_DIR and XDG_CONFIG_HOME.
func Dir() string {
	if d := os.Getenv("DBRO_CONFIG_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "dbro")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "dbro")
}

// StateDir returns the directory for logs and history.
func StateDir() string {
	if d := os.Getenv("DBRO_STATE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "dbro")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "dbro")
}

// ExpandHome turns a leading ~ into the home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

type connectionsFile struct {
	Connection []Connection `toml:"connection"`
}

// LoadConnections reads connections.toml. A missing file is an empty list.
func LoadConnections() ([]Connection, error) {
	var f connectionsFile
	_, err := toml.DecodeFile(filepath.Join(Dir(), "connections.toml"), &f)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read connections.toml: %w", err)
	}
	for i := range f.Connection {
		f.Connection[i].Source = "manual"
	}
	return f.Connection, nil
}

// SaveConnections writes connections.toml, sorted by name.
func SaveConnections(conns []Connection) error {
	sort.Slice(conns, func(i, j int) bool { return strings.ToLower(conns[i].Name) < strings.ToLower(conns[j].Name) })
	return writeTOML("connections.toml", connectionsFile{Connection: conns})
}

// LoadSettings reads config.toml with defaults for missing values.
func LoadSettings() (Settings, error) {
	s := Settings{Lerd: true, LerdConnect: true, QueryTimeout: 30, CheckUpdates: true}
	_, err := toml.DecodeFile(filepath.Join(Dir(), "config.toml"), &s)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return s, fmt.Errorf("read config.toml: %w", err)
	}
	if s.QueryTimeout <= 0 {
		s.QueryTimeout = 30
	}
	return s, nil
}

// SaveSettings writes config.toml.
func SaveSettings(s Settings) error {
	return writeTOML("config.toml", s)
}

func writeTOML(name string, v any) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(Dir(), name+".*")
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(tmp).Encode(v); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(Dir(), name))
}
