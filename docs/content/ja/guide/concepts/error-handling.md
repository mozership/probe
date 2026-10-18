# エラーハンドリング

エラーハンドリングは、失敗や予期せぬ状況を適切に管理し、回復力のあるワークフローを構築するために重要です。このガイドではエラーハンドリング戦略、復旧パターン、耐障害性のある自動化を構築するための技術について詳しく説明します。

## エラーハンドリングの基礎

Probeは、ワークフローの異なるレベルでエラーを処理するためのいくつかのメカニズムを提供します：

1. **ステップレベル**: 個々のステップが失敗にどう対応するかを制御
2. **ジョブレベル**: ジョブの失敗動作と復旧を管理
3. **ワークフローレベル**: 全体的なワークフロー失敗シナリオを処理
4. **条件付き実行**: 成功/失敗状態に基づいて実行をルーティング

### Probeでのエラータイプ

Probeはエラーを次の種類に分けて扱います：

```yaml
# テスト失敗 - アサーションが失敗
- name: API Health Check
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/health"
  test: res.code == 200  # これが失敗する可能性がある

# アクション失敗 - HTTP タイムアウト、接続エラーなど
- name: Timeout Example
  uses: http
  timeout: 5s  # タイムアウトする可能性がある
  with:
    method: GET
    url: "{{vars.SLOW_SERVICE}}"

# 設定エラー - 無効な URL、環境変数の不足
- name: Configuration Error
  uses: http
  with:
    method: GET
    url: "{{vars.MISSING_VAR}}/endpoint"  # 未定義の場合がある
```

## ステップレベルエラーハンドリング

個々のステップでの選択肢は2つです。失敗しても実行を続けるか、代替の手段に切り替えて使える結果を得るかです。

### エラー時継続

ステップが失敗したときにワークフロー実行を継続するかどうかを制御します：

```yaml
steps:
  - name: Critical Database Check
    uses: http
    with:
      method: GET
      url: "{{vars.DB_API}}/health"
    test: res.code == 200

  - name: Optional Analytics Update
    uses: http
    with:
      method: GET
      url: "{{vars.ANALYTICS_API}}/update"
    test: res.code == 200

  - name: This runs regardless of analytics result
    uses: hello
    echo: "Workflow continues..."
```

