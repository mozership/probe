# 実行モデル

Probeの実行モデルを理解することは、効率的なワークフローを設計し、実行の問題をトラブルシューティングするために重要です。このガイドでは、Probeがワークフローコンポーネントを最初から最後までどのようにスケジュール、実行、管理するかについて詳しく説明します。

## 実行概要

Probeは予測可能で決定論的な方法でワークフローを処理する構造化された実行モデルに従います：

1. **ワークフロー解析**: YAML設定の解析と検証
2. **依存関係解決**: ジョブ依存関係に基づく実行グラフの構築
3. **ジョブスケジューリング**: 依存関係に基づくジョブの実行スケジュール
4. **ステップ実行**: 各ジョブ内でステップを順次実行
5. **状態管理**: 実行状態と結果の追跡
6. **リソースクリーンアップ**: 実行後のリソースクリーンアップ

### 実行階層

何が同時に動くかはこの入れ子で決まります。ジョブは並行に、その中のステップは順に実行されます。

```
Workflow
├── Job 1 (独立)
│   ├── Step 1.1 (順次)
│   ├── Step 1.2 (順次)
│   └── Step 1.3 (順次)
├── Job 2 (独立、Job 1 と並列)
│   ├── Step 2.1 (順次)
│   └── Step 2.2 (順次)
└── Job 3 (Job 1 と Job 2 に依存)
    ├── Step 3.1 (順次)
    └── Step 3.2 (順次)
```

## ジョブ実行モデル

`needs`を指定しないジョブは同時に実行されます。`needs`を加えることで、そのジョブ群が順序を持つグラフになります。ジョブは`needs`のジョブがすべて終わった時点で開始し、実行中の他のジョブの終了は待ちません。

### 独立ジョブ実行

依存関係のないジョブは並列実行されます：

```yaml
name: Parallel Service Check
description: Check multiple services simultaneously

jobs:
- id: database-check
  name: Database Health
  steps:
    - name: Check Database
      uses: http
      with:
        method: GET
        url: "{{vars.DB_URL}}/health"
      test: res.code == 200

- id: api-check
  name: API Health
  steps:
    - name: Check API
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health"
      test: res.code == 200

- id: cache-check
  name: Cache Health
  steps:
    - name: Check Cache
      uses: http
      with:
        method: GET
        url: "{{vars.CACHE_URL}}/health"
      test: res.code == 200
```

**実行タイムライン:**
```
時刻 0: database-check、api-check、cache-check を同時開始
時刻 T: すべてのジョブ完了 (T = 全ジョブの最大実行時間)
```

### 依存ジョブ実行

依存関係を持つジョブは前提ジョブの完了を待ちます：

```yaml
name: Staged Deployment Validation
description: Validate deployment in dependency order

jobs:
- id: infrastructure
  name: Infrastructure Check
  steps:
    - name: Database Connectivity
      id: infrastructure
      uses: http
      with:
        method: GET
        url: "{{vars.DB_URL}}/ping"
      test: res.code == 200
      outputs:
        db_healthy: res.code == 200

- id: services
  name: Service Check
  needs: [infrastructure]
  steps:
    - name: API Service
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health"
      test: res.code == 200

- id: integration
  name: Integration Test
  needs: [services]
  steps:
    - name: End-to-End Test
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/integration-test"
      test: res.code == 200

- id: notification
  name: Send Notification
  needs: [integration]
  steps:
    - name: Notify Success
      uses: hello
      echo: "Deployment validation completed successfully"
```

**実行タイムライン:**
```
時刻 0: infrastructure ジョブ開始
時刻 T1: infrastructure 完了 → services ジョブ開始
時刻 T2: services 完了 → integration ジョブ開始
時刻 T3: integration 完了 → notification ジョブ開始
時刻 T4: notification 完了 → ワークフロー完了
```

### 複雑な依存関係グラフ

ジョブは複数の依存関係を持ち、複雑な実行グラフを形成できます：

