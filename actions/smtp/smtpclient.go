package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/textproto"
	"os"
	"strings"
	"time"
)

// Client is an SMTP client forked from net/smtp, keeping what the smtp action
// uses of it.
type Client struct {
	Text       *textproto.Conn
	conn       net.Conn
	tls        bool
	serverName string
	ext        map[string]string
	localName  string
	didHello   bool
	helloError error
	// deadline limits each wait on the server, and is nil without a
	// timeout.
	deadline *deadlineConn
}

// Dial connects to the SMTP server at addr and reads its greeting. A timeout
// above zero limits connecting, and each read from and write to the
// connection after, so that a server that stops answering fails the session
// rather than holding it. The reply to the end of a message may take twice
// the timeout, as RFC 5321 4.5.3.2 gives it 10 minutes against 5 for the
// other replies: a server may check the message before it answers. Zero
// means no limit.
func Dial(addr string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, dialError(err, timeout)
	}
	var deadline *deadlineConn
	if timeout > 0 {
		deadline = &deadlineConn{Conn: conn, timeout: timeout}
		conn = deadline
	}
	host, _, _ := net.SplitHostPort(addr)
	c, err := NewClient(conn, host)
	if err != nil {
		return nil, err
	}
	c.deadline = deadline
	return c, nil
}

// dialError names the timeout when connecting ran out of it. The system
// may give up first, with ETIMEDOUT after a minute or two of a host that
// does not answer, and that error is its own, so it goes as it is. Running
// out of the dial's deadline gives os.ErrDeadlineExceeded or, when it ran
// out before the connection was tried, context.DeadlineExceeded.
func dialError(err error, timeout time.Duration) error {
	if timeout > 0 && (errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)) {
		return fmt.Errorf("%w (could not connect within %s)", err, timeout)
	}
	return err
}

// deadlineConn sets the connection's deadline afresh before each read and
// write. The TLS connection after STARTTLS reads and writes through it, so
// the handshake and what follows are limited the same way.
type deadlineConn struct {
	net.Conn
	timeout time.Duration
}

func (c *deadlineConn) Read(b []byte) (int, error) {
	if err := c.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	n, err := c.Conn.Read(b)
	return n, c.waited(err)
}

func (c *deadlineConn) Write(b []byte) (int, error) {
	if err := c.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	n, err := c.Conn.Write(b)
	return n, c.waited(err)
}

// waited names how long the wait was when err is from running out of the
// deadline. Another timeout, such as the system's ETIMEDOUT of a connection
// that broke, is not that wait, so it goes as it is.
func (c *deadlineConn) waited(err error) error {
	var netErr net.Error
	if c.timeout > 0 && errors.Is(err, os.ErrDeadlineExceeded) && errors.As(err, &netErr) {
		return &waitTimeout{err: netErr, wait: c.timeout}
	}
	return err
}

// waitTimeout is a read or write that ran out of time. It is a net.Error
// still, so that the TLS connection and textproto treat it as the timeout it
// wraps.
type waitTimeout struct {
	err  net.Error
	wait time.Duration
}

func (e *waitTimeout) Error() string {
	return fmt.Sprintf("%s (the server did not answer within %s)", e.err, e.wait)
}

func (e *waitTimeout) Unwrap() error { return e.err }

func (e *waitTimeout) Timeout() bool { return true }

// Temporary is as the wrapped error has it, for net.Error.
func (e *waitTimeout) Temporary() bool { return e.err.Temporary() } //nolint:staticcheck // net.Error still declares it

// waitLonger runs f with the timeout of each wait multiplied by n, or the
// longest time.Duration when the product would not fit one, as a timeout
// set through the Go API may be as long as it likes.
func (c *Client) waitLonger(n time.Duration, f func() error) error {
	if c.deadline == nil {
		return f()
	}
	timeout := c.deadline.timeout
	c.deadline.timeout = scaleTimeout(timeout, n)
	defer func() { c.deadline.timeout = timeout }()
	return f()
}

// scaleTimeout returns d times n for a d and n above zero, or the longest
// time.Duration when that would overflow.
func scaleTimeout(d, n time.Duration) time.Duration {
	if d > math.MaxInt64/n {
		return math.MaxInt64
	}
	return d * n
}

