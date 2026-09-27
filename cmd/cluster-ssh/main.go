// cluster-ssh prepares key-based SSH access to cluster nodes for the
// one-click deployment (scripts/cluster-up.*). It logs in with the operator's
// password once, records each host key (trust on first use) and installs a
// deployment key; every later step uses that key through the OpenSSH client.
// Passwords are read from standard input, never from arguments or files:
//
//	default=<password shared by all nodes>
//	10.0.0.12=<password of one node>
//
//	cluster-ssh check     -key <file> -known-hosts <file> -user root -port 22 -nodes 10.0.0.11,10.0.0.12
//	cluster-ssh bootstrap -key <file> -known-hosts <file> -user root -port 22 -nodes ... < passwords
//
// Each node prints "ok <address>" or "fail <address>: <reason>"; the exit
// status is 1 when any node failed.
package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type config struct {
	keyPath, knownHosts, user, comment string
	port                               int
	timeout                            time.Duration
}

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "check" && os.Args[1] != "bootstrap") {
		fmt.Fprintln(os.Stderr, "usage: cluster-ssh check|bootstrap -key <file> -known-hosts <file> -nodes <ip,...> [-user root] [-port 22]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	var c config
	fs.StringVar(&c.keyPath, "key", "", "deployment private key (created on bootstrap when missing)")
	fs.StringVar(&c.knownHosts, "known-hosts", "", "known_hosts file for the cluster nodes")
	fs.StringVar(&c.user, "user", "root", "SSH user")
	fs.IntVar(&c.port, "port", 22, "SSH port")
	fs.StringVar(&c.comment, "comment", "torchlink-deploy", "comment written with the public key")
	nodes := fs.String("nodes", "", "node addresses, comma separated")
	_ = fs.Parse(os.Args[2:])
	c.timeout = 15 * time.Second
	if c.keyPath == "" || c.knownHosts == "" || *nodes == "" {
		fmt.Fprintln(os.Stderr, "-key, -known-hosts and -nodes are required")
		os.Exit(2)
	}
	var addresses []string
	for _, a := range strings.Split(*nodes, ",") {
		if a = strings.TrimSpace(a); a != "" {
			addresses = append(addresses, a)
		}
	}
	var results map[string]error
	if os.Args[1] == "check" {
		results = check(c, addresses)
	} else {
		passwords, err := readPasswords(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		if results, err = bootstrap(c, addresses, passwords); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
	}
	failed := false
	for _, a := range addresses {
		if err := results[a]; err != nil {
			failed = true
			fmt.Printf("fail %s: %s\n", a, err)
		} else {
			fmt.Printf("ok %s\n", a)
		}
	}
	if failed {
		os.Exit(1)
	}
}

// readPasswords parses "default=..." and "<address>=..." lines.
func readPasswords(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if k, v, ok := strings.Cut(line, "="); ok && k != "" && v != "" {
			out[strings.TrimSpace(k)] = v
		}
	}
	return out, sc.Err()
}

func (c config) address(host string) string { return net.JoinHostPort(host, strconv.Itoa(c.port)) }

// hostKeys accepts a host key on first use and records it; a changed key
// is refused. mu serialises updates of the known_hosts file.
func (c config) hostKeys(mu *sync.Mutex) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		mu.Lock()
		defer mu.Unlock()
		if _, err := os.Stat(c.knownHosts); errors.Is(err, os.ErrNotExist) {
			if err = os.MkdirAll(filepath.Dir(c.knownHosts), 0o700); err != nil {
				return err
			}
			if err = os.WriteFile(c.knownHosts, nil, 0o600); err != nil {
				return err
			}
		}
		verify, err := knownhosts.New(c.knownHosts)
		if err != nil {
			return err
		}
		err = verify(hostname, remote, key)
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			f, ferr := os.OpenFile(c.knownHosts, os.O_APPEND|os.O_WRONLY, 0o600)
			if ferr != nil {
				return ferr
			}
			defer f.Close()
			_, ferr = fmt.Fprintln(f, knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key))
			return ferr
		}
		if errors.As(err, &keyErr) {
			return fmt.Errorf("host key of %s changed since it was first trusted (%s); if the node was reinstalled, remove its line from %s", hostname, ssh.FingerprintSHA256(key), c.knownHosts)
		}
		return err
	}
}