```yaml
jobs:
  # 基盤レイヤー (並列)
- id: database-setup
  name: Database Setup
  steps:
    - name: Initialize Database
      id: database-setup
      outputs:
        db_session_id: "{{random_str(16)}}"

- id: cache-setup
  name: Cache Setup
  steps:
    - name: Initialize Cache
      id: cache-setup
      outputs:
        cache_session_id: "{{random_str(16)}}"

# サービスレイヤー (基盤に依存)
- id: user-service
  name: User Service Test
  needs: [database-setup, cache-setup]
  steps:
    - name: Test User Service
      id: user-service
      outputs:
        user_service_ready: true

- id: order-service
  name: Order Service Test
  needs: [database-setup]  # データベースのみ必要
  steps:
    - name: Test Order Service
      id: order-service
      outputs:
        order_service_ready: true

# 統合レイヤー (サービスに依存)
- id: integration-test
  name: Integration Test
  needs: [user-service, order-service]
  steps:
    - name: Test Service Integration
      uses: hello
      echo: "Testing integration between user and order services"

# レポートレイヤー (すべてに依存)
- id: final-report
  name: Final Report
  needs: [integration-test]
  steps:
    - name: Generate Report
      uses: hello
      echo: |
        Execution Report:
        Database Setup: {{outputs['database-setup'] ? "✅" : "❌"}}
        Cache Setup: {{outputs['cache-setup'] ? "✅" : "❌"}}
        User Service: {{outputs['user-service'] ? "✅" : "❌"}}
        Order Service: {{outputs['order-service'] ? "✅" : "❌"}}
```

**実行タイムライン:**
```
時刻 0: database-setup、cache-setup 開始 (並列)
時刻 T1: 両基盤ジョブ完了 → user-service、order-service 開始
時刻 T2: 両サービスジョブ完了 → integration-test 開始
時刻 T3: integration-test 完了 → final-report 開始
時刻 T4: final-report 完了 → ワークフロー完了
```

## ステップ実行モデル

ジョブ内のステップは記述した順に1つずつ実行され、条件によってスキップできます。

### 順次ステップ実行

ジョブ内では、ステップは定義された順序で順次実行されます：

```yaml
jobs:
- id: user-workflow
  name: User Management Workflow
  steps:
    - name: Step 1 - Create User
      id: create
      uses: http
      with:
        url: "{{vars.API_URL}}/users"
        method: POST
        body: '{"name": "Test User", "email": "test@example.com"}'
      test: res.code == 201
      outputs:
        user_id: res.body.user.id

    - name: Step 2 - Verify User
      id: verify
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/{{outputs.create.user_id}}"
      test: res.code == 200
      outputs:
        user_verified: true

    - name: Step 3 - Update User
      id: update
      uses: http
      with:
        url: "{{vars.API_URL}}/users/{{outputs.create.user_id}}"
        method: PUT
        body: '{"name": "Updated User"}'
      test: res.code == 200

    - name: Step 4 - Delete User
      uses: http
      with:
        url: "{{vars.API_URL}}/users/{{outputs.create.user_id}}"
        method: DELETE
      test: res.code == 204

    - name: Step 5 - Confirm Deletion
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/{{outputs.create.user_id}}"
      test: res.code == 404
```

**ステップ実行順序:**
```
Step 1 → Step 2 → Step 3 → Step 4 → Step 5
```

各ステップは前のステップの完了を待ってから開始します。

### 条件付きステップ実行

ステップの実行順序は変わりません。`skipif`は各ステップを実行するかどうかだけを決めます。式から見えるのは`test`と同じコンテキストで、先行ステップの`outputs`も参照できます。

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


## 状態管理

状態とは、実行のある部分が別の部分に残す情報です。ジョブのステータス、ステップの出力、ジョブをまたいだ参照がこれにあたります。

### ジョブ状態追跡

Probeは各ジョブについて包括的な状態情報を追跡します：

```yaml
# 参照可能なジョブ状態:
jobs:
- id: example-job
  name: example-job
  steps:
    - name: Example Step
      uses: hello
      echo: "Job states can be referenced from other jobs"

- id: dependent-job
  name: dependent-job
  needs: [example-job]
  steps:
    - name: Check Job States
      uses: hello
      echo: |
        Job State Information:
          
          
```

### ステップ状態と出力管理

各ステップは状態と出力情報を生成します：

```yaml
steps:
  - name: API Test Step
    id: api-test
    uses: http
    with:
      method: GET
      url: "{{vars.API_URL}}/test"
    test: res.code == 200 && (rt.sec * 1000) < 1000
    outputs:
      response_time: (rt.sec * 1000)
      status_code: res.code
      api_healthy: res.code == 200

  - name: Reference Previous Step
    uses: hello
    echo: |
      Previous Step Information:
      
      
      Step outputs:
      Response time: {{outputs['api-test'].response_time}}ms
      Status code: {{outputs['api-test'].status_code}}
      API healthy: {{outputs['api-test'].api_healthy}}
```

