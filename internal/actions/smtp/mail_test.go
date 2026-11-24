package smtp

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mozership/probe/internal/smtptest"
)

func TestMail_New(t *testing.T) {
	mail := &Mail{
		Addr:         "localhost:25",
		MailFrom:     "sender@example.com",
		RcptTo:       []string{"recipient@example.com"},
		Data:         []byte("Subject: Test\n\nTest message"),
		MessageCount: 1,
	}

	if mail.Addr != "localhost:25" {
		t.Errorf("expected Addr to be 'localhost:25', got %s", mail.Addr)
	}
	if mail.MailFrom != "sender@example.com" {
		t.Errorf("expected MailFrom to be 'sender@example.com', got %s", mail.MailFrom)
	}
	if len(mail.RcptTo) != 1 || mail.RcptTo[0] != "recipient@example.com" {
		t.Errorf("expected RcptTo to contain 'recipient@example.com', got %v", mail.RcptTo)
	}
}

func TestGetFQDN(t *testing.T) {
	tests := []struct {
		name       string
		envValue   string
		setEnv     bool
		wantPrefix string
		wantEmpty  bool
	}{
		{
			name:       "with FQDN_DOMAIN env var",
			envValue:   "test.example.com",
			setEnv:     true,
			wantPrefix: "test.example.com",
			wantEmpty:  false,
		},
		{
			name:      "without FQDN_DOMAIN env var",
			setEnv:    false,
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				_ = os.Setenv("FQDN_DOMAIN", tt.envValue)
				defer func() { _ = os.Unsetenv("FQDN_DOMAIN") }()
			} else {
				_ = os.Unsetenv("FQDN_DOMAIN")
			}

			fqdn := getFQDN()

			if tt.wantEmpty && fqdn == "" {
				t.Error("expected FQDN to be non-empty")
			}
			if tt.wantPrefix != "" && fqdn != tt.wantPrefix {
				t.Errorf("expected FQDN to be '%s', got %s", tt.wantPrefix, fqdn)
			}
			if !tt.wantEmpty && fqdn == "" {
				t.Error("expected FQDN to be non-empty")
			}
		})
	}
}

func TestGenMessageID(t *testing.T) {
	idLeft, full := genMessageID()

	// idLeft must be "<ns>.<32-hex>"
	dot := strings.IndexByte(idLeft, '.')
	if dot <= 0 {
		t.Fatalf("idLeft missing '.' separator: %q", idLeft)
	}
	nsPart := idLeft[:dot]
	uidPart := idLeft[dot+1:]
	if _, err := strconv.ParseInt(nsPart, 10, 64); err != nil {
		t.Errorf("idLeft prefix is not numeric ns: %q", nsPart)
	}
	if len(uidPart) != 32 {
		t.Errorf("idLeft random part length = %d, want 32", len(uidPart))
	}
	for _, c := range uidPart {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("idLeft random part is not lowercase hex: %q", uidPart)
			break
		}
	}

	// full must be "<idLeft@host>"
	if !strings.HasPrefix(full, "<"+idLeft+"@") {
		t.Errorf("full Message-ID prefix mismatch: got %q, want prefix <%s@", full, idLeft)
	}
	if !strings.HasSuffix(full, ">") {
		t.Errorf("full Message-ID missing trailing '>': %q", full)
	}
	host := strings.TrimSuffix(strings.TrimPrefix(full, "<"+idLeft+"@"), ">")
	if host == "" {
		t.Errorf("full Message-ID has empty host part: %q", full)
	}

	// concurrent calls must produce distinct IDs
	const n = 64
	seen := make(map[string]struct{}, n)
	for range n {
		l, _ := genMessageID()
		if _, dup := seen[l]; dup {
			t.Fatalf("genMessageID produced duplicate idLeft: %s", l)
		}
		seen[l] = struct{}{}
	}
}

func TestTagSubject(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"subject first", "Subject: Hi\nFrom: a@b\n\nBody", "Subject: Hi - id\nFrom: a@b\n\nBody"},
		{"subject later", "From: a@b\nSubject: Hi\n\nBody", "From: a@b\nSubject: Hi - id\n\nBody"},
		{"crlf", "Subject: Hi\r\n\r\nBody\r\n", "Subject: Hi - id\r\n\r\nBody\r\n"},
		{"no subject", "From: a@b\n\nBody", "From: a@b\n\nBody"},
		// A line in the body is not a header, whatever it starts with.
		{"subject in the body only", "From: a@b\n\nSubject: not a header", "From: a@b\n\nSubject: not a header"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tagSubject([]byte(tt.data), "id")); got != tt.want {
				t.Errorf("tagSubject(%q) = %q, want %q", tt.data, got, tt.want)
			}
		})
	}
}

