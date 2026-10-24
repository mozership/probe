package mask

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	env := map[string]string{
		"TOKEN":   "tok-123",
		"LONG":    "tok-123-and-more",
		"EMPTY":   "",
		"UNUSED":  "not-declared",
		"PASS":    "hunter2",
		"PASSDUP": "hunter2",
	}
	m := New([]string{"TOKEN", "LONG", "EMPTY", "MISSING", "PASS", "PASSDUP"}, env)

	tests := []struct {
		in, want string
	}{
		{"token=tok-123", "token=<secret:TOKEN>"},
		{"tok-123-and-more", "<secret:LONG>"},
		{"pw hunter2", "pw <secret:PASS>"},
		{"not-declared stays", "not-declared stays"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := m.String(tt.in); got != tt.want {
			t.Errorf("String(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMasker_NilAndEmpty(t *testing.T) {
	var nilMasker *Masker
	if got := nilMasker.String("x"); got != "x" {
		t.Errorf("nil String = %q", got)
	}
	nilMasker.Learn(map[string]any{"authorization": "a"})

	var buf bytes.Buffer
	if w := nilMasker.Writer(&buf); w != &buf {
		t.Error("a nil masker should return the writer unchanged")
	}

	empty := New(nil, nil)
	if got := empty.String("anything"); got != "anything" {
		t.Errorf("empty String = %q", got)
	}
}

func TestEscapedForms(t *testing.T) {
	value := `a"b\c`
	forms := escapedForms(value)

	goQuoted := strconv.Quote(value)
	js, _ := json.Marshal(value)
	for _, want := range []string{
		value,
		goQuoted[1 : len(goQuoted)-1], // a\"b\\c
		string(js[1 : len(js)-1]),     // a\"b\\c
		`a\"b\c`,                      // a log line escaping only quotes
		`a\\"b\\c`,                    // Go quoted, then quotes escaped by the logger
		`a\\\"b\\\\c`,                 // quoted twice
		`a%22b%5Cc`,                   // percent-encoded in a form body
	} {
		found := false
		for _, f := range forms {
			if f == want {
				found = true
			}
		}
		if !found {
			t.Errorf("escapedForms(%q) is missing %q; got %q", value, want, forms)
		}
	}

	if got := escapedForms("plain"); len(got) != 1 {
		t.Errorf("a value nothing escapes should have one form, got %q", got)
	}
}

func TestMasker_StringHidesEscapedForms(t *testing.T) {
	secret := `s3cr"et\value`
	m := New([]string{"S"}, map[string]string{"S": secret})

	inputs := []string{
		fmt.Sprintf("%q", secret),
		fmt.Sprintf("%#v", map[string]any{"v": secret}),
		fmt.Sprintf("error=%q", fmt.Sprintf("Get %q", secret)),
	}
	for _, in := range inputs {
		out := m.String(in)
		if strings.Contains(out, "s3cr") {
			t.Errorf("String(%s) = %s, still shows the secret", in, out)
		}
	}
}

func TestMasker_Map(t *testing.T) {
	m := New([]string{"S"}, map[string]string{"S": "sekret"})
	original := map[string]any{
		"url": "http://x/?k=sekret",
		"headers": map[string]any{
			"Authorization": "Bearer abc",
			"Accept":        "text/plain",
		},
		"plain":  map[string]string{"cookie": "sid=1", "x": "sekret"},
		"list":   []any{"sekret", map[string]any{"set-cookie": "a=b"}},
		"names":  []string{"sekret"},
		"number": 3,
	}
	snapshot := fmt.Sprintf("%#v", original)

	got := m.Map(original)

	if got["url"] != "http://x/?k=<secret:S>" {
		t.Errorf("url = %v", got["url"])
	}
	headers := got["headers"].(map[string]any)
	if headers["Authorization"] != redactedValue || headers["Accept"] != "text/plain" {
		t.Errorf("headers = %v", headers)
	}
	plain := got["plain"].(map[string]any)
	if plain["cookie"] != redactedValue || plain["x"] != "<secret:S>" {
		t.Errorf("map[string]string = %v", plain)
	}
	list := got["list"].([]any)
	if list[0] != "<secret:S>" || list[1].(map[string]any)["set-cookie"] != redactedValue {
		t.Errorf("list = %v", list)
	}
	if names := got["names"].([]any); names[0] != "<secret:S>" {
		t.Errorf("names = %v", names)
	}
	if got["number"] != 3 {
		t.Errorf("number = %v", got["number"])
	}

	if fmt.Sprintf("%#v", original) != snapshot {
		t.Error("Map must not change the original")
	}

	var nilMasker *Masker
	if r := nilMasker.Map(map[string]any{"cookie": "c", "k": "v"}); r["cookie"] != redactedValue || r["k"] != "v" {
		t.Errorf("a nil masker should still redact credential headers, got %v", r)
	}
	if m.Map(nil) != nil {
		t.Error("nil should stay nil")
	}
}

func TestMasker_Learn(t *testing.T) {
	m := New(nil, nil)
	m.Learn(map[string]any{
		"url": "http://x",
		"headers": map[string]any{
			"AUTHORIZATION": "Bearer runtime-token",
		},
		"nested": []any{
			map[string]any{"set-cookie": []any{"sid=abc123", "theme=dark"}},
		},
		"plain": map[string]string{"Cookie": "jar=789"},
	})

	for in, want := range map[string]string{
		"sent Bearer runtime-token": "sent <redacted>",
		"sid=abc123; theme=dark":    "<redacted>; <redacted>",
		"jar=789":                   "<redacted>",
		"http://x":                  "http://x",
	} {
		if got := m.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}

	// A declared secret keeps its label even when it is also learned.
	d := New([]string{"T"}, map[string]string{"T": "Bearer tok"})
	d.Learn(map[string]any{"authorization": "Bearer tok"})
	if got := d.String("Bearer tok"); got != "<secret:T>" {
		t.Errorf("declared label should win, got %q", got)
	}
}

func TestMasker_ConcurrentLearnAndString(t *testing.T) {
	m := New(nil, nil)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			m.Learn(map[string]any{"authorization": fmt.Sprintf("token-%d", i)})
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = m.String(fmt.Sprintf("token-%d", i))
		}(i)
	}
	wg.Wait()

	for i := range 20 {
		if got := m.String(fmt.Sprintf("token-%d.", i)); got != "<redacted>." {
			t.Errorf("token-%d not learned: %q", i, got)
		}
	}
}

func TestMasker_Writer(t *testing.T) {
	m := New([]string{"S"}, map[string]string{"S": "sekret"})
	var buf bytes.Buffer
	w := m.Writer(&buf)

	in := []byte("log sekret line\n")
	n, err := w.Write(in)
	if err != nil || n != len(in) {
		t.Errorf("Write = %d, %v; want %d, nil", n, err, len(in))
	}
	if buf.String() != "log <secret:S> line\n" {
		t.Errorf("wrote %q", buf.String())
	}

	// Values learned after the writer was made are hidden too.
	m.Learn(map[string]any{"cookie": "late-cookie"})
	_, _ = w.Write([]byte("late-cookie\n"))
	if !strings.HasSuffix(buf.String(), "<redacted>\n") {
		t.Errorf("learned value not hidden: %q", buf.String())
	}
}

// A password passed to an action is hidden even when the workflow does not
// declare it as a secret, wherever the action or Probe shows it.
func TestMasker_LearnCredentialParams(t *testing.T) {
	m := New(nil, nil)
	m.Learn(map[string]any{
		"host":           "imap.example.com",
		"Password":       "imap-pass",
		"key_passphrase": "key-pass",
		"user":           "alice",
	})

	got := m.String(`dial alice:imap-pass with "key-pass" at imap.example.com`)
	want := `dial alice:<redacted> with "<redacted>" at imap.example.com`
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	r := New(nil, nil).Map(map[string]any{"password": "p", "user": "alice"})
	if r["password"] != redactedValue || r["user"] != "alice" {
		t.Errorf("Map() = %v, want the password redacted and the user kept", r)
	}
}

// TestMasker_LearnFormPassword checks that a password sent in a form body,
// where it is percent-encoded, is hidden there as well.
func TestMasker_LearnFormPassword(t *testing.T) {
	m := New(nil, nil)
	m.Learn(map[string]any{
		"form": map[string]any{"user": "alice", "password": "p@ss word&x"},
	})

	got := m.String("password=p%40ss+word%26x&user=alice")
	want := "password=<redacted>&user=alice"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestMasker_Cookies checks that the cookies the http action sends and
// receives are hidden by value, with their names kept.
func TestMasker_Cookies(t *testing.T) {
	m := New(nil, nil)
	m.Learn(map[string]any{"cookies": map[string]any{"session": "e2e-cookie-value"}})
	if got := m.String("sent e2e-cookie-value"); got != "sent <redacted>" {
		t.Errorf("String() = %q", got)
	}

	got := m.Map(map[string]any{
		"res": map[string]any{"cookies": map[string]any{"session": "abc", "lang": "ja"}},
		"req": map[string]any{"cookies": map[string]string{"token": "t"}},
	})
	want := map[string]any{
		"res": map[string]any{"cookies": map[string]any{"session": redactedValue, "lang": redactedValue}},
		"req": map[string]any{"cookies": map[string]any{"token": redactedValue}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Map() = %v, want %v", got, want)
	}
}

func TestMasker_LearnDSNPassword(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		in   string
		want string
	}{
		{
			name: "postgres",
			dsn:  "postgres://app:pg-secret@db:5432/main?sslmode=disable",
			in:   "connect postgres://app:pg-secret@db:5432/main?sslmode=disable",
			want: "connect postgres://app:<redacted>@db:5432/main?sslmode=disable",
		},
		{
			name: "mysql with a tcp address",
			dsn:  "mysql://app:my-secret@tcp(db:3306)/main",
			in:   "app:my-secret@tcp(db:3306)/main",
			want: "app:<redacted>@tcp(db:3306)/main",
		},
		{
			name: "an @ in the password",
			dsn:  "mysql://app:p@ss@db/main",
			in:   "app:p@ss@db",
			want: "app:<redacted>@db",
		},
		{
			name: "percent-encoded",
			dsn:  "postgres://app:a%2Fb%40c@db/main",
			in:   "raw a%2Fb%40c decoded a/b@c",
			want: "raw <redacted> decoded <redacted>",
		},
		{
			name: "no path, an @ in the query",
			dsn:  "postgres://app:sample-pass@db?application_name=ops@example.com",
			in:   "app:sample-pass@db?application_name=ops@example.com",
			want: "app:<redacted>@db?application_name=ops@example.com",
		},
		{
			name: "a fragment",
			dsn:  "postgres://app:frag-pass@db#a@b",
			in:   "frag-pass db#a@b",
			want: "<redacted> db#a@b",
		},
		{
			name: "no password",
			dsn:  "postgres://app@db/main",
			in:   "postgres://app@db/main",
			want: "postgres://app@db/main",
		},
		{
			name: "sqlite",
			dsn:  "file:./data.db",
			in:   "file:./data.db",
			want: "file:./data.db",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(nil, nil)
			m.Learn(map[string]any{"dsn": tt.dsn, "query": "select 1"})
			if got := m.String(tt.in); got != tt.want {
				t.Errorf("String(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMasker_LearnDSNInStringMap(t *testing.T) {
	m := New(nil, nil)
	m.Learn(map[string]any{
		"conn": map[string]string{"dsn": "postgres://app:typed-pass@db/main"},
	})
	if got := m.String("typed-pass"); got != redactedValue {
		t.Errorf("String() = %q, want the DSN password hidden", got)
	}
}

// A password written without quotes is a number in the workflow, but the
// action receives the same digits, so they are hidden too.
func TestMasker_LearnNumericCredential(t *testing.T) {
	tests := []struct {
		name  string
		value any
		in    string
	}{
		{name: "int", value: 482915, in: "pass 482915"},
		{name: "uint64", value: uint64(482915), in: "pass 482915"},
		{name: "float64", value: float64(482915), in: "pass 482915"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(nil, nil)
			m.Learn(map[string]any{"password": tt.value})
			if got := m.String(tt.in); got != "pass "+redactedValue {
				t.Errorf("String(%q) = %q, want the number hidden", tt.in, got)
			}
		})
	}

	m := New(nil, nil)
	m.Learn(map[string]any{"password": true})
	if got := m.String("enabled: true"); got != "enabled: true" {
		t.Errorf("String() = %q, want a boolean password not to hide every true", got)
	}
}

// TestMasker_LearnActionCredentials checks the credentials of actions that
// are not built in: the password in the URL an action connects to, and the
// secret key and session token of object storage.
func TestMasker_LearnActionCredentials(t *testing.T) {
	m := New(nil, nil)
	m.Learn(map[string]any{
		"url":               "redis://app:redis-secret@cache:6379/0",
		"access_key_id":     "AKIAEXAMPLE",
		"secret_access_key": "s3-secret",
		"session_token":     "s3-token",
	})
	in := "redis://app:redis-secret@cache:6379/0 AKIAEXAMPLE s3-secret s3-token"
	want := "redis://app:<redacted>@cache:6379/0 AKIAEXAMPLE <redacted> <redacted>"
	if got := m.String(in); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	// A URL without a password has nothing to hide.
	m = New(nil, nil)
	m.Learn(map[string]any{"url": "https://api.example.com/users?token=visible"})
	if got := m.String("https://api.example.com/users?token=visible"); got != "https://api.example.com/users?token=visible" {
		t.Errorf("String() = %q, want the URL as it is", got)
	}
}

func TestHoldsCredential(t *testing.T) {
	for key, want := range map[string]bool{
		"password":          true,
		"Password":          true,
		"key_passphrase":    true,
		"secret_access_key": true,
		"session_token":     true,
		"authorization":     true,
		"Cookie":            true,
		"dsn":               true,
		"url":               false,
		"headers":           false,
	} {
		if got := HoldsCredential(key); got != want {
			t.Errorf("HoldsCredential(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestLearn_BasicAuth(t *testing.T) {
	// "Basic " + base64("alice@example.test:pa55-word")
	const header = "Basic YWxpY2VAZXhhbXBsZS50ZXN0OnBhNTUtd29yZA=="
	const token = "YWxpY2VAZXhhbXBsZS50ZXN0OnBhNTUtd29yZA=="

	m := New(nil, nil)
	m.Learn(map[string]any{
		"url": "http://localhost",
		"basic_auth": map[string]any{
			"username": "alice@example.test",
			"password": "pa55-word",
		},
	})

	for _, in := range []string{
		"authorization: " + header,
		"map[authorization:" + header + "]",
		"credentials " + token,
		"password pa55-word",
	} {
		got := m.String(in)
		if strings.Contains(got, token) || strings.Contains(got, "pa55-word") {
			t.Errorf("String(%q) = %q, the credentials are not hidden", in, got)
		}
	}
	if got := m.String("user alice@example.test"); got != "user alice@example.test" {
		t.Errorf("the username alone should stay visible, got %q", got)
	}
}

func TestLearn_BasicAuthNumericPassword(t *testing.T) {
	// "Basic " + base64("bob:1000000"), as the http action sends a password
	// decoded as a float.
	const token = "Ym9iOjEwMDAwMDA="

	m := New(nil, nil)
	m.Learn(map[string]any{"basic_auth": map[string]any{"username": "bob", "password": float64(1000000)}})
	if got := m.String("Basic " + token); strings.Contains(got, token) {
		t.Errorf("the header of a numeric password is not hidden: %q", got)
	}
}

func TestLearn_BasicAuthMistypedKey(t *testing.T) {
	// The http action refuses pass, but logs its parameters before that.
	m := New(nil, nil)
	m.Learn(map[string]any{"basic_auth": map[string]any{"username": "alice", "pass": "mistyped-secret"}})
	if got := m.String("pass:mistyped-secret"); strings.Contains(got, "mistyped-secret") {
		t.Errorf("a credential under a mistyped key is not hidden: %q", got)
	}
	if got := m.String("username:alice"); got != "username:alice" {
		t.Errorf("the username should stay visible, got %q", got)
	}
}

func TestLearn_BasicAuthLargeNumber(t *testing.T) {
	// 9007199254740993 cannot be a float64, and reaches the action as
	// 9007199254740992, from which it builds the header:
	// "Basic " + base64("bob:9007199254740992").
	const token = "Ym9iOjkwMDcxOTkyNTQ3NDA5OTI="

	m := New(nil, nil)
	m.Learn(map[string]any{"basic_auth": map[string]any{"username": "bob", "password": uint64(9007199254740993)}})
	for _, in := range []string{"Basic " + token, "password:9007199254740992", "password:9007199254740993"} {
		if got := m.String(in); strings.Contains(got, "9007199254740") || strings.Contains(got, token) {
			t.Errorf("String(%q) = %q, the credential is not hidden", in, got)
		}
	}
}

func TestUserinfo(t *testing.T) {
	tests := []struct{ in, want string }{
		{"redis://app:pw@cache:6379/0", "app:pw"},
		{"redis://app@cache", "app"},
		{"redis://:p@ss@cache", ":p@ss"},
		{"mysql://root:pw@tcp(db:3306)/app", "root:pw"},
		{"https://example.com/a@b", ""},
		{"https://example.com?to=a@b", ""},
		{"redis://cache:6379", ""},
		{"cache:6379", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Userinfo(tt.in); got != tt.want {
			t.Errorf("Userinfo(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if !HoldsURL("URL") || !HoldsURL("dsn") || HoldsURL("endpoint") {
		t.Error("HoldsURL should name url and dsn, in any case, and nothing else")
	}
}

// TestNew_ShortSecret covers a declared secret too short to mask inside other
// text: replacing it wherever it appears would cut it out of unrelated words,
// which mangles the output and gives the value away by the letters missing
// from them. Where it is a whole value, it is masked.
func TestNew_ShortSecret(t *testing.T) {
	env := map[string]string{
		"ONE":   "x",
		"THREE": "dev",
		"KANJI": "秘密鍵",
		"FOUR":  "abcd",
		"EMPTY": "",
	}
	m := New([]string{"ONE", "THREE", "ONE", "KANJI", "FOUR", "EMPTY", "MISSING"}, env)

	tests := []struct {
		in, want string
	}{
		{"probe.StepContext", "probe.StepContext"},
		{"x", "x"},
		{"deploy to dev", "deploy to dev"},
		{"秘密鍵", "秘密鍵"},
		{"key abcd", "key <secret:FOUR>"},
	}
	for _, tt := range tests {
		if got := m.String(tt.in); got != tt.want {
			t.Errorf("String(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	got := m.Map(map[string]any{
		"body":    "x",
		"headers": map[string]string{"X-Env": "dev"},
		"list":    []any{"秘密鍵", "xx"},
		"names":   []string{"dev", "devops"},
	})
	wantMap := map[string]any{
		"body":    "<secret:ONE>",
		"headers": map[string]any{"X-Env": "<secret:THREE>"},
		"list":    []any{"<secret:KANJI>", "xx"},
		"names":   []any{"<secret:THREE>", "devops"},
	}
	if !reflect.DeepEqual(got, wantMap) {
		t.Errorf("Map() = %#v, want %#v", got, wantMap)
	}
	if got := m.Value("x"); got != "<secret:ONE>" {
		t.Errorf("Value(%q) = %v", "x", got)
	}

	want := []string{"ONE", "THREE", "KANJI"}
	if got := m.TooShort(); !reflect.DeepEqual(got, want) {
		t.Errorf("TooShort() = %q, want %q", got, want)
	}
	if got := New([]string{"FOUR"}, env).TooShort(); len(got) != 0 {
		t.Errorf("a secret long enough to mask should not be reported, got %q", got)
	}
	var nilMasker *Masker
	if got := nilMasker.TooShort(); got != nil {
		t.Errorf("nil TooShort() = %q", got)
	}
}

// TestMasker_ShortCredential covers a credential learned at run time that is
// too short to mask in free text: it is left there, but Map still hides it
// under its key, or as the whole value it is, neither of which depends on
// its length.
func TestMasker_ShortCredential(t *testing.T) {
	m := New(nil, nil)
	m.Learn(map[string]any{
		"password": "ab",
		"dsn":      "postgres://app:pw@db/main",
		"headers":  map[string]any{"cookie": "s=1"},
	})

	for _, in := range []string{"about a table", "pwd", "s=1x"} {
		if got := m.String(in); got != in {
			t.Errorf("String(%q) = %q, want it unchanged", in, got)
		}
	}

	got := m.Map(map[string]any{
		"password": "ab",
		"headers":  map[string]any{"Cookie": "s=1", "Accept": "about"},
	})
	if got := m.Map(map[string]any{"echo": "ab"}); got["echo"] != redactedValue {
		t.Errorf("a whole value equal to the credential = %v, want %s", got["echo"], redactedValue)
	}
	if got["password"] != redactedValue {
		t.Errorf("password = %v, want %s", got["password"], redactedValue)
	}
	headers := got["headers"].(map[string]any)
	if headers["Cookie"] != redactedValue || headers["Accept"] != "about" {
		t.Errorf("headers = %v", headers)
	}
}

// TestMasker_MapRedactsURLPassword covers the password in a URL under dsn or
// url, which Map hides by where it stands, whatever its length, and so also
// when it is too short to be learned as a value.
func TestMasker_MapRedactsURLPassword(t *testing.T) {
	with := map[string]any{
		"dsn":     "postgres://app:pw@db/main",
		"url":     "redis://u:p%40w@h:6379/0",
		"conn":    map[string]string{"DSN": "mysql://root:a@tcp(db:3306)/app"},
		"query":   "select 1",
		"plain":   "https://example.com/a@b",
		"noPass":  map[string]any{"url": "redis://app@cache"},
		"longPwd": map[string]any{"dsn": "postgres://app:long-password@db/main"},
	}
	m := New(nil, nil)
	m.Learn(with)

	got := m.Map(with)
	want := map[string]any{
		"dsn":     "postgres://app:<redacted>@db/main",
		"url":     "redis://u:<redacted>@h:6379/0",
		"conn":    map[string]any{"DSN": "mysql://root:<redacted>@tcp(db:3306)/app"},
		"query":   "select 1",
		"plain":   "https://example.com/a@b",
		"noPass":  map[string]any{"url": "redis://app@cache"},
		"longPwd": map[string]any{"dsn": "postgres://app:<redacted>@db/main"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Map() = %#v, want %#v", got, want)
	}
	if with["dsn"] != "postgres://app:pw@db/main" {
		t.Errorf("Map changed the original: %v", with["dsn"])
	}
	if got := New(nil, nil).Map(map[string]any{"url": "redis://:p@ss@cache"}); got["url"] != "redis://:<redacted>@cache" {
		t.Errorf("a password with an @ = %v", got["url"])
	}
}

// TestMasker_ShortSecretAsNumber covers a short secret that reaches Map as a
// number, as 123 does when a JSON body is parsed: it is masked as the whole
// value it is, while other numbers are left as they are.
func TestMasker_ShortSecretAsNumber(t *testing.T) {
	m := New([]string{"PIN"}, map[string]string{"PIN": "123"})
	got := m.Map(map[string]any{
		"pin":   float64(123),
		"int":   123,
		"other": float64(1234),
		"list":  []any{float64(123), float64(12)},
		"flag":  true,
	})
	want := map[string]any{
		"pin":   "<secret:PIN>",
		"int":   "<secret:PIN>",
		"other": float64(1234),
		"list":  []any{"<secret:PIN>", float64(12)},
		"flag":  true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Map() = %#v, want %#v", got, want)
	}
	if got := New(nil, nil).Value(float64(123)); got != float64(123) {
		t.Errorf("a masker with no secrets changed a number: %v", got)
	}
}
