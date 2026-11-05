// Package portal opens the desktop's own file chooser through the XDG desktop portal
// (org.freedesktop.portal.FileChooser), the same picker other apps on the system use.
package portal

import (
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strings"

	"github.com/godbus/dbus/v5"
)

// Filter is one entry in the file type list, e.g. {"CSV", []string{"*.csv"}}.
type Filter struct {
	Name     string
	Patterns []string
}

// ErrUnavailable means no portal answered; callers fall back to another dialog.
var ErrUnavailable = errors.New("file chooser portal unavailable")

type filterSpec struct {
	Name  string
	Rules []struct {
		Kind    uint32
		Pattern string
	}
}

func filters(fs []Filter) []filterSpec {
	out := make([]filterSpec, 0, len(fs))
	for _, f := range fs {
		spec := filterSpec{Name: f.Name}
		for _, p := range f.Patterns {
			spec.Rules = append(spec.Rules, struct {
				Kind    uint32
				Pattern string
			}{0, p})
		}
		out = append(out, spec)
	}
	return out
}

// call runs a FileChooser method and waits for its Response. It returns "" when canceled.
func call(method, title string, opts map[string]dbus.Variant) (string, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return "", ErrUnavailable
	}
	token := fmt.Sprintf("dbro%d", rand.Uint32())
	opts["handle_token"] = dbus.MakeVariant(token)
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	path := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + sender + "/" + token)
	rule := []dbus.MatchOption{dbus.WithMatchObjectPath(path), dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response")}
	if err := conn.AddMatchSignal(rule...); err != nil {
		return "", ErrUnavailable
	}
	defer conn.RemoveMatchSignal(rule...)
	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)
	defer conn.RemoveSignal(ch)

	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	var handle dbus.ObjectPath
	if err := obj.Call("org.freedesktop.portal.FileChooser."+method, 0, "", title, opts).Store(&handle); err != nil {
		return "", ErrUnavailable
	}
	for s := range ch {
		if s.Path != path && s.Path != handle {
			continue
		}
		if len(s.Body) < 2 {
			return "", errors.New("malformed portal response")
		}
		if code, _ := s.Body[0].(uint32); code != 0 {
			return "", nil // canceled
		}
		results, _ := s.Body[1].(map[string]dbus.Variant)
		uris, _ := results["uris"].Value().([]string)
		if len(uris) == 0 {
			return "", nil
		}
		u, err := url.Parse(uris[0])
		if err != nil {
			return "", err
		}
		return u.Path, nil
	}
	return "", ErrUnavailable
}

// OpenFile asks for an existing file, or a directory when dir is true.
func OpenFile(title string, fs []Filter, dir bool) (string, error) {
	opts := map[string]dbus.Variant{"modal": dbus.MakeVariant(true)}
	if len(fs) > 0 {
		opts["filters"] = dbus.MakeVariant(filters(fs))
	}
	if dir {
		opts["directory"] = dbus.MakeVariant(true)
	}
	return call("OpenFile", title, opts)
}

// SaveFile asks where to save a file, suggesting name.
func SaveFile(title, name string, fs []Filter) (string, error) {
	opts := map[string]dbus.Variant{"modal": dbus.MakeVariant(true), "current_name": dbus.MakeVariant(name)}
	if len(fs) > 0 {
		opts["filters"] = dbus.MakeVariant(filters(fs))
	}
	return call("SaveFile", title, opts)
}