func TestStamp(t *testing.T) {
	original := []byte("Subject: Test\nFrom: a@b\n\nBody")

	before := time.Now().UnixNano()
	result := stamp(original)
	after := time.Now().UnixNano()

	lines := strings.SplitN(string(result), "\n", 3)
	if len(lines) != 3 {
		t.Fatalf("expected two prepended lines, got %q", result)
	}

	ts, ok := strings.CutPrefix(lines[0], "X-Send-Timestamp-Ns: ")
	if !ok {
		t.Fatalf("first line = %q, want the send timestamp", lines[0])
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		t.Fatalf("expected numeric timestamp, got %q: %v", ts, err)
	}
	if n < before || n > after {
		t.Errorf("timestamp %d not within [%d, %d]", n, before, after)
	}

	msgID, ok := strings.CutPrefix(lines[1], "Message-ID: <")
	if !ok {
		t.Fatalf("second line = %q, want the Message-ID", lines[1])
	}
	idLeft, _, _ := strings.Cut(msgID, "@")
	if want := "Subject: Test - " + idLeft + "\nFrom: a@b\n\nBody"; lines[2] != want {
		t.Errorf("message = %q, want %q", lines[2], want)
	}
	if string(original) != "Subject: Test\nFrom: a@b\n\nBody" {
		t.Errorf("original payload mutated: %q", original)
	}
}

func TestMail_SendSession_Validation(t *testing.T) {
	tests := []struct {
		name         string
		mail         *Mail
		wantError    bool
		wantErrorMsg string
	}{
		{
			name: "invalid MailFrom with newline",
			mail: &Mail{
				MailFrom: "invalid\nemail@example.com",
				RcptTo:   []string{"recipient@example.com"},
				Data:     []byte("Subject: Test\n\nTest message"),
			},
			wantError:    true,
			wantErrorMsg: "A line must not contain CR or LF",
		},
		{
			name: "invalid MailFrom with carriage return",
			mail: &Mail{
				MailFrom: "invalid\remail@example.com",
				RcptTo:   []string{"recipient@example.com"},
				Data:     []byte("Subject: Test\n\nTest message"),
			},
			wantError:    true,
			wantErrorMsg: "A line must not contain CR or LF",
		},
		{
			name: "invalid RcptTo with carriage return",
			mail: &Mail{
				MailFrom: "sender@example.com",
				RcptTo:   []string{"invalid\rrecipient@example.com"},
				Data:     []byte("Subject: Test\n\nTest message"),
			},
			wantError:    true,
			wantErrorMsg: "smtp rcptto validate error",
		},
		{
			name: "invalid RcptTo with newline",
			mail: &Mail{
				MailFrom: "sender@example.com",
				RcptTo:   []string{"invalid\nrecipient@example.com"},
				Data:     []byte("Subject: Test\n\nTest message"),
			},
			wantError:    true,
			wantErrorMsg: "smtp rcptto validate error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mail.SendSession()

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.wantErrorMsg) {
					t.Errorf("expected error message to contain '%s', got %s", tt.wantErrorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got %v", err)
				}
			}
		})
	}
}

func TestMail_SendSession_Success(t *testing.T) {
	tests := []struct {
		name      string
		mail      *Mail
		wantError bool
	}{
		{
			name: "successful single message",
			mail: &Mail{
				Addr:             "localhost:0", // Will be replaced with actual mock server port
				MailFrom:         "sender@example.com",
				RcptTo:           []string{"recipient@example.com"},
				Data:             []byte("Subject: Test\n\nTest message"),
				StartTLSDisabled: true,
				MessageCount:     1,
			},
			wantError: false,
		},
		{
			name: "successful multiple messages",
			mail: &Mail{
				Addr:             "localhost:0", // Will be replaced with actual mock server port
				MailFrom:         "sender@example.com",
				RcptTo:           []string{"recipient@example.com"},
				Data:             []byte("Subject: Test Multiple\n\nTest message"),
				StartTLSDisabled: true,
				MessageCount:     3,
			},
			wantError: false,
		},
		{
			name: "successful multiple recipients",
			mail: &Mail{
				Addr:             "localhost:0", // Will be replaced with actual mock server port
				MailFrom:         "sender@example.com",
				RcptTo:           []string{"recipient1@example.com", "recipient2@example.com"},
				Data:             []byte("Subject: Test Multiple Recipients\n\nTest message"),
				StartTLSDisabled: true,
				MessageCount:     1,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mail.Addr = startMockServer(t, false)
			err := tt.mail.SendSession()

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got %v", err)
				}
			}
		})
	}
}

