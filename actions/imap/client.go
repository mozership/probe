package imap

import (
	"crypto/tls"
	"errors"
	"fmt"
	"maps"
	"mime"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/mozership/probe/internal/mapping"
)

type Req struct {
	Host            string        `map:"host" validate:"required"`
	Port            int           `map:"port"`
	Username        string        `map:"username" validate:"required"`
	Password        string        `map:"password" validate:"required"`
	TLS             bool          `map:"tls"`
	Timeout         time.Duration `map:"timeout"`
	InsecureSkipTLS bool          `map:"insecure_skip_tls"`
	Commands        []Command     `map:"commands"`
	cb              *Callback
}

type Res struct {
	Code  int    `map:"code"`
	Data  Data   `map:"data"`
	Error string `map:"error"`
}

type Data struct {
	Select      SelectData `map:"select"`
	Examine     SelectData `map:"examine"`
	Search      SearchData `map:"search"`
	List        ListData   `map:"list"`
	Fetch       FetchData  `map:"fetch"`
	Store       StoreData  `map:"store"`
	Copy        CopyData   `map:"copy"`
	Create      CreateData `map:"create"`
	Delete      DeleteData `map:"delete"`
	Rename      RenameData `map:"rename"`
	Subscribe   StatusData `map:"subscribe"`
	Unsubscribe StatusData `map:"unsubscribe"`
	Noop        NoopData   `map:"noop"`
}

type Result struct {
	Req    Req           `map:"req"`
	Res    Res           `map:"res"`
	RT     time.Duration `map:"rt"`
	Status int           `map:"status"`
}

type Criteria struct {
	SeqNums    []string          `map:"seq_nums"`
	UIDs       []string          `map:"uids"`
	Since      string            `map:"since"`
	Before     string            `map:"before"`
	SentSince  string            `map:"sent_since"`
	SentBefore string            `map:"sent_before"`
	Headers    map[string]string `map:"headers"`
	Bodies     []string          `map:"bodies"`
	Texts      []string          `map:"texts"`
	Flags      []string          `map:"flags"`
	NotFlags   []string          `map:"not_flags"`
}

// Without login, logout and Expunge
type Command struct {
	Name       string   `map:"name" validate:"required"`
	Mailbox    string   `map:"mailbox"`
	OldMailbox string   `map:"oldmailbox"`
	NewMailbox string   `map:"newmailbox"`
	Reference  string   `map:"reference"`
	Pattern    string   `map:"pattern"`
	Criteria   Criteria `map:"criteria"`
	Sequence   string   `map:"sequence"`
	Dataitem   string   `map:"dataitem"`
	Value      string   `map:"value"`
}

// Message represents an email message
type Message struct {
	UID   int      `map:"uid"`
	Flags []string `map:"flags"`
	// Date is the envelope's Date in RFC 3339, such as
	// 2025-10-08T07:11:55Z. A time.Time came out of the result as an empty
	// map.
	Date     string            `map:"date"`
	From     string            `map:"from"`
	To       string            `map:"to"`
	Subject  string            `map:"subject"`
	Body     string            `map:"body"`
	HTMLBody string            `map:"html_body"`
	Size     int               `map:"size"`
	Headers  map[string]string `map:"headers"`
}

type SelectData struct {
	Exists         int      `map:"exists"`
	Recent         int      `map:"recent"`
	FirstUnseen    int      `map:"first_unseen"`
	UIDNext        int      `map:"uid_next"`
	Flags          []string `map:"flags"`
	PermanentFlags []string `map:"permanent_flags"`
}

type SearchData struct {
	All   string `map:"all"`
	Min   int    `map:"min"`
	Max   int    `map:"max"`
	Count int    `map:"count"`
}

type ListData struct {
	Mailboxes []MailboxInfo `map:"mailboxes"`
	Count     int           `map:"count"`
}

type MailboxInfo struct {
	Name       string   `map:"name"`
	Attributes []string `map:"attributes"`
	Delimiter  string   `map:"delimiter"`
}

type FetchData struct {
	Messages []Message `map:"messages"`
	Count    int       `map:"count"`
}

type StoreData struct {
	Success bool `map:"success"`
	Count   int  `map:"count"`
}

type CopyData struct {
	Success bool `map:"success"`
	Count   int  `map:"count"`
}

type CreateData struct {
	Success bool   `map:"success"`
	Mailbox string `map:"mailbox"`
}

type DeleteData struct {
	Success bool   `map:"success"`
	Mailbox string `map:"mailbox"`
}

type RenameData struct {
	Success    bool   `map:"success"`
	OldMailbox string `map:"old_mailbox"`
	NewMailbox string `map:"new_mailbox"`
}

type StatusData struct {
	Success bool   `map:"success"`
	Mailbox string `map:"mailbox"`
}

type NoopData struct {
	Success bool `map:"success"`
}

type Option func(*Callback)

type Callback struct {
	before func(req *Req)
	after  func(res *Res)
}

func WithBefore(f func(req *Req)) Option {
	return func(c *Callback) {
		c.before = f
	}
}

func WithAfter(f func(res *Res)) Option {
	return func(c *Callback) {
		c.after = f
	}
}

func Request(data map[string]any, opts ...Option) (map[string]any, error) {
	// A copy, so that parsing timeout leaves the caller's map as it was.
	m := maps.Clone(data)

	// MapToStructByTags reads port, tls and insecure_skip_tls from strings
	// as well, but not a duration, so timeout is parsed here; a number is
	// seconds.
	if timeoutInput, exists := m["timeout"]; exists {
		timeout, err := mapping.ParseTimeout(timeoutInput)
		if err != nil {
			return map[string]any{}, err
		}
		m["timeout"] = timeout
	}

	r := NewReq()
	if err := mapping.MapToStructByTags(m, r); err != nil {
		return map[string]any{}, fmt.Errorf("invalid imap parameters: %w", err)
	}

	cb := &Callback{}
	for _, opt := range opts {
		opt(cb)
	}
	r.cb = cb

	// Do's errors already say what failed: connecting, logging in, or the
	// session running out of time.
	ret, err := r.Do()
	if err != nil {
		return map[string]any{}, err
	}

	mapRet, err := mapping.StructToMapByTags(ret)
	if err != nil {
		return map[string]any{}, fmt.Errorf("failed to convert the imap result: %w", err)
	}

	return mapRet, nil
}

