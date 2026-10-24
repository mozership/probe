# ワークフロー

ワークフローは、完全な自動化または監視プロセスを定義するProbeのトップレベルコンテナです。このガイドでは、ワークフロー構造、設計パターン、保守性と効果的なワークフローを作成するためのベストプラクティスについて説明します。

## ワークフロー構造

すべてのProbeワークフローは、いくつかの重要なコンポーネントで構成されています：

```yaml
name: Workflow Name                    # 必須: 人間が読みやすい名前
description: What this workflow does   # オプション: 詳細な説明
vars:                                  # オプション: 変数
  API_BASE_URL: https://api.example.com
jobs:                                  # 必須: 一つ以上のジョブ
- id: api-check                        # オプション: 他のジョブから参照するためのID
  name: API Check                      # 必須: ジョブ名
  steps:                               # 必須: 一つ以上のステップ
    - name: Check API Health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: res.code == 200
```

### 必須コンポーネント

**名前**: すべてのワークフローは、その目的を明確に識別する説明的な名前を持つ必要があります。

```yaml
# 良い例
name: Production API Health Check
name: E-commerce Checkout Flow Test
name: Database Migration Validation

# 汎用的な名前は避ける
name: Test
name: Workflow
name: Check
```

**ジョブ**: 少なくとも一つのジョブを定義する必要があります。ジョブには実行される実際の作業が含まれます。

### オプションコンポーネント

**説明**: ワークフローの目的、範囲、期待される結果について詳細なコンテキストを提供します。

```yaml
description: |
  プロダクション API の包括的なヘルスチェック:
  - 認証エンドポイント検証
  - コアビジネスロジック検証
  - データベース接続テスト
  - サードパーティサービス統合チェック
```

**変数**: 環境固有または設定変数を定義します。

```yaml
vars:
  API_BASE_URL: https://api.production.example.com
  TIMEOUT_SECONDS: 30
  MAX_RETRY_COUNT: 3
```

**デフォルト**: 各ジョブ内でそのジョブのすべてのステップに適用される共通設定を設定します。トップレベルの`defaults`はすべてのジョブに適用され、ジョブ自身の`defaults`がそれより優先されます。

```yaml
name: Health Check
vars:
  api_url: "{{API_URL}}"

jobs:
- name: Health Check
  defaults:
    http:
      url: "{{vars.api_url}}"
      headers:
        Accept: "application/json"
        User-Agent: "Probe Health Monitor v1.0"
  steps:
    - name: Ping
      uses: http
      with:
        get: /health
      test: res.code == 200
```

## ワークフロー設計パターン

ワークフローの形は依存関係から決まります。ほとんどの場合は4つの形のいずれかになります。一直線、独立したジョブの並列、段階ごとのまとまり、そして分岐させて再び集約する形です。

### 1. 線形ワークフロー

ステップが順次実行され、各ステップは前のステップの成功に依存します。

```yaml
name: Database Migration
description: Execute database schema changes in order
vars:
  DB_API: "{{DB_API}}"

jobs:
- id: migration
  name: Run Migration Steps
  steps:
    - name: Backup Current Schema
      uses: http
      with:
        url: "{{vars.DB_API}}/backup"
        method: POST
      test: res.code == 200

    - name: Apply Schema Changes
      uses: http
      with:
        url: "{{vars.DB_API}}/migrate"
        method: POST
      test: res.code == 200

    - name: Verify Migration
      uses: http
      with:
        url: "{{vars.DB_API}}/schema/version"
        method: GET
      test: res.body.version == "2.1.0"

    - name: Update Documentation
      uses: hello
      echo: "Migration to v2.1.0 completed successfully"
```

**使用例:**
- データベース移行
- デプロイメントパイプライン
- セットアップ/ティアダウンプロセス

### 2. 並列ワークフロー

効率性のため、複数の独立したチェックが同時実行されます。

