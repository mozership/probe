package ssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	testUser     = "probe"
	testPassword = "secret"
)

// session is what a command handler sees of the session it runs in.
type session struct {
	cmd     string
	env     map[string]string
	stdout  io.Writer
	stderr  io.Writer
	signals <-chan string
}

// handler runs a command in a test server. It returns the exit status to
// report, or false to close the session without reporting one.
type handler func(s session) (status uint32, ok bool)

// testServer is an SSH server in the test process. It accepts the user
// testUser with testPassword or with clientKey, sets only environment
// variables whose names start with PROBE_, and hands every command to its
// handler instead of a shell.
type testServer struct {
	addr      string
	hostKey   ssh.PublicKey
	clientKey ed25519.PrivateKey

	mu       sync.Mutex
	commands []string
	signals  []string
	envs     []map[string]string
}

func newTestServer(t *testing.T, handle handler) *testServer {
	t.Helper()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	clientPub, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := ssh.NewPublicKey(clientPub)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == testUser && string(pass) == testPassword {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == testUser && bytes.Equal(key.Marshal(), authorized.Marshal()) {
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
	t.Cleanup(func() { _ = ln.Close() })

	s := &testServer{addr: ln.Addr().String(), hostKey: hostSigner.PublicKey(), clientKey: clientPriv}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(nc, cfg, handle)
		}
	}()
	return s
}

func (s *testServer) serve(nc net.Conn, cfg *ssh.ServerConfig, handle handler) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		_ = nc.Close()
		return
	}
	defer func() { _ = conn.Close() }()
	go ssh.DiscardRequests(reqs)

	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go s.session(ch, chReqs, handle)
	}
}

func (s *testServer) session(ch ssh.Channel, reqs <-chan *ssh.Request, handle handler) {
	env := map[string]string{}
	signals := make(chan string, 4)

	for req := range reqs {
		switch req.Type {
		case "env":
			var kv struct{ Name, Value string }
			_ = ssh.Unmarshal(req.Payload, &kv)
			accepted := strings.HasPrefix(kv.Name, "PROBE_")
			if accepted {
				env[kv.Name] = kv.Value
			}
			_ = req.Reply(accepted, nil)

		case "exec":
			var p struct{ Command string }
			_ = ssh.Unmarshal(req.Payload, &p)
			_ = req.Reply(true, nil)

			s.mu.Lock()
			s.commands = append(s.commands, p.Command)
			s.envs = append(s.envs, env)
			s.mu.Unlock()

			go func() {
				status, ok := handle(session{cmd: p.Command, env: env, stdout: ch, stderr: ch.Stderr(), signals: signals})
				if ok {
					_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				}
				_ = ch.Close()
			}()

		case "signal":
			var p struct{ Signal string }
			_ = ssh.Unmarshal(req.Payload, &p)
			s.mu.Lock()
			s.signals = append(s.signals, p.Signal)
			s.mu.Unlock()
			select {
			case signals <- p.Signal:
			default:
			}
			if req.WantReply {
				_ = req.Reply(true, nil)
			}

		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func (s *testServer) host() string {
	host, _, _ := net.SplitHostPort(s.addr)
	return host
}

func (s *testServer) port() int {
	_, port, _ := net.SplitHostPort(s.addr)
	n, _ := strconv.Atoi(port)
	return n
}

func (s *testServer) received() (commands, signals []string, envs []map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...), append([]string(nil), s.signals...), append([]map[string]string(nil), s.envs...)
}

// writeClientKey writes the key the server accepts, encrypted with
// passphrase unless it is empty, and returns its path.
func (s *testServer) writeClientKey(t *testing.T, passphrase string) string {
	t.Helper()
	var block *pem.Block
	var err error
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(s.clientKey, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(s.clientKey, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeKnownHosts writes a known_hosts file that lists key for the server's
// address, and returns its path.
func (s *testServer) writeKnownHosts(t *testing.T, key ssh.PublicKey) string {
	t.Helper()
	line := knownhosts.Line([]string{knownhosts.Normalize(s.addr)}, key)
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// req returns a request for cmd that logs in with the password and skips the
// host key check.
func (s *testServer) req(cmd string) *Req {
	r := NewReq()
	r.Host = s.host()
	r.Port = s.port()
	r.User = testUser
	r.Password = testPassword
	r.Cmd = cmd
	r.StrictHostCheck = false
	return r
}
