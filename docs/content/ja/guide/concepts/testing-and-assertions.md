# テストとアサーション

テストとアサーションは、Probeワークフローでステップの成否を決める条件です。アクションが期待される結果を生み出すことを検証し、システムの信頼性を確保します。このガイドではテスト式、アサーションパターン、ワークフローに堅牢な検証を組み込むための戦略について説明します。

## テストの基礎

Probeのすべてのステップには、アクションの結果を検証する`test`条件を含めることができます。テストはステップの成功または失敗を決定するブール式です。

### 基本テスト構造

テストはステップに書く1つの式で、それが真に評価されたときにステップは成功します。

```yaml
- name: API Health Check
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/health"
  test: res.code == 200
```

テスト式はレスポンス（`res`）を評価し、成功にはtrue、失敗にはfalseを返します。

### 失敗したテストが示すもの

テストは通常、複数の条件を`&&`でつなげて書きます。テストが偽になると、レポートはリクエストとレスポンスの前に、成立しなかった最初の条件と、その両辺が評価された値を示します：

```yaml
test: |
  res.code == 200 &&
  res.body.status == "healthy"
```

```
⏺ Health (Failed in 0.12s)
  ⎿ 0. ✗  API Health Check
           failed: res.body.status == "healthy"
             res.body.status = "degraded"
           request:
             headers:
               Accept: */*
               User-Agent: probe-http/1.0.0
             method: GET
             timeout: 30s
             url: https://api.example.com/health
           response:
             body:
               status: degraded
             code: 200
             headers:
               Content-Type: application/json
             status: 200 OK
```

条件はProbeが読み取った形で表示されるため、空白や引用符がワークフローの記述と異なることがあります。値が表示されるのは、`==`、`<`、`in`、`contains`などの比較と、`!`が否定する対象です。`"healthy"`のようにテスト自体に書かれた値は繰り返しません。`||`でつないだ条件は1つの条件として、まとめて表示されます。`let`で始まるテストは宣言の後ろを分解し、各条件から宣言した値を参照できます。`random_int`、`random_str`、`unixtime`、`now`のように呼び出すたびに値が変わる関数を使うテストは、評価し直すと別の条件を指しかねないため分解せず、リクエストとレスポンスだけを表示します。同じ内容は`--report`が書き出すファイルの失敗メッセージにも、`test evaluated to false: res.body.status == "healthy" (res.body.status = "degraded")`の形で入ります。

リクエストとレスポンスは、`--verbose`と同じく、名前の順に1項目ずつ表示されます。`GET`が送らなかったボディのような、リクエストとレスポンス直下の空の項目は省きます。0や`false`は省きません。JSONボディのキーや行の列のように、項目が持つ中身は空の値も含めてすべて表示します。値は512バイトか20行、マップやリストは30項目で切り、`… (1234 more bytes)`や`… (80 more lines)`のように切った量を示します。大きなボディに失敗の理由が埋もれないようにするためで、`--verbose`は切らずに表示します。コマンドの出力のような複数行のテキストは、名前の下の`|`に続けて1行ずつ表示します。改行で終わらないテキストは`|-`に続けます。1行のテキストは、別の値と読み違えうるときに引用符で囲みます。`"1"`は文字列、`1`は数値で、`""`は空文字列です。バイナリデータはサイズだけを表示します。シークレットは、ほかの出力と同じく、どちらでも隠されます。レスポンスの`dump`を`false`にするアクションでは、どちらも表示しません。

### テスト式のコンテキスト

テスト式は包括的なレスポンスデータにアクセスできます：

```yaml
# HTTP レスポンステストコンテキスト
test: |
  res.code == 200 &&           // HTTP ステータスコード
  (rt.sec * 1000) < 1000 &&             // レスポンス時間（ミリ秒）
  len(res.rawbody ?? res.body) > 0 &&  // レスポンスボディの長さ
  res.headers["Content-Type"] == "application/json" &&  // レスポンスヘッダー
  res.body.status == "healthy"  // 解析された JSON レスポンス
```

## HTTPレスポンステスト

HTTPレスポンスで検証する対象は4つあります。ステータスコード、応答にかかった時間、返ってきたデータの大きさ、ヘッダーです。

### ステータスコード検証

