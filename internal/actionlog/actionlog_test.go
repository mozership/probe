package actionlog

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/mask"
)

// Outside a verbose run an action's error records are left out of its log,
// as the step's result tells the error, while its warnings are kept.
func TestLoggerExcludesErrorsOutsideAVerboseRun(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		var buf strings.Builder
		log := New("http", &buf, nil, hclog.Warn, verbose)
		log.Warn("the widget is old")
		log.Error("widget failed")
		log.Named("stdio").Error("plugin process exited")

		out := buf.String()
		if !strings.Contains(out, "the widget is old") {
			t.Errorf("verbose=%v: the warning should be kept, got:\n%s", verbose, out)
		}
		for _, msg := range []string{"widget failed", "plugin process exited"} {
			if got := strings.Contains(out, msg); got != verbose {
				t.Errorf("verbose=%v: the log has %q = %v, want %v:\n%s", verbose, msg, got, verbose, out)
			}
		}
		if log.IsError() != verbose || !log.IsWarn() || log.IsInfo() {
			t.Errorf("verbose=%v: IsError, IsWarn, IsInfo = %v, %v, %v", verbose, log.IsError(), log.IsWarn(), log.IsInfo())
		}
	}
}

// An action's records read as the rest of the verbose output does: the
// level and the action first, then the fields as YAML-like lines, maps and
// lists laid out, with no timestamp.
func TestLoggerFormat(t *testing.T) {
	var buf strings.Builder
	log := New("./my-action", &buf, nil, hclog.Debug, true)
	log.Debug("retrying the request",
		"attempt", 2,
		"headers", map[string]any{"Accept": "*/*"},
		"hosts", []any{"a", "b"},
		"error", errors.New("timed out"))
	log.With("job", "j").Named("stdio").Warn("slow")
	log.Trace("left out")
	log.Info("odd", "lonely")

	want := `[DEBUG] ./my-action: retrying the request
[DEBUG]   attempt: 2
[DEBUG]   error: timed out
[DEBUG]   headers:
[DEBUG]     Accept: */*
[DEBUG]   hosts:
[DEBUG]     - a
[DEBUG]     - b
[WARN] ./my-action.stdio: slow
[WARN]   job: j
[INFO] ./my-action: odd
[INFO]   EXTRA_VALUE_AT_END: lonely
`
	if buf.String() != want {
		t.Errorf("log =\n%s\nwant\n%s", buf.String(), want)
	}
}

