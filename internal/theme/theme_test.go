package theme

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOmarchySyncAndCustom(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DBRO_CONFIG_DIR", filepath.Join(dir, "cfg"))
	colors := filepath.Join(dir, "omarchy", "theme", "colors.toml")
	os.MkdirAll(filepath.Dir(colors), 0o755)
	os.WriteFile(colors, []byte("mode = \"dark\"\naccent = \"#111111\" # comment\n"), 0o644)
	t.Setenv("DBRO_OMARCHY_COLORS", colors)

	th, err := Load("omarchy")
	if err != nil || th.Colors["accent"] != "#111111" || th.Colors["background"] == "" {
		t.Fatalf("load omarchy: %+v %v", th.Colors, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan string, 4)
	if err := Watch(ctx, "omarchy", func(t Theme) { got <- t.Colors["accent"] }); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	os.WriteFile(colors, []byte("mode = \"dark\"\naccent = \"#222222\"\n"), 0o644)
	select {
	case a := <-got:
		if a != "#222222" {
			t.Fatalf("accent %s", a)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no live update")
	}

	id, err := SaveCustom("My Theme!", map[string]string{"mode": "light", "accent": "#abcdef"})
	if err != nil || id != "my-theme" {
		t.Fatal(id, err)
	}
	ct, err := Load("my-theme")
	if err != nil || ct.Colors["accent"] != "#abcdef" || ct.Mode() != "light" || ct.Colors["background"] != "#fbf7f6" {
		t.Fatalf("custom: %+v %v", ct, err)
	}
	ids := map[string]bool{}
	for _, i := range List() {
		ids[i.ID] = true
	}
	for _, want := range []string{"dbro-dark", "dbro-light", "nord", "my-theme", "omarchy"} {
		if !ids[want] {
			t.Fatalf("missing %s in %v", want, ids)
		}
	}
	t.Setenv("DBRO_COLOR_SCHEME", "light")
	if s, _ := Load("system"); s.ID != "dbro-light" {
		t.Fatalf("system light: %s", s.ID)
	}
}
