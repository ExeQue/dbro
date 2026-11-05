// Package secrets stores passwords in the system keyring (Secret Service).
// When no keyring is available, passwords live in memory for the session only.
package secrets

import (
	"errors"
	"sync"

	"github.com/zalando/go-keyring"
)

const service = "dbro"

var (
	mu      sync.Mutex
	session = map[string]string{}
)

// ErrNotFound means no password is stored for the key.
var ErrNotFound = errors.New("no stored password")

// Key names the secret for a connection; kind is "" for the database password,
// "ssh" for the SSH password or key passphrase.
func Key(conn, kind string) string {
	if kind == "" {
		return conn
	}
	return conn + "/" + kind
}

// Get returns a stored password.
func Get(key string) (string, error) {
	mu.Lock()
	if v, ok := session[key]; ok {
		mu.Unlock()
		return v, nil
	}
	mu.Unlock()
	v, err := keyring.Get(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", ErrNotFound // no keyring: behave as not stored
	}
	return v, nil
}

// Set stores a password. remember=false keeps it in memory only.
// It reports whether the keyring accepted it.
func Set(key, value string, remember bool) bool {
	mu.Lock()
	session[key] = value
	mu.Unlock()
	if !remember {
		return false
	}
	return keyring.Set(service, key, value) == nil
}

// Delete removes a password from memory and the keyring.
func Delete(key string) {
	mu.Lock()
	delete(session, key)
	mu.Unlock()
	_ = keyring.Delete(service, key)
}
