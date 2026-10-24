# エラーハンドリング戦略

このガイドでは、Probeワークフローで堅牢なエラーハンドリングを実装する方法を説明します。失敗を適切に処理し、回復パターンを実装し、予期しない状況に対処できる耐性のある自動化を構築する方法を学びます。

## 基本的なエラーハンドリングパターン

最初に決めるのは、失敗したときに残りの実行をどうするかです。そこで止めるか、結果を縮退させたうえで続けるかを選びます。

### フェイルファーストvs続行

操作の重要度に基づいて適切なエラーハンドリング戦略を選択します：

```yaml
name: Error Handling Strategy Examples
description: 異なるエラーハンドリングアプローチを実演

vars:
  CRITICAL_SERVICE_URL: https://critical.yourcompany.com
  OPTIONAL_SERVICE_URL: https://optional.yourcompany.com
  NOTIFICATION_URL: https://notifications.yourcompany.com

jobs:
- id: critical-operations
  name: Critical Operations
  # 重要な操作では高速失敗
  fail_fast: true
  steps:
    - name: Critical Database Check
      uses: http
      with:
        method: GET
        url: "{{vars.CRITICAL_SERVICE_URL}}/database/health"
      test: res.code == 200
      outputs:
        database_healthy: res.code == 200

    # このステップはデータベースチェックが成功した場合のみ実行
    - name: Critical API Check
      uses: http
      with:
        method: GET
        url: "{{vars.CRITICAL_SERVICE_URL}}/api/health"
      test: res.code == 200
      outputs:
        api_healthy: res.code == 200

- id: resilient-operations
  name: Resilient Operations
  steps:
    # オプショナルサービスではエラー時に続行
    - name: Optional Analytics Service
      uses: http
      with:
        method: GET
        url: "{{vars.OPTIONAL_SERVICE_URL}}/analytics"
      test: res.code == 200
      outputs:
        analytics_available: res.code == 200
        analytics_error: 'res.code != 200 ? res.status : nil'

    # このステップは前のステップに関係なく常に実行
    - name: Optional Notification Service
      uses: http
      with:
        method: GET
        url: "{{vars.NOTIFICATION_URL}}/health"
      test: res.code == 200
      outputs:
        notifications_available: res.code == 200

    # サービス可用性に基づく条件付きロジック
    # リクエストが届かなかったステップは outputs を公開しないため、
    # それぞれを既定値付きで読む
    - name: Service Availability Report
      uses: hello
      echo: |
        🔧 Service Availability Report:

        Analytics Service: {{(outputs.analytics_available ?? false) ? "✅ Available" : "❌ Unavailable"}}
        {{outputs.analytics_error != nil ? "Error: " + outputs.analytics_error : ""}}

        Notification Service: {{(outputs.notifications_available ?? false) ? "✅ Available" : "❌ Unavailable"}}

        Impact Assessment:
        {{!(outputs.analytics_available ?? false) ? "• Analytics features may be limited" : ""}}
        {{!(outputs.notifications_available ?? false) ? "• User notifications may be delayed" : ""}}
        {{(outputs.analytics_available ?? false) && (outputs.notifications_available ?? false) ? "• All optional services operational" : ""}}
```

### グレースフルデグラデーション

プライマリサービスが失敗した際のフォールバックメカニズムを実装します：

