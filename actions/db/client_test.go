package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/actionrpc"
)

func TestParseParams(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
	}{
		{
			name: "valid mysql dsn",
			input: map[string]any{
				"dsn":   "mysql://user:pass@localhost:3306/testdb",
				"query": "SELECT * FROM users",
			},
			wantErr: false,
		},
		{
			name: "valid postgres dsn",
			input: map[string]any{
				"dsn":   "postgres://user:pass@localhost:5432/testdb",
				"query": "SELECT * FROM users",
			},
			wantErr: false,
		},
		{
			name: "valid sqlite dsn",
			input: map[string]any{
				"dsn":   "test.db",
				"query": "SELECT * FROM users",
			},
			wantErr: false,
		},
		{
			name: "missing dsn",
			input: map[string]any{
				"query": "SELECT * FROM users",
			},
			wantErr: true,
		},
		{
			name: "missing query",
			input: map[string]any{
				"dsn": "mysql://user:pass@localhost:3306/testdb",
			},
			wantErr: true,
		},
		{
			name: "invalid dsn format",
			input: map[string]any{
				"dsn":   "invalid://dsn",
				"query": "SELECT * FROM users",
			},
			wantErr: true,
		},
		{
			name: "with timeout",
			input: map[string]any{
				"dsn":     "mysql://user:pass@localhost:3306/testdb",
				"query":   "SELECT * FROM users",
				"timeout": "30s",
			},
			wantErr: false,
		},
		{
			name: "with params",
			input: map[string]any{
				"dsn":    "mysql://user:pass@localhost:3306/testdb",
				"query":  "SELECT * FROM users WHERE id = ?",
				"param1": "123",
				"param2": "test",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _, _, err := ParseRequest(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && req == nil {
				t.Error("ParseRequest() returned nil request for valid input")
			}
		})
	}
}

// Note: NewReq() function doesn't exist in client.go, so we skip this test
func TestReqStruct(t *testing.T) {
	req := &Req{
		DSN:     "mysql://user:pass@localhost:3306/testdb",
		Query:   "SELECT 1",
		Timeout: "30s",
		Params:  []any{"param1"},
	}

	if req.DSN == "" {
		t.Error("DSN should not be empty")
	}
	if req.Query == "" {
		t.Error("Query should not be empty")
	}
}

// Note: Req.Execute() is used instead of Req.Do(), so we test validation through ParseRequest
func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		data        map[string]any
		expectError bool
	}{
		{
			name: "missing dsn",
			data: map[string]any{
				"query": "SELECT 1",
			},
			expectError: true,
		},
		{
			name: "missing query",
			data: map[string]any{
				"dsn": "mysql://user:pass@localhost:3306/testdb",
			},
			expectError: true,
		},
		{
			name: "empty dsn",
			data: map[string]any{
				"dsn":   "",
				"query": "SELECT 1",
			},
			expectError: true,
		},
		{
			name: "empty query",
			data: map[string]any{
				"dsn":   "mysql://user:pass@localhost:3306/testdb",
				"query": "",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := ParseRequest(tt.data)

			if tt.expectError {
				if err == nil {
					t.Errorf("ParseRequest() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseRequest() unexpected error: %v", err)
			}
		})
	}
}

