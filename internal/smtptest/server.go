// Package smtptest provides an SMTP server for trying the smtp action
// against: it accepts every message, offers STARTTLS when it has a
// certificate, and logs each line it reads and writes.
package smtptest

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
)

const (
	crlf      string = "\r\n"
	outgoing  string = ">"
	incomming string = "<"
	inserver  string = "-"
)

// TLS names the certificate and key the server answers STARTTLS with.
type TLS struct {
	CertPath string
	KeyPath  string
}

// Server is an SMTP server that accepts every message.
type Server struct {
	Addr string
	Name string
	Log  *log.Logger
	*TLS
	// tlsConfig is loaded from TLS when the server starts, and is nil when
	// the certificate cannot be read; the server then offers no STARTTLS.
	tlsConfig *tls.Config
}

type session struct {
	id                string
	server            *Server
	conn              net.Conn
	reader            *bufio.Reader
	writer            *bufio.Writer
	nowDataInProgress bool
	// secure is set once the session has switched to TLS.
	secure bool
}

// Serve listens on Addr and serves each connection until the process ends.
// Without TLS it reads keys/cert.pem and keys/key.pem, and offers no
// STARTTLS when they cannot be read.
func (s *Server) Serve() error {
	if s.TLS == nil {
		s.TLS = &TLS{
			CertPath: "keys/cert.pem",
			KeyPath:  "keys/key.pem",
		}
	}
	if s.Log == nil {
		s.Log = log.New(os.Stderr, "", log.LstdFlags)
	}
	// STARTTLS is offered only with a certificate to answer it with; offering
	// it without one made every client that took it up fail.
	if cert, err := tls.LoadX509KeyPair(s.CertPath, s.KeyPath); err == nil {
		s.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	}

	listener, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	s.Log.Printf("The mocking SMTP server is listening on %s\n", s.Addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			s.Log.Println("Accept error:", err)
			continue
		}

		sess := &session{server: s}
		go sess.handle(conn)
	}
}

func (s *session) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	err := s.setID()
	if err != nil {
		s.server.Log.Println("UID generating error:", err)
		return
	}

	s.conn = conn
	s.reader = bufio.NewReader(conn)
	s.writer = bufio.NewWriter(conn)

	s.writeStringWithLog(fmt.Sprintf("220 %s ESMTP Server", s.server.Name))
	_ = s.writer.Flush()
	s.nowDataInProgress = false

	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.server.Log.Printf("%s %s conn ReadString error: %#v", s.id, inserver, err)
			}
			return
		}

		s.server.Log.Printf("%s %s %s", s.id, incomming, line)
		line = strings.TrimSpace(line)
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		commands := strings.SplitSeq(line, crlf)
		for cmd := range commands {
			s.handleCommand(cmd)
		}

		_ = s.writer.Flush()
	}
}

func (s *session) handleCommand(cmd string) {
	first := ""
	second := ""
	parts := strings.Fields(strings.TrimSpace(cmd))
	if len(parts) == 0 {
		return
	}
	first = strings.ToUpper(parts[0])
	if len(parts) > 1 {
		second = strings.ToUpper(parts[1])
	}
	switch first {
	case "EHLO":
		lines := []string{s.server.Name, "PIPELINING", "SIZE 10240000"}
		if s.server.tlsConfig != nil && !s.secure {
			lines = append(lines, "STARTTLS")
		}
		lines = append(lines, "8BITMIME")
		for i, l := range lines {
			sep := "-"
			if i == len(lines)-1 {
				sep = " "
			}
			s.writeStringWithLog("250" + sep + l)
		}
	case "HELO":
		s.writeStringWithLog(fmt.Sprintf("250 Hello %s", parts[1]))
	case "MAIL":
		if strings.Contains(second, "FROM:") {
			s.writeStringWithLog("250 2.1.0 Ok")
		}
	case "RCPT":
		if strings.Contains(second, "TO:") {
			s.writeStringWithLog("250 2.1.5 Ok")
		}
	case "DATA":
		s.nowDataInProgress = true
		s.writeStringWithLog("354 End data with <CR><LF>.<CR><LF>")
	case "QUIT":
		s.writeStringWithLog("221 2.0.0 Bye")
		_ = s.writer.Flush()
		return
	case ".":
		s.nowDataInProgress = false
		s.writeStringWithLog("250 2.0.0 Ok: queued")
	case "RSET":
		s.writeStringWithLog("250 2.0.0 Ok")
	case "NOOP":
		s.writeStringWithLog("250 2.0.0 Ok")
	case "VRFY":
		s.writeStringWithLog("502 5.5.1 VRFY command is disabled")
	case "STARTTLS":
		if s.server.tlsConfig == nil || s.secure {
			s.writeStringWithLog("454 4.7.0 TLS not available")
			return
		}
		s.writeStringWithLog("220 2.0.0 Ready to start TLS")
		_ = s.writer.Flush()
		s.startTLS()
	default:
		if !s.nowDataInProgress {
			s.writeStringWithLog("500 Command not recognized")
		}
	}
}

// setID gives the session a random ID to tell its lines apart in the log.
func (s *session) setID() error {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	s.id = hex.EncodeToString(b)
	return nil
}

// startTLS switches the session to TLS. A failed handshake ends it, since
// the connection is in no state to go on in plain text.
func (s *session) startTLS() {
	tlsConn := tls.Server(s.conn, s.server.tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		s.server.Log.Printf("%s %s TLS handshake error: %v", s.id, inserver, err)
		_ = s.conn.Close()
		return
	}
	s.secure = true
	s.reader = bufio.NewReader(tlsConn)
	s.writer = bufio.NewWriter(tlsConn)
}

func (s *session) writeStringWithLog(str string) {
	_, err := s.writer.WriteString(str + crlf)
	if err != nil {
		s.server.Log.Printf("%s %s WriteString error: %#v", s.id, outgoing, err)
	}
	s.server.Log.Printf("%s %s %s", s.id, outgoing, strings.ReplaceAll(str, crlf, "\\r\\n"))
}