func NewReq() *Req {
	return &Req{
		Port:            993,
		TLS:             true,
		Timeout:         30 * time.Second,
		InsecureSkipTLS: false,
	}
}

// Do connects, logs in, runs the commands in one session and logs out.
func (r *Req) Do() (*Result, error) {
	result := &Result{Req: *r}

	if r.cb != nil && r.cb.before != nil {
		r.cb.before(r)
	}

	start := time.Now()

	res, err := r.run()
	result.RT = time.Since(start)
	if err != nil {
		return result, err
	}

	result.Res = *res
	result.Status = res.Code

	if r.cb != nil && r.cb.after != nil {
		r.cb.after(res)
	}

	return result, nil
}

// dialError reports a failed connection, naming the timeout when it ran out.
func (r *Req) dialError(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("IMAP connection timed out after %s: %w", r.Timeout, err)
	}
	return fmt.Errorf("failed to dial IMAP server: %w", err)
}

// timeoutError reports a session that ran out of time while doing what.
func (r *Req) timeoutError(doing string, err error) error {
	if err == nil {
		return fmt.Errorf("IMAP session timed out after %s while %s", r.Timeout, doing)
	}
	return fmt.Errorf("IMAP session timed out after %s while %s: %w", r.Timeout, doing, err)
}

// dial connects to the server r names, with TLS or without.
func (r *Req) dial() (*imapclient.Client, error) {
	addr := net.JoinHostPort(r.Host, strconv.Itoa(r.Port))
	options := &imapclient.Options{
		Dialer:      &net.Dialer{Timeout: r.Timeout},
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
		// Without a TLSConfig the client verifies the certificate whatever
		// insecure_skip_tls says, which fails against a server with a
		// self-signed certificate.
		TLSConfig: &tls.Config{InsecureSkipVerify: r.InsecureSkipTLS},
	}

	dial := imapclient.DialInsecure
	if r.TLS {
		dial = imapclient.DialTLS
	}
	cl, err := dial(addr, options)
	if err != nil {
		return nil, r.dialError(err)
	}
	return cl, nil
}

// run is the session Do times. A command that fails is reported in the Res;
// a connection, login or timeout failure is the error.
func (r *Req) run() (*Res, error) {
	// timeout bounds the whole session. The dialer covers connecting and the
	// TLS handshake; a timer started once connected covers the rest.
	deadline := time.Now().Add(r.Timeout)

	cl, err := r.dial()
	if err != nil {
		return nil, err
	}

	// Closing the connection is what stops a command waiting on a server
	// that does not answer; the command then fails and is reported as a
	// timeout below.
	// A zero Timeout, possible only through the Go API, means no limit.
	var timedOut atomic.Bool
	if r.Timeout > 0 {
		timer := time.AfterFunc(time.Until(deadline), func() {
			timedOut.Store(true)
			_ = cl.Close()
		})
		defer timer.Stop()
	}
	defer func() {
		_ = cl.Close()
	}()

	if err := cl.Login(r.Username, r.Password).Wait(); err != nil {
		if timedOut.Load() {
			return nil, r.timeoutError("logging in", err)
		}
		return nil, fmt.Errorf("failed to login: %w", err)
	}

	s := &session{cl: cl}
	cmdRes, err := s.runCommands(r.Commands)
	res := Res{Data: *cmdRes}

	// A session cut short is the environment's failure, not the command's,
	// so it is an action error rather than res.code 1.
	if timedOut.Load() {
		return nil, r.timeoutError("running commands", err)
	}

	if err != nil {
		res.Code = 1
		res.Error = err.Error()
		// The session can still run out while logging out; that is a
		// timeout like any other, with the command's failure kept in it.
		if logoutErr := cl.Logout().Wait(); timedOut.Load() {
			return nil, fmt.Errorf("%w (after the command failed: %v)", r.timeoutError("logging out", logoutErr), err)
		}
		return &res, nil
	}

	if err := cl.Logout().Wait(); err != nil {
		if timedOut.Load() {
			return nil, r.timeoutError("logging out", err)
		}
		res.Code = 2
		res.Error = fmt.Sprintf("command succeeded but logout failed: %v", err)
		return &res, nil
	}

	res.Code = 0

	return &res, nil
}

// session is a logged-in connection that runs a Req's commands. It keeps
// the messages the last search found, which a fetch, store or copy without
// a sequence acts on.
type session struct {
	cl           *imapclient.Client
	latestSeqSet string
	latestUidSet string
}

// commandFunc runs cmd in s and puts its answer in its entry of data.
type commandFunc func(s *session, cmd Command, data *Data) error

// commandFuncs holds what runs each command, by its name in lower case.
var commandFuncs = map[string]commandFunc{
	"select": func(s *session, c Command, d *Data) error {
		return assign(&d.Select)(s.Select(c.Mailbox))
	},
	"examine": func(s *session, c Command, d *Data) error {
		return assign(&d.Examine)(s.Examine(c.Mailbox))
	},
	"search": func(s *session, c Command, d *Data) error {
		return assign(&d.Search)(s.Search(c.Criteria))
	},
	"uid search": func(s *session, c Command, d *Data) error {
		return assign(&d.Search)(s.UIDSearch(c.Criteria))
	},
	"list": func(s *session, c Command, d *Data) error {
		return assign(&d.List)(s.List(c.Reference, c.Pattern))
	},
	"fetch": func(s *session, c Command, d *Data) error {
		return assign(&d.Fetch)(s.Fetch(c.Sequence, c.Dataitem))
	},
	"uid fetch": func(s *session, c Command, d *Data) error {
		return assign(&d.Fetch)(s.UIDFetch(c.Sequence, c.Dataitem))
	},
	"store": func(s *session, c Command, d *Data) error {
		return assign(&d.Store)(s.Store(c.Sequence, c.Dataitem, c.Value, false))
	},
	"uid store": func(s *session, c Command, d *Data) error {
		return assign(&d.Store)(s.Store(c.Sequence, c.Dataitem, c.Value, true))
	},
	"copy": func(s *session, c Command, d *Data) error {
		return assign(&d.Copy)(s.Copy(c.Sequence, c.Mailbox, false))
	},
	"uid copy": func(s *session, c Command, d *Data) error {
		return assign(&d.Copy)(s.Copy(c.Sequence, c.Mailbox, true))
	},
	"create": func(s *session, c Command, d *Data) error {
		return assign(&d.Create)(s.Create(c.Mailbox))
	},
	"delete": func(s *session, c Command, d *Data) error {
		return assign(&d.Delete)(s.Delete(c.Mailbox))
	},
	"rename": func(s *session, c Command, d *Data) error {
		return assign(&d.Rename)(s.Rename(c.OldMailbox, c.NewMailbox))
	},
	"subscribe": func(s *session, c Command, d *Data) error {
		return assign(&d.Subscribe)(s.Subscribe(c.Mailbox))
	},
	"unsubscribe": func(s *session, c Command, d *Data) error {
		return assign(&d.Unsubscribe)(s.Unsubscribe(c.Mailbox))
	},
	"noop": func(s *session, _ Command, d *Data) error {
		return assign(&d.Noop)(s.Noop())
	},
}

