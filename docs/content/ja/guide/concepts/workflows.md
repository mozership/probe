# ワークフロー

ワークフローは、完全な自動化または監視プロセスを定義するProbeのトップレベルコンテナです。このガイドでは、ワークフロー構造、設計パターン、保守性と効果的なワークフローを作成するためのベストプラクティスについて説明します。

## ワークフロー構造

すべてのProbeワークフローは、いくつかの重要なコンポーネントで構成されています：

```yaml
name: Workflow Name                    # 必須: 人間が読みやすい名前
description: What this workflow does   # オプション: 詳細な説明
vars:                                  # オプション: 変数
  API_BASE_URL: https://api.example.com
jobs:                                 # 必須: 一つ以上のジョブ
- name: job-name                      # ジョブは配列形式
  defaults:                           # オプション: ジョブレベルのデフォルト設定
    http:
      headers:
        User-Agent: "Probe Monitor"
  # ジョブ定義...
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
jobs:
- name: Example Job
  defaults:
    http:
      headers:
        Accept: "application/json"
        User-Agent: "Probe Health Monitor v1.0"
  steps:
    # このジョブ内のすべてのステップに上記のdefaultsが適用されます
```

## ワークフロー設計パターン

ワークフローの形は依存関係から決まります。ほとんどの場合は4つの形のいずれかになります。一直線、独立したジョブの並列、段階ごとのまとまり、そして分岐させて再び集約する形です。

### 1. 線形ワークフロー

ステップが順次実行され、各ステップは前のステップの成功に依存します。

```yaml
name: Database Migration
description: Execute database schema changes in order

jobs:
- name: Run Migration Steps
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
      get: "/schema/version"
    test: res.body.version == "2.1.0"

  - name: Update Documentation
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

jobs:
- name: User Service Health
  id: user-service
  steps:
  - name: Check User API
    uses: http
    with:
      get: "/health"
    test: res.code == 200

- name: Payment Service Health
  id: payment-service
  steps:
  - name: Check Payment API
    uses: http
    with:
      get: "/health"
    test: res.code == 200

- name: Notification Service Health
  id: notification-service
  steps:
  - name: Check Notification API
    uses: http
    with:
      get: "/health"
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

jobs:
# ステージ 1: インフラチェック（並列）
- name: Database Connectivity
  id: database-check
  steps:
  - name: Test Database Connection
    uses: http
    with:
      get: "/health"
    test: res.code == 200

- name: Cache Service Check
  id: cache-check
  steps:
  - name: Test Redis Connection
    uses: http
    with:
      get: "/health"
    test: res.code == 200

# ステージ 2: アプリケーションチェック（インフラに依存）
- name: API Service Validation
  id: api-validation
  needs: [database-check, cache-check]
  steps:
  - name: Test Core API Endpoints
    uses: http
    with:
      get: "/health"
    test: res.code == 200

# ステージ 3: エンドツーエンドテスト（API に依存）
- name: End-to-End Tests
  id: e2e-tests
  needs: [api-validation]
  steps:
  - name: Test User Registration Flow
    uses: http
    with:
      post: "/auth/register"
      body:
        email: "test@example.com"
        password: "testpass123"
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
- name: US East Region Check
  id: us-east-check
  steps:
  - name: Check US East API
    uses: http
    with:
      method: GET
      url: https://us-east.api.example.com/health
    test: res.code == 200
    outputs:
      region: "us-east"
      status: res.body.status
      response_time: (rt.sec * 1000)

- name: US West Region Check
  id: us-west-check
  steps:
  - name: Check US West API
    uses: http
    with:
      method: GET
      url: https://us-west.api.example.com/health
    test: res.code == 200
    outputs:
      region: "us-west"
      status: res.body.status
      response_time: (rt.sec * 1000)

- name: Europe Region Check
  id: eu-check
  steps:
  - name: Check EU API
    uses: http
    with:
      method: GET
      url: https://eu.api.example.com/health
    test: res.code == 200
    outputs:
      region: "eu"
      status: res.body.status
      response_time: (rt.sec * 1000)

# ファンイン: 結果を集約
- name: Regional Summary
  id: summary
  needs: [us-east-check, us-west-check, eu-check]
  steps:
  - name: Generate Report
    echo: |
      Regional Health Check Results:
      
      US East: {{outputs['us-east-check'].status}} ({{outputs['us-east-check'].response_time}}ms)
      US West: {{outputs['us-west-check'].status}} ({{outputs['us-west-check'].response_time}}ms)
      Europe: {{outputs['eu-check'].status}} ({{outputs['eu-check'].response_time}}ms)
      
      Total regions healthy: {{
        (outputs['us-east-check'].status == "healthy" ? 1 : 0) +
        (outputs['us-west-check'].status == "healthy" ? 1 : 0) +
        (outputs['eu-check'].status == "healthy" ? 1 : 0)
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
```yaml
name: Service Monitoring
description: Monitor critical services

