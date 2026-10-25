// Package expr evaluates the expressions and {{ }} templates of a workflow.
package expr

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	ex "github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/mozership/probe/jsonutil"
	"github.com/mozership/probe/truncate"
)

var (
	templateStart = "{{"
	templateEnd   = "}}"

	// Security: Maximum expression length and evaluation timeout
	maxExpressionLength = 1000000
	evaluationTimeout   = 5 * time.Second

	// Security: Maximum string length to prevent memory exhaustion
	maxStringLength = 1000000

	// maxTemplateDepth bounds how deep template() calls may nest, as they do
	// when a file expands a template that reads another file.
	maxTemplateDepth = 10

	// maxTemplateOutput bounds the text the template() calls nested in one
	// call expand in all. Each result is bounded on its own, but a template
	// that calls template twice, at every level, would expand twice as much
	// at each.
	maxTemplateOutput = int64(maxStringLength) * int64(maxTemplateDepth)
)

// Expr evaluates expressions and templates against an environment such as
// a step's context.
type Expr struct {
	// BeforeTemplate, when it is set, is called with the text template() is
	// about to expand, at any depth. An error it returns stops the
	// evaluation, and is found in the error returned with errors.As.
	BeforeTemplate func(text string) error

	// depth is how many template() calls this evaluation is inside.
	depth int
	// maxOutput, when it is not zero, is the length EvalTemplate stops at,
	// as soon as the text it builds would pass it.
	maxOutput int
	// expanded counts the text the template() calls nested in the outermost
	// one have expanded, which they share.
	expanded *atomic.Int64
}

