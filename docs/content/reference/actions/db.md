# Database Action

The `db` action executes SQL queries on MySQL, PostgreSQL, and SQLite databases, providing comprehensive result handling and error reporting.

## Basic Syntax

A database step gives the connection string and the statement to run.

```yaml
steps:
  - name: "Database Query"
    uses: db
    with:
      dsn: "mysql://user:password@localhost:3306/database"
      query: "SELECT * FROM users WHERE active = ?"
      params: [true]
    test: res.code == 0 && res.rows_affected > 0
```

## Parameters

A database step is described by four parameters: where to connect, what to run, the values to bind, and how long to wait.

### `dsn` (required)

**Type:** String  
**Description:** Database connection string with automatic driver detection  
**Supports:** Template expressions

```yaml
# MySQL
vars:
  db_pass: "{{DB_PASS}}"

with:
  dsn: "mysql://user:password@localhost:3306/database"
  dsn: "mysql://{{vars.db_user}}:{{vars.db_pass}}@{{vars.db_host}}/{{vars.db_name}}"

# PostgreSQL
vars:
  pg_user: "{{PG_USER}}"
  pg_pass: "{{PG_PASS}}"
  pg_host: "{{PG_HOST}}"
  pg_db: "{{PG_DB}}"

with:
  dsn: "postgres://user:password@localhost:5432/database?sslmode=disable"
  dsn: "postgres://{{vars.pg_user}}:{{vars.pg_pass}}@{{vars.pg_host}}/{{vars.pg_db}}"

# SQLite
with:
  dsn: "file:./testdata/sqlite.db"
  dsn: "file:/absolute/path/to/database.db"
  dsn: "file:{{vars.data_dir}}/app.db"
```

### `query` (required)

**Type:** String  
**Description:** SQL query to execute  
**Supports:** Template expressions and multi-line strings

```yaml
with:
  query: "SELECT * FROM users"
  query: "INSERT INTO logs (message, timestamp) VALUES (?, NOW())"
  query: |
    SELECT u.name, u.email, p.title 
    FROM users u 
    JOIN profiles p ON u.id = p.user_id 
    WHERE u.active = ? AND u.created_at > ?
```

### `params` (optional)

**Type:** Array of mixed values (String, Number, Boolean)  
**Description:** Query parameters for prepared statements  
**Supports:** Template expressions

```yaml
with:
  query: "SELECT * FROM users WHERE id = ? AND active = ?"
  params: [123, true, "{{vars.user_email}}"]
```

### `timeout` (optional)

**Type:** Duration  
**Default:** `30s`  
**Description:** Time limit for connecting and running the query. A Go duration such as `"60s"`, or a number of seconds; it must be above zero

```yaml
with:
  query: "SELECT COUNT(*) FROM large_table"
  timeout: "60s"
```

A query that runs out of time is stopped, and the step goes on to its test with `res.code` set to `1` and `res.error` starting with `timed out after 60s`.

## Response Object

The database action provides a `res` object with the following properties:

| Property | Type | Description |
|----------|------|-------------|
| `code` | Integer | Operation result (0 = success, 1 = error) |
| `rows_affected` | Integer | Number of rows affected by the query |
| `rows` | Array | Query results for SELECT statements (as objects) |
| `error` | String | Error message if operation failed |

Besides `res`, the step can test these:

| Field | Type | Description |
|-------|------|-------------|
| `status` | Integer | `0` when the query succeeds |
| `rt.duration` | String | How long the query took, such as `"1.3ms"` |
| `rt.sec` | Float | The same in seconds |

## Response Examples

What the response holds depends on the statement. One whose first word is `SELECT`, `SHOW`, `DESCRIBE`, `DESC`, `EXPLAIN` or `WITH`, in any case, returns rows; any other, such as an `INSERT` or `UPDATE`, returns counts instead.

### SELECT Query Response

Rows come back in `res.rows`, and the count of them in `res.rows_affected`.