```yaml
name: Multi-Service Health Check
description: Check health of all microservices in parallel
vars:
  USER_SERVICE_URL: "{{USER_SERVICE_URL}}"
  PAYMENT_SERVICE_URL: "{{PAYMENT_SERVICE_URL}}"
  NOTIFICATION_SERVICE_URL: "{{NOTIFICATION_SERVICE_URL}}"

jobs:
- id: user-service
  name: User Service Health
  steps:
    - name: Check User API
      uses: http
      with:
        method: GET
        url: "{{vars.USER_SERVICE_URL}}/health"
      test: res.code == 200

- id: payment-service
  name: Payment Service Health
  steps:
    - name: Check Payment API
      uses: http
      with:
        method: GET
        url: "{{vars.PAYMENT_SERVICE_URL}}/health"
      test: res.code == 200

- id: notification-service
  name: Notification Service Health
  steps:
    - name: Check Notification API
      uses: http
      with:
        method: GET
        url: "{{vars.NOTIFICATION_SERVICE_URL}}/health"
      test: res.code == 200
```

**使用例:**
- マルチサービス監視
- 独立機能テスト
- リソース検証

### 3. 段階的ワークフロー

依存関係による並列と順次実行を組み合わせます。

```yaml
name: Application Deployment Validation
description: Validate deployment across multiple stages
vars:
  DB_HEALTH_URL: "{{DB_HEALTH_URL}}"
  REDIS_HEALTH_URL: "{{REDIS_HEALTH_URL}}"
  API_URL: "{{API_URL}}"

jobs:
  # ステージ 1: インフラチェック（並列）
- id: database-check
  name: Database Connectivity
  steps:
    - name: Test Database Connection
      uses: http
      with:
        method: GET
        url: "{{vars.DB_HEALTH_URL}}"
      test: res.code == 200

- id: cache-check
  name: Cache Service Check
  steps:
    - name: Test Redis Connection
      uses: http
      with:
        method: GET
        url: "{{vars.REDIS_HEALTH_URL}}"
      test: res.code == 200

# ステージ 2: アプリケーションチェック（インフラに依存）
- id: api-validation
  name: API Service Validation
  needs: [database-check, cache-check]
  steps:
    - name: Test Core API Endpoints
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health"
      test: res.code == 200

# ステージ 3: エンドツーエンドテスト（API に依存）
- id: e2e-tests
  name: End-to-End Tests
  needs: [api-validation]
  steps:
    - name: Test User Registration Flow
      uses: http
      with:
        url: "{{vars.API_URL}}/auth/register"
        method: POST
        body: |
          {
            "email": "test@example.com",
            "password": "testpass123"
          }
      test: res.code == 201
```

**使用例:**
- デプロイメント検証
- 複雑なシステムテスト
- マルチティアアプリケーション監視

### 4. ファンアウト/ファンインワークフロー

並列実行とその後の集約。

```yaml
name: Regional Service Check
description: Check services across multiple regions and aggregate results

jobs:
  # ファンアウト: 各リージョンを並列チェック
- id: us-east-check
  name: US East Region Check
  steps:
    - name: Check US East API
      id: us-east-check
      uses: http
      with:
        method: GET
        url: https://us-east.api.example.com/health
      test: res.code == 200
      outputs:
        us_east_status: res.body.status
        us_east_time: (rt.sec * 1000)

- id: us-west-check
  name: US West Region Check
  steps:
    - name: Check US West API
      id: us-west-check
      uses: http
      with:
        method: GET
        url: https://us-west.api.example.com/health
      test: res.code == 200
      outputs:
        us_west_status: res.body.status
        us_west_time: (rt.sec * 1000)

- id: eu-check
  name: Europe Region Check
  steps:
    - name: Check EU API
      id: eu-check
      uses: http
      with:
        method: GET
        url: https://eu.api.example.com/health
      test: res.code == 200
      outputs:
        eu_status: res.body.status
        eu_time: (rt.sec * 1000)

# ファンイン: 結果を集約
- id: summary
  name: Regional Summary
  needs: [us-east-check, us-west-check, eu-check]
  steps:
    - name: Generate Report
      uses: hello
      echo: |
        Regional Health Check Results:

        US East: {{outputs.us_east_status}} ({{outputs.us_east_time}}ms)
        US West: {{outputs.us_west_status}} ({{outputs.us_west_time}}ms)
        Europe: {{outputs.eu_status}} ({{outputs.eu_time}}ms)

        Total regions healthy: {{
          (outputs.us_east_status == "healthy" ? 1 : 0) +
          (outputs.us_west_status == "healthy" ? 1 : 0) +
          (outputs.eu_status == "healthy" ? 1 : 0)
        }}/3
```

