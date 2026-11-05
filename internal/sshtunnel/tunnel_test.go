package sshtunnel

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/ExeQue/dbro/internal/config"
	"github.com/ExeQue/dbro/internal/sshtunnel/sshtest"
)

// echoServer answers each connection with "hello".
func echoServer(t *testing.T) int {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("hello"))
			c.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func read(t *testing.T, port int) string {
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	b, _ := io.ReadAll(c)
	return string(b)
}

func TestTunnel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)

	// a client key for the jump host
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, _ := ssh.MarshalPrivateKey(priv, "")
	os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), pem.EncodeToMemory(block), 0o600)
	clientPub, _ := ssh.NewPublicKey(pub)

	sshPort := sshtest.Server(t, "s3cret", clientPub)
	target := echoServer(t)
	s := config.SSH{Host: "127.0.0.1", Port: sshPort, User: "dev"}

	// unknown host key first, then trust it
	_, err := Open(s, "127.0.0.1", target, "s3cret")
	var uh *UnknownHostError
	if !errors.As(err, &uh) {
		t.Fatalf("want unknown host, got %v", err)
	}
	if err := Trust(uh); err != nil {
		t.Fatal(err)
	}

	// wrong password, then right password
	if tun, err := Open(config.SSH{Host: "127.0.0.1", Port: sshtest.Server(t, "x", nil), User: "dev"}, "127.0.0.1", target, "nope"); err == nil {
		tun.Close()
		t.Fatal("unknown second server must not be trusted")
	}
	tun, err := Open(s, "127.0.0.1", target, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, tun.Port); got != "hello" {
		t.Fatalf("through tunnel: %q", got)
	}
	tun.Close()

	// ProxyJump: jump host via key auth, target via password, both from ~/.ssh/config
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(fmt.Sprintf(`Host jumpbox
  HostName 127.0.0.1
  Port %d
  User dev

Host behind
  HostName 127.0.0.1
  Port %d
  User dev
  ProxyJump jumpbox
`, sshPort, sshPort)), 0o600)
	reloadSSHConfig()
	tun, err = Open(config.SSH{Host: "behind"}, "127.0.0.1", target, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, tun.Port); got != "hello" {
		t.Fatalf("through jump: %q", got)
	}
	tun.Close()
}
