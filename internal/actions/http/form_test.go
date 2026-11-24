package http

import (
	"io"
	"maps"
	"mime"
	"mime/multipart"
	hp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRequestForm(t *testing.T) {
	var contentType string
	var got map[string][]string
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		contentType = r.Header.Get("Content-Type")
		if err := r.ParseForm(); err != nil {
			t.Errorf("server could not parse the form: %v", err)
		}
		got = r.PostForm
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ret, err := Request(map[string]any{
		"url":  srv.URL,
		"post": "/login",
		// A JSON content type set for the job's other requests is replaced.
		"headers": map[string]any{"Content-Type": "application/json"},
		"form": map[string]any{
			"user":     "alice",
			"password": "p@ss word&x",
			"remember": true,
			"age":      float64(30),
			"tags":     []any{"a", "b"},
			"note":     nil,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if contentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", contentType)
	}
	want := map[string][]string{
		"user":     {"alice"},
		"password": {"p@ss word&x"},
		"remember": {"true"},
		"age":      {"30"},
		"tags":     {"a", "b"},
		"note":     {""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("server saw %v, want %v", got, want)
	}

	req := ret["req"].(map[string]any)
	wantBody := "age=30&note=&password=p%40ss+word%26x&remember=true&tags=a&tags=b&user=alice"
	if req["body"] != wantBody {
		t.Errorf("req.body = %q, want %q", req["body"], wantBody)
	}
	if _, ok := req["form"]; ok {
		t.Error("form should not be carried into req")
	}
	headers := req["headers"].(map[string]string)
	if len(headers) != 3 || headers["content-type"] != "application/x-www-form-urlencoded" {
		t.Errorf("req should show the one Content-Type it sent, got %#v", headers)
	}
}

func TestRequestMultipart(t *testing.T) {
	dir := t.TempDir()
	logo := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(logo, []byte("\x89PNG-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(dir, "doc.bin")
	if err := os.WriteFile(doc, []byte("doc-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	type part struct {
		name, filename, contentType, data string
	}
	var parts []part
	var contentLength int64
	var transferEncoding []string
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		contentLength = r.ContentLength
		transferEncoding = r.TransferEncoding
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("not a multipart request: %v", err)
			return
		}
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("reading a part: %v", err)
				return
			}
			data, _ := io.ReadAll(p)
			ct := ""
			if p.FileName() != "" {
				ct = p.Header.Get("Content-Type")
			}
			parts = append(parts, part{p.FormName(), p.FileName(), ct, string(data)})
		}
		w.WriteHeader(hp.StatusCreated)
	}))
	defer srv.Close()

	spec := map[string]any{
		"image": map[string]any{"file": logo},
		"title": "hello",
		"attachments": []any{
			map[string]any{"file": doc},
			map[string]any{"content": "a,b\n1,2\n", "filename": "data.csv", "content_type": "text/csv"},
		},
		"tags":  []any{"x", float64(2)},
		"notes": map[string]any{"content": "inline"},
	}
	ret, err := Request(map[string]any{
		"url":       srv.URL,
		"post":      "/upload",
		"headers":   map[string]any{"content-type": "application/json"},
		"multipart": spec,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Text fields first and files after them, each sorted by name, and the
	// values of a list in their order.
	want := []part{
		{"tags", "", "", "x"},
		{"tags", "", "", "2"},
		{"title", "", "", "hello"},
		{"attachments", "doc.bin", "application/octet-stream", "doc-bytes"},
		{"attachments", "data.csv", "text/csv", "a,b\n1,2\n"},
		{"image", "logo.png", "image/png", "\x89PNG-bytes"},
		{"notes", "notes", "application/octet-stream", "inline"},
	}
	if !reflect.DeepEqual(parts, want) {
		t.Errorf("server saw parts\n%q\nwant\n%q", parts, want)
	}
	if contentLength <= 0 || len(transferEncoding) != 0 {
		t.Errorf("the body should be sent with its length, got Content-Length %d and Transfer-Encoding %v", contentLength, transferEncoding)
	}

	req := ret["req"].(map[string]any)
	if req["body"] != "" {
		t.Errorf("req.body should not hold the multipart body, got %q", req["body"])
	}
	if !reflect.DeepEqual(req["multipart"], spec) {
		t.Errorf("req.multipart = %#v, want what was written", req["multipart"])
	}
	headers := req["headers"].(map[string]string)
	mediaType, params, err := mime.ParseMediaType(headers["content-type"])
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		t.Errorf("req should show the multipart Content-Type it sent, got %#v", headers)
	}
	res := ret["res"].(map[string]any)
	if res["code"] != hp.StatusCreated {
		t.Errorf("res.code = %v", res["code"])
	}
}

// TestRequestMultipartReadsFileEachTime checks that a file is read when the
// request is made, so that a retried step sends the file as it is then.
func TestRequestMultipartReadsFileEachTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.txt")
	var got string
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		f, _, err := r.FormFile("report")
		if err != nil {
			t.Errorf("no file: %v", err)
			return
		}
		data, _ := io.ReadAll(f)
		got = string(data)
	}))
	defer srv.Close()

	for _, content := range []string{"first", "second"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Request(map[string]any{
			"url":       srv.URL,
			"post":      "/",
			"multipart": map[string]any{"report": map[string]any{"file": path}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != content {
			t.Errorf("server saw %q, want %q", got, content)
		}
	}
}

func TestRequestFormRejected(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.png")
	tests := []struct {
		name    string
		data    map[string]any
		wantErr string
	}{
		{
			name:    "form and multipart",
			data:    map[string]any{"form": map[string]any{}, "multipart": map[string]any{}},
			wantErr: "form and multipart cannot be given together",
		},
		{
			name:    "form and body",
			data:    map[string]any{"form": map[string]any{"a": "b"}, "body": "x"},
			wantErr: "form and body cannot be given together",
		},
		{
			name:    "multipart and body",
			data:    map[string]any{"multipart": map[string]any{"a": "b"}, "body": "x"},
			wantErr: "multipart and body cannot be given together",
		},
		{
			name:    "a form that is not a map",
			data:    map[string]any{"form": "a=b"},
			wantErr: "form must be a map of field names and values",
		},
		{
			name:    "a form value that is a map",
			data:    map[string]any{"form": map[string]any{"a": map[string]any{"b": "c"}}},
			wantErr: "form.a must be a string, a number or a boolean",
		},
		{
			name:    "a multipart that is not a map",
			data:    map[string]any{"multipart": []any{"a"}},
			wantErr: "multipart must be a map of field names and values",
		},
		{
			name:    "a multipart value in a nested list",
			data:    map[string]any{"multipart": map[string]any{"a": []any{[]any{"b"}}}},
			wantErr: "multipart.a must be a string, a number or a boolean",
		},
		{
			name:    "file and content",
			data:    map[string]any{"multipart": map[string]any{"a": map[string]any{"file": "x", "content": "y"}}},
			wantErr: "multipart.a takes file or content, not both",
		},
		{
			name:    "neither file nor content",
			data:    map[string]any{"multipart": map[string]any{"a": map[string]any{"filename": "x.txt"}}},
			wantErr: "multipart.a needs file or content",
		},
		{
			name:    "an unknown key",
			data:    map[string]any{"multipart": map[string]any{"a": map[string]any{"path": "x"}}},
			wantErr: "multipart.a takes file, content, filename and content_type, not path",
		},
		{
			name:    "a file that does not exist",
			data:    map[string]any{"multipart": map[string]any{"a": map[string]any{"file": missing}}},
			wantErr: "multipart.a: open " + missing + ": no such file or directory",
		},
		{
			name:    "a file that is not a string",
			data:    map[string]any{"multipart": map[string]any{"a": map[string]any{"file": float64(1)}}},
			wantErr: "multipart.a.file must be a path",
		},
		{
			name:    "a filename that is not a string",
			data:    map[string]any{"multipart": map[string]any{"a": map[string]any{"content": "x", "filename": true}}},
			wantErr: "multipart.a.filename must be a string",
		},
		{
			name:    "a content_type with a line break",
			data:    map[string]any{"multipart": map[string]any{"a": map[string]any{"content": "x", "content_type": "text/plain\r\nX-Injected: 1"}}},
			wantErr: "multipart.a.content_type must not contain a line break",
		},
		{
			name:    "an empty content_type",
			data:    map[string]any{"multipart": map[string]any{"a": map[string]any{"content": "x", "content_type": ""}}},
			wantErr: "multipart.a.content_type must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
				called = true
			}))
			defer srv.Close()

			data := map[string]any{"url": srv.URL, "method": "POST"}
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

func TestEncodeMultipartEscapesNames(t *testing.T) {
	body, contentType, err := encodeMultipart(map[string]any{
		`a"b`:       map[string]any{"content": "x", "filename": `c"d\e.txt`},
		"t\r\nX: 1": "v",
		"f\r\nX: 2": map[string]any{"content": "x", "filename": "n\r\nX: 3"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		`name="a\"b"; filename="c\"d\\e.txt"`,
		`name="t%0D%0AX: 1"`,
		`name="f%0D%0AX: 2"; filename="n%0D%0AX: 3"`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("names should be escaped in Content-Disposition as %s, got %q", want, body)
		}
	}
	if strings.Contains(string(body), "\r\nX: ") {
		t.Errorf("a line break in a name should not begin a header, got %q", body)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data; boundary=") {
		t.Errorf("contentType = %q", contentType)
	}
}

// TestEncodeMultipartMixedList checks that a list holding both text values
// and files sends its text values first, each keeping their order.
func TestEncodeMultipartMixedList(t *testing.T) {
	body, contentType, err := encodeMultipart(map[string]any{
		"a": []any{
			map[string]any{"content": "file1"},
			"text1",
			map[string]any{"content": "file2"},
			"text2",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, params, _ := mime.ParseMediaType(contentType)
	mr := multipart.NewReader(strings.NewReader(string(body)), params["boundary"])
	var got []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(p)
		got = append(got, string(data))
	}
	want := []string{"text1", "text2", "file1", "file2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parts = %q, want %q", got, want)
	}
}

// TestRequestKeepsCallerHeaders checks that the headers a caller passes are
// not changed by the headers basic_auth, form and multipart add or replace.
func TestRequestKeepsCallerHeaders(t *testing.T) {
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {}))
	defer srv.Close()

	for _, extra := range []map[string]any{
		{"basic_auth": map[string]any{"username": "a", "password": "b"}},
		{"form": map[string]any{"a": "b"}},
		{"multipart": map[string]any{"a": "b"}},
	} {
		headers := map[string]string{"Content-Type": "application/json"}
		data := map[string]any{"url": srv.URL, "post": "/", "headers": headers}
		maps.Copy(data, extra)
		if _, err := Request(data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := map[string]string{"Content-Type": "application/json"}
		if !reflect.DeepEqual(headers, want) {
			t.Errorf("with %v, the caller's headers became %v", extra, headers)
		}
	}
}