`res.code`は単一の値とも、範囲とも、許容するコードの集合とも比較できます。

```yaml
# 正確なステータスコード
test: res.code == 200

# ステータスコード範囲
test: res.code >= 200 && res.code < 300

# 複数の許可されるコード
test: res.code in [200, 201, 202]

# クライアント vs サーバーエラー
test: res.code < 400  # 成功またはリダイレクト
test: res.code >= 400 && res.code < 500  # クライアントエラー
test: res.code >= 500  # サーバーエラー
```

### レスポンス時間テスト

`rt.sec`には往復にかかった秒数が入るため、時間の上限は通常の比較として書けます。

```yaml
# パフォーマンス検証
test: (rt.sec * 1000) < 1000                    # 1秒以内にレスポンス必要
test: (rt.sec * 1000) >= 100 && (rt.sec * 1000) <= 500  # レスポンス時間範囲
test: (rt.sec * 1000) < int(vars.MAX_RESPONSE_TIME ?? 2000)  # 設定可能な閾値

# パフォーマンスカテゴリ
test: |
  res.code == 200 && (
    (rt.sec * 1000) < 200 ? "excellent" :
    (rt.sec * 1000) < 500 ? "good" :
    (rt.sec * 1000) < 1000 ? "acceptable" : "poor"
  ) != "poor"
```

### レスポンスサイズ検証

ボディの長さ`len(res.rawbody ?? res.body)`を使うと、ボディが空の場合と、想定よりはるかに大きい場合を検出できます。`res.rawbody`はレスポンスがJSONだったときのパース前のボディで、それ以外のときは`res.body`がボディそのものです。

```yaml
# コンテンツ存在
test: len(res.rawbody ?? res.body) > 0  # コンテンツあり
test: len(res.rawbody ?? res.body) > 100  # 最小コンテンツサイズ
test: len(res.rawbody ?? res.body) < 1048576  # 最大 1MB レスポンス

# サイズベース検証
test: |
  res.code == 200 &&
  len(res.rawbody ?? res.body) > 50 &&  // 空のエラーメッセージでない
  len(res.rawbody ?? res.body) < 100000  // 予期せず大きくない
```

### ヘッダー検証

`res.headers`はヘッダー名で参照します。コンテンツタイプや、応答が持つべきセキュリティ関連のヘッダーを確認できます。

```yaml
# コンテンツタイプチェック
test: res.headers["Content-Type"] == "application/json"
test: res.headers["Content-Type"] startsWith "text/"
test: res.headers["Content-Type"] contains "charset=utf-8"

# セキュリティヘッダー
test: |
  "X-Frame-Options" in res.headers &&
  "X-Content-Type-Options" in res.headers &&
  res.headers["X-Frame-Options"] == "DENY"

# キャッシュ制御
test: res.headers["Cache-Control"] contains "no-cache"

# レート制限
test: res.headers["X-Rate-Limit-Remaining"] > "10"

# カスタムヘッダー
test: |
  "X-Request-Id" in res.headers &&
  len(res.headers["X-Request-Id"]) == 36  // UUID形式
```

## JSONレスポンステスト

ボディがJSONの場合、Probeはこれをパースします。そのためテストでは、テキストを照合するのではなくフィールドを直接参照できます。

### 基本JSON検証

ボディがパースされていれば、テストはフィールド名をたどって中身を参照できます。

```yaml
# JSON 構造検証
test: |
  res.code == 200 &&
  res.body != nil &&
  res.body.status == "success" &&
  res.body.data != nil

# 必須フィールド存在
test: |
  "id" in res.body &&
  "name" in res.body &&
  "email" in res.body &&
  "created_at" in res.body
```

### データタイプ検証

`type`を使うと、フィールドが存在するかだけでなく、クライアントが期待する型で返っているかを検証できます。JSONの数値は`float`、オブジェクトは`map`として読まれます。

```yaml
# タイプチェック
test: |
  type(res.body.id) == "float" &&
  type(res.body.name) == "string" &&
  type(res.body.active) == "bool" &&
  type(res.body.tags) == "array" &&
  type(res.body.metadata) == "map"

# 値制約
test: |
  res.body.id > 0 &&
  len(res.body.name) >= 2 &&
  res.body.score >= 0 && res.body.score <= 100
```

### 配列とコレクションテスト

