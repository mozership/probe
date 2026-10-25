package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/mapping"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type Req struct {
	Driver  string `map:"driver"`
	DSN     string `map:"dsn"`
	Query   string `map:"query"`
	Params  []any  `map:"params"`
	Timeout string `map:"timeout"`
	cb      *Callback
	// readOnly runs the query where the database refuses a write: in a
	// read-only transaction, or on a SQLite connection that only queries.
	readOnly bool
}

// queryer runs a query: a database, a connection or a transaction.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type Res struct {
	Code         int    `map:"code"`
	RowsAffected int64  `map:"rows_affected"`
	Rows         []any  `map:"rows"`
	Error        string `map:"error"`
}

type Result struct {
	Req    Req           `map:"req"`
	Res    Res           `map:"res"`
	RT     time.Duration `map:"rt"`
	Status int           `map:"status"`
}

func ParseRequest(with map[string]any) (*Req, string, time.Duration, error) {
	// Use MapToStructByTags to directly populate Req struct
	req := &Req{}
	err := mapping.MapToStructByTags(with, req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("failed to parse request: %w", err)
	}

	// Manually handle params field because MapToStructByTags doesn't handle []interface{} properly
	if paramsInput, exists := with["params"]; exists {
		if paramsList, ok := paramsInput.([]any); ok {
			req.Params = paramsList
		}
	}

	// Validate required parameters
	if req.DSN == "" {
		return nil, "", 0, fmt.Errorf("dsn parameter is required")
	}
	if req.Query == "" {
		return nil, "", 0, fmt.Errorf("query parameter is required")
	}

	// Parse driver and DSN from URL-style DSN
	driver, driverDSN, err := parseDSN(req.DSN)
	if err != nil {
		return nil, "", 0, err
	}
	req.Driver = driver

	// Parse timeout
	timeout := 30 * time.Second // Default timeout
	if req.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(req.Timeout); err == nil {
			timeout = parsedTimeout
		} else if _, errInt := strconv.Atoi(req.Timeout); errInt == nil {
			// Parsed as a duration of seconds, so that a number too large
			// for one is an error rather than wrapping around to a short,
			// positive limit.
			parsedTimeout, err := time.ParseDuration(req.Timeout + "s")
			if err != nil {
				return nil, "", 0, fmt.Errorf("invalid timeout: %s (%w)", req.Timeout, err)
			}
			timeout = parsedTimeout
		} else {
			return nil, "", 0, fmt.Errorf("invalid timeout format: %s (use duration string like '30s' or integer seconds)", req.Timeout)
		}
		if timeout <= 0 {
			return nil, "", 0, fmt.Errorf("invalid timeout: %s (must be above zero)", req.Timeout)
		}
	}
	req.Timeout = timeout.String()

	return req, driverDSN, timeout, nil
}

const (
	defaultTimeout   = 30
	sqliteScheme     = "file:"
	mysqlScheme      = "mysql://"
	postgresqlScheme = "postgresql://"
)

