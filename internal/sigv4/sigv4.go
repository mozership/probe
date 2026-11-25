// Package sigv4 signs HTTP requests with AWS Signature Version 4, which AWS
// services and the storage and queues compatible with them take.
package sigv4

import (
	"cmp"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const algorithm = "AWS4-HMAC-SHA256"

// UnsignedPayload stands for the digest of a body S3 is told it is not
// given, for a body too large to read twice.
const UnsignedPayload = "UNSIGNED-PAYLOAD"

// unsignedHeaders are left out of a signature, as something on the way may
// change or add them.
var unsignedHeaders = []string{"authorization", "user-agent", "accept-encoding", "expect", "x-amzn-trace-id"}

// Credentials are the keys a request is signed with. SessionToken is set
// for temporary credentials only.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// FromEnv reads credentials from AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
// and AWS_SESSION_TOKEN, as the AWS CLI does. It returns the zero
// Credentials when none is set.
func FromEnv(getenv func(string) string) (Credentials, error) {
	c := Credentials{
		AccessKeyID:     getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: getenv("AWS_SECRET_ACCESS_KEY"),
		SessionToken:    getenv("AWS_SESSION_TOKEN"),
	}
	if (c.AccessKeyID == "") != (c.SecretAccessKey == "") {
		return Credentials{}, errors.New("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set together")
	}
	if c.SessionToken != "" && c.AccessKeyID == "" {
		return Credentials{}, errors.New("AWS_SESSION_TOKEN needs AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY")
	}
	return c, nil
}

// PayloadHash returns the digest of body that a signature covers.
func PayloadHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Sign adds to req the headers of a signature for service in region: the
// date, the session token when there is one, for S3 the digest of the body,
// and the Authorization built from them and from every other header of
// req. A header set on req after this is not covered by the signature.
//
// The path is encoded once more for every service but S3, as AWS asks.
func Sign(req *http.Request, creds Credentials, region, service, payloadHash string, now time.Time) {
	date := now.UTC().Format("20060102T150405Z")
	day := date[:8]
	req.Header.Set("X-Amz-Date", date)
	if creds.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}
	if service == "s3" {
		req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	}

	host := cmp.Or(req.Host, req.URL.Host)
	names := []string{"host"}
	values := map[string]string{"host": host}
	for name, v := range req.Header {
		name = strings.ToLower(name)
		if name == "host" || slices.Contains(unsignedHeaders, name) {
			continue
		}
		names = append(names, name)
		trimmed := make([]string, len(v))
		for i, s := range v {
			trimmed[i] = strings.Join(strings.Fields(s), " ")
		}
		values[name] = strings.Join(trimmed, ",")
	}
	slices.Sort(names)

	var headers strings.Builder
	for _, name := range names {
		headers.WriteString(name + ":" + values[name] + "\n")
	}
	signedHeaders := strings.Join(names, ";")

	path := cmp.Or(req.URL.EscapedPath(), "/")
	if service != "s3" {
		path = encode(path, false)
	}
	canonical := strings.Join([]string{
		req.Method,
		path,
		canonicalQuery(req.URL.RawQuery),
		headers.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{day, region, service, "aws4_request"}, "/")
	toSign := strings.Join([]string{algorithm, date, scope, PayloadHash([]byte(canonical))}, "\n")

	key := hmacSHA256([]byte("AWS4"+creds.SecretAccessKey), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, service)
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))

	req.Header.Set("Authorization", algorithm+" Credential="+creds.AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

// canonicalQuery writes a query as a signature takes it: each name and
// value percent-encoded, sorted by name and then by value. A name without
// a value, as in ?acl, has an empty one.
func canonicalQuery(raw string) string {
	type pair struct{ name, value string }
	var pairs []pair
	for part := range strings.SplitSeq(raw, "&") {
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		pairs = append(pairs, pair{encode(unescape(name), true), encode(unescape(value), true)})
	}
	slices.SortFunc(pairs, func(a, b pair) int {
		return cmp.Or(strings.Compare(a.name, b.name), strings.Compare(a.value, b.value))
	})
	s := make([]string, len(pairs))
	for i, p := range pairs {
		s[i] = p.name + "=" + p.value
	}
	return strings.Join(s, "&")
}

// unescape decodes the percent-encoding of a part of a query, and a plus
// as a space. A part that is not well encoded is taken as it is written.
func unescape(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return s
}

// encode percent-encodes s as a signature expects: every byte but the
// unreserved ones, and a slash too when encodeSlash is set.
func encode(s string, encodeSlash bool) string {
	const upper = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upper[c>>4])
			b.WriteByte(upper[c&15])
		}
	}
	return b.String()
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
