# データベースアクション

`db`アクションはMySQL、PostgreSQL、SQLiteデータベースでSQLクエリを実行し、包括的な結果処理とエラーレポートを提供します。

## 基本的な構文

データベースのステップでは、接続文字列と実行する文を指定します。

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

## パラメータ

データベースステップは4つのパラメータで決まります。接続先、実行する文、バインドする値、待ち時間の上限です。

### `dsn` (必須)

**型:** String  
**説明:** 自動ドライバー検出付きのデータベース接続文字列  
**サポート:** テンプレート式

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

### `query` (必須)

**型:** String  
**説明:** 実行するSQLクエリ  
**サポート:** テンプレート式と複数行文字列

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

### `params` (オプション)

**型:** 混合値の配列 (String, Number, Boolean)  
**説明:** プリペアドステートメント用のクエリパラメータ  
**サポート:** テンプレート式

```yaml
with:
  query: "SELECT * FROM users WHERE id = ? AND active = ?"
  params: [123, true, "{{vars.user_email}}"]
```

### `timeout` (オプション)

**型:** Duration  
**デフォルト:** `30s`  
**説明:** 接続とクエリの実行にかける制限時間。`"60s"`のようなGoのduration形式か秒数で、0より大きい値

```yaml
with:
  query: "SELECT COUNT(*) FROM large_table"
  timeout: "60s"
```

制限時間を過ぎたクエリは止められ、ステップはそのままテストに進みます。このとき`res.code`は`1`になり、`res.error`は`timed out after 60s`で始まります。

## レスポンスオブジェクト

データベースアクションは次のプロパティを持つ`res`オブジェクトを提供します：

| プロパティ | 型 | 説明 |
|----------|------|-------------|
| `code` | Integer | 操作結果 (0 = 成功, 1 = エラー) |
| `rows_affected` | Integer | クエリによって影響を受けた行数 |
| `rows` | Array | SELECTステートメントのクエリ結果（オブジェクトとして） |
| `error` | String | 操作が失敗した場合のエラーメッセージ |

`res`のほかに、テストでは次の値も使えます。

| フィールド | 型 | 説明 |
|-------|------|-------------|
| `status` | Integer | クエリが成功すれば`0` |
| `rt.duration` | String | クエリにかかった時間。`"1.3ms"`など |
| `rt.sec` | Float | 同じ時間を秒で表した値 |

## レスポンス例

レスポンスの中身は実行した文によって変わります。最初の語が`SELECT`、`SHOW`、`DESCRIBE`、`DESC`、`EXPLAIN`、`WITH`のいずれか（大文字小文字は問わない）の文では行が返り、`INSERT`や`UPDATE`などそれ以外の文では件数が返ります。

### SELECTクエリレスポンス

取得した行は`res.rows`に、その件数は`res.rows_affected`に入ります。

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

### INSERT/UPDATEクエリレスポンス

書き込みでは行が返らないため、結果は変更された行数で確認します。

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

## データベース固有の機能

`dsn`と`query`の書き方はどのドライバでも共通ですが、クエリの中の構文は共通ではありません。以下では対応しているデータベースごとの書き方を示します。

### MySQL例

接続オプションはDSNに含めます。ストアドプロシージャも通常の文と同じように呼び出せます。

```yaml
# 接続オプション付きMySQL
- name: "MySQL Query"
  uses: db
  with:
    dsn: "mysql://user:pass@tcp(localhost:3306)/database?charset=utf8mb4&parseTime=true"
    query: "SELECT VERSION() as mysql_version, NOW() as current_time"
  test: res.code == 0

# MySQLストアドプロシージャ
- name: "Call Procedure"
  uses: db
  with:
    dsn: "mysql://user:pass@localhost:3306/database"
    query: "CALL GetUsersByDepartment(?)"
    params: ["Engineering"]
  test: res.code == 0
```

### PostgreSQL例

PostgreSQLではJSONや配列の演算子をクエリの中で直接使えます。

```yaml
# JSON操作付きPostgreSQL
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

# PostgreSQL配列操作
- name: "Array Query"
  uses: db
  with:
    dsn: "postgres://user:pass@localhost:5432/database"
    query: "SELECT name FROM users WHERE tags && $1"
    params: ['{"admin","moderator"}']
  test: res.code == 0
```