// What the masker knows is hidden in an action's records, in the message
// and in the fields, credentials by their names as well.
func TestLoggerHidesSecrets(t *testing.T) {
	m := mask.New(nil, nil)
	m.Learn(map[string]any{"password": "hunter2-secret"})
	var buf strings.Builder
	log := New("db", &buf, m, hclog.Debug, true)
	log.Debug("connecting with hunter2-secret", "dsn", "app:hunter2-secret@db", "body", "line one\nhunter2-secret\n", "password", "other")

	out := buf.String()
	for _, secret := range []string{"hunter2-secret", "other"} {
		if strings.Contains(out, secret) {
			t.Errorf("the log holds %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "[DEBUG] db: connecting with") {
		t.Errorf("the log lost the record:\n%s", out)
	}
}

// A secret is hidden wherever a record carries it: in a value of any kind
// fieldValue lays out, in a map or a list, in the implied fields and in the
// name of the logger.
func TestLoggerHidesSecretsInValues(t *testing.T) {
	const secret = "hunter2-secret"
	tests := []struct {
		name string
		log  func(l hclog.Logger)
	}{
		{"string", func(l hclog.Logger) { l.Info("m", "dsn", "app:"+secret+"@db") }},
		{"error", func(l hclog.Logger) { l.Info("m", "error", errors.New("auth "+secret+" refused")) }},
		{"format", func(l hclog.Logger) { l.Info("m", "url", hclog.Fmt("https://u:%s@host", secret)) }},
		{"stringer", func(l hclog.Logger) { l.Info("m", "token", stringer(secret)) }},
		{"map", func(l hclog.Logger) { l.Info("m", "headers", map[string]any{"X-Key": secret}) }},
		{"list", func(l hclog.Logger) { l.Info("m", "args", []any{"-p", secret}) }},
		{"missing key", func(l hclog.Logger) { l.Info("m", secret) }},
		{"implied", func(l hclog.Logger) { l.With("dsn", secret).Info("m") }},
		{"key", func(l hclog.Logger) { l.Info("m", secret, "v") }},
		{"name", func(l hclog.Logger) { l.Named(secret).Info("m") }},
		{"credential by name", func(l hclog.Logger) { l.Info("m", "password", "not-learned") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mask.New(nil, nil)
			m.Learn(map[string]any{"password": secret})
			var buf strings.Builder
			tt.log(New("db", &buf, m, hclog.Debug, true))
			out := buf.String()
			if out == "" {
				t.Fatal("nothing was logged")
			}
			for _, s := range []string{secret, "not-learned"} {
				if strings.Contains(out, s) {
					t.Errorf("the log holds %q:\n%s", s, out)
				}
			}
		})
	}
}

type stringer string

func (s stringer) String() string { return string(s) }

// go-plugin skips the records of an action below the level of the logger
// it logs them through, and the runner's level decides what is shown.
func TestLevel(t *testing.T) {
	tests := []struct {
		verbose, trace bool
		want           hclog.Level
	}{
		{false, false, hclog.Warn},
		{false, true, hclog.Warn},
		{true, false, hclog.Debug},
		{true, true, hclog.Trace},
	}
	for _, tt := range tests {
		if got := Level(tt.verbose, tt.trace); got != tt.want {
			t.Errorf("Level(%v, %v) = %v, want %v", tt.verbose, tt.trace, got, tt.want)
		}
		log := New("a", io.Discard, nil, Level(tt.verbose, tt.trace), tt.verbose)
		if log.Named("a").GetLevel() != tt.want {
			t.Errorf("a named log has level %v, want %v", log.Named("a").GetLevel(), tt.want)
		}
	}
	if New("a", io.Discard, nil, hclog.Off, true).IsError() {
		t.Error("an Off log should show nothing")
	}
}

// The standard writer logs at the level its options force, or at info
// level when they force none, and the level of the log still filters it.
func TestStandardWriterLevel(t *testing.T) {
	tests := []struct {
		name    string
		opts    *hclog.StandardLoggerOptions
		level   hclog.Level
		verbose bool
		want    string
	}{
		{"no options", nil, hclog.Debug, true, "[INFO] a: hello\n"},
		{"no level forced", &hclog.StandardLoggerOptions{ForceLevel: hclog.NoLevel}, hclog.Debug, true, "[INFO] a: hello\n"},
		{"forced debug", &hclog.StandardLoggerOptions{ForceLevel: hclog.Debug}, hclog.Debug, true, "[DEBUG] a: hello\n"},
		{"forced trace below the level", &hclog.StandardLoggerOptions{ForceLevel: hclog.Trace}, hclog.Debug, true, ""},
		{"forced warn above the level", &hclog.StandardLoggerOptions{ForceLevel: hclog.Warn}, hclog.Warn, false, "[WARN] a: hello\n"},
		{"forced error outside a verbose run", &hclog.StandardLoggerOptions{ForceLevel: hclog.Error}, hclog.Warn, false, ""},
		{"forced error in a verbose run", &hclog.StandardLoggerOptions{ForceLevel: hclog.Error}, hclog.Debug, true, "[ERROR] a: hello\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			log := New("a", &buf, nil, tt.level, tt.verbose)
			if _, err := io.WriteString(log.StandardWriter(tt.opts), "hello\n"); err != nil {
				t.Fatal(err)
			}
			if buf.String() != tt.want {
				t.Errorf("writer logged %q, want %q", buf.String(), tt.want)
			}

			buf.Reset()
			log.StandardLogger(tt.opts).Print("hello")
			if buf.String() != tt.want {
				t.Errorf("standard logger logged %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

// A line writer takes each write as whole lines: it calls itself with each
// line without its newline, a last line without one as well, and so a line
// split across writes as two.
func TestLineWriter(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		want   []string
	}{
		{"nothing", []string{""}, nil},
		{"one line", []string{"a\n"}, []string{"a"}},
		{"no trailing newline", []string{"a"}, []string{"a"}},
		{"lines", []string{"a\nb\nc\n"}, []string{"a", "b", "c"}},
		{"lines without a trailing newline", []string{"a\nb"}, []string{"a", "b"}},
		{"empty lines", []string{"\n\na\n"}, []string{"", "", "a"}},
		{"carriage returns", []string{"a\r\nb\r\n"}, []string{"a", "b"}},
		{"a line split across writes", []string{"par", "tial\n"}, []string{"par", "tial"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			w := lineWriter(func(line string) { got = append(got, line) })
			for _, s := range tt.writes {
				n, err := w.Write([]byte(s))
				if n != len(s) || err != nil {
					t.Errorf("Write(%q) = %d, %v, want %d, nil", s, n, err, len(s))
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("lines = %q, want %q", abbreviated(got), abbreviated(tt.want))
			}
		})
	}
}

// abbreviated returns lines with each cut to a length a failure can show.
func abbreviated(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		if len(line) > 20 {
			line = fmt.Sprintf("%s... (%d bytes)", line[:20], len(line))
		}
		out[i] = line
	}
	return out
}

// A field's value is laid out as hclog would write it: an error as its
// message, an hclog.Format as the text it formats, a Stringer as its
// string, and anything else as it is, for dump to lay out.
func TestFieldValue(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want any
	}{
		{"error", errors.New("timed out"), "timed out"},
		{"format", hclog.Fmt("%d of %s", 2, "three"), "2 of three"},
		{"format without arguments", hclog.Format{"100%%"}, "100%"},
		{"empty format", hclog.Format{}, ""},
		{"stringer", stringer("s"), "s"},
		{"int", 2, 2},
		{"map", map[string]any{"k": "v"}, map[string]any{"k": "v"}},
		{"nil", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fieldValue(tt.v); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fieldValue(%#v) = %#v, want %#v", tt.v, got, tt.want)
			}
		})
	}
}

// The fields of a record are its keys and values in turn; a value left
// without a key is kept under hclog.MissingKey, and a key that is not a
// string is written as one.
func TestFieldMap(t *testing.T) {
	tests := []struct {
		name string
		args []any
		want map[string]any
	}{
		{"none", nil, map[string]any{}},
		{"pairs", []any{"a", 1, "b", "two"}, map[string]any{"a": 1, "b": "two"}},
		{"a lone value", []any{"lonely"}, map[string]any{hclog.MissingKey: "lonely"}},
		{"an odd value at the end", []any{"a", 1, "lonely"}, map[string]any{"a": 1, hclog.MissingKey: "lonely"}},
		{"a key that is not a string", []any{7, "v"}, map[string]any{"7": "v"}},
		{"values laid out", []any{"err", errors.New("boom")}, map[string]any{"err": "boom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]any{}, tt.args...)
			if got := fieldMap(args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fieldMap(%#v) = %#v, want %#v", tt.args, got, tt.want)
			}
			if !slices.Equal(args, tt.args) {
				t.Errorf("fieldMap changed its arguments to %#v", args)
			}
		})
	}
}

// fieldMap leaves the arguments it is given as they were, so that a lone
// value at the end is not written over in the caller's slice.
func TestFieldMapKeepsTheCallersSlice(t *testing.T) {
	backing := []any{"a", 1, "lonely", "after"}
	fieldMap(backing[:3])
	if backing[3] != "after" {
		t.Errorf("fieldMap wrote %#v past the arguments it was given", backing[3])
	}
}
