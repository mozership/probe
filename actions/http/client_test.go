package http

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	hp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/jarcoal/httpmock"
	"github.com/mozership/probe/actionrpc"
)

func TestNewReq(t *testing.T) {
	got := NewReq()

	expects := &Req{
		URL:    "",
		Method: "GET",
		Header: map[string]string{
			"Accept":     "*/*",
			"User-Agent": "probe-http/1.0.0",
		},
		Timeout: "30s",
	}

	if !reflect.DeepEqual(got, expects) {
		t.Errorf("\nExpected:\n%#v\nGot:\n%#v", expects, got)
	}
}

func TestDo(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	res := httpmock.NewStringResponder(200, "Hello World\n")
	httpmock.RegisterResponder("GET", "http://localhost:8080/foo/bar", res)

	req := NewReq()
	req.URL = "http://localhost:8080/foo/bar"

	got, err := req.Do()
	if err != nil {
		t.Errorf("got error %s", err)
	}

	expects := "Hello World\n"

	if string(got.Res.Body) != expects {
		t.Errorf("\nExpected:\n%s\nGot:\n%s", expects, got.Res.Body)
	}

	// Check that RT field is populated
	if got.RT <= 0 {
		t.Errorf("RT should be greater than 0, got: %v", got.RT)
	}
}

func TestResolveMethodAndURL(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
		wantErr  bool
	}{
		{
			name: "GET method with relative path",
			input: map[string]any{
				"get": "/users",
				"url": "https://api.example.com",
			},
			expected: map[string]any{
				"method": "GET",
				"url":    "https://api.example.com/users",
			},
			wantErr: false,
		},
		{
			name: "POST method with complete HTTPS URL",
			input: map[string]any{
				"post": "https://api.example.com/users",
			},
			expected: map[string]any{
				"method": "POST",
				"url":    "https://api.example.com/users",
			},
			wantErr: false,
		},
		{
			name: "GET method with relative path and query parameters",
			input: map[string]any{
				"get": "/search?q=hello&page=1",
				"url": "https://api.example.com",
			},
			expected: map[string]any{
				"method": "GET",
				"url":    "https://api.example.com/search?q=hello&page=1",
			},
			wantErr: false,
		},
		{
			name: "POST method with relative path and query parameters",
			input: map[string]any{
				"post": "/users?active=true",
				"url":  "https://api.example.com/v2",
			},
			expected: map[string]any{
				"method": "POST",
				"url":    "https://api.example.com/v2/users?active=true",
			},
			wantErr: false,
		},
		{
			name: "GET method with base URL and route both containing query parameters",
			input: map[string]any{
				"get": "/search?q=hello&page=1",
				"url": "https://api.example.com?token=abc",
			},
			expected: map[string]any{
				"method": "GET",
				// The route's query parameters overwrite the base URL's query.
				"url": "https://api.example.com/search?q=hello&page=1",
			},
			wantErr: false,
		},
		{
			name: "GET method with complete URL containing query parameters",
			input: map[string]any{
				"get": "https://api.example.com/search?q=hello&page=1",
			},
			expected: map[string]any{
				"method": "GET",
				"url":    "https://api.example.com/search?q=hello&page=1",
			},
			wantErr: false,
		},
		{
			name: "Missing URL with relative path",
			input: map[string]any{
				"get": "/users",
			},
			expected: map[string]any{
				"get": "/users",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ResolveMethodAndURL(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			for key, expectedValue := range tt.expected {
				actualValue, exists := tt.input[key]
				if !exists {
					t.Errorf("Expected key %s not found in result", key)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("Expected %s=%s, got %s=%s", key, expectedValue, key, actualValue)
				}
			}
		})
	}
}

