package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/mozership/probe/mapping"
)

// NOTE: SSH config file support is intentionally not implemented to maintain
// portability and reproducibility. All SSH connection parameters must be
// explicitly defined in the workflow configuration to ensure that workflows
// are self-contained and can be reliably reproduced across different environments.

type Req struct {
	Host            string            `map:"host" validate:"required"`
	Port            int               `map:"port"`
	User            string            `map:"user" validate:"required"`
	Cmd             string            `map:"cmd" validate:"required"`
	Password        string            `map:"password"`
	KeyFile         string            `map:"key_file"`
	KeyPassphrase   string            `map:"key_passphrase"`
	Timeout         string            `map:"timeout"`
	Workdir         string            `map:"workdir"`
	Env             map[string]string `map:"env"`
	StrictHostCheck bool              `map:"strict_host_check"`
	KnownHosts      string            `map:"known_hosts"`
	cb              *Callback
}

type Res struct {
	Code   int    `map:"code"`
	Stdout string `map:"stdout"`
	Stderr string `map:"stderr"`
	// TimedOut is true when the command was stopped at the timeout. Code is
	// then the status it exited with once signalled, or -1 when it did not
	// stop, and Stdout and Stderr hold what it wrote until then.
	TimedOut bool `map:"timed_out"`
}

// stopGrace is how long a command signalled at its timeout has to stop
// before it is given up on.
var stopGrace = 5 * time.Second

type Result struct {
	Req    Req           `map:"req"`
	Res    Res           `map:"res"`
	RT     time.Duration `map:"rt"`
	Status int           `map:"status"`
}

type sshParams struct {
	host            string
	port            int
	user            string
	cmd             string
	password        string
	keyFile         string
	keyPassphrase   string
	timeout         time.Duration
	workdir         string
	env             map[string]string
	strictHostCheck bool
	knownHosts      string
}

type Option func(*Callback)

type Callback struct {
	before     func(host string, port int, user string, cmd string)
	after      func(result *Result)
	envRefused func(name string, err error)
}

func NewReq() *Req {
	return &Req{
		Port:            22,
		Timeout:         "30s",
		Env:             make(map[string]string),
		StrictHostCheck: true,
	}
}

func parseParams(req *Req) (*sshParams, error) {
	params := &sshParams{
		host:            req.Host,
		port:            req.Port,
		user:            req.User,
		cmd:             req.Cmd,
		password:        req.Password,
		keyFile:         req.KeyFile,
		keyPassphrase:   req.KeyPassphrase,
		workdir:         req.Workdir,
		env:             req.Env,
		strictHostCheck: req.StrictHostCheck,
		knownHosts:      req.KnownHosts,
	}

	// Validate required parameters
	if params.host == "" {
		return nil, fmt.Errorf("host parameter is required")
	}
	if params.user == "" {
		return nil, fmt.Errorf("user parameter is required")
	}
	if params.cmd == "" {
		return nil, fmt.Errorf("cmd parameter is required")
	}

	// Set default port
	if params.port <= 0 {
		params.port = 22
	}

	// Validate port range
	if params.port < 1 || params.port > 65535 {
		return nil, fmt.Errorf("invalid port number: %d", params.port)
	}

	// Parse timeout
	timeoutStr := req.Timeout
	if timeoutStr == "" {
		timeoutStr = "30s"
	}
	timeout, err := mapping.ParseTimeout(timeoutStr)
	if err != nil {
		return nil, err
	}
	params.timeout = timeout

	// Validate authentication method
	if params.password == "" && params.keyFile == "" {
		return nil, fmt.Errorf("either password or key_file must be provided for authentication")
	}

	// Validate key file if provided
	if params.keyFile != "" {
		if err := validateKeyFile(params.keyFile); err != nil {
			return nil, err
		}
	}

	// Validate known hosts file if provided
	if params.knownHosts != "" {
		if err := validateKnownHostsFile(params.knownHosts); err != nil {
			return nil, err
		}
	}

	return params, nil
}

// expandHome returns path with a leading ~/ taken as the home directory of
// the user, as a shell reads it. Any other path is returned as it is.
func expandHome(path string) (string, error) {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return path, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, rest), nil
}

func validateKeyFile(keyFile string) error {
	keyFile, err := expandHome(keyFile)
	if err != nil {
		return err
	}

	// Check if key file exists and is readable
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		return fmt.Errorf("key file does not exist: %s", keyFile)
	}

	return nil
}

func validateKnownHostsFile(knownHostsFile string) error {
	knownHostsFile, err := expandHome(knownHostsFile)
	if err != nil {
		return err
	}

	// Check if known hosts file exists and is readable
	if _, err := os.Stat(knownHostsFile); os.IsNotExist(err) {
		return fmt.Errorf("known hosts file does not exist: %s", knownHostsFile)
	}

	return nil
}

