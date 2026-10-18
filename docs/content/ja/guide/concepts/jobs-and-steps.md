# ジョブとステップ

ジョブとステップはProbeワークフローの構築要素です。それらの仕組み、実行モデル、相互作用パターンを理解することは、効果的な自動化を構築するために重要です。このガイドでは詳細な動作と高度な使用例について説明します。

## ジョブの基礎

**ジョブ**は、関連するステップをまとめて一つの単位として実行する論理的なグループです。ジョブは以下を提供します：

- **分離**: 各ジョブは独自のコンテキストで実行される
- **並列性**: 依存関係がない限り、ジョブは同時実行可能
- **状態管理**: ジョブごとに実行状態と結果を記録
- **出力共有**: ジョブは他のジョブが使用する出力を生成可能

### ジョブ構造

ジョブの定義では以下のプロパティを指定します。

```yaml
jobs:
- id: job-id
  name: Human Readable Name # オプション: 表示名
  needs: [other-job]       # オプション: ジョブ依存関係
  timeout: 300s            # オプション: ジョブタイムアウト
  steps:                   # 必須: ステップの配列
    # ステップ定義...
```

### ジョブライフサイクル

ジョブは実行中に複数の状態を経過します：

1. **Pending**: ジョブが実行待ちキューに入っている
2. **Running**: ジョブがアクティブにステップを実行中
3. **Success**: すべてのステップが正常に完了
4. **Failed**: 一つ以上のステップが失敗
5. **Skipped**: 条件によりジョブがスキップされた
6. **Cancelled**: タイムアウトやエラーによりジョブがキャンセルされた

### ジョブ依存関係

`needs`キーワードを使用して実行依存関係を作成します：

```yaml
jobs:
- id: setup
  name: Environment Setup
  steps:
    - name: Initialize Database
      id: setup
      uses: http
      with:
        post: "/init"
      test: res.code == 200
      outputs:
        db_session_id: res.body.session_id

- id: test-suite-a
  name: API Test Suite A
  needs: [setup]           # setup の完了を待つ
  steps:
    - name: Test User API
      uses: http
      with:
        get: "/users"
        headers:
          X-Session-ID: "{{outputs.setup.db_session_id}}"
      test: res.code == 200

- id: test-suite-b
  name: API Test Suite B
  needs: [setup]           # setup にも依存
  steps:
    - name: Test Order API
      uses: http
      with:
        get: "/orders"
        headers:
          X-Session-ID: "{{outputs.setup.db_session_id}}"
      test: res.code == 200

- id: cleanup
  name: Environment Cleanup
  needs: [test-suite-a, test-suite-b]  # 両方のテストスイートを待つ
  steps:
    - name: Clean Database
      uses: http
      with:
        post: "/cleanup"
        headers:
          X-Session-ID: "{{outputs.setup.db_session_id}}"
      test: res.code == 200
```

### 条件付きジョブ実行

ジョブは`skipif`が真になったときにスキップされます。式からは`vars`と、依存しているジョブが公開した`outputs`を参照できます。

ジョブが失敗すると、それに依存するジョブはまとめてスキップされます。結果で分岐したい場合は、`test`で失敗させるのではなくoutputsとして公開してください。

```yaml
jobs:
- id: health-check
  name: Basic Health Check
  steps:
    - name: Ping Service
      id: ping
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/ping"
      outputs:
        service_responsive: res.code == 200

- name: Detailed Health Check
  needs: [health-check]
  skipif: "!outputs.ping.service_responsive"
  steps:
    - name: Deep Health Check
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/health/detailed"
      test: res.code == 200

- name: Send Notification
  needs: [health-check]
  skipif: outputs.ping.service_responsive
  steps:
    - name: Alert Team
      uses: hello
      echo: "{{vars.service_url}} did not respond to ping"
```

設定値だけでジョブをスキップすることもできます。

```yaml
- name: Production smoke test
  skipif: vars.environment != "production"
  steps:
    - name: Check
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/health"
      test: res.code == 200
```


## ステップの基礎

**ステップ**はProbeの最小実行単位です。各ステップは特定のアクションを実行し、以下が可能です：

- アクションの実行（HTTPリクエスト、コマンドなど）
- アサーションによる結果テスト
- 他のステップで使用する出力の生成
- コンソールへのメッセージ出力
- 条件付き実行

### ステップ構造

ステップの定義では以下のプロパティを指定します。

```yaml
steps:
  - name: Step Name          # 必須: 説明的な名前
    id: step-id             # オプション: 参照用の一意識別子
    action: http            # オプション: 実行するアクション
    with:                   # オプション: アクションパラメータ
      url: https://api.example.com
      method: GET
    test: res.code == 200 # オプション: テスト条件
    outputs:                # オプション: 他のステップに渡すデータ
      response_time: (rt.sec * 1000)
      user_count: res.body.total_users
    echo: "Message"         # オプション: メッセージ表示
    timeout: 30s            # オプション: ステップタイムアウト
```