```yaml
name: Graceful Degradation Pattern
description: フォールバックサービスとグレースフルデグラデーションを実装

vars:
  PRIMARY_API_URL: https://primary.api.yourcompany.com
  SECONDARY_API_URL: https://secondary.api.yourcompany.com
  CACHE_API_URL: https://cache.yourcompany.com
  FALLBACK_API_URL: https://fallback.api.yourcompany.com

jobs:
- id: service-with-fallbacks
  name: Service with Multiple Fallbacks
  steps:
    # まずプライマリサービスを試行
    - name: Primary Service Attempt
      id: primary
      uses: http
      timeout: 10s
      with:
        method: GET
        url: "{{vars.PRIMARY_API_URL}}/data"
      test: res.code == 200 && (rt.sec * 1000) < 5000
      outputs:
        success: res.code == 200 && (rt.sec * 1000) < 5000
        response_time: (rt.sec * 1000)
        data: res.body

    # プライマリが失敗または遅い場合はセカンダリサービスを試行
    - name: Secondary Service Attempt
      id: secondary
      uses: http
      timeout: 15s
      skipif: outputs.primary?.success ?? false
      with:
        method: GET
        url: "{{vars.SECONDARY_API_URL}}/data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        response_time: (rt.sec * 1000)
        data: res.body

    # プライマリとセカンダリの両方が失敗した場合はキャッシュを試行
    - name: Cache Fallback
      id: cache
      uses: http
      timeout: 5s
      skipif: (outputs.primary?.success ?? false) || (outputs.secondary?.success ?? false)
      with:
        method: GET
        url: "{{vars.CACHE_API_URL}}/cached-data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        response_time: (rt.sec * 1000)
        data: res.body
        cached_data: true

    # 静的データへの最終フォールバック
    - name: Static Fallback
      id: fallback
      uses: http
      skipif: (outputs.primary?.success ?? false) || (outputs.secondary?.success ?? false) || (outputs.cache?.success ?? false)
      with:
        method: GET
        url: "{{vars.FALLBACK_API_URL}}/static-data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        response_time: (rt.sec * 1000)
        data: res.body
        static_data: true

    # スキップされたステップと、リクエストが届かなかったステップは
    # outputs を公開しないため、その outputs.<id> は nil になる
    - name: Service Resolution Summary
      uses: hello
      echo: |
        🎯 Service Resolution Summary:

        Resolution Path:
        {{(outputs.primary?.success ?? false) ? "✅ Primary Service (optimal)" : "❌ Primary Service failed/slow (" + (outputs.primary != nil ? string(int(outputs.primary.response_time)) + "ms" : "no response") + ")"}}
        {{outputs.secondary == nil ? "" : outputs.secondary.success ? "✅ Secondary Service (backup)" : "❌ Secondary Service failed"}}
        {{outputs.cache == nil ? "" : outputs.cache.success ? "✅ Cache Service (degraded)" : "❌ Cache Service failed"}}
        {{outputs.fallback == nil ? "" : outputs.fallback.success ? "✅ Static Fallback (minimal)" : "❌ All services failed"}}

        Final Status: {{
          (outputs.primary?.success ?? false) ? "🟢 Optimal Performance" :
          (outputs.secondary?.success ?? false) ? "🟡 Backup Service Active" :
          (outputs.cache?.success ?? false) ? "🟠 Degraded Mode (cached data)" :
          (outputs.fallback?.success ?? false) ? "🔴 Minimal Functionality (static data)" :
          "🚨 Total Service Failure"
        }}

        Data Source: {{
          (outputs.primary?.success ?? false) ? "Live Primary" :
          (outputs.secondary?.success ?? false) ? "Live Secondary" :
          (outputs.cache?.success ?? false) ? "Cached (may be stale)" :
          (outputs.fallback?.success ?? false) ? "Static Fallback" :
          "None Available"
        }}
```

## リトライパターン

リトライが有効なのは失敗が一時的な場合だけです。試行間隔を空け、失敗が続いたら打ち切ることで、状況を悪化させずに済みます。

### 指数バックオフリトライ

遅延を増加させるリトライロジックを実装します：

```yaml
name: Retry with Exponential Backoff
description: 一時的な失敗に対するリトライパターンを実装

vars:
  UNRELIABLE_SERVICE_URL: https://api.unreliable.service.com
  MAX_RETRIES: 3

jobs:
- id: retry-pattern
  name: Exponential Backoff Retry Pattern
  steps:
    # 最初の試行
    - name: Initial Attempt
      id: attempt1
      uses: http
      timeout: 10s
      with:
        method: GET
        url: "{{vars.UNRELIABLE_SERVICE_URL}}/data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        attempt_number: 1
        response_time: (rt.sec * 1000)
        error_code: 'res.code != 200 ? res.code : nil'

    # 2回目の試行（2秒遅延）。それまでの試行が成功していればスキップ
    - name: Retry Attempt 1 (2s delay)
      id: attempt2
      uses: http
      wait: 2s
      skipif: outputs.attempt1?.success ?? false
      timeout: 15s
      with:
        method: GET
        url: "{{vars.UNRELIABLE_SERVICE_URL}}/data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        attempt_number: 2
        response_time: (rt.sec * 1000)
        error_code: 'res.code != 200 ? res.code : nil'

    # 3回目の試行（4秒遅延）
    - name: Retry Attempt 2 (4s delay)
      id: attempt3
      uses: http
      wait: 4s
      skipif: (outputs.attempt1?.success ?? false) || (outputs.attempt2?.success ?? false)
      timeout: 20s
      with:
        method: GET
        url: "{{vars.UNRELIABLE_SERVICE_URL}}/data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        attempt_number: 3
        response_time: (rt.sec * 1000)
        error_code: 'res.code != 200 ? res.code : nil'

    # 最終試行（8秒遅延）
    - name: Final Attempt (8s delay)
      id: attempt4
      uses: http
      wait: 8s
      skipif: (outputs.attempt1?.success ?? false) || (outputs.attempt2?.success ?? false) || (outputs.attempt3?.success ?? false)
      timeout: 30s
      with:
        method: GET
        url: "{{vars.UNRELIABLE_SERVICE_URL}}/data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        attempt_number: 4
        response_time: (rt.sec * 1000)
        error_code: 'res.code != 200 ? res.code : nil'

    # スキップされた試行と、リクエストが届かなかった試行は
    # outputs を公開しないため、その outputs.<id> は nil になる
    - name: Retry Summary
      uses: hello
      echo: |
        🔄 Retry Pattern Results:

        Attempt History:
        1. Initial: {{outputs.attempt1 == nil ? "❌ No response" : outputs.attempt1.success ? "✅ Success (" + string(int(outputs.attempt1.response_time)) + "ms)" : "❌ Failed (HTTP " + string(outputs.attempt1.error_code) + ")"}}
        {{outputs.attempt2 == nil ? "" : "2. Retry 1: " + (outputs.attempt2.success ? "✅ Success (" + string(int(outputs.attempt2.response_time)) + "ms)" : "❌ Failed (HTTP " + string(outputs.attempt2.error_code) + ")")}}
        {{outputs.attempt3 == nil ? "" : "3. Retry 2: " + (outputs.attempt3.success ? "✅ Success (" + string(int(outputs.attempt3.response_time)) + "ms)" : "❌ Failed (HTTP " + string(outputs.attempt3.error_code) + ")")}}
        {{outputs.attempt4 == nil ? "" : "4. Final: " + (outputs.attempt4.success ? "✅ Success (" + string(int(outputs.attempt4.response_time)) + "ms)" : "❌ Failed (HTTP " + string(outputs.attempt4.error_code) + ")")}}

        Final Result: {{
          (outputs.attempt1?.success ?? false) ? "✅ Success on first attempt" :
          (outputs.attempt2?.success ?? false) ? "✅ Success on retry 1" :
          (outputs.attempt3?.success ?? false) ? "✅ Success on retry 2" :
          (outputs.attempt4?.success ?? false) ? "✅ Success on final attempt" :
          "❌ All attempts failed"
        }}

        {{
          (outputs.attempt1?.success ?? false) ? "" :
          (outputs.attempt2?.success ?? false) ? "Service recovered after transient failure" :
          (outputs.attempt3?.success ?? false) ? "Service required multiple retries" :
          (outputs.attempt4?.success ?? false) ? "Service barely recoverable" :
          "Service appears to be down"
        }}
```

