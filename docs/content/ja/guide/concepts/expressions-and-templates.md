# 式とテンプレート

式とテンプレートは、Probeワークフローで実行時に値を求める仕組みです。条件付きロジック、データ変換、動的設定を可能にします。このガイドでは、式システム、テンプレート構文、高度な使用パターンについて詳しく説明します。

## 式システム概要

Probeは2つのタイプの式を使用します：

1. **テンプレート式** (`{{expression}}`) - 動的値の挿入用
2. **テスト式** (`expression`) - ブール条件と検証用

両方とも、セキュリティ強化とカスタム関数を備えた [expr](https://github.com/antonmedv/expr) ベースの同じ基礎式エンジンを使用しています。

## テンプレート式

テンプレート式は`{{}}`構文を使用して文字列に動的値を挿入します。

### 基本テンプレート構文

`{{ }}`の中は評価され、その値に置き換わります。入れ子になったデータへのパスも書けます。

```yaml
# シンプルな変数置換
- name: Greet User
  echo: "Hello {{vars.USERNAME}}!"

# ネストされたデータへのアクセス
- name: API Request
  uses: http
  with:
    method: GET
    url: "{{vars.API_BASE_URL}}/users/{{outputs.auth.user_id}}"
    headers:
      Authorization: "Bearer {{outputs.auth.access_token}}"

# 複雑な式
- name: Dynamic Configuration
  echo: "Environment: {{vars.NODE_ENV || 'development'}}, Users: {{outputs.api.user_count || 0}}"
```

テンプレートは、テキスト中で最初に現れる`}}`ではなく、そのテンプレートを閉じる`}}`で終わります。マップリテラルの波括弧は入れ子として数え、引用符で囲んだ文字列はまとめて読み飛ばすため、次の例はどちらも1つのテンプレートです。

```yaml
- name: Braces inside a template
  uses: hello
  echo: "port={{ {'db': {'port': 5432}}.db.port }}, id={{ parse_json('{\"user\": {\"id\": 1}}').user.id }}"
```

`{{{`は、文字の`{`とそれに続くテンプレートとして読みます。そのため`{{{vars.name}}}`は値を波括弧で囲んで出力します。マップリテラルで始まるテンプレートは、`{{ {'a': 1} }}`のように`{{`のあとに空白を入れてください。閉じられていない`{{`は文字のまま残ります。

マップのキーにもテンプレートを書けます。前のステップが返したIDやヘッダー名のように、ワークフローを実行するまで分からない値をキーにできます。

```yaml
- name: Activate the tenant
  uses: http
  with:
    post: /status
    headers:
      content-type: application/json
      "{{ vars.tenant_header }}": "{{ outputs.tenant.id }}"
    body:
      type: status
      "{{ outputs.tenant.id }}":
        active: true
```

キーは文字列なので、テンプレートは文字列として評価します。評価した結果が同じになるキーが2つあるとエラーになり、評価できないキーもエラーになります。エラーは、書かれたままのキーを示します。キーで別の変数を読む変数は、値で読む場合と同じく、その変数の後に評価されます。ただし、変数名そのもの（`vars`の直下のキー）は評価しません。書かれたままの名前の変数になります。

### テンプレート式コンテキスト

テンプレート式はいくつかのデータソースにアクセスできます：

#### 環境変数 (`env`)

環境の値は`vars`から読みます。値がない場合は`||`で既定値を与えます。

```yaml
variables:
  api_url: "{{vars.API_URL}}"                    # 環境変数
  port: "{{vars.PORT || '3000'}}"               # デフォルト値付き
  debug_mode: "{{vars.DEBUG == 'true'}}"        # ブール変換
```

#### ステップ出力 (`outputs`)

`outputs`を宣言したステップは、その`id`を使って後続のステップから参照できます。

```yaml
steps:
  - name: Get User Info
    id: user-info
    uses: http
    with:
      method: GET
      url: "{{vars.API_URL}}/user/current"
    outputs:
      user_id: res.body.id
      user_name: res.body.name
      user_email: res.body.email

  - name: Send Welcome Email
    uses: smtp
    with:
      addr: "{{vars.smtp_addr}}"
      from: "probe@example.com"
      to: "{{outputs['user-info'].user_email}}"
      subject: "Welcome {{outputs['user-info'].user_name}}!"
      session: 1
      message: 1
      length: 500
    echo: "Your user ID is: {{outputs['user-info'].user_id}}"
```

#### ジョブ出力 (ジョブ間参照)

ジョブをまたぐ場合は参照がジョブ名から始まります。そのため、読む側のジョブは`needs`でそのジョブを指定する必要があります。

```yaml
jobs:
- id: setup
  name: setup
  steps:
    - name: Initialize
      id: setup
      outputs:
        session_id: "{{random_str(16)}}"

- id: main-test
  name: main-test
  needs: [setup]
  steps:
    - name: Use Session
      uses: http
      with:
        headers:
          X-Session-ID: "{{outputs.setup.session_id}}"
```

### 高度なテンプレートパターン

テンプレートでできるのは値の参照だけではありません。値の選択、文字列の加工、算術演算、入れ子になったデータへのアクセスも行えます。

#### 条件付き値

三項演算子は2つの値から一方を選び、`||`は左辺が空のときに右辺へ切り替えます。

```yaml
# 三項演算子
- name: Environment-specific URL
  echo: "URL: {{vars.NODE_ENV == 'production' ? 'https://api.prod.com' : 'https://api.dev.com'}}"

# Null 合体
- name: Default Configuration
  echo: "Timeout: {{vars.TIMEOUT || '30s'}}"
```

#### 文字列操作

テンプレートは互いに隣接させることも、リテラルの文字列と並べることもできます。パスやURLはこの方法で組み立てます。

```yaml
# 文字列連結
- name: Build File Path
  echo: "File: {{vars.BASE_PATH}}/{{vars.FILE_NAME}}.{{vars.FILE_EXT}}"

# 文字列メソッド（限定サポート）
- name: Format Output
  echo: "User: {{upper(outputs.user.name)}} ({{lower(outputs.user.email)}})"
```

#### 算術演算

出力から読んだ数値はテンプレートの中で計算できます。集計用のステップの中で数値を求められます。

```yaml
# 数学的演算
- name: Calculate Metrics
  echo: |
    Performance Metrics:
    Average Response Time: {{(outputs.test1.time + outputs.test2.time + outputs.test3.time) / 3}}ms
    Total Requests: {{outputs.test1.requests + outputs.test2.requests + outputs.test3.requests}}
    Success Rate: {{(outputs.successful.count / outputs.total.count) * 100}}%
```

#### 複雑なデータアクセス

配列はインデックスで、オブジェクトのプロパティは名前で参照します。どちらも同じ式の中に書けます。

```yaml
# 配列アクセス
- name: Process User List
  echo: "First user: {{outputs.users.list[0].name}}"

# オブジェクトプロパティアクセス
- name: Nested Data Access
  echo: "Database: {{outputs.config.database.host}}:{{outputs.config.database.port}}"
```

## テスト式

テスト式は`test`と`skipif`で使うブール式です。

### 基本テスト構文

`test`には真偽値の式を書きます。この式は、そのステップの実行結果に対して評価されます。

```yaml
# シンプルなステータスチェック
- name: Health Check
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/health"
  test: res.code == 200

# 複雑な条件
- name: Comprehensive API Test
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/api/data"
  test: |
    res.code == 200 &&
    res.body.success == true &&
    res.body.data != nil &&
    (rt.sec * 1000) < 1000
```

### HTTPレスポンステスト

`res`オブジェクトは包括的なレスポンスデータを提供します：

```yaml
# ステータスコードテスト
test: res.code == 200
test: res.code >= 200 && res.code < 300
test: res.code in [200, 201, 202]

# レスポンス時間テスト
test: (rt.sec * 1000) < 1000                           # 1秒未満
test: (rt.sec * 1000) >= 100 && (rt.sec * 1000) <= 500      # 100-500ms の間

# レスポンスサイズテスト
test: res.body_size > 0                        # コンテンツあり
test: res.body_size < 1048576                  # 1MB 未満

# ヘッダーテスト
test: res.headers["Content-Type"] == "application/json"
test: res.headers["X-Rate-Limit-Remaining"] > "10"

# JSON レスポンステスト
test: res.body.status == "success"
test: len(res.body.data.users) > 0
test: res.body.error == nil

# テキストレスポンステスト
test: res.body contains "Success"
test: res.body startsWith "<!DOCTYPE html>"
test: len(res.body) > 100
```

### 高度なテスト条件

フィールドと定数を比較する以外にも、パターンとの一致、コレクションの検査、複数の検査を1つの条件にまとめる書き方があります。

#### 正規表現

`matches`はパターンを照合します。ボディ全体にも、個々のフィールドにも適用できます。

```yaml
# レスポンステキストのパターンマッチング
test: res.body matches "user-\\d+@example\\.com"

# JSON フィールドパターン検証
test: res.body.user.email matches "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}"
```

#### 配列とオブジェクトのテスト

`len`と`contains`はコレクションに対して使います。`all`と`any`は、条件をすべての要素に、あるいは少なくとも1つの要素に適用します。

```yaml
# 配列テスト
test: len(res.body.users) == 5
test: res.body.tags contains "production"
test: 'all(res.body.permissions, #.active == true)'
test: 'any(res.body.items, #.price > 100)'

# オブジェクトプロパティテスト
test: '"id" in res.body.user && "email" in res.body.user'
test: res.body.config.database.host != nil
```

#### 複雑な論理条件

括弧でまとめれば、1つのテストで複数の正常な結果を受け付けられます。

```yaml
# 複数条件検証
test: |
  (res.code == 200 && res.body.success == true) ||
  (res.code == 202 && res.body.processing == true)

# ネストした条件検証
test: |
  res.code == 200 &&
  res.body.data != nil &&
  (
    (res.body.data.type == "user" && res.body.data.user.active == true) ||
    (res.body.data.type == "system" && res.body.data.system.healthy == true)
  )
```

## 組み込み関数

Probeは一般的な操作のためのいくつかの組み込み関数を提供します。

### ランダム関数

ランダムな値を使うと、繰り返し実行したときに同じレコードで衝突することを避けられます。

#### `random_int(max)`
ランダムな整数を生成：

```yaml
# ランダムなユーザーID生成
- name: Create Test User
  uses: http
  with:
    url: "{{vars.API_URL}}/users"
    method: POST
    body: |
      {
        "id": {{random_int(999999)}},
        "name": "TestUser{{random_int(1000)}}",
        "group": {{random_int(10)}}
      }
```

#### `random_str(length)`
ランダムな文字列を生成：

```yaml
# ユニークな識別子を生成
- name: Create Session
  outputs:
    session_id: "session_{{random_str(16)}}"
    transaction_id: "txn_{{random_str(12)}}"
    correlation_id: "{{random_str(32)}}"

# テストデータ生成
- name: Create Test Record
  uses: http
  with:
    body: |
      {
        "username": "user_{{random_str(8)}}",
        "email": "test_{{random_str(6)}}@example.com",
        "api_key": "{{random_str(40)}}"
      }
```

### 時間関数

時間関数は現在時刻を返します。タイムスタンプや、実行ごとに異なる必要がある値に使います。

#### `unixtime()`
現在のUnixタイムスタンプを取得：

```yaml
# リクエストにタイムスタンプを追加
- name: Timestamped Request
  uses: http
  with:
    url: "{{vars.API_URL}}/events"
    method: POST
    body: |
      {
        "event": "test_execution",
        "timestamp": {{unixtime()}},
        "execution_id": "exec_{{unixtime()}}_{{random_str(8)}}"
      }

# 時間ベースのテスト
- name: Check Timestamp
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/status"
  test: res.body.server_time >= {{unixtime() - 300}}  # 過去5分以内
```

### カスタム関数使用パターン

これらの関数は組み合わせたときに役立ちます。衝突しないテストデータの生成と、1回の実行のステップ同士を結び付ける識別子の生成です。

#### ユニークテストデータ生成

識別子を1度だけ生成して使い回せば、後続のステップは最初のステップが作成したレコードを指定できます。

```yaml
jobs:
- id: user-lifecycle-test
  name: user-lifecycle-test
  steps:
    - name: Create Unique User
      id: create-user
      uses: http
      with:
        url: "{{vars.API_URL}}/users"
        method: POST
        body: |
          {
            "username": "testuser_{{unixtime()}}_{{random_str(6)}}",
            "email": "test_{{random_str(8)}}@example.com",
            "password": "{{random_str(16)}}",
            "user_id": {{random_int(1000000)}}
          }
      test: res.code == 201
      outputs:
        user_id: res.body.user.id
        username: res.body.user.username

    - name: Verify User Creation
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/{{outputs['create-user'].user_id}}"
      test: |
        res.code == 200 &&
        res.body.user.username == "{{outputs['create-user'].username}}"

    - name: Clean Up User
      uses: http
      with:
        url: "{{vars.API_URL}}/users/{{outputs['create-user'].user_id}}"
        method: DELETE
      test: res.code == 204
```

#### セッションと関連ID

開始時に生成した値を全リクエストに付けて送ると、サーバー側のログで1回の実行のリクエストがまとまります。

```yaml
jobs:
- id: distributed-trace-test
  name: distributed-trace-test
  steps:
    - name: Initialize Trace
      uses: hello
      id: trace
      echo: "Starting distributed trace"
      outputs:
        trace_id: "trace_{{unixtime()}}_{{random_str(16)}}"
        correlation_id: "corr_{{random_str(32)}}"

    - name: Service A Call
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_A_URL}}/process"
        headers:
          X-Trace-ID: "{{outputs.trace.trace_id}}"
          X-Correlation-ID: "{{outputs.trace.correlation_id}}"
      test: res.code == 200

    - name: Service B Call
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_B_URL}}/process"
        headers:
          X-Trace-ID: "{{outputs.trace.trace_id}}"
          X-Correlation-ID: "{{outputs.trace.correlation_id}}"
      test: res.code == 200

    - name: Verify Trace Correlation
      uses: http
      with:
        method: GET
        url: "{{vars.TRACING_URL}}/traces/{{outputs.trace.trace_id}}"
      test: |
        res.code == 200 &&
        len(res.body.spans) >= 2 &&
        res.body.correlation_id == "{{outputs.trace.correlation_id}}"
```

## 条件付きロジックパターン

`if`式は実行するかどうかを決めます。個々のステップにもジョブ全体にも指定でき、環境を参照する使い方が一般的です。

### ステップレベル条件

ステップは`skipif`が真のときスキップされます。`skipif`が参照する値は、先行ステップのoutputsとして公開しておきます。

```yaml
steps:
  - name: Check Primary Service
    id: primary
    uses: http
    with:
      method: GET
      url: "{{vars.primary_url}}/health"
    outputs:
      primary_healthy: res.code == 200

  - name: Check Secondary Service
    id: secondary
    uses: http
    skipif: outputs.primary.primary_healthy
    with:
      method: GET
      url: "{{vars.secondary_url}}/health"
    outputs:
      secondary_healthy: res.code == 200

  - name: Success Path
    uses: hello
    skipif: "!(outputs.primary_healthy || (outputs.secondary_healthy ?? false))"
    echo: "At least one service is healthy"

  - name: Failure Path
    uses: hello
    skipif: outputs.primary_healthy || (outputs.secondary_healthy ?? false)
    echo: "All services are down!"
```

### ジョブレベル条件

ジョブの`skipif`からは`vars`と、依存しているジョブのoutputsを参照できます。

```yaml
jobs:
- id: health-check
  name: Health Check
  steps:
    - name: Basic Health Check
      id: health
      uses: http
      with:
        method: GET
        url: "{{vars.api_url}}/health"
      outputs:
        healthy: res.code == 200

- name: Detailed Analysis
  needs: [health-check]
  skipif: outputs.health.healthy
  steps:
    - name: Deep Diagnostic
      uses: http
      with:
        method: GET
        url: "{{vars.api_url}}/diagnostics"
      test: res.code == 200

- name: Performance Test
  needs: [health-check]
  skipif: "!outputs.health.healthy"
  steps:
    - name: Load Test
      uses: http
      with:
        method: GET
        url: "{{vars.api_url}}/load-test"
      test: res.code == 200
```


### 環境ベースの条件

`skipif`は環境を参照します。特定の環境向けのステップは、それ以外の環境では飛ばされます。

```yaml
steps:
  - name: Development Setup
    uses: hello
    skipif: vars.node_env != "development"
    echo: "Running in development mode"

  - name: Production Validation
    uses: http
    skipif: vars.node_env != "production"
    with:
      method: GET
      url: "{{vars.api_url}}/production-check"
    test: res.code == 200

  - name: Feature Flag Check
    uses: http
    skipif: "!(vars.feature_flags contains \"new-api\")"
    with:
      method: GET
      url: "{{vars.api_url}}/v2/endpoint"
    test: res.code == 200
```

## セキュリティ考慮事項

式はワークフローの内部で評価されます。そのため、式から何を参照できるか、何を出力するかがどちらも問題になります。

### 式のセキュリティ機能

Probeはいくつかのセキュリティ対策を実装しています：

1. **式長制限**: リソース枯渇を防止
2. **危険な関数ブロック**: システム関数へのアクセスをブロック
3. **環境変数フィルタリング**: 機密変数へのアクセスを制限
4. **タイムアウト保護**: 式での無限ループを防止

### 安全な式パターン

式で行うのは値の参照であって、検証していない入力から値を組み立てることではありません。

```yaml
# 良い例: 安全な環境変数アクセス
- name: Safe Config
  echo: "API URL: {{vars.API_URL}}"

# 良い例: 制限されたデータアクセス
- name: Safe Data Access
  test: len(res.body.users) <= 1000

# 避ける: 無制限の操作
# test: all(res.body.data.some_huge_array, expensive_operation(#))

# 良い例: シンプルな条件
- name: Simple Validation
  test: res.code == 200 && res.body.success == true

# 避ける: 複雑なネスト式
# test: deeply.nested.complex.expression.with.many.operations()
```

### 機密データの処理

秘密情報は環境変数に置き、出力に出さずに使います。

```yaml
# 良い例: シークレットに環境変数を使用
- name: Authenticated Request
  uses: http
  with:
    headers:
      Authorization: "Bearer {{vars.API_TOKEN}}"

# 良い例: 機密データのログを避ける
- name: Login Test
  uses: http
  with:
    body: |
      {
        "username": "{{vars.TEST_USERNAME}}",
        "password": "{{vars.TEST_PASSWORD}}"
      }
  # 機密レスポンスデータを出力しない
  outputs:
    login_successful: res.code == 200
    # NG: auth_token: res.body.token (ログに露出する)
```

## パフォーマンス最適化

式はステップごとに評価され、繰り返し実行するワークフローでは毎回評価し直されます。式を軽く保てば、その分のコストは増えません。

### 効率的な式の記述

`&&`は最初に偽となった時点で評価を止めます。そのため、軽い検査を先に書きます。

```yaml
# 良い例: シンプルで直接的な式
test: res.code == 200

# 良い例: && による早期終了
test: res.code == 200 && res.body.success == true

# 避ける: 式での複雑な計算
# test: expensive_calculation(res.body.large_dataset) == expected_value

# 良い例: 複雑な値を事前計算
outputs:
  user_count: len(res.body.users)
  active_users: 'len(filter(res.body.users, #.active == true))'
```

### テンプレート最適化

値を1つ置き換えるだけのテンプレートのコストはほぼゼロです。複数の操作で文字列を組み立てるテンプレートは、ステップごとにその分のコストがかかります。

```yaml
# 良い例: シンプルなテンプレート置換
echo: "User {{outputs.user.name}} logged in"

# 良い例: 最小限の文字列操作
url: "{{vars.BASE_URL}}/users/{{outputs.user.id}}"

# 避ける: 複雑なテンプレート式
# echo: "{{complex_calculation(outputs.data) + another_operation(vars.CONFIG)}}"
```

## 式のデバッグ

式が失敗しても、その理由はほとんど示されません。以下ではよく遭遇する症状を扱います。

### よくある問題と解決方法

失敗の多くは3つに分かれます。テンプレートが解決されない場合、テストが期待どおりに評価されない場合、そしてフィールドの値が誤っているのではなく存在しない場合です。

#### テンプレート式エラー

テンプレートは値をそのまま埋め込みます。そのため、前後のクォートは書く側の責任になります。

```yaml
# エラー: JSON でクォート不足
body: |
  {
    "name": {{outputs.user.name}}      # エラー: クォートなし
  }

# 解決方法: 適切な JSON クォート
body: |
  {
    "name": "{{outputs.user.name}}"    # 正解: 文字列をクォート
  }
```

#### テスト式デバッグ

理由がわからないままテストが失敗する場合は、比較する前にその式が読む値を出力します。

```bash
# 詳細モードでデバッグ
probe -v workflow.yml
```

```yaml
# デバッグ出力を追加
- name: Debug Values
  echo: |
    Debug Information:
    Status: {{res.code}}
    Response Time: {{rt.duration}}
    JSON Success: {{res.body.success}}
    Headers: {{res.headers}}
```

#### JavaScript や Python から来た場合

式は [expr-lang](https://expr-lang.org) で書きます。expr-lang の値はメソッドを持たないため、JavaScript や Python で値に対して呼ぶ処理は、演算子か関数で書きます。メソッド呼び出しはステップの実行時に `invalid operation: int(string)` などのエラーで失敗し、エラーの後に以下の書き方を示すヒントが続きます。`probe check` はワークフローを実行する前にこれを報告します。

| 誤り | 正しい書き方 |
|------|--------------|
| `res.body.contains("x")`, `res.body.includes("x")` | `res.body contains "x"`、リストなら `"x" in res.body.items` |
| `res.body.startsWith("x")`, `startsWith(res.body, "x")` | `res.body startsWith "x"`、または `hasPrefix(res.body, "x")` |
| `res.body.endsWith("x")` | `res.body endsWith "x"`、または `hasSuffix(res.body, "x")` |
| `res.body.matches("^a")` | `res.body matches "^a"` |
| `res.body.length`, `items.size()` | `len(res.body)`, `len(items)` |
| `s.toLowerCase()`, `s.toUpperCase()`, `s.trim()` | `lower(s)`, `upper(s)`, `trim(s)` |
| `s.split(",")`, `items.join(",")`, `s.replace("a", "b")` | `split(s, ",")`, `join(items, ",")`, `replace(s, "a", "b")` |
| `n.toString()`, `obj.keys()` | `string(n)`, `keys(obj)` |
| `===`, `!==` | `==`, `!=` |
| `null`, `undefined`, `None` | `nil` |

#### Null値の処理

存在しないフィールドはnullになり、それをそのまま比較しても意味のある結果は得られません。先に存在を確認します。

```yaml
# 良い例: 潜在的な nil 値を処理
test: res.body.user != nil && res.body.user.active == true

# 良い例: デフォルト値を使用
echo: "User count: {{outputs.api.user_count || 0}}"

# 良い例: アクセス前に存在をチェック
test: '"data" in res.body && "users" in res.body.data'
```

## ベストプラクティス

式は書く回数よりも読む回数のほうがはるかに多く、ワークフローの中で最もデバッグしにくい部分です。以下では読みやすさを保つための指針を示します。

### 1. 式をシンプルに保つ

読み解く必要のある式は、失敗したときに読み違えられます。

```yaml
# 良い例: シンプルで読みやすい式
test: res.code == 200 && (rt.sec * 1000) < 1000

# 避ける: 過度に複雑な式
# test: (res.code >= 200 && res.code < 300) && ((rt.sec * 1000) < (vars.MAX_TIME || 1000)) && (len(filter(res.body.data.items, #.active && #.validated)) > 0)
```

### 2. 意味のある変数名を使用

出力の名前は後続のステップが読むものです。取得元ではなく、その値が何であるかを表す名前にします。

```yaml
# 良い例: 説明的な出力名
outputs:
  user_id: res.body.user.id
  auth_token: res.body.access_token
  expires_at: res.body.expires_in

# 避ける: 汎用的な名前
outputs:
  data1: res.body.user.id
  value: res.body.access_token
```

### 3. エッジケースを処理

各階層が存在することを確認してから読み進めます。そうすればフィールドの欠落がその場で失敗として現れます。

```yaml
# 良い例: 防御的プログラミング
test: |
  res.code == 200 &&
  res.body != nil &&
  res.body.users != nil &&
  len(res.body.users) > 0

# 良い例: デフォルト値を提供
echo: "Processing {{outputs.api.item_count || 0}} items"
```

### 4. 複雑な式を文書化

条件が業務上の規則を表している場合、その規則をコメントに書いておけば、次に読む人が式から読み取り直さずに済みます。

```yaml
- name: Complex Business Logic Validation
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/business-data"
  # テストは以下を検証:
  # 1. レスポンスが成功 (200)
  # 2. 処理時間が許容範囲 (< 2s)
  # 3. データ整合性が維持 (必須フィールド存在)
  # 4. ビジネスルールが満たされる (アクティブユーザー > 0、売上 > 閾値)
  test: |
    res.code == 200 &&
    (rt.sec * 1000) < 2000 &&
    res.body.users != nil &&
    res.body.revenue != nil &&
    len(filter(res.body.users, #.active == true)) > 0 &&
    res.body.revenue > 1000
```

## 次のステップ

式とテンプレートを理解したら、以下を探索してください：

1. **[データフロー](/ja/guide/concepts/data-flow)** - ワークフローを通してデータがどのように移動するかを学ぶ
2. **[テストとアサーション](/ja/guide/concepts/testing-and-assertions)** - 検証技術をマスターする
3. **[ハウツー](/ja/guide/how-tos/api-testing)** - 実用的な式使用パターンを見る

式とテンプレートは、Probeで実行時の値を扱う仕組みです。これらの概念をマスターして、柔軟でデータ駆動の自動化ワークフローを構築しましょう。
