package sources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ExeQue/dbro/internal/config"
)

func TestLaravelEnv(t *testing.T) {
	dir := t.TempDir()
	lerdCfg := filepath.Join(dir, "lerd.yaml")
	os.WriteFile(lerdCfg, []byte("services:\n  mysql:\n    enabled: true\n    port: 3306\n    published_port: 3307\n  postgres:\n    enabled: true\n    port: 5432\n  redis:\n    enabled: true\n    port: 6379\n"), 0o600)
	t.Setenv("DBRO_LERD_CONFIG", lerdCfg)

	svcs, err := LerdServices()
	if err != nil || len(svcs) != 2 || svcs[0].Port != 3307 || svcs[1].Driver != "postgres" {
		t.Fatalf("services: %+v %v", svcs, err)
	}

	c := applyEnv(config.Connection{}, dir, map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "lerd-mysql", "DB_PORT": "3306", "DB_DATABASE": "app", "DB_USERNAME": "root"})
	if c.Host != "127.0.0.1" || c.Port != 3307 || c.Password != "lerd" || c.Database != "app" {
		t.Fatalf("lerd mapping: %+v", c)
	}

	c = applyEnv(config.Connection{}, dir, map[string]string{"DB_CONNECTION": "sqlite"})
	if c.File != filepath.Join(dir, "database/database.sqlite") || !c.Missing {
		t.Fatalf("sqlite: %+v", c)
	}

	c = applyEnv(config.Connection{}, dir, map[string]string{"DB_URL": "pgsql://bob:pw@db.example.com:6543/shop"})
	if c.Driver != "postgres" || c.Host != "db.example.com" || c.Port != 6543 || c.User != "bob" || c.Password != "pw" || c.Database != "shop" {
		t.Fatalf("url: %+v", c)
	}

	c = applyEnv(config.Connection{}, dir, map[string]string{"DB_CONNECTION": "pgsql", "DB_HOST": "remote", "DB_USERNAME": "u"})
	if c.Password != "" || c.Driver != "postgres" {
		t.Fatalf("missing password must stay empty: %+v", c)
	}
}