```yaml
steps:
  - name: "Fetch Users"
    id: fetch_users
    uses: db
    with:
      dsn: "mysql://user:pass@localhost/db"
      query: "SELECT id, name, email FROM users WHERE active = ?"
      params: [true]
    test: res.code == 0 && res.rows_affected > 0
    outputs:
      user_count: res.rows_affected
      first_user_id: res.rows[0].id
      first_user_name: res.rows[0].name
```

### INSERT/UPDATE Query Response

A write returns no rows, so the result is in the count of rows it changed.

```yaml
steps:
  - name: "Insert User"
    uses: db
    with:
      dsn: "postgres://user:pass@localhost/db"
      query: "INSERT INTO users (name, email) VALUES ($1, $2)"
      params: ["John Doe", "john@example.com"]
    test: res.code == 0 && res.rows_affected == 1
```

## Database-Specific Features

The `dsn` and `query` parameters are the same for every driver, but the syntax inside the query is not. The examples below show what each supported database adds.

### MySQL Examples

Connection options go in the DSN, and stored procedures are called like any other statement.

```yaml
# MySQL with connection options
- name: "MySQL Query"
  uses: db
  with:
    dsn: "mysql://user:pass@tcp(localhost:3306)/database?charset=utf8mb4&parseTime=true"
    query: "SELECT VERSION() as mysql_version, NOW() as current_time"
  test: res.code == 0

# MySQL stored procedure
- name: "Call Procedure"
  uses: db
  with:
    dsn: "mysql://user:pass@localhost:3306/database"
    query: "CALL GetUsersByDepartment(?)"
    params: ["Engineering"]
  test: res.code == 0
```

### PostgreSQL Examples

PostgreSQL adds JSON and array operators that can be used directly in the query.

```yaml
# PostgreSQL with JSON operations
- name: "JSON Query"
  uses: db
  with:
    dsn: "postgres://user:pass@localhost:5432/database?sslmode=disable"
    query: |
      SELECT name, data->>'role' as role, data->'preferences' as prefs
      FROM users 
      WHERE data ? 'role' AND data->>'role' = $1
    params: ["admin"]
  test: res.code == 0

# PostgreSQL array operations
- name: "Array Query"
  uses: db
  with:
    dsn: "postgres://user:pass@localhost:5432/database"
    query: "SELECT name FROM users WHERE tags && $1"
    params: ['{"admin","moderator"}']
  test: res.code == 0
```

### SQLite Examples

SQLite takes a file path, or `:memory:` for a database that lasts only as long as the step.

```yaml
# SQLite with file creation
- name: "SQLite Query"
  uses: db
  with:
    dsn: "file:./testdata/sqlite.db"
    query: |
      CREATE TABLE IF NOT EXISTS users (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        email TEXT UNIQUE,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
      )
  test: res.code == 0

# SQLite with in-memory database
- name: "Memory Database"
  uses: db
  with:
    dsn: "file::memory:"
    query: "CREATE TABLE temp_data (id INTEGER, value TEXT)"
  test: res.code == 0
```

## Common Query Patterns

Database steps in a workflow tend to do one of three things: assert that the data is in the state it should be, watch how the database itself is behaving, or write a batch of records for later steps to read.

### Data Validation Queries

A query that counts what should not exist turns an invariant into a test.

```yaml
- name: "Check Data Integrity"
  uses: db
  with:
    dsn: "mysql://user:pass@localhost/db"
    query: |
      SELECT 
        COUNT(*) as total_users,
        COUNT(CASE WHEN active = 1 THEN 1 END) as active_users,
        COUNT(CASE WHEN email IS NULL THEN 1 END) as missing_emails
      FROM users
  test: |
    res.code == 0 && 
    res.rows[0].total_users > 0 &&
    res.rows[0].missing_emails == 0
```

### Performance Monitoring

The database's own statistics tables report connections and slow queries.