jobs:
- name: API Check
  id: api-check
  defaults:
    http:
  steps:
  - name: Check API Health
    uses: http
    with:
      method: GET
      url: "{{vars.API_BASE_URL}}/health"
    test: res.code == 200
```

**production.yml:**
```yaml
vars:
  API_BASE_URL: https://api.production.example.com
  DEFAULT_TIMEOUT: 10s
```

**staging.yml:**
```yaml
vars:
  API_BASE_URL: https://api.staging.example.com
  DEFAULT_TIMEOUT: 30s
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
      outputs:
        service_healthy: res.code == 200

- name: Deep Diagnostic
  needs: [health-check]
  skipif: outputs['service-check'].service_healthy
  steps:
    - name: Run Diagnostics
      id: diagnostics
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/diagnostics"
      outputs:
        diagnostics_ok: res.code == 200

- name: Send Alert
  needs: [health-check]
  skipif: outputs['service-check'].service_healthy
  steps:
    - name: Critical Alert
      uses: hello
      echo: "CRITICAL: {{vars.service_url}} is not healthy"
```

ジョブが失敗すると後続がまとめてスキップされるため、ヘルスチェックは`test`で失敗させず、結果をoutputsとして公開しています。


### 2. 動的設定

式を使用して、実行時の条件に応じてワークフローの設定値を決めます。

```yaml
jobs:
- name: Load Testing
  id: load-test
  steps:
  - name: Determine Load Parameters
    id: params
    echo: "Load test configuration determined"
    outputs:
      concurrent_users: "{{vars.LOAD_TEST_USERS || 10}}"
      test_duration: "{{vars.LOAD_TEST_DURATION || 60}}"

  - name: Execute Load Test
    uses: http
    with:
      url: "{{vars.LOAD_TEST_URL}}"
      method: POST
      body: |
        {
          "concurrent_users": {{outputs.params.concurrent_users}},
          "duration_seconds": {{outputs.params.test_duration}}
        }
    test: res.code == 200
```

### 3. ワークフロー構成

複雑なワークフローを再利用可能なコンポーネントに分解します。

**common-setup.yml:**
```yaml
jobs:
- name: Common Setup
  id: setup
  steps:
  - name: Initialize Environment
    echo: "Environment initialized"
    outputs:
      timestamp: "{{unixtime()}}"
      session_id: "{{random_str(8)}}"
```

**main-workflow.yml:**
```yaml
name: Complete System Test
description: Full system validation with common setup

# これは common-setup.yml とマージされる
jobs:
- name: API Tests
  id: api-tests
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

# ジョブ名: 説明的で具体的
jobs:
- name: User Authentication Test
  id: user-authentication-test
  
- name: Database Connectivity Check
  id: database-connectivity-check

# ステップ名: アクション指向
steps:
  - name: Verify User Login Endpoint
  - name: Test Database Connection Pool
  - name: Validate Cache Expiration
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
jobs:
- name: Primary Service Check
  id: primary-check
  steps:
  - name: Check Primary Service
    id: primary
    uses: http
    with:
      method: GET
      url: "{{vars.PRIMARY_SERVICE_URL}}"
    test: res.code == 200

- name: Fallback Service Check
  id: fallback-check
  steps:
  - name: Check Fallback Service
    uses: http
    with:
      method: GET
      url: "{{vars.FALLBACK_SERVICE_URL}}"
    test: res.code == 200

- name: Send Notifications
  id: notification
  needs: [primary-check, fallback-check]
  steps:
  - name: Success Notification
    echo: "Primary service is healthy"
    
  - name: Fallback Notification
    echo: "Primary service down, fallback operational"
    
  - name: Critical Alert
    echo: "CRITICAL: Both primary and fallback services are down"
```

### 4. パフォーマンス考慮事項

最適なパフォーマンスのためワークフローを設計します：

```yaml
# 良い例: 独立操作の並列実行
jobs:
- name: Frontend Check    # これらは並列実行される
  id: frontend-check
- name: Backend Check     # パフォーマンス向上のため
  id: backend-check
- name: Database Check
  id: database-check

# 良い例: 効率的なジョブ依存関係
jobs:
- name: Infrastructure Check    # 基盤チェックを最初に
  id: infrastructure
- name: Application Check       # 次にアプリケーションチェック
  id: application
  needs: [infrastructure]
- name: Integration Test       # 最後に統合テスト
  id: integration
  needs: [application]

# 避ける: 不要な順次依存関係
jobs:
- name: Check A
  id: check-a
- name: Check B
  id: check-b
  needs: [check-a]  # B が実際に A に依存する場合のみ
```

## よくあるアンチパターン

以下の3つの形はワークフローを変更しにくくします。いずれにも単純な直し方があります。

### 1. モノリシックワークフロー

**避ける:**
```yaml
name: Everything Check
jobs:
- name: Massive Job
  id: massive-job
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
