// Package sqllog records every statement dbro runs, from the GUI and from agents,
// in one JSON-lines file that both processes append to.
package sqllog

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ExeQue/dbro/internal/config"
)

// Entry is one logged statement.
type Entry struct {
	Time       time.Time `json:"time"`
	Source     string    `json:"source"` // gui | mcp
	Conn       string    `json:"conn"`
	Database   string    `json:"database,omitempty"`
	SQL        string    `json:"sql"`
	DurationMs float64   `json:"durationMs"`
	Rows       int64     `json:"rows"`
	Error      string    `json:"error,omitempty"`
}

const maxSize = 5 << 20

var (
	mu        sync.Mutex
	listeners []func(Entry)
)

// Path is the shared log file.
func Path() string { return filepath.Join(config.StateDir(), "sql.log") }

// Listen registers a callback for entries written by this process.
func Listen(f func(Entry)) {
	mu.Lock()
	listeners = append(listeners, f)
	mu.Unlock()
}

// Write appends an entry to the file and notifies in-process listeners.
func Write(e Entry) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	mu.Lock()
	ls := append([]func(Entry){}, listeners...)
	if err := os.MkdirAll(config.StateDir(), 0o700); err == nil {
		if st, err := os.Stat(Path()); err == nil && st.Size() > maxSize {
			os.Truncate(Path(), 0)
		}
		if f, err := os.OpenFile(Path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			b, _ := json.Marshal(e)
			f.Write(append(b, '\n'))
			f.Close()
		}
	}
	mu.Unlock()
	for _, l := range ls {
		l(e)
	}
}

// Tail calls f for each entry appended to the file by other processes with the given source,
// polling every interval until stop is closed.
func Tail(source string, interval time.Duration, stop <-chan struct{}, f func(Entry)) {
	var offset int64
	if st, err := os.Stat(Path()); err == nil {
		offset = st.Size()
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		file, err := os.Open(Path())
		if err != nil {
			continue
		}
		st, _ := file.Stat()
		if st.Size() < offset {
			offset = 0 // truncated
		}
		file.Seek(offset, io.SeekStart)
		r := bufio.NewReader(file)
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				break
			}
			offset += int64(len(line))
			var e Entry
			if json.Unmarshal(line, &e) == nil && e.Source == source {
				f(e)
			}
		}
		file.Close()
	}
}
