package smtp

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDialErrorNamesOnlyItsOwnTimeout(t *testing.T) {
	deadline := &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	early := &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}
	// The system gave up on its own, which net.Error also calls a timeout.
	system := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ETIMEDOUT}

	tests := []struct {
		name    string
		err     error
		timeout time.Duration
		suffix  bool
	}{
		{"the dial's deadline", deadline, time.Minute, true},
		{"a deadline gone before the dial", early, time.Minute, true},
		{"the system's ETIMEDOUT", system, time.Minute, false},
		{"no timeout", deadline, 0, false},
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, time.Minute, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dialError(tt.err, tt.timeout)
			has := strings.Contains(got.Error(), "(could not connect within")
			if has != tt.suffix {
				t.Errorf("dialError(%v, %s) = %q, want the timeout named: %v", tt.err, tt.timeout, got, tt.suffix)
			}
			if !has && got != tt.err {
				t.Errorf("dialError(%v, %s) = %v, want the error as it is", tt.err, tt.timeout, got)
			}
			if !errors.Is(got, tt.err) {
				t.Errorf("dialError(%v) does not wrap it", tt.err)
			}
		})
	}
}

func TestDialNamesTheTimeoutItRanOutOf(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lis.Close() })
	_, err = Dial(lis.Addr().String(), time.Nanosecond)
	if err == nil || !strings.Contains(err.Error(), "(could not connect within 1ns)") {
		t.Errorf("Dial() error = %v, want one naming the 1ns timeout", err)
	}
}

func TestDeadlineConnNamesOnlyItsOwnWait(t *testing.T) {
	c := &deadlineConn{timeout: time.Second}
	read := &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}
	if got := c.waited(read); !strings.Contains(got.Error(), "(the server did not answer within 1s)") || !errors.Is(got, os.ErrDeadlineExceeded) {
		t.Errorf("waited(deadline) = %v, want the 1s wait named", got)
	}
	var netErr net.Error
	if got := c.waited(read); !errors.As(got, &netErr) || !netErr.Timeout() {
		t.Errorf("waited(deadline) = %v, want a net.Error that is a timeout", got)
	}

	broken := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ETIMEDOUT}
	if got := c.waited(broken); got != error(broken) {
		t.Errorf("waited(ETIMEDOUT) = %v, want it as it is", got)
	}
	if got := c.waited(nil); got != nil {
		t.Errorf("waited(nil) = %v", got)
	}
}
