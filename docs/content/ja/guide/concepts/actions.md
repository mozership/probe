# アクション

アクションはProbeの中で実際の作業を実行するコアとなる実行単位です。各アクションは独自のプロセスで動き、これがProbeを拡張可能でモジュラーなものにしています。このガイドではアクションシステム、組み込みアクション、Probeがアクションを実行する仕組みを詳しく説明します。

## アクションシステム概要

Probeのアクションシステムは、以下を提供します：

- **モジュラー性**: 各アクションは同じgRPCのインターフェースを持つ独立した単位です
- **拡張性**: カスタムアクションを簡単に追加できます
- **分離**: アクションは安定性のために別プロセスで実行されます
- **標準化**: すべてのアクションは同じインターフェースに従います

### アクション実行フロー

1. **解決**: ProbeはStepが指定したアクションを特定します。組み込みアクションか、最初のJobの開始前に解決した外部アクションです
2. **起動**: アクションが独自のプロセスで起動されます
3. **通信**: ProbeはgRPCを介してアクションと通信します
4. **実行**: アクションがStepの処理を実行します
5. **レスポンス**: 結果が処理のためにProbeに返されます
6. **クリーンアップ**: Stepの結果を受け取ると、アクションのプロセスを終了します

## 組み込みアクション

Probeには一般的な使用例をカバーする組み込みアクションがいくつか付属しています。

### HTTPアクション

`http`アクションは最も汎用性があり、HTTP/HTTPSリクエストを行うために最も一般的に使用されるアクションです。

#### 基本的な使用方法

GETリクエストに必要なのはURLだけです。

```yaml
- name: Simple GET Request
  uses: http
  with:
    url: https://api.example.com/users
    method: GET
  test: res.code == 200
```

#### 完全なHTTPアクション リファレンス

このアクションが受け取るフィールドを1つのステップにまとめた例です。

```yaml
- name: Comprehensive HTTP Request
  uses: http
  timeout: 30s                                  # オプション: リクエストタイムアウト
  with:
    url: https://api.example.com/users/123        # 必須: ターゲット URL
    method: POST                                  # オプション: HTTP メソッド (デフォルト: GET)
    headers:                                      # オプション: リクエストヘッダー
      Content-Type: "application/json"
      Authorization: "Bearer {{vars.api_token}}"
      X-Request-ID: "{{random_str(16)}}"
    body: |                                       # オプション: リクエストボディ
      {
        "name": "John Doe",
        "email": "john@example.com",
        "active": true
      }
  test: res.code == 200 && res.body.success == true
  outputs:
    user_id: res.body.user.id
    created_at: res.body.user.created_at
    response_time: (rt.sec * 1000)
```

#### HTTPレスポンス オブジェクト

HTTPアクションは豊富なレスポンスオブジェクトを提供します：

```yaml
# 利用可能なレスポンスプロパティ:
test: |
  res.code == 200 &&                    # HTTP ステータスコード
  (rt.sec * 1000) < 1000 &&                      # レスポンス時間（ミリ秒）
  res.body_size < 10000 &&               # レスポンスボディサイズ（バイト）
  res.headers["Content-Type"] == "application/json" &&  # レスポンスヘッダー
  res.body.success == true &&            # 解析された JSON ボディ（該当する場合）
  res.body contains "success"           # テキストとしてのレスポンスボディ
```

#### 一般的なHTTPパターン

**API認証:**
```yaml
jobs:
- id: api-test
  name: api-test
  steps:
    - name: Authenticate
      id: auth
      uses: http
      with:
        url: "{{vars.api_base_url}}/auth/login"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "username": "{{vars.api_username}}",
            "password": "{{vars.api_password}}"
          }
      test: res.code == 200
      outputs:
        access_token: res.body.access_token
        refresh_token: res.body.refresh_token

    - name: Make Authenticated Request
      uses: http
      with:
        url: "{{vars.api_base_url}}/protected/resource"
        method: GET
        headers:
          Authorization: "Bearer {{outputs.auth.access_token}}"
      test: res.code == 200
```

**ファイルアップロード:**
```yaml
- name: Upload File
  uses: http
  with:
    url: "{{vars.api_url}}/upload"
    method: POST
    headers:
      Content-Type: "multipart/form-data"
    body: |
      --boundary123
      Content-Disposition: form-data; name="file"; filename="test.txt"
      Content-Type: text/plain
      
      This is test file content
      --boundary123--
  test: res.code == 201
```

**GraphQLクエリ:**
```yaml
- name: GraphQL Query
  uses: http
  with:
    url: "{{vars.graphql_endpoint}}"
    method: POST
    headers:
      Content-Type: "application/json"
      Authorization: "Bearer {{vars.graphql_token}}"
    body: |
      {
        "query": "query GetUser($id: ID!) { user(id: $id) { name email active } }",
        "variables": { "id": "{{vars.test_user_id}}" }
      }
  test: res.code == 200 && res.body.data.user != null
  outputs:
    user_name: res.body.data.user.name
    user_email: res.body.data.user.email
```

### Shellアクション

`shell`アクションはワークフロー内でシェルコマンドとスクリプトの安全な実行を可能にします。包括的な出力キャプチャ、タイムアウト保護、環境変数サポートを提供します。

#### 基本的な使用方法

シェルのステップに必要なのはコマンドだけです。

```yaml
- name: Build Application
  uses: shell
  with:
    cmd: "npm run build"
    workdir: "/app"
    timeout: "5m"
  test: res.code == 0
```

#### 完全なShellアクション リファレンス

このアクションが受け取るフィールドを1つのステップにまとめた例です。

```yaml
- name: Deploy Application
  uses: shell
  with:
    cmd: "./deploy.sh production"              # 必須: 実行するコマンド
    shell: "/bin/bash"                        # オプション: 使用するシェル (デフォルト: /bin/sh)
    workdir: "/deploy"                        # オプション: 作業ディレクトリ（絶対パス）
    timeout: "15m"                           # オプション: 実行タイムアウト（デフォルト: 30s）
    env:                                     # オプション: 環境変数
      DEPLOY_ENV: "production"
      API_KEY: "{{vars.production_api_key}}"
      BUILD_VERSION: "{{vars.version}}"
  test: res.code == 0 && res.stdout contains "Deploy successful"
  outputs:
    deploy_time: res.rt
    deploy_log: res.stdout
```

