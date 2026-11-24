package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestNewReq(t *testing.T) {
	got := NewReq()

	expected := &Req{
		Cmd:        "",
		Shell:      "/bin/sh",
		Workdir:    "",
		Timeout:    "30s",
		Env:        map[string]string{},
		Background: false,
	}

	if !reflect.DeepEqual(got, expected) {
		t.Errorf("\nExpected:\n%#v\nGot:\n%#v", expected, got)
	}
}

func TestValidateShellPath(t *testing.T) {
	tests := []struct {
		name    string
		shell   string
		wantErr bool
	}{
		{"valid /bin/sh", "/bin/sh", false},
		{"valid /bin/bash", "/bin/bash", false},
		{"valid /bin/zsh", "/bin/zsh", false},
		{"valid /usr/bin/bash", "/usr/bin/bash", false},
		{"invalid shell", "/usr/local/bin/fish", true},
		{"empty shell", "", true},
		{"relative path", "bash", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShellPath(tt.shell)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateShellPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateWorkdir(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "workdir_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to cleanup temp dir: %v", err)
		}
	}()

	// Get current working directory
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	tests := []struct {
		name    string
		workdir string
		wantErr bool
	}{
		{"absolute path exists", tempDir, false},
		{"absolute path not exists", "/nonexistent/path", true},
		{"relative path exists", ".", false},
		{"relative path not exists", "nonexistent", true},
		{"current directory", wd, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWorkdir(tt.workdir)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateWorkdir() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseParamsTimeout(t *testing.T) {
	tests := []struct {
		name        string
		timeout     string
		expected    time.Duration
		expectError bool
	}{
		{"empty is the default", "", 30 * time.Second, false},
		{"plain seconds", "30", 30 * time.Second, false},
		{"fractional seconds", "1.5", 1500 * time.Millisecond, false},
		{"duration format", "30s", 30 * time.Second, false},
		{"minutes", "5m", 5 * time.Minute, false},
		{"hours", "1h", 1 * time.Hour, false},
		{"invalid format", "invalid", 0, true},
		{"zero", "0", 0, true},
		{"zero duration", "0s", 0, true},
		{"negative", "-1s", 0, true},
		{"too many seconds for a duration", "18446744074", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := parseParams(&Req{Cmd: "true", Shell: "/bin/sh", Timeout: tt.timeout})
			if tt.expectError {
				if err == nil {
					t.Errorf("parseParams() timeout %q = %v, want an error", tt.timeout, params.timeout)
				} else if !strings.Contains(err.Error(), "invalid timeout") {
					t.Errorf("parseParams() error = %v, want it to name the timeout", err)
				}
				return
			}

			if err != nil {
				t.Errorf("parseParams() unexpected error: %v", err)
				return
			}

			if params.timeout != tt.expected {
				t.Errorf("parseParams() timeout = %v, want %v", params.timeout, tt.expected)
			}
		})
	}
}

func TestDo(t *testing.T) {
	tests := []struct {
		name        string
		req         *Req
		expectError bool
		checkOutput bool
	}{
		{
			name: "simple echo command",
			req: &Req{
				Cmd:     "echo 'Hello World'",
				Shell:   "/bin/sh",
				Timeout: "5s",
			},
			expectError: false,
			checkOutput: true,
		},
		{
			name: "command with exit code 1",
			req: &Req{
				Cmd:     "exit 1",
				Shell:   "/bin/sh",
				Timeout: "5s",
			},
			expectError: false, // Exit code 1 should not cause error, just status = 1
			checkOutput: false,
		},
		{
			name: "empty command",
			req: &Req{
				Cmd:   "",
				Shell: "/bin/sh",
			},
			expectError: true,
			checkOutput: false,
		},
		{
			name: "command with relative workdir",
			req: &Req{
				Cmd:     "pwd",
				Shell:   "/bin/sh",
				Workdir: ".",
				Timeout: "5s",
			},
			expectError: false,
			checkOutput: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track if callbacks were called
			beforeCalled := false
			afterCalled := false

			// Set up callbacks for testing
			tt.req.cb = &Callback{
				before: func(cmd string, shell string, workdir string) {
					beforeCalled = true
				},
				after: func(result *Result) {
					afterCalled = true
				},
			}

			result, err := tt.req.Do()
			if result != nil {
				cleanupBackground(t, result.Res.PID, result.Res.Log)
			}

			if tt.expectError {
				if err == nil {
					t.Errorf("Do() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Do() unexpected error: %v", err)
				return
			}

			// Verify callbacks were called
			if !beforeCalled {
				t.Error("before callback was not called")
			}
			if !afterCalled {
				t.Error("after callback was not called")
			}

			// Check basic result structure
			if result == nil {
				t.Error("Do() returned nil result")
				return
			}

			// Check that RT field is populated
			if result.RT <= 0 {
				t.Errorf("RT should be greater than 0, got: %v", result.RT)
			}

			// Check request fields are preserved
			if result.Req.Cmd != tt.req.Cmd {
				t.Errorf("Req.Cmd = %v, want %v", result.Req.Cmd, tt.req.Cmd)
			}

			if tt.checkOutput {
				// Different checks based on command type
				if tt.req.Cmd == "echo 'Hello World'" {
					// For echo command, stdout should contain "Hello World"
					if !strings.Contains(result.Res.Stdout, "Hello World") {
						t.Errorf("Expected stdout to contain 'Hello World', got: %s", result.Res.Stdout)
					}
				} else if tt.req.Cmd == "pwd" && tt.req.Workdir == "." {
					// For pwd command with relative workdir, should output current directory
					expectedPath, _ := filepath.Abs(tt.req.Workdir)
					if !strings.Contains(result.Res.Stdout, expectedPath) {
						t.Errorf("Expected stdout to contain current directory path '%s', got: %s", expectedPath, result.Res.Stdout)
					}
				}

				// Successful command should have status 0
				if result.Status != 0 {
					t.Errorf("Expected status 0 for successful command, got: %d", result.Status)
				}

				// Exit code should be 0
				if result.Res.Code != 0 {
					t.Errorf("Expected exit code 0, got: %d", result.Res.Code)
				}
			}
		})
	}
}

// TestExecute_Env pins that env reaches the command, in every form the
// parameters can take. env used to be turned into a string before mapping,
// which dropped it without an error.
func TestExecute_Env(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want string
	}{
		{
			name: "nested env map",
			data: map[string]any{
				"cmd": `printf '%s|%s' "$GREETING" "$PORT"`,
				"env": map[string]any{"GREETING": "hello", "PORT": 8080},
			},
			want: "hello|8080",
		},
		{
			name: "flat env__ keys",
			data: map[string]any{
				"cmd":           `printf '%s' "$TEST_VAR"`,
				"env__TEST_VAR": "hello_world",
			},
			want: "hello_world",
		},
		{
			name: "nested map wins over a flat key of the same name",
			data: map[string]any{
				"cmd":       `printf '%s' "$MODE"`,
				"env":       map[string]any{"MODE": "nested"},
				"env__MODE": "flat",
			},
			want: "nested",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Execute(tt.data)
			if err != nil {
				t.Fatalf("Execute() error: %v", err)
			}
			res, ok := result["res"].(map[string]any)
			if !ok {
				t.Fatalf("result has no res: %v", result)
			}
			if res["stdout"] != tt.want {
				t.Errorf("stdout = %q, want %q", res["stdout"], tt.want)
			}
			req, _ := result["req"].(map[string]any)
			if env, _ := req["env"].(map[string]string); len(env) == 0 {
				t.Errorf("the request should report the env it used, got %#v", req["env"])
			}
		})
	}
}

