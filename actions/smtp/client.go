package smtp

import (
	"fmt"
	"strings"
	"time"

	"github.com/mozership/probe/binary"
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
	cb              *Callback
}

// The values of starttls.
const (
	StartTLSOff      = "off"
	StartTLSAuto     = "auto"
	StartTLSRequired = "required"
)

type Res struct {
	Code     int    `map:"code"`
	Sent     int    `map:"sent"`
	Failed   int    `map:"failed"`
	Total    int    `map:"total"`
	Error    string `map:"error"`
	MailData string `map:"maildata"` // For text mail data
	FilePath string `map:"filepath"` // For binary mail data files
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

func NewReq() *Req {
	return &Req{
		Session:  1,
		Message:  1,
		Length:   0,
		StartTLS: StartTLSAuto,
	}
}

func (r *Req) Do() (*Result, error) {
	// Always create result with current request data, even if validation fails
	result := &Result{Req: *r}

	if r.Addr == "" {
		return result, fmt.Errorf("Req.Addr is required")
	}
	if r.From == "" {
		return result, fmt.Errorf("Req.From is required")
	}
	if r.To == "" {
		return result, fmt.Errorf("Req.To is required")
	}
	startTLS := strings.ToLower(strings.TrimSpace(r.StartTLS))
	if startTLS == "" {
		startTLS = StartTLSAuto
	}
	switch startTLS {
	case StartTLSOff, StartTLSAuto, StartTLSRequired:
	default:
		return result, fmt.Errorf("invalid starttls %q: use off, auto or required", r.StartTLS)
	}

	// callback before
	if r.cb != nil && r.cb.before != nil {
		r.cb.before(r.From, r.To, r.Subject)
	}

	start := time.Now()

	// Create parameter map for bulk email
	params := map[string]string{
		"addr":       r.Addr,
		"from":       r.From,
		"to":         r.To,
		"subject":    r.Subject,
		"myhostname": r.MyHostname,
	}

	if r.Session > 0 {
		params["session"] = fmt.Sprintf("%d", r.Session)
	}
	if r.Message > 0 {
		params["message"] = fmt.Sprintf("%d", r.Message)
	}
	if r.Length > 0 {
		params["length"] = fmt.Sprintf("%d", r.Length)
	}

	// Convert params to map[string]any for NewBulk
	paramsAny := make(map[string]any)
	for k, v := range params {
		paramsAny[k] = v
	}

	// Create bulk mailer
	bulk, err := NewBulk(paramsAny)
	if err != nil {
		return result, fmt.Errorf("failed to create bulk mailer: %w", err)
	}
	bulk.StartTLS = startTLS
	bulk.InsecureSkipTLS = r.InsecureSkipTLS

	// Execute mail delivery
	deliveryResult := bulk.DeliverWithResult()
	result.RT = time.Since(start)

	// Determine status based on delivery success
	status := 1 // default to failure
	if deliveryResult.Failed == 0 && deliveryResult.Sent > 0 {
		status = 0 // success if no failures and at least one sent
	}

	// Get mail data for processing
	mailData := bulk.makeData()

	// Process mail data (check if it's text or binary)
	bodyString, filePath, err := binary.ProcessHttpBody(mailData, "text/plain")
	if err != nil {
		return result, fmt.Errorf("failed to process mail data: %w", err)
	}

	result.Res = Res{
		Code:     status,
		Sent:     deliveryResult.Sent,
		Failed:   deliveryResult.Failed,
		Total:    deliveryResult.Total,
		Error:    deliveryResult.Error,
		MailData: bodyString,
		FilePath: filePath,
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

func Send(data map[string]any, opts ...Option) (map[string]any, error) {
	// Convert map[string]any to map[string]string for internal processing
	dataCopy := make(map[string]string)
	for k, v := range data {
		if str, ok := v.(string); ok {
			dataCopy[k] = str
		} else {
			dataCopy[k] = fmt.Sprintf("%v", v)
		}
	}

	// Convert map[string]string to map[string]any for HeaderToStringValue
	dataCopyAny := make(map[string]any)
	for k, v := range dataCopy {
		dataCopyAny[k] = v
	}
	m := mapping.HeaderToStringValue(dataCopyAny)

	r := NewReq()

	cb := &Callback{}
	for _, opt := range opts {
		opt(cb)
	}
	r.cb = cb

	// A parameter that cannot be read stops the step before anything is
	// sent: going on would send with that parameter at its zero value, such
	// as insecure_skip_tls left false.
	if err := mapping.MapToStructByTags(m, r); err != nil {
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