func TestHasJSONContentType(t *testing.T) {
	tests := []struct {
		name     string
		headers  any
		expected bool
	}{
		{
			name:     "map[string]any with content-type lowercase",
			headers:  map[string]any{"content-type": "application/json"},
			expected: true,
		},
		{
			name:     "map[string]any with Content-Type capitalized",
			headers:  map[string]any{"Content-Type": "application/json"},
			expected: true,
		},
		{
			name:     "map[string]string with content-type",
			headers:  map[string]string{"content-type": "application/json"},
			expected: true,
		},
		{
			name:     "map[string]interface{} with content-type",
			headers:  map[string]any{"content-type": "application/json"},
			expected: true,
		},
		{
			name:     "json with a charset",
			headers:  map[string]any{"Content-Type": "application/json; charset=utf-8"},
			expected: true,
		},
		{
			name:     "json in another case",
			headers:  map[string]any{"Content-Type": "Application/JSON"},
			expected: true,
		},
		{
			name:     "structured syntax suffix",
			headers:  map[string]any{"Content-Type": "application/problem+json"},
			expected: true,
		},
		{
			name:     "non-json content-type",
			headers:  map[string]any{"content-type": "text/plain"},
			expected: false,
		},
		{
			name:     "json only as a parameter",
			headers:  map[string]any{"content-type": "text/plain; format=application/json"},
			expected: false,
		},
		{
			name:     "no content-type header",
			headers:  map[string]any{"accept": "application/json"},
			expected: false,
		},
		{
			name:     "nil headers",
			headers:  nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasJSONContentType(tt.headers)
			if result != tt.expected {
				t.Errorf("hasJSONContentType() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMarshalBodyIfJSON(t *testing.T) {
	tests := []struct {
		name         string
		data         map[string]any
		expectedBody any
	}{
		{
			name: "marshal array body",
			data: map[string]any{
				"headers": map[string]any{"content-type": "application/json"},
				"body":    []any{map[string]any{"id": 1}, map[string]any{"id": 2}},
			},
			expectedBody: `[{"id":1},{"id":2}]`,
		},
		{
			name: "marshal map body",
			data: map[string]any{
				"headers": map[string]any{"content-type": "application/json"},
				"body":    map[string]any{"name": "test"},
			},
			expectedBody: `{"name":"test"}`,
		},
		{
			name: "string body unchanged",
			data: map[string]any{
				"headers": map[string]any{"content-type": "application/json"},
				"body":    `{"already":"json"}`,
			},
			expectedBody: nil, // not modified
		},
		{
			name: "non-json content-type unchanged",
			data: map[string]any{
				"headers": map[string]any{"content-type": "text/plain"},
				"body":    []any{map[string]any{"id": 1}},
			},
			expectedBody: nil, // not modified
		},
		{
			name: "no body",
			data: map[string]any{
				"headers": map[string]any{"content-type": "application/json"},
			},
			expectedBody: nil, // not modified
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := make(map[string]any)
			MarshalBodyIfJSON(tt.data, m)

			if tt.expectedBody == nil {
				if _, exists := m["body"]; exists {
					t.Errorf("MarshalBodyIfJSON() should not set body, got %v", m["body"])
				}
			} else {
				body, exists := m["body"]
				if !exists {
					t.Errorf("MarshalBodyIfJSON() should set body")
					return
				}
				// Compare JSON
				var expected, actual any
				if err := json.Unmarshal([]byte(tt.expectedBody.(string)), &expected); err != nil {
					t.Fatalf("Failed to parse expected JSON: %v", err)
				}
				if err := json.Unmarshal([]byte(body.(string)), &actual); err != nil {
					t.Fatalf("Failed to parse actual JSON: %v", err)
				}
				expectedBytes, _ := json.Marshal(expected)
				actualBytes, _ := json.Marshal(actual)
				if string(expectedBytes) != string(actualBytes) {
					t.Errorf("MarshalBodyIfJSON() = %v, want %v", body, tt.expectedBody)
				}
			}
		})
	}
}

func TestRequestBodyJSONConversion(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	tests := []struct {
		name         string
		data         map[string]any
		expectedBody string
	}{
		{
			name: "array body with map[string]any headers",
			data: map[string]any{
				"url":    "http://localhost:8080/api",
				"method": "POST",
				"headers": map[string]any{
					"content-type": "application/json",
				},
				"body": []any{
					map[string]any{"name": "metric1", "value": 1.5, "time": 1234567890},
					map[string]any{"name": "metric2", "value": 2.5, "time": 1234567891},
				},
			},
			expectedBody: `[{"name":"metric1","time":1234567890,"value":1.5},{"name":"metric2","time":1234567891,"value":2.5}]`,
		},
		{
			name: "array body with map[string]string headers",
			data: map[string]any{
				"url":    "http://localhost:8080/api",
				"method": "POST",
				"headers": map[string]string{
					"content-type": "application/json",
				},
				"body": []any{
					map[string]any{"name": "test", "value": 100},
				},
			},
			expectedBody: `[{"name":"test","value":100}]`,
		},
		{
			name: "array body with map[string]interface{} headers",
			data: map[string]any{
				"url":    "http://localhost:8080/api",
				"method": "POST",
				"headers": map[string]any{
					"content-type": "application/json",
				},
				"body": []any{
					map[string]any{"id": 1},
				},
			},
			expectedBody: `[{"id":1}]`,
		},
		{
			name: "array body with Content-Type (capital case)",
			data: map[string]any{
				"url":    "http://localhost:8080/api",
				"method": "POST",
				"headers": map[string]any{
					"Content-Type": "application/json",
				},
				"body": []any{
					map[string]any{"key": "value"},
				},
			},
			expectedBody: `[{"key":"value"}]`,
		},
		{
			name: "map body with application/json",
			data: map[string]any{
				"url":    "http://localhost:8080/api",
				"method": "POST",
				"headers": map[string]any{
					"content-type": "application/json",
				},
				"body": map[string]any{
					"name":  "test",
					"value": 42,
				},
			},
			expectedBody: `{"name":"test","value":42}`,
		},
		{
			name: "string body should not be converted",
			data: map[string]any{
				"url":    "http://localhost:8080/api",
				"method": "POST",
				"headers": map[string]any{
					"content-type": "application/json",
				},
				"body": `{"already":"json"}`,
			},
			expectedBody: `{"already":"json"}`,
		},
		{
			name: "non-json content-type should not convert body",
			data: map[string]any{
				"url":    "http://localhost:8080/api",
				"method": "POST",
				"headers": map[string]any{
					"content-type": "text/plain",
				},
				"body": "plain text body",
			},
			expectedBody: "plain text body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody string
			httpmock.Reset()
			httpmock.RegisterResponder("POST", "http://localhost:8080/api",
				func(req *hp.Request) (*hp.Response, error) {
					body, _ := io.ReadAll(req.Body)
					capturedBody = string(body)
					return httpmock.NewStringResponse(200, "OK"), nil
				})

			_, err := Request(tt.data)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}

			// For JSON bodies, parse and compare to handle key ordering differences
			if strings.HasPrefix(tt.expectedBody, "[") || strings.HasPrefix(tt.expectedBody, "{") {
				var expectedJSON, actualJSON any
				if err := json.Unmarshal([]byte(tt.expectedBody), &expectedJSON); err == nil {
					if err := json.Unmarshal([]byte(capturedBody), &actualJSON); err == nil {
						expectedBytes, _ := json.Marshal(expectedJSON)
						actualBytes, _ := json.Marshal(actualJSON)
						if string(expectedBytes) != string(actualBytes) {
							t.Errorf("Body mismatch:\nExpected: %s\nGot: %s", tt.expectedBody, capturedBody)
						}
						return
					}
				}
			}

			if capturedBody != tt.expectedBody {
				t.Errorf("Body mismatch:\nExpected: %s\nGot: %s", tt.expectedBody, capturedBody)
			}
		})
	}
}

func TestJoinHeader(t *testing.T) {
	tests := []struct {
		name   string
		header hp.Header
		want   map[string]string
	}{
		{"none", hp.Header{}, map[string]string{}},
		{"one value", hp.Header{"Content-Type": {"text/plain"}}, map[string]string{"Content-Type": "text/plain"}},
		{
			"values joined",
			hp.Header{"Accept": {"text/html", "application/xml;q=0.9"}, "Set-Cookie": {"a=1; Path=/", "b=2"}},
			map[string]string{"Accept": "text/html, application/xml;q=0.9", "Set-Cookie": "a=1; Path=/, b=2"},
		},
		{"no values", hp.Header{"X-Empty": {}}, map[string]string{"X-Empty": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := joinHeader(tt.header); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("joinHeader() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasHeader(t *testing.T) {
	tests := []struct {
		name   string
		header map[string]string
		key    string
		want   bool
	}{
		{"nil", nil, "authorization", false},
		{"same case", map[string]string{"authorization": "x"}, "authorization", true},
		{"other case", map[string]string{"Authorization": "x"}, "authorization", true},
		{"other header", map[string]string{"X-Auth": "x"}, "authorization", false},
		{"empty value counts", map[string]string{"X-Trace": ""}, "x-trace", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasHeader(tt.header, tt.key); got != tt.want {
				t.Errorf("hasHeader(%v, %q) = %v, want %v", tt.header, tt.key, got, tt.want)
			}
		})
	}
}

func TestSetHeader(t *testing.T) {
	tests := []struct {
		name   string
		header map[string]string
		want   map[string]string
	}{
		{"nil is made", nil, map[string]string{"content-type": "text/csv"}},
		{"added", map[string]string{"Accept": "*/*"}, map[string]string{"Accept": "*/*", "content-type": "text/csv"}},
		{"same case replaced", map[string]string{"content-type": "application/json"}, map[string]string{"content-type": "text/csv"}},
		{
			"every other case replaced",
			map[string]string{"Content-Type": "application/json", "CONTENT-TYPE": "text/plain", "Accept": "*/*"},
			map[string]string{"content-type": "text/csv", "Accept": "*/*"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := setHeader(tt.header, "content-type", "text/csv"); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("setHeader() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeHeaders(t *testing.T) {
	tests := []struct {
		name           string
		defaultHeaders map[string]string
		customHeaders  map[string]string
		expected       map[string]string
	}{
		{
			name: "no custom headers",
			defaultHeaders: map[string]string{
				"Accept":     "*/*",
				"User-Agent": "probe-http/1.0.0",
			},
			customHeaders: nil,
			expected: map[string]string{
				"Accept":     "*/*",
				"User-Agent": "probe-http/1.0.0",
			},
		},
		{
			name: "override user-agent lowercase",
			defaultHeaders: map[string]string{
				"Accept":     "*/*",
				"User-Agent": "probe-http/1.0.0",
			},
			customHeaders: map[string]string{
				"user-agent": "custom-agent/1.0",
			},
			expected: map[string]string{
				"Accept":     "*/*",
				"user-agent": "custom-agent/1.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeHeaders(tt.defaultHeaders, tt.customHeaders)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d headers, got %d", len(tt.expected), len(result))
			}

			for expectedKey, expectedValue := range tt.expected {
				actualValue, exists := result[expectedKey]
				if !exists {
					t.Errorf("Expected header %s not found", expectedKey)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("Expected %s = %s, got %s", expectedKey, expectedValue, actualValue)
				}
			}

			// Check for duplicates (case-insensitive)
			userAgentCount := 0
			for key := range result {
				if strings.ToLower(key) == "user-agent" {
					userAgentCount++
				}
			}
			if userAgentCount > 1 {
				t.Errorf("Found %d User-Agent headers, expected only 1. Headers: %v", userAgentCount, result)
			}
		})
	}
}

// TestRequest_DoesNotLeakRequestDataToDefaultLogger guards against the
// pre-existing "DEBUG: ..." hclog.Info calls in Req.Do and Request that
// emitted request bodies and headers (including Authorization) to stderr
// regardless of verbose mode. The test installs a recording logger via
// hclog.SetDefault and asserts that no sentinel content from a request
// reaches it.
func TestRequest_DoesNotLeakRequestDataToDefaultLogger(t *testing.T) {
	var buf bytes.Buffer
	recorder := hclog.New(&hclog.LoggerOptions{
		Name:   "http-test-recorder",
		Output: &buf,
		Level:  hclog.Trace,
	})
	old := hclog.SetDefault(recorder)
	t.Cleanup(func() { hclog.SetDefault(old) })

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder("POST", "http://example.test/v1/auth",
		httpmock.NewStringResponder(200, "ok"))

	const (
		sentinelBody = "secret-payload-must-not-leak"
		sentinelAuth = "Bearer SUPER-SECRET-TOKEN"
	)

	if _, err := Request(map[string]any{
		"url":    "http://example.test/v1/auth",
		"method": "POST",
		"body":   sentinelBody,
		"headers": map[string]any{
			"Authorization": sentinelAuth,
		},
	}); err != nil {
		t.Fatalf("Request: %v", err)
	}

	out := buf.String()
	for _, sentinel := range []string{"DEBUG:", sentinelBody, sentinelAuth} {
		if strings.Contains(out, sentinel) {
			t.Errorf("default logger leaked %q.\nlogger output:\n%s", sentinel, out)
		}
	}
}

func TestParseTimeout(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		expects time.Duration
		wantErr bool
	}{
		{"empty falls back to the default", "", DefaultTimeout, false},
		{"duration string", "10s", 10 * time.Second, false},
		{"compound duration string", "1m30s", 90 * time.Second, false},
		{"milliseconds", "250ms", 250 * time.Millisecond, false},
		{"bare number is seconds", "30", 30 * time.Second, false},
		{"fractional number is seconds", "0.5", 500 * time.Millisecond, false},
		{"surrounding spaces are ignored", " 5s ", 5 * time.Second, false},
		{"zero disables the limit", "0", 0, false},
		{"zero duration disables the limit", "0s", 0, false},
		{"negative duration is rejected", "-1s", 0, true},
		{"negative number is rejected", "-1", 0, true},
		{"unparsable value is rejected", "soon", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTimeout(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %s", tt.input, err)
			}
			if got != tt.expects {
				t.Errorf("expected %v, got %v", tt.expects, got)
			}
		})
	}
}

func TestDoTimesOutSlowResponse(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		<-blocked
	}))
	defer func() {
		close(blocked)
		srv.Close()
	}()

	req := NewReq()
	req.URL = srv.URL
	req.Timeout = "50ms"

	start := time.Now()
	_, err := req.Do()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the request to time out")
	}
	if !strings.Contains(err.Error(), "Client.Timeout") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected a timeout error, got: %s", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("expected the request to give up quickly, took %v", elapsed)
	}
}

