package smtp

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/mozership/probe/mapping"
)

type Req struct {
	Addr       string `map:"addr" validate:"required"`
	From       string `map:"from" validate:"required"`
	To         string `map:"to" validate:"required"`
	Subject    string `map:"subject"`
	MyHostname string `map:"myhostname"`
	Session    int    `map:"session"`
	Message    int    `map:"message"`
	Length     int    `map:"length"`
	// StartTLS is off, auto (use it when the server offers it) or required
	// (fail when it does not).
	StartTLS        string `map:"starttls"`
	InsecureSkipTLS bool   `map:"insecure_skip_tls"`
	// Timeout limits connecting to the server, and each wait on it to
	// read or write in a session, with twice it for the reply to the end of
	// a message. Zero, possible only through the Go API, means no limit.
	Timeout time.Duration `map:"timeout"`
	cb      *Callback
}

// The values of starttls.
const (
	StartTLSOff      = "off"
	StartTLSAuto     = "auto"
	StartTLSRequired = "required"
)

type Res struct {
	Code   int    `map:"code"`
	Sent   int    `map:"sent"`
	Failed int    `map:"failed"`
	Total  int    `map:"total"`
	Error  string `map:"error"`
	// MailData is the last message the server accepted, as it was sent.
	MailData string `map:"maildata"`
}

type Result struct {
	Req    Req           `map:"req"`
	Res    Res           `map:"res"`
	RT     time.Duration `map:"rt"`
	Status int           `map:"status"`
}

type Option func(*Callback)

type Callback struct {
	before func(from string, to string, subject string)
	after  func(result *Result)
}

// maxSession is the most sessions a step may open at once. Each is a
// connection of its own, and 1000 is far beyond what one server takes from
// one client, such as the 50 of Postfix's smtpd_client_connection_count_limit,
// while it keeps the goroutines and file descriptors of a step in bounds.
const maxSession = 1000

// maxLength is the longest body a step may ask for, 100 MiB. It is above the
// limits of common servers, such as Postfix's 10240000 bytes and Gmail's
// 25 MB, so that a test can still go past a server's limit, while it keeps
// a session from asking for more memory than the machine has.
const maxLength = 100 << 20

// defaultTimeout is the timeout when the step does not give one: the 5
// minutes RFC 5321 4.5.3.2 asks a client to wait for most replies.
const defaultTimeout = 5 * time.Minute

func NewReq() *Req {
	return &Req{
		Session:  1,
		Message:  1,
		Length:   0,
		StartTLS: StartTLSAuto,
		Timeout:  defaultTimeout,
	}
}

func (r *Req) Do() (*Result, error) {
	// Always create result with current request data, even if validation fails
	result := &Result{Req: *r}

	startTLS, err := r.validate()
	if err != nil {
		return result, err
	}

	// callback before
	if r.cb != nil && r.cb.before != nil {
		r.cb.before(r.From, r.To, r.Subject)
	}

	start := time.Now()

	bulk := &Bulk{
		Addr:            r.Addr,
		From:            r.From,
		To:              r.To,
		Subject:         r.Subject,
		MyHostname:      r.MyHostname,
		Session:         r.Session,
		Message:         r.Message,
		Length:          r.Length,
		StartTLS:        startTLS,
		InsecureSkipTLS: r.InsecureSkipTLS,
		Timeout:         r.Timeout,
	}

	// Execute mail delivery
	deliveryResult := bulk.DeliverWithResult()
	result.RT = time.Since(start)

	// Determine status based on delivery success
	status := 1 // default to failure
	if deliveryResult.Failed == 0 && deliveryResult.Sent > 0 {
		status = 0 // success if no failures and at least one sent
	}

	result.Res = Res{
		Code:     status,
		Sent:     deliveryResult.Sent,
		Failed:   deliveryResult.Failed,
		Total:    deliveryResult.Total,
		Error:    deliveryResult.Error,
		MailData: string(deliveryResult.LastSent),
	}
	result.Status = status

	// callback after
	if r.cb != nil && r.cb.after != nil {
		r.cb.after(result)
	}

	// A server that refused the mail answered, which is a result a test can
	// check; only failing to reach it at all is an error.
	if deliveryResult.Failed > 0 && deliveryResult.Sent == 0 && deliveryResult.Error != "" && !deliveryResult.Rejected {
		return result, fmt.Errorf("mail delivery failed: %s", deliveryResult.Error)
	}

	return result, nil
}

// validate checks the parameters before anything is sent, naming the one
// that is wrong, and returns starttls in the form Bulk takes.
func (r *Req) validate() (string, error) {
	switch {
	case r.Addr == "":
		return "", errors.New("addr is required")
	case r.From == "":
		return "", errors.New("from is required")
	case r.To == "":
		return "", errors.New("to is required")
	// Fewer than one session or message would send nothing and report
	// only a failure, and a negative length was taken as 0. Sessions and
	// length have an upper bound as well, since every session opens its
	// connection at once and holds its own copy of the message.
	case r.Session < 1 || r.Session > maxSession:
		return "", fmt.Errorf("invalid session %d: use 1 to %d", r.Session, maxSession)
	case r.Message < 1:
		return "", fmt.Errorf("invalid message %d: use 1 or more", r.Message)
	case r.Length < 0 || r.Length > maxLength:
		return "", fmt.Errorf("invalid length %d: use 0 to %d", r.Length, maxLength)
	case r.Timeout < 0:
		return "", fmt.Errorf("invalid timeout %s: use a duration above zero", r.Timeout)
	}
	startTLS := strings.ToLower(strings.TrimSpace(r.StartTLS))
	if startTLS == "" {
		startTLS = StartTLSAuto
	}
	switch startTLS {
	case StartTLSOff, StartTLSAuto, StartTLSRequired:
		return startTLS, nil
	default:
		return "", fmt.Errorf("invalid starttls %q: use off, auto or required", r.StartTLS)
	}
}

// Deliver runs the smtp action: it reads the parameters in data, sends the
// mail they describe, and returns the result as a map.
func Deliver(data map[string]any, opts ...Option) (map[string]any, error) {
	r := NewReq()

	cb := &Callback{}
	for _, opt := range opts {
		opt(cb)
	}
	r.cb = cb

	// A parameter that cannot be read stops the step before anything is
	// sent: going on would send with that parameter at its zero value, such
	// as insecure_skip_tls left false.
	if v, ok := data["timeout"]; ok {
		timeout, err := mapping.ParseTimeout(v)
		if err != nil {
			return map[string]any{}, err
		}
		data = maps.Clone(data)
		data["timeout"] = timeout
	}
	// MapToStructByTags reads a number or a numeric string into session,
	// message and length, and a bool or "true" and "false" into
	// insecure_skip_tls, so the other parameters go to it as they are.
	if err := mapping.MapToStructByTags(data, r); err != nil {
		return map[string]any{}, err
	}

	result, err := r.Do()
	if err != nil {
		// Even on error, try to return a structured result if we have one
		if result != nil {
			if mapResult, structErr := mapping.StructToMapByTags(result); structErr == nil {
				return mapResult, err
			}
		}
		return map[string]any{}, err
	}

	mapResult, err := mapping.StructToMapByTags(result)
	if err != nil {
		return map[string]any{}, err
	}

	return mapResult, nil
}

func WithBefore(f func(from string, to string, subject string)) Option {
	return func(c *Callback) {
		c.before = f
	}
}

func WithAfter(f func(result *Result)) Option {
	return func(c *Callback) {
		c.after = f
	}
}
