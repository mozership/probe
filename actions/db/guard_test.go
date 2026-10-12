package db

import (
	"strings"
	"testing"

	"github.com/mozership/probe/actionrpc"
)

func TestReqCheckGuard(t *testing.T) {
	readOnly := actionrpc.Guard{ReadOnly: true}
	tests := []struct {
		name    string
		guard   actionrpc.Guard
		dsn     string
		query   string
		refused string // a part of the reason; empty when the query runs
	}{
		{name: "SELECT", guard: readOnly, query: "SELECT * FROM users"},
		{name: "lower case, with a trailing semicolon", guard: readOnly, query: "  select 1;  "},
		{name: "SHOW", guard: readOnly, query: "SHOW TABLES"},
		{name: "EXPLAIN", guard: readOnly, query: "EXPLAIN SELECT 1"},
		{name: "WITH", guard: readOnly, query: "WITH x AS (SELECT 1) SELECT * FROM x"},
		{name: "a comment", guard: readOnly, query: "SELECT 1 -- a comment\n, 2 /* another */"},
		{name: "a blank query", guard: readOnly, query: " ; ", refused: "the query holds no statement"},
		{name: "a semicolon in a string", guard: readOnly, query: "SELECT 'a;b'", refused: "the query holds a semicolon"},
		{name: "a semicolon in a comment", guard: readOnly, query: "SELECT 1 /* ; */", refused: "the query holds a semicolon"},
		// SQLite does not take a backslash to escape a quote, so the string
		// ends before the semicolon.
		{name: "a backslash before a quote", guard: readOnly, query: `SELECT '\'; PRAGMA query_only=OFF; DELETE FROM t`, refused: "the query holds a semicolon"},
		// MySQL takes -- for a comment only before a space, so 1--1 is 1 - -1.
		{name: "a double dash that is not a comment", guard: readOnly, query: "SELECT 1--1; COMMIT; DELETE FROM t", refused: "the query holds a semicolon"},
		{name: "INSERT", guard: readOnly, query: "INSERT INTO users VALUES (1)", refused: "the statement INSERT may write"},
		{name: "UPDATE", guard: readOnly, query: "update users set a = 1", refused: "the statement UPDATE may write"},
		{name: "two statements", guard: readOnly, query: "SELECT 1; DELETE FROM users", refused: "the query holds a semicolon"},
		{name: "INSERT without a guard", query: "INSERT INTO users VALUES (1)"},
		{name: "a server allowed", guard: actionrpc.Guard{AllowHosts: []string{"db.internal"}}, dsn: "mysql://u:p@db.internal:3306/app", query: "INSERT INTO t VALUES (1)"},
		{name: "the default port", guard: actionrpc.Guard{AllowHosts: []string{"db.internal:5432"}}, dsn: "postgres://u:p@db.internal/app", query: "SELECT 1"},
		{name: "localhost without a host", guard: actionrpc.Guard{AllowHosts: []string{"localhost:3306"}}, dsn: "mysql://u:p@/app", query: "SELECT 1"},
		{name: "a server not allowed", guard: actionrpc.Guard{AllowHosts: []string{"db.internal"}}, dsn: "postgres://u:p@prod.example.com:5432/app", query: "SELECT 1", refused: "the host prod.example.com:5432 is not one the run allows"},
		{name: "SQLite names no host", guard: actionrpc.Guard{AllowHosts: []string{"db.internal"}}, dsn: "file:./test.db", query: "SELECT 1"},
		// The servers are those lib/pq resolves the DSN to, which it then
		// connects to.
		{name: "a PostgreSQL port parameter", guard: actionrpc.Guard{AllowHosts: []string{"allowed.example:5432"}}, dsn: "postgres://u:p@allowed.example/app?port=5433", query: "SELECT 1", refused: "the host allowed.example:5433 is not one the run allows"},
		{name: "a PostgreSQL hostaddr", guard: actionrpc.Guard{AllowHosts: []string{"db.internal"}}, dsn: "postgres://u:p@db.internal/app?hostaddr=10.0.0.9", query: "SELECT 1", refused: "the host 10.0.0.9:5432 is not one the run allows"},
		{name: "a PostgreSQL hostaddr allowed", guard: actionrpc.Guard{AllowHosts: []string{"10.0.0.9"}}, dsn: "postgres://u:p@db.internal/app?hostaddr=10.0.0.9", query: "SELECT 1"},
		{name: "one of several PostgreSQL hosts not allowed", guard: actionrpc.Guard{AllowHosts: []string{"a.example"}}, dsn: "postgres://u:p@/app?host=a.example,b.example", query: "SELECT 1", refused: "the host b.example:5432 is not one the run allows"},
		{name: "several PostgreSQL hosts allowed", guard: actionrpc.Guard{AllowHosts: []string{"*.example"}}, dsn: "postgres://u:p@/app?host=a.example,b.example", query: "SELECT 1"},
		{name: "a PostgreSQL DSN lib/pq cannot read", guard: actionrpc.Guard{AllowHosts: []string{"a.example"}}, dsn: "postgres://u:p@a.example/app?sslmode=bogus", query: "SELECT 1", refused: "the DSN cannot be read for its server"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := tt.dsn
			if dsn == "" {
				dsn = "file::memory:"
			}
			driver, _, err := parseDSN(dsn)
			if err != nil {
				t.Fatal(err)
			}
			r := &Req{Driver: driver, DSN: dsn, Query: tt.query}
			err = r.checkGuard(tt.guard)
			if tt.refused == "" {
				if err != nil {
					t.Errorf("checkGuard = %v, want nil", err)
				}
				return
			}
			if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), tt.refused) {
				t.Errorf("checkGuard = %v, want a refusal saying %q", err, tt.refused)
			}
		})
	}
}

