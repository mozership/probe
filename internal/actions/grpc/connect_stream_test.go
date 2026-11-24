package grpc

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestConnectStreamOfABrokenAnswer(t *testing.T) {
	envelope := func(flags byte, data string) []byte {
		var b bytes.Buffer
		writeEnvelope(&b, flags, []byte(data))
		return b.Bytes()
	}
	user := `{"id": "1", "name": "User 1"}`

	tests := []struct {
		name         string
		status       int
		contentType  string
		body         []byte
		wantCode     string
		wantMessage  string
		wantMessages int
		wantComplete any
	}{
		{
			name:         "a stream without its end message",
			status:       http.StatusOK,
			contentType:  "application/connect+json",
			body:         envelope(0, user),
			wantCode:     "INTERNAL",
			wantMessage:  "without its end message",
			wantMessages: 1,
			wantComplete: false,
		},
		{
			name:         "an end message that is not JSON",
			status:       http.StatusOK,
			contentType:  "application/connect+json",
			body:         append(envelope(0, user), envelope(connectEnvelopeEndStream, "{")...),
			wantCode:     "INTERNAL",
			wantMessage:  "is not JSON",
			wantMessages: 1,
			wantComplete: true,
		},
		{
			name:        "a call refused before its stream",
			status:      http.StatusNotFound,
			contentType: "text/plain",
			body:        []byte("404 page not found"),
			wantCode:    "UNIMPLEMENTED",
			wantMessage: "404 Not Found",
		},
		{
			name:        "an answer that is no Connect stream",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        []byte(user),
			wantCode:    "UNKNOWN",
			wantMessage: "invalid content-type",
		},
		{
			name:        "a stream in another codec",
			status:      http.StatusOK,
			contentType: "application/connect+proto",
			body:        nil,
			wantCode:    "INTERNAL",
			wantMessage: "invalid content-type",
		},
	}
	files := protoOf(writeProto(t, userProto(t)), false)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = w.Write(tt.body)
			}))
			t.Cleanup(srv.Close)
			_, res, err := streamCall(t, map[string]any{
				"protocol": "connect", "addr": srv.URL, "method": "WatchUsers", "body": `{"count": 1}`, "proto": files,
			})
			if err != nil {
				t.Fatalf("Request() error: %v, want the answer as a result", err)
			}
			if res["status_code"] != tt.wantCode || !strings.Contains(res["status_message"].(string), tt.wantMessage) {
				t.Errorf("status = %v %q, want %s with %q", res["status_code"], res["status_message"], tt.wantCode, tt.wantMessage)
			}
			if got := len(messageIDs(res)); got != tt.wantMessages {
				t.Errorf("messages = %v, want %d", res["messages"], tt.wantMessages)
			}
			if tt.wantComplete != nil && res["complete"] != tt.wantComplete {
				t.Errorf("complete = %v, want %v", res["complete"], tt.wantComplete)
			}
		})
	}
}

func TestReadEnvelopeOfALengthNoMessageFollows(t *testing.T) {
	// The length says 4 GiB, but three bytes follow, which is all the
	// memory the read may take.
	r := bytes.NewReader([]byte{0, 0xff, 0xff, 0xff, 0xff, 'a', 'b', 'c'})
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, _, err := readEnvelope(r)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("readEnvelope() error = %v, want io.ErrUnexpectedEOF", err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Errorf("readEnvelope() allocated %d bytes for three", allocated)
	}
}

func TestConnectStreamOfAnEmptyAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/connect+json")
	}))
	t.Cleanup(srv.Close)
	_, _, err := streamCall(t, map[string]any{
		"protocol": "connect", "addr": srv.URL, "method": "WatchUsers", "body": `{"count": 1}`,
		"proto": protoOf(writeProto(t, userProto(t)), false),
	})
	if err == nil || !strings.Contains(err.Error(), "without any message or its end message") {
		t.Errorf("Request() error = %v, want one saying the stream held nothing", err)
	}
}

func TestConnectStreamSendsTheProtocolVersion(t *testing.T) {
	s := startConnectServer(t, "")
	if _, _, err := streamCall(t, map[string]any{
		"protocol": "connect", "addr": s.url, "method": "WatchUsers", "body": `{"count": 1}`,
		"proto": protoOf(writeProto(t, userProto(t)), false),
	}); err != nil {
		t.Fatalf("Request() error: %v", err)
	}
	h, _ := s.header.Load().(http.Header)
	if h.Get("Connect-Protocol-Version") != "1" {
		t.Errorf("Connect-Protocol-Version = %q, want 1", h.Get("Connect-Protocol-Version"))
	}
}
