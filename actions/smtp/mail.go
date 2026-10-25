package smtp

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Mail struct {
	Addr string
	// LocalName is the host name sent in EHLO and HELO. When it is empty the
	// machine's host name is used, falling back to "localhost".
	LocalName        string
	MailFrom         string
	RcptTo           []string
	Data             []byte
	StartTLSDisabled bool
	// StartTLSRequired fails the session when the server does not offer
	// STARTTLS, instead of going on in plain text.
	StartTLSRequired bool
	// InsecureSkipVerify accepts any certificate after STARTTLS, such as a
	// self-signed one.
	InsecureSkipVerify bool
	// Timeout limits connecting, and each wait on the server to read or
	// write, as Dial takes it. Zero means no limit.
	Timeout      time.Duration
	MessageCount int
	// Delivered is how many messages the server accepted in the last
	// SendSession. Each message is committed on its own, so a session that
	// fails part way has still delivered the ones before.
	Delivered int
	// LastSent is the last message the server accepted in the last
	// SendSession, as it was written to DATA, with its Message-ID and send
	// time. It is nil when none was accepted.
	LastSent *SentMessage
}

// SentMessage is a message as it was written to DATA: its own headers, and
// the body it shares, unchanged, with the other messages of the session.
type SentMessage struct {
	Header []byte
	Body   []byte
}

// String returns the message as one text, or "" for a nil message.
func (m *SentMessage) String() string {
	if m == nil {
		return ""
	}
	return string(m.Header) + string(m.Body)
}

// localName returns the name to greet the server with. Many servers refuse
// "localhost" in EHLO as not a real host name, so the machine's own name is
// the default, as a mail server would use.
func (m *Mail) localName() string {
	if m.LocalName != "" {
		return m.LocalName
	}
	if name, err := osHostname(); err == nil && name != "" {
		return name
	}
	return "localhost"
}

// osHostname is os.Hostname, replaced in tests.
var osHostname = os.Hostname

// SendSession opens one SMTP session to Addr and sends MessageCount copies of
// Data in it, each stamped with its own Message-ID and send time.
func (m *Mail) SendSession() error {
	m.Delivered = 0
	m.LastSent = nil
	if err := validateLine(m.MailFrom); err != nil {
		return err
	}
	for _, recp := range m.RcptTo {
		if err := validateLine(recp); err != nil {
			return fmt.Errorf("smtp rcptto validate error: %w", err)
		}
	}
	c, err := Dial(m.Addr, m.Timeout)
	if err != nil {
		return fmt.Errorf("tcp dial error: %w", err)
	}
	defer func() { _ = c.Close() }()
	if err = c.Hello(m.localName()); err != nil {
		return fmt.Errorf("smtp hello error: %w", err)
	}
	if !m.StartTLSDisabled {
		ok, _ := c.Extension("STARTTLS")
		switch {
		case ok:
			config := &tls.Config{ServerName: c.serverName, InsecureSkipVerify: m.InsecureSkipVerify}
			if testHookStartTLS != nil {
				testHookStartTLS(config)
			}
			if err = c.StartTLS(config); err != nil {
				return fmt.Errorf("starttls error: %w", err)
			}
		case m.StartTLSRequired:
			return errors.New("starttls error: the server does not offer STARTTLS")
		}
	}

	for i := 0; i < m.MessageCount; i++ {
		if err = c.Mail(m.MailFrom); err != nil {
			return fmt.Errorf("smtp mailfrom error: %w", err)
		}
		for _, addr := range m.RcptTo {
			if err = c.Rcpt(addr); err != nil {
				return fmt.Errorf("smtp rcptto error: %w", err)
			}
		}
		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("smtp data error: %w", err)
		}

		// Only the headers are stamped, so that each message copies them
		// and not the body, which may be long.
		header, body := splitHeader(m.Data)
		sent := &SentMessage{Header: stamp(header), Body: body}
		_, err = w.Write(sent.Header)
		if err == nil {
			_, err = w.Write(sent.Body)
		}
		if err != nil {
			return fmt.Errorf("smtp data write error: %w", err)
		}
		err = w.Close()
		if err != nil {
			return fmt.Errorf("smtp data close error: %w", err)
		}
		m.Delivered++
		m.LastSent = sent
	}

	return c.Quit()
}

func getFQDN() string {
	if fqdn := os.Getenv("FQDN_DOMAIN"); fqdn != "" {
		return fqdn
	}
	hostname, err := os.Hostname()
	if err != nil {
		// RFC 6761 reserves .invalid for cases where a real domain is unavailable.
		return "localhost.invalid"
	}
	return hostname
}

// genMessageID returns the id-left component (ns + 128bit random) and the
// fully bracketed Message-ID. The id-left is reused as the Subject tracking
// suffix so each mail's Message-ID and Subject can be cross-referenced.
func genMessageID() (idLeft, full string) {
	ns := time.Now().UnixNano()
	uid, err := randomID()
	if err != nil {
		idLeft = fmt.Sprintf("%d", ns)
	} else {
		idLeft = fmt.Sprintf("%d.%s", ns, uid)
	}
	full = fmt.Sprintf("<%s@%s>", idLeft, getFQDN())
	return
}

// stamp returns data as it is sent: with the id-left of a new Message-ID
// added to its Subject, and Message-ID and X-Send-Timestamp-Ns headers put
// before it. The timestamp is the current time in Unix nanoseconds, taken
// last so that it is as close as it can be to writing the message.
func stamp(data []byte) []byte {
	idLeft, msgID := genMessageID()
	data = prependHeader(tagSubject(data, idLeft), "Message-ID", msgID)
	return prependHeader(data, "X-Send-Timestamp-Ns", strconv.FormatInt(time.Now().UnixNano(), 10))
}

// splitHeader splits data after its headers, which end at the first empty
// line, so that the body starts with that line. Data with no empty line is
// all headers. Both are slices of data.
func splitHeader(data []byte) (header, body []byte) {
	for i := 0; i < len(data); {
		end := bytes.IndexByte(data[i:], '\n')
		if end < 0 {
			break
		}
		line := bytes.TrimSuffix(data[i:i+end], []byte("\r"))
		if len(line) == 0 {
			return data[:i:i], data[i:]
		}
		i += end + 1
	}
	return data, nil
}

// prependHeader puts a header line before data.
func prependHeader(data []byte, name, value string) []byte {
	h := fmt.Appendf(nil, "%s: %s\n", name, value)
	return append(h, data...)
}

// tagSubject adds " - " and tag to the end of the Subject header in data.
// It looks only at the headers, which end at the first empty line, and
// returns data as it is when they have no Subject.
func tagSubject(data []byte, tag string) []byte {
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		text, cr := bytes.CutSuffix(line, []byte("\r"))
		if len(text) == 0 {
			break
		}
		if !bytes.HasPrefix(text, []byte("Subject:")) {
			continue
		}
		tagged := append(append([]byte{}, text...), " - "+tag...)
		if cr {
			tagged = append(tagged, '\r')
		}
		lines[i] = tagged
		return bytes.Join(lines, []byte("\n"))
	}
	return data
}