### ステップタイプ

ステップはアクションを呼ぶか、何かを出力するか、その両方を1つで行うかのいずれかです。

#### 1. アクションステップ

HTTPリクエストなどの特定のアクションを実行：

```yaml
- name: Check User API
  uses: http
  with:
    url: "{{vars.API_URL}}/users/{{vars.TEST_USER_ID}}"
    method: GET
    headers:
      Authorization: "Bearer {{vars.API_TOKEN}}"
      Accept: "application/json"
  test: res.code == 200 && res.body.user.active == true
  outputs:
    user_id: res.body.user.id
    user_email: res.body.user.email
    last_login: res.body.user.last_login
```

#### 2. Echoステップ

メッセージや計算された値を表示：

```yaml
- name: Display Results
  echo: |
    Test Results Summary:
    
    User ID: {{outputs['previous-step'].user_id}}
    Email: {{outputs['previous-step'].user_email}}
    Last Login: {{outputs['previous-step'].last_login}}
    
    Response Time: {{outputs['previous-step'].response_time}}ms
    Test Completed: {{unixtime()}}
```

#### 3. ハイブリッドステップ

アクションとechoメッセージを組み合わせ：

```yaml
- name: Test and Report
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/status"
  test: res.code == 200
  echo: |
    API Status Check:
    Status Code: {{res.status}}
    Response Time: {{rt.duration}}
    API Version: {{res.body.version}}
```

### ステップ実行フロー

ジョブ内のステップはデフォルトで順次実行されます：

```yaml
jobs:
- id: sequential-test
  name: Sequential Step Execution
  steps:
    - name: Step 1 - Setup
      id: setup
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/setup"
      test: res.code == 200
      outputs:
        session_id: res.body.session_id

    - name: Step 2 - Execute Test
      id: test
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/test"
        headers:
          X-Session-ID: "{{outputs.setup.session_id}}"
      test: res.code == 200
      outputs:
        test_result: res.body.result

    - name: Step 3 - Cleanup
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/cleanup"
        headers:
          X-Session-ID: "{{outputs.setup.session_id}}"
      test: res.code == 200

    - name: Step 4 - Report
      uses: hello
      echo: "Test completed with result: {{outputs.test.test_result}}"
```

### 条件付きステップ実行

ステップは`skipif`が真になったときにスキップされます。式から見えるのは`test`と同じコンテキストで、先行ステップの`outputs`も参照できます。

```yaml
steps:
  - name: Primary Health Check
    id: primary
    uses: http
    with:
      method: GET
      url: "{{vars.primary_url}}/health"
    outputs:
      primary_healthy: res.code == 200

  - name: Backup Service Check
    id: backup
    uses: http
    skipif: outputs.primary.primary_healthy
    with:
      method: GET
      url: "{{vars.backup_url}}/health"
    outputs:
      backup_healthy: res.code == 200

  - name: Report
    uses: hello
    echo: |
      Primary: {{outputs.primary.primary_healthy ? "Online" : "Offline"}}
      Backup: {{outputs.backup_healthy ?? "not checked"}}
```


## 高度なパターン

以下のパターンは、ステップを順に実行するだけでは扱えない場面に使います。失敗からの回復、複数ステップにまたがる結果の収集、先に求めた値によるステップの設定です。

### 1. エラー回復パターン

回復ステップによる堅牢なエラーハンドリングを実装：

```yaml
jobs:
- id: resilient-check
  name: Resilient Service Check
  steps:
    - name: Attempt Primary Connection
      id: primary-attempt
      uses: http
      timeout: 10s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/api/v1/health"
      test: res.code == 200
      outputs:
        primary_success: res.code == 200

    - name: Try Alternative Endpoint
      id: alt-attempt
      uses: http
      timeout: 15s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/api/v2/health"
      test: res.code == 200
      outputs:
        alt_success: res.code == 200

    - name: Fallback to Legacy Endpoint
      id: legacy-attempt
      uses: http
      timeout: 20s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        legacy_success: res.code == 200

    - name: Final Status Report
      uses: hello
      echo: |
        Service Health Check Results:
          
        Primary API (v1): {{outputs['primary-attempt'].primary_success ? "✅" : "❌"}}
        Alternative API (v2): {{outputs['alt-attempt'].alt_success ? "✅" : "❌"}}
        Legacy API: {{outputs['legacy-attempt'].legacy_success ? "✅" : "❌"}}
          
        Overall Status: {{
          outputs['primary-attempt'].primary_success || 
          outputs['alt-attempt'].alt_success || 
          outputs['legacy-attempt'].legacy_success ? "HEALTHY" : "DOWN"
        }}
```