// Options builds the expr options used to compile every workflow expression.
//
// Two guards that used to live here have been removed because neither did
// anything. ex.DisableBuiltin had no effect on all / any / one / filter / map /
// count: expr's parser recognises those as predicate builtins before it looks
// at the disabled set (parser.go, the `predicates` table), so the calls were
// dead and the functions have always been reachable. A per-key blocklist that
// filtered the environment was equally inert, because the filtered copy only
// ever reached the type checker while ex.Run receives the caller's env, and
// enforcing it would have broken the documented way of reading credentials
// into `vars`.
//
// What still bounds an expression is the length check in checkLength,
// the evaluation timeout in executeWithTimeout, and the argument limits on the
// functions registered below.
func (e *Expr) Options(env any) []ex.Option {
	// A nil Expr evaluates as the zero one does.
	if e == nil {
		e = &Expr{}
	}
	return []ex.Option{
		ex.Env(env),
		// Workflow expressions routinely reference names that only exist at
		// run time, such as environment variables read into `vars`.
		ex.AllowUndefinedVariables(),

		// Functions probe adds on top of expr's own builtins.
		ex.Function(
			"match_json",
			func(params ...any) (any, error) {
				if len(params) != 2 {
					return false, fmt.Errorf("match_json requires exactly 2 parameters")
				}
				src, ok1 := params[0].(map[string]any)
				target, ok2 := params[1].(map[string]any)
				if !ok1 || !ok2 {
					return false, fmt.Errorf("match_json parameters must be objects")
				}
				ok, err := jsonutil.Match(src, target)
				if err != nil {
					return false, fmt.Errorf("match_json: %w", err)
				}
				return ok, nil
			},
		),
		ex.Function(
			"diff_json",
			func(params ...any) (any, error) {
				if len(params) != 2 {
					return nil, fmt.Errorf("diff_json requires exactly 2 parameters")
				}
				src, ok1 := params[0].(map[string]any)
				target, ok2 := params[1].(map[string]any)
				if !ok1 || !ok2 {
					return nil, fmt.Errorf("diff_json parameters must be objects")
				}
				diff, err := jsonutil.Diff(src, target)
				if err != nil {
					return nil, fmt.Errorf("diff_json: %w", err)
				}
				return diff, nil
			},
		),
		ex.Function(
			"random_int",
			func(params ...any) (any, error) {
				if len(params) != 1 {
					return nil, fmt.Errorf("random_int requires exactly 1 parameter")
				}
				n, ok := params[0].(int)
				if !ok {
					// Try to convert float64 to int (common in JSON/expr)
					if f, ok := params[0].(float64); ok {
						n = int(f)
					} else {
						return nil, fmt.Errorf("random_int parameter must be an integer")
					}
				}
				if n <= 0 {
					return nil, fmt.Errorf("random_int parameter must be positive")
				}
				return rand.IntN(n), nil
			},
		),
		ex.Function(
			"random_str",
			func(params ...any) (any, error) {
				if len(params) != 1 {
					return nil, fmt.Errorf("random_str requires exactly 1 parameter")
				}
				length, ok := params[0].(int)
				if !ok {
					// Try to convert float64 to int (common in JSON/expr)
					if f, ok := params[0].(float64); ok {
						length = int(f)
					} else {
						return nil, fmt.Errorf("random_str parameter must be an integer")
					}
				}
				if length <= 0 {
					return nil, fmt.Errorf("random_str parameter must be positive")
				}
				if length > 1000000 {
					return nil, fmt.Errorf("random_str parameter must be <= 1000000")
				}

				const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
				b := make([]byte, length)
				for i := range b {
					b[i] = charset[rand.IntN(len(charset))]
				}

				return string(b), nil
			},
		),
		ex.Function(
			"unixtime",
			func(params ...any) (any, error) {
				if len(params) != 0 {
					return nil, fmt.Errorf("unixtime takes no parameters")
				}
				return time.Now().Unix(), nil
			},
		),
		ex.Function(
			"parse_float",
			func(params ...any) (any, error) {
				if len(params) != 1 {
					return nil, fmt.Errorf("parse_float requires exactly 1 parameter")
				}
				s, ok := params[0].(string)
				if !ok {
					return nil, fmt.Errorf("parse_float parameter must be a string")
				}
				f, err := strconv.ParseFloat(s, 64)
				if err != nil {
					return nil, fmt.Errorf("parse_float error: %w", err)
				}
				return f, nil
			},
		),
		ex.Function(
			"parse_int",
			func(params ...any) (any, error) {
				if len(params) != 1 {
					return nil, fmt.Errorf("parse_int requires exactly 1 parameter")
				}

				switch v := params[0].(type) {
				case string:
					i, err := strconv.ParseInt(v, 10, 64)
					if err != nil {
						return nil, fmt.Errorf("parse_int error: %w", err)
					}
					return i, nil
				case int:
					return int64(v), nil
				case int64:
					return v, nil
				case uint64:
					return int64(v), nil
				case float64:
					return int64(v), nil
				default:
					return nil, fmt.Errorf("parse_int parameter must be a string or number, got %T", v)
				}
			},
		),
		ex.Function(
			"encode_base64",
			func(params ...any) (any, error) {
				if len(params) != 1 {
					return nil, fmt.Errorf("encode_base64 requires exactly 1 parameter")
				}
				s, ok := params[0].(string)
				if !ok {
					return nil, fmt.Errorf("encode_base64 parameter must be a string")
				}
				if len(s) > maxStringLength {
					return nil, fmt.Errorf("encode_base64 parameter exceeds maximum length (%d chars)", maxStringLength)
				}
				return base64.StdEncoding.EncodeToString([]byte(s)), nil
			},
		),
		ex.Function(
			"parse_json",
			func(params ...any) (any, error) {
				if len(params) != 1 {
					return nil, fmt.Errorf("parse_json requires exactly 1 parameter")
				}
				s, ok := params[0].(string)
				if !ok {
					return nil, fmt.Errorf("parse_json parameter must be a string")
				}
				if len(s) > maxStringLength {
					return nil, fmt.Errorf("parse_json parameter exceeds maximum length (%d chars)", maxStringLength)
				}
				return jsonutil.Parse(s)
			},
		),
		ex.Function(
			"decode_base64",
			func(params ...any) (any, error) {
				if len(params) != 1 {
					return nil, fmt.Errorf("decode_base64 requires exactly 1 parameter")
				}
				s, ok := params[0].(string)
				if !ok {
					return nil, fmt.Errorf("decode_base64 parameter must be a string")
				}
				if len(s) > maxStringLength {
					return nil, fmt.Errorf("decode_base64 parameter exceeds maximum length (%d chars)", maxStringLength)
				}
				decoded, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return nil, fmt.Errorf("decode_base64 error: %w", err)
				}
				return string(decoded), nil
			},
		),
		ex.Function(
			"file",
			func(params ...any) (any, error) {
				if len(params) != 1 {
					return nil, fmt.Errorf("file requires exactly 1 parameter")
				}
				path, ok := params[0].(string)
				if !ok || path == "" {
					return nil, fmt.Errorf("file parameter must be a path")
				}
				return readFile(path)
			},
		),
		ex.Function(
			"template",
			func(params ...any) (any, error) {
				if len(params) != 1 {
					return nil, fmt.Errorf("template requires exactly 1 parameter")
				}
				s, ok := params[0].(string)
				if !ok {
					return nil, fmt.Errorf("template parameter must be a string")
				}
				if len(s) > maxStringLength {
					return nil, fmt.Errorf("template parameter exceeds maximum length (%d chars)", maxStringLength)
				}
				if e.depth >= maxTemplateDepth {
					return nil, fmt.Errorf("template calls nest deeper than %d", maxTemplateDepth)
				}
				if e.BeforeTemplate != nil {
					if err := e.BeforeTemplate(s); err != nil {
						return nil, err
					}
				}
				expanded := e.expanded
				if expanded == nil {
					expanded = &atomic.Int64{}
				}
				// The templates in s are evaluated against the same
				// environment as the expression that calls template.
				nested := &Expr{BeforeTemplate: e.BeforeTemplate, depth: e.depth + 1, expanded: expanded, maxOutput: maxStringLength}
				out, err := nested.EvalTemplate(s, env)
				if err != nil {
					return nil, err
				}
				if expanded.Add(int64(len(out))) > maxTemplateOutput {
					return nil, fmt.Errorf("template calls expand more than %d chars in all", maxTemplateOutput)
				}
				return out, nil
			},
		),
	}
}

