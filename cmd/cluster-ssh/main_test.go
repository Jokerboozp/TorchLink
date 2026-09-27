package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testNode is an SSH server that accepts one password, runs exec requests
// with sh in its own home directory and accepts keys from that home's
// ~/.ssh/authorized_keys, like sshd.
type testNode struct {
	home     string
	port     int
	password string
	mu       sync.Mutex
	logins   []string
}

func startNode(t *testing.T, password string, hostKey ssh.Signer) *testNode {
	t.Helper()
	n := &testNode{home: t.TempDir(), password: password}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			if c.User() == "root" && string(p) == n.password {
				n.record("password")
				return nil, nil
			}
			return nil, os.ErrPermission
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			b, _ := os.ReadFile(filepath.Join(n.home, ".ssh", "authorized_keys"))
			if bytes.Contains(b, bytes.TrimSpace(ssh.MarshalAuthorizedKey(key))) {
				n.record("key")
				return nil, nil
			}
			return nil, os.ErrPermission
		},
	}
	cfg.AddHostKey(hostKey)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	n.port = ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go n.serve(conn, cfg)
		}
	}()
	return n
}

func (n *testNode) record(kind string) {
	n.mu.Lock()
	n.logins = append(n.logins, kind)
	n.mu.Unlock()
}

func (n *testNode) serve(conn net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer channel.Close()
			for req := range requests {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				var payload struct{ Command string }
				_ = ssh.Unmarshal(req.Payload, &payload)
				_ = req.Reply(true, nil)
				cmd := exec.Command("sh", "-c", payload.Command)
				cmd.Env = []string{"HOME=" + n.home, "PATH=" + os.Getenv("PATH")}
				cmd.Stdout, cmd.Stderr = channel, channel.Stderr()
				status := 0
				if err := cmd.Run(); err != nil {
					status = 1
				}
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(status)}))
				return
			}
		}()
	}
}

func hostKey(t *testing.T) ssh.Signer {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBootstrapInstallsKeyWithUnifiedOrPerNodePasswords(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test server runs commands with sh")
	}
	state := t.TempDir()
	// Two nodes on one address need distinct ports; the tool takes one port,
	// so each node is bootstrapped with its own config sharing the key.
	a := startNode(t, "same-password", hostKey(t))
	b := startNode(t, "other-password", hostKey(t))
	base := config{keyPath: filepath.Join(state, "deploy_key"), knownHosts: filepath.Join(state, "known_hosts"), user: "root", comment: "torchlink-deploy@test", timeout: 5 * time.Second}
	run := func(n *testNode, passwords map[string]string) error {
		c := base
		c.port = n.port
		res, err := bootstrap(c, []string{"127.0.0.1"}, passwords)
		if err != nil {
			t.Fatal(err)
		}
		return res["127.0.0.1"]
	}
	if err := run(a, map[string]string{"default": "wrong"}); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatal("a wrong password must be reported", err)
	}
	if err := run(a, map[string]string{"default": "same-password"}); err != nil {
		t.Fatal(err)
	}
	if err := run(b, map[string]string{"default": "same-password", "127.0.0.1": "other-password"}); err != nil {
		t.Fatal("per-node password must win over the default", err)
	}
	info, err := os.Stat(base.keyPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("deployment key", err)
	}
	keys, _ := os.ReadFile(filepath.Join(a.home, ".ssh", "authorized_keys"))
	if strings.Count(string(keys), "torchlink-deploy@test") != 1 {
		t.Fatalf("authorized_keys: %q", keys)
	}
	// A second bootstrap uses the key and needs no password; the key line is not duplicated.
	if err := run(a, nil); err != nil {
		t.Fatal(err)
	}
	keys, _ = os.ReadFile(filepath.Join(a.home, ".ssh", "authorized_keys"))
	if strings.Count(string(keys), "torchlink-deploy@test") != 1 {
		t.Fatal("key appended twice")
	}
	c := base
	c.port = a.port
	if res := check(c, []string{"127.0.0.1"}); res["127.0.0.1"] != nil {
		t.Fatal(res)
	}
	known, _ := os.ReadFile(base.knownHosts)
	if !strings.Contains(string(known), "[127.0.0.1]:"+strconv.Itoa(a.port)) || !strings.Contains(string(known), "[127.0.0.1]:"+strconv.Itoa(b.port)) {
		t.Fatalf("known_hosts: %s", known)
	}
	// A node whose host key changed is refused, even with the right password.
	replaced := startNode(t, "same-password", hostKey(t))
	c.port = replaced.port
	// Pretend the replaced server answers on a's recorded address.
	b2, _ := os.ReadFile(base.knownHosts)
	_ = os.WriteFile(base.knownHosts, bytes.ReplaceAll(b2, []byte("[127.0.0.1]:"+strconv.Itoa(a.port)), []byte("[127.0.0.1]:"+strconv.Itoa(replaced.port))), 0o600)
	res, err := bootstrap(c, []string{"127.0.0.1"}, map[string]string{"default": "same-password"})
	if err != nil || res["127.0.0.1"] == nil || !strings.Contains(res["127.0.0.1"].Error(), "host key") {
		t.Fatal("a changed host key must be refused", res, err)
	}
}

func TestPasswordsNeverReachOutputOrFiles(t *testing.T) {
	passwords, err := readPasswords(strings.NewReader("default=Shared=Pass\r\n10.0.0.5=Own-Pass\n\nbad-line\n"))
	if err != nil || passwords["default"] != "Shared=Pass" || passwords["10.0.0.5"] != "Own-Pass" || len(passwords) != 2 {
		t.Fatal(passwords, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	state := t.TempDir()
	n := startNode(t, "Secret-Pass-123", hostKey(t))
	c := config{keyPath: filepath.Join(state, "k"), knownHosts: filepath.Join(state, "kh"), user: "root", port: n.port, comment: "c", timeout: 5 * time.Second}
	if res, _ := bootstrap(c, []string{"127.0.0.1"}, map[string]string{"default": "Secret-Pass-123"}); res["127.0.0.1"] != nil {
		t.Fatal(res)
	}
	_ = filepath.Walk(state, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if b, _ := os.ReadFile(p); bytes.Contains(b, []byte("Secret-Pass-123")) {
				t.Fatalf("password written to %s", p)
			}
		}
		return nil
	})
}
