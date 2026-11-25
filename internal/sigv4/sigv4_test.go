package sigv4

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

var example = Credentials{
	AccessKeyID:     "AKIDEXAMPLE",
	SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
}

func signature(t *testing.T, req *http.Request) string {
	t.Helper()
	_, sig, ok := strings.Cut(req.Header.Get("Authorization"), "Signature=")
	if !ok {
		t.Fatalf("Authorization = %q, want a signature", req.Header.Get("Authorization"))
	}
	return sig
}

// The examples of the AWS documentation and of its test suite of
// Signature Version 4.
func TestSign_AWSExamples(t *testing.T) {
	at := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)

	t.Run("get-vanilla", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "https://example.amazonaws.com/", nil)
		Sign(req, example, "us-east-1", "service", PayloadHash(nil), at)
		want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, SignedHeaders=host;x-amz-date, Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
		if got := req.Header.Get("Authorization"); got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
	})

	t.Run("iam ListUsers", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08", nil)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
		Sign(req, example, "us-east-1", "iam", PayloadHash(nil), at)
		if got, want := signature(t, req), "5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"; got != want {
			t.Errorf("signature = %s, want %s", got, want)
		}
	})

	t.Run("s3 GET Object", func(t *testing.T) {
		creds := Credentials{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
		req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
		req.Header.Set("Range", "bytes=0-9")
		Sign(req, creds, "us-east-1", "s3", PayloadHash(nil), time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
		if got, want := signature(t, req), "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"; got != want {
			t.Errorf("signature = %s, want %s", got, want)
		}
		if got := req.Header.Get("X-Amz-Content-Sha256"); got != PayloadHash(nil) {
			t.Errorf("X-Amz-Content-Sha256 = %q, want the digest of an empty body", got)
		}
	})
}

func TestSign_SessionToken(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://sqs.us-east-1.amazonaws.com/", nil)
	creds := example
	creds.SessionToken = "token"
	Sign(req, creds, "us-east-1", "sqs", PayloadHash(nil), time.Now())
	if got := req.Header.Get("X-Amz-Security-Token"); got != "token" {
		t.Errorf("X-Amz-Security-Token = %q, want token", got)
	}
	if !strings.Contains(req.Header.Get("Authorization"), "x-amz-security-token") {
		t.Errorf("Authorization = %q, want the token signed", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-Amz-Content-Sha256") != "" {
		t.Error("X-Amz-Content-Sha256 is set for a service other than S3")
	}
}

func TestSign_LeavesOutUnsignedHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.amazonaws.com/", nil)
	req.Header.Set("User-Agent", "probe-http/1.0.0")
	req.Header.Set("Accept", "*/*")
	Sign(req, example, "us-east-1", "service", PayloadHash(nil), time.Now())
	if !strings.Contains(req.Header.Get("Authorization"), "SignedHeaders=accept;host;x-amz-date,") {
		t.Errorf("Authorization = %q, want accept, host and x-amz-date signed", req.Header.Get("Authorization"))
	}
}

func TestCanonicalQuery(t *testing.T) {
	tests := []struct {
		raw, want string
	}{
		{"", ""},
		{"b=2&a=1", "a=1&b=2"},
		{"a=2&a=1", "a=1&a=2"},
		{"a-b=1&a=2", "a=2&a-b=1"},
		{"acl", "acl="},
		{"q=a+b&r=%2F", "q=a%20b&r=%2F"},
		{"k=%zz", "k=%25zz"},
	}
	for _, tt := range tests {
		if got := canonicalQuery(tt.raw); got != tt.want {
			t.Errorf("canonicalQuery(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	got, err := FromEnv(env(map[string]string{"AWS_ACCESS_KEY_ID": "id", "AWS_SECRET_ACCESS_KEY": "secret", "AWS_SESSION_TOKEN": "token"}))
	if err != nil || got != (Credentials{"id", "secret", "token"}) {
		t.Errorf("FromEnv = %+v, %v", got, err)
	}
	if got, err := FromEnv(env(nil)); err != nil || got != (Credentials{}) {
		t.Errorf("FromEnv of nothing = %+v, %v, want the zero Credentials", got, err)
	}
	for _, m := range []map[string]string{
		{"AWS_ACCESS_KEY_ID": "id"},
		{"AWS_SECRET_ACCESS_KEY": "secret"},
		{"AWS_SESSION_TOKEN": "token"},
	} {
		if _, err := FromEnv(env(m)); err == nil {
			t.Errorf("FromEnv(%v) returned no error", m)
		}
	}
}
