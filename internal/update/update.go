// Package update finds the latest dbro release on GitHub and installs it over the running binary.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const api = "https://api.github.com/repos/ExeQue/dbro/releases/latest"

// Release is the latest published version.
type Release struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	Available bool   `json:"available"` // newer than the running version
	// Installable is false when dbro cannot replace itself (a package manager owns it, or the folder is read-only).
	Installable bool   `json:"installable"`
	Reason      string `json:"reason,omitempty"`
	assets      map[string]string
}

var client = &http.Client{Timeout: 30 * time.Second}

// Latest asks GitHub for the newest release and compares it with current.
func Latest(ctx context.Context, current string) (Release, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var gh struct {
		Tag    string `json:"tag_name"`
		URL    string `json:"html_url"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return Release{}, err
	}
	r := Release{Version: gh.Tag, URL: gh.URL, Available: Newer(gh.Tag, current), assets: map[string]string{}}
	for _, a := range gh.Assets {
		r.assets[a.Name] = a.URL
	}
	r.Reason = installable(r)
	r.Installable = r.Reason == ""
	return r, nil
}

// Newer reports whether release is a later version than current. A development build
// ("dev") never asks to update; "v0.1.4-3-gabc" counts as v0.1.4.
func Newer(release, current string) bool {
	a, ok1 := parse(release)
	b, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var n [3]int
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

// target names the release archive for this platform.
func target() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64":
		return "linux-" + runtime.GOARCH + ".tar.gz", nil
	case "darwin/amd64", "darwin/arm64":
		return "macos-universal.zip", nil
	case "windows/amd64":
		return "windows-amd64.zip", nil
	}
	return "", fmt.Errorf("no release build for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// self is the running binary, read at start: once an update moves it aside, os.Executable points at the old file.
var self = func() string {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return ""
	}
	return exe
}()

// Executable is the path dbro was started from, where an update puts the new version.
func Executable() string { return self }

// dest is what an update replaces: the binary, or the whole dbro.app on macOS.
func dest() (string, error) {
	exe := self
	if exe == "" {
		return "", errors.New("cannot find the dbro binary")
	}
	if runtime.GOOS == "darwin" {
		if app := filepath.Dir(filepath.Dir(filepath.Dir(exe))); strings.HasSuffix(app, ".app") {
			return app, nil
		}
	}
	return exe, nil
}

// installable explains why this copy cannot update itself, or returns "".
func installable(r Release) string {
	t, err := target()
	if err != nil {
		return err.Error()
	}
	if _, ok := r.assets[archiveName(r.Version, t)]; !ok {
		return "the release has no build for this platform"
	}
	d, err := dest()
	if err != nil {
		return err.Error()
	}
	if strings.HasPrefix(d, "/usr/") {
		return "installed by a package manager; update it there"
	}
	f, err := os.CreateTemp(filepath.Dir(d), ".dbro-update-*")
	if err != nil {
		return filepath.Dir(d) + " is not writable"
	}
	f.Close()
	os.Remove(f.Name())
	return ""
}

func archiveName(version, target string) string { return "dbro-" + version + "-" + target }

// Install downloads the release, checks its SHA-256 and swaps it in for the running copy.
// The new version runs from the next start.
func Install(ctx context.Context, r Release) error {
	if !r.Installable {
		return errors.New(r.Reason)
	}
	t, _ := target()
	name := archiveName(r.Version, t)
	archive, err := get(ctx, r.assets[name])
	if err != nil {
		return err
	}
	sum, err := get(ctx, r.assets[name+".sha256"])
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	got := sha256.Sum256(archive)
	if want, _, _ := strings.Cut(string(sum), " "); hex.EncodeToString(got[:]) != strings.TrimSpace(want) {
		return errors.New("the download does not match its checksum")
	}
	d, err := dest()
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		return installApp(archive, d)
	}
	bin, err := extractBinary(archive)
	if err != nil {
		return err
	}
	return swapFile(bin, d)
}

func get(ctx context.Context, url string) ([]byte, error) {
	if url == "" {
		return nil, errors.New("missing download")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download answered %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// extractBinary pulls the dbro binary out of a Linux tar.gz or the Windows zip.
func extractBinary(archive []byte) ([]byte, error) {
	want := "dbro"
	if runtime.GOOS == "windows" {
		want = "dbro.exe"
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == want {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == want {
				return io.ReadAll(tr)
			}
		}
	}
	return nil, fmt.Errorf("%s not found in the download", want)
}

// swapFile replaces path with data. A running binary can be renamed (also on Windows),
// so the old one moves aside and is removed now or, on Windows, on the next start.
func swapFile(data []byte, path string) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	old := path + ".old"
	os.Remove(old)
	if err := os.Rename(path, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Rename(old, path)
		return err
	}
	os.Remove(old) // fails on Windows while running; Cleanup takes it later
	return nil
}

// installApp unpacks dbro.app next to the current one and swaps the folders.
func installApp(archive []byte, app string) error {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(app), ".dbro-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, f := range zr.File {
		p := filepath.Join(stage, f.Name)
		if !strings.HasPrefix(p, stage+string(os.PathSeparator)) {
			return fmt.Errorf("bad path in download: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		if f.Mode()&os.ModeSymlink != 0 {
			err = os.Symlink(string(data), p)
		} else {
			err = os.WriteFile(p, data, f.Mode().Perm())
		}
		if err != nil {
			return err
		}
	}
	fresh := filepath.Join(stage, "dbro.app")
	if _, err := os.Stat(fresh); err != nil {
		return errors.New("dbro.app not found in the download")
	}
	old := filepath.Join(stage, "old.app")
	if err := os.Rename(app, old); err != nil {
		return err
	}
	if err := os.Rename(fresh, app); err != nil {
		os.Rename(old, app)
		return err
	}
	return nil
}

// Cleanup removes the binary an update on Windows had to leave behind.
func Cleanup() {
	if d, err := dest(); err == nil {
		os.Remove(d + ".old")
	}
}