### サーキットブレーカーパターン

障害の連鎖を防ぐためのサーキットブレーカーを実装します：

```yaml
name: Circuit Breaker Pattern
description: 障害の分離のためのサーキットブレーカーを実装

vars:
  MONITORED_SERVICE_URL: https://api.monitored.service.com
  MONITORING_API_URL: "{{MONITORING_API_URL}}"
  SERVICE_NAME: "{{SERVICE_NAME}}"
  CIRCUIT_BREAKER_THRESHOLD: 5
  CIRCUIT_RECOVERY_TIME: 300  # 5分

jobs:
- id: circuit-breaker-check
  name: Circuit Breaker Health Check
  steps:
    # 現在のサーキットブレーカー状態をチェック
    - name: Check Circuit Breaker Status
      id: circuit-status
      uses: http
      with:
        method: GET
        url: "{{vars.MONITORING_API_URL}}/circuit-breaker/{{vars.SERVICE_NAME}}"
      test: res.code == 200
      outputs:
        circuit_state: res.body.state
        failure_count: res.body.failure_count
        last_failure_time: res.body.last_failure_time
        last_success_time: res.body.last_success_time

    # サーキットブレーカー状態を評価
    - name: Circuit Breaker Decision
      uses: hello
      id: decision
      echo: "Evaluating circuit breaker state"
      outputs:
        # 最近の失敗が多すぎる場合はサーキットを開く
        circuit_open: outputs['circuit-status'].failure_count >= int(vars.CIRCUIT_BREAKER_THRESHOLD)
        # サーキットが十分な時間開いていればプローブを許可
        time_since_failure: unixtime() - outputs['circuit-status'].last_failure_time
        should_probe: (unixtime() - outputs['circuit-status'].last_failure_time) > int(vars.CIRCUIT_RECOVERY_TIME)

- id: service-test
  name: Service Test with Circuit Breaker
  needs: [circuit-breaker-check]
  steps:
    # サーキットが閉じている場合の通常動作
    - name: Normal Service Test
      id: normal-test
      uses: http
      timeout: 10s
      skipif: outputs.decision.circuit_open
      with:
        method: GET
        url: "{{vars.MONITORED_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        test_successful: res.code == 200
        response_time: (rt.sec * 1000)
        error_code: 'res.code != 200 ? res.code : nil'

    # サーキットが開いているが回復時間が経過した場合のプローブテスト
    - name: Circuit Recovery Probe
      id: probe-test
      uses: http
      timeout: 5s
      skipif: '!outputs.decision.circuit_open || !outputs.decision.should_probe'
      with:
        method: GET
        url: "{{vars.MONITORED_SERVICE_URL}}/ping"  # より軽いプローブ
      test: res.code == 200
      outputs:
        probe_successful: res.code == 200
        response_time: (rt.sec * 1000)

    # サーキットブレーカー状態を更新（サーキットが開いていてプローブの時期でない場合を除く）
    # リクエストが届かなかったテストは outputs を公開せず、失敗として扱う
    - name: Update Circuit Breaker
      uses: http
      skipif: outputs.decision.circuit_open && !outputs.decision.should_probe
      with:
        url: "{{vars.MONITORING_API_URL}}/circuit-breaker/{{vars.SERVICE_NAME}}/update"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "test_result": {{
              outputs['normal-test']?.test_successful ??
              outputs['probe-test']?.probe_successful ?? false
            }},
            "response_time": {{
              outputs['normal-test']?.response_time ??
              outputs['probe-test']?.response_time ?? "null"
            }},
            "timestamp": {{unixtime()}}
          }
      test: res.code == 200

    - name: Circuit Breaker Status Report
      uses: hello
      echo: |
        ⚡ Circuit Breaker Status Report:

        Previous State:
        Circuit State: {{outputs['circuit-status'].circuit_state}}
        Failure Count: {{outputs['circuit-status'].failure_count}}
        Time Since Last Failure: {{outputs.decision.time_since_failure}} seconds

        Current Test:
        {{outputs['normal-test'] != nil ? "Normal Test: " + (outputs['normal-test'].test_successful ? "✅ Passed" : "❌ Failed (HTTP " + string(outputs['normal-test'].error_code) + ")") : ""}}
        {{outputs['probe-test'] != nil ? "Recovery Probe: " + (outputs['probe-test'].probe_successful ? "✅ Passed" : "❌ Failed") : ""}}
        {{outputs.decision.circuit_open && !outputs.decision.should_probe ? "⏸️ Circuit Open - Skipping test (recovery time not reached)" : ""}}

        Circuit Action: {{
          (outputs['normal-test']?.test_successful ?? false) ? "✅ Circuit remains closed" :
          (outputs['probe-test']?.probe_successful ?? false) ? "🟢 Circuit should close (service recovered)" :
          outputs.decision.circuit_open && outputs.decision.should_probe ? "🔴 Circuit remains open (service still failing)" :
          !outputs.decision.circuit_open ? "🔴 Circuit should open (service failing)" :
          "⏸️ No test performed"
        }}
```