### SQLite例

SQLiteにはファイルパスを渡します。`:memory:`を指定すると、そのステップの間だけ存在するデータベースになります。

```yaml
# ファイル作成付きSQLite
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

# インメモリデータベースSQLite
- name: "Memory Database"
  uses: db
  with:
    dsn: "file::memory:"
    query: "CREATE TABLE temp_data (id INTEGER, value TEXT)"
  test: res.code == 0
```

## 一般的なクエリパターン

ワークフロー中のデータベースステップの用途は、おおむね3つに分かれます。データが期待する状態にあることの検証、データベース自体の挙動の監視、後続のステップが読むレコードの一括投入です。

### データ検証クエリ

存在してはならないものを数えるクエリを書けば、データの不変条件をそのままテストにできます。

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

### パフォーマンス監視

データベース自身の統計テーブルから、接続数やスロークエリを取得できます。

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

### バッチ操作

1つの文で複数行を挿入し、後続のステップが読むデータを用意します。

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

## セキュリティ機能

データベースアクションはいくつかのセキュリティ対策を実装しています：

- **プリペアドステートメント**: すべてのパラメータ化クエリでプリペアドステートメントを使用してSQLインジェクションを防止
- **接続文字列マスキング**: ログと出力でパスワードをマスク
- **タイムアウト保護**: 長時間実行されるクエリのハングを防止
- **ドライバー検証**: 承認されたデータベースドライバーのみをサポート
- **DSN検証**: 実行前に接続文字列形式を検証

## ガードの下での動作

`--read-only`を指定して実行すると、`SELECT`、`SHOW`、`DESCRIBE`、`DESC`、`EXPLAIN`、`WITH`のいずれかで始まる1文だけを実行し、それ以外の文は接続する前に拒否します。末尾以外にセミコロンを含むクエリも、文字列やコメントの中であっても拒否します。文字列やコメントの終わり方はデータベースや接続の設定によって異なり、文字列のつもりのセミコロンが2つ目の文を始めることがあるからです。読み取り専用トランザクションの前、接続を開くときにドライバに文を実行させるDSNも拒否します。MySQLでは`multiStatements`と、go-sql-driverが独自の文で設定するシステム変数、SQLiteでは`mode`、`cache`、`immutable`、`_txlock`、`_time_format`以外のパラメータ（`_pragma`など）です。その文は読み取り専用トランザクション（SQLiteでは問い合わせしかできない接続）で実行するため、`WITH x AS (DELETE ...) SELECT ...`のように許可した語で始まる文が書き込みを含んでいても、データベース自身が拒否します。その場合、ステップはデータベースが報告するとおりに失敗します。`--allow-host`を指定して実行すると、ドライバが接続しうる各サーバーがすべて許可されていない限り、DSNを拒否します。サーバーは、ドライバ自身がDSNを解釈した結果で確かめます。ポートがなければドライバの既定のポートとして扱います。MySQLでは、URLを変換したDSNからgo-sql-driverが読む、接続先のアドレスです。PostgreSQLでは、lib/pqがDSNを解決した結果を使います。これは接続に使うのと同じDSNです。パラメータ、サービスファイル、`PGHOST`、`PGHOSTADDR`、`PGPORT`なども考慮し、ホストのリストはすべて確かめ、`hostaddr`があればそのアドレスを確かめます。lib/pqが読めないDSNは拒否します。SQLiteのファイルはホストを持ちません。拒否されたステップは種類`refused`で失敗します。[`--read-only`](/ja/reference/cli-reference#--read-only)を参照してください。

## エラーハンドリング

データベースがクエリを拒否した場合や接続に失敗した場合も、ステップは止まりません。`res.code`が`1`になり、データベースのメッセージが`res.error`に入るので、想定した失敗かどうかをテストで確かめられます。メッセージはデータベースによって異なり、SQLiteは存在しないテーブルを`no such table`と報告します。

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

## トランザクション例

アクションは直接トランザクションをサポートしませんが、データベース固有のトランザクション構文を使用できます：

```yaml
# PostgreSQLトランザクション
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
