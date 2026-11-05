// Package theme loads dbro's colour themes: built-in ones, the user's own
// (~/.config/dbro/themes/*.toml) and Omarchy's current theme, all in Omarchy's colors.toml format.
package theme

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/fsnotify/fsnotify"
	"github.com/godbus/dbus/v5"

	"github.com/ExeQue/dbro/internal/config"
)

//go:embed builtin/*.toml
var builtin embed.FS

// Theme maps Omarchy colour keys (accent, background, ...) to values, plus "mode".
type Theme struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Source string            `json:"source"` // builtin | custom | omarchy
	Colors map[string]string `json:"colors"`
}

// Mode is "light" or "dark".
func (t Theme) Mode() string {
	if t.Colors["mode"] == "light" {
		return "light"
	}
	return "dark"
}

// Info describes a theme in the picker.
type Info struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
	Mode   string `json:"mode"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)

func parse(b []byte) (map[string]string, error) {
	var raw map[string]any
	if err := toml.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if k == "mode" || hexColor.MatchString(s) {
			out[k] = s
		}
	}
	return out, nil
}

func builtinTheme(id string) (Theme, error) {
	b, err := builtin.ReadFile("builtin/" + id + ".toml")
	if err != nil {
		return Theme{}, fmt.Errorf("unknown theme %q", id)
	}
	cols, err := parse(b)
	if err != nil {
		return Theme{}, err
	}
	return Theme{ID: id, Name: title(id), Source: "builtin", Colors: cols}, nil
}

func title(id string) string {
	if strings.HasPrefix(id, "dbro-") {
		return "dbro " + strings.ToUpper(id[5:6]) + id[6:]
	}
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// CustomDir holds the user's themes.
func CustomDir() string { return filepath.Join(config.Dir(), "themes") }

// OmarchyPath returns Omarchy's current colors.toml, or "" when Omarchy is not installed.
func OmarchyPath() string {
	if p := os.Getenv("DBRO_OMARCHY_COLORS"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".local", "state", "omarchy", "current", "theme", "colors.toml"),
		filepath.Join(home, ".config", "omarchy", "current", "theme", "colors.toml"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func omarchyName() string {
	p := OmarchyPath()
	if p == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil && filepath.Base(real) != "theme" {
		return title(filepath.Base(real))
	}
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(p)), "theme.name")); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

// List returns every available theme.
func List() []Info {
	var out []Info
	entries, _ := builtin.ReadDir("builtin")
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".toml")
		if t, err := builtinTheme(id); err == nil {
			out = append(out, Info{ID: id, Name: t.Name, Source: "builtin", Mode: t.Mode()})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		// dbro defaults first
		return strings.HasPrefix(out[i].ID, "dbro-") && !strings.HasPrefix(out[j].ID, "dbro-")
	})
	customs, _ := filepath.Glob(filepath.Join(CustomDir(), "*.toml"))
	for _, p := range customs {
		id := strings.TrimSuffix(filepath.Base(p), ".toml")
		if t, err := Load(id); err == nil {
			out = append(out, Info{ID: id, Name: t.Name, Source: "custom", Mode: t.Mode()})
		}
	}
	if OmarchyPath() != "" {
		name := "Omarchy (sync)"
		if n := omarchyName(); n != "" {
			name = "Omarchy (sync): " + n
		}
		out = append(out, Info{ID: "omarchy", Name: name, Source: "omarchy"})
	}
	return out
}

// Load resolves a theme id: "system", "omarchy", a custom file name or a built-in id.
// Missing keys are filled from dbro's default theme of the same mode.
func Load(id string) (Theme, error) {
	var t Theme
	switch id {
	case "", "system":
		if SystemPrefersLight() {
			return builtinTheme("dbro-light")
		}
		return builtinTheme("dbro-dark")
	case "omarchy":
		p := OmarchyPath()
		if p == "" {
			return Theme{}, errors.New("Omarchy theme not found")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return Theme{}, err
		}
		cols, err := parse(b)
		if err != nil {
			return Theme{}, fmt.Errorf("omarchy colors.toml: %w", err)
		}
		t = Theme{ID: "omarchy", Name: "Omarchy", Source: "omarchy", Colors: cols}
		if n := omarchyName(); n != "" {
			t.Name = "Omarchy: " + n
		}
	default:
		p := filepath.Join(CustomDir(), filepath.Base(id)+".toml")
		if b, err := os.ReadFile(p); err == nil {
			cols, err := parse(b)
			if err != nil {
				return Theme{}, fmt.Errorf("%s: %w", p, err)
			}
			t = Theme{ID: id, Name: title(id), Source: "custom", Colors: cols}
		} else {
			return builtinTheme(id)
		}
	}
	base, _ := builtinTheme("dbro-" + t.Mode())
	for k, v := range base.Colors {
		if _, ok := t.Colors[k]; !ok {
			t.Colors[k] = v
		}
	}
	return t, nil
}

// SaveCustom writes a theme to ~/.config/dbro/themes/<name>.toml.
func SaveCustom(name string, colors map[string]string) (string, error) {
	id := strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(name), "-"), "-")
	if id == "" {
		return "", errors.New("theme name is empty")
	}
	if err := os.MkdirAll(CustomDir(), 0o755); err != nil {
		return "", err
	}
	keys := make([]string, 0, len(colors))
	for k := range colors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s, saved by dbro. Same keys as Omarchy's colors.toml.\n", name)
	if m, ok := colors["mode"]; ok {
		fmt.Fprintf(&b, "mode = %q\n", m)
	}
	for _, k := range keys {
		if k != "mode" {
			fmt.Fprintf(&b, "%s = %q\n", k, colors[k])
		}
	}
	return id, os.WriteFile(filepath.Join(CustomDir(), id+".toml"), []byte(b.String()), 0o644)
}

// SystemPrefersLight asks the XDG desktop portal for the colour-scheme preference
// (0 none, 1 dark, 2 light).
func SystemPrefersLight() bool {
	if v := os.Getenv("DBRO_COLOR_SCHEME"); v != "" {
		return v == "light"
	}
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	var v dbus.Variant
	err = obj.Call("org.freedesktop.portal.Settings.ReadOne", 0, "org.freedesktop.appearance", "color-scheme").Store(&v)
	if err != nil {
		err = obj.Call("org.freedesktop.portal.Settings.Read", 0, "org.freedesktop.appearance", "color-scheme").Store(&v)
		if inner, ok := v.Value().(dbus.Variant); ok {
			v = inner
		}
	}
	if err != nil {
		return false
	}
	n, _ := v.Value().(uint32)
	return n == 2
}

// Watch calls onChange whenever the theme behind id changes on disk
// (or the system colour scheme changes, for "system"), until ctx ends.
func Watch(ctx context.Context, id string, onChange func(Theme)) error {
	var dirs []string
	switch id {
	case "omarchy":
		p := OmarchyPath()
		if p == "" {
			return nil
		}
		// Omarchy swaps a symlink when the theme changes, so watch the parents too.
		dirs = []string{filepath.Dir(p), filepath.Dir(filepath.Dir(p))}
	case "", "system":
		go watchPortal(ctx, func() {
			if t, err := Load(id); err == nil {
				onChange(t)
			}
		})
		return nil
	default:
		if _, err := os.Stat(filepath.Join(CustomDir(), id+".toml")); err != nil {
			return nil // built-in themes never change
		}
		dirs = []string{CustomDir()}
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, d := range dirs {
		w.Add(d)
	}
	go func() {
		defer w.Close()
		last := ""
		var timer *time.Timer
		fire := func() {
			t, err := Load(id)
			if err != nil {
				return
			}
			key := fmt.Sprint(t.Colors)
			if key != last {
				last = key
				onChange(t)
			}
			// re-add in case the watched directory was replaced
			for _, d := range dirs {
				w.Add(d)
			}
		}
		if t, err := Load(id); err == nil {
			last = fmt.Sprint(t.Colors)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.Events:
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(150*time.Millisecond, fire)
			case <-w.Errors:
			}
		}
	}()
	return nil
}

func watchPortal(ctx context.Context, f func()) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return
	}
	rule := []dbus.MatchOption{dbus.WithMatchInterface("org.freedesktop.portal.Settings"), dbus.WithMatchMember("SettingChanged")}
	if err := conn.AddMatchSignal(rule...); err != nil {
		return
	}
	ch := make(chan *dbus.Signal, 8)
	conn.Signal(ch)
	defer func() {
		conn.RemoveSignal(ch)
		conn.RemoveMatchSignal(rule...)
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case s := <-ch:
			if len(s.Body) >= 2 && s.Body[0] == "org.freedesktop.appearance" && s.Body[1] == "color-scheme" {
				f()
			}
		}
	}
}