// assign returns a function that takes a command's answer and puts it in
// entry, unless the command failed. A failed command leaves the entry as it
// was, as it stops the commands after it.
func assign[T any](entry *T) func(*T, error) error {
	return func(v *T, err error) error {
		if err != nil {
			return err
		}
		*entry = *v
		return nil
	}
}

// runCommands runs cmds in order and stops at the first that fails. With
// the error it gives what the commands before it returned.
func (s *session) runCommands(cmds []Command) (*Data, error) {
	data := Data{}
	for i, cmd := range cmds {
		run, ok := commandFuncs[strings.ToLower(cmd.Name)]
		if !ok {
			return &data, fmt.Errorf("failed to execute command %d: unsupported command '%s'", i+1, cmd.Name)
		}
		if err := run(s, cmd, &data); err != nil {
			return &data, fmt.Errorf("failed to execute command %d (%s): %w", i+1, cmd.Name, err)
		}
	}
	return &data, nil
}

// newSelectData converts the server's answer to SELECT or EXAMINE.
func newSelectData(data *imap.SelectData) *SelectData {
	sd := SelectData{
		Exists:      int(data.NumMessages),
		Recent:      int(data.NumRecent),
		FirstUnseen: int(data.FirstUnseenSeqNum),
		UIDNext:     int(data.UIDNext),
	}

	for _, flag := range data.Flags {
		sd.Flags = append(sd.Flags, string(flag))
	}

	for _, flag := range data.PermanentFlags {
		sd.PermanentFlags = append(sd.PermanentFlags, string(flag))
	}

	return &sd
}

// Select implements SELECT.
func (s *session) Select(mb string) (*SelectData, error) {
	return s.selectMailbox(mb, false)
}

// Examine implements EXAMINE, a read-only SELECT.
func (s *session) Examine(mb string) (*SelectData, error) {
	return s.selectMailbox(mb, true)
}

// selectMailbox opens mb, read-only for EXAMINE. It forgets the last
// search, whose numbers belong to the mailbox open before.
func (s *session) selectMailbox(mb string, readOnly bool) (*SelectData, error) {
	opts := &imap.SelectOptions{
		ReadOnly:  readOnly,
		CondStore: false,
	}

	data, err := s.cl.Select(mb, opts).Wait()
	if err != nil {
		command := "Select"
		if readOnly {
			command = "Examine"
		}
		return nil, fmt.Errorf("failed to %s: %w", command, err)
	}

	s.forgetSearch()
	return newSelectData(data), nil
}

func (s *session) Search(cr Criteria) (*SearchData, error) {
	return s.search(cr, false)
}

func (s *session) UIDSearch(cr Criteria) (*SearchData, error) {
	return s.search(cr, true)
}

// search implements SEARCH, or UID SEARCH when uid is true, which finds the
// same messages but reports their UIDs. Only the latest search counts: a
// command without a sequence must not fall back to an older search of the
// other kind.
func (s *session) search(cr Criteria, uid bool) (*SearchData, error) {
	command := "Search"
	if uid {
		command = "UIDSearch"
	}

	criteria, err := buildSearchCriteria(cr)
	if err != nil {
		return nil, fmt.Errorf("invalid search criteria: %w", err)
	}

	opts := &imap.SearchOptions{
		ReturnMin:   true,
		ReturnMax:   true,
		ReturnAll:   true,
		ReturnCount: true,
		ReturnSave:  true,
	}

	run := s.cl.Search
	if uid {
		run = s.cl.UIDSearch
	}
	data, err := run(criteria, opts).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to %s: %w", command, err)
	}

	// No match gives no set at all, which String would dereference.
	all := ""
	if data.All != nil {
		all = data.All.String()
	}
	sd := SearchData{
		All:   all,
		Min:   int(data.Min),
		Max:   int(data.Max),
		Count: int(data.Count),
	}

	s.forgetSearch()
	if uid {
		s.latestUidSet = all
	} else {
		s.latestSeqSet = all
	}
	return &sd, nil
}

