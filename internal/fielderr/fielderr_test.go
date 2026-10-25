package fielderr

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mozership/probe/internal/expr"
)

func Test_unjoin(t *testing.T) {
	a, b := errors.New("a"), errors.New("b")
	if got := unjoin(errors.Join(a, b)); !reflect.DeepEqual(got, []error{a, b}) {
		t.Errorf("unjoin(joined) = %v, want a and b", got)
	}
	if got := unjoin(a); !reflect.DeepEqual(got, []error{a}) {
		t.Errorf("unjoin(a) = %v, want a alone", got)
	}
}

// paths returns the path and the evaluated path of each error joined into
// err, or the text of one that is not a *expr.FieldError.
func paths(t *testing.T, err error) [][2]string {
	t.Helper()
	var got [][2]string
	for _, e := range unjoin(err) {
		var fe *expr.FieldError
		if errors.As(e, &fe) {
			got = append(got, [2]string{fe.Path, fe.EvaluatedPath})
		} else {
			got = append(got, [2]string{e.Error(), ""})
		}
	}
	return got
}

func TestPrefix(t *testing.T) {
	cause := errors.New("cause")
	err := errors.Join(
		&expr.FieldError{Path: "url", Err: cause},
		&expr.FieldError{Path: "{{k}}.x", EvaluatedPath: "key.x", Err: cause},
		errors.New("other"),
	)
	got := paths(t, Prefix(err, "with."))
	want := [][2]string{{"with.url", ""}, {"with.{{k}}.x", "with.key.x"}, {"other", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Prefix() paths = %q, want %q", got, want)
	}

	var fe *expr.FieldError
	if e := Prefix(&expr.FieldError{Path: "a", Err: cause}, "p."); !errors.As(e, &fe) || !errors.Is(e, cause) {
		t.Errorf("Prefix() = %v, want a field error wrapping the cause", e)
	}
}

func TestRename(t *testing.T) {
	cause := errors.New("cause")
	err := errors.Join(
		&expr.FieldError{Path: "list[1]", Err: cause},
		&expr.FieldError{Path: "list[0].{{k}}", EvaluatedPath: "list[0].key", Err: cause},
		&expr.FieldError{Path: "other", Err: cause},
		errors.New("plain"),
	)
	got := paths(t, Rename(err, "list", "vars.l"))
	want := [][2]string{{"vars.l[1]", ""}, {"vars.l[0].{{k}}", "vars.l[0].key"}, {"other", ""}, {"plain", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Rename() paths = %q, want %q", got, want)
	}
}