`len`で要素数を取得し、要素はインデックスで参照して中身を確認します。

```yaml
# 配列検証
test: |
  res.body.users != nil &&
  len(res.body.users) > 0 &&
  len(res.body.users) <= 100

# 配列コンテンツ検証
test: |
  all(res.body.users, #.id != nil &&
    #.email != nil)

# 特定要素チェック
test: |
  any(res.body.users, #.role == "admin") &&
  len(filter(res.body.users, #.active == true)) > 0

# 配列ユニーク性
test: |
  len(res.body.user_ids) == len(uniq(res.body.user_ids))
```

### ネストしたデータ検証

入れ子のフィールドに到達するには途中の階層がすべて存在している必要があるため、上から順に確認します。

```yaml
# 深いオブジェクト検証
test: |
  res.body.user != nil &&
  res.body.user.profile != nil &&
  res.body.user.profile.preferences != nil &&
  res.body.user.profile.preferences.notifications == true

# 複雑なネスト構造
test: |
  all(res.body.data.orders, #.id != nil &&
    len(#.items) > 0 &&
    all(#.items, #.product_id != nil && #.quantity > 0 && #.price > 0) &&
    #.total == sum(map(#.items, #.quantity * #.price)))
```

## テキストレスポンステスト

ボディがJSONでない場合は文字列として扱います。テストは部分一致、正規表現、長さのいずれかで書きます。

### パターンマッチング

`contains`、`startsWith`、`endsWith`はボディを文字列として扱います。`lower`を挟めば大文字と小文字を区別せずに比較できます。

```yaml
# シンプルなテキストマッチング
test: res.body contains "success"
test: res.body startsWith "<!DOCTYPE html>"
test: res.body endsWith "</html>"

# 大文字小文字を区別しないマッチング
test: lower(res.body) contains "error"

# 複数パターン
test: |
  res.body contains "status" &&
  res.body contains "healthy" &&
  !(res.body contains "error")
```

### 正規表現テスト

`matches`は正規表現で照合します。値が固定の文字列ではなく形式で決まる場合に使います。

```yaml
# レスポンス内のメール検証
test: res.body matches "user-\\d+@example\\.com"

# URL パターン検証
test: res.body matches "https://[a-zA-Z0-9.-]+/api/v\\d+/"

# データフォーマット検証
test: |
  res.body matches "\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}Z"  // ISO タイムスタンプ

# バージョンとそのメジャー番号
test: |
  res.body matches "Version: v\\d+\\.\\d+\\.\\d+" &&
  res.body matches "Version: v([2-9]|[1-9][0-9]+)\\."  // メジャーバージョン2以上
```

### コンテンツ長と品質

ボディに対する`len`で分量の範囲を決められます。同じ式で、返ってきたのがエラーページでないことも確認できます。

```yaml
# コンテンツ長検証
test: |
  len(res.body) > 100 &&
  len(res.body) < 10000

# コンテンツ品質チェック
test: |
  len(split(res.body, "\n")) > 5 &&           // 複数行コンテンツ
  !(res.body contains "Lorem ipsum") &&          // プレースホルダーテキストでない
  len(split(res.body, " ")) > 20               // 実質的なコンテンツ
```

## 高度なテストパターン

単一のレスポンスだけでは書けない検証もあります。実行時の状況、先行するステップ、アプリケーションが守るべき業務ルールに依存する検証です。

### 条件付きテスト

テストは式なので、実行している環境に応じてしきい値自体を切り替えられます。

```yaml
# 環境固有テスト
test: |
  res.code == 200 &&
  (vars.NODE_ENV == "development" ? 
    (rt.sec * 1000) < 5000 :           // 開発環境では緩い設定
    (rt.sec * 1000) < 1000             // プロダクション用は厳格
  )

# 機能フラグテスト
test: |
  res.code == 200 &&
  (res.body.features.beta_enabled == true ?
    res.body.beta_data != nil :    // ベータ機能にはデータが必要
    res.body.beta_data == nil      // ベータ機能は存在しないべき
  )
```

### ステップ間検証

ステップは先行するステップの出力に対しても検証できます。2つのエンドポイントの結果が一致するかはこの方法で確認します。