// startOfDay returns midnight at the start of t's day, in t's time zone.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// formatDate formats a message date, leaving a missing one empty.
func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func parseDate(st string) (time.Time, error) {
	original := strings.TrimSpace(st)
	lower := strings.ToLower(original)

	// A day starts at midnight local time. Truncating to 24 hours gave
	// midnight UTC, which is the day before for half the world in the
	// hours after their midnight.
	switch lower {
	case "today":
		return startOfDay(time.Now()), nil
	case "yesterday":
		return startOfDay(time.Now().AddDate(0, 0, -1)), nil
	}

	// Handle relative time expressions
	if strings.Contains(lower, "hour") || strings.Contains(lower, "minute") {
		re := regexp.MustCompile(`(\d+)\s*(hour|minute)s?\s*ago`)
		matches := re.FindStringSubmatch(lower)
		if len(matches) == 3 {
			num, err := strconv.Atoi(matches[1])
			if err == nil {
				var duration time.Duration
				if matches[2] == "hour" {
					duration = time.Duration(num) * time.Hour
				} else {
					duration = time.Duration(num) * time.Minute
				}
				return time.Now().Add(-duration), nil
			}
		}
	}

	formats := []string{
		"2006-01-02",
		"02-Jan-2006",
		"2006/01/02",
		"02/01/2006",
		time.RFC3339,
		time.RFC822,
	}

	for _, format := range formats {
		if date, err := time.Parse(format, original); err == nil {
			return date, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported date format: %s", original)
}

func buildSearchCriteria(cr Criteria) (*imap.SearchCriteria, error) {
	res := imap.SearchCriteria{}

	rangeStr := ":"
	allStr := "*"

	if len(cr.SeqNums) > 0 {
		seqset := new(imap.SeqSet)
		for _, v := range cr.SeqNums {
			arr := strings.Split(v, rangeStr)
			if len(arr) > 1 {
				if strings.Contains(v, allStr) {
					num, err := strconv.Atoi(arr[0])
					if err == nil {
						seqset.AddRange(uint32(num), 0)
					}
				} else {
					num1, err := strconv.Atoi(arr[0])
					if err != nil {
						continue
					}
					num2, err := strconv.Atoi(arr[1])
					if err != nil {
						continue
					}
					seqset.AddRange(uint32(num1), uint32(num2))
				}
			} else {
				if v == allStr {
					seqset.AddRange(1, 0)
				} else {
					num, err := strconv.Atoi(v)
					if err == nil {
						seqset.AddNum(uint32(num))
					}
				}
			}
		}
		// One set holds them all. Adding it once per value made a criterion
		// for every partial set, and the server matches all of them, so only
		// the first value counted.
		res.SeqNum = append(res.SeqNum, *seqset)
	}

	if len(cr.UIDs) > 0 {
		uidset := new(imap.UIDSet)
		for _, v := range cr.UIDs {
			arr := strings.Split(v, rangeStr)
			if len(arr) > 1 {
				if strings.Contains(v, allStr) {
					num, err := strconv.Atoi(arr[0])
					if err == nil {
						uidset.AddRange(imap.UID(num), 0)
					}
				} else {
					num1, err := strconv.Atoi(arr[0])
					if err != nil {
						continue
					}
					num2, err := strconv.Atoi(arr[1])
					if err != nil {
						continue
					}
					uidset.AddRange(imap.UID(num1), imap.UID(num2))
				}
			} else {
				if v == allStr {
					uidset.AddRange(1, 0)
				} else {
					num, err := strconv.Atoi(v)
					if err == nil {
						uidset.AddNum(imap.UID(num))
					}
				}
			}
		}
		res.UID = append(res.UID, *uidset)
	}

	if cr.Since != "" {
		d, err := parseDate(cr.Since)
		if err != nil {
			return nil, fmt.Errorf("invalid since date %s: %w", cr.Since, err)
		}
		res.Since = d
	}

	if cr.Before != "" {
		d, err := parseDate(cr.Before)
		if err != nil {
			return nil, fmt.Errorf("invalid before date %s: %w", cr.Before, err)
		}
		res.Before = d
	}

	if cr.SentSince != "" {
		d, err := parseDate(cr.SentSince)
		if err != nil {
			return nil, fmt.Errorf("invalid sent_since date %s: %w", cr.SentSince, err)
		}
		res.SentSince = d
	}

	if cr.SentBefore != "" {
		d, err := parseDate(cr.SentBefore)
		if err != nil {
			return nil, fmt.Errorf("invalid sent_before date %s: %w", cr.SentBefore, err)
		}
		res.SentBefore = d
	}

	if len(cr.Headers) > 0 {
		for key, val := range cr.Headers {
			fi := imap.SearchCriteriaHeaderField{
				Key:   key,
				Value: val,
			}
			res.Header = append(res.Header, fi)
		}
	}

	if len(cr.Bodies) > 0 {
		res.Body = cr.Bodies
	}

	if len(cr.Texts) > 0 {
		res.Text = cr.Texts
	}

	for _, flag := range cr.Flags {
		res.Flag = append(res.Flag, searchFlag(flag))
	}
	for _, flag := range cr.NotFlags {
		res.NotFlag = append(res.NotFlag, searchFlag(flag))
	}

	return &res, nil
}

// systemFlags are the flags IMAP defines, which are written with a backslash.
var systemFlags = map[string]imap.Flag{
	"seen":     imap.FlagSeen,
	"answered": imap.FlagAnswered,
	"flagged":  imap.FlagFlagged,
	"deleted":  imap.FlagDeleted,
	"draft":    imap.FlagDraft,
	"recent":   "\\Recent",
}

// searchFlag turns a flag as written in a workflow into the IMAP flag. A
// system flag can be given without its backslash, as in seen; anything else
// is a keyword such as $Important, which has none and is passed as it is.
func searchFlag(flag string) imap.Flag {
	if strings.HasPrefix(flag, "\\") {
		return imap.Flag(flag)
	}
	if f, ok := systemFlags[strings.ToLower(flag)]; ok {
		return f
	}
	return imap.Flag(flag)
}

// List implements LIST.
func (s *session) List(ref, pattern string) (*ListData, error) {
	if pattern == "" {
		pattern = "*"
	}

	list, err := s.cl.List(ref, pattern, nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to List: %w", err)
	}

	ld := ListData{
		Mailboxes: make([]MailboxInfo, 0, len(list)),
		Count:     len(list),
	}

	for _, mailbox := range list {
		attrs := make([]string, 0, len(mailbox.Attrs))
		for _, attr := range mailbox.Attrs {
			attrs = append(attrs, string(attr))
		}

		ld.Mailboxes = append(ld.Mailboxes, MailboxInfo{
			Name:       mailbox.Mailbox,
			Attributes: attrs,
			Delimiter:  string(mailbox.Delim),
		})
	}

	return &ld, nil
}

// The commands below have no answer but whether they succeeded. A failed one
// stops the commands after it with res.code 1, so success is true whenever
// its entry is filled; it is false only for a command that did not run.

// Create implements CREATE.
func (s *session) Create(mailbox string) (*CreateData, error) {
	if mailbox == "" {
		return nil, fmt.Errorf("mailbox name is required for CREATE command")
	}

	if err := s.cl.Create(mailbox, nil).Wait(); err != nil {
		return nil, fmt.Errorf("failed to Create mailbox %s: %w", mailbox, err)
	}

	return &CreateData{Success: true, Mailbox: mailbox}, nil
}

// Delete implements DELETE.
func (s *session) Delete(mailbox string) (*DeleteData, error) {
	if mailbox == "" {
		return nil, fmt.Errorf("mailbox name is required for DELETE command")
	}

	if err := s.cl.Delete(mailbox).Wait(); err != nil {
		return nil, fmt.Errorf("failed to Delete mailbox %s: %w", mailbox, err)
	}

	return &DeleteData{Success: true, Mailbox: mailbox}, nil
}

// Rename implements RENAME.
func (s *session) Rename(oldMailbox, newMailbox string) (*RenameData, error) {
	if oldMailbox == "" || newMailbox == "" {
		return nil, fmt.Errorf("both old and new mailbox names are required for RENAME command")
	}

	if err := s.cl.Rename(oldMailbox, newMailbox, nil).Wait(); err != nil {
		return nil, fmt.Errorf("failed to Rename mailbox from %s to %s: %w", oldMailbox, newMailbox, err)
	}

	return &RenameData{Success: true, OldMailbox: oldMailbox, NewMailbox: newMailbox}, nil
}

// Subscribe implements SUBSCRIBE.
func (s *session) Subscribe(mailbox string) (*StatusData, error) {
	if mailbox == "" {
		return nil, fmt.Errorf("mailbox name is required for SUBSCRIBE command")
	}

	if err := s.cl.Subscribe(mailbox).Wait(); err != nil {
		return nil, fmt.Errorf("failed to Subscribe to mailbox %s: %w", mailbox, err)
	}

	return &StatusData{Success: true, Mailbox: mailbox}, nil
}

// Unsubscribe implements UNSUBSCRIBE.
func (s *session) Unsubscribe(mailbox string) (*StatusData, error) {
	if mailbox == "" {
		return nil, fmt.Errorf("mailbox name is required for UNSUBSCRIBE command")
	}

	if err := s.cl.Unsubscribe(mailbox).Wait(); err != nil {
		return nil, fmt.Errorf("failed to Unsubscribe from mailbox %s: %w", mailbox, err)
	}

	return &StatusData{Success: true, Mailbox: mailbox}, nil
}

// Noop implements NOOP.
func (s *session) Noop() (*NoopData, error) {
	if err := s.cl.Noop().Wait(); err != nil {
		return nil, fmt.Errorf("failed to execute Noop: %w", err)
	}

	return &NoopData{Success: true}, nil
}

// Fetch implements FETCH command
func (s *session) Fetch(sequence, dataitem string) (*FetchData, error) {
	return s.fetch(sequence, dataitem, false)
}

func (s *session) UIDFetch(sequence, dataitem string) (*FetchData, error) {
	return s.fetch(sequence, dataitem, true)
}

// fetch implements FETCH, or UID FETCH when uid is true, which takes UIDs in
// sequence. FETCH needs dataitem; UID FETCH fetches ALL without it.
func (s *session) fetch(sequence, dataitem string, uid bool) (*FetchData, error) {
	if sequence == "" {
		sequence = s.latestSet(uid)
	}
	if sequence == "" {
		command := "FETCH command"
		if uid {
			command = "UID FETCH"
		}
		return nil, fmt.Errorf("sequence is required for %s: give one, or run a search of the same kind (search, or uid search for a uid command) just before it in this mailbox", command)
	}
	if dataitem == "" {
		if !uid {
			return nil, fmt.Errorf("dataitem is required for FETCH command")
		}
		dataitem = "ALL"
	}

	numSet, err := parseNumSet(sequence, uid)
	if err != nil {
		return nil, err
	}

	fetchItems, err := parseFetchItems(dataitem)
	if err != nil {
		return nil, fmt.Errorf("invalid dataitem %s: %w", dataitem, err)
	}

	messages, err := s.cl.Fetch(numSet, fetchItems).Collect()
	if err != nil {
		failed := "Fetch"
		if uid {
			failed = "UID Fetch"
		}
		return nil, fmt.Errorf("failed to %s: %w", failed, err)
	}

	fd := FetchData{
		Messages: make([]Message, 0, len(messages)),
		Count:    len(messages),
	}

	for _, msg := range messages {
		message := Message{
			UID:     int(msg.UID),
			Flags:   make([]string, 0, len(msg.Flags)),
			Headers: make(map[string]string),
		}

		for _, flag := range msg.Flags {
			message.Flags = append(message.Flags, string(flag))
		}

		if msg.Envelope != nil {
			message.Date = formatDate(msg.Envelope.Date)
			if len(msg.Envelope.From) > 0 {
				message.From = fmt.Sprintf("%s@%s", msg.Envelope.From[0].Mailbox, msg.Envelope.From[0].Host)
			}
			if len(msg.Envelope.To) > 0 {
				message.To = fmt.Sprintf("%s@%s", msg.Envelope.To[0].Mailbox, msg.Envelope.To[0].Host)
			}
			message.Subject = msg.Envelope.Subject
		}

		message.Size = int(msg.RFC822Size)

		// Handle body sections
		if len(msg.BodySection) > 0 {
			for _, bodySection := range msg.BodySection {
				if len(bodySection.Bytes) > 0 {
					content := string(bodySection.Bytes)

					// Determine the type of body section based on the Section metadata
					if bodySection.Section != nil {
						// Check if this is a header section
						if bodySection.Section.Specifier == imap.PartSpecifierHeader {
							// Parse headers and add to message.Headers
							headers := parseHeaderData(content)
							maps.Copy(message.Headers, headers)
						} else if len(bodySection.Section.HeaderFields) > 0 {
							// Handle HEADER.FIELDS specifically
							headers := parseHeaderData(content)
							maps.Copy(message.Headers, headers)
						} else if bodySection.Section.Specifier == imap.PartSpecifierText || bodySection.Section.Specifier == "" {
							// This is text content or full message
							if message.Body == "" {
								message.Body = content
							}
							// If this looks like HTML content, also set HTMLBody
							if strings.Contains(strings.ToLower(content), "<html") {
								message.HTMLBody = content
							}
						}
					} else {
						// No specific section metadata, treat as body content
						if message.Body == "" {
							message.Body = content
						}
						// If this looks like HTML content, also set HTMLBody
						if strings.Contains(strings.ToLower(content), "<html") {
							message.HTMLBody = content
						}
					}
				}
			}
		}

		fd.Messages = append(fd.Messages, message)
	}

	return &fd, nil
}

// Store implements STORE and UID STORE: it changes the flags of the given
// messages. dataitem is FLAGS, +FLAGS or -FLAGS, optionally followed by
// .SILENT, and value lists the flags, as in "\Seen \Flagged" or "(\Seen)".
func (s *session) Store(sequence, dataitem, value string, uid bool) (*StoreData, error) {
	if sequence == "" {
		sequence = s.latestSet(uid)
	}
	if sequence == "" {
		return nil, fmt.Errorf("sequence is required for STORE command: give one, or run a search of the same kind (search, or uid search for a uid command) just before it in this mailbox")
	}
	if dataitem == "" {
		return nil, fmt.Errorf("dataitem is required for STORE command")
	}

	numSet, err := parseNumSet(sequence, uid)
	if err != nil {
		return nil, err
	}
	store, err := parseStoreFlags(dataitem, value)
	if err != nil {
		return nil, err
	}

	messages, err := s.cl.Store(numSet, store, nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to Store: %w", err)
	}

	// With .SILENT the server does not answer with the new flags, so no
	// message is counted.
	return &StoreData{Success: true, Count: len(messages)}, nil
}

// parseStoreFlags turns a STORE data item and its flag list into the flags
// to send.
func parseStoreFlags(dataitem, value string) (*imap.StoreFlags, error) {
	item := strings.ToUpper(strings.TrimSpace(dataitem))
	store := &imap.StoreFlags{}

	if rest, ok := strings.CutSuffix(item, ".SILENT"); ok {
		store.Silent = true
		item = rest
	}
	switch {
	case strings.HasPrefix(item, "+"):
		store.Op = imap.StoreFlagsAdd
		item = item[1:]
	case strings.HasPrefix(item, "-"):
		store.Op = imap.StoreFlagsDel
		item = item[1:]
	default:
		store.Op = imap.StoreFlagsSet
	}
	if item != "FLAGS" {
		return nil, fmt.Errorf("unsupported STORE data item %q: use FLAGS, +FLAGS or -FLAGS, optionally with .SILENT", dataitem)
	}

	list := strings.TrimSpace(value)
	open, closed := strings.HasPrefix(list, "("), strings.HasSuffix(list, ")")
	if open != closed {
		// A lone parenthesis would leave an empty list, and FLAGS with no
		// flags clears them all; only "" or "()" may ask for that.
		return nil, fmt.Errorf("unbalanced parentheses in flags %q", value)
	}
	if open {
		list = list[1 : len(list)-1]
	}
	for f := range strings.FieldsSeq(list) {
		store.Flags = append(store.Flags, imap.Flag(f))
	}
	if len(store.Flags) == 0 && store.Op != imap.StoreFlagsSet {
		return nil, fmt.Errorf("value must list the flags to add or remove, such as \\Seen")
	}
	return store, nil
}

// Copy implements COPY and UID COPY: it copies the given messages into
// mailbox. Count is the number of messages the server reports copied, which
// it does when it supports UIDPLUS; otherwise it is 0.
func (s *session) Copy(sequence, mailbox string, uid bool) (*CopyData, error) {
	if sequence == "" {
		sequence = s.latestSet(uid)
	}
	if sequence == "" {
		return nil, fmt.Errorf("sequence is required for COPY command: give one, or run a search of the same kind (search, or uid search for a uid command) just before it in this mailbox")
	}
	if mailbox == "" {
		return nil, fmt.Errorf("mailbox is required for COPY command")
	}

	numSet, err := parseNumSet(sequence, uid)
	if err != nil {
		return nil, err
	}

	copied, err := s.cl.Copy(numSet, mailbox).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to Copy: %w", err)
	}

	count := 0
	if copied != nil {
		if uids, ok := copied.SourceUIDs.Nums(); ok {
			count = len(uids)
		}
	}
	return &CopyData{Success: true, Count: count}, nil
}

// parseNumSet parses sequence as sequence numbers, or as UIDs for the UID
// form of a command.
func parseNumSet(sequence string, uid bool) (imap.NumSet, error) {
	if uid {
		uidset, err := parseUIDSet(sequence)
		if err != nil {
			return nil, fmt.Errorf("invalid UID set %s: %w", sequence, err)
		}
		return *uidset, nil
	}
	seqset, err := parseSequenceSet(sequence)
	if err != nil {
		return nil, fmt.Errorf("invalid sequence set %s: %w", sequence, err)
	}
	return *seqset, nil
}

// forgetSearch drops the last search result. Sequence numbers and UIDs
// belong to one mailbox, so after another is selected the result would
// point at unrelated messages.
func (s *session) forgetSearch() {
	s.latestSeqSet = ""
	s.latestUidSet = ""
}

// latestSet returns the messages the last SEARCH or UID SEARCH found, which
// a FETCH, STORE or COPY without a sequence acts on. A search of one kind
// leaves nothing for the other.
func (s *session) latestSet(uid bool) string {
	if uid {
		return s.latestUidSet
	}
	return s.latestSeqSet
}

// numRange is one element of a sequence or UID set. 0 stands for "*", the
// last message, as it does in go-imap.
type numRange struct{ start, stop uint32 }

// parseNumRanges parses a set such as "1:3,5,9:*" or "*". Every number must
// be from 1 to 4294967295: 0 would turn into "*" and a larger value would
// wrap around, so "1:0" used to mean the whole mailbox. A lone "*" is the
// last message, not "1:*".
func parseNumRanges(sequence, what string) ([]numRange, error) {
	var ranges []numRange
	for part := range strings.SplitSeq(sequence, ",") {
		part = strings.TrimSpace(part)
		lo, hi, isRange := strings.Cut(part, ":")
		if !isRange {
			n, err := parseNumOrStar(lo, what)
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, numRange{n, n})
			continue
		}
		if strings.TrimSpace(lo) == "*" {
			return nil, fmt.Errorf("start of range cannot be *: %s", part)
		}
		start, err := parseNumOrStar(lo, what)
		if err != nil {
			return nil, err
		}
		stop, err := parseNumOrStar(hi, what)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, numRange{start, stop})
	}
	return ranges, nil
}

