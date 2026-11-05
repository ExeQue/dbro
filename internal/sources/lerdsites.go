package sources

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"

	"github.com/ExeQue/dbro/internal/config"
)

// LerdProjectPrefix starts the name of every connection found from a lerd site.
const LerdProjectPrefix = "lerd project · "

// framework is the part of a lerd framework definition dbro reads.
type framework struct {
	Name    string `yaml:"name"`
	Label   string `yaml:"label"`
	Version string `yaml:"version"`
	Env     struct {
		File           string                `yaml:"file"`
		Format         string                `yaml:"format"`
		FallbackFile   string                `yaml:"fallback_file"`
		FallbackFormat string                `yaml:"fallback_format"`
		AppFile        string                `yaml:"app_file"`
		AppFormat      string                `yaml:"app_format"`
		DefaultService string                `yaml:"default_service"`
		Services       map[string]serviceDef `yaml:"services"`
		SQLite         *serviceDef           `yaml:"sqlite"`
	} `yaml:"env"`
}

type serviceDef struct {
	Detect []struct {
		Key         string `yaml:"key"`
		ValuePrefix string `yaml:"value_prefix"`
	} `yaml:"detect"`
	Vars []string `yaml:"vars"`
}

func lerdFrameworkDirs() []string {
	if d := os.Getenv("DBRO_LERD_FRAMEWORKS"); d != "" {
		return filepath.SplitList(d)
	}
	home, _ := os.UserHomeDir()
	// user definitions win over lerd's bundled ones
	return []string{filepath.Join(home, ".config", "lerd", "frameworks"), filepath.Join(home, ".local", "share", "lerd", "frameworks")}
}

// loadFramework returns the newest definition of a framework.
func loadFramework(name string) *framework {
	for _, dir := range lerdFrameworkDirs() {
		files, _ := filepath.Glob(filepath.Join(dir, name+"@*.yaml"))
		if single := filepath.Join(dir, name+".yaml"); fileExists(single) {
			files = append(files, single)
		}
		if len(files) == 0 {
			continue
		}
		sort.Slice(files, func(i, j int) bool { return versionOf(files[i]) > versionOf(files[j]) })
		b, err := os.ReadFile(files[0])
		if err != nil {
			continue
		}
		var f framework
		if yaml.Unmarshal(b, &f) == nil {
			return &f
		}
	}
	return nil
}