### ジョブ間状態参照

ジョブは他のジョブの状態を参照できます：

```yaml
jobs:
- id: health-check
  name: Health Check
  steps:
    - name: Check Service
      id: health-check
      outputs:
        service_healthy: true

- id: performance-test
  name: Performance Test
  needs: [health-check]
  steps:
    - name: Load Test
      id: performance-test
      outputs:
        avg_response_time: 250

- id: reporting
  name: Generate Report
  needs: [health-check, performance-test]
  steps:
    - name: Status Report
      uses: hello
      echo: |
        System Status Report:
          
        Performance Test: {{
            "⏸️ Skipped"
        }}
          
          "Average Response Time: " + outputs['performance-test'].avg_response_time + "ms" : 
          "Performance data not available"}}
```

## タイミングとパフォーマンス

実行時間を決めるのは3つです。各ジョブが開始する時点、ステップを打ち切るまでの待ち時間、そしてグラフのどこまでを同時に進められるかです。

### 実行タイミング

Probeは複数のレベルでタイミング情報を追跡します：

```yaml
jobs:
- id: timing-example
  name: Timing Example
  steps:
    - name: Quick Operation
      id: quick
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/ping"
      test: res.code == 200
      outputs:
        ping_time: (rt.sec * 1000)

    - name: Slow Operation
      id: slow
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/complex-query"
      test: res.code == 200
      outputs:
        query_time: (rt.sec * 1000)

    - name: Timing Summary
      uses: hello
      echo: |
        Operation Timing:
          
        Quick operation: {{outputs.quick.ping_time}}ms
        Slow operation: {{outputs.slow.query_time}}ms
        Total step time: {{outputs.quick.ping_time + outputs.slow.query_time}}ms
          
        Performance classification:
        Quick: {{outputs.quick.ping_time < 100 ? "Excellent" : (outputs.quick.ping_time < 500 ? "Good" : "Slow")}}
        Slow: {{outputs.slow.query_time < 1000 ? "Fast" : (outputs.slow.query_time < 5000 ? "Acceptable" : "Too Slow")}}
```

### タイムアウト管理

異なるレベルでタイムアウトを設定します：

```yaml
jobs:
- id: timeout-management
  name: Timeout Management Example
  timeout: 300s  # ジョブレベルタイムアウト（5分）
  steps:
    - name: Quick API Call
      uses: http
      timeout: 10s  # ステップレベルタイムアウト
      with:
        method: GET
        url: "{{vars.API_URL}}/quick"
      test: res.code == 200

    - name: Database Query
      uses: http
      timeout: 60s  # 複雑な操作にはより長いタイムアウト
      with:
        method: GET
        url: "{{vars.DB_API}}/complex-query"
      test: res.code == 200

    - name: External Service Call
      uses: http
      timeout: 30s  # 外部サービスは遅い場合がある
      with:
        method: GET
        url: "{{vars.EXTERNAL_API}}/data"
      test: res.code == 200
```

### 並列実行最適化

効果的な並列化によりワークフロー実行を最適化します：

```yaml
name: Optimized Parallel Execution
description: Efficiently organize jobs for maximum parallelism

jobs:
  # ティア1: 独立基盤チェック (すべて並列)
- id: database-check
  name: Database Health
  steps:
    - name: DB Connection Test
      uses: http
      with:
        method: GET
        url: "{{vars.DB_URL}}/ping"
      test: res.code == 200

- id: cache-check
  name: Cache Health
  steps:
    - name: Cache Connection Test
      uses: http
      with:
        method: GET
        url: "{{vars.CACHE_URL}}/ping"
      test: res.code == 200

- id: network-check
  name: Network Connectivity
  steps:
    - name: External API Test
      uses: http
      with:
        method: GET
        url: "{{vars.EXTERNAL_API}}/ping"
      test: res.code == 200

# ティア2: サービスレベルチェック (並列、インフラに依存)
- id: user-service-test
  name: User Service Test
  needs: [database-check, cache-check]
  steps:
    - name: User API Test
      uses: http
      with:
        method: GET
        url: "{{vars.USER_API}}/health"
      test: res.code == 200

- id: order-service-test
  name: Order Service Test
  needs: [database-check]
  steps:
    - name: Order API Test
      uses: http
      with:
        method: GET
        url: "{{vars.ORDER_API}}/health"
      test: res.code == 200

- id: notification-service-test
  name: Notification Service Test
  needs: [network-check]
  steps:
    - name: Notification API Test
      uses: http
      with:
        method: GET
        url: "{{vars.NOTIFICATION_API}}/health"
      test: res.code == 200

# ティア3: 統合テスト (サービスに依存)
- id: user-order-integration
  name: User-Order Integration
  needs: [user-service-test, order-service-test]
  steps:
    - name: Integration Test
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/integration/user-order"
      test: res.code == 200

# ティア4: 最終検証 (統合に依存)
- id: end-to-end-test
  name: End-to-End Test
  needs: [user-order-integration, notification-service-test]
  steps:
    - name: Complete Workflow Test
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/e2e/complete-workflow"
      test: res.code == 200
```