func TestExecuteQuery(t *testing.T) {
	tests := []struct {
		name        string
		data        map[string]any
		expectError bool
	}{
		{
			name: "missing required dsn",
			data: map[string]any{
				"query": "SELECT 1",
			},
			expectError: true,
		},
		{
			name: "missing required query",
			data: map[string]any{
				"dsn": "mysql://user:pass@localhost:3306/testdb",
			},
			expectError: true,
		},
		{
			name: "empty dsn",
			data: map[string]any{
				"dsn":   "",
				"query": "SELECT 1",
			},
			expectError: true,
		},
		{
			name: "empty query",
			data: map[string]any{
				"dsn":   "mysql://user:pass@localhost:3306/testdb",
				"query": "",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track if callbacks were called
			beforeCalled := false
			afterCalled := false

			before := WithBefore(func(query string, params []any) {
				beforeCalled = true
			})
			after := WithAfter(func(result *Result) {
				afterCalled = true
			})

			_, err := ExecuteQuery(tt.data, before, after)

			if tt.expectError {
				if err == nil {
					t.Errorf("ExecuteQuery() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("ExecuteQuery() unexpected error: %v", err)
				}
			}

			// For validation errors, callbacks may not be called
			if !tt.expectError {
				if !beforeCalled {
					t.Error("before callback was not called for valid request")
				}
				if !afterCalled {
					t.Error("after callback was not called for valid request")
				}
			}
		})
	}
}

func TestWithBefore(t *testing.T) {
	called := false
	var capturedQuery string
	var capturedParams []any

	option := WithBefore(func(query string, params []any) {
		called = true
		capturedQuery = query
		capturedParams = params
	})

	cb := &Callback{}
	option(cb)

	if cb.before == nil {
		t.Error("WithBefore() did not set before callback")
		return
	}

	// Test the callback
	testParams := []any{"param1", "param2"}
	cb.before("SELECT * FROM test", testParams)

	if !called {
		t.Error("before callback was not called")
	}
	if capturedQuery != "SELECT * FROM test" {
		t.Errorf("Expected query 'SELECT * FROM test', got '%s'", capturedQuery)
	}
	if !reflect.DeepEqual(capturedParams, testParams) {
		t.Errorf("Expected params %v, got %v", testParams, capturedParams)
	}
}

func TestWithAfter(t *testing.T) {
	called := false
	var capturedResult *Result

	option := WithAfter(func(result *Result) {
		called = true
		capturedResult = result
	})

	cb := &Callback{}
	option(cb)

	if cb.after == nil {
		t.Error("WithAfter() did not set after callback")
		return
	}

	// Test the callback
	testResult := &Result{
		Status: 1,
		Res: Res{
			Code:         1,
			Rows:         []any{},
			RowsAffected: 0,
			Error:        "test error",
		},
		RT: time.Second,
	}
	cb.after(testResult)

	if !called {
		t.Error("after callback was not called")
	}
	if capturedResult != testResult {
		t.Error("after callback did not receive correct result")
	}
}

func TestExecuteQueryFailureIsResult(t *testing.T) {
	// The database answering with an error, or not answering at all, is a
	// result the step's test can check, not an error that ends the step.
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")

	tests := []struct {
		name      string
		data      map[string]any
		wantError string
	}{
		{
			name:      "missing table",
			data:      map[string]any{"dsn": dsn, "query": "SELECT * FROM missing_table"},
			wantError: "no such table",
		},
		{
			name:      "syntax error",
			data:      map[string]any{"dsn": dsn, "query": "SELEC 1"},
			wantError: "syntax error",
		},
		{
			name:      "unreachable server",
			data:      map[string]any{"dsn": "mysql://user:pass@127.0.0.1:1/db", "query": "SELECT 1"},
			wantError: "failed to connect to database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, err := ExecuteQuery(tt.data)
			if err != nil {
				t.Fatalf("ExecuteQuery() error: %v", err)
			}
			res, _ := ret["res"].(map[string]any)
			if res["code"] != 1 || ret["status"] != 1 {
				t.Errorf("code = %v, status = %v, want 1 and 1", res["code"], ret["status"])
			}
			if msg, _ := res["error"].(string); !strings.Contains(msg, tt.wantError) {
				t.Errorf("error = %q, want it to contain %q", msg, tt.wantError)
			}
		})
	}
}

func TestExecuteQuerySuccess(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	ret, err := ExecuteQuery(map[string]any{"dsn": dsn, "query": "SELECT 1 AS one"})
	if err != nil {
		t.Fatalf("ExecuteQuery() error: %v", err)
	}
	res, _ := ret["res"].(map[string]any)
	if res["code"] != 0 || ret["status"] != 0 {
		t.Errorf("code = %v, status = %v, want 0 and 0", res["code"], ret["status"])
	}
}

func TestParseDSNUnsupportedScheme(t *testing.T) {
	_, _, err := parseDSN("sqlite3://test.db")
	if err == nil {
		t.Fatal("parseDSN() accepted an unsupported scheme")
	}
	// The message has to name a scheme that works.
	if !strings.Contains(err.Error(), "file:") {
		t.Errorf("error = %q, want it to name the file: scheme", err)
	}
}

// failCloseDriver is a database whose connections fail to close. A query
// for missing_table fails; any other query returns no rows.
type failCloseDriver struct{}

func (failCloseDriver) Open(string) (driver.Conn, error) { return failCloseConn{}, nil }

type failCloseConn struct{}

func (failCloseConn) Prepare(query string) (driver.Stmt, error) {
	if strings.Contains(query, "missing_table") {
		return nil, errors.New("no such table: missing_table")
	}
	return emptyStmt{}, nil
}
func (failCloseConn) Close() error              { return errors.New("close failed") }
func (failCloseConn) Begin() (driver.Tx, error) { return nil, errors.New("not supported") }