### 2. データ収集と集約

分析のため複数のステップでデータを収集：

```yaml
jobs:
- id: performance-analysis
  name: Performance Analysis
  steps:
    - name: Test Homepage
      id: homepage
      uses: http
      with:
        method: GET
        url: "{{vars.BASE_URL}}/"
      test: res.code == 200
      outputs:
        homepage_time: (rt.sec * 1000)
        homepage_size: res.body_size

    - name: Test API Endpoint
      id: api
      uses: http
      with:
        method: GET
        url: "{{vars.BASE_URL}}/api/users"
      test: res.code == 200
      outputs:
        api_time: (rt.sec * 1000)
        api_size: res.body_size

    - name: Test Search Function
      id: search
      uses: http
      with:
        method: GET
        url: "{{vars.BASE_URL}}/search?q=test"
      test: res.code == 200
      outputs:
        search_time: (rt.sec * 1000)
        search_size: res.body_size

    - name: Performance Summary
      uses: hello
      echo: |
        Performance Analysis Results:
          
        Homepage:
          Response Time: {{outputs.homepage.homepage_time}}ms
          Size: {{outputs.homepage.homepage_size}} bytes
            
        API Endpoint:
          Response Time: {{outputs.api.api_time}}ms
          Size: {{outputs.api.api_size}} bytes
            
        Search Function:
          Response Time: {{outputs.search.search_time}}ms
          Size: {{outputs.search.search_size}} bytes
            
        Average Response Time: {{
          (outputs.homepage.homepage_time + 
           outputs.api.api_time + 
           outputs.search.search_time) / 3
        }}ms
          
        Total Data Transfer: {{
          outputs.homepage.homepage_size + 
          outputs.api.api_size + 
          outputs.search.search_size
        }} bytes
```

### 3. 動的ステップ設定

実行時条件に基づいてステップを設定：

```yaml
jobs:
- id: adaptive-monitoring
  name: Adaptive Monitoring
  steps:
    - name: Determine Environment
      id: env-detect
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/config"
      test: res.code == 200
      outputs:
        environment: res.body.environment
        feature_flags: res.body.features
        monitoring_level: res.body.monitoring.level

    - name: Basic Health Check
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/health"
      test: res.code == 200

    - name: Detailed Monitoring
      id: adaptive-monitoring
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/metrics"
      test: res.code == 200
      outputs:
        cpu_usage: res.body.system.cpu_percent
        memory_usage: res.body.system.memory_percent

    - name: Feature-Specific Tests
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/beta/features"
      test: res.code == 200

    - name: Production Alerts
      uses: hello
      echo: |
        🚨 PRODUCTION ALERT: High resource usage detected!
        CPU: {{outputs.detailed.cpu_usage}}%
        Memory: {{outputs.detailed.memory_usage}}%
```

## ステップとジョブの識別

結果を参照するには、それを生成した対象に名前が必要です。参照にはステップIDとジョブ名を使います。

### ステップID

`id`を使用してワークフローの他の部分からステップを参照します：

```yaml
steps:
  - name: User Authentication Test
    id: auth-test                    # 参照用IDを定義
    uses: http
    with:
      url: "{{vars.API_URL}}/auth/login"
      method: POST
      body: |
        {
          "username": "testuser",
          "password": "{{vars.TEST_PASSWORD}}"
        }
    test: res.code == 200
    outputs:
      auth_token: res.body.token
      user_id: res.body.user.id

  - name: User Profile Test
    uses: http
    with:
      method: GET
      url: "{{vars.API_URL}}/users/{{outputs['auth-test'].user_id}}"  # IDで参照
      headers:
        Authorization: "Bearer {{outputs['auth-test'].auth_token}}"   # IDで参照
    test: res.code == 200
```

### ジョブ参照

他のジョブからジョブ結果を参照：

```yaml
jobs:
- id: database-check
  name: Database Connectivity
  steps:
    - name: Test Database
      uses: http
      with:
        method: GET
        url: "{{vars.DB_API}}/ping"
      test: res.code == 200

- id: api-check
  name: API Functionality
  needs: [database-check]
  steps:
    - name: Test API
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health"
      test: res.code == 200

    - name: Skip Message
      uses: hello
      echo: "Skipping API test due to database connectivity issues"
```

## パフォーマンス最適化

実行時間は最も長い依存の連鎖で決まります。短縮できるのはその連鎖を短くする部分と、各ステップの所要時間を短くする部分です。

### 1. 並列ジョブ実行

可能な限りジョブが並列実行されるよう構造化：