**実行視覚化:**
```
時刻 0-T1: database-check、cache-check、network-check (並列)
時刻 T1-T2: user-service-test、order-service-test、notification-service-test (並列)
時刻 T2-T3: user-order-integration
時刻 T3-T4: end-to-end-test
```

## エラー伝播と回復

ステップの失敗は、そのステップだけでは終わりません。その後の挙動は、失敗がグラフをどう伝わるかと、回復のためにワークフローに何を書いたかで決まります。

### エラー伝播モデル

実行モデルを通してエラーがどのように伝播するかを理解します：

```yaml
jobs:
- id: critical-foundation
  name: Critical Foundation
  steps:
    - name: Critical Check
      uses: http
      with:
        method: GET
        url: "{{vars.CRITICAL_SERVICE}}/health"
      test: res.code == 200
      # ステップが失敗するとジョブは失敗扱いになるが、同じジョブの後続ステップは実行される

- id: dependent-service
  name: Dependent Service
  needs: [critical-foundation]  # 基盤が失敗した場合は実行されない
  steps:
    - name: Service Test
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/test"
      test: res.code == 200

- id: resilient-check
  name: Resilient Check
  # 依存関係なし - 常に実行
  steps:
    - name: Independent Check
      uses: http
      with:
        method: GET
        url: "{{vars.INDEPENDENT_SERVICE}}/health"
      test: res.code == 200

- id: conditional-cleanup
  name: Conditional Cleanup
  needs: [critical-foundation, dependent-service, resilient-check]
  steps:
    - name: Cleanup Failed State
      uses: hello
      echo: |
        Cleaning up after failures:
```

### 回復実行モデル

失敗パターンに基づいて実行される回復ワークフローを実装します：

```yaml
jobs:
- id: primary-workflow
  name: Primary Workflow
  steps:
    - name: Main Process
      id: main
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/main-process"
      test: res.code == 200
      outputs:
        process_successful: res.code == 200

- id: recovery-workflow
  name: Recovery Workflow
  steps:
    - name: Diagnose Failure
      id: diagnose
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/diagnostics"
      test: res.code == 200
      outputs:
        diagnosis: res.body.issue_type

    - name: Automated Recovery
      uses: http
      with:
        url: "{{vars.API_URL}}/recovery/auto"
        method: POST
      test: res.code == 200

    - name: Manual Recovery Alert
      uses: hello
      echo: "🚨 Critical failure detected - manual intervention required"

- id: validation-workflow
  name: Validation Workflow
  needs: [primary-workflow, recovery-workflow]
  steps:
    - name: Validate Final State
      id: validation-workflow
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/validate"
      test: res.code == 200
      outputs:
        system_healthy: res.code == 200

- id: final-report
  name: Final Report
  needs: [validation-workflow]
  steps:
    - name: Execution Summary
      uses: hello
      echo: |
        Workflow Execution Summary:
          
          
        Overall result: {{
          "❌ System failed"
        }}
```

## リソース管理

各アクションは独自のプロセスとして動きます。そのため実行中には、起動と終了の対象となるプロセスが存在します。

### アクションのライフサイクル管理

Probeは、アクションを使うStepごとにそのアクションを起動します：