func TestReqHostsTakeTheEnvironment(t *testing.T) {
	t.Setenv("PGHOST", "env.example")
	t.Setenv("PGPORT", "6543")
	r := &Req{Driver: "postgres", DSN: "postgres://u:p@/app", Query: "SELECT 1"}
	err := r.checkGuard(actionrpc.Guard{AllowHosts: []string{"localhost"}})
	if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), "the host env.example:6543") {
		t.Errorf("checkGuard = %v, want the host PGHOST and PGPORT name refused", err)
	}
	// A host the DSN names wins over PGHOST.
	r.DSN = "postgres://u:p@localhost/app"
	if err := r.checkGuard(actionrpc.Guard{AllowHosts: []string{"localhost"}}); err != nil {
		t.Errorf("checkGuard = %v, want nil", err)
	}
}

func TestReqHostsTakePGHOSTADDR(t *testing.T) {
	t.Setenv("PGHOSTADDR", "10.0.0.7")
	r := &Req{Driver: "postgres", DSN: "postgres://u:p@db.internal/app", Query: "SELECT 1"}
	err := r.checkGuard(actionrpc.Guard{AllowHosts: []string{"db.internal"}})
	if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), "the host 10.0.0.7:5432") {
		t.Errorf("checkGuard = %v, want the address PGHOSTADDR names refused", err)
	}
}

// PostgreSQL takes a backslash in a string as itself, as
// standard_conforming_strings is on by default, so the string ends before
// the first semicolon.
func TestReqCheckGuardPostgreSQLBackslash(t *testing.T) {
	r := &Req{Driver: "postgres", DSN: "postgres://u:p@db.internal/app", Query: `SELECT '\'; COMMIT; DELETE FROM t; -- '`}
	if err := r.checkGuard(actionrpc.Guard{ReadOnly: true}); !actionrpc.IsRefused(err) {
		t.Errorf("checkGuard = %v, want a refusal", err)
	}
}