func NewClient(conn net.Conn, host string) (*Client, error) {
	text := textproto.NewConn(conn)
	_, _, err := text.ReadResponse(220)
	if err != nil {
		_ = text.Close()
		return nil, err
	}
	c := &Client{Text: text, conn: conn, serverName: host, localName: "localhost"}
	_, c.tls = conn.(*tls.Conn)
	return c, nil
}

func (c *Client) Close() error {
	return c.Text.Close()
}

func (c *Client) hello() error {
	if !c.didHello {
		c.didHello = true
		err := c.ehlo()
		if err != nil {
			c.helloError = c.helo()
		}
	}
	return c.helloError
}

func (c *Client) Hello(localName string) error {
	if err := validateLine(localName); err != nil {
		return err
	}
	if c.didHello {
		return errors.New("smtp: Hello called after other methods")
	}
	c.localName = localName
	return c.hello()
}

func (c *Client) cmd(expectCode int, format string, args ...any) (int, string, error) {
	id, err := c.Text.Cmd(format, args...)
	if err != nil {
		return 0, "", err
	}
	c.Text.StartResponse(id)
	defer c.Text.EndResponse(id)
	code, msg, err := c.Text.ReadResponse(expectCode)
	return code, msg, err
}

func (c *Client) helo() error {
	c.ext = nil
	_, _, err := c.cmd(250, "HELO %s", c.localName)
	return err
}

func (c *Client) ehlo() error {
	_, msg, err := c.cmd(250, "EHLO %s", c.localName)
	if err != nil {
		return err
	}
	ext := make(map[string]string)
	extList := strings.Split(msg, "\n")
	if len(extList) > 1 {
		extList = extList[1:]
		for _, line := range extList {
			k, v, _ := strings.Cut(line, " ")
			ext[k] = v
		}
	}
	c.ext = ext
	return err
}

func (c *Client) StartTLS(config *tls.Config) error {
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(220, "STARTTLS")
	if err != nil {
		return err
	}
	c.conn = tls.Client(c.conn, config)
	c.Text = textproto.NewConn(c.conn)
	c.tls = true
	return c.ehlo()
}

func (c *Client) Mail(from string) error {
	if err := validateLine(from); err != nil {
		return err
	}
	if err := c.hello(); err != nil {
		return err
	}
	cmdStr := "MAIL FROM:<%s>"
	if c.ext != nil {
		if _, ok := c.ext["8BITMIME"]; ok {
			cmdStr += " BODY=8BITMIME"
		}
		if _, ok := c.ext["SMTPUTF8"]; ok {
			cmdStr += " SMTPUTF8"
		}
	}
	_, _, err := c.cmd(250, cmdStr, from)
	return err
}

func (c *Client) Rcpt(to string) error {
	if err := validateLine(to); err != nil {
		return err
	}
	_, _, err := c.cmd(25, "RCPT TO:<%s>", to)
	return err
}

type dataCloser struct {
	c *Client
	io.WriteCloser
}

func (d *dataCloser) Close() error {
	_ = d.WriteCloser.Close()
	return d.c.waitLonger(2, func() error {
		_, _, err := d.c.Text.ReadResponse(250)
		return err
	})
}

func (c *Client) Data() (io.WriteCloser, error) {
	_, _, err := c.cmd(354, "DATA")
	if err != nil {
		return nil, err
	}
	return &dataCloser{c, c.Text.DotWriter()}, nil
}

var testHookStartTLS func(*tls.Config) // nil, except for tests

func (c *Client) Extension(ext string) (bool, string) {
	if err := c.hello(); err != nil {
		return false, ""
	}
	if c.ext == nil {
		return false, ""
	}
	ext = strings.ToUpper(ext)
	param, ok := c.ext[ext]
	return ok, param
}

func (c *Client) Quit() error {
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(221, "QUIT")
	if err != nil {
		return err
	}
	return c.Text.Close()
}

func validateLine(line string) error {
	if strings.ContainsAny(line, "\n\r") {
		return errors.New("smtp: A line must not contain CR or LF")
	}
	return nil
}