func parseDSN(dsn string) (driver, driverDSN string, err error) {
	// Parse URL-style DSN
	u, err := url.Parse(dsn)
	if err != nil {
		return "", "", fmt.Errorf("invalid DSN format: %w", err)
	}

	switch u.Scheme {
	case "mysql":
		// Convert to MySQL DSN format: user:password@tcp(host:port)/database?params
		userInfo := ""
		if u.User != nil {
			password, hasPassword := u.User.Password()
			if hasPassword {
				// Password is explicitly set (even if empty)
				userInfo = fmt.Sprintf("%s:%s@", u.User.Username(), password)
			} else {
				// No password field in URL
				userInfo = fmt.Sprintf("%s@", u.User.Username())
			}
		}

		host := u.Host
		if host == "" {
			host = "localhost:3306"
		}

		database := strings.TrimPrefix(u.Path, "/")
		query := ""
		if u.RawQuery != "" {
			query = "?" + u.RawQuery
		}

		driverDSN = fmt.Sprintf("%stcp(%s)/%s%s", userInfo, host, database, query)
		return "mysql", driverDSN, nil

	case "postgres", "postgresql":
		// PostgreSQL DSN can be used as-is with some modifications
		driverDSN = dsn
		// Replace scheme if needed
		if u.Scheme == "postgresql" {
			driverDSN = strings.Replace(driverDSN, "postgresql://", "postgres://", 1)
		}
		return "postgres", driverDSN, nil

	case "file", "":
		if strings.Contains(dsn, ":memory:") {
			return "sqlite", dsn, nil
		}

		abs, err := filepath.Abs(strings.TrimPrefix(dsn, sqliteScheme))
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve SQLite file path: %w", err)
		}
		// Check if file exists or can be created
		dir := filepath.Dir(abs)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return "", "", fmt.Errorf("directory does not exist for SQLite file: %s", dir)
		}

		// SQLite DSN: just the file path
		return "sqlite", abs, nil

	default:
		return "", "", fmt.Errorf("unsupported database scheme: %q (use mysql://, postgres://, postgresql:// or file: for SQLite)", u.Scheme)
	}
}

func (r *Req) Execute(driverDSN string, timeout time.Duration) (res map[string]any, err error) {
	start := time.Now()

	// Before callback
	if r.cb != nil && r.cb.before != nil {
		r.cb.before(r.Query, r.Params)
	}

	// Open database connection
	db, err := sql.Open(r.Driver, driverDSN)
	if err != nil {
		return r.createErrorResult(start, fmt.Errorf("failed to open database: %w", err))
	}
	// A failing Close must not turn the result into an error, which would
	// drop it. A result that already reports a failure keeps it, since that
	// first error says more; a successful one reports the Close instead.
	defer func() {
		closeErr := db.Close()
		if closeErr == nil || err != nil {
			return
		}
		if status, _ := res["status"].(int); status != 0 {
			return
		}
		res, err = r.createErrorResult(start, fmt.Errorf("failed to close database: %w", closeErr))
	}()

	// Test connection
	// The timeout covers connecting as well as the query.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err = db.PingContext(ctx); err != nil {
		return r.createErrorResult(start, timeoutError(ctx, fmt.Errorf("failed to connect to database: %w", err), timeout))
	}

	// A statement that reads runs as a query, so that its rows come back.
	isSelect := returnsRows(firstVerb(r.Query))

	var q queryer = db
	if r.readOnly {
		ro, done, roErr := r.readOnlyQueryer(ctx, db)
		if roErr != nil {
			return r.createErrorResult(start, timeoutError(ctx, roErr, timeout))
		}
		defer done()
		q = ro
	}

	var result *Result

	if isSelect {
		result, err = r.executeSelectQuery(ctx, q, start)
	} else {
		result, err = r.executeNonSelectQuery(ctx, q, start)
	}

	if err != nil {
		return r.createErrorResult(start, timeoutError(ctx, err, timeout))
	}

	// After callback on success
	if r.cb != nil && r.cb.after != nil {
		r.cb.after(result)
	}

	// Convert to map[string]any using probe's mapping function
	mapResult, err := mapping.StructToMapByTags(result)
	if err != nil {
		return r.createErrorResult(start, fmt.Errorf("failed to convert result to map: %w", err))
	}

	return mapResult, nil
}

// readOnlyQueryer returns where to run the query so that the database
// refuses a write itself, with what to call once it has run: a SQLite
// connection told to only query, or a read-only transaction, which is
// rolled back, as it holds nothing to keep.
func (r *Req) readOnlyQueryer(ctx context.Context, db *sql.DB) (queryer, func(), error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	if r.Driver == "sqlite" {
		if _, err := conn.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("failed to make the connection read-only: %w", err)
		}
		return conn, func() { _ = conn.Close() }, nil
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("failed to begin a read-only transaction: %w", err)
	}
	return tx, func() {
		_ = tx.Rollback()
		_ = conn.Close()
	}, nil
}