func TestMail_SendSession_NetworkErrors(t *testing.T) {
	tests := []struct {
		name         string
		addr         string
		wantErrorMsg string
	}{
		{
			name:         "connection refused",
			addr:         "localhost:9999", // Non-existent port
			wantErrorMsg: "tcp dial error",
		},
		{
			name:         "invalid address",
			addr:         "invalid-host:25",
			wantErrorMsg: "tcp dial error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mail := &Mail{
				Addr:             tt.addr,
				MailFrom:         "sender@example.com",
				RcptTo:           []string{"recipient@example.com"},
				Data:             []byte("Subject: Test\n\nTest message"),
				StartTLSDisabled: true,
				MessageCount:     1,
			}

			err := mail.SendSession()
			if err == nil {
				t.Error("expected error but got none")
				return
			}
			if !strings.Contains(err.Error(), tt.wantErrorMsg) {
				t.Errorf("expected error message to contain '%s', got %s", tt.wantErrorMsg, err.Error())
			}
		})
	}
}

// writeSelfSignedCert writes a self-signed certificate for 127.0.0.1 and its
// key into dir, and returns their paths.
func writeSelfSignedCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "probe-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

// startMockServer runs the mock SMTP server on 127.0.0.1, offering STARTTLS
// with a self-signed certificate when withTLS is set, and returns its address.
func startMockServer(t *testing.T, withTLS bool) string {
	t.Helper()
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "none.pem"), filepath.Join(dir, "none.key")
	if withTLS {
		certPath, keyPath = writeSelfSignedCert(t, dir)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	server := &smtptest.Server{
		Addr: addr,
		Name: "test.example.com",
		Log:  log.New(io.Discard, "", 0),
		TLS:  &smtptest.TLS{CertPath: certPath, KeyPath: keyPath},
	}
	go func() { _ = server.Serve() }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if conn, err := net.Dial("tcp", addr); err == nil {
			_ = conn.Close()
			return addr
		}
		if time.Now().After(deadline) {
			t.Fatalf("mock server on %s did not start", addr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMail_SendSession_StartTLS(t *testing.T) {
	tests := []struct {
		name       string
		serverTLS  bool
		disabled   bool
		required   bool
		skipVerify bool
		wantErr    string
	}{
		{name: "off, server offers it", serverTLS: true, disabled: true},
		// The certificate is self-signed, so verifying it fails: STARTTLS was
		// taken up.
		{name: "auto, verified", serverTLS: true, wantErr: "certificate"},
		{name: "auto, not verified", serverTLS: true, skipVerify: true},
		{name: "auto, server does not offer it", serverTLS: false},
		{name: "required, server offers it", serverTLS: true, required: true, skipVerify: true},
		{name: "required, server does not offer it", serverTLS: false, required: true, wantErr: "does not offer STARTTLS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Mail{
				Addr:               startMockServer(t, tt.serverTLS),
				MailFrom:           "sender@example.com",
				RcptTo:             []string{"recipient@example.com"},
				Data:               []byte("Subject: Test\n\nTest message"),
				StartTLSDisabled:   tt.disabled,
				StartTLSRequired:   tt.required,
				InsecureSkipVerify: tt.skipVerify,
				MessageCount:       1,
			}
			err := m.SendSession()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("SendSession() error: %v", err)
				}
				if m.Delivered != 1 {
					t.Errorf("Delivered = %d, want 1", m.Delivered)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("SendSession() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestMail_SendSession_StartTLSHook(t *testing.T) {
	originalHook := testHookStartTLS
	defer func() { testHookStartTLS = originalHook }()

	hookCalled := false
	testHookStartTLS = func(config *tls.Config) {
		hookCalled = true
		if config == nil {
			t.Error("expected TLS config to be non-nil")
			return
		}
		// Verify ServerName is set (will be IP address from listener)
		if config.ServerName == "" {
			t.Error("expected ServerName to be non-empty")
		}
	}

	mail := &Mail{
		Addr:               startMockServer(t, true),
		MailFrom:           "sender@example.com",
		RcptTo:             []string{"recipient@example.com"},
		Data:               []byte("Subject: Test\n\nTest message"),
		InsecureSkipVerify: true,
		MessageCount:       1,
	}
	if err := mail.SendSession(); err != nil {
		t.Errorf("SendSession() error: %v", err)
	}

	if !hookCalled {
		t.Error("expected testHookStartTLS to be called")
	}
}

// greetingRecorder is an SMTP server that accepts mail and remembers the
// EHLO and HELO lines it was sent, which the shared mock server does not.
type greetingRecorder struct {
	ln    net.Listener
	mu    sync.Mutex
	lines []string
}

func newGreetingRecorder(t *testing.T) *greetingRecorder {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &greetingRecorder{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go r.serve()
	return r
}

func (r *greetingRecorder) addr() string { return r.ln.Addr().String() }

func (r *greetingRecorder) greetings() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

func (r *greetingRecorder) serve() {
	for {
		conn, err := r.ln.Accept()
		if err != nil {
			return
		}
		go r.session(conn)
	}
}

func (r *greetingRecorder) session(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	rd := bufio.NewReader(conn)
	write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }

	write("220 recorder ESMTP")
	inData := false
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				inData = false
				write("250 2.0.0 Ok: queued")
			}
			continue
		}
		cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch cmd {
		case "EHLO", "HELO":
			r.mu.Lock()
			r.lines = append(r.lines, line)
			r.mu.Unlock()
			if cmd == "EHLO" {
				write("250-recorder")
				write("250 8BITMIME")
			} else {
				write("250 recorder")
			}
		case "MAIL", "RCPT", "RSET", "NOOP":
			write("250 Ok")
		case "DATA":
			inData = true
			write("354 End data with <CR><LF>.<CR><LF>")
		case "QUIT":
			write("221 Bye")
			return
		default:
			write("502 Command not implemented")
		}
	}
}

func stubHostname(t *testing.T, name string, err error) {
	t.Helper()
	orig := osHostname
	osHostname = func() (string, error) { return name, err }
	t.Cleanup(func() { osHostname = orig })
}

func TestMail_SendSession_EHLOName(t *testing.T) {
	tests := []struct {
		name      string
		localName string
		host      string
		hostErr   error
		want      string
	}{
		{"given name", "probe-client.local", "machine-a", nil, "EHLO probe-client.local"},
		{"machine host name when none is given", "", "machine-a", nil, "EHLO machine-a"},
		{"localhost when the host name is unknown", "", "", errors.New("no hostname"), "EHLO localhost"},
		{"localhost when the host name is empty", "", "", nil, "EHLO localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubHostname(t, tt.host, tt.hostErr)
			srv := newGreetingRecorder(t)

			m := &Mail{
				Addr:             srv.addr(),
				LocalName:        tt.localName,
				MailFrom:         "from@example.com",
				RcptTo:           []string{"to@example.com"},
				Data:             []byte("Subject: hi\r\n\r\nbody\r\n"),
				StartTLSDisabled: true,
				MessageCount:     1,
			}
			if err := m.SendSession(); err != nil {
				t.Fatalf("SendSession: %v", err)
			}

			got := srv.greetings()
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("greetings = %q, want [%q]", got, tt.want)
			}
		})
	}
}