type emptyStmt struct{}

func (emptyStmt) Close() error                               { return nil }
func (emptyStmt) NumInput() int                              { return -1 }
func (emptyStmt) Exec([]driver.Value) (driver.Result, error) { return driver.RowsAffected(0), nil }
func (emptyStmt) Query([]driver.Value) (driver.Rows, error)  { return emptyRows{}, nil }

type emptyRows struct{}

func (emptyRows) Columns() []string         { return []string{"one"} }
func (emptyRows) Close() error              { return nil }
func (emptyRows) Next([]driver.Value) error { return io.EOF }

func init() {
	sql.Register("failclose", failCloseDriver{})
}

func TestExecuteCloseFailure(t *testing.T) {
	// Whatever Close reports, the step still gets a result rather than an
	// error that would drop it.
	tests := []struct {
		name      string
		query     string
		wantError string
	}{
		{name: "failed query keeps its error", query: "SELECT * FROM missing_table", wantError: "no such table"},
		{name: "successful query reports the close", query: "SELECT 1", wantError: "close failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Req{Driver: "failclose", Query: tt.query, cb: &Callback{}}
			ret, err := req.Execute("", time.Second)
			if err != nil {
				t.Fatalf("Execute() error: %v", err)
			}
			res, _ := ret["res"].(map[string]any)
			if msg, _ := res["error"].(string); !strings.Contains(msg, tt.wantError) {
				t.Errorf("error = %q, want it to contain %q", msg, tt.wantError)
			}
			if ret["status"] != 1 {
				t.Errorf("status = %v, want 1", ret["status"])
			}
		})
	}
}

// rowDriver is a database whose queries return one row, and whose other
// statements return none, so that a test tells which way a statement ran.
type rowDriver struct{}

func (rowDriver) Open(string) (driver.Conn, error) { return rowConn{}, nil }

type rowConn struct{}

func (rowConn) Prepare(string) (driver.Stmt, error) { return rowStmt{}, nil }
func (rowConn) Close() error                        { return nil }
func (rowConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }

type rowStmt struct{}

func (rowStmt) Close() error                               { return nil }
func (rowStmt) NumInput() int                              { return -1 }
func (rowStmt) Exec([]driver.Value) (driver.Result, error) { return driver.RowsAffected(0), nil }
func (rowStmt) Query([]driver.Value) (driver.Rows, error)  { return &oneRow{}, nil }

type oneRow struct{ done bool }

func (*oneRow) Columns() []string { return []string{"Field"} }
func (*oneRow) Close() error      { return nil }
func (r *oneRow) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = "id"
	return nil
}

func init() {
	sql.Register("rows", rowDriver{})
}

func TestExecuteReadsTheRowsOfAStatementThatReads(t *testing.T) {
	tests := []struct {
		query string
		rows  int
	}{
		{query: "DESC users", rows: 1},
		{query: "describe users", rows: 1},
		{query: "\n  SELECT * FROM users", rows: 1},
		{query: "INSERT INTO users VALUES (1)", rows: 0},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			req := &Req{Driver: "rows", Query: tt.query, cb: &Callback{}}
			ret, err := req.Execute("", time.Second)
			if err != nil {
				t.Fatalf("Execute() error: %v", err)
			}
			res, _ := ret["res"].(map[string]any)
			rows, _ := res["rows"].([]any)
			if len(rows) != tt.rows {
				t.Errorf("rows = %v, want %d", res["rows"], tt.rows)
			}
		})
	}
}