```yaml
vars:
  API_URL: "{{API_URL}}"

jobs:
- id: data-consistency-test
  name: data-consistency-test
  steps:
    - name: Get User Count
      id: user-count
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/count"
      test: res.code == 200
      outputs:
        total_users: res.body.count

    - name: Get User List
      id: user-list
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users"
      test: |
        res.code == 200 &&
        len(res.body.users) == outputs['user-count'].total_users  // 整合性チェック
      outputs:
        user_list: res.body.users

    - name: Validate User Data Integrity
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/{{outputs['user-list'].user_list[0].id}}"
      test: |
        res.code == 200 &&
        res.body.user.id == outputs['user-list'].user_list[0].id &&
        res.body.user.email == outputs['user-list'].user_list[0].email
```

### ビジネスロジックテスト

個々のフィールドではなくフィールド間で成り立つ規則もあります。合計金額が明細と一致するかといった検証です。

```yaml
- name: E-commerce Business Logic Test
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/orders/{{vars.TEST_ORDER_ID}}"
  test: |
    res.code == 200 &&
    res.body.order != nil &&

    // 注文合計が品目の合計と等しい
    res.body.order.total == 
      sum(map(res.body.order.line_items, #.quantity * #.price)) &&

    // 税計算が正しい（8%税率を想定）
    res.body.order.tax_amount == 
      round(res.body.order.subtotal * 0.08 * 100) / 100 &&

    // 送料が正しく適用される
    (res.body.order.subtotal >= 100 ? 
      res.body.order.shipping_cost == 0 :     // $100以上は送料無料
      res.body.order.shipping_cost == 9.99    // 標準送料
    ) &&

    // 最終合計計算
    res.body.order.total == 
      res.body.order.subtotal + res.body.order.tax_amount + res.body.order.shipping_cost
```

## エラーテストと負のケース

成功する経路しか通らないワークフローでは、エラー処理が検証されないまま残ります。以下では不正な入力が期待どおりに拒否されることを検証します。

### 期待されるエラーシナリオ

ここでは失敗するレスポンスが期待される結果です。そのためエラーコードと、付随するメッセージを検証します。

```yaml
- name: Test Invalid Authentication
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/protected"
    headers:
      Authorization: "Bearer invalid-token"
  test: |
    res.code == 401 &&
    res.body.error == "invalid_token" &&
    res.body.message contains "authentication"

- name: Test Rate Limiting
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/rate-limited-endpoint"
  test: |
    res.code in [200, 429] &&  // 成功またはレート制限
    (res.code == 429 ? 
      "Retry-After" in res.headers && 
      res.body.error == "rate_limit_exceeded" :
      res.body.status == "success"
    )

- name: Test Malformed Request
  uses: http
  with:
    url: "{{vars.API_URL}}/users"
    method: POST
    body: '{"invalid": json}'  # 意図的に不正なフォーマット
  test: |
    res.code == 400 &&
    res.body.error contains "json" &&
    res.body.details != nil
```

### 境界値テスト

許容範囲の境目にある入力は検証処理が壊れやすい箇所です。そこで意図的にその値を送ります。

```yaml
- name: Test Input Boundaries
  uses: http
  with:
    url: "{{vars.API_URL}}/users"
    method: POST
    # nameは最大長、ageは上限境界、scoreは下限境界
    body: |
      {
        "name": "{{random_str(255)}}",
        "age": 150,
        "score": 0
      }
  test: |
    res.code in [201, 400] &&  // 作成成功またはバリデーションエラー
    (res.code == 400 ? 
      res.body.validation_errors != nil :
      res.body.user.id != nil
    )
```

## テスト整理パターン

ワークフローが大きくなると、どの検証をどこに置くかが問題になります。以下では検証を層に分け、スイートとしてまとめる方法を示します。

### レイヤードテスト戦略

検証をジョブに分ければ、軽いものを先に実行し、重いものが始まる前に打ち切れます。

```yaml
vars:
  API_URL: "{{API_URL}}"

jobs:
- id: smoke-tests
  name: Smoke Tests
  steps:
    - name: Basic Connectivity
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/ping"
      test: res.code == 200

- id: functional-tests
  name: Functional Tests
  needs: [smoke-tests]
  steps:
    - name: User Management
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users"
      test: |
        res.code == 200 &&
        res.body.users != nil &&
        res.body.pagination != nil

- id: integration-tests
  name: Integration Tests
  needs: [functional-tests]
  steps:
    - name: Cross-Service Integration
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/integration/full-flow"
      test: |
        res.code == 200 &&
        res.body.all_services_connected == true &&
        res.body.data_consistency_check == true
```