// TestExecute_KeepsParameterTypes checks that parameters other than env are
// still mapped when they arrive with their own types rather than as strings.
func TestExecute_KeepsParameterTypes(t *testing.T) {
	result, err := Execute(map[string]any{
		"cmd":     "pwd",
		"workdir": "/",
		"timeout": "5s",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	res := result["res"].(map[string]any)
	if res["stdout"] != "/\n" {
		t.Errorf("stdout = %q, want the workdir", res["stdout"])
	}

	bg, err := Execute(map[string]any{"cmd": "sleep 0", "background": true})
	if err != nil {
		t.Fatalf("Execute() background error: %v", err)
	}
	if status, _ := bg["status"].(int); status != -1 {
		t.Errorf("background status = %v, want -1", bg["status"])
	}
	if bgRes, ok := bg["res"].(map[string]any); ok {
		pid, _ := bgRes["pid"].(int)
		log, _ := bgRes["log"].(string)
		cleanupBackground(t, pid, log)
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name        string
		data        map[string]any
		expectError bool
		checkStatus bool
	}{
		{
			name: "simple command execution",
			data: map[string]any{
				"cmd":   "echo 'test output'",
				"shell": "/bin/sh",
			},
			expectError: false,
			checkStatus: true,
		},
		{
			name: "command with environment variable",
			data: map[string]any{
				"cmd":           "echo $TEST_VAR",
				"shell":         "/bin/sh",
				"env__TEST_VAR": "hello_world",
			},
			expectError: false,
			checkStatus: true,
		},
		{
			name: "missing required cmd parameter",
			data: map[string]any{
				"shell": "/bin/sh",
			},
			expectError: true,
			checkStatus: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track if callbacks were called
			beforeCalled := false
			afterCalled := false

			before := WithBefore(func(cmd string, shell string, workdir string) {
				beforeCalled = true
			})
			after := WithAfter(func(result *Result) {
				afterCalled = true
			})

			result, err := Execute(tt.data, before, after)
			if res, ok := result["res"].(map[string]any); ok {
				pid, _ := res["pid"].(int)
				log, _ := res["log"].(string)
				cleanupBackground(t, pid, log)
			}

			if tt.expectError {
				if err == nil {
					t.Errorf("Execute() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Execute() unexpected error: %v", err)
				return
			}

			// Verify callbacks were called
			if !beforeCalled {
				t.Error("before callback was not called")
			}
			if !afterCalled {
				t.Error("after callback was not called")
			}

			if result == nil {
				t.Error("Execute() returned nil result")
				return
			}

			if tt.checkStatus {
				// Check that basic fields exist in nested result structure
				if req, exists := result["req"]; exists {
					if reqMap, ok := req.(map[string]any); ok {
						if _, exists := reqMap["cmd"]; !exists {
							t.Error("Expected 'cmd' field in req")
						}
					} else {
						t.Error("Expected req to be map[string]any")
					}
				} else {
					t.Error("Expected 'req' field in result")
				}
				if res, exists := result["res"]; exists {
					if resMap, ok := res.(map[string]any); ok {
						if _, exists := resMap["code"]; !exists {
							t.Error("Expected 'code' field in res")
						}
					} else {
						t.Error("Expected res to be map[string]any")
					}
				} else {
					t.Error("Expected 'res' field in result")
				}
				if _, exists := result["status"]; !exists {
					t.Error("Expected 'status' field in result")
				}
			}
		})
	}
}

func TestWithBefore(t *testing.T) {
	called := false
	var capturedCmd, capturedShell, capturedWorkdir string

	option := WithBefore(func(cmd string, shell string, workdir string) {
		called = true
		capturedCmd = cmd
		capturedShell = shell
		capturedWorkdir = workdir
	})

	cb := &Callback{}
	option(cb)

	if cb.before == nil {
		t.Error("WithBefore() did not set before callback")
		return
	}

	// Test the callback
	cb.before("test-cmd", "/bin/bash", "/tmp")

	if !called {
		t.Error("before callback was not called")
	}
	if capturedCmd != "test-cmd" {
		t.Errorf("Expected cmd 'test-cmd', got '%s'", capturedCmd)
	}
	if capturedShell != "/bin/bash" {
		t.Errorf("Expected shell '/bin/bash', got '%s'", capturedShell)
	}
	if capturedWorkdir != "/tmp" {
		t.Errorf("Expected workdir '/tmp', got '%s'", capturedWorkdir)
	}
}

func TestWithAfter(t *testing.T) {
	called := false
	var capturedResult *Result

	option := WithAfter(func(result *Result) {
		called = true
		capturedResult = result
	})

	cb := &Callback{}
	option(cb)

	if cb.after == nil {
		t.Error("WithAfter() did not set after callback")
		return
	}

	// Test the callback
	testResult := &Result{
		Status: 0,
		Res: Res{
			Code:   0,
			Stdout: "test output",
			PID:    12345,
		},
	}
	cb.after(testResult)

	if !called {
		t.Error("after callback was not called")
	}
	if capturedResult != testResult {
		t.Error("after callback did not receive correct result")
	}
}

func TestDoBackground(t *testing.T) {
	tests := []struct {
		name        string
		req         *Req
		expectError bool
	}{
		{
			name: "background sleep command",
			req: &Req{
				Cmd:        "sleep 5",
				Shell:      "/bin/sh",
				Timeout:    "30s",
				Background: true,
			},
			expectError: false,
		},
		{
			name: "background echo command",
			req: &Req{
				Cmd:        "echo 'background test'",
				Shell:      "/bin/sh",
				Timeout:    "30s",
				Background: true,
			},
			expectError: false,
		},
		{
			name: "empty command background",
			req: &Req{
				Cmd:        "",
				Shell:      "/bin/sh",
				Background: true,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track if callbacks were called
			beforeCalled := false
			afterCalled := false

			// Set up callbacks for testing
			tt.req.cb = &Callback{
				before: func(cmd string, shell string, workdir string) {
					beforeCalled = true
				},
				after: func(result *Result) {
					afterCalled = true
				},
			}

			result, err := tt.req.Do()
			if result != nil {
				cleanupBackground(t, result.Res.PID, result.Res.Log)
			}

			if tt.expectError {
				if err == nil {
					t.Errorf("Do() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Do() unexpected error: %v", err)
				return
			}

			// Verify callbacks were called
			if !beforeCalled {
				t.Error("before callback was not called")
			}
			if !afterCalled {
				t.Error("after callback was not called")
			}

			// Check basic result structure
			if result == nil {
				t.Error("Do() returned nil result")
				return
			}

			// For background execution, verify specific behaviors
			if tt.req.Background {
				// Status should be -1 for background execution
				if result.Status != -1 {
					t.Errorf("Expected status -1 for background execution, got: %d", result.Status)
				}

				// Code should be -1 for background process
				if result.Res.Code != -1 {
					t.Errorf("Expected exit code -1 for background process, got: %d", result.Res.Code)
				}

				// PID should be greater than 0
				if result.Res.PID <= 0 {
					t.Errorf("Expected PID > 0 for background process, got: %d", result.Res.PID)
				}

				// Stdout and Stderr should be empty for background execution
				if result.Res.Stdout != "" {
					t.Errorf("Expected empty stdout for background execution, got: %s", result.Res.Stdout)
				}
				if result.Res.Stderr != "" {
					t.Errorf("Expected empty stderr for background execution, got: %s", result.Res.Stderr)
				}

				// RT should be very small (just startup time)
				if result.RT > time.Second {
					t.Errorf("Expected RT < 1s for background execution, got: %v", result.RT)
				}

				// Log field should be present and point to tmp directory
				if result.Res.Log == "" {
					t.Error("Expected log field to be populated for background execution")
				} else {
					// Verify log path is in tmp directory
					if !strings.HasPrefix(result.Res.Log, os.TempDir()) {
						t.Errorf("Expected log path to be in tmp directory, got: %s", result.Res.Log)
					}
					// Verify log filename format
					filename := filepath.Base(result.Res.Log)
					if !strings.HasPrefix(filename, "probe-shell-action.") || !strings.HasSuffix(filename, ".log") {
						t.Errorf("Expected log filename format 'probe-shell-action.<random>.log', got: %s", filename)
					}
				}
			}

			// Check that RT field is populated
			if result.RT <= 0 {
				t.Errorf("RT should be greater than 0, got: %v", result.RT)
			}

			// Check request fields are preserved
			if result.Req.Cmd != tt.req.Cmd {
				t.Errorf("Req.Cmd = %v, want %v", result.Req.Cmd, tt.req.Cmd)
			}
		})
	}
}

func TestExecuteBackground(t *testing.T) {
	tests := []struct {
		name        string
		data        map[string]any
		expectError bool
	}{
		{
			name: "background command execution",
			data: map[string]any{
				"cmd":        "sleep 3",
				"shell":      "/bin/sh",
				"background": "true",
			},
			expectError: false,
		},
		{
			name: "background command with bool",
			data: map[string]any{
				"cmd":        "echo 'test'",
				"shell":      "/bin/sh",
				"background": true,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track if callbacks were called
			beforeCalled := false
			afterCalled := false

			before := WithBefore(func(cmd string, shell string, workdir string) {
				beforeCalled = true
			})
			after := WithAfter(func(result *Result) {
				afterCalled = true
			})

			result, err := Execute(tt.data, before, after)
			if res, ok := result["res"].(map[string]any); ok {
				pid, _ := res["pid"].(int)
				log, _ := res["log"].(string)
				cleanupBackground(t, pid, log)
			}

			if tt.expectError {
				if err == nil {
					t.Errorf("Execute() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Execute() unexpected error: %v", err)
				return
			}

			// Verify callbacks were called
			if !beforeCalled {
				t.Error("before callback was not called")
			}
			if !afterCalled {
				t.Error("after callback was not called")
			}

			if result == nil {
				t.Error("Execute() returned nil result")
				return
			}

			// Check that it's background execution
			if status, exists := result["status"]; exists {
				if statusInt, ok := status.(int); ok && statusInt == -1 {
					// Verify PID exists in response
					if res, exists := result["res"]; exists {
						if resMap, ok := res.(map[string]any); ok {
							if pid, exists := resMap["pid"]; exists {
								if pidInt, ok := pid.(int); ok {
									if pidInt <= 0 {
										t.Errorf("Expected PID > 0 for background process, got: %d", pidInt)
									}
								} else {
									t.Error("Expected PID to be int")
								}
							} else {
								t.Error("Expected 'pid' field in res for background execution")
							}

							// Verify log field exists and is correctly formatted
							if log, exists := resMap["log"]; exists {
								if logStr, ok := log.(string); ok {
									// Verify log path is in tmp directory
									if !strings.HasPrefix(logStr, os.TempDir()) {
										t.Errorf("Expected log path to be in tmp directory, got: %s", logStr)
									}
									// Verify log filename format
									filename := filepath.Base(logStr)
									if !strings.HasPrefix(filename, "probe-shell-action.") || !strings.HasSuffix(filename, ".log") {
										t.Errorf("Expected log filename format 'probe-shell-action.<random>.log', got: %s", filename)
									}
								} else {
									t.Error("Expected log to be string")
								}
							} else {
								t.Error("Expected 'log' field in res for background execution")
							}
						}
					}
				}
			}
		})
	}
}

// cleanupBackground stops a process the test started in the background and
// removes its log, which is what the workflow does once it is over.
func cleanupBackground(t *testing.T, pid int, log string) {
	t.Helper()
	t.Cleanup(func() {
		if pid > 0 {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
		if log != "" {
			_ = os.Remove(log)
		}
	})
}

func TestDoBackgroundSeparateLogs(t *testing.T) {
	// The same command started twice must not share a log: each run's output
	// has to stay readable on its own.
	var logs []string
	for range 2 {
		req := &Req{Cmd: "echo run-$$", Shell: "/bin/sh", Timeout: "5s", Background: true}
		result, err := req.Do()
		if err != nil {
			t.Fatalf("Do() error: %v", err)
		}
		cleanupBackground(t, result.Res.PID, result.Res.Log)
		logs = append(logs, result.Res.Log)
	}

	if logs[0] == logs[1] {
		t.Fatalf("both runs wrote to %s", logs[0])
	}

	for _, log := range logs {
		var got []byte
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			got, _ = os.ReadFile(log)
			if len(got) > 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if lines := strings.Count(string(got), "\n"); lines != 1 {
			t.Errorf("%s = %q, want the output of one run", log, got)
		}
	}
}

func TestDoStdoutNotEmpty(t *testing.T) {
	// Reproduces a race condition where cmd.Wait() closes the stdout pipe
	// before the reading goroutine finishes, causing stdout to be empty.
	// This is especially likely with fast commands like "grep -c | tr -d '\n'"
	// on resource-constrained environments like GitHub Actions runners.
	// Running concurrently increases goroutine scheduling pressure to
	// make the race more likely to manifest.
	const iterations = 100
	const concurrency = 8

	var wg sync.WaitGroup
	errChan := make(chan string, iterations*concurrency)

	for c := range concurrency {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := range iterations {
				req := &Req{
					Cmd:     "echo hello | grep -c hello | tr -d '\\n'",
					Shell:   "/bin/sh",
					Timeout: "5s",
				}
				result, err := req.Do()
				if err != nil {
					errChan <- fmt.Sprintf("worker %d, iteration %d: Do() error: %v", workerID, i, err)
					return
				}
				if result.Res.Stdout == "" {
					errChan <- fmt.Sprintf("worker %d, iteration %d: expected stdout '1', got empty string", workerID, i)
					return
				}
				if result.Res.Stdout != "1" {
					errChan <- fmt.Sprintf("worker %d, iteration %d: expected stdout '1', got %q", workerID, i, result.Res.Stdout)
					return
				}
			}
		}(c)
	}

	wg.Wait()
	close(errChan)

	for msg := range errChan {
		t.Fatal(msg)
	}
}

func TestExecuteStopsOnUnreadableParameter(t *testing.T) {
	// A parameter that cannot be read fails the step before the command
	// runs; it used to run the command with the parameter at its zero value,
	// here in the foreground, and report the error only afterwards.
	// The command names a fixed file in workdir, so that no character of the
	// path can change what the shell is asked to do; the space in it is there
	// to make sure of that.
	dir := filepath.Join(t.TempDir(), "with space")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "ran")
	_, err := Execute(map[string]any{
		"cmd":        "touch ran",
		"workdir":    dir,
		"background": "maybe",
	})
	if err == nil {
		t.Fatal("Execute() succeeded with an unreadable background")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Errorf("the command ran although background could not be read: %v", statErr)
	}
}

// A command stopped at its timeout is a result marked as timed out, with what
// it wrote until then, and it is not waited on past the timeout even when a
// process it started still holds its output open.
func TestReqDo_Timeout(t *testing.T) {
	grace := outputGrace
	outputGrace = 100 * time.Millisecond
	t.Cleanup(func() { outputGrace = grace })

	r := &Req{Cmd: "echo started; sleep 30", Shell: "/bin/sh", Timeout: "300ms"}
	start := time.Now()
	res, err := r.Do()
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Do() took %v, want it to stop at the timeout", elapsed)
	}
	if !res.Res.TimedOut || res.Res.Code != -1 || res.Status != 1 {
		t.Errorf("Res = %+v, Status = %d; want timed out with code -1 and status 1", res.Res, res.Status)
	}
	if res.Res.Stdout != "started\n" {
		t.Errorf("Stdout = %q, want the output from before the timeout", res.Res.Stdout)
	}
}

// A command that exits while something it started still holds its output
// open finishes when it exits, not when that process does.
func TestReqDo_ExitsWhileChildHoldsOutput(t *testing.T) {
	grace := outputGrace
	outputGrace = 100 * time.Millisecond
	t.Cleanup(func() { outputGrace = grace })

	r := &Req{Cmd: "sleep 30 & echo done", Shell: "/bin/sh", Timeout: "20s"}
	start := time.Now()
	res, err := r.Do()
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Do() took %v, want it to return once the command exited", elapsed)
	}
	if res.Res.TimedOut || res.Res.Code != 0 || res.Res.Stdout != "done\n" {
		t.Errorf("Res = %+v, want a finished command", res.Res)
	}
}

func TestReqDo_NotTimedOut(t *testing.T) {
	res, err := (&Req{Cmd: "exit 3", Shell: "/bin/sh", Timeout: "5s"}).Do()
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if res.Res.TimedOut || res.Res.Code != 3 {
		t.Errorf("Res = %+v, want exit code 3, not timed out", res.Res)
	}
}

// A command that exited before the deadline did not time out, even when the
// deadline passes while the output a process it left running holds open is
// still being read.
func TestReqDo_DeadlinePassesWhileReadingOutput(t *testing.T) {
	grace := outputGrace
	outputGrace = time.Second
	t.Cleanup(func() { outputGrace = grace })

	for cmd, code := range map[string]int{"sleep 5 & echo done": 0, "sleep 5 & exit 4": 4} {
		t.Run(cmd, func(t *testing.T) {
			res, err := (&Req{Cmd: cmd, Shell: "/bin/sh", Timeout: "200ms"}).Do()
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			if res.Res.TimedOut || res.Res.Code != code {
				t.Errorf("Res = %+v, want code %d, not timed out", res.Res, code)
			}
		})
	}
}
