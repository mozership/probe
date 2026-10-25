package smtp

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
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
	MessageCount       int
	// Delivered is how many messages the server accepted in the last Send.
	// Each message is committed on its own, so a Send that fails part way
	// has still delivered the ones before.
	Delivered int
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

func (m *Mail) Send() error {
	m.Delivered = 0
	if err := validateLine(m.MailFrom); err != nil {
		return err
	}
	for _, recp := range m.RcptTo {
		if err := validateLine(recp); err != nil {
			return fmt.Errorf("smtp rcptto validate error: %w", err)
		}
	}
	c, err := Dial(m.Addr)
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

		_, err = w.Write(m.appendSendTimestamp(m.appendIDtoSubject(m.Data)))
		if err != nil {
			return fmt.Errorf("smtp data write error: %w", err)
		}
		err = w.Close()
		if err != nil {
			return fmt.Errorf("smtp data close error: %w", err)
		}
		m.Delivered++
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
	uid, err := OptimisticUID()
	if err != nil {
		idLeft = fmt.Sprintf("%d", ns)
	} else {
		idLeft = fmt.Sprintf("%d.%s", ns, uid)
	}
	full = fmt.Sprintf("<%s@%s>", idLeft, getFQDN())
	return
}

// appendSendTimestamp prepends an X-Send-Timestamp-Ns header carrying the
// current time in Unix nanoseconds, captured at the moment the message is
// about to be written to the SMTP DATA stream.
func (m *Mail) appendSendTimestamp(data []byte) []byte {
	h := fmt.Appendf(nil, "X-Send-Timestamp-Ns: %d\n", time.Now().UnixNano())
	return append(h, data...)
}

func (m *Mail) appendIDtoSubject(data []byte) []byte {
	idLeft, msgID := genMessageID()
	lines := bytes.Split(data, []byte("\n"))

	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("Subject:")) {
			lines[i] = append(line, []byte(" - "+idLeft)...)
			break
		}
	}

	mid := []byte("Message-ID: " + msgID)
	lines = append([][]byte{mid}, lines...)

	return bytes.Join(lines, []byte("\n"))
}
