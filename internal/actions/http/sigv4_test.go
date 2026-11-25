package http

import (
	"io"
	hp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/internal/sigv4"
	"github.com/mozership/probe/sdk"
)

// verifySigV4 checks the signature of r as AWS does: it signs again the
// headers the request says it signed, at the time it says it was signed,
// with the secret, and compares the two.
func verifySigV4(t *testing.T, r *hp.Request, body []byte, creds sigv4.Credentials, region, service string) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	_, rest, ok := strings.Cut(auth, "SignedHeaders=")
	if !ok {
		t.Errorf("Authorization = %q, want a signature", auth)
		return
	}
	signed, _, _ := strings.Cut(rest, ",")
	at, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil {
		t.Errorf("X-Amz-Date: %v", err)
		return
	}
	again, _ := hp.NewRequest(r.Method, "http://"+r.Host+r.URL.RequestURI(), nil)
	for name := range strings.SplitSeq(signed, ";") {
		if name != "host" {
			again.Header[hp.CanonicalHeaderKey(name)] = r.Header.Values(name)
		}
	}
	sigv4.Sign(again, creds, region, service, sigv4.PayloadHash(body), at)
	if got := again.Header.Get("Authorization"); got != auth {
		t.Errorf("Authorization = %q, want %q", auth, got)
	}
}

func TestRequest_AWSSigV4(t *testing.T) {
	creds := sigv4.Credentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "secret", SessionToken: "token"}
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		body, _ := io.ReadAll(r.Body)
		verifySigV4(t, r, body, creds, "ap-northeast-1", "sqs")
		if !strings.HasPrefix(string(body), `{"QueueUrl":`) {
			t.Errorf("body = %s, want the object sent as JSON", body)
		}
		if got := r.Header.Get("X-Amz-Security-Token"); got != "token" {
			t.Errorf("X-Amz-Security-Token = %q, want token", got)
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_, _ = w.Write([]byte(`{"Messages":[]}`))
	}))
	defer srv.Close()

	ret, _, err := RequestStep(sdk.Call{With: map[string]any{
		"post": srv.URL + "/?a=1",
		"headers": map[string]any{
			"Content-Type": "application/x-amz-json-1.0",
			"X-Amz-Target": "AmazonSQS.ReceiveMessage",
		},
		"body": map[string]any{"QueueUrl": "https://sqs.ap-northeast-1.amazonaws.com/1/q", "WaitTimeSeconds": 20},
		"aws_sigv4": map[string]any{
			"service":           "sqs",
			"region":            "ap-northeast-1",
			"access_key_id":     creds.AccessKeyID,
			"secret_access_key": creds.SecretAccessKey,
			"session_token":     creds.SessionToken,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	res := ret["res"].(map[string]any)
	if res["code"] != 200 || res["body"] != `{"Messages":[]}` {
		t.Errorf("res = %v, want 200 and the body as text", res)
	}
}

func TestTakeSigV4_FromEnv(t *testing.T) {
	env := map[string]string{
		"AWS_ACCESS_KEY_ID":     "id",
		"AWS_SECRET_ACCESS_KEY": "secret",
		"AWS_DEFAULT_REGION":    "eu-west-1",
	}
	m := map[string]any{"aws_sigv4": map[string]any{"service": "execute-api"}}
	got, err := takeSigV4(m, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	want := awsSigning{creds: sigv4.Credentials{AccessKeyID: "id", SecretAccessKey: "secret"}, region: "eu-west-1", service: "execute-api"}
	if *got != want {
		t.Errorf("takeSigV4 = %+v, want %+v", *got, want)
	}
	if _, ok := m["aws_sigv4"]; ok {
		t.Error("aws_sigv4 is left in the parameters")
	}

	env["AWS_REGION"] = "us-west-2"
	got, _ = takeSigV4(map[string]any{"aws_sigv4": map[string]any{"service": "s3"}}, func(k string) string { return env[k] })
	if got.region != "us-west-2" {
		t.Errorf("region = %q, want AWS_REGION before AWS_DEFAULT_REGION", got.region)
	}

	got, _ = takeSigV4(map[string]any{"aws_sigv4": map[string]any{"service": "s3", "region": "ap-south-1"}}, func(k string) string { return env[k] })
	if got.region != "ap-south-1" {
		t.Errorf("region = %q, want the one given before the environment", got.region)
	}

	if got, err := takeSigV4(map[string]any{}, func(string) string { return "" }); got != nil || err != nil {
		t.Errorf("takeSigV4 without aws_sigv4 = %v, %v, want nothing", got, err)
	}
}

func TestTakeSigV4_Errors(t *testing.T) {
	full := map[string]string{"AWS_ACCESS_KEY_ID": "id", "AWS_SECRET_ACCESS_KEY": "secret", "AWS_REGION": "us-east-1"}
	tests := []struct {
		name string
		v    any
		env  map[string]string
		want string
	}{
		{"not a map", "sqs", full, "must be a map"},
		{"unknown key", map[string]any{"service": "sqs", "secret": "x"}, full, "not secret"},
		{"not a string", map[string]any{"service": 1}, full, "aws_sigv4.service must be a string"},
		{"no service", map[string]any{}, full, "aws_sigv4.service is required"},
		{"no region", map[string]any{"service": "sqs"}, map[string]string{"AWS_ACCESS_KEY_ID": "id", "AWS_SECRET_ACCESS_KEY": "secret"}, "aws_sigv4.region is required"},
		{"key id alone", map[string]any{"service": "sqs", "access_key_id": "id"}, full, "must be given together"},
		{"token alone", map[string]any{"service": "sqs", "session_token": "t"}, full, "session_token needs"},
		{"no credentials", map[string]any{"service": "sqs"}, map[string]string{"AWS_REGION": "us-east-1"}, "needs credentials"},
		{"half the environment", map[string]any{"service": "sqs"}, map[string]string{"AWS_REGION": "us-east-1", "AWS_ACCESS_KEY_ID": "id"}, "must be set together"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := takeSigV4(map[string]any{"aws_sigv4": tt.v}, func(k string) string { return tt.env[k] })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one that says %q", err, tt.want)
			}
		})
	}
}

func TestRequest_AWSSigV4WithAuthorization(t *testing.T) {
	sign := map[string]any{"service": "sqs", "region": "us-east-1", "access_key_id": "id", "secret_access_key": "secret"}
	for _, with := range []map[string]any{
		{"get": "http://127.0.0.1:1/", "aws_sigv4": sign, "headers": map[string]any{"Authorization": "Bearer x"}},
		{"get": "http://127.0.0.1:1/", "aws_sigv4": sign, "basic_auth": map[string]any{"username": "u", "password": "p"}},
	} {
		_, _, err := RequestStep(sdk.Call{With: with})
		if err == nil || !strings.Contains(err.Error(), "cannot be given together") {
			t.Errorf("err = %v, want aws_sigv4 refused with another Authorization", err)
		}
	}
}