// systemKnownHosts is the machine-wide known_hosts file, replaced in tests.
var systemKnownHosts = "/etc/ssh/ssh_known_hosts"

// existingFiles returns those of the default known_hosts files that exist.
// Either is enough, as with OpenSSH; only having neither is an error, since
// no host could then be verified.
func existingFiles(paths ...string) ([]string, error) {
	var found []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to read known hosts file: %w", err)
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no known hosts file: %s missing; set known_hosts, or strict_host_check: false to skip the check", strings.Join(paths, " and "))
	}
	return found, nil
}

func createSSHConfig(params *sshParams) (*ssh.ClientConfig, error) {
	config := &ssh.ClientConfig{
		User:    params.user,
		Timeout: params.timeout,
	}

	// Configure authentication
	var authMethods []ssh.AuthMethod

	// Password authentication
	if params.password != "" {
		authMethods = append(authMethods, ssh.Password(params.password))
	}

	// Key-based authentication
	if params.keyFile != "" {
		keyFile, err := expandHome(params.keyFile)
		if err != nil {
			return nil, err
		}

		key, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read private key file: %w", err)
		}

		var signer ssh.Signer
		if params.keyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(params.keyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}

		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	config.Auth = authMethods

	// Configure host key verification
	if params.strictHostCheck {
		if params.knownHosts != "" {
			// Use custom known hosts file
			knownHostsFile, err := expandHome(params.knownHosts)
			if err != nil {
				return nil, err
			}

			hostKeyCallback, err := knownhosts.New(knownHostsFile)
			if err != nil {
				return nil, fmt.Errorf("failed to create known hosts callback: %w", err)
			}
			config.HostKeyCallback = hostKeyCallback
		} else {
			// Use default known hosts files
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get home directory: %w", err)
			}

			files, err := existingFiles(filepath.Join(homeDir, ".ssh", "known_hosts"), systemKnownHosts)
			if err != nil {
				return nil, err
			}
			hostKeyCallback, err := knownhosts.New(files...)
			if err != nil {
				return nil, fmt.Errorf("failed to create known hosts callback: %w", err)
			}
			config.HostKeyCallback = hostKeyCallback
		}
	} else {
		// Skip host key verification (not recommended for production)
		config.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	return config, nil
}

