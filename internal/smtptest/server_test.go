package smtptest

import (
	"io"
	"log"
	"net"
	"net/smtp"
	"path/filepath"
	"testing"
	"time"
)

// start runs a Server with no certificate on a free port of 127.0.0.1 and
// returns its address once it accepts connections.
func start(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()

	dir := t.TempDir()
	s := &Server{
		Addr: addr,
		Name: "smtptest.example.com",
		Log:  log.New(io.Discard, "", 0),
		TLS:  &TLS{CertPath: filepath.Join(dir, "none.pem"), KeyPath: filepath.Join(dir, "none.key")},
	}
	go func() { _ = s.Serve() }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if conn, err := net.Dial("tcp", addr); err == nil {
			_ = conn.Close()
			return addr
		}
		if time.Now().After(deadline) {
			t.Fatalf("server on %s did not start", addr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServerAcceptsMail(t *testing.T) {
	c, err := smtp.Dial(start(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Hello("client.example.com"); err != nil {
		t.Fatalf("Hello: %v", err)
	}
	// Without a certificate to answer it with, STARTTLS is not offered.
	if ok, _ := c.Extension("STARTTLS"); ok {
		t.Error("STARTTLS is offered without a certificate")
	}
	if err := c.Mail("from@example.com"); err != nil {
		t.Fatalf("Mail: %v", err)
	}
	if err := c.Rcpt("to@example.com"); err != nil {
		t.Fatalf("Rcpt: %v", err)
	}
	w, err := c.Data()
	if err != nil {
		t.Fatalf("Data: %v", err)
	}
	if _, err := io.WriteString(w, "Subject: hi\r\n\r\nbody\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("the message was not accepted: %v", err)
	}
	if err := c.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
}

func TestServerRefusesStartTLSWithoutACertificate(t *testing.T) {
	c, err := smtp.Dial(start(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Text.PrintfLine("STARTTLS"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Text.ReadResponse(220); err == nil {
		t.Error("STARTTLS was taken up without a certificate")
	}
}