func TestMail_SendSession_RejectsLineBreakInName(t *testing.T) {
	srv := newGreetingRecorder(t)
	m := &Mail{
		Addr:             srv.addr(),
		LocalName:        "evil.example\r\nRSET",
		MailFrom:         "from@example.com",
		RcptTo:           []string{"to@example.com"},
		Data:             []byte("x\r\n"),
		StartTLSDisabled: true,
		MessageCount:     1,
	}
	if err := m.SendSession(); err == nil {
		t.Error("a name with a line break must be refused, not sent as a second command")
	}
	if got := srv.greetings(); len(got) != 0 {
		t.Errorf("nothing should have been sent, got %q", got)
	}
}

// startOneMessageServer runs an SMTP server that accepts one message per
// connection and refuses the MAIL FROM of the next with 451.
func startOneMessageServer(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				text := textproto.NewConn(conn)
				_ = text.PrintfLine("220 test ESMTP")
				mails := 0
				for {
					line, err := text.ReadLine()
					if err != nil {
						return
					}
					switch strings.ToUpper(strings.SplitN(line, " ", 2)[0]) {
					case "MAIL":
						mails++
						if mails > 1 {
							_ = text.PrintfLine("451 4.7.1 Try again later")
							continue
						}
						_ = text.PrintfLine("250 ok")
					case "DATA":
						_ = text.PrintfLine("354 go ahead")
						if _, err := text.ReadDotBytes(); err != nil {
							return
						}
						_ = text.PrintfLine("250 queued")
					case "QUIT":
						_ = text.PrintfLine("221 bye")
						return
					default:
						_ = text.PrintfLine("250 ok")
					}
				}
			}(conn)
		}
	}()

	return lis.Addr().String()
}