## エラー回復戦略

ワークフローにできるのは失敗の報告だけではありません。復旧を試み、サービスが戻ったことを確認するところまで行えます。

### セルフヒーリングワークフロー

異常なサービスを再起動やスケールアップで自動的に復旧させるワークフローを実装します：

```yaml
name: Self-Healing Service Monitor
description: サービスを監視し、自動的に回復を試行

vars:
  SERVICE_NAME: user-service
  SERVICE_HEALTH_URL: https://user-service.yourcompany.com/health
  ADMIN_API_URL: https://admin.yourcompany.com/api
  RECOVERY_ATTEMPTS: 3
  SMTP_HOST: "{{SMTP_HOST}}"
  ENVIRONMENT: "{{ENVIRONMENT}}"
  DASHBOARD_URL: "{{DASHBOARD_URL}}"
  RUNBOOK_URL: "{{RUNBOOK_URL}}"

jobs:
- id: health-monitoring
  name: Health Monitoring and Recovery
  steps:
    # ステップ1: サービスヘルスチェック
    - name: Service Health Check
      id: health-check
      uses: http
      timeout: 30s
      with:
        method: GET
        url: "{{vars.SERVICE_HEALTH_URL}}"
      test: res.code == 200
      outputs:
        healthy: res.code == 200
        status_code: res.code
        response_time: (rt.sec * 1000)
        error_details: 'res.code != 200 ? (res.rawbody ?? res.body) : nil'

    # ステップ2: 異常な場合の詳細診断
    - name: Service Diagnostics
      id: diagnostics
      uses: http
      timeout: 45s
      skipif: outputs['health-check']?.healthy ?? false
      with:
        method: GET
        url: "{{vars.SERVICE_HEALTH_URL}}/diagnostics"
      test: res.code == 200
      outputs:
        diagnostics_available: res.code == 200
        memory_usage: res.body.memory_usage_percent
        cpu_usage: res.body.cpu_usage_percent
        active_connections: res.body.active_connections
        error_rate: res.body.error_rate_1min

# サービスが異常だとヘルスチェックが失敗し、それを needs に持つジョブはスキップされる。
# そのため always で実行し、不要なときは skipif で自らスキップする
- id: automated-recovery
  name: Automated Recovery Procedures
  needs: [health-monitoring]
  always: true
  skipif: outputs['health-check']?.healthy ?? false
  steps:
    # 回復試行1: グレースフル再起動
    - name: Graceful Service Restart
      id: restart-attempt-1
      uses: http
      with:
        url: "{{vars.ADMIN_API_URL}}/services/{{vars.SERVICE_NAME}}/restart"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "restart_type": "graceful",
            "drain_connections": true,
            "timeout_seconds": 60
          }
      test: res.code == 200
      outputs:
        restart_initiated: res.code == 200
        restart_id: res.body.restart_id

    # 最初の再起動を待機・検証
    - name: Verify Graceful Restart
      id: verify-restart
      uses: http
      wait: 30s
      skipif: '!(outputs["restart-attempt-1"]?.restart_initiated ?? false)'
      timeout: 60s
      with:
        method: GET
        url: "{{vars.SERVICE_HEALTH_URL}}"
      test: res.code == 200
      outputs:
        restart_successful: res.code == 200

    # 回復試行2: グレースフルが失敗した場合は強制再起動
    - name: Force Service Restart
      id: restart-attempt-2
      uses: http
      skipif: outputs['verify-restart']?.restart_successful ?? false
      with:
        url: "{{vars.ADMIN_API_URL}}/services/{{vars.SERVICE_NAME}}/restart"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "restart_type": "force",
            "timeout_seconds": 30
          }
      test: res.code == 200
      outputs:
        force_restart_initiated: res.code == 200

    # 強制再起動を検証
    - name: Verify Force Restart
      id: verify-force-restart
      uses: http
      wait: 30s
      skipif: '!(outputs["restart-attempt-2"]?.force_restart_initiated ?? false)'
      timeout: 60s
      with:
        method: GET
        url: "{{vars.SERVICE_HEALTH_URL}}"
      test: res.code == 200
      outputs:
        force_restart_successful: res.code == 200

    # 回復試行3: 新しいインスタンスをスケールアップ
    - name: Scale Up Service
      id: scale-up
      uses: http
      skipif: (outputs['verify-restart']?.restart_successful ?? false) || (outputs['verify-force-restart']?.force_restart_successful ?? false)
      with:
        url: "{{vars.ADMIN_API_URL}}/services/{{vars.SERVICE_NAME}}/scale"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "action": "scale_up",
            "additional_instances": 2,
            "health_check_grace_period": 120
          }
      test: res.code == 200
      outputs:
        scale_up_initiated: res.code == 200

    # 最終ヘルスチェック
    - name: Final Health Verification
      id: final-check
      uses: http
      timeout: 120s
      with:
        method: GET
        url: "{{vars.SERVICE_HEALTH_URL}}"
      test: res.code == 200
      outputs:
        final_health_status: res.code == 200

- id: recovery-reporting
  name: Recovery Status Reporting
  needs: [health-monitoring, automated-recovery]
  always: true
  steps:
    - name: Recovery Status Report
      uses: hello
      echo: |
        🏥 Service Recovery Report for {{vars.SERVICE_NAME}}:
        ================================================

        INITIAL HEALTH CHECK:
        Status: {{(outputs['health-check']?.healthy ?? false) ? "✅ Healthy" : "❌ Unhealthy (HTTP " + string(outputs['health-check']?.status_code ?? "no response") + ")"}}
        Response Time: {{outputs['health-check']?.response_time ?? "-"}}ms
        {{outputs['health-check']?.error_details != nil ? "Error Details: " + outputs['health-check'].error_details : ""}}

        {{(outputs.diagnostics?.diagnostics_available ?? false) ? "DIAGNOSTICS:" : ""}}
        {{(outputs.diagnostics?.diagnostics_available ?? false) ? "Memory Usage: " + string(outputs.diagnostics.memory_usage) + "%" : ""}}
        {{(outputs.diagnostics?.diagnostics_available ?? false) ? "CPU Usage: " + string(outputs.diagnostics.cpu_usage) + "%" : ""}}
        {{(outputs.diagnostics?.diagnostics_available ?? false) ? "Active Connections: " + string(outputs.diagnostics.active_connections) : ""}}
        {{(outputs.diagnostics?.diagnostics_available ?? false) ? "Error Rate: " + string(outputs.diagnostics.error_rate) + "/min" : ""}}

        RECOVERY ACTIONS:
        {{(outputs['restart-attempt-1']?.restart_initiated ?? false) ? "1. Graceful Restart: " + ((outputs['verify-restart']?.restart_successful ?? false) ? "✅ Successful" : "❌ Failed") : "1. Graceful Restart: ⏸️ Not attempted"}}
        {{(outputs['restart-attempt-2']?.force_restart_initiated ?? false) ? "2. Force Restart: " + ((outputs['verify-force-restart']?.force_restart_successful ?? false) ? "✅ Successful" : "❌ Failed") : "2. Force Restart: ⏸️ Not attempted"}}
        {{(outputs['scale-up']?.scale_up_initiated ?? false) ? "3. Scale Up: ✅ Initiated" : "3. Scale Up: ⏸️ Not attempted"}}

        FINAL STATUS:
        Service Health: {{(outputs['final-check']?.final_health_status ?? false) ? "✅ Healthy" : "❌ Still Unhealthy"}}

        RECOVERY RESULT: {{
          (outputs['health-check']?.healthy ?? false) ? "ℹ️ No recovery needed - service was healthy" :
          (outputs['verify-restart']?.restart_successful ?? false) ? "🟢 Recovered via graceful restart" :
          (outputs['verify-force-restart']?.force_restart_successful ?? false) ? "🟡 Recovered via force restart" :
          (outputs['final-check']?.final_health_status ?? false) ? "🟢 Recovered via scaling" :
          "🔴 Recovery failed - manual intervention required"
        }}

        {{!(outputs['final-check']?.final_health_status ?? false) && !(outputs['health-check']?.healthy ?? false) ? "🚨 ALERT: Service recovery failed - escalating to on-call team" : ""}}

    # 回復が失敗した場合のエスカレーション通知
    - name: Escalation Alert
      uses: smtp
      skipif: (outputs['health-check']?.healthy ?? false) || (outputs['final-check']?.final_health_status ?? false)
      with:
        addr: "{{vars.SMTP_HOST}}:587"
        from: "alerts@yourcompany.com"
        to: "oncall@yourcompany.com"
        subject: "🚨 CRITICAL: Service Recovery Failed - {{vars.SERVICE_NAME}}"
        session: 1
        message: 1
        length: 500
      test: res.code == 0 && res.sent > 0
      echo: |
        CRITICAL SERVICE RECOVERY FAILURE
        =================================

        Service: {{vars.SERVICE_NAME}}
        Time: {{unixtime()}}
        Environment: {{vars.ENVIRONMENT}}

        Initial Problem:
        - Health Check: Failed (HTTP {{outputs['health-check']?.status_code ?? "no response"}})
        - Response Time: {{outputs['health-check']?.response_time ?? "-"}}ms

        Recovery Attempts:
        {{(outputs['restart-attempt-1']?.restart_initiated ?? false) ? "- Graceful Restart: " + ((outputs['verify-restart']?.restart_successful ?? false) ? "Success" : "Failed") : "- Graceful Restart: Not attempted"}}
        {{(outputs['restart-attempt-2']?.force_restart_initiated ?? false) ? "- Force Restart: " + ((outputs['verify-force-restart']?.force_restart_successful ?? false) ? "Success" : "Failed") : "- Force Restart: Not attempted"}}
        {{(outputs['scale-up']?.scale_up_initiated ?? false) ? "- Scale Up: Initiated" : "- Scale Up: Not attempted"}}

        Current Status: Service remains unhealthy

        MANUAL INTERVENTION REQUIRED

        Please investigate immediately:
        1. Check service logs
        2. Verify infrastructure status
        3. Consider emergency rollback
        4. Update incident status

        Dashboard: {{vars.DASHBOARD_URL}}
        Runbook: {{vars.RUNBOOK_URL}}
```