func (c config) signer() (ssh.Signer, error) {
	b, err := os.ReadFile(c.keyPath)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(b)
}

// ensureKey creates the ed25519 deployment key pair when missing.
func (c config) ensureKey() (ssh.Signer, error) {
	if s, err := c.signer(); err == nil {
		return s, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("deployment key %s: %w", c.keyPath, err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, c.comment)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(c.keyPath), 0o700); err != nil {
		return nil, err
	}
	if err = os.WriteFile(c.keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " " + c.comment + "\n"
	return signer, os.WriteFile(c.keyPath+".pub", []byte(pub), 0o644)
}

func (c config) dial(host string, mu *sync.Mutex, auth ...ssh.AuthMethod) (*ssh.Client, error) {
	return ssh.Dial("tcp", c.address(host), &ssh.ClientConfig{User: c.user, Auth: auth, HostKeyCallback: c.hostKeys(mu), Timeout: c.timeout})
}

// check reports whether the deployment key already logs in to every node.
func check(c config, hosts []string) map[string]error {
	out := map[string]error{}
	signer, err := c.signer()
	var mu sync.Mutex
	for _, h := range hosts {
		if err != nil {
			out[h] = fmt.Errorf("no deployment key yet")
			continue
		}
		client, derr := c.dial(h, &mu, ssh.PublicKeys(signer))
		if derr != nil {
			out[h] = shortError(derr)
			continue
		}
		client.Close()
		out[h] = nil
	}
	return out
}

// bootstrap logs in with the node's password (or the default one), installs
// the deployment public key and confirms that key login works.
func bootstrap(c config, hosts []string, passwords map[string]string) (map[string]error, error) {
	signer, err := c.ensureKey()
	if err != nil {
		return nil, err
	}
	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " " + c.comment
	out := map[string]error{}
	var mu sync.Mutex
	for _, h := range hosts {
		// Nodes that already accept the key need no password.
		if client, err := c.dial(h, &mu, ssh.PublicKeys(signer)); err == nil {
			client.Close()
			out[h] = nil
			continue
		}
		password := passwords[h]
		if password == "" {
			password = passwords["default"]
		}
		if password == "" {
			out[h] = fmt.Errorf("no password given for this node")
			continue
		}
		// keyboard-interactive: sshd configurations that only offer PAM
		// prompts get the same password.
		answer := func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = password
			}
			return answers, nil
		}
		client, err := c.dial(h, &mu, ssh.Password(password), ssh.KeyboardInteractive(answer))
		if err != nil {
			out[h] = shortError(err)
			continue
		}
		out[h] = install(client, authorized)
		client.Close()
		if out[h] != nil {
			continue
		}
		verify, err := c.dial(h, &mu, ssh.PublicKeys(signer))
		if err != nil {
			out[h] = fmt.Errorf("key installed but key login still fails (check sshd PubkeyAuthentication and %s's home permissions): %s", c.user, shortError(err))
			continue
		}
		verify.Close()
	}
	return out, nil
}

// install appends the key once; the key line has no shell metacharacters
// (base64 and a plain comment), so single quoting is safe.
func install(client *ssh.Client, authorized string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	cmd := "umask 077; mkdir -p ~/.ssh && touch ~/.ssh/authorized_keys && chmod 700 ~/.ssh && chmod 600 ~/.ssh/authorized_keys && " +
		"(grep -qxF '" + authorized + "' ~/.ssh/authorized_keys || printf '%s\\n' '" + authorized + "' >> ~/.ssh/authorized_keys)"
	if out, err := session.CombinedOutput(cmd); err != nil {
		return fmt.Errorf("installing the key failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func shortError(err error) error {
	msg := err.Error()
	switch {
	// "unexpected message type 51" is x/crypto's report when sshd rejects
	// the last offered method (for example after a wrong password).
	case strings.Contains(msg, "unable to authenticate"), strings.Contains(msg, "unexpected message type 51"):
		return errors.New("login refused (wrong password or user, or password login disabled in sshd)")
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "connection refused"), strings.Contains(msg, "no route to host"):
		return fmt.Errorf("cannot reach SSH: %s", msg)
	}
	return err
}