func versionOf(path string) float64 {
	base := strings.TrimSuffix(filepath.Base(path), ".yaml")
	_, v, _ := strings.Cut(base, "@")
	n, _ := strconv.ParseFloat(v, 64)
	return n
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

var (
	phpConst = regexp.MustCompile(`define\(\s*['"]([A-Za-z0-9_]+)['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)
	phpPair  = regexp.MustCompile(`['"]([A-Za-z0-9_]+)['"]\s*=>\s*['"]([^'"]*)['"]`)
	phpArray = regexp.MustCompile(`\$([A-Za-z_]+)((?:\[['"][A-Za-z0-9_]+['"]\])+)\s*=\s*(?:array\(|\[)`)
)

// readEnv parses one framework config file into flat keys. php-vars arrays become dotted keys
// ($databases['default']['default'] = ['driver' => 'mysql'] gives databases.default.default.driver).
func readEnv(path, format string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	switch format {
	case "dotenv":
		env, err := godotenv.Unmarshal(string(b))
		if err != nil {
			return nil
		}
		if over, err := godotenv.Read(filepath.Join(filepath.Dir(path), ".env.lerd_override")); err == nil {
			for k, v := range over {
				env[k] = v
			}
		}
		return env
	case "php-const":
		for _, m := range phpConst.FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = m[2]
		}
	case "php-vars", "php-array":
		src := string(b)
		for _, loc := range phpArray.FindAllStringSubmatchIndex(src, -1) {
			prefix := src[loc[2]:loc[3]] + strings.NewReplacer("['", ".", "\"]", "", "[\"", ".", "']", "").Replace(src[loc[4]:loc[5]])
			end := strings.Index(src[loc[1]:], ";")
			if end < 0 {
				continue
			}
			for _, m := range phpPair.FindAllStringSubmatch(src[loc[1]:loc[1]+end], -1) {
				out[prefix+"."+m[1]] = m[2]
			}
		}
	default:
		return nil
	}
	return out
}

// siteEnv merges the framework's config files for a project directory.
func siteEnv(f *framework, dir string) map[string]string {
	env := map[string]string{}
	for _, src := range [][2]string{{f.Env.AppFile, f.Env.AppFormat}, {f.Env.FallbackFile, f.Env.FallbackFormat}, {f.Env.File, f.Env.Format}} {
		if src[0] == "" || src[1] == "" || src[1] == "none" {
			continue
		}
		for k, v := range readEnv(filepath.Join(dir, src[0]), src[1]) {
			env[k] = v // later sources (the primary env file) win
		}
	}
	return env
}

func detects(s serviceDef, env map[string]string) bool {
	for _, d := range s.Detect {
		if v, ok := env[d.Key]; ok && strings.HasPrefix(v, d.ValuePrefix) {
			return true
		}
	}
	return false
}

// projectConnection resolves a project's database from its framework definition.
// ok is false when the project has no database lerd knows how to read.
func projectConnection(f *framework, dir string) (config.Connection, bool) {
	env := siteEnv(f, dir)
	if f.Name == "laravel" && env["DB_CONNECTION"] == "" && fileExists(filepath.Join(dir, "database", "database.sqlite")) {
		env["DB_CONNECTION"] = "sqlite" // Laravel 11+ falls back to database/database.sqlite
	}
	if len(env) == 0 {
		return config.Connection{}, false
	}
	services := map[string]serviceDef{}
	for k, v := range f.Env.Services {
		services[k] = v
	}
	if f.Env.SQLite != nil {
		services["sqlite"] = *f.Env.SQLite
	}
	pick := ""
	for _, name := range []string{"mysql", "mariadb", "postgres", "sqlite"} {
		if s, ok := services[name]; ok && detects(s, env) {
			pick = name
			break
		}
	}
	if pick == "" {
		// Laravel-style DB_* settings the definition does not list (e.g. sqlite)
		if _, ok := env["DB_CONNECTION"]; ok {
			c := applyEnv(config.Connection{}, dir, env)
			return c, c.Database != "" || (c.File != "" && !c.Missing)
		}
		return config.Connection{}, false
	}
	svc := services[pick]
	c := config.Connection{Driver: map[string]string{"mysql": "mysql", "mariadb": "mariadb", "postgres": "postgres", "sqlite": "sqlite"}[pick]}
	defaultUser := lerdDefaults[c.Driver][0]
	for _, v := range svc.Vars {
		key, tmpl, _ := strings.Cut(v, "=")
		val, set := env[key]
		if !set && c.Driver == "sqlite" && strings.Contains(tmpl, ".sqlite") {
			val, set = tmpl, true // the definition's default database file
		}
		if !set {
			continue
		}
		switch {
		case strings.Contains(tmpl, "://"):
			if u, err := url.Parse(val); err == nil {
				c.Host, c.User = u.Hostname(), u.User.Username()
				c.Port, _ = strconv.Atoi(u.Port())
				c.Password, _ = u.User.Password()
				c.Database = strings.TrimPrefix(u.Path, "/")
			}
		case c.Driver == "sqlite" && (strings.Contains(tmpl, ".sqlite") || strings.Contains(tmpl, "{{site}}")):
			c.File = val
		case strings.Contains(tmpl, "{{site}}"):
			c.Database = val
		case strings.HasPrefix(tmpl, "lerd-"):
			c.Host = val
		case tmpl == "3306" || tmpl == "5432":
			c.Port, _ = strconv.Atoi(val)
		case tmpl == defaultUser && defaultUser != "":
			c.User = val
		case tmpl == "lerd":
			c.Password = val
		}
	}
	if c.Driver == "sqlite" {
		if c.File == "" {
			return config.Connection{}, false
		}
		if !filepath.IsAbs(c.File) {
			c.File = filepath.Join(dir, c.File)
		}
		c.Missing = !fileExists(c.File)
		return c, true
	}
	if c.Database == "" {
		return config.Connection{}, false
	}
	if lp, ld, ok := lerdHost(c.Host); ok {
		c.Host, c.Port = "127.0.0.1", lp
		if c.User == "" {
			c.User = lerdDefaults[ld][0]
		}
		if c.Password == "" {
			// secrets from an env provider (e.g. Infisical) are not in the file
			c.Password = lerdDefaults[ld][1]
		}
	}
	if c.Host == "" || c.Host == "localhost" {
		c.Host = "127.0.0.1"
	}
	return c, true
}

// worktrees lists a project's other git worktrees as (path, branch label).
func worktrees(dir string) [][2]string {
	out, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil
	}
	var list [][2]string
	var path string
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			if filepath.Clean(path) != filepath.Clean(dir) {
				list = append(list, [2]string{path, strings.TrimPrefix(line, "branch refs/heads/")})
			}
		}
	}
	return list
}

// LerdProjects returns one connection per lerd site (and git worktree) whose framework
// definition says where its database settings live.
func LerdProjects() ([]config.Connection, error) {
	b, err := os.ReadFile(lerdSitesPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f struct {
		Sites []struct {
			Name      string `yaml:"name"`
			Path      string `yaml:"path"`
			Framework string `yaml:"framework"`
		} `yaml:"sites"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	var out []config.Connection
	for _, s := range f.Sites {
		if s.Framework == "" {
			continue
		}
		fw := loadFramework(s.Framework)
		if fw == nil {
			continue
		}
		label := fw.Label
		if label == "" {
			label = s.Framework
		}
		add := func(dir, name string) {
			c, ok := projectConnection(fw, dir)
			if !ok {
				return
			}
			c.Name = LerdProjectPrefix + name
			c.Source = "lerd-project"
			c.Label = label
			c.Color = "blue"
			out = append(out, c)
		}
		add(s.Path, s.Name)
		for _, wt := range worktrees(s.Path) {
			add(wt[0], s.Name+" ↳ "+wt[1])
		}
	}
	return out, nil
}