// A DSN can have the driver run a statement as the connection opens,
// before the query runs in its read-only transaction, so that under a
// read-only guard only the options known not to are allowed.
func TestReqCheckGuardConnectOptions(t *testing.T) {
	readOnly := actionrpc.Guard{ReadOnly: true}
	tests := []struct {
		name    string
		dsn     string
		refused string // a part of the reason; empty when the DSN is allowed
	}{
		{name: "MySQL without options", dsn: "mysql://root:p@localhost:3306/app"},
		{name: "MySQL with options of the driver", dsn: "mysql://root:p@localhost:3306/app?timeout=5s&parseTime=true&charset=utf8mb4"},
		{name: "MySQL with multiStatements", dsn: "mysql://root:p@localhost:3306/app?multiStatements=true", refused: "allows multiStatements"},
		{name: "MySQL with a system variable", dsn: "mysql://root:p@localhost:3306/app?sql_mode=%27%27", refused: "the DSN sets sql_mode on connecting"},
		{name: "MySQL with a statement hidden in a system variable", dsn: "mysql://root:p@localhost:3306/app?multiStatements=true&sql_mode=%27%27%3BDELETE%20FROM%20t", refused: "allows multiStatements"},
		{name: "SQLite without options", dsn: "file:./app.db"},
		{name: "SQLite opened read-only", dsn: "file:./app.db?mode=ro&_txlock=deferred"},
		{name: "SQLite with a pragma", dsn: "file:./app.db?_pragma=user_version(7)", refused: "the DSN sets _pragma"},
		{name: "SQLite with an unknown option", dsn: "file:./app.db?vfs=mine", refused: "the DSN sets vfs"},
		{name: "PostgreSQL with options", dsn: "postgres://u:p@localhost/app?sslmode=disable&options=-c%20statement_timeout%3D5000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, _, err := parseDSN(tt.dsn)
			if err != nil {
				t.Fatal(err)
			}
			r := &Req{Driver: driver, DSN: tt.dsn, Query: "SELECT 1"}
			err = r.checkGuard(readOnly)
			if tt.refused == "" {
				if err != nil {
					t.Errorf("checkGuard = %v, want nil", err)
				}
				return
			}
			if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), tt.refused) {
				t.Errorf("checkGuard = %v, want a refusal saying %q", err, tt.refused)
			}
			// Without the guard, the options are the workflow's to set.
			if err := r.checkGuard(actionrpc.Guard{}); err != nil {
				t.Errorf("checkGuard without a guard = %v, want nil", err)
			}
		})
	}
}

// The MySQL server checked is the address go-sql-driver dials, which a path
// that holds an @tcp(...) of its own would otherwise hide.
func TestReqCheckGuardMySQLAddress(t *testing.T) {
	guard := actionrpc.Guard{AllowHosts: []string{"allowed.example"}}
	tests := []struct {
		dsn     string
		refused string // a part of the reason; empty when the DSN is allowed
	}{
		{dsn: "mysql://u:p@allowed.example:3306/app"},
		{dsn: "mysql://u:p@allowed.example/app"},
		{dsn: "mysql://u:p@allowed.example:3306/x@tcp(evil.example:3306)/app", refused: "the host evil.example:3306 is not one the run allows"},
		{dsn: "mysql://u:p@allowed.example:3306/app)tcp(evil.example:3306/x", refused: "the DSN cannot be read for its server"},
		{dsn: "mysql://u:p@/app", refused: "the host localhost:3306 is not one the run allows"},
	}
	for _, tt := range tests {
		r := &Req{Driver: "mysql", DSN: tt.dsn, Query: "SELECT 1"}
		err := r.checkGuard(guard)
		if tt.refused == "" {
			if err != nil {
				t.Errorf("checkGuard(%s) = %v, want nil", tt.dsn, err)
			}
			continue
		}
		if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), tt.refused) {
			t.Errorf("checkGuard(%s) = %v, want a refusal saying %q", tt.dsn, err, tt.refused)
		}
	}
}
