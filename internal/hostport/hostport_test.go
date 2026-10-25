package hostport

import (
	"net/url"
	"testing"
)

func TestWithPort(t *testing.T) {
	tests := []struct {
		hostport string
		port     string
		want     string
	}{
		{"example.com", "53", "example.com:53"},
		{"example.com:5353", "53", "example.com:5353"},
		{"192.0.2.1", "443", "192.0.2.1:443"},
		{"192.0.2.1:8443", "443", "192.0.2.1:8443"},
		{"::1", "53", "[::1]:53"},
		{"[::1]", "53", "[::1]:53"},
		{"[::1]:5353", "53", "[::1]:5353"},
		{"2001:db8::1", "853", "[2001:db8::1]:853"},
	}
	for _, tt := range tests {
		if got := WithPort(tt.hostport, tt.port); got != tt.want {
			t.Errorf("WithPort(%q, %q) = %q, want %q", tt.hostport, tt.port, got, tt.want)
		}
	}
}

func TestOfURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"http://example.com/path", "example.com:80"},
		{"https://example.com/path", "example.com:443"},
		{"https://example.com:8443/path", "example.com:8443"},
		{"http://example.com:8080", "example.com:8080"},
		{"h2c://example.com", "example.com:80"},
		{"https://[::1]/", "[::1]:443"},
		{"http://[::1]:8080/", "[::1]:8080"},
		{"https://example.com:/", "example.com:443"},
	}
	for _, tt := range tests {
		u, err := url.Parse(tt.url)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", tt.url, err)
		}
		if got := OfURL(u); got != tt.want {
			t.Errorf("OfURL(%s) = %q, want %q", tt.url, got, tt.want)
		}
	}
}