#### Shellレスポンス オブジェクト

終了コードと、標準出力および標準エラー出力をテストから参照できます。

```yaml
# 利用可能なレスポンスプロパティ:
test: |
  res.code == 0 &&                         # 終了コード（0 = 成功）
  res.stdout contains "success" &&        # 標準出力
  res.stderr == "" &&                      # 標準エラー（空 = エラーなし）
  req.cmd == "npm run build" &&           # 元のコマンド
  req.shell == "/bin/bash"                # 実行に使用されたシェル
```

#### 一般的なShellパターン

**ビルドとテストのパイプライン:**
```yaml
jobs:
- id: build-and-test
  name: build-and-test
  steps:
    - name: Install Dependencies
      uses: shell
      with:
        cmd: "npm ci"
        workdir: "/app"
        timeout: "5m"
      test: res.code == 0

    - name: Run Tests
      uses: shell
      with:
        cmd: "npm test"
        workdir: "/app"
        env:
          NODE_ENV: "test"
          CI: "true"
      test: res.code == 0

    - name: Build Application
      uses: shell
      with:
        cmd: "npm run build"
        workdir: "/app"
        env:
          NODE_ENV: "production"
      test: res.code == 0
```

**システムヘルス監視:**
```yaml
- name: Check System Health
  uses: shell
  with:
    cmd: |
      echo "=== System Health Report ===" &&
      echo "CPU Usage: $(top -bn1 | grep Cpu | cut -d' ' -f2)" &&
      echo "Memory: $(free -h | grep Mem)" &&
      echo "Disk: $(df -h /)"
  test: res.code == 0
  outputs:
    health_report: res.stdout
```

#### セキュリティ機能

shellアクションは複数のセキュリティレイヤーを実装します：

- **シェル制限**: 承認されたシェル実行ファイルのみを許可
- **パス検証**: 作業ディレクトリは絶対パスである必要があります
- **タイムアウト保護**: 暴走プロセスを防止
- **環境分離**: 安全な環境変数ハンドリング
- **出力サニタイズ**: コマンド出力の安全なキャプチャ

### Helloアクション

`hello`アクションは主にテストとデモンストレーションに使用されます。Probeがアクションを起動して呼び出せることを確かめるシンプルな方法を提供します。

```yaml
- name: Test Hello Action
  id: hello
  uses: hello
  with:
    message: "Test message"           # res にそのまま返る
  echo: "{{res.message}}"
  outputs:
    greeting: res.message
```

**Helloアクション レスポンス:** このアクション固有のパラメータはありません。`with`に渡したキーはそのまま`res`に返り、`status`は常に`0`です。

```yaml
test: status == 0 && res.message != null
```

### SMTPアクション

`smtp`アクションは通知とアラートのメール送信機能を有効にします。

```yaml
- name: Send Email Notification
  uses: smtp
  with:
    addr: "smtp.gmail.com              # SMTP サーバーホスト:587                         # SMTP サーバーポート"
    from: alerts@mycompany.com        # 送信者メールアドレス
    to: ["admin@mycompany.com", "team@mycompany.com"]  # 受信者
    subject: "System Alert: {{vars.alert_type}}"       # メール件名
    session: 1
    message: 1
    length: 500
  echo: |                           # メール本文（プレーンテキストまたは HTML）
  test: res.code == "sent"
  outputs:
    message_id: res.message_id
    recipients_count: res.recipients_count
```

**SMTP設定例:**

**Gmail:**
```yaml
with:
  host: smtp.gmail.com
  port: 587
  username: "your-email@gmail.com"
  password: "your-app-password"
  tls: true
```

**AWS SES:**
```yaml
with:
  host: email-smtp.us-east-1.amazonaws.com
  port: 587
  username: "{{vars.aws_ses_username}}"
  password: "{{vars.aws_ses_password}}"
  tls: true
```

**Office 365:**
```yaml
with:
  host: smtp.office365.com
  port: 587
  username: "your-email@company.com"
  password: "{{vars.o365_password}}"
  tls: true
```

## リトライ機能

Probeは全てのアクションで利用可能な統一されたリトライ機能を提供します。この機能により、一時的な障害や起動待機時間が必要なサービスに対してアクションを自動的に再実行できます。

### 基本的なリトライ構文

リトライはステップレベルで設定し、任意のアクション（`http`、`shell`、`db`など）と組み合わせて使用できます：

```yaml
- name: "Service Health Check"
  uses: http
  with:
    method: GET
    url: "http://localhost:8080/health"
  retry:
    max_attempts: 10      # 最大試行回数
    interval: "2s"        # リトライ間隔
    initial_delay: "5s"   # 初回実行前の待機時間（オプション）
  test: res.code == 200
```

### リトライパラメータ

リトライの挙動は3つのパラメータで決まります。試行回数の上限、試行の間隔、そして初回実行前の待機時間です。

#### `max_attempts` (必須)
- **型:** Integer
- **範囲:** 1-10000 (上限は環境変数`PROBE_MAX_ATTEMPTS`で変更可能)
- **説明:** リトライする最大試行回数

#### `interval` (オプション)
- **型:** StringまたはDuration
- **デフォルト:** `0s` (待たずに次を試行)
- **形式:** Go duration形式 (`500ms`, `2s`, `1m`) または数値 (秒)
- **説明:** 各リトライ試行の間隔

#### `initial_delay` (オプション)
- **型:** StringまたはDuration
- **デフォルト:** `0s` (遅延なし)
- **形式:** Go duration形式 (`500ms`, `2s`, `1m`) または数値 (秒)
- **説明:** 最初の試行前の待機時間

### 成功条件

リトライが成功したかどうかは、ステップの`test`で判定します。`test`が真になった時点で成功として結果を返し、偽のままなら次の試行に進みます。`test`のないステップはリトライせず、1回だけ実行します。

### 実行フロー