**使用例:**
- マルチリージョン監視
- 環境間での負荷テスト
- 分散システム検証

## ワークフロー整理戦略

ワークフローが複数になると、どこでファイルを分けるか、各ファイルを実行環境にどう合わせるかが問題になります。

### 1. 単一目的ワークフロー

ワークフローを単一のよく定義された目的に焦点を絞って保持します。

```yaml
# 良い例: API ヘルスチェックに焦点
name: API Health Check
description: Monitor the health of our REST API endpoints

# 良い例: データベース操作に焦点
name: Database Maintenance
description: Perform routine database maintenance tasks

# 避ける: 混在した責任
name: API and Database and Email Check
description: Check everything
```

### 2. レイヤードワークフロー

アーキテクチャレイヤーによってワークフローを整理します。

```yaml
# infrastructure-health.yml
name: Infrastructure Health Check
description: Check foundational infrastructure components

# application-health.yml  
name: Application Health Check
description: Check application-level services

# business-logic-tests.yml
name: Business Logic Validation
description: Test core business functionality
```

### 3. 環境対応ワークフロー

設定マージを使用して異なる環境で動作するワークフローを設計します。

**base-monitoring.yml:**
```yaml merge=monitoring base
name: Service Monitoring
description: Monitor critical services
vars:
  # production.yml または staging.yml の vars で置き換わる
  API_BASE_URL: "{{API_BASE_URL}}"

jobs:
- id: api-check
  name: API Check
  steps:
    - name: Check API Health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: res.code == 200
```

**production.yml:**
```yaml merge=monitoring
vars:
  API_BASE_URL: https://api.production.example.com
```

**staging.yml:**
```yaml merge=monitoring
vars:
  API_BASE_URL: https://api.staging.example.com
```

使用方法:
```bash
# プロダクション監視
probe base-monitoring.yml,production.yml

# ステージング監視  
probe base-monitoring.yml,staging.yml
```

## 高度なワークフロー技術

以下の手法を使うと、本来は複数のファイルが必要な場面を1つのファイルで扱えます。ジョブの条件付き実行、実行中の設定の導出、共通部分からのワークフローの組み立てです。

### 1. 条件付きジョブ実行

ジョブは`skipif`が真になったときにスキップされます。式からは`vars`と、依存しているジョブの`outputs`を参照できます。

```yaml
name: Conditional Diagnostics
vars:
  service_url: "{{SERVICE_URL}}"

jobs:
- id: health-check
  name: Basic Health Check
  steps:
    - name: Check Service
      id: service-check
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/health"
      test: res.code == 200
      outputs:
        service_healthy: res.code == 200

- name: Deep Diagnostic
  needs: [health-check]
  always: true
  skipif: outputs['service-check']?.service_healthy ?? false
  steps:
    - name: Run Diagnostics
      id: diagnostics
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/diagnostics"
      test: res.code == 200

- name: Send Alert
  needs: [health-check]
  always: true
  skipif: outputs['service-check']?.service_healthy ?? false
  steps:
    - name: Critical Alert
      uses: hello
      echo: "CRITICAL: {{vars.service_url}} is not healthy"
```

ヘルスチェックは結果を`test`で確かめ、同じ結果をoutputsとしても公開しています。依存先のジョブが失敗したジョブはスキップされるので、チェックが失敗したときのための2つのジョブには`always: true`を付けています。接続できなかったチェックは出力を公開しないため、出力は`?.`と`??`で読みます。


### 2. 動的設定

式を使用して、実行時の条件に応じてワークフローの設定値を決めます。