func parseNumOrStar(s, what string) (uint32, error) {
	s = strings.TrimSpace(s)
	if s == "*" {
		return 0, nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("invalid %s %q: use a number from 1 to 4294967295, or *", what, s)
	}
	return uint32(n), nil
}

// parseSequenceSet parses a sequence set string into an imap.SeqSet
func parseSequenceSet(sequence string) (*imap.SeqSet, error) {
	ranges, err := parseNumRanges(sequence, "sequence number")
	if err != nil {
		return nil, err
	}
	seqset := new(imap.SeqSet)
	for _, rg := range ranges {
		if rg.start == rg.stop {
			seqset.AddNum(rg.start)
		} else {
			seqset.AddRange(rg.start, rg.stop)
		}
	}
	return seqset, nil
}

// parseUIDSet parses a UID set string into an imap.UIDSet
func parseUIDSet(sequence string) (*imap.UIDSet, error) {
	ranges, err := parseNumRanges(sequence, "UID")
	if err != nil {
		return nil, err
	}
	uidset := new(imap.UIDSet)
	for _, rg := range ranges {
		if rg.start == rg.stop {
			uidset.AddNum(imap.UID(rg.start))
		} else {
			uidset.AddRange(imap.UID(rg.start), imap.UID(rg.stop))
		}
	}
	return uidset, nil
}