func (r *Req) executeSelectQuery(ctx context.Context, db queryer, start time.Time) (res *Result, err error) {
	rows, err := db.QueryContext(ctx, r.Query, r.Params...)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := rows.Close()
		if err == nil {
			err = closeErr
		}
	}()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []any

	// Scan rows
	for rows.Next() {
		// Create a slice for scanning
		values := make([]any, len(columns))
		scanArgs := make([]any, len(columns))
		for i := range values {
			scanArgs[i] = &values[i]
		}

		// Scan the row
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, err
		}

		// Create a map for this row
		rowMap := make(map[string]any)
		for i, col := range columns {
			val := values[i]
			// Convert []byte to string for text fields
			if b, ok := val.([]byte); ok {
				val = string(b)
			}
			rowMap[col] = val
		}
		results = append(results, rowMap)
	}

	// Check for errors from iteration
	if err := rows.Err(); err != nil {
		return nil, err
	}

	duration := time.Since(start)

	return &Result{
		Req: *r,
		Res: Res{
			Code:         0,
			RowsAffected: int64(len(results)),
			Rows:         results,
		},
		RT:     duration,
		Status: 0, // success
	}, nil
}

func (r *Req) executeNonSelectQuery(ctx context.Context, db queryer, start time.Time) (*Result, error) {
	result, err := db.ExecContext(ctx, r.Query, r.Params...)
	if err != nil {
		return nil, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}

	duration := time.Since(start)

	return &Result{
		Req: *r,
		Res: Res{
			Code:         0,
			RowsAffected: rowsAffected,
			Rows:         []any{},
		},
		RT:     duration,
		Status: 0, // success
	}, nil
}

// timeoutError names the timeout when it is what ended the work, since the
// driver's own message only says that a context ran out.
//
// The context is checked as well as the error, since some drivers report a
// query cancelled at the deadline in their own words; lib/pq returns the
// server's cancellation error.
func timeoutError(ctx context.Context, err error, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %s: %w", timeout, err)
	}
	return err
}

// createErrorResult reports a query the database refused, or a database that
// could not be reached, as a result rather than an error, so that the step's
// test can check for the failure it expects.
func (r *Req) createErrorResult(start time.Time, err error) (map[string]any, error) {
	duration := time.Since(start)

	result := &Result{
		Req: *r,
		Res: Res{
			Code:         1,
			RowsAffected: 0,
			Rows:         []any{},
			Error:        err.Error(),
		},
		RT:     duration,
		Status: 1, // failure
	}

	// Convert to map[string]any
	mapResult, mapErr := mapping.StructToMapByTags(result)
	if mapErr != nil {
		return map[string]any{}, fmt.Errorf("failed to convert error result to map: %w", mapErr)
	}

	return mapResult, nil
}

type Option func(*Callback)

type Callback struct {
	before func(query string, params []any)
	after  func(result *Result)
	// guard is what the run allows.
	guard actionrpc.Guard
}

func ExecuteQuery(data map[string]any, opts ...Option) (map[string]any, error) {
	req, driverDSN, timeout, err := ParseRequest(data)
	if err != nil {
		return map[string]any{}, err
	}

	cb := &Callback{}
	for _, opt := range opts {
		opt(cb)
	}
	req.cb = cb

	if err := req.checkGuard(cb.guard); err != nil {
		return map[string]any{}, err
	}
	req.readOnly = cb.guard.ReadOnly

	return req.Execute(driverDSN, timeout)
}

// WithGuard runs the query under the guard of the run.
func WithGuard(guard actionrpc.Guard) Option {
	return func(c *Callback) {
		c.guard = guard
	}
}

func WithBefore(f func(query string, params []any)) Option {
	return func(c *Callback) {
		c.before = f
	}
}

func WithAfter(f func(result *Result)) Option {
	return func(c *Callback) {
		c.after = f
	}
}
