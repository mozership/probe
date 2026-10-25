// Package fielderr rewrites the paths of the *expr.FieldError joined into an
// error, so that a path names the value from where the error is reported.
package fielderr

import (
	"errors"
	"strings"

	"github.com/mozership/probe/expr"
)

// Unjoin returns the errors joined into err, or err alone.
func Unjoin(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}
	return []error{err}
}

// Prefix puts prefix before the path of each *expr.FieldError joined into
// err, so that the path names the value from further up.
func Prefix(err error, prefix string) error {
	var errs []error
	for _, e := range Unjoin(err) {
		var fe *expr.FieldError
		if errors.As(e, &fe) {
			evaluated := ""
			if fe.EvaluatedPath != "" {
				evaluated = prefix + fe.EvaluatedPath
			}
			e = &expr.FieldError{Path: prefix + fe.Path, EvaluatedPath: evaluated, Err: fe.Err}
		}
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}

// Rename names the value at the top of the path of each *expr.FieldError
// joined into err, from, as to.
func Rename(err error, from, to string) error {
	rename := func(path string) string {
		if rest, ok := strings.CutPrefix(path, from); ok {
			return to + rest
		}
		return path
	}
	var errs []error
	for _, e := range Unjoin(err) {
		var fe *expr.FieldError
		if errors.As(e, &fe) {
			evaluated := ""
			if fe.EvaluatedPath != "" {
				evaluated = rename(fe.EvaluatedPath)
			}
			e = &expr.FieldError{Path: rename(fe.Path), EvaluatedPath: evaluated, Err: fe.Err}
		}
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}