// parseFetchItems parses fetch items string into imap.FetchItem
// splitFetchItems splits a list of fetch items on the spaces between them.
// A space inside brackets or parentheses belongs to its item, as in
// BODY[HEADER.FIELDS (SUBJECT FROM)].
func splitFetchItems(dataitem string) []string {
	var items []string
	depth := 0
	start := 0
	for i, c := range dataitem {
		switch c {
		case '[', '(':
			depth++
		case ']', ')':
			if depth > 0 {
				depth--
			}
		case ' ':
			if depth == 0 {
				if i > start {
					items = append(items, dataitem[start:i])
				}
				start = i + 1
			}
		}
	}
	if start < len(dataitem) {
		items = append(items, dataitem[start:])
	}
	return items
}

func parseFetchItems(dataitem string) (*imap.FetchOptions, error) {
	opts := &imap.FetchOptions{}

	dataitem = strings.TrimSpace(dataitem)
	// A list may be written in parentheses, as IMAP writes it.
	if strings.HasPrefix(dataitem, "(") && strings.HasSuffix(dataitem, ")") {
		dataitem = strings.TrimSpace(dataitem[1 : len(dataitem)-1])
	}
	if dataitem == "" {
		return nil, fmt.Errorf("no fetch data item given")
	}

	// A macro stands alone.
	switch strings.ToUpper(dataitem) {
	case "ALL":
		opts.Envelope = true
		opts.Flags = true
		opts.InternalDate = true
		opts.RFC822Size = true
		return opts, nil
	case "FAST":
		opts.Flags = true
		opts.InternalDate = true
		opts.RFC822Size = true
		return opts, nil
	case "FULL":
		opts.Envelope = true
		opts.Flags = true
		opts.InternalDate = true
		opts.RFC822Size = true
		opts.BodyStructure = &imap.FetchItemBodyStructure{}
		return opts, nil
	}

	for _, item := range splitFetchItems(dataitem) {
		if err := addFetchItem(opts, item); err != nil {
			return nil, err
		}
	}

	return opts, nil
}