### 包括的テストスイート

スイート全体では、検証対象ごとにケースをまとめ、最初に取得した認証情報を以降のステップへ引き継ぎます。

```yaml
vars:
  API_URL: "{{API_URL}}"
  TEST_USERNAME: "{{TEST_USERNAME}}"
  TEST_PASSWORD: "{{TEST_PASSWORD}}"

jobs:
- id: api-test-suite
  name: Comprehensive API Test Suite
  steps:
    # 認証テスト
    - name: Valid Login
      id: login
      uses: http
      with:
        url: "{{vars.API_URL}}/auth/login"
        method: POST
        body: |
          {
            "username": "{{vars.TEST_USERNAME}}",
            "password": "{{vars.TEST_PASSWORD}}"
          }
      test: |
        res.code == 200 &&
        res.body.access_token != nil &&
        res.body.refresh_token != nil &&
        res.body.expires_in > 0
      outputs:
        access_token: res.body.access_token

    - name: Invalid Login
      uses: http
      with:
        url: "{{vars.API_URL}}/auth/login"
        method: POST
        body: |
          {
            "username": "invalid",
            "password": "wrong"
          }
      test: |
        res.code == 401 &&
        res.body.error == "invalid_credentials"

    # CRUD 操作テスト
    - name: Create User
      id: create-user
      uses: http
      with:
        url: "{{vars.API_URL}}/users"
        method: POST
        headers:
          Authorization: "Bearer {{outputs.login.access_token}}"
        body: |
          {
            "name": "Test User {{random_str(6)}}",
            "email": "test{{random_str(8)}}@example.com",
            "role": "user"
          }
      test: |
        res.code == 201 &&
        res.body.user.id != nil &&
        res.body.user.name != nil &&
        res.body.user.email != nil
      outputs:
        user_id: res.body.user.id
        user_email: res.body.user.email

    - name: Read User
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/{{outputs['create-user'].user_id}}"
        headers:
          Authorization: "Bearer {{outputs.login.access_token}}"
      test: |
        res.code == 200 &&
        res.body.user.id == outputs['create-user'].user_id &&
        res.body.user.email == outputs['create-user'].user_email

    - name: Update User
      uses: http
      with:
        url: "{{vars.API_URL}}/users/{{outputs['create-user'].user_id}}"
        method: PUT
        headers:
          Authorization: "Bearer {{outputs.login.access_token}}"
        body: |
          {
            "name": "Updated Test User"
          }
      test: |
        res.code == 200 &&
        res.body.user.name == "Updated Test User"

    - name: Delete User
      uses: http
      with:
        url: "{{vars.API_URL}}/users/{{outputs['create-user'].user_id}}"
        method: DELETE
        headers:
          Authorization: "Bearer {{outputs.login.access_token}}"
      test: res.code == 204

    - name: Verify Deletion
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/{{outputs['create-user'].user_id}}"
        headers:
          Authorization: "Bearer {{outputs.login.access_token}}"
      test: res.code == 404
```

## パフォーマンステスト

`res.time`により応答時間も通常の検証として書けます。正しさだけでなく、時間の上限もワークフローで守らせられます。

### レスポンス時間ベンチマーク

ベンチマークでは、正しさの検証と、許容する所要時間を組にして書きます。

```yaml
- name: Performance Benchmark Test
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/performance-test"
  test: |
    res.code == 200 &&

    // 段階的パフォーマンス期待値
    (vars.NODE_ENV == "production" ? 
      (rt.sec * 1000) < 500 :              // プロダクション: < 500ms
      (rt.sec * 1000) < 2000               // 非プロダクション: < 2s
    ) &&

    // 追加パフォーマンスメトリクス
    res.body.query_time < 100 &&    // データベースクエリ時間
    res.body.render_time < 50       // テンプレートレンダー時間
  outputs:
    response_time: (rt.sec * 1000)
    query_time: res.body.query_time
    render_time: res.body.render_time
```

### 負荷テスト検証

負荷を別のツールで生成する場合は、Probeでその結果を取得して検証します。

