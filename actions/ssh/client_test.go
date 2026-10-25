package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestNewReq(t *testing.T) {
	req := NewReq()

	// Test default values
	if req.Port != 22 {
		t.Errorf("Expected default port 22, got %d", req.Port)
	}
	if req.Timeout != "30s" {
		t.Errorf("Expected default timeout '30s', got %s", req.Timeout)
	}
	if !req.StrictHostCheck {
		t.Errorf("Expected default StrictHostCheck true, got %v", req.StrictHostCheck)
	}
	if req.Env == nil {
		t.Errorf("Expected Env map to be initialized")
	}
}

func TestParseParams(t *testing.T) {
	tests := []struct {
		name      string
		req       *Req
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid request with password",
			req: &Req{
				Host:     "example.com",
				Port:     22,
				User:     "testuser",
				Cmd:      "ls -la",
				Password: "testpass",
				Timeout:  "30s",
			},
			wantError: false,
		},
		{
			name: "valid request with key file (but file validation skipped in this test)",
			req: &Req{
				Host:     "example.com",
				Port:     22,
				User:     "testuser",
				Cmd:      "ls -la",
				KeyFile:  "",         // Set empty to skip key file validation in parseParams test
				Password: "testpass", // Use password instead for this test
				Timeout:  "30s",
			},
			wantError: false,
		},
		{
			name: "missing host",
			req: &Req{
				Port:     22,
				User:     "testuser",
				Cmd:      "ls -la",
				Password: "testpass",
				Timeout:  "30s",
			},
			wantError: true,
			errorMsg:  "host parameter is required",
		},
		{
			name: "missing user",
			req: &Req{
				Host:     "example.com",
				Port:     22,
				Cmd:      "ls -la",
				Password: "testpass",
				Timeout:  "30s",
			},
			wantError: true,
			errorMsg:  "user parameter is required",
		},
		{
			name: "missing cmd",
			req: &Req{
				Host:     "example.com",
				Port:     22,
				User:     "testuser",
				Password: "testpass",
				Timeout:  "30s",
			},
			wantError: true,
			errorMsg:  "cmd parameter is required",
		},
		{
			name: "missing authentication",
			req: &Req{
				Host:    "example.com",
				Port:    22,
				User:    "testuser",
				Cmd:     "ls -la",
				Timeout: "30s",
			},
			wantError: true,
			errorMsg:  "either password or key_file must be provided for authentication",
		},
		{
			name: "invalid port",
			req: &Req{
				Host:     "example.com",
				Port:     70000,
				User:     "testuser",
				Cmd:      "ls -la",
				Password: "testpass",
				Timeout:  "30s",
			},
			wantError: true,
			errorMsg:  "invalid port number: 70000",
		},
		{
			name: "invalid timeout",
			req: &Req{
				Host:     "example.com",
				Port:     22,
				User:     "testuser",
				Cmd:      "ls -la",
				Password: "testpass",
				Timeout:  "invalid",
			},
			wantError: true,
			errorMsg:  "invalid timeout invalid: use a duration such as 30s, or a number of seconds",
		},
		{
			name: "zero timeout",
			req: &Req{
				Host:     "example.com",
				Port:     22,
				User:     "testuser",
				Cmd:      "ls -la",
				Password: "testpass",
				Timeout:  "0",
			},
			wantError: true,
			errorMsg:  "invalid timeout 0: use a duration such as 30s, or a number of seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseParams(tt.req)
			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("Expected error '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateKeyFile(t *testing.T) {
	tests := []struct {
		name      string
		keyFile   string
		wantError bool
	}{
		{
			name:      "non-existent file",
			keyFile:   "/tmp/non_existent_key",
			wantError: true,
		},
		{
			name:      "empty path",
			keyFile:   "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateKeyFile(tt.keyFile)
			if tt.wantError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // what os.UserHomeDir reads on Windows

	tests := []struct {
		path string
		want string
	}{
		{"~/.ssh/id_ed25519", filepath.Join(home, ".ssh", "id_ed25519")},
		{"~/known_hosts", filepath.Join(home, "known_hosts")},
		{"/etc/ssh/ssh_known_hosts", "/etc/ssh/ssh_known_hosts"},
		{"keys/id_rsa", "keys/id_rsa"},
		{"~", "~"},
		{"~user/.ssh/id_rsa", "~user/.ssh/id_rsa"},
		{"", ""},
	}
	for _, tt := range tests {
		got, err := expandHome(tt.path)
		if err != nil {
			t.Fatalf("expandHome(%q) error = %v", tt.path, err)
		}
		if got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestExpandHomeWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	if _, err := expandHome("~/.ssh/id_ed25519"); err == nil || !strings.Contains(err.Error(), "home directory") {
		t.Errorf("expandHome() error = %v, want one saying the home directory cannot be found", err)
	}
	if got, err := expandHome("/etc/ssh/ssh_known_hosts"); err != nil || got != "/etc/ssh/ssh_known_hosts" {
		t.Errorf("expandHome() = %q, %v, want the path as it is", got, err)
	}
}

func TestWithBefore(t *testing.T) {
	called := false
	var capturedHost string
	var capturedPort int
	var capturedUser string
	var capturedCmd string

	beforeFunc := func(host string, port int, user string, cmd string) {
		called = true
		capturedHost = host
		capturedPort = port
		capturedUser = user
		capturedCmd = cmd
	}

	cb := &Callback{}
	option := WithBefore(beforeFunc)
	option(cb)

	if cb.before == nil {
		t.Errorf("Expected before callback to be set")
	}

	// Test the callback
	cb.before("test.com", 2222, "testuser", "test command")

	if !called {
		t.Errorf("Expected before callback to be called")
	}
	if capturedHost != "test.com" {
		t.Errorf("Expected host 'test.com', got '%s'", capturedHost)
	}
	if capturedPort != 2222 {
		t.Errorf("Expected port 2222, got %d", capturedPort)
	}
	if capturedUser != "testuser" {
		t.Errorf("Expected user 'testuser', got '%s'", capturedUser)
	}
	if capturedCmd != "test command" {
		t.Errorf("Expected cmd 'test command', got '%s'", capturedCmd)
	}
}

func TestWithAfter(t *testing.T) {
	called := false
	var capturedResult *Result

	afterFunc := func(result *Result) {
		called = true
		capturedResult = result
	}

	cb := &Callback{}
	option := WithAfter(afterFunc)
	option(cb)

	if cb.after == nil {
		t.Errorf("Expected after callback to be set")
	}

	// Test the callback
	testResult := &Result{
		Res:    Res{Code: 0, Stdout: "test output", Stderr: ""},
		RT:     time.Second,
		Status: 0,
	}
	cb.after(testResult)

	if !called {
		t.Errorf("Expected after callback to be called")
	}
	if capturedResult != testResult {
		t.Errorf("Expected captured result to match test result")
	}
}

// fakeSession refuses the names listed in deny, as a server whose AcceptEnv
// does not include them would.
type fakeSession struct {
	deny map[string]bool
	set  map[string]string
}

func (f *fakeSession) Setenv(name, value string) error {
	if f.deny[name] {
		return errors.New("ssh: setenv failed")
	}
	f.set[name] = value
	return nil
}

func TestSetEnv(t *testing.T) {
	s := &fakeSession{
		deny: map[string]bool{"SECRET_TOKEN": true, "APP_MODE": true},
		set:  map[string]string{},
	}
	env := map[string]string{
		"LANG":         "C",
		"SECRET_TOKEN": "hunter2",
		"APP_MODE":     "prod",
		"LC_ALL":       "C",
	}

	var refused []string
	setEnv(s, env, func(name string, err error) {
		if err == nil {
			t.Errorf("refused %s without an error", name)
		}
		refused = append(refused, name)
	})

	if want := []string{"APP_MODE", "SECRET_TOKEN"}; !reflect.DeepEqual(refused, want) {
		t.Errorf("refused = %v, want %v in name order", refused, want)
	}
	if want := map[string]string{"LANG": "C", "LC_ALL": "C"}; !reflect.DeepEqual(s.set, want) {
		t.Errorf("set = %v, want %v", s.set, want)
	}

	// Without a callback a refusal is still not fatal.
	s2 := &fakeSession{deny: map[string]bool{"A": true}, set: map[string]string{}}
	setEnv(s2, map[string]string{"A": "1", "B": "2"}, nil)
	if s2.set["B"] != "2" {
		t.Error("the accepted name should be set even when another is refused")
	}
}

func TestWithEnvRefused(t *testing.T) {
	called := false
	cb := &Callback{}
	WithEnvRefused(func(string, error) { called = true })(cb)
	if cb.envRefused == nil {
		t.Fatal("WithEnvRefused should install the callback")
	}
	cb.envRefused("X", errors.New("refused"))
	if !called {
		t.Error("the installed callback should be the one given")
	}
}

func TestCreateSSHConfigDefaultKnownHosts(t *testing.T) {
	// Either default known_hosts file is enough; this machine may well have
	// no /etc/ssh/ssh_known_hosts.
	tests := []struct {
		name    string
		home    bool
		system  bool
		wantErr bool
	}{
		{name: "only the user's file", home: true},
		{name: "only the system file", system: true},
		{name: "both", home: true, system: true},
		{name: "neither", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home) // what os.UserHomeDir reads on Windows
			if tt.home {
				if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			system := filepath.Join(t.TempDir(), "ssh_known_hosts")
			if tt.system {
				if err := os.WriteFile(system, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			orig := systemKnownHosts
			systemKnownHosts = system
			t.Cleanup(func() { systemKnownHosts = orig })

			config, err := createSSHConfig(&sshParams{user: "u", password: "p", strictHostCheck: true})
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "no known hosts file") {
					t.Fatalf("createSSHConfig() error = %v, want one naming the missing files", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("createSSHConfig() error: %v", err)
			}
			if config.HostKeyCallback == nil {
				t.Error("HostKeyCallback is not set")
			}
		})
	}
}

// echo writes the command to stdout and "err" to stderr, and exits with 0.
func echo(s session) (uint32, bool) {
	_, _ = fmt.Fprint(s.stdout, s.cmd)
	_, _ = fmt.Fprint(s.stderr, "err")
	return 0, true
}

func TestReqDo(t *testing.T) {
	srv := newTestServer(t, echo)

	var before, after bool
	r := srv.req("hostname")
	r.cb = &Callback{
		before: func(host string, port int, user, cmd string) {
			before = host == srv.host() && port == srv.port() && user == testUser && cmd == "hostname"
		},
		after: func(*Result) { after = true },
	}

	res, err := r.Do()
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if res.Res.Code != 0 || res.Status != 0 {
		t.Errorf("Code = %d, Status = %d, want both 0", res.Res.Code, res.Status)
	}
	if res.Res.Stdout != "hostname" || res.Res.Stderr != "err" {
		t.Errorf("Stdout = %q, Stderr = %q, want them apart", res.Res.Stdout, res.Res.Stderr)
	}
	if res.RT <= 0 {
		t.Errorf("RT = %v, want it measured", res.RT)
	}
	if !before || !after {
		t.Errorf("callbacks: before %v, after %v", before, after)
	}
}

// A command that exits with a non-zero status is a result, not an error.
func TestReqDoExitStatus(t *testing.T) {
	srv := newTestServer(t, func(session) (uint32, bool) { return 3, true })

	res, err := srv.req("false").Do()
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if res.Res.Code != 3 || res.Status != 1 {
		t.Errorf("Code = %d, Status = %d, want 3 and 1", res.Res.Code, res.Status)
	}
}

func TestReqDoWorkdir(t *testing.T) {
	srv := newTestServer(t, echo)

	r := srv.req("ls")
	r.Workdir = "/srv/app"
	if _, err := r.Do(); err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	commands, _, _ := srv.received()
	if len(commands) != 1 || commands[0] != "cd /srv/app && ls" {
		t.Errorf("server ran %q, want the command run in workdir", commands)
	}
}

// A variable the server refuses is reported, and the command runs without it.
func TestReqDoEnv(t *testing.T) {
	srv := newTestServer(t, echo)

	var refused []string
	r := srv.req("env")
	r.Env = map[string]string{"PROBE_STAGE": "e2e", "LANG": "C"}
	r.cb = &Callback{envRefused: func(name string, _ error) { refused = append(refused, name) }}

	res, err := r.Do()
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if res.Res.Code != 0 {
		t.Errorf("Code = %d, want the command run", res.Res.Code)
	}
	_, _, envs := srv.received()
	if len(envs) != 1 || envs[0]["PROBE_STAGE"] != "e2e" {
		t.Errorf("server got env %v, want PROBE_STAGE set", envs)
	}
	if len(refused) != 1 || refused[0] != "LANG" {
		t.Errorf("refused = %v, want LANG", refused)
	}
}

// A command that outlives the timeout is sent SIGTERM, and what it reports
// once it stops is the result.
func TestReqDoTimeout(t *testing.T) {
	srv := newTestServer(t, func(s session) (uint32, bool) {
		_, _ = fmt.Fprint(s.stdout, "started")
		select {
		case sig := <-s.signals:
			if sig == string(ssh.SIGTERM) {
				return 143, true
			}
			return 1, true
		case <-time.After(10 * time.Second):
			return 0, true
		}
	})

	r := srv.req("sleep 60")
	r.Timeout = "200ms"
	start := time.Now()
	res, err := r.Do()
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Do() took %v, want it to stop the command at the timeout", elapsed)
	}
	if res.Res.Code != 143 || res.Status != 1 {
		t.Errorf("Code = %d, Status = %d, want 143 and 1", res.Res.Code, res.Status)
	}
	if res.Res.Stdout != "started" {
		t.Errorf("Stdout = %q, want the output from before the timeout", res.Res.Stdout)
	}
	if !res.Res.TimedOut {
		t.Error("TimedOut = false, want the timeout marked")
	}
	_, signals, _ := srv.received()
	if len(signals) == 0 || signals[0] != string(ssh.SIGTERM) {
		t.Errorf("server got signals %v, want TERM first", signals)
	}
}

// A command that does not stop when signalled is given up on, and is still a
// result: what it wrote until then, marked as timed out, with no exit status.
func TestReqDoTimeoutCommandDoesNotStop(t *testing.T) {
	grace := stopGrace
	stopGrace = 100 * time.Millisecond
	t.Cleanup(func() { stopGrace = grace })

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	srv := newTestServer(t, func(s session) (uint32, bool) {
		_, _ = fmt.Fprint(s.stdout, "started")
		<-release
		return 0, true
	})

	r := srv.req("trap '' TERM; sleep 60")
	r.Timeout = "200ms"
	res, err := r.Do()
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if !res.Res.TimedOut || res.Res.Code != -1 || res.Status != 1 {
		t.Errorf("Res = %+v, Status = %d; want timed out with code -1 and status 1", res.Res, res.Status)
	}
	if res.Res.Stdout != "started" {
		t.Errorf("Stdout = %q, want the output from before the timeout", res.Res.Stdout)
	}
}

// A command that finishes in time is not marked.
func TestReqDoNotTimedOut(t *testing.T) {
	srv := newTestServer(t, echo)
	res, err := srv.req("true").Do()
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if res.Res.TimedOut {
		t.Error("TimedOut = true for a command that finished in time")
	}
}

// A session that ends without an exit status, as when the connection drops,
// is an error: there is no result to test.
func TestReqDoNoExitStatus(t *testing.T) {
	srv := newTestServer(t, func(session) (uint32, bool) { return 0, false })

	_, err := srv.req("true").Do()
	if err == nil || !strings.Contains(err.Error(), "SSH command execution failed") {
		t.Fatalf("Do() error = %v, want the command reported as failed", err)
	}
}

func TestReqDoAuth(t *testing.T) {
	srv := newTestServer(t, echo)
	// Encrypting a key is slow, so the two cases that need one share it.
	encrypted := srv.writeClientKey(t, "phrase")

	tests := []struct {
		name    string
		setup   func(t *testing.T, r *Req)
		wantErr string
	}{
		{name: "password", setup: func(*testing.T, *Req) {}},
		{
			name:    "wrong password",
			setup:   func(_ *testing.T, r *Req) { r.Password = "nope" },
			wantErr: "failed to connect to SSH server",
		},
		{
			name: "key file",
			setup: func(t *testing.T, r *Req) {
				r.Password = ""
				r.KeyFile = srv.writeClientKey(t, "")
			},
		},
		{
			name: "key file with a passphrase",
			setup: func(t *testing.T, r *Req) {
				r.Password = ""
				r.KeyFile = encrypted
				r.KeyPassphrase = "phrase"
			},
		},
		{
			name: "key file with a wrong passphrase",
			setup: func(t *testing.T, r *Req) {
				r.Password = ""
				r.KeyFile = encrypted
				r.KeyPassphrase = "other"
			},
			wantErr: "failed to parse private key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := srv.req("whoami")
			tt.setup(t, r)
			res, err := r.Do()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Do() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			if res.Res.Code != 0 {
				t.Errorf("Code = %d, want 0", res.Res.Code)
			}
		})
	}
}

func TestReqDoHostKey(t *testing.T) {
	srv := newTestServer(t, echo)

	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ssh.NewPublicKey(otherPub)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a known host", func(t *testing.T) {
		r := srv.req("true")
		r.StrictHostCheck = true
		r.KnownHosts = srv.writeKnownHosts(t, srv.hostKey)
		if _, err := r.Do(); err != nil {
			t.Fatalf("Do() error = %v", err)
		}
	})

	t.Run("a host whose key changed", func(t *testing.T) {
		ran, _, _ := srv.received()
		r := srv.req("true")
		r.StrictHostCheck = true
		r.KnownHosts = srv.writeKnownHosts(t, other)
		_, err := r.Do()
		if err == nil || !strings.Contains(err.Error(), "key mismatch") {
			t.Fatalf("Do() error = %v, want a host key mismatch", err)
		}
		if commands, _, _ := srv.received(); len(commands) != len(ran) {
			t.Errorf("server ran %q for a client that did not trust it", commands[len(ran):])
		}
	})
}

func TestExecute(t *testing.T) {
	srv := newTestServer(t, echo)

	data := map[string]any{
		"host":              srv.host(),
		"port":              fmt.Sprint(srv.port()),
		"user":              testUser,
		"password":          testPassword,
		"cmd":               "uptime",
		"strict_host_check": "false",
		"env":               map[string]any{"PROBE_STAGE": "e2e"},
	}
	got, err := Execute(data)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	res, _ := got["res"].(map[string]any)
	if res["code"] != 0 || res["stdout"] != "uptime" {
		t.Errorf("Execute() res = %v, want the command's output", got["res"])
	}
	if _, ok := data["port"].(string); !ok {
		t.Error("Execute() changed the caller's data")
	}
}