// fetchItemNames lists the data items addFetchItem takes, for its error.
const fetchItemNames = "ENVELOPE, FLAGS, INTERNALDATE, RFC822, RFC822.HEADER, RFC822.SIZE, RFC822.TEXT, UID, BODYSTRUCTURE, BODY[...] or BODY.PEEK[...]"

// bodySectionPartial matches a section with a partial range after it, as
// IMAP writes it: BODY[TEXT]<0.512>.
var bodySectionPartial = regexp.MustCompile(`^(BODY(?:\.PEEK)?\[.*)\](<\d+\.\d+>)$`)

// addFetchItem adds one data item of a list to opts. An item it does not
// know is an error: ignoring it fetched nothing for it without a word.
func addFetchItem(opts *imap.FetchOptions, item string) error {
	upper := strings.ToUpper(strings.TrimSpace(item))

	if strings.HasPrefix(upper, "BODY[") || strings.HasPrefix(upper, "BODY.PEEK[") {
		// parseBodySection takes the partial range inside the brackets.
		if m := bodySectionPartial.FindStringSubmatch(upper); m != nil {
			upper = m[1] + m[2] + "]"
		}
		if !strings.HasSuffix(upper, "]") {
			return fmt.Errorf("invalid BODY section: %s", item)
		}
		bodySection, err := parseBodySection(upper)
		if err != nil {
			return fmt.Errorf("failed to parse BODY section: %w", err)
		}
		opts.BodySection = append(opts.BodySection, bodySection)
		return nil
	}

	switch upper {
	case "ENVELOPE":
		opts.Envelope = true
	case "FLAGS":
		opts.Flags = true
	case "INTERNALDATE":
		opts.InternalDate = true
	case "RFC822.SIZE":
		opts.RFC822Size = true
	case "UID":
		opts.UID = true
	case "BODYSTRUCTURE":
		opts.BodyStructure = &imap.FetchItemBodyStructure{}
	// The RFC822 items are the sections RFC 3501 defines them as.
	case "RFC822":
		opts.BodySection = append(opts.BodySection, &imap.FetchItemBodySection{})
	case "RFC822.HEADER":
		opts.BodySection = append(opts.BodySection, &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true})
	case "RFC822.TEXT":
		opts.BodySection = append(opts.BodySection, &imap.FetchItemBodySection{Specifier: imap.PartSpecifierText})
	case "ALL", "FAST", "FULL":
		return fmt.Errorf("fetch macro %s cannot be combined with other data items", upper)
	default:
		return fmt.Errorf("unknown fetch data item %q: use %s, or one of the macros ALL, FAST and FULL alone", item, fetchItemNames)
	}
	return nil
}