1. `initial_delay`が指定されている場合、その時間だけ待機
2. アクションを実行
3. `test`が真なら、成功として結果を返す
4. アクションがエラーを返したか`test`が偽で、まだ試行回数に余裕がある場合：
   - `interval`の時間だけ待機
   - ステップ2に戻る
5. 最大試行回数に達した場合、最後の実行結果を返す

### アクション別の使用例

待つ対象はアクションによって変わります。以下ではHTTP、Shell、DBのそれぞれで、起動や接続の確立を待つ書き方を示します。

#### HTTPリトライ - API起動待機

起動直後のAPIは接続を受け付けないことがあります。応答するまでリクエストを繰り返します。

```yaml
- name: "Wait for API Server"
  uses: http
  timeout: "5s"
  with:
    url: "{{vars.api_base_url}}/health"
    method: GET
  retry:
    max_attempts: 30
    interval: "2s"
    initial_delay: "10s"
  test: res.code == 200 && res.body.status == "healthy"
```

#### Shellリトライ - サービス起動監視

コマンドの終了コードを条件にして、サービスが立ち上がるまで待ちます。

```yaml
- name: "Wait for Database"
  uses: shell
  with:
    cmd: "pg_isready -h postgres -p 5432 -U app"
  retry:
    max_attempts: 60
    interval: "1s"
    initial_delay: "5s"
  test: res.code == 0
```

#### DBリトライ - 接続確立

データベースは接続を受け付けるまでに時間がかかります。接続できるまでクエリを繰り返します。

```yaml
- name: "Database Connection Test"
  uses: db
  with:
    dsn: "postgres://user:pass@localhost:5432/testdb"
    query: "SELECT 1"
  retry:
    max_attempts: 20
    interval: "3s"
  test: res.code == 0
```

### 高度なリトライパターン

リトライのパラメータにも式を書けます。これにより、環境に応じた待ち時間や、条件を満たしたときだけのリトライを表現できます。

#### 段階的な遅延

依存の順に待つジョブを並べれば、起動の順序に沿ったリトライになります。

```yaml
jobs:
- id: staged-startup
  name: staged-startup
  steps:
    - name: "Quick Health Check"
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/ping"
      retry:
        max_attempts: 5
        interval: "100ms"
      test: res.code == 200

    - name: "Detailed Health Check"
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/health"
      retry:
        max_attempts: 30
        interval: "2s"
        initial_delay: "1s"
      test: res.code == 200 && res.body.database_connected == true
```

#### 条件付きリトライ

リトライのパラメータに式を書けば、環境に応じて待ち方を変えられます。

```yaml
- name: "Environment-Aware Health Check"
  uses: http
  with:
    method: GET
    url: "{{vars.service_url}}/health"
  retry:
    max_attempts: "{{vars.environment == 'production' ? 60 : 10}}"
    interval: "{{vars.environment == 'production' ? '5s' : '1s'}}"
  test: res.code == 200
```

### ベストプラクティス

リトライを設定するときは、1回あたりのタイムアウト、試行回数の上限、初期遅延の3つを対象の性質に合わせます。

#### 1. 適切なタイムアウト設定

1回あたりのタイムアウトは、リトライ間隔より短くします。

```yaml
# 良い例: リトライ間隔より短いタイムアウト
- name: "Quick API Check"
  uses: http
  timeout: "2s"        # 短いタイムアウト
  with:
    method: GET
    url: "{{vars.api_url}}/ping"
  retry:
    max_attempts: 10
    interval: "3s"       # タイムアウトより長い間隔
```

#### 2. 実用的な最大試行回数

試行回数は、待つ対象が実際に立ち上がるまでの時間から決めます。

```yaml
# 良い例: 合理的な試行回数
- name: "Service Startup"
  uses: shell
  with:
    cmd: "service myapp status"
  retry:
    max_attempts: 30     # 30回 × 2秒 = 最大1分待機
    interval: "2s"
```

#### 3. 初期遅延の活用

起動に時間がかかるとわかっている対象には、最初の試行自体を遅らせます。

```yaml
# 良い例: サービス起動時間を考慮した初期遅延
- name: "Database Health Check"
  uses: db
  with:
    dsn: "{{vars.db_dsn}}"
    query: "SELECT 1"
  retry:
    max_attempts: 20
    interval: "3s"
    initial_delay: "15s"  # データベース起動に時間がかかる場合
```

## 高度なアクション使用方法

アクションは単独で呼ぶだけでなく、組み合わせて使います。失敗の扱い、複数のアクションの連結、実行中に求めた値からの設定を扱います。

### アクションでのエラーハンドリング

アクション失敗に対する堅牢なエラーハンドリングを実装します：

```yaml
jobs:
- id: resilient-http-check
  name: resilient-http-check
  steps:
    - name: Primary Endpoint Check
      id: primary
      uses: http
      timeout: 10s
      with:
        url: "{{vars.primary_url}}/health"
        method: GET
      test: res.code == 200
      outputs:
        primary_healthy: res.code == 200
        primary_response_time: (rt.sec * 1000)

    - name: Secondary Endpoint Check
      id: secondary
      uses: http
      timeout: 15s
      with:
        url: "{{vars.secondary_url}}/health"
        method: GET
      test: res.code == 200
      outputs:
        secondary_healthy: res.code == 200
        secondary_response_time: (rt.sec * 1000)

    - name: Alert on Total Failure
      uses: smtp
      with:
        addr: "{{vars.smtp_host}}:587"
        from: "alerts@company.com"
        to: "ops-team@company.com"
        subject: "CRITICAL: All endpoints down"
        session: 1
        message: 1
        length: 500
      echo: |
        CRITICAL ALERT: All monitored endpoints are down
          
        Primary Endpoint: FAILED
        Secondary Endpoint: FAILED
          
        Time: {{unixtime()}}
          
        Immediate investigation required!
```

### アクション構成パターン

アクションを組み合わせて複雑なワークフローを作成します：

