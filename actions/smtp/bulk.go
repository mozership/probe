package smtp

import (
	"errors"
	"fmt"
	"net/textproto"
	"strings"
	"sync"
	"time"
)

// Bulk sends Message generated messages from From to To over Session SMTP
// sessions at the same time. Req.Do builds it from the smtp action's
// parameters once they are checked.
type Bulk struct {
	Addr       string
	From       string
	To         string
	Subject    string
	MyHostname string
	Session    int
	Message    int
	Length     int

	// StartTLS is off, auto or required, as the smtp action's starttls. It
	// is off when empty, as Bulk always was.
	StartTLS string
	// InsecureSkipTLS accepts any certificate after STARTTLS.
	InsecureSkipTLS bool

	mu    sync.Mutex
	count int
}

type DeliveryResult struct {
	Sent     int
	Failed   int
	Total    int
	Sessions int
	Error    string
	// Rejected is set when a failed session got a reply from the server,
	// such as a 550 to RCPT TO, rather than failing to reach it.
	Rejected bool
}

func (b *Bulk) DeliverWithResult() DeliveryResult {
	type sendResult struct {
		count int
		err   error
	}

	// Every delivery hands out the messages from the start.
	b.mu.Lock()
	b.count = 0
	b.mu.Unlock()

	var wg sync.WaitGroup
	resultCh := make(chan sendResult, b.Session)

	for i := 0; i < b.Session; i++ {
		wg.Go(func() {
			count, err := b.Send()
			resultCh <- sendResult{count: count, err: err}
		})
	}

	wg.Wait()
	close(resultCh)

	totalSent := 0
	sessionsSuccess := 0
	sessionsFailed := 0
	// Sessions finish in any order, so the outcome cannot depend on which
	// failure is read last: one reply from the server is enough to say it
	// answered, and that reply is the error worth reporting.
	var lastError, rejection string
	for result := range resultCh {
		if result.err != nil {
			sessionsFailed++
			// The messages accepted before the failure were delivered all
			// the same.
			totalSent += result.count
			lastError = result.err.Error()
			var reply *textproto.Error
			if errors.As(result.err, &reply) {
				rejection = result.err.Error()
			}
		} else {
			sessionsSuccess++
			totalSent += result.count
		}
	}

	// Every message is handed to a session, so the count is how many were
	// attempted.
	b.mu.Lock()
	attempted := b.count
	b.mu.Unlock()

	result := DeliveryResult{
		Sent:     totalSent,
		Failed:   sessionsFailed,
		Total:    attempted,
		Sessions: sessionsSuccess + sessionsFailed,
		Error:    lastError,
		Rejected: rejection != "",
	}
	if rejection != "" {
		result.Error = rejection
	}
	return result
}

func (b *Bulk) Send() (int, error) {
	n := b.calcMessageNumEachSession()
	if n == 0 {
		return 0, nil
	}

	m := &Mail{
		Addr:               b.Addr,
		LocalName:          b.MyHostname,
		MailFrom:           b.From,
		RcptTo:             splitRecipients(b.To),
		Data:               b.makeData(),
		StartTLSDisabled:   b.StartTLS == "" || b.StartTLS == StartTLSOff,
		StartTLSRequired:   b.StartTLS == StartTLSRequired,
		InsecureSkipVerify: b.InsecureSkipTLS,
		MessageCount:       n,
	}

	err := m.Send()
	if err != nil {
		return m.Delivered, err
	}
	return n, nil
}

// splitRecipients splits a comma-separated list of addresses, dropping the
// spaces around each one and any empty entries, so that "a@x, b@x" gives
// "b@x" rather than " b@x".
func splitRecipients(to string) []string {
	var addrs []string
	for a := range strings.SplitSeq(to, ",") {
		if a = strings.TrimSpace(a); a != "" {
			addrs = append(addrs, a)
		}
	}
	return addrs
}

func (b *Bulk) calcMessageNumEachSession() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Spread the messages over the sessions, rounding up, so that the last
	// sessions get what is left and those after them get none. Resetting the
	// count once it ran out, as this used to, handed the messages out again
	// to the sessions after an empty one.
	n := (b.Message + b.Session - 1) / b.Session
	if left := b.Message - b.count; left < n {
		n = left
	}
	b.count += n
	return n
}

func (b *Bulk) makeData() []byte {
	now := time.Now().Format("Mon, 02 Jan 2006 15:04:05 -0700")
	appendingText := insertLF(strings.Repeat("*", b.Length), 80)
	data := fmt.Sprintf(`From: %s
To: %s
Date: %s
Subject: %s

This is a test mail.

%s
`, b.From, b.To, now, b.Subject, appendingText)
	return []byte(data)
}

func insertLF(s string, length int) string {
	n := len(s)
	if n <= length {
		return s
	}

	var result strings.Builder
	for i := 0; i < n; i += length {
		if i+length < len(s) {
			result.WriteString(s[i : i+length])
			result.WriteByte('\n')
		} else {
			result.WriteString(s[i:])
		}
	}

	return result.String()
}