```yaml
- name: Load Test Results Validation
  uses: http
  with:
    method: GET
    url: "{{vars.LOAD_TEST_URL}}/results"
  test: |
    res.code == 200 &&
    res.body.test_completed == true &&

    // 成功率要件
    res.body.success_rate >= 0.95 &&

    // パフォーマンスパーセンタイル
    res.body.percentiles.p50 < 1000 &&
    res.body.percentiles.p95 < 2000 &&
    res.body.percentiles.p99 < 5000 &&

    // エラー率制限
    res.body.error_rate < 0.05 &&

    // 重大なエラーがない
    res.body.critical_errors == 0
```

## セキュリティテスト

どのAPIでも検証する価値があるものが2つあります。正しい認証情報のないリクエストを拒否すること、そして受け付けるべきでない入力を拒否することです。

### 認証と承認

認証情報のないリクエストは拒否される必要があります。テストではその拒否を検証します。

```yaml
- name: Test Unauthorized Access
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/admin/users"
  test: |
    res.code == 401 &&
    res.body.error == "authentication_required"

- name: Test Insufficient Permissions
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/admin/users"
    headers:
      Authorization: "Bearer {{vars.USER_TOKEN}}"  # 通常ユーザートークン
  test: |
    res.code == 403 &&
    res.body.error == "insufficient_permissions"

- name: Test Token Expiration
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/protected"
    headers:
      Authorization: "Bearer {{vars.EXPIRED_TOKEN}}"
  test: |
    res.code == 401 &&
    res.body.error == "token_expired"
```

### 入力検証セキュリティ

攻撃的な入力は拒否されるか無害化される必要があります。実行されない限り、どちらの結果でも構いません。

```yaml
- name: Test SQL Injection Protection
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/users?search='; DROP TABLE users; --"
  test: |
    res.code in [200, 400] &&  // フィルタされるか拒否される
    !(res.body contains "sql") && // SQL エラーメッセージなし
    !(res.body contains "syntax") &&
    res.body.error != "internal_server_error"  // サーバーエラーを起こさない

- name: Test XSS Protection
  uses: http
  with:
    url: "{{vars.API_URL}}/comments"
    method: POST
    body: |
      {
        "content": "<script>alert('xss')</script>"
      }
  test: |
    res.code in [201, 400] &&
    (res.code == 201 ? 
      !(res.body.comment.content contains "<script>") :  // サニタイズされるべき
      res.body.validation_errors != nil               // または拒否される
    )
```

## テストドキュメントとレポート

テストが失敗したとき、何を期待していたかが出力からわかって初めて役に立ちます。命名と結果の集約がその役割を担います。

### 自己文書化テスト

ステップ名と検証式を合わせて読めば、何を確認したケースかがわかる状態にします。そうすれば失敗時に別の説明が要りません。

```yaml
- name: User Registration Flow Test
  uses: http
  with:
    url: "{{vars.API_URL}}/auth/register"
    method: POST
    body: |
      {
        "email": "{{random_str(8)}}@example.com",
        "password": "TestPass123!",
        "confirm_password": "TestPass123!"
      }
  # 明確な検証ポイント付き包括的テスト
  test: |
    res.code == 201 &&                                    // 1. 作成成功
    res.body.user.id != nil &&                            // 2. ユーザーID割り当て
    res.body.user.email != nil &&                         // 3. メール保存
    res.body.user.password == nil &&                      // 4. パスワード未返却
    res.body.user.created_at != nil &&                    // 5. タイムスタンプ記録
    res.body.user.email_verified == false &&               // 6. 初期未認証メール
    res.body.verification_email_sent == true &&            // 7. 認証トリガー
    "Location" in res.headers &&                         // 8. Location ヘッダー存在
    res.headers["Location"] contains "/users/"             // 9. 正しいリダイレクトパス
  outputs:
    user_id: res.body.user.id
    user_email: res.body.user.email
  echo: |
    Registration test completed:
    - User ID: {{res.body.user.id}}
    - Email: {{res.body.user.email}}
    - Verification: {{res.body.verification_email_sent ? "Sent" : "Failed"}}
    - Response time: {{rt.duration}}
```

### テスト結果集約