```yaml
name: Load Test
vars:
  load_test_url: "{{LOAD_TEST_URL}}"
  load_test_users: "{{LOAD_TEST_USERS ?? 10}}"
  load_test_duration: "{{LOAD_TEST_DURATION ?? 60}}"

jobs:
- id: load-test
  name: Load Testing
  steps:
    - name: Determine Load Parameters
      uses: hello
      id: params
      echo: "Load test configuration determined"
      outputs:
        concurrent_users: int(vars.load_test_users)
        test_duration: int(vars.load_test_duration)

    - name: Execute Load Test
      uses: http
      with:
        url: "{{vars.load_test_url}}"
        method: POST
        body: |
          {
            "concurrent_users": {{outputs.params.concurrent_users}},
            "duration_seconds": {{outputs.params.test_duration}}
          }
      test: res.code == 200
```

### 3. ワークフロー構成

複雑なワークフローを再利用可能なコンポーネントに分解します。`jobs`を持てるのは1ファイルだけなので、共有するファイルではジョブをYAMLアンカーの下に置き、ワークフロー側で自分のジョブと並べて列挙します。

**common-setup.yml:**
```yaml merge=composition base
shared:
  setup_job: &setup_job
    id: setup
    name: Common Setup
    steps:
      - name: Initialize Environment
        uses: hello
        id: setup
        echo: "Environment initialized"
        outputs:
          timestamp: unixtime()
          session_id: random_str(8)
```

**main-workflow.yml:**
```yaml merge=composition
name: Complete System Test
description: Full system validation with common setup

vars:
  API_URL: "{{API_URL}}"

jobs:
- *setup_job    # common-setup.yml で定義
- id: api-tests
  name: API Tests
  needs: [setup]
  steps:
    - name: Test API with Session
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/test"
        headers:
          X-Session-ID: "{{outputs.setup.session_id}}"
      test: res.code == 200
```

使用方法:
```bash
probe common-setup.yml,main-workflow.yml
```

## ベストプラクティス

以下では、書いた本人以外が読んでも理解できるワークフローにするための指針と、規模が大きくなっても壊れないようにするための指針を扱います。

### 1. 命名規則

一貫性のある説明的な命名を使用します：

```yaml
# ワークフロー名: タイトルケースを使用
name: Production API Health Check
vars:
  API_URL: "{{API_URL}}"

# ジョブ名: 説明的で具体的
jobs:
- id: user-authentication-test
  name: User Authentication Test
  steps:
    # ステップ名: アクション指向
    - name: Verify User Login Endpoint
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/auth/login"
      test: res.code == 200

- id: database-connectivity-check
  name: Database Connectivity Check
  steps:
    - name: Test Database Connection Pool
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health/db-pool"
      test: res.code == 200

    - name: Validate Cache Expiration
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health/cache"
      test: res.code == 200
```

### 2. ドキュメント

包括的なドキュメントを含めます：

```yaml
name: E-commerce Checkout Flow Test
description: |
  以下を含む完全な e コマースチェックアウトプロセスを検証します：

  1. プロダクトカタログブラウジング
  2. ショッピングカート管理  
  3. ユーザー認証
  4. 決済処理
  5. 注文確認
  6. メール通知配信

  このワークフローは、プロダクト選択から注文完了までの実際の
  ユーザージャーニーをシミュレートし、すべての重要なビジネス
  ロジックが正しく機能することを確認します。

  前提条件:
  - 有効な決済方法を持つテストユーザーアカウント
  - テストデータが投入されたプロダクトカタログ
  - 通知用に設定されたメールサービス

  予想実行時間: 2-3分

  テストされる失敗シナリオ:
  - 無効な決済情報
  - 在庫切れプロダクト
  - メール配信失敗
```

### 3. エラーハンドリング戦略

失敗シナリオを計画します：