## 包括的なエラーコンテキスト

失敗した時点で何を集めておくかによって、あとからその報告を手がかりにできるかどうかが決まります。

### エラー情報収集

デバッグのための包括的なエラー情報を収集します：

```yaml
name: Comprehensive Error Context Collection
description: 効果的なデバッグのための詳細なエラー情報を収集

vars:
  API_BASE_URL: https://api.yourservice.com
  API_TOKEN: "{{API_TOKEN}}"
  TEST_USER_ID: "{{TEST_USER_ID}}"
  ENVIRONMENT: "{{ENVIRONMENT}}"
  CORRELATION_ID: "{{random_str(32)}}"

jobs:
- id: error-context-collection
  name: Error Context Collection
  steps:
    - name: API Test with Error Context
      id: api-test
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/complex-operation"
        method: POST
        headers:
          Content-Type: "application/json"
          X-Correlation-ID: "{{vars.CORRELATION_ID}}"
          Authorization: "Bearer {{vars.API_TOKEN}}"
        body: |
          {
            "operation": "test_operation",
            "parameters": {
              "user_id": {{vars.TEST_USER_ID}},
              "data_size": "large",
              "timeout": 30
            },
            "metadata": {
              "test_run_id": "{{vars.CORRELATION_ID}}",
              "timestamp": {{unixtime()}}
            }
          }
      test: res.code == 200 && res.body.success == true
      outputs:
        # 成功指標
        operation_successful: res.code == 200 && res.body.success == true

        # レスポンスメタデータ
        status_code: res.code
        response_time: (rt.sec * 1000)
        response_size: len(res.rawbody ?? res.body)
        content_type: res.headers["Content-Type"]

        # エラーコンテキスト（失敗時のみ入力）
        error_message: 'res.code != 200 ? res.body.error?.message : nil'
        error_code: 'res.code != 200 ? res.body.error?.code : nil'
        error_details: 'res.code != 200 ? res.body.error?.details : nil'
        trace_id: res.headers["X-Trace-Id"]
        request_id: res.headers["X-Request-Id"]

        # パフォーマンスコンテキスト
        server_response_time: res.headers["X-Response-Time"]
        database_time: res.body.debug?.database_time_ms
        cache_hit: res.body.debug?.cache_hit

        # ビジネスコンテキスト
        affected_user: res.body.error?.affected_user
        operation_id: res.body.operation_id
        retry_after: res.headers["Retry-After"]

    # リクエストに成功以外のレスポンスが返った場合のみ実行
    - name: Error Analysis and Enrichment
      uses: hello
      id: error-analysis
      skipif: outputs['api-test']?.operation_successful ?? true
      echo: "Analyzing error context"
      outputs:
        # エラータイプを分類
        error_category: |
          outputs['api-test'].status_code >= 500 ? "server_error" :
            outputs['api-test'].status_code == 429 ? "rate_limit" :
            outputs['api-test'].status_code >= 400 && outputs['api-test'].status_code < 500 ? "client_error" :
            "unknown"

        # 深刻度を決定
        severity_level: |
          outputs['api-test'].status_code >= 500 ? "high" :
            outputs['api-test'].status_code == 429 ? "medium" :
            outputs['api-test'].status_code >= 400 && outputs['api-test'].status_code < 500 ? "low" :
            "critical"

        # トラブルシューティングヒントを生成
        troubleshooting_hints: |
          outputs['api-test'].status_code == 401 ? "Check authentication token expiry and permissions" :
            outputs['api-test'].status_code == 403 ? "Verify user has required permissions for this operation" :
            outputs['api-test'].status_code == 404 ? "Confirm API endpoint exists and user/resource exists" :
            outputs['api-test'].status_code == 409 ? "Resource conflict - check for duplicate operations" :
            outputs['api-test'].status_code == 429 ? "Rate limit exceeded - implement backoff or check quota" :
            outputs['api-test'].status_code >= 500 ? "Server error - check application logs and infrastructure" :
            "Unexpected response - compare it with the API contract"

        # デバッグ用のコンテキスト
        debug_context: |
          "Correlation ID: " + vars.CORRELATION_ID + "\n" +
          "Test User ID: " + vars.TEST_USER_ID + "\n" +
          "Request Timestamp: " + string(unixtime()) + "\n" +
          "Environment: " + vars.ENVIRONMENT

    - name: Detailed Error Report
      uses: hello
      skipif: outputs['error-analysis'] == nil
      echo: |
        🔍 Comprehensive Error Analysis Report
        =====================================

        ERROR OVERVIEW:
        Correlation ID: {{vars.CORRELATION_ID}}
        Error Category: {{outputs['error-analysis'].error_category}}
        Severity Level: {{outputs['error-analysis'].severity_level}}
        Timestamp: {{unixtime()}}

        REQUEST DETAILS:
        URL: {{vars.API_BASE_URL}}/complex-operation
        Method: POST
        User ID: {{vars.TEST_USER_ID}}
        Content Type: {{outputs['api-test'].content_type}}

        RESPONSE DETAILS:
        Status Code: {{outputs['api-test'].status_code}}
        Response Time: {{outputs['api-test'].response_time}}ms
        Response Size: {{outputs['api-test'].response_size}} bytes
        Server Response Time: {{outputs['api-test'].server_response_time}}ms

        ERROR INFORMATION:
        Error Code: {{outputs['api-test'].error_code}}
        Error Message: {{outputs['api-test'].error_message}}
        Error Details: {{outputs['api-test'].error_details}}

        TRACING INFORMATION:
        Trace ID: {{outputs['api-test'].trace_id}}
        Request ID: {{outputs['api-test'].request_id}}
        Operation ID: {{outputs['api-test'].operation_id}}

        PERFORMANCE CONTEXT:
        {{outputs['api-test'].database_time != nil ? "Database Time: " + string(outputs['api-test'].database_time) + "ms" : ""}}
        {{outputs['api-test'].cache_hit != nil ? "Cache Hit: " + string(outputs['api-test'].cache_hit) : ""}}
        {{outputs['api-test'].retry_after != nil ? "Retry After: " + outputs['api-test'].retry_after + " seconds" : ""}}

        BUSINESS CONTEXT:
        {{outputs['api-test'].affected_user != nil ? "Affected User: " + string(outputs['api-test'].affected_user) : ""}}

        TROUBLESHOOTING:
        {{outputs['error-analysis'].troubleshooting_hints}}

        DEBUG CONTEXT:
        {{outputs['error-analysis'].debug_context}}

        NEXT STEPS:
        1. Review application logs with Trace ID: {{outputs['api-test'].trace_id}}
        2. Check infrastructure metrics around {{unixtime()}}
        3. Validate request parameters and authentication
        {{outputs['api-test'].retry_after != nil ? "4. Retry after " + outputs['api-test'].retry_after + " seconds" : ""}}
        5. Escalate to development team if issue persists

    - name: Success Report
      uses: hello
      skipif: '!(outputs["api-test"]?.operation_successful ?? false)'
      echo: |
        ✅ Operation Completed Successfully

        Correlation ID: {{vars.CORRELATION_ID}}
        Response Time: {{outputs['api-test'].response_time}}ms
        Operation ID: {{outputs['api-test'].operation_id}}

        Performance Metrics:
        Server Response Time: {{outputs['api-test'].server_response_time}}ms
        {{outputs['api-test'].database_time != nil ? "Database Time: " + string(outputs['api-test'].database_time) + "ms" : ""}}
        {{outputs['api-test'].cache_hit != nil ? "Cache Hit: " + string(outputs['api-test'].cache_hit) : ""}}
```