```yaml
jobs:
- id: comprehensive-api-test
  name: Comprehensive API Testing
  steps:
    # 1. ヘルスチェック
    - name: Verify API Health
      id: health
      uses: http
      with:
        method: GET
        url: "{{vars.api_url}}/health"
      test: res.code == 200
      outputs:
        api_version: res.body.version
        database_connected: res.body.database.connected

    # 2. 認証テスト
    - name: Test Authentication
      id: auth
      uses: http
      with:
        url: "{{vars.api_url}}/auth/token"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "client_id": "{{vars.client_id}}",
            "client_secret": "{{vars.client_secret}}",
            "grant_type": "client_credentials"
          }
      test: res.code == 200
      outputs:
        access_token: res.body.access_token
        token_expires: res.body.expires_in

    # 3. 機能テスト
    - name: Test Core Functionality
      id: functional
      uses: http
      with:
        url: "{{vars.api_url}}/api/test"
        method: GET
        headers:
          Authorization: "Bearer {{outputs.auth.access_token}}"
      test: res.code == 200 && res.body.test_passed == true
      outputs:
        test_duration: (rt.sec * 1000)
        test_results: res.body.results

    # 4. パフォーマンス検証
    - name: Validate Performance
      uses: smtp
      with:
        addr: "{{vars.smtp_host}}:587"
        from: "performance@company.com"
        to: "dev-team@company.com"
        subject: "Performance Alert: Slow API Response"
        session: 1
        message: 1
        length: 500
      echo: |
        Performance Alert
          
        API Version: {{outputs.health.api_version}}
        Response Time: {{outputs.functional.test_duration}}ms
        Expected: < 2000ms
          
        Please investigate performance degradation.

        功通知
    - name: Success Report
      uses: hello
      echo: |
        ✅ API Test Suite Completed Successfully
          
        Health Check: ✅ (v{{outputs.health.api_version}})
        Authentication: ✅ (expires in {{outputs.auth.token_expires}}s)
        Functionality: ✅ ({{outputs.functional.test_duration}}ms)
        Performance: ✅ (within acceptable limits)
```

### 動的アクション設定

実行時の条件に基づいてアクションを動的に設定します：

```yaml
jobs:
- id: adaptive-monitoring
  name: adaptive-monitoring
  steps:
    - name: Determine Environment
      id: env
      uses: http
      with:
        method: GET
        url: "{{vars.config_service_url}}/environment"
      test: res.code == 200
      outputs:
        environment: res.body.environment
        notification_level: res.body.notifications.level
        smtp_config: res.body.smtp

    - name: Environment-Specific Health Check
      id: health
      uses: http
      timeout: "{{outputs.vars.environment == 'production' ? '5s' : '30s'}}"
      with:
        method: GET
        url: "{{vars.service_url}}/health"
      test: res.code == 200
      outputs:
        service_status: res.body.status
        error_count: res.body.errors

    - name: Conditional Alert
      uses: smtp
      with:
        addr: "{{outputs.vars.smtp_config.host}}:{{outputs.vars.smtp_config.port}}"
        from: "monitoring@company.com"
        to: "{{outputs.vars.environment == 'production' ? ['ops@company.com', 'management@company.com'] : ['dev@company.com']}}"
        subject: "{{outputs.vars.environment == 'production' ? 'PRODUCTION' : 'NON-PROD'}} Alert: Service Errors Detected"
        session: 1
        message: 1
        length: 500
      echo: |
        Service Error Alert
          
        Environment: {{outputs.vars.environment}}
        Service Status: {{outputs.health.service_status}}
        Error Count: {{outputs.health.error_count}}
          
        {{outputs.vars.environment == "production" ? "IMMEDIATE ACTION REQUIRED" : "Please investigate when convenient"}}
```

## アクションアーキテクチャ詳細

アクションはすべて独自のプロセスで動きます。以下ではProbeとアクションの通信方法、アクションの開始と終了、組み込みアクションの実行方法を扱います。

### アクションとの通信

Probeはアクションとの通信にgRPCを使用し、以下を提供します：

- **型安全性**: Protocol Buffersによる強い型付け
- **パフォーマンス**: 効率的なバイナリシリアライゼーション
- **クロスランゲージ**: gRPCをサポートする任意の言語でアクションを作成可能
- **信頼性**: 組み込みのエラーハンドリングとタイムアウト

### アクションのライフサイクル