// readFile returns the content of the file at path, relative to the working
// directory, as the paths of actions are. Only a regular file is read, and no
// more of it than a string an expression may hold: a device such as
// /dev/zero, or a file that grows while it is read, would otherwise be read
// on after the evaluation has timed out. A larger file is an error rather
// than cut short.
func readFile(path string) (string, error) {
	// The path is looked at before it is opened: opening a FIFO to read
	// waits for a writer.
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("file: %w", err)
	}
	if err := checkRegular(path, info); err != nil {
		return "", err
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// It is looked at again once it is open, in case it was replaced.
	opened, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("file: %w", err)
	}
	if err := checkRegular(path, opened); err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("file: %s changed while it was opened", path)
	}

	data, err := io.ReadAll(io.LimitReader(f, int64(maxStringLength)+1))
	if err != nil {
		return "", fmt.Errorf("file: %w", err)
	}
	if len(data) > maxStringLength {
		return "", fmt.Errorf("file: %s exceeds maximum length (%d bytes)", path, maxStringLength)
	}
	return string(data), nil
}

// checkRegular returns an error unless info is of a regular file.
func checkRegular(path string, info os.FileInfo) error {
	if info.IsDir() {
		return fmt.Errorf("file: %s is a directory", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("file: %s is not a regular file", path)
	}
	return nil
}

// checkLength returns an error when input, an expression or a template, is
// longer than an expression may be, so that it is bounded before it is
// compiled.
//
// This used to also reject any expression whose text contained "env.secret",
// "env.path" and similar. That guarded a namespace expressions never had -
// the evaluation context exposes vars, res, req, rt, status, outputs and
// repeat_index, never env - while rejecting ordinary strings that happen to
// contain one of the patterns, such as the URL "https://env.pathfinder.example.com".
func checkLength(input string) error {
	if len(input) > maxExpressionLength {
		return fmt.Errorf("SECURITY: expression exceeds maximum length (%d chars)", maxExpressionLength)
	}
	return nil
}

// EvalOrEvalTemplate evaluates input as a template when it holds one, as
// HasTemplate tells, and as an expression otherwise, and returns the value
// as text. Braces that are not a template, such as those in the string
// literals of "}}" + "{{", leave input an expression.
func (e *Expr) EvalOrEvalTemplate(input string, env any) (string, error) {
	// Security: Validate input expression
	if err := checkLength(input); err != nil {
		return "", fmt.Errorf("expression validation failed: %w", err)
	}

	if HasTemplate(input) {
		return e.EvalTemplate(input, env)
	}
	output, err := e.Eval(input, env)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", output), nil
}

// Eval evaluates input, an expression, against env and returns its value.
// When input cannot be evaluated, the value is nil.
func (e *Expr) Eval(input string, env any) (any, error) {
	// Security: Validate expression before compilation
	if err := checkLength(input); err != nil {
		return nil, fmt.Errorf("expression validation failed: %w", err)
	}

	program, err := ex.Compile(input, e.Options(env)...)
	if err != nil {
		return nil, withHints(input, err)
	}

	// Security: Execute with timeout to prevent infinite loops
	output, err := executeWithTimeout(program, env)
	return output, withHints(input, err)
}

// executeWithTimeout runs program against env, and returns an error instead
// of its value when it panics or runs for longer than evaluationTimeout.
func executeWithTimeout(program *vm.Program, env any) (any, error) {
	type result struct {
		output any
		err    error
	}

	// The channel holds the one result the goroutine sends, so that it does
	// not block on sending it after the evaluation has timed out.
	resultCh := make(chan result, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultCh <- result{nil, fmt.Errorf("expression execution panicked: %v", r)}
			}
		}()

		output, err := ex.Run(program, env)
		resultCh <- result{output, err}
	}()

	select {
	case res := <-resultCh:
		return res.output, res.err
	case <-time.After(evaluationTimeout):
		return nil, fmt.Errorf("SECURITY: expression evaluation timed out after %v", evaluationTimeout)
	}
}

