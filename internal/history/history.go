// Package history keeps per-connection query history and saved snippets.
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/ExeQue/dbro/internal/config"
)

const maxEntries = 500

// Entry is one query run from a query tab.
type Entry struct {
	Time     time.Time `json:"time"`
	Database string    `json:"database"`
	SQL      string    `json:"sql"`
}

// Snippet is a saved query.
type Snippet struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}

var (
	mu   sync.Mutex
	safe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

func historyPath(conn string) string {
	return filepath.Join(config.StateDir(), "history", safe.ReplaceAllString(conn, "_")+".json")
}

func readJSON(path string, v any) {
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, v)
	}
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

// List returns a connection's history, newest first.
func List(conn string) []Entry {
	mu.Lock()
	defer mu.Unlock()
	var out []Entry
	readJSON(historyPath(conn), &out)
	return out
}

// Add records a query, skipping an immediate repeat.
func Add(conn, database, sql string) error {
	mu.Lock()
	defer mu.Unlock()
	var list []Entry
	readJSON(historyPath(conn), &list)
	if len(list) > 0 && list[0].SQL == sql && list[0].Database == database {
		list[0].Time = time.Now()
	} else {
		list = append([]Entry{{Time: time.Now(), Database: database, SQL: sql}}, list...)
	}
	if len(list) > maxEntries {
		list = list[:maxEntries]
	}
	return writeJSON(historyPath(conn), list)
}

func snippetsPath() string { return filepath.Join(config.Dir(), "snippets.json") }

// Snippets returns saved snippets sorted by name.
func Snippets() []Snippet {
	mu.Lock()
	defer mu.Unlock()
	out := []Snippet{}
	readJSON(snippetsPath(), &out)
	return out
}

// SaveSnippet adds or replaces a snippet by name.
func SaveSnippet(s Snippet) error {
	mu.Lock()
	defer mu.Unlock()
	var list []Snippet
	readJSON(snippetsPath(), &list)
	replaced := false
	for i := range list {
		if list[i].Name == s.Name {
			list[i], replaced = s, true
		}
	}
	if !replaced {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return writeJSON(snippetsPath(), list)
}

// DeleteSnippet removes a snippet.
func DeleteSnippet(name string) error {
	mu.Lock()
	defer mu.Unlock()
	var list, keep []Snippet
	readJSON(snippetsPath(), &list)
	for _, s := range list {
		if s.Name != name {
			keep = append(keep, s)
		}
	}
	return writeJSON(snippetsPath(), keep)
}