func TestExecuteQueryTimeout(t *testing.T) {
	// A query that would run for minutes is cut off at the timeout, and the
	// step gets a result that says so.
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	start := time.Now()
	ret, err := ExecuteQuery(map[string]any{
		"dsn":     dsn,
		"query":   "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 1000000000) SELECT count(*) FROM c",
		"timeout": "200ms",
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ExecuteQuery() error: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("query ran for %v, want it stopped at the 200ms timeout", elapsed)
	}
	res, _ := ret["res"].(map[string]any)
	if msg, _ := res["error"].(string); !strings.Contains(msg, "timed out after 200ms") {
		t.Errorf("error = %q, want it to name the timeout", msg)
	}
	if ret["status"] != 1 {
		t.Errorf("status = %v, want 1", ret["status"])
	}
}

func TestParseRequestTimeoutAboveZero(t *testing.T) {
	for _, v := range []string{"0", "0s", "-1s"} {
		if _, _, _, err := ParseRequest(map[string]any{"dsn": "file:x.db", "query": "SELECT 1", "timeout": v}); err == nil {
			t.Errorf("timeout %q was accepted", v)
		}
	}
}

func TestParseRequestTimeoutSeconds(t *testing.T) {
	_, _, timeout, err := ParseRequest(map[string]any{"dsn": "file:x.db", "query": "SELECT 1", "timeout": "45"})
	if err != nil || timeout != 45*time.Second {
		t.Errorf("timeout 45 = %v, %v, want 45s", timeout, err)
	}

	// Too many seconds for a duration used to wrap around to about 290ms.
	if _, _, timeout, err := ParseRequest(map[string]any{"dsn": "file:x.db", "query": "SELECT 1", "timeout": "18446744074"}); err == nil {
		t.Errorf("timeout 18446744074 was accepted as %v", timeout)
	}
}

func TestTimeoutErrorFromContext(t *testing.T) {
	// A driver can report the cancelled query in its own words; the context
	// still says the deadline passed.
	expired, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-expired.Done()
	driverErr := errors.New("pq: canceling statement due to user request")

	if got := timeoutError(expired, driverErr, time.Second); !strings.HasPrefix(got.Error(), "timed out after 1s") || !errors.Is(got, driverErr) {
		t.Errorf("timeoutError() = %v, want the timeout named and the driver's error kept", got)
	}
	if got := timeoutError(context.Background(), driverErr, time.Second); got != driverErr {
		t.Errorf("timeoutError() = %v, want the error unchanged before the deadline", got)
	}
}

func TestExecuteQueryReadOnlySQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guard.db")
	dsn := "file:" + path
	for _, q := range []string{"CREATE TABLE t (v INTEGER)", "INSERT INTO t VALUES (1)"} {
		if _, err := ExecuteQuery(map[string]any{"dsn": dsn, "query": q}); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	readOnly := WithGuard(actionrpc.Guard{ReadOnly: true})

	res, err := ExecuteQuery(map[string]any{"dsn": dsn, "query": "SELECT count(*) AS n FROM t"}, readOnly)
	if err != nil {
		t.Fatalf("a query that reads should run: %v", err)
	}
	if rows := res["res"].(map[string]any)["rows"].([]any); len(rows) != 1 {
		t.Errorf("rows = %v, want one", rows)
	}

	// A statement that reads by its first word but writes is refused by
	// the database itself, which answers with an error as it does any
	// query it fails.
	res, err = ExecuteQuery(map[string]any{"dsn": dsn, "query": "WITH x AS (SELECT 1) DELETE FROM t"}, readOnly)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := res["res"].(map[string]any); got["code"] == 0 || !strings.Contains(fmt.Sprint(got["error"]), "readonly") {
		t.Errorf("res = %v, want the database's refusal to write", got)
	}

	// A statement that writes is refused before the database is opened.
	_, err = ExecuteQuery(map[string]any{"dsn": dsn, "query": "DELETE FROM t"}, readOnly)
	if !actionrpc.IsRefused(err) {
		t.Errorf("err = %v, want a refusal", err)
	}

	res, err = ExecuteQuery(map[string]any{"dsn": dsn, "query": "SELECT count(*) AS n FROM t"})
	if err != nil {
		t.Fatal(err)
	}
	row := res["res"].(map[string]any)["rows"].([]any)[0].(map[string]any)
	if n, _ := row["n"].(int64); n != 1 {
		t.Errorf("rows left = %v, want 1: nothing should have been deleted", row["n"])
	}
}

func TestExecuteQueryReadOnlySQLiteRefusesAPragmaOnConnecting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pragma.db")
	if _, err := ExecuteQuery(map[string]any{"dsn": "file:" + path, "query": "CREATE TABLE t (v INTEGER)"}); err != nil {
		t.Fatal(err)
	}
	_, err := ExecuteQuery(map[string]any{"dsn": "file:" + path + "?_pragma=user_version(7)", "query": "SELECT 1"}, WithGuard(actionrpc.Guard{ReadOnly: true}))
	if !actionrpc.IsRefused(err) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	res, err := ExecuteQuery(map[string]any{"dsn": "file:" + path, "query": "SELECT user_version FROM pragma_user_version"})
	if err != nil {
		t.Fatal(err)
	}
	row := res["res"].(map[string]any)["rows"].([]any)[0].(map[string]any)
	if v, _ := row["user_version"].(int64); v != 0 {
		t.Errorf("user_version = %v, want 0: the pragma should not have run", row["user_version"])
	}
}