// parseBodySection parses BODY[...] or BODY.PEEK[...] sections
func parseBodySection(bodyItem string) (*imap.FetchItemBodySection, error) {
	bodySection := &imap.FetchItemBodySection{}

	// Remove BODY[ or BODY.PEEK[ prefix and ] suffix
	var sectionContent string
	if strings.HasPrefix(bodyItem, "BODY.PEEK[") {
		bodySection.Peek = true
		sectionContent = strings.TrimSuffix(strings.TrimPrefix(bodyItem, "BODY.PEEK["), "]")
	} else if after, ok := strings.CutPrefix(bodyItem, "BODY["); ok {
		sectionContent = strings.TrimSuffix(after, "]")
	} else {
		return nil, fmt.Errorf("invalid BODY section format: %s", bodyItem)
	}

	// Handle partial fetch (e.g., BODY[TEXT]<0.512>)
	if strings.Contains(sectionContent, "<") && strings.Contains(sectionContent, ">") {
		partialIdx := strings.Index(sectionContent, "<")
		partialContent := sectionContent[partialIdx+1:]
		sectionContent = sectionContent[:partialIdx]
		partialContent = strings.TrimSuffix(partialContent, ">")

		// Parse partial range (e.g., "0.512")
		if strings.Contains(partialContent, ".") {
			parts := strings.Split(partialContent, ".")
			if len(parts) == 2 {
				offset, err := strconv.ParseUint(parts[0], 10, 32)
				if err != nil {
					return nil, fmt.Errorf("invalid partial offset: %s", parts[0])
				}
				size, err := strconv.ParseUint(parts[1], 10, 32)
				if err != nil {
					return nil, fmt.Errorf("invalid partial size: %s", parts[1])
				}
				bodySection.Partial = &imap.SectionPartial{
					Offset: int64(offset),
					Size:   int64(size),
				}
			}
		}
	}

	// Parse the section content
	if sectionContent == "" {
		// Empty section means full message
		return bodySection, nil
	}

	// Handle different section specifiers
	sectionContent = strings.TrimSpace(sectionContent)

	// Check for part numbers (e.g., "1.2.3")
	if regexp.MustCompile(`^\d+(\.\d+)*$`).MatchString(sectionContent) {
		// Parse part numbers
		partStrs := strings.Split(sectionContent, ".")
		parts := make([]int, len(partStrs))
		for i, partStr := range partStrs {
			part, err := strconv.Atoi(partStr)
			if err != nil {
				return nil, fmt.Errorf("invalid part number: %s", partStr)
			}
			parts[i] = part
		}
		bodySection.Part = parts
		return bodySection, nil
	}

	// Check for part numbers followed by specifier (e.g., "1.2.HEADER")
	if strings.Contains(sectionContent, ".") {
		lastDotIdx := strings.LastIndex(sectionContent, ".")
		partPrefix := sectionContent[:lastDotIdx]
		specifier := sectionContent[lastDotIdx+1:]

		if regexp.MustCompile(`^\d+(\.\d+)*$`).MatchString(partPrefix) {
			// Parse part numbers
			partStrs := strings.Split(partPrefix, ".")
			parts := make([]int, len(partStrs))
			for i, partStr := range partStrs {
				part, err := strconv.Atoi(partStr)
				if err != nil {
					return nil, fmt.Errorf("invalid part number: %s", partStr)
				}
				parts[i] = part
			}
			bodySection.Part = parts

			// Set specifier
			switch strings.ToUpper(specifier) {
			case "HEADER":
				bodySection.Specifier = imap.PartSpecifierHeader
			case "TEXT":
				bodySection.Specifier = imap.PartSpecifierText
			case "MIME":
				bodySection.Specifier = imap.PartSpecifierMIME
			default:
				return nil, fmt.Errorf("unsupported part specifier: %s", specifier)
			}
			return bodySection, nil
		}
	}

	// Handle header field requests (e.g., "HEADER.FIELDS (FROM TO)")
	if after, ok := strings.CutPrefix(sectionContent, "HEADER.FIELDS"); ok {
		headerContent := after
		headerContent = strings.TrimSpace(headerContent)

		// Check for NOT version
		isNot := false
		if strings.HasPrefix(headerContent, ".NOT") {
			isNot = true
			headerContent = strings.TrimPrefix(headerContent, ".NOT")
			headerContent = strings.TrimSpace(headerContent)
		}

		// Parse field list in parentheses
		if strings.HasPrefix(headerContent, "(") && strings.HasSuffix(headerContent, ")") {
			fieldList := strings.TrimSuffix(strings.TrimPrefix(headerContent, "("), ")")
			fields := strings.Fields(fieldList)

			if isNot {
				bodySection.HeaderFieldsNot = fields
				bodySection.Specifier = imap.PartSpecifierHeader
			} else {
				bodySection.HeaderFields = fields
				bodySection.Specifier = imap.PartSpecifierHeader
			}
			return bodySection, nil
		}
		return nil, fmt.Errorf("invalid HEADER.FIELDS format: %s", sectionContent)
	}

	// Handle simple specifiers
	switch sectionContent {
	case "HEADER":
		bodySection.Specifier = imap.PartSpecifierHeader
	case "TEXT":
		bodySection.Specifier = imap.PartSpecifierText
	case "MIME":
		bodySection.Specifier = imap.PartSpecifierMIME
	default:
		return nil, fmt.Errorf("unsupported section content: %s", sectionContent)
	}

	return bodySection, nil
}

// parseHeaderData parses RFC 2822 header format and returns a map
func parseHeaderData(headerData string) map[string]string {
	headers := make(map[string]string)
	lines := strings.Split(headerData, "\n")

	var currentKey string
	var currentValue strings.Builder

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		// Check if this is a continuation line (starts with whitespace)
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if currentKey != "" {
				currentValue.WriteString(" ")
				currentValue.WriteString(strings.TrimSpace(line))
			}
			continue
		}

		// Save previous header if exists
		if currentKey != "" {
			headers[strings.ToLower(currentKey)] = strings.TrimSpace(currentValue.String())
			currentValue.Reset()
		}

		// Parse new header line
		colonIndex := strings.Index(line, ":")
		if colonIndex > 0 {
			currentKey = strings.TrimSpace(line[:colonIndex])
			if colonIndex+1 < len(line) {
				currentValue.WriteString(strings.TrimSpace(line[colonIndex+1:]))
			}
		} else {
			currentKey = ""
		}
	}

	// Save last header if exists
	if currentKey != "" {
		headers[strings.ToLower(currentKey)] = strings.TrimSpace(currentValue.String())
	}

	return headers
}