func TestSendSessionCountsMessagesBeforeFailure(t *testing.T) {
	// Each message is committed on its own, so the one accepted before the
	// refusal was delivered even though SendSession fails.
	m := &Mail{
		Addr:             startOneMessageServer(t),
		MailFrom:         "from@example.com",
		RcptTo:           []string{"to@example.com"},
		Data:             []byte("Subject: test\r\n\r\nbody\r\n"),
		StartTLSDisabled: true,
		MessageCount:     3,
	}
	if err := m.SendSession(); err == nil {
		t.Fatal("SendSession() succeeded although the second message was refused")
	}
	if m.Delivered != 1 {
		t.Errorf("Delivered = %d, want 1", m.Delivered)
	}
}

func TestDeliverCountsMessagesOfFailedSessions(t *testing.T) {
	// Two sessions of three messages each: every session delivers one and
	// then fails, and the two delivered count as sent.
	b := &Bulk{
		Addr:    startOneMessageServer(t),
		From:    "from@example.com",
		To:      "to@example.com",
		Session: 2,
		Message: 6,
	}
	result := b.DeliverWithResult()
	if result.Sent != 2 || result.Failed != 2 {
		t.Errorf("sent = %d, failed = %d, want 2 and 2", result.Sent, result.Failed)
	}
	// All six were attempted, although only two went out.
	if result.Total != 6 {
		t.Errorf("total = %d, want 6", result.Total)
	}
}

func TestSplitHeader(t *testing.T) {
	tests := []struct {
		data, header, body string
	}{
		{"Subject: a\nFrom: b\n\nbody\n", "Subject: a\nFrom: b\n", "\nbody\n"},
		{"Subject: a\r\n\r\nbody\r\n", "Subject: a\r\n", "\r\nbody\r\n"},
		{"Subject: a\nFrom: b", "Subject: a\nFrom: b", ""},
		{"\nbody", "", "\nbody"},
		{"", "", ""},
	}
	for _, tt := range tests {
		header, body := splitHeader([]byte(tt.data))
		if string(header) != tt.header || string(body) != tt.body {
			t.Errorf("splitHeader(%q) = %q, %q, want %q, %q", tt.data, header, body, tt.header, tt.body)
		}
	}
}

func TestSendSessionSharesTheBody(t *testing.T) {
	// Each message copies its headers to stamp them, but writes the body
	// from Data as it is, so that sessions of a long body hold one copy.
	data := []byte("Subject: hi\nFrom: a@b\n\n" + strings.Repeat("*", 1000) + "\n")
	original := string(data)
	m := &Mail{
		Addr:             startMockServer(t, false),
		MailFrom:         "from@example.com",
		RcptTo:           []string{"to@example.com"},
		Data:             data,
		StartTLSDisabled: true,
		MessageCount:     2,
	}
	if err := m.SendSession(); err != nil {
		t.Fatalf("SendSession() error: %v", err)
	}
	header, _ := splitHeader(data)
	if body := m.LastSent.Body; len(body) == 0 || &body[0] != &data[len(header)] {
		t.Error("the body sent is a copy, want the slice of Data")
	}
	if !strings.Contains(string(m.LastSent.Header), "Subject: hi - ") || !strings.HasPrefix(string(m.LastSent.Header), "X-Send-Timestamp-Ns: ") {
		t.Errorf("headers sent = %q, want them stamped", m.LastSent.Header)
	}
	if string(data) != original {
		t.Error("SendSession changed Data, which the other sessions share")
	}
}