```yaml
jobs:
  # これらのジョブは並列実行可能（依存関係なし）
- id: frontend-test
  name: Frontend Tests
  steps:
    - name: Test UI Components
      uses: http
      with:
        method: GET
        url: "{{vars.FRONTEND_URL}}"
      test: res.code == 200

- id: backend-test
  name: Backend Tests
  steps:
    - name: Test API Endpoints
      uses: http
      with:
        method: GET
        url: "{{vars.BACKEND_URL}}/api"
      test: res.code == 200

- id: database-test
  name: Database Tests
  steps:
    - name: Test Database Connection
      uses: http
      with:
        method: GET
        url: "{{vars.DB_URL}}/health"
      test: res.code == 200

# このジョブはすべての並列ジョブの完了を待つ
- id: integration-test
  name: Integration Tests
  needs: [frontend-test, backend-test, database-test]
  steps:
    - name: End-to-End Test
      uses: http
      with:
        method: GET
        url: "{{vars.APP_URL}}/integration-test"
      test: res.code == 200
```

### 2. 効率的なリソース使用

ベターなリソース利用のためステップ実行を最適化：

```yaml
jobs:
- id: efficient-monitoring
  name: Efficient Resource Monitoring
  steps:
    # タイムアウトを使用してハングを防止
    - name: Quick Health Check
      uses: http
      timeout: 5s                    # ping 用の短いタイムアウト
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/ping"
      test: res.code == 200

    # 条件付きの高コスト操作
    - name: Detailed Analysis
      uses: http
      timeout: 30s                   # 詳細分析用の長いタイムアウト
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/detailed-metrics"
      test: res.code == 200

    # 関連操作をバッチ化
    - name: Batch Status Check
      uses: http
      with:
        url: "{{vars.SERVICE_URL}}/batch-status"
        method: POST
        body: |
          {
            "checks": [
              {"type": "health", "endpoint": "/health"},
              {"type": "metrics", "endpoint": "/metrics"},
              {"type": "version", "endpoint": "/version"}
            ]
          }
      test: res.code == 200 && res.body.all_passed == true
```

## ベストプラクティス

以下では、1つのジョブにどれだけのステップをまとめるか、ステップにどう名前を付けるか、失敗をどう扱うかを扱います。

### 1. ジョブ粒度

ジョブサイズの適切なバランスを保つ：

```yaml
# 良い例: 焦点を絞った、一貫性のあるジョブ
jobs:
- id: authentication-tests
  name: Authentication System Tests
  steps:
    - name: Test Login
    - name: Test Logout
    - name: Test Token Refresh
    - name: Test Password Reset

- id: user-management-tests
  name: User Management Tests
  steps:
    - name: Test User Creation
    - name: Test User Update
    - name: Test User Deletion

# 避ける: 過度に細かいジョブ
jobs:
- id: test-login
  name: test-login
  steps:
    - name: Test Login
- id: test-logout
  name: test-logout
  steps:
    - name: Test Logout

# 避ける: モノリシックなジョブ
jobs:
- id: all-tests
  name: all-tests
  steps:
    - name: Test Login
    - name: Test Database
    - name: Test Email
    - name: Test Files
    # ... 50個以上の無関係なステップ
```

### 2. 明確なステップ名

説明的でアクション指向のステップ名を使用：

```yaml
steps:
  # 良い例: 明確で具体的な名前
  - name: Verify User Registration API Returns 201
  - name: Test Database Connection Pool Health
  - name: Validate JWT Token Expiration Logic
  - name: Check Email Service Rate Limiting

  # 避ける: 曖昧または汎用的な名前
  - name: Test API           # 曖昧すぎる
  - name: Check Thing        # 説明的でない
  - name: Step 1             # コンテキストなし
```

### 3. 適切なエラーハンドリング

適切なエラーハンドリング戦略を実装：

```yaml
steps:
  # 重要なステップ - 高速失敗
  - name: Verify Database Connectivity
    uses: http
    with:
      method: GET
      url: "{{vars.DB_URL}}/ping"
    test: res.code == 200

  # 非重要ステップ - 失敗時も継続
  - name: Update Usage Analytics
    uses: http
    with:
      method: GET
      url: "{{vars.ANALYTICS_URL}}/update"
    test: res.code == 200

  # 回復ステップ
  - name: Log Failure Details
    uses: hello
    echo: "Analytics update failed, but continuing with main workflow"
```

## 次のステップ

ジョブとステップを詳しく理解したら、以下を探索してください：

1. **[アクション](/ja/guide/concepts/actions)** - アクションシステムと利用可能なアクションについて学ぶ
2. **[式とテンプレート](/ja/guide/concepts/expressions-and-templates)** - 動的設定とテストをマスターする
3. **[データフロー](/ja/guide/concepts/data-flow)** - ワークフローを通してデータがどう移動するかを理解する

Probeはジョブとステップを単位にワークフローを実行します。これらの概念をマスターして、効率的で信頼性が高く、保守しやすい自動化ワークフローを構築しましょう。
