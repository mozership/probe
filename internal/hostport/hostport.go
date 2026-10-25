// Package hostport completes an address an action connects to with the port
// it is taken at when it names none, so that the guard of a run checks the
// same host and port the action dials.
package hostport

import (
	"net"
	"net/url"
	"strings"
)

// WithPort returns hostport with port when it names none. An IPv6 address
// may be given bare or in brackets.
func WithPort(hostport, port string) string {
	if _, _, err := net.SplitHostPort(hostport); err == nil {
		return hostport
	}
	return net.JoinHostPort(strings.Trim(hostport, "[]"), port)
}

// OfURL returns the host and port of u, taken at the port of its scheme when
// it names none: 443 for https, and 80 for any other.
func OfURL(u *url.URL) string {
	if u.Port() != "" {
		return u.Host
	}
	port := "80"
	if u.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(u.Hostname(), port)
}