他のジョブを`needs`に指定した最後のジョブを置けば、各ジョブのステップが公開した値を読んで1つの要約を出力できます。`always: true`を付けると、どれかが失敗しても実行されます。`?? false`は、接続できず何も公開しなかったステップに備えたものです。

```yaml
vars:
  API_URL: "{{API_URL}}"
  NODE_ENV: "{{NODE_ENV ?? 'development'}}"

jobs:
- id: smoke-tests
  name: Smoke Tests
  steps:
    - name: Basic Connectivity
      id: smoke
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/ping"
      test: res.code == 200
      outputs:
        smoke_passed: res.code == 200

- id: functional-tests
  name: Functional Tests
  steps:
    - name: User Management
      id: functional
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users"
      test: res.code == 200
      outputs:
        functional_passed: res.code == 200

- id: security-tests
  name: Security Tests
  steps:
    - name: Unauthorized Access
      id: security
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/admin/users"
      test: res.code == 401
      outputs:
        security_passed: res.code == 401

- id: test-summary
  name: Test Results Summary
  needs: [smoke-tests, functional-tests, security-tests]
  always: true
  steps:
    - name: Generate Test Report
      uses: hello
      echo: |
        Test Execution Summary
        ======================

        Smoke: {{(outputs.smoke?.smoke_passed ?? false) ? "passed" : "failed"}}
        Functional: {{(outputs.functional?.functional_passed ?? false) ? "passed" : "failed"}}
        Security: {{(outputs.security?.security_passed ?? false) ? "passed" : "failed"}}

        Finished At: {{unixtime()}}
        Test Environment: {{vars.NODE_ENV}}
```

## ベストプラクティス

以下では、テストが増えても読みやすさを保ち、失敗したときに原因がわかる状態を保つための指針を示します。

### 1. 明確なテスト意図

テストには期待する内容を書きます。値がnullでないことだけを確認する式は、そのケースと無関係な理由でも成功します。

```yaml
# 良い例: 具体的でテスト可能な条件
test: |
  res.code == 200 &&
  len(res.body.users) >= 1 &&
  res.body.users[0].id != nil

# 避ける: 曖昧または不完全なテスト
test: res.code == 200  # レスポンス内容はどうなのか？
```

### 2. 包括的エラーカバレッジ

検証する価値のあるケースには、必ず失敗する側の対があります。どちらもスイートに入れます。

```yaml
# 良い例: 成功と失敗の両方のパスをテスト
- name: Valid Request Test
  test: res.code == 200 && res.body.success == true

- name: Invalid Request Test  
  test: res.code == 400 && res.body.error != nil
```

### 3. パフォーマンスを意識したテスト

既存のテストに時間の上限を加えるコストはありません。それでいて、正しさの検証だけでは通ってしまう劣化を検出できます。

```yaml
# 良い例: パフォーマンス検証を含む
test: |
  res.code == 200 &&
  (rt.sec * 1000) < 1000 &&
  res.body.data != nil

# 良い例: 環境固有のパフォーマンス閾値
test: |
  res.code == 200 &&
  (rt.sec * 1000) < int(vars.MAX_RESPONSE_TIME ?? 2000)
```

### 4. 保守しやすいテスト式

長い条件は1行に1つの検証を書いて改行したほうが読めます。

```yaml
# 良い例: 読みやすく、よく構造化されたテスト
test: |
  res.code == 200 &&
  res.body.user != nil &&
  res.body.user.id > 0 &&
  res.body.user.email contains "@"

# 避ける: 複雑で読みにくいテスト
test: res.code == 200 && res.body.user != nil && res.body.user.id > 0 && res.body.user.email contains "@" && res.body.user.active == true && (rt.sec * 1000) < 1000
```

## 次のステップ

テストとアサーションを理解したら、以下を探索してください：

1. **[エラーハンドリング](/ja/guide/concepts/error-handling)** - 失敗を適切に処理する方法を学ぶ
2. **[実行モデル](/ja/guide/concepts/execution-model)** - ワークフロー実行フローを理解する
3. **[ハウツー](/ja/guide/how-tos/api-testing)** - 実用的なテストパターンの実例を見る

テストとアサーションは、ステップの成否を決める条件です。これらの概念をマスターして、問題がシステムに影響を与える前に検出する信頼性の高い堅牢な自動化を構築しましょう。