## ベストプラクティス

以下では、失敗をどう分類するか、失敗とともに何を記録するか、対応をどう段階的に強めるかを扱います。

### 1. エラー分類

失敗への対処は種類によって変わります。そのためまず種類を判別します。

```yaml
# 良い例: タイプと深刻度でエラーを分類
outputs:
  error_type: |
    res.code >= 500 ? "server_error" :
      res.code == 429 ? "rate_limit" :
      res.code >= 400 ? "client_error" : "none"

  severity: |
    res.code >= 500 ? "critical" :
      res.code == 429 ? "warning" : "error"
```

### 2. コンテキスト情報

失敗したリクエストを特定できる情報を記録します。サーバー側のログから探せるようにするためです。

```yaml
# 良い例: 包括的なコンテキストを取得
outputs:
  error_context: |
    "Request ID: " + (res.headers["X-Request-Id"] ?? "-") + "\n" +
    "Timestamp: " + string(unixtime()) + "\n" +
    "User: " + vars.TEST_USER_ID + "\n" +
    "Operation: " + vars.operation_name
```

### 3. 回復戦略の選択

失敗を分類できていれば、対応はその分類から決まります。

```yaml
# 良い例: エラータイプに基づいた回復戦略の選択
- name: Recovery Strategy
  uses: hello
  echo: |
    Recovery Strategy: {{
      outputs.error_type == "rate_limit" ? "Wait and retry" :
      outputs.error_type == "server_error" ? "Switch to backup service" :
      outputs.error_type == "client_error" ? "Fix request and retry" :
      "Investigate and escalate"
    }}
```