func TestDoCompletesWithinTimeout(t *testing.T) {
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	req := NewReq()
	req.URL = srv.URL
	req.Timeout = "5s"

	got, err := req.Do()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got.Res.Code != 200 {
		t.Errorf("expected 200, got %d", got.Res.Code)
	}
}

func TestDoReadsStructuredSyntaxBodiesAsText(t *testing.T) {
	tests := []struct {
		contentType string
		body        string
	}{
		{"application/problem+json", `{"title":"Not Found","status":404}`},
		{"application/vnd.api+json; charset=utf-8", `{"data":[]}`},
		{"application/soap+xml", `<Envelope/>`},
		{"image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg"/>`},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(hp.StatusNotFound)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			req := NewReq()
			req.URL = srv.URL

			got, err := req.Do()
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if got.Res.Body != tt.body {
				t.Errorf("expected the body %q, got %q", tt.body, got.Res.Body)
			}
			if got.Res.FilePath != "" {
				t.Errorf("expected no file, got %q", got.Res.FilePath)
			}
		})
	}
}

func TestDoRejectsInvalidTimeout(t *testing.T) {
	req := NewReq()
	req.URL = "http://localhost:8080/"
	req.Timeout = "soon"

	if _, err := req.Do(); err == nil {
		t.Fatal("expected an error for an invalid timeout")
	}
}

func TestRequestPassesTimeoutFromParams(t *testing.T) {
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	got, err := Request(map[string]any{
		"url":     srv.URL,
		"method":  "GET",
		"timeout": "7s",
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	req, ok := got["req"].(map[string]any)
	if !ok {
		t.Fatalf("expected req in the result, got %#v", got["req"])
	}
	if req["timeout"] != "7s" {
		t.Errorf("expected the timeout to be carried through, got %#v", req["timeout"])
	}
}

func TestRequestUsesDefaultTimeoutWhenOmitted(t *testing.T) {
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	got, err := Request(map[string]any{
		"url":    srv.URL,
		"method": "GET",
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	req, ok := got["req"].(map[string]any)
	if !ok {
		t.Fatalf("expected req in the result, got %#v", got["req"])
	}
	if req["timeout"] != DefaultTimeout.String() {
		t.Errorf("expected the default timeout, got %#v", req["timeout"])
	}
}

func TestRequestAcceptsNumericTimeout(t *testing.T) {
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	if _, err := Request(map[string]any{
		"url":     srv.URL,
		"method":  "GET",
		"timeout": 5,
	}); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestRequestBasicAuth(t *testing.T) {
	type seen struct {
		user, pass string
		ok         bool
	}
	tests := []struct {
		name string
		auth any
		want seen
	}{
		{
			name: "username and password",
			auth: map[string]any{"username": "alice@example.test", "password": "s3cr:et"},
			want: seen{"alice@example.test", "s3cr:et", true},
		},
		{
			name: "a password written as a number",
			auth: map[string]any{"username": "bob", "password": uint64(123456)},
			want: seen{"bob", "123456", true},
		},
		{
			name: "a password decoded as a float",
			auth: map[string]any{"username": "bob", "password": float64(1000000)},
			want: seen{"bob", "1000000", true},
		},
		{
			name: "no password",
			auth: map[string]any{"username": "carol"},
			want: seen{"carol", "", true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got seen
			srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
				got.user, got.pass, got.ok = r.BasicAuth()
				_, _ = w.Write([]byte("ok"))
			}))
			defer srv.Close()

			ret, err := Request(map[string]any{
				"url":        srv.URL,
				"get":        "/",
				"basic_auth": tt.auth,
				"headers":    map[string]any{"accept": "text/plain"},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("server saw %+v, want %+v", got, tt.want)
			}

			req := ret["req"].(map[string]any)
			if _, ok := req["basic_auth"]; ok {
				t.Error("basic_auth should not be carried into req")
			}
			headers := req["headers"].(map[string]string)
			if !strings.HasPrefix(headers["authorization"], "Basic ") {
				t.Errorf("req should show the header it sent, got %#v", headers)
			}
			if headers["accept"] != "text/plain" {
				t.Errorf("other headers should be kept, got %#v", headers)
			}
		})
	}
}

func TestRequestBasicAuthRejected(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]any
		wantErr string
	}{
		{
			name:    "with an authorization header",
			data:    map[string]any{"basic_auth": map[string]any{"username": "a"}, "headers": map[string]any{"Authorization": "Bearer x"}},
			wantErr: "basic_auth and an authorization header cannot be given together",
		},
		{
			name:    "not a map",
			data:    map[string]any{"basic_auth": "alice:pw"},
			wantErr: "basic_auth must be a map of username and password",
		},
		{
			name:    "no username",
			data:    map[string]any{"basic_auth": map[string]any{"password": "pw"}},
			wantErr: "basic_auth.username is required",
		},
		{
			name:    "a colon in the username",
			data:    map[string]any{"basic_auth": map[string]any{"username": "a:b", "password": "pw"}},
			wantErr: "basic_auth.username must not contain a colon",
		},
		{
			name:    "an unknown key",
			data:    map[string]any{"basic_auth": map[string]any{"user": "a", "password": "pw"}},
			wantErr: "basic_auth takes username and password, not user",
		},
		{
			name:    "a password that is a map",
			data:    map[string]any{"basic_auth": map[string]any{"username": "a", "password": map[string]any{}}},
			wantErr: "basic_auth.password must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
				called = true
			}))
			defer srv.Close()

			data := map[string]any{"url": srv.URL, "method": "GET"}
			maps.Copy(data, tt.data)
			_, err := Request(data)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
			if called {
				t.Error("the request should not be sent")
			}
		})
	}
}

func TestRequestStepUnderAGuard(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		hits.Add(1)
		if r.URL.Path == "/away" {
			// localhost is the server's own address by another name.
			hp.Redirect(w, r, strings.Replace(srvURL(r), "127.0.0.1", "localhost", 1)+"/ok", hp.StatusFound)
			return
		}
		w.WriteHeader(hp.StatusOK)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	tests := []struct {
		name     string
		guard    actionrpc.Guard
		with     map[string]any
		refused  string // a part of the reason; empty when the request is sent
		wantHits int32
	}{
		{name: "GET under read-only", guard: actionrpc.Guard{ReadOnly: true}, with: map[string]any{"get": "/ok"}, wantHits: 1},
		{name: "HEAD under read-only", guard: actionrpc.Guard{ReadOnly: true}, with: map[string]any{"method": "HEAD"}, wantHits: 1},
		{name: "POST under read-only", guard: actionrpc.Guard{ReadOnly: true}, with: map[string]any{"post": "/items"}, refused: "POST " + srv.URL + "/items may write"},
		{name: "DELETE under read-only", guard: actionrpc.Guard{ReadOnly: true}, with: map[string]any{"delete": "/items/1"}, refused: "DELETE"},
		{name: "POST without a guard", with: map[string]any{"post": "/items"}, wantHits: 1},
		{name: "a host allowed", guard: actionrpc.Guard{AllowHosts: []string{"127.0.0.1"}}, with: map[string]any{"get": "/ok"}, wantHits: 1},
		{name: "a host and port allowed", guard: actionrpc.Guard{AllowHosts: []string{host}}, with: map[string]any{"get": "/ok"}, wantHits: 1},
		{name: "a host not allowed", guard: actionrpc.Guard{AllowHosts: []string{"api.example.com"}}, with: map[string]any{"get": "/ok"}, refused: "the host " + host + " is not one the run allows"},
		{name: "a redirect to a host not allowed", guard: actionrpc.Guard{AllowHosts: []string{"127.0.0.1"}}, with: map[string]any{"get": "/away"}, refused: "the host localhost:", wantHits: 1},
		{name: "a redirect without a guard", with: map[string]any{"get": "/away"}, wantHits: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits.Store(0)
			with := map[string]any{"url": srv.URL}
			maps.Copy(with, tt.with)
			_, _, err := RequestStep(actionrpc.Call{With: with, Guard: tt.guard})
			if tt.refused == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), tt.refused) {
				t.Fatalf("err = %v, want a refusal saying %q", err, tt.refused)
			}
			if n := hits.Load(); n != tt.wantHits {
				t.Errorf("the server was sent %d requests, want %d", n, tt.wantHits)
			}
		})
	}
}

func TestCheckGuardTakesTheSchemePort(t *testing.T) {
	tests := []struct {
		url   string
		allow string
		want  bool
	}{
		{"http://api.example.com/x", "api.example.com:80", true},
		{"https://api.example.com/x", "api.example.com:443", true},
		{"https://api.example.com/x", "api.example.com:80", false},
		{"https://api.example.com:8443/x", "api.example.com:8443", true},
	}
	for _, tt := range tests {
		req, err := hp.NewRequest(hp.MethodGet, tt.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		r := &Req{guard: actionrpc.Guard{AllowHosts: []string{tt.allow}}}
		if err := r.checkGuard(req); (err == nil) != tt.want {
			t.Errorf("checkGuard(%s) under %s = %v, want allowed %v", tt.url, tt.allow, err, tt.want)
		}
	}
}

// srvURL is the address the request r was sent to.
func srvURL(r *hp.Request) string {
	return "http://" + r.Host
}