```yaml
jobs:
- id: multi-action-workflow
  name: Multi Action Workflow
  steps:
    # このステップのために http アクションが起動される
    - name: API Test
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/test"
      test: res.code == 200

    # このステップのために smtp アクションが起動される
    - name: Send Notification
      uses: smtp
      with:
        addr: "{{vars.SMTP_HOST}}:25"
        from: "probe@example.com"
        to: "admin@company.com"
        subject: "Test Completed"
        session: 1
        message: 1
        length: 500
      echo: "API test completed successfully"
    - name: Debug Message
      uses: hello
      with:
        message: "Debug checkpoint reached"

    # http アクションが新しいプロセスで再び起動される
    - name: Follow-up API Test
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/follow-up"
      test: res.code == 200
```

アクションのライフサイクル:
1. Stepがアクションを使うときに、そのアクションのプロセスが起動される
2. Stepはそのプロセスで実行され、結果を受け取るとプロセスは終了される
3. 後のStepが同じアクションを使うと新しいプロセスが起動される。`keep_cookies`のクッキーのようにアクションがJob内で保持するものは、Probeが引き渡す

### メモリとパフォーマンス最適化

Probeは実行をパフォーマンスとリソース使用量に最適化します：

```yaml
jobs:
- id: optimized-workflow
  name: Performance Optimized Workflow
  steps:
    # 効率的: 直接的なプロパティアクセス
    - name: User Data Collection
      id: user-data
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users"
      test: res.code == 200
      outputs:
        user_count: res.body.total_users    # 特定の値を抽出
        first_user_id: res.body.users[0].id # 直接配列アクセス
        # 避ける: large_user_list: res.body.users (配列全体を保存)

    # 効率的: 条件付き処理
    - name: Process Large Dataset
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/batch-process"
      test: res.code == 200

    # 効率的: スコープされた出力
    - name: Summary Generation
      uses: hello
      echo: |
        Processing Summary:
        Total users: {{outputs['user-data'].user_count}}
        First user: {{outputs['user-data'].first_user_id}}
        # 不要なデータを保存せずに効率的な出力
```

## ベストプラクティス

以下は実行モデルから導かれる指針です。依存関係の形、失敗時の挙動、出力に載せる量、そして流れを読み取りやすくする方法を扱います。

### 1. 依存関係設計

依存は、データや順序が実際に必要とする箇所にだけ宣言します。余分な依存はその分だけ並行性を失わせます。

```yaml
# 良い例: 論理的な依存関係グループ化
jobs:
- id: infrastructure
  name: infrastructure
- id: application
  name: application
  needs: [infrastructure]
- id: integration
  name: integration
  needs: [application]

# 避ける: 不要な依存関係
jobs:
- id: independent-check-1
  name: independent-check-1
- id: independent-check-2
  name: independent-check-2
  needs: [independent-check-1]  # 真に独立なら不要
```

### 2. エラーハンドリング戦略

失敗で実行を止めるか、そのまま先へ進めるかをステップごとに決めます。

```yaml
# 良い例: 戦略的エラーハンドリング
- name: Critical Operation
  test: res.code == 200

- name: Optional Operation
  test: res.code == 200
```

### 3. 出力効率

出力に載せるのは後続のステップが読む値だけにし、レスポンス全体は載せません。

```yaml
# 良い例: 効率的な出力
outputs:
  essential_data: res.body.id
  computed_value: len(res.body.items)
  status_flag: res.code == 200

# 避ける: 大きなオブジェクトの保存
outputs:
  # entire_response: res.body  # 非常に大きくなる可能性
```

### 4. 実行フロードキュメント

想定している実行順序は、ワークフローのdescriptionに書けます。

```yaml
name: Well-Documented Workflow
description: |
  実行フロー:
  1. インフラ検証 (並列)
  2. サービスヘルスチェック (並列、インフラに依存)
  3. 統合テスト (順次、サービスに依存)
  4. レポート (全ての前段階に依存)
  
  予想実行時間: 2-5分
  クリティカルパス: infrastructure → services → integration → reporting
```

## 次のステップ

実行モデルを理解したら、以下を探索してください：

1. **[ファイルマージ](/ja/guide/concepts/file-merging)** - 設定構成技術を学ぶ
2. **[ハウツー](/ja/guide/how-tos/api-testing)** - 実用的な実行パターンの実例を見る
3. **[リファレンス](/ja/reference/actions/variables)** - 詳細な構文と設定リファレンス

実行モデルを理解することで、並列処理を最適に活用し、失敗を適切に処理する効率的で予測可能なワークフローを設計できるようになります。