```yaml
- name: "Database Performance Check"
  uses: db
  with:
    dsn: "postgres://user:pass@localhost/db"
    query: |
      SELECT 
        schemaname, 
        tablename, 
        seq_scan, 
        seq_tup_read, 
        idx_scan, 
        idx_tup_fetch
      FROM pg_stat_user_tables 
      WHERE seq_scan > 1000
    timeout: "10s"
  test: res.code == 0
  outputs:
    high_seq_scan_tables: res.rows_affected
```

### Batch Operations

Inserting several rows in one statement prepares the data that later steps read.

```yaml
- name: "Batch Insert"
  uses: db
  with:
    dsn: "mysql://user:pass@localhost/db"
    query: |
      INSERT INTO audit_log (action, table_name, record_id, timestamp) VALUES
      ('CREATE', 'users', 123, NOW()),
      ('UPDATE', 'profiles', 456, NOW()),
      ('DELETE', 'sessions', 789, NOW())
  test: res.code == 0 && res.rows_affected == 3
```

## Security Features

The database action implements several security measures:

- **Prepared Statements**: All parameterized queries use prepared statements to prevent SQL injection
- **Connection String Masking**: Passwords are masked in logs and output
- **Timeout Protection**: Prevents long-running queries from hanging
- **Driver Validation**: Only supports approved database drivers
- **DSN Validation**: Validates connection string format before execution

## Under a Guard

Run with `--read-only`, the action runs only one statement that starts with `SELECT`, `SHOW`, `DESCRIBE`, `DESC`, `EXPLAIN` or `WITH`, and refuses any other statement before connecting, as well as a query that holds a semicolon anywhere but at its end, in a string or a comment too: what ends a string or a comment differs from one database, and one connection setting, to another, so that a semicolon taken for text could start a second statement. It also refuses a DSN that would have the driver run a statement as the connection opens, before the read-only transaction: for MySQL `multiStatements` and any system variable, which go-sql-driver sets with a statement of its own, and for SQLite any parameter but `mode`, `cache`, `immutable`, `_txlock` and `_time_format`, such as `_pragma`. It runs that statement in a read-only transaction, or on a SQLite connection that only queries, so that the database itself refuses a write the statement hides, such as `WITH x AS (DELETE ...) SELECT ...`; that step fails as the database reports it. Run with `--allow-host`, it refuses a DSN unless the run allows each server the driver may connect to, as the driver itself reads the DSN, at the driver's port when none is named. For MySQL it is the address go-sql-driver dials, read from the DSN the URL is turned into. For PostgreSQL they are the servers lib/pq resolves the DSN to, the same DSN it connects with: its parameters, a service file, and `PGHOST`, `PGHOSTADDR`, `PGPORT` and the like count, every host of a list is checked, and the address of `hostaddr`, when it is given, is the one checked. A DSN lib/pq cannot read is refused. A SQLite file names no host. A refused step fails with the kind `refused`. See [`--read-only`](/reference/cli-reference#--read-only).

## Error Handling

A query the database rejects, or a connection that fails, does not stop the step: `res.code` is `1` and the database's message is in `res.error`, so the test can check for the failure it expects. The message depends on the database; SQLite reports a missing table as `no such table`.

```yaml
- name: "Query a Table That Does Not Exist"
  uses: db
  with:
    dsn: "file:./testdata/app.db"
    query: "SELECT * FROM missing_table"
  test: res.code == 1 && res.error contains "no such table"
  outputs:
    error: res.error
```

## Transaction Examples

While the action doesn't directly support transactions, you can use database-specific transaction syntax:

```yaml
# PostgreSQL transaction
- name: "Begin Transaction"
  uses: db
  with:
    dsn: "postgres://user:pass@localhost/db"
    query: "BEGIN"
  test: res.code == 0

- name: "Insert Data"
  uses: db
  with:
    dsn: "postgres://user:pass@localhost/db"
    query: "INSERT INTO users (name) VALUES ($1)"
    params: ["Test User"]
  test: res.code == 0

- name: "Commit Transaction"
  uses: db
  with:
    dsn: "postgres://user:pass@localhost/db"
    query: "COMMIT"
  test: res.code == 0
```
