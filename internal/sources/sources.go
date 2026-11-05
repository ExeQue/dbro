// Package sources finds connections that dbro does not store itself:
// lerd's database services and Laravel projects' .env files.
package sources

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"

	"github.com/ExeQue/dbro/internal/config"
)

// LerdService is one database service from lerd's config.yaml.
type LerdService struct {
	Name   string // mysql, postgres, mariadb-11, ...
	Driver string
	Port   int // host port
}

// lerd's documented default credentials (see lerd's quadlets and getting-started docs).
var lerdDefaults = map[string][2]string{
	"mysql":    {"root", "lerd"},
	"mariadb":  {"root", "lerd"},
	"postgres": {"postgres", "lerd"},
}

func lerdConfigPath() string {
	if p := os.Getenv("DBRO_LERD_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "lerd", "config.yaml")
}

// LerdServices reads enabled database services from lerd's config.yaml.
// It returns nil when lerd is not installed.
func LerdServices() ([]LerdService, error) {
	b, err := os.ReadFile(lerdConfigPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Services map[string]struct {
			Enabled       bool `yaml:"enabled"`
			Port          int  `yaml:"port"`
			PublishedPort int  `yaml:"published_port"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse lerd config: %w", err)
	}
	var out []LerdService
	for name, s := range cfg.Services {
		driver := lerdDriver(name)
		if driver == "" || !s.Enabled {
			continue
		}
		port := s.PublishedPort
		if port == 0 {
			port = s.Port
		}
		out = append(out, LerdService{Name: name, Driver: driver, Port: port})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func lerdDriver(service string) string {
	for _, d := range []string{"mysql", "mariadb", "postgres"} {
		if service == d || strings.HasPrefix(service, d+"-") {
			return d
		}
	}
	return ""
}

// LerdConnections turns lerd services into connections.
func LerdConnections() ([]config.Connection, error) {
	svcs, err := LerdServices()
	if err != nil {
		return nil, err
	}
	var out []config.Connection
	for _, s := range svcs {
		cred := lerdDefaults[s.Driver]
		out = append(out, config.Connection{
			Name:     "lerd · " + s.Name,
			Driver:   s.Driver,
			Host:     "127.0.0.1",
			Port:     s.Port,
			User:     cred[0],
			Password: cred[1],
			Color:    "blue",
			Source:   "lerd",
		})
	}
	return out, nil
}

// lerdHost maps a lerd container hostname (lerd-mysql) to the host port lerd publishes.
func lerdHost(host string) (int, string, bool) {
	name, ok := strings.CutPrefix(host, "lerd-")
	if !ok {
		return 0, "", false
	}
	svcs, _ := LerdServices()
	for _, s := range svcs {
		if s.Name == name {
			return s.Port, s.Driver, true
		}
	}
	return 0, "", false
}

// ResolveLaravel fills c's database settings from the Laravel project at c.Laravel.
// It reads .env and, when present, lerd's .env.lerd_override on top.
func ResolveLaravel(c config.Connection) (config.Connection, error) {
	dir := config.ExpandHome(c.Laravel)
	env, err := godotenv.Read(filepath.Join(dir, ".env"))
	if err != nil {
		c.Missing = true
		return c, fmt.Errorf("read %s/.env: %w", dir, err)
	}
	if over, err := godotenv.Read(filepath.Join(dir, ".env.lerd_override")); err == nil {
		for k, v := range over {
			env[k] = v
		}
	}
	return applyEnv(c, dir, env), nil
}

func applyEnv(c config.Connection, dir string, env map[string]string) config.Connection {
	driver := env["DB_CONNECTION"]
	host, port, user, pass, db := env["DB_HOST"], env["DB_PORT"], env["DB_USERNAME"], env["DB_PASSWORD"], env["DB_DATABASE"]
	_, hasPass := env["DB_PASSWORD"]
	if u := env["DB_URL"]; u != "" {
		if pu, err := url.Parse(u); err == nil {
			if driver == "" {
				driver = pu.Scheme
			}
			host, port = pu.Hostname(), pu.Port()
			if pu.User != nil {
				user = pu.User.Username()
				pass, hasPass = pu.User.Password()
			}
			if p := strings.TrimPrefix(pu.Path, "/"); p != "" {
				db = p
			}
		}
	}
	switch driver {
	case "pgsql", "postgresql":
		driver = "postgres"
	case "":
		driver = "mysql"
	}
	c.Driver = driver
	c.Source = "laravel"
	c.Label = filepath.Base(dir)
	if driver == "sqlite" {
		if db == "" {
			db = "database/database.sqlite"
		}
		if !filepath.IsAbs(db) {
			db = filepath.Join(dir, db)
		}
		c.File = db
		if _, err := os.Stat(db); err != nil {
			c.Missing = true
		}
		return c
	}
	c.Host, c.User, c.Database = host, user, db
	c.Port, _ = strconv.Atoi(port)
	if lp, ld, ok := lerdHost(host); ok {
		c.Host, c.Port = "127.0.0.1", lp
		if !hasPass || pass == "" {
			// Secrets come from an env provider (e.g. Infisical via lerd): fall back to lerd's defaults.
			pass = lerdDefaults[ld][1]
			hasPass = true
		}
	}
	if c.Host == "" || c.Host == "localhost" {
		c.Host = "127.0.0.1"
	}
	if hasPass {
		c.Password = pass
	}
	return c
}

func lerdSitesPath() string {
	if p := os.Getenv("DBRO_LERD_SITES"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "lerd", "sites.yaml")
}

// LerdSiteDatabases maps "<lerd service>/<database>" to the lerd site that uses it,
// read from each site's .env. Testing databases (<db>_testing) are labeled too.
func LerdSiteDatabases() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(lerdSitesPath())
	if err != nil {
		return out
	}
	var f struct {
		Sites []struct {
			Name string `yaml:"name"`
			Path string `yaml:"path"`
		} `yaml:"sites"`
	}
	if yaml.Unmarshal(b, &f) != nil {
		return out
	}
	for _, s := range f.Sites {
		env, err := godotenv.Read(filepath.Join(s.Path, ".env"))
		if err != nil {
			continue
		}
		if over, err := godotenv.Read(filepath.Join(s.Path, ".env.lerd_override")); err == nil {
			for k, v := range over {
				env[k] = v
			}
		}
		svc, ok := strings.CutPrefix(env["DB_HOST"], "lerd-")
		dbName := env["DB_DATABASE"]
		if !ok || dbName == "" {
			continue
		}
		if _, taken := out[svc+"/"+dbName]; !taken {
			out[svc+"/"+dbName] = s.Name
		}
		if _, taken := out[svc+"/"+dbName+"_testing"]; !taken {
			out[svc+"/"+dbName+"_testing"] = s.Name + " (testing)"
		}
	}
	return out
}
