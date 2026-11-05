package sources

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLerdProjects(t *testing.T) {
	dir := t.TempDir()
	fw := filepath.Join(dir, "frameworks")
	write(t, filepath.Join(fw, "laravel@11.yaml"), `name: laravel
label: Laravel
env:
  file: .env
  format: dotenv
  services:
    mysql:
      detect: [{key: DB_CONNECTION, value_prefix: mysql}]
      vars: [DB_CONNECTION=mysql, DB_HOST=lerd-mysql, DB_PORT=3306, "DB_DATABASE={{site}}", DB_USERNAME=root, DB_PASSWORD=lerd]
    postgres:
      detect: [{key: DB_CONNECTION, value_prefix: pgsql}]
      vars: [DB_CONNECTION=pgsql, DB_HOST=lerd-postgres, DB_PORT=5432, "DB_DATABASE={{site}}", DB_USERNAME=postgres, DB_PASSWORD=lerd]
  sqlite:
    detect: [{key: DB_CONNECTION, value_prefix: sqlite}]
    vars: [DB_CONNECTION=sqlite, DB_DATABASE=database/database.sqlite]
`)
	write(t, filepath.Join(fw, "laravel@10.yaml"), "name: laravel\nlabel: Old\n")
	write(t, filepath.Join(fw, "symfony@7.yaml"), `name: symfony
label: Symfony
env:
  file: .env.local
  format: dotenv
  services:
    postgres:
      detect: [{key: DATABASE_URL, value_prefix: "postgresql://"}]
      vars: ["DATABASE_URL=postgresql://postgres:lerd@lerd-postgres:5432/{{site}}"]
`)
	write(t, filepath.Join(fw, "wordpress@6.yaml"), `name: wordpress
label: WordPress
env:
  app_file: wp-config.php
  app_format: php-const
  services:
    mysql:
      detect: [{key: DB_HOST}]
      vars: ["DB_NAME={{site}}", DB_USER=root, DB_PASSWORD=lerd, DB_HOST=lerd-mysql]
`)
	write(t, filepath.Join(fw, "drupal@11.yaml"), `name: drupal
label: Drupal
env:
  app_file: web/sites/default/settings.php
  app_format: php-vars
  services:
    postgres:
      detect: [{key: databases.default.default.driver, value_prefix: pgsql}]
      vars: [databases.default.default.driver=pgsql, databases.default.default.host=lerd-postgres, "databases.default.default.database={{site}}", databases.default.default.username=postgres, databases.default.default.password=lerd]
`)
	t.Setenv("DBRO_LERD_FRAMEWORKS", fw)
	lerdCfg := filepath.Join(dir, "lerd.yaml")
	write(t, lerdCfg, "services:\n  mysql:\n    enabled: true\n    port: 3306\n    published_port: 3307\n  postgres:\n    enabled: true\n    port: 5432\n")
	t.Setenv("DBRO_LERD_CONFIG", lerdCfg)

	p := func(n string) string { return filepath.Join(dir, n) }
	write(t, p("shop/.env"), "DB_CONNECTION=mysql\nDB_HOST=lerd-mysql\nDB_DATABASE=shop\nDB_USERNAME=root\n") // password from Infisical
	write(t, p("blog/.env"), "DB_CONNECTION=sqlite\n")
	write(t, p("blog/database/database.sqlite"), "")
	write(t, p("api/.env.local"), "DATABASE_URL=postgresql://postgres:lerd@lerd-postgres:5432/api_db?serverVersion=16\n")
	write(t, p("wp/wp-config.php"), "<?php\ndefine( 'DB_NAME', 'press' );\ndefine('DB_USER', 'root');\ndefine('DB_PASSWORD', 'lerd');\ndefine('DB_HOST', 'lerd-mysql');\n")
	write(t, p("cms/web/sites/default/settings.php"), "<?php\n$databases['default']['default'] = [\n  'database' => 'cms',\n  'username' => 'postgres',\n  'password' => 'lerd',\n  'host' => 'lerd-postgres',\n  'driver' => 'pgsql',\n];\n")
	write(t, p("spa/package.json"), "{}")
	write(t, p("slim/.env"), "APP_NAME=slim\n") // no DB_* at all: Laravel's default sqlite file
	write(t, p("slim/database/database.sqlite"), "")
	sites := "sites:\n"
	for _, s := range [][2]string{{"shop", "laravel"}, {"blog", "laravel"}, {"api", "symfony"}, {"wp", "wordpress"}, {"cms", "drupal"}, {"spa", ""}, {"slim", "laravel"}} {
		sites += "  - name: " + s[0] + "\n    path: " + p(s[0]) + "\n    framework: " + s[1] + "\n"
	}
	write(t, filepath.Join(dir, "sites.yaml"), sites)
	t.Setenv("DBRO_LERD_SITES", filepath.Join(dir, "sites.yaml"))

	conns, err := LerdProjects()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range conns {
		got[c.Name[len(LerdProjectPrefix):]] = c.Driver + " " + c.Host + ":" + strconv.Itoa(c.Port) + " " + c.User + "/" + c.Password + " " + c.Database + c.File + " " + c.Label
	}
	want := map[string]string{
		"shop": "mysql 127.0.0.1:3307 root/lerd shop Laravel",
		"blog": "sqlite :0 / " + p("blog/database/database.sqlite") + " Laravel",
		"api":  "postgres 127.0.0.1:5432 postgres/lerd api_db Symfony",
		"wp":   "mysql 127.0.0.1:3307 root/lerd press WordPress",
		"cms":  "postgres 127.0.0.1:5432 postgres/lerd cms Drupal",
		"slim": "sqlite :0 / " + p("slim/database/database.sqlite") + " Laravel",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s:\n got  %q\n want %q", k, got[k], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("extra projects: %v", got)
	}
}