func (r *Req) Do() (re *Result, er error) {
	params, err := parseParams(r)
	if err != nil {
		return nil, err
	}

	result := &Result{Req: *r}

	// callback before
	if r.cb != nil && r.cb.before != nil {
		r.cb.before(params.host, params.port, params.user, params.cmd)
	}

	start := time.Now()

	// Create SSH configuration
	config, err := createSSHConfig(params)
	if err != nil {
		return result, fmt.Errorf("failed to create SSH config: %w", err)
	}

	// Connect to SSH server
	address := net.JoinHostPort(params.host, strconv.Itoa(params.port))
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return result, fmt.Errorf("failed to connect to SSH server: %w", err)
	}
	defer func() {
		err := client.Close()
		if er == nil {
			er = err
		}
	}()

	// Create SSH session
	session, err := client.NewSession()
	if err != nil {
		return result, fmt.Errorf("failed to create SSH session: %w", err)
	}
	// Note: We will explicitly close the session after command completion
	// instead of using defer to ensure proper cleanup timing

	// Set environment variables. A server accepts only the names its
	// AcceptEnv allows; a refused one is reported and the command still runs.
	var refused func(string, error)
	if r.cb != nil {
		refused = r.cb.envRefused
	}
	setEnv(session, params.env, refused)

	// Prepare command with working directory if specified
	cmd := params.cmd
	if params.workdir != "" {
		cmd = fmt.Sprintf("cd %s && %s", params.workdir, params.cmd)
	}

	// Setup pipes for stdout and stderr to capture output separately
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return result, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return result, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := session.Start(cmd); err != nil {
		return result, fmt.Errorf("failed to start command: %w", err)
	}

	// Create context with timeout for command execution
	ctx, cancel := context.WithTimeout(context.Background(), params.timeout)
	defer cancel()

	// Read stdout and stderr concurrently
	stdoutChan := make(chan []byte, 1)
	stderrChan := make(chan []byte, 1)
	waitChan := make(chan error, 1)

	go func() {
		defer close(stdoutChan)
		output, _ := io.ReadAll(stdoutPipe)
		stdoutChan <- output
	}()

	go func() {
		defer close(stderrChan)
		output, _ := io.ReadAll(stderrPipe)
		stderrChan <- output
	}()

	// Wait for command completion in a goroutine
	go func() {
		defer close(waitChan)
		waitChan <- session.Wait()
	}()

	// Wait for either command completion or timeout
	var cmdErr error
	timedOut, stopped := false, true
	select {
	case cmdErr = <-waitChan:
		// Command completed normally
	case <-ctx.Done():
		// select picks at random when both are ready, so a command that
		// finished as the deadline passed is taken as finished.
		select {
		case cmdErr = <-waitChan:
			break
		default:
			timedOut = true
			// Try to signal the session to stop
			if signalErr := session.Signal(ssh.SIGTERM); signalErr != nil {
				// If SIGTERM fails, try SIGKILL
				_ = session.Signal(ssh.SIGKILL)
			}
			// Wait a bit for graceful termination, then proceed
			select {
			case cmdErr = <-waitChan:
				// Command terminated after signal
			case <-time.After(stopGrace):
				// The command did not stop: give up on it.
				stopped = false
			}
		}
	}

	result.RT = time.Since(start)

	// A command that did not stop still holds its output open; closing the
	// session ends the reads with what it wrote so far.
	if !stopped {
		_ = session.Close()
	}

	// Collect output (may still be available even if command failed/timed out)
	var stdoutBytes, stderrBytes []byte
	select {
	case stdoutBytes = <-stdoutChan:
	case <-time.After(1 * time.Second):
		stdoutBytes = []byte{}
	}

	select {
	case stderrBytes = <-stderrChan:
	case <-time.After(1 * time.Second):
		stderrBytes = []byte{}
	}

	stdout := string(stdoutBytes)
	stderr := string(stderrBytes)

	// Get exit code
	exitCode := 0
	var exitError *ssh.ExitError
	switch {
	case !stopped:
		exitCode = -1
	case errors.As(cmdErr, &exitError):
		exitCode = exitError.ExitStatus()
	case cmdErr != nil:
		// Connection or other error
		return result, fmt.Errorf("SSH command execution failed: %w", cmdErr)
	}

	// Determine status based on exit code (0 = success, 1 = failure)
	status := 1 // default to failure
	if exitCode == 0 {
		status = 0 // success
	}

	result.Res = Res{
		Code:     exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
		TimedOut: timedOut,
	}
	result.Status = status

	_ = session.Close()

	// callback after
	if r.cb != nil && r.cb.after != nil {
		r.cb.after(result)
	}

	return result, nil
}

func Execute(data map[string]any, opts ...Option) (map[string]any, error) {
	// EnvToStringValue copies data, so the caller's map is left as it was.
	m := mapping.HeaderToStringValue(mapping.EnvToStringValue(data))

	// Manually handle type conversions BEFORE MapToStructByTags to prevent reflection panics
	if portInput, exists := m["port"]; exists {
		if portStr, ok := portInput.(string); ok {
			if portInt, err := strconv.Atoi(portStr); err == nil {
				m["port"] = portInt
			}
		}
	}

	if strictInput, exists := m["strict_host_check"]; exists {
		if strictStr, ok := strictInput.(string); ok {
			if strictBool, err := strconv.ParseBool(strictStr); err == nil {
				m["strict_host_check"] = strictBool
			}
		}
	}

	r := NewReq()

	cb := &Callback{}
	for _, opt := range opts {
		opt(cb)
	}
	r.cb = cb

	if err := mapping.MapToStructByTags(m, r); err != nil {
		return map[string]any{}, err
	}

	result, err := r.Do()
	if err != nil {
		return map[string]any{}, err
	}

	mapResult, err := mapping.StructToMapByTags(result)
	if err != nil {
		return map[string]any{}, err
	}

	return mapResult, nil
}

func WithBefore(f func(host string, port int, user string, cmd string)) Option {
	return func(c *Callback) {
		c.before = f
	}
}

func WithAfter(f func(result *Result)) Option {
	return func(c *Callback) {
		c.after = f
	}
}

// WithEnvRefused is called for each environment variable the server refuses
// to set, typically because sshd_config does not list it in AcceptEnv. The
// command still runs, without that variable.
func WithEnvRefused(f func(name string, err error)) Option {
	return func(c *Callback) {
		c.envRefused = f
	}
}

// envSetter is the part of an SSH session that sets environment variables.
type envSetter interface {
	Setenv(name, value string) error
}

// setEnv sets each variable on the session in name order, and reports every
// name the server refuses through refused, which may be nil.
func setEnv(s envSetter, env map[string]string, refused func(name string, err error)) {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := s.Setenv(name, env[name]); err != nil && refused != nil {
			refused(name, err)
		}
	}
}
