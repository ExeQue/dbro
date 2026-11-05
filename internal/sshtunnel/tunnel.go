// Package sshtunnel forwards a local port to a database through an SSH server,
// using ~/.ssh/config, ssh-agent and known_hosts like OpenSSH does.
package sshtunnel

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/ExeQue/dbro/internal/config"
)

// sshConfig is ~/.ssh/config, read fresh for every tunnel so edits apply.
type sshConfig struct{ files []*ssh_config.Config }

func (c sshConfig) Get(alias, key string) string {
	for _, f := range c.files {
		if v, err := f.Get(alias, key); err == nil && v != "" {
			return v
		}
	}
	return ""
}

func loadSSHConfig() sshConfig {
	var c sshConfig
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, ".ssh", "config"), "/etc/ssh/ssh_config"} {
		if f, err := os.Open(p); err == nil {
			if cfg, err := ssh_config.Decode(f); err == nil {
				c.files = append(c.files, cfg)
			}
			f.Close()
		}
	}
	return c
}

var sshcfg = loadSSHConfig()

func reloadSSHConfig() { sshcfg = loadSSHConfig() }

// UnknownHostError asks the user to trust a host key.
type UnknownHostError struct {
	Host        string
	Fingerprint string
	key         ssh.PublicKey
}

func (e *UnknownHostError) Error() string {
	return fmt.Sprintf("unknown SSH host key for %s (%s)", e.Host, e.Fingerprint)
}

// Tunnel is an open SSH tunnel listening on 127.0.0.1.
type Tunnel struct {
	Port    int
	clients []*ssh.Client
	ln      net.Listener
	wg      sync.WaitGroup
}

// Close stops the listener and the SSH connections.
func (t *Tunnel) Close() error {
	t.ln.Close()
	for i := len(t.clients) - 1; i >= 0; i-- {
		t.clients[i].Close()
	}
	t.wg.Wait()
	return nil
}

type hop struct {
	addr, user, key string
	alias           string
}

func resolve(alias string, port int, user, key string) hop {
	h := hop{alias: alias}
	host := sshcfg.Get(alias, "HostName")
	if host == "" {
		host = alias
	}
	if port == 0 {
		port, _ = strconv.Atoi(sshcfg.Get(alias, "Port"))
	}
	if port == 0 {
		port = 22
	}
	if user == "" {
		user = sshcfg.Get(alias, "User")
	}
	if user == "" {
		user = os.Getenv("USER")
	}
	if key == "" {
		if k := sshcfg.Get(alias, "IdentityFile"); k != "" && k != "~/.ssh/identity" {
			key = k
		}
	}
	h.addr = net.JoinHostPort(host, strconv.Itoa(port))
	h.user, h.key = user, config.ExpandHome(key)
	return h
}