### 4. 段階的エラーハンドリング

各ジョブは次のジョブより軽い手段を試し、最後のジョブで初めて打ち切ります。

```yaml
# 良い例: 段階的エラーハンドリング
jobs:
- id: quick-retry        # 即座にリトライを試行
- id: fallback-service   # 代替サービスを試行
- id: cache-fallback     # キャッシュされたデータを使用
- id: manual-escalation  # 人間にアラート
```

## 一般的なエラーシナリオ

実際に遭遇する失敗の大半は3つに集中し、それぞれ対応が異なります。

### ネットワーク接続の問題

タイムアウトを指定すれば、到達できないホストと遅いだけのホストを区別できます。

```yaml
- name: Network Connectivity Test
  uses: http
  timeout: 5s
  with:
    method: GET
    url: "{{vars.EXTERNAL_SERVICE_URL}}/ping"
  test: res.code == 200
  outputs:
    connectivity_ok: res.code == 200
```

### 認証失敗

401が一時的であることはほとんどありません。リトライしても時間を使うだけで、報告したほうが早く解決します。

```yaml
- name: Authentication Error Handler
  uses: hello
  echo: |
    Authentication failed:
    1. Check token expiry
    2. Verify credentials
    3. Refresh authentication
```

### レート制限

429には次に試してよい時刻が含まれます。待ち時間は推測ではなくレスポンスから取ります。

```yaml
- name: Rate Limit Handler
  uses: http
  with:
    method: GET
    url: "{{vars.api_url}}/data"
  test: res.code != 429
  echo: |
    {{res.code == 429 ? "Rate limit exceeded" : "Within the rate limit"}}
    Retry after: {{res.headers["Retry-After"]}} seconds
    Current quota: {{res.headers["X-Rate-Limit-Remaining"]}}
```

## 次のステップ

効果的にエラーを処理できるようになったので、次を探索してください：

- **[パフォーマンステスト](/ja/guide/how-tos/performance-testing)** - 応答時間への上限と、専用のツールによる負荷試験
- **[環境管理](/ja/guide/how-tos/environment-management)** - 環境間での設定を管理
- **[モニタリングワークフロー](/ja/guide/how-tos/monitoring-workflows)** - 包括的な監視システムを構築

エラーハンドリングを書いておけば、失敗したときの影響を抑えられます。これらのパターンをマスターして、予期しない事態を適切に処理し、可能な場合はリトライ、代替サービスへの切り替え、サービスの再起動で自動的に復旧を試みるワークフローを構築しましょう。