```yaml
name: Service Failover
vars:
  primary_service_url: "{{PRIMARY_SERVICE_URL}}"
  fallback_service_url: "{{FALLBACK_SERVICE_URL}}"

jobs:
- id: primary-check
  name: Primary Service Check
  steps:
    - name: Check Primary Service
      id: primary
      uses: http
      with:
        method: GET
        url: "{{vars.primary_service_url}}"
      test: res.code == 200
      outputs:
        primary_healthy: res.code == 200

- id: fallback-check
  name: Fallback Service Check
  steps:
    - name: Check Fallback Service
      id: fallback
      uses: http
      with:
        method: GET
        url: "{{vars.fallback_service_url}}"
      test: res.code == 200
      outputs:
        fallback_healthy: res.code == 200

- id: notification
  name: Send Notifications
  needs: [primary-check, fallback-check]
  always: true    # チェックが失敗しても実行する
  steps:
    - name: Success Notification
      uses: hello
      skipif: outputs.primary_healthy != true
      echo: "✅ Primary service is healthy"

    - name: Fallback Notification
      uses: hello
      skipif: outputs.primary_healthy == true || outputs.fallback_healthy != true
      echo: "⚠️ Primary service down, fallback operational"

    - name: Critical Alert
      uses: hello
      skipif: outputs.primary_healthy == true || outputs.fallback_healthy == true
      echo: "🚨 CRITICAL: Both primary and fallback services are down"
```

`always: true`により、チェックが失敗しても通知ジョブは実行されます。接続できなかったチェックは出力を公開せず、その出力はnilとして読まれます。そのため条件は`true`と比較し、各通知は該当する場合以外はスキップされます。

### 4. パフォーマンス考慮事項

最適なパフォーマンスのためワークフローを設計します：

```yaml fragment
# 良い例: 独立操作の並列実行
jobs:
- id: frontend-check   # これらは並列実行される
- id: backend-check    # パフォーマンス向上のため
- id: database-check

# 良い例: 効率的なジョブ依存関係
jobs:
- id: infrastructure
  name: infrastructure
- id: application
  name: application
  needs: [infrastructure]
- id: integration
  name: integration
  needs: [application]

# 避ける: 不要な順次依存関係
jobs:
- id: check-a
  name: check-a
- id: check-b
  name: check-b
  needs: [check-a]  # B が実際に A に依存する場合のみ
```

## よくあるアンチパターン

以下の3つの形はワークフローを変更しにくくします。いずれにも単純な直し方があります。

### 1. モノリシックワークフロー

**避ける:**
```yaml fragment
name: Everything Check
jobs:
- id: massive-job
  name: massive-job
  steps:
    - name: Check API
    - name: Check Database
    - name: Check Cache
    - name: Check Email
    - name: Check Files
    - name: Check Logs
    # ... 50個以上のステップ
```

**代替案:**
```yaml
# 焦点を絞ったワークフローに分割
name: API Health Check
name: Database Health Check  
name: Infrastructure Health Check
```

### 2. 密結合

**避ける:**
```yaml
# 全体にハードコーディングされた値
- name: Check Production API
  uses: http
  with:
    method: GET
    url: https://prod-api.company.com/health
```

**代替案:**
```yaml
# 設定と環境変数を使用
- name: Check API
  uses: http
  with:
    method: GET
    url: "{{vars.API_BASE_URL}}/health"
```

### 3. エラーハンドリング不足

**避ける:**
```yaml
steps:
  - name: Critical Operation
    uses: http
    with:
      method: GET
      url: "{{vars.CRITICAL_SERVICE}}"
    # テスト条件やエラーハンドリングなし
```

**代替案:**
```yaml
steps:
  - name: Critical Operation
    uses: http
    with:
      method: GET
      url: "{{vars.CRITICAL_SERVICE}}"
    test: res.code == 200
```

## 次のステップ

ワークフロー設計とパターンを理解したら、以下を探索してください：

1. **[ジョブとステップ](/ja/guide/concepts/jobs-and-steps)** - ジョブとステップの仕組みを深く理解する
2. **[アクション](/ja/guide/concepts/actions)** - アクションシステムと利用可能なアクションについて学ぶ
3. **[式とテンプレート](/ja/guide/concepts/expressions-and-templates)** - 動的設定をマスターする

ワークフローはProbe自動化の基盤です。堅実なワークフロー設計スキルがあれば、保守性が高く、効率的で、信頼性のある自動化プロセスを構築できます。