// isWholeStringTemplate checks if the input string contains only a single template expression
func isWholeStringTemplate(input string) bool {
	trimmed := strings.TrimSpace(input)
	spans := findTemplates(trimmed)
	return len(spans) == 1 && spans[0].start == 0 && spans[0].end == len(trimmed)
}

// extractTemplateExpression extracts the expression from a whole string template
func extractTemplateExpression(input string) string {
	if !isWholeStringTemplate(input) {
		return ""
	}
	return strings.TrimSpace(findTemplates(strings.TrimSpace(input))[0].expr)
}

// EvalTemplate replaces each {{ }} template in input with the value of its
// expression. A template that does not compile or fails to run is an error
// naming the template, and no string is returned: a value with the error
// written into it would otherwise be sent on, as a URL or a header, as if it
// were right.
func (e *Expr) EvalTemplate(input string, env any) (string, error) {
	// Security: Validate template input
	if err := checkLength(input); err != nil {
		return "", fmt.Errorf("template validation failed: %w", err)
	}

	var b strings.Builder
	// A nil Expr evaluates as the zero one does, with no limit.
	limit := 0
	if e != nil {
		limit = e.maxOutput
	}
	// write adds s to the text, unless it would pass the limit there is.
	write := func(s string) error {
		if limit > 0 && b.Len()+len(s) > limit {
			return fmt.Errorf("template result exceeds maximum length (%d chars)", limit)
		}
		b.WriteString(s)
		return nil
	}
	last := 0
	for _, span := range findTemplates(input) {
		if err := write(input[last:span.start]); err != nil {
			return "", err
		}
		last = span.end

		expression := strings.TrimSpace(span.expr)

		// Security: Validate individual expression
		if err := checkLength(expression); err != nil {
			return "", fmt.Errorf("template expression validation failed: %w", err)
		}

		// Evaluate the expression using expr
		program, err := ex.Compile(expression, e.Options(env)...)
		if err != nil {
			return "", &TemplateError{Template: input[span.start:span.end], Err: withHints(expression, err)}
		}

		// Security: Execute with timeout protection
		output, err := executeWithTimeout(program, env)
		if err != nil {
			return "", &TemplateError{Template: input[span.start:span.end], Err: withHints(expression, err)}
		}

		// Convert the output to string with size limit
		outputStr := fmt.Sprintf("%v", output)
		if len(outputStr) > maxStringLength {
			outputStr = outputStr[:maxStringLength] + truncate.Message()
		}
		if err := write(outputStr); err != nil {
			return "", err
		}
	}
	if err := write(input[last:]); err != nil {
		return "", err
	}

	return b.String(), nil
}

// TemplateError is a {{ }} template that could not be evaluated.
type TemplateError struct {
	Template string // The template as written, braces included
	Err      error  // Why it could not be evaluated
}

func (e *TemplateError) Error() string {
	return fmt.Sprintf("%s: %v", e.Template, e.Err)
}

func (e *TemplateError) Unwrap() error {
	return e.Err
}

// FieldError is an error in the value under a key of a map evaluated by
// EvalTemplateMap. Path names the value from the top of that map, as in
// headers.authorization or items[0].name.
type FieldError struct {
	Path string
	// EvaluatedPath is Path with the keys that hold templates as they were
	// evaluated, which is what tells a value such as a header named by a
	// template apart. It is empty where it is not known.
	EvaluatedPath string
	Err           error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e *FieldError) Unwrap() error {
	return e.Err
}