1. **解決**: Probeは最初のJobの開始前に、すべての外部アクションの`action.yml`を読み、実行のガードが許可するアクションの実行ファイルを取得します。ガードが許可しないアクションのStepは、実行ファイルを取得せずに拒否されます。組み込みアクションは解決不要です
2. **Stepごとの起動**: アクションはStepが使うときに起動されます
3. **プロセス分離**: 各アクションは独自のプロセスで実行されます
4. **Stepごとの停止**: Stepの結果を受け取るとすぐにプロセスを終了するので、Stepの間で動き続けるアクションはありません
5. **後始末**: ワークフローが終わると、Probeは後始末を求めた外部アクションをもう一度だけ起動し、Stepが残したものを消させます。[実行の後始末](#実行の後始末)を参照してください
6. **エラー分離**: アクションが失敗したり終了したりしても、失敗するのはそのStepで、Probeは止まりません

### 組み込みアクションの管理

組み込みアクションはProbeのバイナリに含まれています。Probeは自身の実行ファイルを`<executable> builtin-actions <name>`として起動し直すことで、各アクションを実行します：

```bash
probe workflow.yml  # Stepが使う組み込みアクションを起動します

# 組み込みアクションに別途インストールは不要:
# http, db, shell, ssh, grpc, smtp, imap,
# dns, embedded, hello
```

## アクションのベストプラクティス

どのアクションを使う場合でも、同じ点を決める必要があります。どれだけ待つか、失敗時にどうするか、認証情報をどこから得るか、何を検証するか、次のステップに何を渡すかです。

### 1. タイムアウト設定

常に適切なタイムアウトを設定します：

```yaml
# 良い例: 期待されるレスポンス時間に基づく具体的なタイムアウト
- name: Quick Health Check
  uses: http
  timeout: 5s              # クイックping は高速でレスポンスすべき
  with:
    method: GET
    url: "{{vars.api_url}}/ping"

- name: Complex Query
  uses: http
  timeout: 60s             # 複雑な操作にはより多くの時間が必要
  with:
    method: GET
    url: "{{vars.api_url}}/complex-report"
```

### 2. エラーハンドリング戦略

適切なエラーハンドリングを実装します：

```yaml
# 重要なアクション - 高速失敗
- name: Database Connectivity Check
  uses: http
  with:
    method: GET
    url: "{{vars.db_url}}/ping"
  test: res.code == 200

# 非重要なアクション - エラーでも継続
- name: Optional Analytics Update
  uses: http
  with:
    method: GET
    url: "{{vars.analytics_url}}/update"
  test: res.code == 200
```

### 3. 安全な設定

機密データを適切に扱います：

```yaml
# 良い例: シークレットに環境変数を使用
- name: Authenticated Request
  uses: http
  with:
    method: GET
    url: "{{vars.api_url}}/secure"
    headers:
      Authorization: "Bearer {{vars.api_token}}"  # vars から

# 良い例: 安全な SMTP 設定を使用
- name: Send Alert
  uses: smtp
  with:
    addr: "{{vars.smtp_host}}:25"
    from: "probe@example.com"
    to: "ops@example.com"
    session: 1
    message: 1
    length: 500
- name: Bad Example
  uses: http
  with:
    headers:
      Authorization: "Bearer secret-token-123"  # これは決してしてはいけません！
```

### 4. レスポンス検証

アクションレスポンスを徹底的に検証します：

```yaml
- name: Comprehensive API Test
  uses: http
  with:
    method: GET
    url: "{{vars.api_url}}/users"
  test: |
    res.code == 200 &&
    res.headers["Content-Type"] contains "application/json" &&
    res.body.users != null &&
    len(res.body.users) > 0 &&
    (rt.sec * 1000) < 1000
  outputs:
    user_count: len(res.body.users)
    response_time: (rt.sec * 1000)
```

### 5. 意味のある出力

他のステップのために有用な出力を定義します：

```yaml
- name: User Creation Test
  uses: http
  with:
    url: "{{vars.api_url}}/users"
    method: POST
    body: '{"name": "Test User", "email": "test@example.com"}'
  test: res.code == 201
  outputs:
    created_user_id: res.body.user.id
    created_user_email: res.body.user.email
    creation_timestamp: res.body.user.created_at
    response_time: (rt.sec * 1000)
```

## カスタムアクション（上級）

Probeには強力な組み込みアクションが付属していますが、特殊なニーズのためにカスタムアクションで拡張できます。

### カスタムアクション インターフェース

カスタムアクションは`actionrpc.Action`インターフェースを実装します：

```go
type Action interface {
    Run(with map[string]any) (map[string]any, error)
}
```

`with`にはステップの`with`パラメータが渡されます。返したマップは、組み込みアクションと同じくステップの`req`、`res`、`rt`、`status`になります。結果に含めるマップのキーは、文字列、数値、真偽値のいずれかにします。キーは文字列として送られるので、`map[int]string{404: "not found"}`は`{"404": "not found"}`として届きます。それ以外の型のキーや、`1`と`"1"`のように文字列にすると同じになるキーがあると、値を落とさずにステップをアクションエラーで失敗させます。

#### ステップの情報を受け取り、状態を保持する

httpアクションがトレースヘッダーやCookieのためにそうするように、実行するステップの情報を使うアクションや、ステップをまたいで何かを保持するアクションは、`actionrpc.StepAction`も実装します：

```go
type StepAction interface {
    Action
    RunStep(call Call) (result, newState map[string]any, err error)
}

type Call struct {
    With    map[string]any    // ステップのwithパラメータ
    State   map[string]any    // アクションがジョブに残した状態
    Step    Step              // 実行するステップ
    Guard   Guard             // 実行のガードがアクションに許可する操作
    Actions map[string]string // ワークフローのactionsで外部アクションに付けた名前
    Cleanups *Cleanups        // 実行の終わりにProbeが後始末をする場合はnilでない
    Processes *Processes      // アクションがバックグラウンドで動かし続けるプロセス
    Contract *Contract        // OpenAPIドキュメントなどの契約に対するステップの検証
}

type Step struct {
    RunID   string // probeの実行。すべてのステップで同じ
    JobID   string
    JobName string
    Index   int    // ジョブの中でのステップの位置（0から）
    ID      string
    Name    string
    Repeat  int    // 繰り返すジョブの何回目か（0から）
    Attempt int    // リトライするステップの何回目の試行か（1から）
}
```

Probeは、こうしたアクションに実行するステップの情報を常に渡します。それをどう使うかはアクション次第です。`RunID`は、1回の実行のすべてのステップで同じで、embeddedアクションで実行するジョブでも同じです。

`Guard`は、`--read-only`、`--allow-host`、`--allow-action`で指定する実行のガードです。アクションは、許可されない操作に対して`actionrpc.Refuse(...)`を返すことでガードに従います。そのステップは種類`refused`で失敗します。何が許可されているかは`Guard.ReadOnly`、`Guard.AllowsHost`、`Guard.CheckHost`で分かります。外部アクションは、従うガードの種類を`action.yml`の`guard`で宣言します。宣言していない種類のガードの下では、`--allow-action`で指定しない限り拒否されます。詳しくは[ガード](/ja/guide/concepts/guard)を参照してください。

`Actions`には、ワークフローの[`actions`](/ja/reference/yaml-configuration#actions)で外部アクションに付けた名前と、それが指すアクションの完全な形が入ります。ローカルのアクションは絶対パスです。ワークフロー自身の中の名前はProbeが置き換え済みなので、アクションがこれを使うのは、embeddedアクションがジョブファイルを実行するときのように、自分でステップを実行する場合だけです。

`State`には、そのアクションがジョブに残した状態が入ります。残していなければnilです。Probeは`newState`を読まずに保持し、同じジョブでそのアクションを使う次のステップに渡します。状態は結果と同じく、文字列をキーとするマップ、リスト、単純な値で表します。そう表せない状態は、ステップをアクションエラーで失敗させます。`newState`がnilの場合は、状態をそのまま保ちます。ステップがアクションエラーで失敗した場合やタイムアウトした場合も同様です。状態はジョブごと、アクションごとに分けて保持し、繰り返すジョブの各回と、embeddedアクションで実行するジョブは、状態を持たない状態から始まります。状態は出力に表示しないので、認証情報を入れても構いません。

#### 実行の後始末

アクションは、ステップが終わるとすぐに止められるプロセスで動きます。そのため、ステップが後続のステップのために残したもの（起動したコンテナなど）は、アクションより長く残ります。何かを残すアクションは、`action.yml`に`cleanup: true`を書き、`actionrpc.Cleaner`も実装します：

```go
type Cleaner interface {
    Action
    Cleanup(call CleanupCall) error
}

type CleanupCall struct {
    RunID string // 終わった実行。Step.RunIDと同じ値
}
```

Probeは、ワークフローが終わると`Cleanup`を呼びます。成功しても失敗しても、Ctrl+C、`SIGTERM`、`SIGHUP`で終わっても呼びます：

- **アクションごとに1回です。** 何個のステップやジョブが使っても、どの名前で使っても1回です。どのステップも実行しなかったアクションは呼びません。
- **渡すのは実行だけです。** アクションは`RunID`で消すものを探すので、ステップは作ったものに`RunID`を付けておきます。コンテナならラベルです。同時に動く2つの実行はIDが異なるので、互いのものは消しません。
- **ステップがバックグラウンドで動かし続けたプロセスを止めた後に呼びます。** 最初に使われた順の逆に呼ぶので、後のステップが始めたものが、それが依存しているかもしれないものより先に消えます。
- **後始末の時間は1つのアクションにつき30秒までです。** 失敗した後始末や時間切れの後始末は警告として伝え、実行の結果も終了ステータスも変えません。

`Cleanup`は、すでに無いものに対して失敗してはいけません。ステップが何も残さなかった実行に対して呼ばれることも、同じ実行に対して複数回呼ばれることもあります。シグナルで実行が終わるときは、そのアクションのステップがまだ動いている間に呼ばれることもあります。

embeddedアクションで実行するジョブのステップが残したものは、そのジョブを埋め込んだワークフローが終わるまで残ります。ワークフロー自身のジョブと同じです。

`Call.Cleanups`は、後始末をするProbeの下ではnilではありません。そのProbeは、`action.yml`に`cleanup`を書いたアクションの`Cleanup`を呼びます。後始末の仕組みができる前のProbeの下ではnilです。残したものが消されないままになるアクションは、そのときエラーを返せます。embeddedアクションのように自分でステップを実行するアクションは、そのステップが実行した外部アクションを`Call.Cleanups.Add`で伝え、Probeに呼ばせます。

shellアクションの`background: true`のように、バックグラウンドでプロセスを動かし続けるアクションは、アクションより長く残るように、そのプロセスを独立したセッションで起動します。そして、`Call.Processes.Add(actionrpc.Process{PID: pid, Log: logFile})`でProbeに伝えます。その後でステップが失敗する場合も伝えます。Probeは、ワークフローが終わったとき、またはCtrl+C、`SIGTERM`、`SIGHUP`で終わったときに、そのプロセスをリーダーとするプロセスグループを止め、ログファイルがあれば削除します。この仕組みができる前のProbeには伝わらないので、プロセスは動いたまま残ります。

httpアクションの`openapi`のように、ステップを契約に照らして検証するアクションは、`Call.Contract.Report(actionrpc.ContractCheck{Matched: matched, Violations: violations})`でその結果をProbeに伝えます。契約がリクエストとレスポンスをすべて許可していれば、`Violations`は空です。Probeはこの結果をステップのテストとして扱います。違反があれば、`test`が成り立っていても、ステップは`contract_request`または`contract_response`として失敗します。`test`のないステップは、契約だけで検証されます。`Matched`は契約がステップに対応づけたもので、`probe coverage`が数えます。対応するものがなければnilです。この仕組みができる前のProbeには伝わらないので、ステップは検証されていないものとして扱われます。`probe check`では外部アクションのどのキーが契約による検証を行うのかを判別できないので、そのようなステップに`test`がなければ、何も検証していないと警告します。

`SIGKILL`のように即座に終了させられたProbeは、何も呼びません。アクションは、後の実行のステップが始まるときに以前の実行が残したものを消すことで、これを補えます。

[container](/ja/reference/actions/container)アクションは、この仕組みで書かれています。各コンテナに`Step.RunID`のラベルを付け、`Cleanup`でそのラベルのコンテナを削除します。

### 外部アクション

アクションはProbeの外、独立したリポジトリに置くこともできます。Probeはアクションを提供する実行ファイルをダウンロードし、組み込みアクションと同じように実行します。ステップではリポジトリとコミットでアクションを指定します：

```yaml
- name: Ask the API who I am
  uses: github.com/mozership/probe-graphql@<40文字のコミットSHA>
  with:
    url: https://api.example.com/graphql
    query: '{ viewer { login } }'
  test: res.code == 200
```

`uses`で外部アクションを指定する形式は次の2つです。スラッシュを含まない名前は組み込みアクションです。

| 形式 | 例 |
|---|---|
| `github.com/<owner>/<repo>[/<dir>]@<commit>` | `github.com/mozership/probe-graphql@3f2a…` |
| `./`、`../`、`/`で始まるパス | `./actions/greet` |

リモートのアクションは40文字のコミットSHAで固定する必要があります。タグやブランチは、ワークフローをレビューした後で別のコードを指すように動かせるため受け付けません。現在対応しているのはGitHubだけです。ローカルのパスはワークフローファイルからの相対パスです。[embedded](/ja/reference/actions/embedded)アクションで実行するジョブの中では、そのジョブファイルからの相対パスです。

Probeは最初のジョブを始める前にすべての外部アクションを解決します。解決できない参照があれば終了コード2で実行を止め、ダウンロードの時間はステップのタイムアウトに含まれません。実行ファイルはユーザーのキャッシュディレクトリ（Linuxでは`~/.cache`、macOSでは`~/Library/Caches`）の`probe/actions`に置かれるので、ダウンロードは1回で済みます。一方、`action.yml`は実行のたびにGitHubから読みます。実行ファイルを照合するダイジェストを持つファイルなので、ディスク上で書き換えられたかもしれない写しは使いません。

#### 外部アクションに名前を付ける

外部アクションを複数のステップで使うワークフローでは、トップレベルの`actions`で一度だけ名前を付け、`uses`と`defaults`にその名前を書けます：

```yaml
actions:
  graphql: github.com/mozership/probe-graphql@<40文字のコミットSHA> # v0.2.0

jobs:
- name: Who am I
  defaults:
    graphql:
      url: https://api.example.com/graphql
  steps:
  - name: Ask the API who I am
    uses: graphql
    with:
      query: '{ viewer { login } }'
    test: res.code == 200
```

コミットを書く場所が1か所になり、新しいリリースへの更新はその1行の変更で済みます。組み込みアクションの名前は付けられません。`--allow-action`には、名前ではなく完全な形のアクションを指定します。規則は[`actions`](/ja/reference/yaml-configuration#actions)を参照してください。

#### 公開されているアクション

Probeとあわせて公開している外部アクションです。それぞれ独立したリポジトリにあり、パラメータと結果は各ページで説明しています。各リリースのノートの先頭に、コピーして使う`uses`の行があります。

| アクション | 内容 | 最新 |
|---|---|---|
| [agent](/ja/reference/actions/agent) | 自然言語で書いたタスク、またはペルソナと目標からWebアプリケーションをテストします。モデルが実際のChromeで操作し、別に呼び出したモデルがページに表示された内容から判定します。Anthropic、OpenAI、xAI、Gemini、さくらのAI Engineのモデルで動きます | v0.2.0 |
| [browser](/ja/reference/actions/browser) | chromedpを通して実際のChromeを操作します。ページを開き、内容を読み、入力し、スクリーンショットを撮ります。Probe v1.21.0までは組み込みアクションでした | v0.2.0 |
| [container](/ja/reference/actions/container) | 後続のステップのためにコンテナを起動します。空いているポートで公開し、準備ができるまで待つこともできます。実行が終わると、その実行のコンテナを削除します。`docker`のほか、Podmanなどのランタイムでも動きます | v0.1.0 |
| [graphql](/ja/reference/actions/graphql) | GraphQLのクエリをHTTPで送り、レスポンスの`data`と`errors`を分けて返します | v0.2.0 |
| [jmap](/ja/reference/actions/jmap) | [JMAP](https://jmap.io/)のメソッドを呼びます。セッションを取得し、各呼び出しのアカウントを補い、HTTP 200で返るメソッドのエラーを`res.errors`にまとめます | v0.2.0 |
| [mail-latency](/ja/reference/actions/mail-latency) | Maildirのメッセージを読み、`Received`ヘッダーから各メッセージの配送遅延を計算してCSVに書きます。Probe v1.21.0までは組み込みアクションでした | v0.1.0 |
| [redis](/ja/reference/actions/redis) | RedisまたはValkeyのサーバーでコマンドを実行し、その応答を返します。サーバーが返したエラーは`res.errors`に集めます。`--read-only`の下では、読み取りだけと分かっているコマンドだけを実行します | v0.1.0 |
| [s3](/ja/reference/actions/s3) | S3と、MinIOやCloudflare R2のようなS3互換ストレージのオブジェクトを読み、一覧し、書き、削除します。リクエストの署名はアクション自身が行います | v0.1.0 |
| [websocket](/ja/reference/actions/websocket) | WebSocketのサーバーに接続し、メッセージを順に送受信して、受け取ったものを返します。受信は次の1通、指定した数、サーバーが閉じるまでのすべての中から選べ、一致するものだけに絞ることもできます | v0.1.0 |

#### action.yml

アクションのディレクトリには、どの実行ファイルがアクションを提供するかを書いた`action.yml`を置きます：

```yaml
name: graphql
description: Send a GraphQL query over HTTP
guard: [read-only, allow-host]
params: [url, query, variables, operation_name, headers, timeout]
runs:
  using: binary
  url: https://github.com/mozership/probe-graphql/releases/download/v0.2.0/probe-graphql_{os}_{arch}
  checksums:
    darwin_amd64: <probe-graphql_darwin_amd64のSHA-256>
    darwin_arm64: <probe-graphql_darwin_arm64のSHA-256>
    linux_amd64: <probe-graphql_linux_amd64のSHA-256>
    linux_arm64: <probe-graphql_linux_arm64のSHA-256>
```

| キー | 説明 |
|---|---|
| `runs.using` | `binary`のみ |
| `runs.url` | 実行ファイルのダウンロード元。`{os}`と`{arch}`はGoの`GOOS`と`GOARCH`（`linux`、`arm64`など）に置き換わります |
| `runs.path` | アクションのディレクトリからの相対パスで指す実行ファイル。プレースホルダーは`url`と同じです。ローカルのアクションでのみ使えます |
| `runs.checksums` | `<os>_<arch>`ごとの実行ファイルのSHA-256ダイジェスト（小文字の16進数） |
| `guard` | アクションが従うガードの種類。`read-only`、`allow-host`、またはその両方。Probeはこの宣言を検証せずに、その種類のガードの下でアクションを実行します。書かなければ、`--allow-action`で指定しない限り、どのガードの下でも拒否されます。[ガード](/ja/guide/concepts/guard)を参照してください |
| `params` | アクションが`with`で受け取るキー。`probe check`が、受け取らないキーを報告するのに使います。書かなければ`with`は検査しません。`[]`はキーを受け取らないことを示します |
| `cleanup` | ワークフローが終わったときにProbeに呼ばせ、Stepが残したものを消すアクションでは`true`。書かなければ呼ばれません。[実行の後始末](#実行の後始末)を参照してください |

`runs`には`url`と`path`のどちらか一方だけを書きます。`url`の場合は実行中のプラットフォームのチェックサムが必須で、ダイジェストが一致しないダウンロードは拒否します。ダイジェストは実行ファイルを起動するたびにも確かめます。`uses`のコミットが`action.yml`を固定し、`action.yml`がダイジェストを固定するので、実行されるファイルはコミットで一意に決まります。

`path`を使うローカルのアクションにチェックサムは不要です。書いた場合はリモートと同じく照合します。

#### 外部アクションの作り方

実行ファイルは`actionrpc.Serve`でアクションを提供します：

```go
package main

import (
    "fmt"
    "time"

    "github.com/hashicorp/go-hclog"
    "github.com/mozership/probe/actionrpc"
)

// Greet は専用の実行ファイルで提供されるアクションです。
type Greet struct {
    log hclog.Logger
}

func (g *Greet) Run(with map[string]any) (map[string]any, error) {
    start := time.Now()
    actionrpc.LogParams(g.log, "greet received parameters", with)

    name, _ := with["name"].(string)
    return map[string]any{
        "req":    with,
        "res":    map[string]any{"message": fmt.Sprintf("Hello, %s!", name)},
        "rt":     time.Since(start).String(),
        "status": 0,
    }, nil
}

func main() {
    actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
        return &Greet{log: log}
    })
}
```

`Run`が返すエラーは、ステップの結果と`--report`が書くファイルにそのまま表示されます。ステップが失敗した理由を読む人に向けた文言にしてください。返す前にパニックしたり終了したりしたアクションのステップは、プロセスの終了状態と、パニックならそのメッセージとともに失敗します。

アクションがエラーレベルで記録したログは、`--verbose`を付けたときだけ表示されます。返すエラーが同じことをすでに伝えるためです。ステップの結果にかかわらず利用者に見せる必要があることは警告レベルで、不具合の調査に役立つことはデバッグレベルで記録してください。

アクションは現在のProbeの`actionrpc`でビルドしてください。Probeは、現在の`actionrpc`がエラーに付ける印で、アクションが返したエラーと接続の切断とを区別します。印のないエラーのgRPCコードが`Unavailable`だと、Probeはプロセスが終了したかどうかを確かめるために少し待ちます。

対応するプラットフォームごとにビルドして実行ファイルを公開し、そのURLとダイジェストを書いた`action.yml`をコミットします。利用者はその`action.yml`を含むコミットを指定します。[mozership/probe-graphql](https://github.com/mozership/probe-graphql)では、GoReleaserと、リリースのたびにダイジェストをコミットするワークフローでこれを行っています。

リリースの`action.yml`は`probe manifest`で作れます。リポジトリの`action.yml`を読み、`runs.url`のタグを置き換え、`runs.checksums`をチェックサムファイルのダイジェストに置き換えて出力します：

```bash
probe manifest v0.2.0 dist/checksums.txt > "$RUNNER_TEMP/action.yml"
cp "$RUNNER_TEMP/action.yml" action.yml
```

名前、説明、`guard`、`params`、`cleanup`は`action.yml`にあるまま残るので、これらを書く場所は`action.yml`だけになります。

新しいアクションでは、`probe manifest init`が雛形の`action.yml`を出力します。アクションを公開するGitHubリポジトリを指定して実行し、説明と`guard`、`params`、`cleanup`を書き入れてコミットします。最初のリリースで、そのタグとダイジェストが入ります：

```bash
probe manifest init mozership/probe-greet > action.yml
```

どちらの引数も[`manifest`](/ja/reference/cli-reference#manifest)を参照してください。

開発中は、ローカルの`action.yml`でビルドした実行ファイルを指します：

```yaml
runs:
  using: binary
  path: probe-greet
```

```yaml
- name: Say hello
  uses: ./greet
  with:
    name: probe
  test: res.message == "Hello, probe!"
```

### Probeへのカスタムアクションの組み込み

カスタムアクションをProbe自体に組み込むこともできます。Probeは組み込みアクションごとに別のプロセスを使い、自身の実行ファイルを`<実行ファイル> builtin-actions <名前>`として起動し直します。そのため、このようなアクションは独自にビルドしたProbeに組み込みます。Probeをライブラリとして使うプログラムを作り、このサブコマンドで自分のアクションを提供し、それ以外の名前では組み込みアクションを提供します。

```go
package main

import (
    "fmt"
    "os"
    "time"

    "github.com/hashicorp/go-hclog"
    "github.com/mozership/probe"
    "github.com/mozership/probe/actionrpc"
    "github.com/mozership/probe/actions"
)

// Greet は `uses: greet` で使うカスタムアクション
type Greet struct {
    log hclog.Logger
}

func (g *Greet) Run(with map[string]any) (map[string]any, error) {
    start := time.Now()
    actionrpc.LogParams(g.log, "greet received parameters", with)

    name, _ := with["name"].(string)
    return map[string]any{
        "req":    with,
        "res":    map[string]any{"message": fmt.Sprintf("Hello, %s!", name)},
        "rt":     time.Since(start).String(),
        "status": 0,
    }, nil
}

func main() {
    // Probe はアクションごとに、この実行ファイルを
    // `<実行ファイル> builtin-actions <名前>` として起動し直す
    if len(os.Args) == 3 && os.Args[1] == actionrpc.BuiltinCmd {
        name := os.Args[2]
        if name == "greet" {
            actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
                return &Greet{log: log}
            })
            return
        }
        if serve, ok := actions.Lookup(name); ok {
            serve()
            return
        }
        fmt.Fprintf(os.Stderr, "unknown action: %s\n", name)
        os.Exit(1)
    }

    p := probe.New(os.Args[1], false)
    if err := p.Do(); err != nil {
        fmt.Fprintln(os.Stderr, err)
    }
    os.Exit(p.ExitStatus())
}
```

これをビルドし、`probe`の代わりにできたバイナリでワークフローを実行します：

```yaml
- name: Say hello
  uses: greet
  with:
    name: probe
  test: res.message == "Hello, probe!"
```

## 次のステップ

アクションシステムを理解したら、以下を探索してください：

1. **[式とテンプレート](/ja/guide/concepts/expressions-and-templates)** - 動的設定とテストを学ぶ
2. **[データフロー](/ja/guide/concepts/data-flow)** - アクション間でのデータの流れを理解する
3. **[ハウツー](/ja/guide/how-tos/api-testing)** - 実用的なアクション使用パターンを見る

Probeのステップが行う処理は、すべてアクションが実行します。組み込みアクションをマスターし、Probeがアクションを実行する仕組みを理解して、強力で拡張可能な自動化ワークフローを構築しましょう。