ステップは前のステップが失敗していても実行されるので、どの検査もレポートに1行ずつ残ります。後のステップが前のステップの作ったものを使うだけの場合は、ジョブに[`fail_fast`](/ja/reference/yaml-configuration#fail_fast)を付けます。失敗したステップより後のステップはスキップされ、レポートにはそこから派生した失敗ではなく、原因の1件だけが残ります。

```yaml
jobs:
- name: Checkout flow
  fail_fast: true
  steps:
    - name: Log in
      id: login
      uses: http
      with:
        post: "{{vars.API}}/login"
      test: res.code == 200
      outputs:
        token: res.body.token

    - name: Place an order          # ログインに失敗したときはスキップ
      uses: http
      with:
        post: "{{vars.API}}/orders"
        headers:
          authorization: "Bearer {{outputs.login.token}}"
      test: res.code == 201
```

### グレースフル・デグラデーション

重要でない障害を適切に処理します：

```yaml
jobs:
- id: monitoring-with-fallbacks
  name: Monitoring with Graceful Degradation
  steps:
    - name: Primary Service Check
      id: primary
      uses: http
      with:
        method: GET
        url: "{{vars.PRIMARY_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        primary_healthy: res.code == 200
        primary_response_time: (rt.sec * 1000)

    - name: Secondary Service Check
      id: secondary
      uses: http
      with:
        method: GET
        url: "{{vars.SECONDARY_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        secondary_healthy: res.code == 200
        secondary_response_time: (rt.sec * 1000)

    - name: Cache Service Check
      id: cache
      uses: http
      with:
        method: GET
        url: "{{vars.CACHE_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        cache_healthy: res.code == 200

    - name: Service Status Summary
      uses: hello
      echo: |
        Service Health Summary:
          
        Primary Service: {{outputs.primary.primary_healthy ? "✅ Healthy" : "❌ Down"}}
        {{outputs.primary.primary_healthy ? "Response Time: " + outputs.primary.primary_response_time + "ms" : ""}}
          
        {{outputs.secondary ? "Fallback Service: " + (outputs.secondary.secondary_healthy ? "✅ Healthy" : "❌ Down") : ""}}
        {{outputs.secondary.secondary_healthy ? "Response Time: " + outputs.secondary.secondary_response_time + "ms" : ""}}
          
        Cache Service: {{outputs.cache.cache_healthy ? "✅ Healthy" : "❌ Down"}}
          
        Overall Status: {{
          outputs.primary.primary_healthy || outputs.secondary.secondary_healthy ? 
          "✅ Operational" : "❌ Service Unavailable"
        }}
```

## ジョブレベルエラーハンドリング

ジョブが失敗すると、そのジョブを`needs`で指定したジョブに影響します。それらを実行するかどうかは条件で決めます。

### ジョブ依存関係と失敗の伝播

ジョブの失敗が依存ジョブにどう影響するかを制御します：

```yaml
jobs:
- id: infrastructure-check
  name: Infrastructure Validation
  steps:
    - name: Database Connectivity
      uses: http
      with:
        method: GET
        url: "{{vars.DB_URL}}/ping"
      test: res.code == 200

    - name: Message Queue Health
      uses: http
      with:
        method: GET
        url: "{{vars.QUEUE_URL}}/health"
      test: res.code == 200

- id: application-test
  name: Application Tests
  needs: [infrastructure-check]  # インフラが正常な場合のみ実行
  steps:
    - name: API Tests
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/test"
      test: res.code == 200

- id: notification
  name: Send Notifications
  needs: [infrastructure-check, application-test]
  steps:
    - name: Alert on Failures
      uses: hello
      echo: |
        Monitoring Alert:
          
          
```

### 失敗後の後片付け

他のジョブが作ったものを片付けるジョブは、それらが失敗したときにも実行する必要があります。`always: true`を付けたジョブは、`needs`のジョブがどう終わったかにかかわらず、すべてが終わり次第実行されます：

```yaml
jobs:
- id: tests
  name: Tests
  steps:
    - name: Create a user
      uses: http
      with:
        post: "{{vars.API_URL}}/users"
        body:
          name: probe-test
      test: res.code == 201

- name: Cleanup
  needs: [tests]
  always: true
  steps:
    - name: Delete the test users
      uses: http
      with:
        delete: "{{vars.API_URL}}/users?name=probe-test"
      test: res.code == 200 || res.code == 404
```

`tests`が失敗していれば、実行全体は失敗のままです。このジョブの実行時に成り立つ条件は[`always`](/ja/reference/yaml-configuration#always)を参照してください。

### 条件付きジョブ実行

`always`を除けば、他のジョブが失敗したかどうかで分岐する手段はありません。ジョブが失敗すると、それに依存するジョブはまとめてスキップされます。結果に応じて処理を変えたい場合は、結果をoutputsとして公開し、後続のジョブの`skipif`でその値を参照します。

```yaml
jobs:
- id: health-check
  name: Primary Health Check
  steps:
    - name: Service Health Check
      id: health
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/health"
      outputs:
        service_healthy: res.code == 200
        error_body: 'res.code != 200 ? res.body : null'

- id: recovery-procedures
  name: Recovery Procedures
  needs: [health-check]
  skipif: outputs.health.service_healthy
  steps:
    - name: Attempt Service Restart
      id: restart
      uses: http
      with:
        url: "{{vars.admin_api}}/restart"
        method: POST
      outputs:
        restart_successful: res.code == 200

    - name: Verify Recovery
      id: verify
      uses: http
      skipif: "!outputs.restart.restart_successful"
      with:
        method: GET
        url: "{{vars.service_url}}/health"
      outputs:
        recovery_successful: res.code == 200

- name: Escalate
  needs: [health-check, recovery-procedures]
  skipif: outputs.health.service_healthy || (outputs.recovery_successful ?? false)
  steps:
    - name: Critical Alert
      uses: smtp
      with:
        addr: "{{vars.smtp_addr}}"
        from: "alerts@example.com"
        to: "ops-team@example.com"
        subject: "CRITICAL: {{vars.service_name}} is down"
        session: 1
        message: 1
        length: 500
      test: res.code == 0
```


## リトライと回復力パターン

時間が経てば解消する失敗と、そうでない失敗があります。前者にはリトライが向き、後者にはサーキットブレーカーによって際限のない再試行を止める方法が向きます。

### フォールバック付き暗黙的リトライ

条件付き実行を使用してリトライロジックを実装します：

```yaml
jobs:
- id: resilient-api-test
  name: Resilient API Testing
  steps:
    - name: Primary Attempt
      id: attempt1
      uses: http
      timeout: 10s
      with:
        method: GET
        url: "{{vars.API_URL}}/endpoint"
      test: res.code == 200
      outputs:
        success: res.code == 200
        response_time: (rt.sec * 1000)

    - name: Retry After Brief Delay
      id: attempt2
      uses: http
      timeout: 15s
      with:
        method: GET
        url: "{{vars.API_URL}}/endpoint"
      test: res.code == 200
      outputs:
        success: res.code == 200
        response_time: (rt.sec * 1000)

    - name: Final Attempt with Extended Timeout
      id: attempt3
      uses: http
      timeout: 30s
      with:
        method: GET
        url: "{{vars.API_URL}}/endpoint"
      test: res.code == 200
      outputs:
        success: res.code == 200
        response_time: (rt.sec * 1000)

    - name: Fallback to Alternative Endpoint
      id: fallback
      uses: http
      timeout: 20s
      with:
        method: GET
        url: "{{vars.FALLBACK_API_URL}}/endpoint"
      test: res.code == 200
      outputs:
        success: res.code == 200
        response_time: (rt.sec * 1000)

    - name: Results Summary
      uses: hello
      echo: |
        API Test Results:
          
        Attempt 1: {{outputs.attempt1.success ? "✅ Success (" + outputs.attempt1.response_time + "ms)" : "❌ Failed"}}
        {{outputs.attempt2 ? "Attempt 2: " + (outputs.attempt2.success ? "✅ Success (" + outputs.attempt2.response_time + "ms)" : "❌ Failed") : ""}}
        {{outputs.attempt3 ? "Attempt 3: " + (outputs.attempt3.success ? "✅ Success (" + outputs.attempt3.response_time + "ms)" : "❌ Failed") : ""}}
        {{outputs.fallback ? "Fallback: " + (outputs.fallback.success ? "✅ Success (" + outputs.fallback.response_time + "ms)" : "❌ Failed") : ""}}
          
        Final Result: {{
          outputs.attempt1.success || outputs.attempt2.success || outputs.attempt3.success || outputs.fallback.success ?
          "✅ API Accessible" : "❌ All attempts failed"
        }}
```

### サーキットブレーカーパターン

カスケード障害を防ぐためのサーキットブレーカーロジックを実装します：

```yaml
jobs:
- id: circuit-breaker-test
  name: Circuit Breaker Pattern
  steps:
    - name: Check Service Health History
      id: health-history
      uses: http
      with:
        method: GET
        url: "{{vars.MONITORING_API}}/service/{{vars.SERVICE_NAME}}/health-history"
      test: res.code == 200
      outputs:
        recent_failures: res.body.failures_last_5_minutes
        error_rate: res.body.error_rate_percentage
        last_success: res.body.last_success_timestamp

    - name: Circuit Breaker Decision
      uses: hello
      id: circuit-decision
      echo: "Evaluating circuit breaker state"
      outputs:
        circuit_open: "{{outputs['health-history'].error_rate > 50 || outputs['health-history'].recent_failures > 10}}"
        should_test: "{{!outputs['health-history'].circuit_open || (unixtime() - outputs['health-history'].last_success) > 300}}"

    - name: Service Test (Circuit Closed)
      id: normal-test
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/test"
      test: res.code == 200
      outputs:
        test_successful: res.code == 200

    - name: Probe Test (Circuit Open)
      id: probe-test
      uses: http
      timeout: 5s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/health"  # より軽量なプローブ
      test: res.code == 200
      outputs:
        probe_successful: res.code == 200

    - name: Circuit Breaker Status
      uses: hello
      echo: |
        Circuit Breaker Status Report:
          
        Error Rate: {{outputs['health-history'].error_rate}}%
        Recent Failures: {{outputs['health-history'].recent_failures}}
        Circuit State: {{outputs['circuit-decision'].circuit_open ? "🔴 OPEN (Service Degraded)" : "🟢 CLOSED (Normal Operation)"}}
          
        {{outputs['normal-test'] ? "Normal Test: " + (outputs['normal-test'].test_successful ? "✅ Passed" : "❌ Failed") : ""}}
        {{outputs['probe-test'] ? "Probe Test: " + (outputs['probe-test'].probe_successful ? "✅ Passed" : "❌ Failed") : ""}}
          
        {{outputs['circuit-decision'].circuit_open && !outputs['circuit-decision'].should_test ? "⏸️ Skipping tests - circuit breaker active" : ""}}
        {{outputs['probe-test'].probe_successful ? "🟢 Service recovery detected - circuit may close" : ""}}
```

## エラーコンテキストとデバッグ

失敗に対処するには、発生時点で何が起きていたかを記録し、それを原因となったリクエストまでたどれるようにしておく必要があります。

### 包括的なエラー情報

デバッグのために詳細なエラーコンテキストをキャプチャします：

```yaml
- name: Detailed Error Capture
  id: api-test
  uses: http
  with:
    url: "{{vars.API_URL}}/complex-operation"
    method: POST
    body: |
      {
        "operation": "test",
        "timestamp": {{unixtime()}},
        "user_id": "{{vars.TEST_USER_ID}}"
      }
  test: res.code == 200 && res.body.success == true
  outputs:
    success: res.code == 200
    status_code: res.code
    response_time: (rt.sec * 1000)
    response_size: res.body_size
    error_message: 'res.code != 200 ? res.body : null'
    response_headers: res.headers
    partial_response: 'res.code != 200 ? res.body[0:500] : null'

- name: Error Analysis and Reporting
  echo: |
    🚨 API Test Failure Analysis:
    
    Request Details:
    - URL: {{vars.API_URL}}/complex-operation
    - Method: POST
    - User ID: {{vars.TEST_USER_ID}}
    - Timestamp: {{unixtime()}}
    
    Response Details:
    - Status Code: {{outputs['api-test'].status_code}}
    - Response Time: {{outputs['api-test'].response_time}}ms
    - Response Size: {{outputs['api-test'].response_size}} bytes
    - Content Type: {{outputs['api-test'].response_headers["content-type"]}}
    
    Error Information:
    {{outputs['api-test'].error_message ? outputs['api-test'].error_message : "No error message available"}}
    
    Response Preview:
    {{outputs['api-test'].partial_response ? outputs['api-test'].partial_response : "No response content"}}
    
    Troubleshooting Suggestions:
    {{outputs['api-test'].status_code == 404 ? "- Check if the endpoint URL is correct" : ""}}
    {{outputs['api-test'].status_code == 401 ? "- Verify authentication credentials" : ""}}
    {{outputs['api-test'].status_code == 403 ? "- Check user permissions" : ""}}
    {{outputs['api-test'].status_code == 500 ? "- Server error - check application logs" : ""}}
    {{outputs['api-test'].response_time > 10000 ? "- Request timed out - check network connectivity" : ""}}
```

### エラー関連性と追跡

複数のステップとジョブ間でエラーを追跡します：

```yaml
jobs:
- id: distributed-test
  name: Distributed System Test
  steps:
    - name: Initialize Correlation ID
      uses: hello
      id: init
      echo: "Starting distributed test"
      outputs:
        correlation_id: "test_{{unixtime()}}_{{random_str(8)}}"
        start_time: "{{unixtime()}}"

    - name: Service A Test
      id: service-a
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_A_URL}}/test"
        headers:
          X-Correlation-ID: "{{outputs.init.correlation_id}}"
      test: res.code == 200
      outputs:
        success: res.code == 200
        error_code: 'res.code != 200 ? res.code : null'
        trace_id: res.headers["X-Trace-Id"]

    - name: Service B Test
      id: service-b
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_B_URL}}/test"
        headers:
          X-Correlation-ID: "{{outputs.init.correlation_id}}"
      test: res.code == 200
      outputs:
        success: res.code == 200
        error_code: 'res.code != 200 ? res.code : null'
        trace_id: res.headers["X-Trace-Id"]

    - name: Service C Test
      id: service-c
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_C_URL}}/test"
        headers:
          X-Correlation-ID: "{{outputs.init.correlation_id}}"
      test: res.code == 200
      outputs:
        success: res.code == 200
        error_code: 'res.code != 200 ? res.code : null'
        trace_id: res.headers["X-Trace-Id"]

    - name: Error Correlation Report
      uses: hello
      echo: |
        🔍 Distributed Test Error Report
          
        Correlation ID: {{outputs.init.correlation_id}}
        Test Duration: {{unixtime() - outputs.init.start_time}} seconds
          
        Service Status Summary:
        - Service A: {{outputs['service-a'].success ? "✅ Healthy" : "❌ Failed (HTTP " + outputs['service-a'].error_code + ")"}}
          {{outputs['service-a'].trace_id ? "Trace ID: " + outputs['service-a'].trace_id : ""}}
          
        - Service B: {{outputs['service-b'].success ? "✅ Healthy" : "❌ Failed (HTTP " + outputs['service-b'].error_code + ")"}}
          {{outputs['service-b'].trace_id ? "Trace ID: " + outputs['service-b'].trace_id : ""}}
          
        - Service C: {{outputs['service-c'].success ? "✅ Healthy" : "❌ Failed (HTTP " + outputs['service-c'].error_code + ")"}}
          {{outputs['service-c'].trace_id ? "Trace ID: " + outputs['service-c'].trace_id : ""}}
          
        Error Pattern Analysis:
        {{!outputs['service-a'].success && !outputs['service-b'].success && !outputs['service-c'].success ? "🚨 Total system failure - check infrastructure" : ""}}
        {{!outputs['service-a'].success && outputs['service-b'].success && outputs['service-c'].success ? "⚠️ Service A isolated failure" : ""}}
        {{outputs['service-a'].success && !outputs['service-b'].success && outputs['service-c'].success ? "⚠️ Service B isolated failure" : ""}}
        {{outputs['service-a'].success && outputs['service-b'].success && !outputs['service-c'].success ? "⚠️ Service C isolated failure" : ""}}
          
        Investigation Steps:
        1. Check application logs with correlation ID: {{outputs.init.correlation_id}}
        2. Review distributed traces using trace IDs above
        3. Monitor system metrics for the test time window
        4. Verify network connectivity between services
```

## エラー回復戦略

失敗を検知したあとの対処もワークフローに書けます。回復手順を実行し、サービスが正常に戻ったことを段階的に確認します。

### 自動回復手順

一般的な失敗シナリオに対して自動回復を実装します：

```yaml
jobs:
- id: self-healing-monitor
  name: Self-Healing Monitoring
  steps:
    - name: Service Health Check
      id: health-check
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        healthy: res.code == 200
        status_code: res.code

    - name: Memory Usage Check
      id: memory-check
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/metrics"
      test: res.code == 200 && res.body.memory_usage_percent < 90
      outputs:
        memory_ok: res.code == 200 && res.body.memory_usage_percent < 90
        memory_usage: res.body.memory_usage_percent

    - name: Restart Service (Health Failure)
      id: restart-health
      uses: http
      with:
        url: "{{vars.ADMIN_API}}/services/{{vars.SERVICE_NAME}}/restart"
        method: POST
      test: res.code == 200
      outputs:
        restart_initiated: res.code == 200
        restart_reason: "health_check_failed"

    - name: Restart Service (Memory Issue)
      id: restart-memory
      uses: http
      with:
        url: "{{vars.ADMIN_API}}/services/{{vars.SERVICE_NAME}}/restart"
        method: POST
      test: res.code == 200
      outputs:
        restart_initiated: res.code == 200
        restart_reason: "high_memory_usage"

    - name: Wait for Service Recovery
      id: recovery-wait
      uses: http
      timeout: 60s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        recovery_successful: res.code == 200

    - name: Recovery Status Report
      uses: hello
      echo: |
        🔄 Self-Healing Monitor Report
          
        Initial Health Check: {{outputs['health-check'].healthy ? "✅ Healthy" : "❌ Failed (" + outputs['health-check'].status_code + ")"}}
        {{outputs['memory-check'] ? "Memory Usage: " + outputs['memory-check'].memory_usage + "% " + (outputs['memory-check'].memory_ok ? "✅ Normal" : "⚠️ High") : ""}}
          
        Recovery Actions:
        {{outputs['restart-health'].restart_initiated ? "🔄 Service restarted due to health check failure" : ""}}
        {{outputs['restart-memory'].restart_initiated ? "🔄 Service restarted due to high memory usage (" + outputs['memory-check'].memory_usage + "%)" : ""}}
          
        Recovery Result:
        {{outputs['recovery-wait'] ? (outputs['recovery-wait'].recovery_successful ? "✅ Service recovered successfully" : "❌ Service failed to recover") : "ℹ️ No recovery action needed"}}
          
        Next Actions:
        {{outputs['recovery-wait'] && !outputs['recovery-wait'].recovery_successful ? "🚨 Manual intervention required - service did not recover" : ""}}
        {{outputs['health-check'].healthy && (!outputs['memory-check'] || outputs['memory-check'].memory_ok) ? "✅ All systems normal - monitoring continues" : ""}}
```

### 段階的回復テスト

回復中のシステムに過負荷をかけないよう段階的にテストします：

```yaml
jobs:
- id: gradual-recovery-test
  name: Gradual Recovery Testing
  steps:
    - name: Basic Connectivity Test
      id: ping
      uses: http
      timeout: 5s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/ping"
      test: res.code == 200
      outputs:
        connectivity: res.code == 200

    - name: Health Endpoint Test
      id: health
      uses: http
      timeout: 10s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        health_ok: res.code == 200

    - name: Light Functional Test
      id: light-test
      uses: http
      timeout: 15s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/api/status"
      test: res.code == 200 && res.body.status == "ready"
      outputs:
        light_test_passed: res.code == 200 && res.body.status == "ready"

    - name: Standard Load Test
      id: standard-test
      uses: http
      timeout: 30s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/api/users"
      test: res.code == 200 && res.body.users != null
      outputs:
        standard_test_passed: res.code == 200

    - name: Recovery Assessment
      uses: hello
      echo: |
        🏥 Recovery Assessment Report
          
        Recovery Progress:
        1. Basic Connectivity: {{outputs.ping.connectivity ? "✅ Restored" : "❌ Failed"}}
        {{outputs.ping.connectivity ? "2. Health Check: " + (outputs.health.health_ok ? "✅ Passed" : "❌ Failed") : "2. Health Check: ⏸️ Skipped"}}
        {{outputs.health.health_ok ? "3. Light Function Test: " + (outputs['light-test'].light_test_passed ? "✅ Passed" : "❌ Failed") : "3. Light Function Test: ⏸️ Skipped"}}
        {{outputs['light-test'].light_test_passed ? "4. Standard Load Test: " + (outputs['standard-test'].standard_test_passed ? "✅ Passed" : "❌ Failed") : "4. Standard Load Test: ⏸️ Skipped"}}
          
        Recovery Status: {{
          outputs['standard-test'].standard_test_passed ? "🟢 FULLY RECOVERED" :
          outputs['light-test'].light_test_passed ? "🟡 PARTIALLY RECOVERED" :
          outputs.health.health_ok ? "🟡 BASIC FUNCTIONALITY RESTORED" :
          outputs.ping.connectivity ? "🟡 CONNECTIVITY RESTORED" :
          "🔴 SERVICE STILL DOWN"
        }}
          
        Recommendations:
        {{!outputs.ping.connectivity ? "- Check network connectivity and service deployment" : ""}}
        {{outputs.ping.connectivity && !outputs.health.health_ok ? "- Service is starting but not ready - wait and retry" : ""}}
        {{outputs.health.health_ok && !outputs['light-test'].light_test_passed ? "- Service health OK but functionality impaired - check dependencies" : ""}}
        {{outputs['light-test'].light_test_passed && !outputs['standard-test'].standard_test_passed ? "- Service functional but may be under load - monitor performance" : ""}}
        {{outputs['standard-test'].standard_test_passed ? "- Service fully operational - resume normal monitoring" : ""}}
```

## 通知とアラート

無人で動くワークフローでは、失敗の通知もワークフロー側で行う必要があります。通知のステップはエラー時の経路で実行し、そこで集めたコンテキストを載せます。

### エラー駆動通知

`needs`で依存しているジョブが失敗するとそのジョブはスキップされるため、通知は、対象のチェックと同じジョブのステップとして書きます。各チェックの結果を出力し、通知はそれが異常を示すときだけ実行します。失敗したステップの後のステップも実行されるため、通知のステップも実行されます。

```yaml
jobs:
- name: Health with alerts
  steps:
    - name: Health check
      id: health
      uses: http
      with:
        url: "{{vars.api_url}}/health"
        method: GET
      test: res.code == 200
      outputs:
        healthy: res.code == 200

    # 接続の拒否などでリクエスト自体が失敗するとoutputs.healthはないため、
    # フォールバックでそれも異常として扱う
    - name: Critical alert
      uses: smtp
      skipif: outputs.health?.healthy ?? false
      with:
        addr: "{{vars.smtp_host}}:587"
        from: critical-alerts@example.com
        to: oncall@example.com
        subject: "CRITICAL: {{vars.service_name}} is down"
      test: res.code == 0
      echo: |
        Service: {{vars.service_name}}
        Environment: {{vars.environment}}
        Time: {{unixtime()}}

    - name: All systems operational
      uses: hello
      skipif: '!(outputs.health?.healthy ?? false)'
      echo: No alert sent; the health check passed.
```

smtpアクションが送るのは生成されたテストメッセージで、本文を指定するパラメータはありません。そのため詳細は`echo`に書き、実行のレポートに載せます。

## ベストプラクティス

以下の指針は、どこまでの失敗なら実行を続け、どこで打ち切るか、打ち切るときに何を残すかを決めます。

### 1. フェイルファストと回復力のバランス

重要な経路のステップは実行を止め、情報を補うだけのステップは止めないようにします。

```yaml
# 重要パス - フェイルファスト
- name: Database Connection
  uses: http
  with:
    method: GET
    url: "{{vars.DB_URL}}/ping"
  test: res.code == 200

# 非重要パス - 回復力を持つ
- name: Analytics Tracking
  uses: http
  with:
    method: GET
    url: "{{vars.ANALYTICS_URL}}/track"
  test: res.code == 200
```

### 2. エラーコンテキストの保持

失敗したステップが受け取った内容はその場で保存します。後続のステップからは参照できなくなるためです。

```yaml
# 良い例: エラーコンテキストを保持
outputs:
  success: res.code == 200
  error_details: |
    {{res.code != 200 ? 
      "HTTP " + res.code + ": " + res.body[0:200] : 
      null}}
  debug_info: |
    {{res.code != 200 ? 
      "URL: " + vars.API_URL + ", Time: " + (rt.sec * 1000) + "ms" : 
      null}}
```

### 3. 段階的応答

対応は深刻度に合わせます。ログに残すだけで済む場合も、アラートを出す場合もあります。

```yaml
# 良い例: 異なるエラータイプに対する異なる応答
- name: Error Response Strategy
  echo: |
    Error Response:
    {{outputs['api-test'].status_code == 500 ? "🚨 Server error - escalate immediately" : ""}}
    {{outputs['api-test'].status_code == 404 ? "⚠️ Endpoint not found - check configuration" : ""}}
    {{outputs['api-test'].status_code == 401 ? "🔐 Authentication failed - refresh credentials" : ""}}
```

### 4. エラー回復ドキュメント

復旧手順を失敗とともに出力すれば、呼び出された人の目に入る場所に置けます。

```yaml
# ワークフロー内で回復手順を文書化
- name: Recovery Instructions
  echo: |
    🛠️ Recovery Procedures for {{vars.SERVICE_NAME}}:
    
    1. Check service logs: kubectl logs -l app={{vars.SERVICE_NAME}}
    2. Verify configuration: check {{vars.CONFIG_PATH}}
    3. Restart service: kubectl rollout restart deployment/{{vars.SERVICE_NAME}}
    4. Monitor recovery: watch kubectl get pods -l app={{vars.SERVICE_NAME}}
    
    Escalation: If service doesn't recover in 10 minutes, contact on-call team.
```

## 次のステップ

エラーハンドリングを理解したら、以下を探索してください：

1. **[実行モデル](/ja/guide/concepts/execution-model)** - Probeがワークフローを実行する方法を学ぶ
2. **[ファイルマージ](/ja/guide/concepts/file-merging)** - 設定の構成を理解する
3. **[ハウツー](/ja/guide/how-tos/api-testing)** - 実用的なエラーハンドリングパターンを見る

エラーハンドリングは、失敗したときのワークフローの挙動を決めるものです。これらのパターンをマスターして、予期しないことを適切に処理し、可能な場合は自動的に回復するワークフローを構築しましょう。