func (e *Expr) EvalTemplateWithTypePreservation(input string, env any) (any, error) {
	// Security: Validate template input
	if err := checkLength(input); err != nil {
		return "", fmt.Errorf("template validation failed: %w", err)
	}

	// Check if this is a whole string template for type preservation
	if isWholeStringTemplate(input) {
		expression := extractTemplateExpression(input)
		if expression == "" {
			return "", fmt.Errorf("failed to extract expression from template: %s", input)
		}

		// Use Eval directly to preserve type
		output, err := e.Eval(expression, env)
		if err != nil {
			return nil, &TemplateError{Template: strings.TrimSpace(input), Err: err}
		}
		return output, nil
	}

	// For partial templates, fall back to string processing
	return e.EvalTemplate(input, env)
}

// EvalTemplateMap evaluates every template in the keys and values of input,
// at any depth, keeping the type of a value that is a single template and
// evaluating a key as text. The errors of all the keys and values that could
// not be evaluated are joined, each as a *FieldError naming the value by its
// keys as written. A key that comes to the same key as another is an error
// too. The map is returned with the errors, so that a caller can still see
// what could be evaluated, such as credentials to hide from the errors: it
// holds nil where a value could not be evaluated, and leaves out a key that
// could not be, or that came to a key taken.
func (e *Expr) EvalTemplateMap(input map[string]any, env any) (map[string]any, error) {
	var errs []error
	results := e.evalTemplateMap(input, env, "", "", &errs)
	return results, errors.Join(errs...)
}

func (e *Expr) evalTemplateMap(input map[string]any, env any, path, evalPath string, errs *[]error) map[string]any {
	results := make(map[string]any)

	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	// Sorted so that the errors come in the same order on every run.
	slices.Sort(keys)

	// written records the key as written that each key comes from, to tell
	// two that come to the same key apart.
	written := make(map[string]string, len(keys))
	for i, key := range keys {
		// Security: Limit the number of processed keys to prevent DoS. Every
		// key counts, one that fails or collides too.
		if i > 1000 {
			results["_truncated"] = "Map processing truncated due to size limits"
			break
		}

		// An error names the key as written, which is what can be found in
		// the workflow, and says the key as evaluated as well.
		keyPath := joinPath(path, key)

		// A key holding templates is evaluated as text, as a key is one.
		name := key
		if len(findTemplates(key)) > 0 {
			out, err := e.EvalTemplate(key, env)
			if err != nil {
				*errs = append(*errs, &FieldError{Path: keyPath, EvaluatedPath: joinPath(evalPath, key), Err: err})
				continue
			}
			name = out
		}
		nameEvalPath := joinPath(evalPath, name)
		// The keys are named as written: the key they come to may be a
		// credential, such as a token, which nothing has learned to hide.
		if from, taken := written[name]; taken {
			*errs = append(*errs, &FieldError{Path: keyPath, EvaluatedPath: nameEvalPath, Err: fmt.Errorf("key %s comes to the same key as %q", key, from)})
			continue
		}
		written[name] = key

		results[name] = e.evalTemplateValue(input[key], env, keyPath, nameEvalPath, errs)
	}

	return results
}

// evalTemplateArray evaluates templates in array elements
func (e *Expr) evalTemplateArray(input []any, env any, path, evalPath string, errs *[]error) []any {
	results := make([]any, len(input))

	for i, val := range input {
		// Security: Limit the number of processed elements to prevent DoS
		if i > 1000 {
			results = append(results, "_truncated: Array processing truncated due to size limits")
			break
		}

		results[i] = e.evalTemplateValue(val, env, fmt.Sprintf("%s[%d]", path, i), fmt.Sprintf("%s[%d]", evalPath, i), errs)
	}

	return results
}

// joinPath returns the path of key in the map at path.
func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func (e *Expr) evalTemplateValue(val any, env any, path, evalPath string, errs *[]error) any {
	switch v := val.(type) {
	case string:
		output, err := e.EvalTemplateWithTypePreservation(v, env)
		if err != nil {
			*errs = append(*errs, &FieldError{Path: path, EvaluatedPath: evalPath, Err: err})
			return nil
		}
		return output
	case map[string]any:
		return e.evalTemplateMap(v, env, path, evalPath, errs)
	case []any:
		return e.evalTemplateArray(v, env, path, evalPath, errs)
	default:
		return v
	}
}