// Open dials the SSH server (through ProxyJump hosts when configured) and
// forwards a local port to remoteHost:remotePort.
// secret is the SSH password or key passphrase; it may be empty.
func Open(s config.SSH, remoteHost string, remotePort int, secret string) (*Tunnel, error) {
	reloadSSHConfig()
	target := resolve(s.Host, s.Port, s.User, s.Key)
	var hops []hop
	if pj := sshcfg.Get(s.Host, "ProxyJump"); pj != "" && pj != "none" {
		for _, j := range strings.Split(pj, ",") {
			user, host := "", strings.TrimSpace(j)
			if u, h, ok := strings.Cut(host, "@"); ok {
				user, host = u, h
			}
			port := 0
			if h, p, err := net.SplitHostPort(host); err == nil {
				host = h
				port, _ = strconv.Atoi(p)
			}
			hops = append(hops, resolve(host, port, user, ""))
		}
	}
	hops = append(hops, target)

	t := &Tunnel{}
	var prev *ssh.Client
	for i, h := range hops {
		pass := ""
		if i == len(hops)-1 {
			pass = secret
		}
		cfg, err := clientConfig(h, pass)
		if err != nil {
			t.Close2()
			return nil, err
		}
		var c *ssh.Client
		if prev == nil {
			c, err = ssh.Dial("tcp", h.addr, cfg)
		} else {
			var nc net.Conn
			nc, err = prev.Dial("tcp", h.addr)
			if err == nil {
				var cc ssh.Conn
				var chans <-chan ssh.NewChannel
				var reqs <-chan *ssh.Request
				cc, chans, reqs, err = ssh.NewClientConn(nc, h.addr, cfg)
				if err == nil {
					c = ssh.NewClient(cc, chans, reqs)
				}
			}
		}
		if err != nil {
			t.Close2()
			var uh *UnknownHostError
			if errors.As(err, &uh) {
				return nil, uh
			}
			return nil, fmt.Errorf("ssh %s: %w", h.addr, err)
		}
		t.clients = append(t.clients, c)
		prev = c
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Close2()
		return nil, err
	}
	t.ln = ln
	t.Port = ln.Addr().(*net.TCPAddr).Port
	remote := net.JoinHostPort(remoteHost, strconv.Itoa(remotePort))
	go func() {
		for {
			local, err := ln.Accept()
			if err != nil {
				return
			}
			t.wg.Add(1)
			go func() {
				defer t.wg.Done()
				defer local.Close()
				rc, err := prev.Dial("tcp", remote)
				if err != nil {
					return
				}
				defer rc.Close()
				done := make(chan struct{}, 2)
				go func() { io.Copy(rc, local); done <- struct{}{} }()
				go func() { io.Copy(local, rc); done <- struct{}{} }()
				<-done
			}()
		}
	}()
	return t, nil
}

// Close2 closes clients opened so far, before a listener exists.
func (t *Tunnel) Close2() {
	for _, c := range t.clients {
		c.Close()
	}
}

func knownHostsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "known_hosts")
}

func clientConfig(h hop, secret string) (*ssh.ClientConfig, error) {
	var auths []ssh.AuthMethod
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			auths = append(auths, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}
	keys := []string{h.key}
	if h.key == "" {
		home, _ := os.UserHomeDir()
		keys = []string{filepath.Join(home, ".ssh", "id_ed25519"), filepath.Join(home, ".ssh", "id_rsa"), filepath.Join(home, ".ssh", "id_ecdsa")}
	}
	for _, k := range keys {
		b, err := os.ReadFile(k)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(b)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) && secret != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(b, []byte(secret))
		}
		if err == nil {
			auths = append(auths, ssh.PublicKeys(signer))
		}
	}
	if secret != "" {
		auths = append(auths, ssh.Password(secret))
	}

	khPath := knownHostsPath()
	cb, err := knownhosts.New(khPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	hostKey := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if cb == nil {
			return &UnknownHostError{Host: hostname, Fingerprint: ssh.FingerprintSHA256(key), key: key}
		}
		err := cb(hostname, remote, key)
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) && len(ke.Want) == 0 {
			return &UnknownHostError{Host: hostname, Fingerprint: ssh.FingerprintSHA256(key), key: key}
		}
		return err
	}
	return &ssh.ClientConfig{User: h.user, Auth: auths, HostKeyCallback: hostKey}, nil
}

// Trust appends the host key from an UnknownHostError to ~/.ssh/known_hosts.
func Trust(e *UnknownHostError) error {
	p := knownHostsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, knownhosts.Line([]string{knownhosts.Normalize(e.Host)}, e.key))
	return err
}

// FetchHostKey connects only far enough to learn the server's key, for Trust after an UnknownHostError
// that crossed a process boundary (the error itself carries the key in-process).
func FetchHostKey(s config.SSH) (*UnknownHostError, error) {
	h := resolve(s.Host, s.Port, s.User, s.Key)
	var got *UnknownHostError
	cfg := &ssh.ClientConfig{User: h.user, HostKeyCallback: func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		got = &UnknownHostError{Host: hostname, Fingerprint: ssh.FingerprintSHA256(key), key: key}
		return errors.New("stop")
	}}
	_, err := ssh.Dial("tcp", h.addr, cfg)
	if got != nil {
		return got, nil
	}
	return nil, err
}
