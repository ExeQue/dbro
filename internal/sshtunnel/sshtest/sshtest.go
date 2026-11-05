// Package sshtest runs an in-process SSH server for tests.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Server accepts the given password or client key and forwards direct-tcpip channels.
func Server(t *testing.T, password string, clientKey ssh.PublicKey) int {
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, _ := ssh.NewSignerFromKey(hostPriv)
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			if string(p) == password {
				return nil, nil
			}
			return nil, errors.New("bad password")
		},
		PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if clientKey != nil && string(k.Marshal()) == string(clientKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unknown key")
		},
	}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					if ch.ChannelType() != "direct-tcpip" {
						ch.Reject(ssh.UnknownChannelType, "no")
						continue
					}
					d := ch.ExtraData()
					hl := binary.BigEndian.Uint32(d[:4])
					host := string(d[4 : 4+hl])
					port := binary.BigEndian.Uint32(d[4+hl : 8+hl])
					target, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
					if err != nil {
						ch.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					c, creqs, _ := ch.Accept()
					go ssh.DiscardRequests(creqs)
					go func() { io.Copy(c, target); c.Close() }()
					go func() { io.Copy(target, c); target.Close() }()
				}
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

