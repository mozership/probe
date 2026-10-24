# モニタリングワークフロー

このガイドでは、Probeを使用して包括的な監視システムを構築する方法を説明します。サービスヘルスのチェック、パフォーマンスの監視、問題のアラートを行うワークフローの作成方法を学びます。

## 基本的なサービス監視

監視は1つのヘルスチェックから始め、そこから1回の実行で複数のサービスを対象にする形へ広げます。

### シンプルなヘルスチェック

基本的なヘルスチェックワークフローから始めます：

```yaml
name: Basic Service Health Check
description: 必須サービスエンドポイントを監視

vars:
  API_BASE_URL: https://api.yourcompany.com
  HEALTH_ENDPOINT: /health
  TIMEOUT: 30s

jobs:
- id: health-check
  name: Service Health Check
  defaults:
    http:
      headers:
        User-Agent: "Probe Monitor v1.0"
  steps:
    - name: API Health Check
      id: health-check
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}{{vars.HEALTH_ENDPOINT}}"
      test: res.code == 200
      outputs:
        api_healthy: res.code == 200
        response_time: (rt.sec * 1000)
        api_version: res.body.version

    - name: Health Status Report
      uses: hello
      echo: |
        🏥 Health Check Results:

        API Status: {{outputs.api_healthy ? "✅ Healthy" : "❌ Down"}}
        Response Time: {{outputs.response_time}}ms
        API Version: {{outputs.api_version}}
        Timestamp: {{unixtime()}}
```

**使用方法:**
```bash
probe health-check.yml
```

### マルチサービスヘルス監視

複数のサービスを並列で監視します。集計のジョブには`always: true`があるため、チェックが失敗しても結果を報告します。接続できなかったチェックはoutputsを公開しないため、集計では各出力をデフォルト値付きで読みます。

```yaml
name: Multi-Service Health Monitor
description: すべての重要なサービスのヘルスをチェック

vars:
  USER_SERVICE_URL: https://users.api.yourcompany.com
  ORDER_SERVICE_URL: https://orders.api.yourcompany.com
  PAYMENT_SERVICE_URL: https://payments.api.yourcompany.com
  NOTIFICATION_SERVICE_URL: https://notifications.api.yourcompany.com

jobs:
- id: user-service
  name: User Service Health
  steps:
    - name: User Service Check
      id: user-service
      uses: http
      with:
        method: GET
        url: "{{vars.USER_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        healthy: res.code == 200
        response_time: (rt.sec * 1000)
        user_count: res.body.active_users

- id: order-service
  name: Order Service Health
  steps:
    - name: Order Service Check
      id: order-service
      uses: http
      with:
        method: GET
        url: "{{vars.ORDER_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        healthy: res.code == 200
        response_time: (rt.sec * 1000)
        pending_orders: res.body.pending_orders

- id: payment-service
  name: Payment Service Health
  steps:
    - name: Payment Service Check
      id: payment-service
      uses: http
      with:
        method: GET
        url: "{{vars.PAYMENT_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        healthy: res.code == 200
        response_time: (rt.sec * 1000)
        transaction_queue: res.body.queue_length

- id: notification-service
  name: Notification Service Health
  steps:
    - name: Notification Service Check
      id: notification-service
      uses: http
      with:
        method: GET
        url: "{{vars.NOTIFICATION_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        healthy: res.code == 200
        response_time: (rt.sec * 1000)
        queue_size: res.body.notification_queue

- id: summary-report
  name: Health Summary
  needs: [user-service, order-service, payment-service, notification-service]
  always: true
  steps:
    - name: Generate Health Report
      uses: hello
      echo: |
        🎯 Multi-Service Health Report
        ===============================

        User Service: {{(outputs['user-service']?.healthy ?? false) ? "✅" : "❌"}} ({{outputs['user-service']?.response_time ?? "-"}}ms)
          Active Users: {{outputs['user-service']?.user_count ?? "-"}}

        Order Service: {{(outputs['order-service']?.healthy ?? false) ? "✅" : "❌"}} ({{outputs['order-service']?.response_time ?? "-"}}ms)
          Pending Orders: {{outputs['order-service']?.pending_orders ?? "-"}}

        Payment Service: {{(outputs['payment-service']?.healthy ?? false) ? "✅" : "❌"}} ({{outputs['payment-service']?.response_time ?? "-"}}ms)
          Transaction Queue: {{outputs['payment-service']?.transaction_queue ?? "-"}}

        Notification Service: {{(outputs['notification-service']?.healthy ?? false) ? "✅" : "❌"}} ({{outputs['notification-service']?.response_time ?? "-"}}ms)
          Notification Queue: {{outputs['notification-service']?.queue_size ?? "-"}}

        Overall System Status: {{
          (outputs['user-service']?.healthy ?? false) &&
          (outputs['order-service']?.healthy ?? false) &&
          (outputs['payment-service']?.healthy ?? false) &&
          (outputs['notification-service']?.healthy ?? false) ?
          "🟢 ALL SYSTEMS OPERATIONAL" : "🔴 ISSUES DETECTED"
        }}

        Timestamp: {{unixtime()}}
```

## データベースとインフラストラクチャ監視

背後のデータベースが壊れていても、サービスのヘルスエンドポイントは応答し続けることがあります。その層を直接確認すれば、HTTPの監視では分からない障害も検出できます。

### データベースヘルス監視

データベースに到達して応答があることは、実際にクエリが返ることでしか確認できません。

```yaml
name: Database Health Monitor
description: データベース接続とパフォーマンスを監視

vars:
  DB_HOST: db.yourcompany.com
  DB_PORT: 5432
  DB_NAME: production
  REDIS_HOST: redis.yourcompany.com
  REDIS_PORT: 6379
  DB_API_URL: "{{DB_API_URL}}"
  CACHE_API_URL: "{{CACHE_API_URL}}"

jobs:
- id: database-connectivity
  name: Database Connectivity
  steps:
    - name: PostgreSQL Connection Test
      id: database-connectivity
      uses: http
      with:
        method: GET
        url: "{{vars.DB_API_URL}}/ping"
      test: res.code == 200
      outputs:
        db_connected: res.code == 200
        connection_time: (rt.sec * 1000)
        active_connections: res.body.active_connections
        max_connections: res.body.max_connections

    - name: Database Performance Check
      id: database-performance
      uses: http
      with:
        method: GET
        url: "{{vars.DB_API_URL}}/stats"
      test: res.code == 200 && res.body.query_performance.avg_ms < 100
      outputs:
        avg_query_time: res.body.query_performance.avg_ms
        slow_queries: res.body.slow_queries.count
        db_size_mb: res.body.database_size_mb

- id: cache-monitoring
  name: Cache System Health
  steps:
    - name: Redis Connection Test
      id: cache-connectivity
      uses: http
      with:
        method: GET
        url: "{{vars.CACHE_API_URL}}/ping"
      test: res.code == 200
      outputs:
        cache_connected: res.code == 200
        cache_response_time: (rt.sec * 1000)
        memory_usage_percent: res.body.memory.usage_percent
        keys_count: res.body.keys.total

    - name: Cache Performance Check
      id: cache-performance
      uses: http
      with:
        method: GET
        url: "{{vars.CACHE_API_URL}}/stats"
      test: res.code == 200 && res.body.hit_rate > 0.8
      outputs:
        hit_rate: res.body.hit_rate
        miss_rate: res.body.miss_rate
        evicted_keys: res.body.evicted_keys

- id: infrastructure-report
  name: Infrastructure Report
  needs: [database-connectivity, cache-monitoring]
  steps:
    - name: Infrastructure Health Summary
      uses: hello
      echo: |
        🏗️ Infrastructure Health Report
        =================================

        Database Status:
        Connection: {{outputs['database-connectivity'].db_connected ? "✅ Connected" : "❌ Failed"}}
        Response Time: {{outputs['database-connectivity'].connection_time}}ms
        Active Connections: {{outputs['database-connectivity'].active_connections}}/{{outputs['database-connectivity'].max_connections}}
        Average Query Time: {{outputs['database-performance'].avg_query_time}}ms
        Slow Queries: {{outputs['database-performance'].slow_queries}}
        Database Size: {{outputs['database-performance'].db_size_mb}}MB

        Cache Status:
        Connection: {{outputs['cache-connectivity'].cache_connected ? "✅ Connected" : "❌ Failed"}}
        Response Time: {{outputs['cache-connectivity'].cache_response_time}}ms
        Memory Usage: {{outputs['cache-connectivity'].memory_usage_percent}}%
        Total Keys: {{outputs['cache-connectivity'].keys_count}}
        Hit Rate: {{(outputs['cache-performance'].hit_rate * 100)}}%
        Miss Rate: {{(outputs['cache-performance'].miss_rate * 100)}}%

        Performance Alerts:
        {{outputs['database-performance'].avg_query_time > 100 ? "⚠️ Database queries are slow (" + string(outputs['database-performance'].avg_query_time) + "ms)" : ""}}
        {{outputs['database-performance'].slow_queries > 10 ? "⚠️ High number of slow queries (" + string(outputs['database-performance'].slow_queries) + ")" : ""}}
        {{outputs['cache-connectivity'].memory_usage_percent > 80 ? "⚠️ Cache memory usage high (" + string(outputs['cache-connectivity'].memory_usage_percent) + "%)" : ""}}
        {{outputs['cache-performance'].hit_rate < 0.8 ? "⚠️ Cache hit rate low (" + string(outputs['cache-performance'].hit_rate * 100) + "%)" : ""}}
```

## 包括的システム監視

以下のワークフローでは、ここまでの確認をまとめ、1回の実行でシステム全体を対象にします。

### フルスタック監視ワークフロー

層ごとにジョブを分ければ、何かが落ちたことではなく、どの層が落ちたかがレポートからわかります。

```yaml
name: Full-Stack System Monitor
description: すべてのシステムコンポーネントの包括的監視

vars:
  # サービスURL
  FRONTEND_URL: https://app.yourcompany.com
  API_GATEWAY_URL: https://api.yourcompany.com
  LOAD_BALANCER_URL: "{{LOAD_BALANCER_URL}}"
  CDN_URL: "{{CDN_URL}}"
  SECURITY_API_URL: "{{SECURITY_API_URL}}"

  # 監視しきい値
  MAX_RESPONSE_TIME: 2000
  MIN_SUCCESS_RATE: 0.95
  MAX_ERROR_RATE: 0.05

jobs:
  # Tier 1: インフラストラクチャ層
- id: infrastructure-health
  name: Infrastructure Health Check
  steps:
    - name: Load Balancer Health
      id: load-balancer
      uses: http
      with:
        method: GET
        url: "{{vars.LOAD_BALANCER_URL}}/health"
      test: res.code == 200
      outputs:
        lb_healthy: res.code == 200
        active_backends: res.body.active_backends
        total_backends: res.body.total_backends

    - name: CDN Performance
      id: cdn
      uses: http
      with:
        method: GET
        url: "{{vars.CDN_URL}}/health"
      test: res.code == 200 && (rt.sec * 1000) < 500
      outputs:
        cdn_healthy: res.code == 200
        cdn_response_time: (rt.sec * 1000)
        cache_hit_ratio: res.body.cache_hit_ratio

# Tier 2: アプリケーション層
- id: application-health
  name: Application Health Check
  needs: [infrastructure-health]
  steps:
    - name: Frontend Health
      id: frontend
      uses: http
      with:
        method: GET
        url: "{{vars.FRONTEND_URL}}/health"
      test: res.code == 200
      outputs:
        frontend_healthy: res.code == 200
        frontend_version: res.body.version
        frontend_build: res.body.build

    - name: API Gateway Health
      id: api-gateway
      uses: http
      with:
        method: GET
        url: "{{vars.API_GATEWAY_URL}}/health"
      test: res.code == 200
      outputs:
        gateway_healthy: res.code == 200
        gateway_version: res.body.version
        registered_services: len(res.body.services)

# Tier 3: ビジネスロジック層
- id: business-logic-health
  name: Business Logic Health
  needs: [application-health]
  steps:
    - name: User Service Functional Test
      id: user-service
      uses: http
      with:
        method: GET
        url: "{{vars.API_GATEWAY_URL}}/users/health-check"
      test: res.code == 200 && res.body.functional_test_passed == true
      outputs:
        user_service_functional: res.body.functional_test_passed
        active_sessions: res.body.active_sessions

    - name: Order Service Functional Test
      id: order-service
      uses: http
      with:
        method: GET
        url: "{{vars.API_GATEWAY_URL}}/orders/health-check"
      test: res.code == 200 && res.body.functional_test_passed == true
      outputs:
        order_service_functional: res.body.functional_test_passed
        processing_queue_length: res.body.queue_length

# Tier 4: パフォーマンス検証
- id: performance-validation
  name: Performance Validation
  needs: [business-logic-health]
  steps:
    - name: End-to-End Performance Test
      id: performance-validation
      uses: http
      with:
        url: "{{vars.API_GATEWAY_URL}}/performance/e2e-test"
        method: POST
        body: |
          {
            "test_type": "quick_validation",
            "max_duration_seconds": 30
          }
      test: |
        res.code == 200 &&
        res.body.success_rate >= vars.MIN_SUCCESS_RATE &&
        res.body.avg_response_time <= vars.MAX_RESPONSE_TIME
      outputs:
        success_rate: res.body.success_rate
        avg_response_time: res.body.avg_response_time
        p95_response_time: res.body.p95_response_time
        error_rate: res.body.error_rate

# Tier 5: セキュリティとコンプライアンス
- id: security-checks
  name: Security Health Checks
  needs: [performance-validation]
  steps:
    - name: SSL Certificate Check
      id: ssl-certificates
      uses: http
      with:
        url: "{{vars.SECURITY_API_URL}}/ssl-check"
        method: POST
        body: |
          {
            "domains": [
              "{{vars.FRONTEND_URL}}",
              "{{vars.API_GATEWAY_URL}}"
            ]
          }
      test: res.code == 200 && res.body.all_certificates_valid == true
      outputs:
        ssl_valid: res.body.all_certificates_valid
        cert_expiry_days: res.body.min_days_to_expiry

    - name: Security Headers Check
      id: security-headers
      uses: http
      with:
        url: "{{vars.SECURITY_API_URL}}/headers-check"
        method: POST
        body: |
          {
            "url": "{{vars.FRONTEND_URL}}"
          }
      test: res.code == 200 && res.body.security_score >= 0.8
      outputs:
        security_score: res.body.security_score
        missing_headers: res.body.missing_headers

# 最終レポート
- id: system-health-report
  name: System Health Report
  needs: [infrastructure-health, application-health, business-logic-health, performance-validation, security-checks]
  steps:
    - name: Generate Comprehensive Report
      uses: hello
      echo: |
        🌐 Full-Stack System Health Report
        ===================================
        Generated: {{unixtime()}}

        📊 INFRASTRUCTURE LAYER
        Load Balancer: {{outputs['load-balancer'].lb_healthy ? "✅ Healthy" : "❌ Issues"}}
          Backends: {{outputs['load-balancer'].active_backends}}/{{outputs['load-balancer'].total_backends}} active
        CDN: {{outputs['cdn'].cdn_healthy ? "✅ Healthy" : "❌ Issues"}} ({{outputs['cdn'].cdn_response_time}}ms)
          Cache Hit Ratio: {{(outputs['cdn'].cache_hit_ratio * 100)}}%

        🖥️ APPLICATION LAYER
        Frontend: {{outputs['frontend'].frontend_healthy ? "✅ Healthy" : "❌ Issues"}}
          Version: {{outputs['frontend'].frontend_version}} (Build: {{outputs['frontend'].frontend_build}})
        API Gateway: {{outputs['api-gateway'].gateway_healthy ? "✅ Healthy" : "❌ Issues"}}
          Version: {{outputs['api-gateway'].gateway_version}}
          Services: {{outputs['api-gateway'].registered_services}} registered

        🏢 BUSINESS LOGIC LAYER
        User Service: {{outputs['user-service'].user_service_functional ? "✅ Functional" : "❌ Issues"}}
          Active Sessions: {{outputs['user-service'].active_sessions}}
        Order Service: {{outputs['order-service'].order_service_functional ? "✅ Functional" : "❌ Issues"}}
          Processing Queue: {{outputs['order-service'].processing_queue_length}} items

        ⚡ PERFORMANCE METRICS
        Success Rate: {{(outputs['performance-validation'].success_rate * 100)}}%
        Average Response Time: {{outputs['performance-validation'].avg_response_time}}ms
        95th Percentile: {{outputs['performance-validation'].p95_response_time}}ms
        Error Rate: {{(outputs['performance-validation'].error_rate * 100)}}%

        🔒 SECURITY STATUS
        SSL Certificates: {{outputs['ssl-certificates'].ssl_valid ? "✅ Valid" : "❌ Issues"}}
          Expiry: {{outputs['ssl-certificates'].cert_expiry_days}} days minimum
        Security Headers: Score {{(outputs['security-headers'].security_score * 100)}}%
        {{len(outputs['security-headers'].missing_headers) > 0 ? "Missing Headers: " + join(outputs['security-headers'].missing_headers, ", ") : ""}}

        🎯 OVERALL SYSTEM STATUS
        {{
          outputs['load-balancer'].lb_healthy &&
          outputs['cdn'].cdn_healthy &&
          outputs['frontend'].frontend_healthy &&
          outputs['api-gateway'].gateway_healthy &&
          outputs['user-service'].user_service_functional &&
          outputs['order-service'].order_service_functional &&
          outputs['performance-validation'].success_rate >= vars.MIN_SUCCESS_RATE &&
          outputs['performance-validation'].avg_response_time <= vars.MAX_RESPONSE_TIME &&
          outputs['ssl-certificates'].ssl_valid &&
          outputs['security-headers'].security_score >= 0.8
          ? "🟢 ALL SYSTEMS OPERATIONAL"
          : "🔴 ISSUES REQUIRE ATTENTION"
        }}

        ⚠️ ALERTS
        {{outputs['load-balancer'].active_backends != outputs['load-balancer'].total_backends ? "• Load balancer has inactive backends" : ""}}
        {{outputs['cdn'].cdn_response_time > 1000 ? "• CDN response time is high" : ""}}
        {{outputs['performance-validation'].success_rate < vars.MIN_SUCCESS_RATE ? "• Success rate below threshold" : ""}}
        {{outputs['performance-validation'].avg_response_time > vars.MAX_RESPONSE_TIME ? "• Average response time exceeds threshold" : ""}}
        {{outputs['ssl-certificates'].cert_expiry_days < 30 ? "• SSL certificates expiring soon" : ""}}
        {{outputs['security-headers'].security_score < 0.8 ? "• Security headers need improvement" : ""}}
```

## アラートと通知の統合

誰も見ない確認は監視になりません。通知のステップを加えることで、結果が人に届きます。

### メールアラート付き監視

`needs`で依存しているジョブが失敗するとそのジョブはスキップされるため、通知は、チェックと同じジョブのステップとして書きます。失敗したステップの後のステップも実行されるため、通知のステップも実行されます。各通知は、チェックの結果をまとめたステップの値に応じて、実行するかどうかが決まります。

```yaml
name: Monitoring with Email Alerts
description: 自動メール通知付きヘルス監視

vars:
  # サービス
  USER_SERVICE_URL: https://users.yourcompany.com
  PAYMENT_SERVICE_URL: https://payments.yourcompany.com
  ORDER_SERVICE_URL: https://orders.yourcompany.com

  # SMTP設定
  SMTP_HOST: smtp.yourcompany.com
  SMTP_PORT: 587
  ALERT_FROM: alerts@yourcompany.com
  ALERT_RECIPIENTS: ops@yourcompany.com, dev-team@yourcompany.com

jobs:
- name: Health Monitoring
  steps:
    - name: User Service Check
      id: user_service
      uses: http
      with:
        method: GET
        url: "{{vars.USER_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        healthy: res.code == 200
        status_code: res.code
        response_time: rt.sec * 1000

    - name: Payment Service Check
      id: payment_service
      uses: http
      with:
        method: GET
        url: "{{vars.PAYMENT_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        healthy: res.code == 200
        status_code: res.code
        response_time: rt.sec * 1000

    - name: Order Service Check
      id: order_service
      uses: http
      with:
        method: GET
        url: "{{vars.ORDER_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        healthy: res.code == 200
        status_code: res.code
        response_time: rt.sec * 1000

    # 接続の拒否などでリクエスト自体が失敗したチェックはoutputsを公開しないため、
    # フォールバックでそれも停止として扱う
    - name: Summarize the checks
      id: summary
      uses: hello
      outputs:
        all_healthy: (outputs.user_service?.healthy ?? false) && (outputs.payment_service?.healthy ?? false) && (outputs.order_service?.healthy ?? false)
        slow: (outputs.user_service?.response_time ?? 0) > 2000 || (outputs.payment_service?.response_time ?? 0) > 2000 || (outputs.order_service?.response_time ?? 0) > 2000

    - name: Critical Service Alert
      uses: smtp
      skipif: outputs.summary.all_healthy
      with:
        addr: "{{vars.SMTP_HOST}}:{{vars.SMTP_PORT}}"
        from: "{{vars.ALERT_FROM}}"
        to: "{{vars.ALERT_RECIPIENTS}}"
        subject: "CRITICAL: Service Health Alert"
      test: res.code == 0
      echo: |
        CRITICAL SERVICE HEALTH ALERT
        Time: {{unixtime()}}
        User Service: {{outputs.user_service?.healthy ?? false ? "healthy" : "DOWN"}} (HTTP {{outputs.user_service?.status_code ?? "-"}})
        Payment Service: {{outputs.payment_service?.healthy ?? false ? "healthy" : "DOWN"}} (HTTP {{outputs.payment_service?.status_code ?? "-"}})
        Order Service: {{outputs.order_service?.healthy ?? false ? "healthy" : "DOWN"}} (HTTP {{outputs.order_service?.status_code ?? "-"}})

    - name: Performance Warning Alert
      uses: smtp
      skipif: '!outputs.summary.slow'
      with:
        addr: "{{vars.SMTP_HOST}}:{{vars.SMTP_PORT}}"
        from: "{{vars.ALERT_FROM}}"
        to: "{{vars.ALERT_RECIPIENTS}}"
        subject: "WARNING: Performance Degradation Detected"
      test: res.code == 0
      echo: |
        PERFORMANCE WARNING (threshold: 2000ms)
        Time: {{unixtime()}}
        User Service: {{outputs.user_service?.response_time ?? "-"}}ms
        Payment Service: {{outputs.payment_service?.response_time ?? "-"}}ms
        Order Service: {{outputs.order_service?.response_time ?? "-"}}ms

    - name: All Clear Notification
      uses: hello
      skipif: '!outputs.summary.all_healthy || outputs.summary.slow'
      echo: All critical services are healthy; no alert sent.
```

smtpアクションが送るのは生成されたテストメッセージで、本文を指定するパラメータはありません。そのため詳細は`echo`に書き、実行のレポートに載せます。

## 環境固有の監視

しきい値も接続先も環境ごとに異なります。1つの監視用ファイルで扱うには、これらを直接書かずに選択する形にします。

### マルチ環境設定

**base-monitoring.yml:**
```yaml merge=env-monitoring base
name: Service Health Monitor
description: すべての環境用のベース監視ワークフロー

jobs:
- id: service-health
  name: Service Health Check
  defaults:
    http:
      headers:
        User-Agent: "Probe Monitor"
        Accept: "application/json"
  steps:
    - name: API Health
      id: api-health
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health"
      test: res.code == 200
      outputs:
        api_healthy: res.code == 200
        response_time: (rt.sec * 1000)

    - name: Database Health
      id: database-health
      uses: http
      with:
        method: GET
        url: "{{vars.DB_API_URL}}/ping"
      test: res.code == 200
      outputs:
        db_healthy: res.code == 200
        db_response_time: (rt.sec * 1000)

- id: monitoring-report
  name: Monitoring Report
  needs: [service-health]
  always: true
  defaults:
    http:
      headers:
        User-Agent: "Probe Monitor"
        Accept: "application/json"
  steps:
    - name: Status Report
      uses: hello
      echo: |
        Environment: {{vars.ENVIRONMENT}}
        API: {{(outputs['api-health']?.api_healthy ?? false) ? "✅" : "❌"}} ({{outputs['api-health']?.response_time ?? "-"}}ms)
        Database: {{(outputs['database-health']?.db_healthy ?? false) ? "✅" : "❌"}} ({{outputs['database-health']?.db_response_time ?? "-"}}ms)

  # プロダクション固有のセキュリティ監視
- id: security-monitoring
  name: Security Monitoring
  needs: [service-health]
  skipif: vars.ENVIRONMENT != "production"
  steps:
    - name: SSL Certificate Check
      id: security-monitoring
      uses: http
      with:
        method: GET
        url: "{{vars.SECURITY_API_URL}}/ssl-status"
      test: res.code == 200 && res.body.all_valid == true
      outputs:
        ssl_valid: res.body.all_valid
        days_to_expiry: res.body.min_days_to_expiry
```

**development.yml:**
```yaml merge=env-monitoring
vars:
  ENVIRONMENT: development
  API_URL: http://localhost:3000
  DB_API_URL: http://localhost:5432
  SECURITY_API_URL: http://localhost:4000  # 未使用: セキュリティチェックはプロダクションでのみ実行
```

**production.yml:**
```yaml merge=env-monitoring
vars:
  ENVIRONMENT: production
  API_URL: https://api.yourcompany.com
  DB_API_URL: https://db-api.yourcompany.com
  SECURITY_API_URL: https://security-api.yourcompany.com
```

**使用方法:**
```bash
# 開発環境監視
probe base-monitoring.yml,development.yml

# プロダクション環境監視（セキュリティチェック含む）
probe base-monitoring.yml,production.yml
```

後のファイルに`jobs`があると、前のファイルのジョブはすべて置き換わります。そのため、プロダクション専用のチェックは`base-monitoring.yml`に置き、`skipif`で`ENVIRONMENT`が`production`のときだけ実行します。`development.yml`にも`SECURITY_API_URL`を定義し、どちらの実行でもジョブが読む変数がすべて設定されるようにしています。`monitoring-report`には`always: true`があるため、チェックが失敗してもスキップされずに結果を報告します。

## ベストプラクティス

以下では、何を監視するか、アラートを意味のあるものに保つ方法、確認自体にかかるコスト、そして保守の続け方を扱います。

### 1. 監視戦略

- **監視を階層化**: インフラストラクチャ → アプリケーション → ビジネスロジック
- **適切なタイムアウトを設定**: プロダクション用は短く、開発用は長く
- **結果をoutputsに出す**: 重要でないチェックは`test`で判定せず結果を記録する
- **段階的アラート**: 情報 → 警告 → 重大

### 2. アラート疲れの防止

わずかな揺らぎでも発生するアラートは読まれなくなります。送る価値があるかは条件で判断します。

```yaml
# 良い例: 条件付きアラート
- name: Smart Alerting
  uses: smtp
  # ...

# 避ける: すべての問題でアラート
- name: Noisy Alerting
  uses: smtp
  # アラート疲れを引き起こす
```

### 3. パフォーマンスの考慮事項

独立した確認は別々のジョブにします。そうすれば全体の所要時間は合計ではなく、最も遅いものの時間になります。

```yaml
vars:
  SERVICE_A_URL: https://service-a.yourcompany.com
  SERVICE_B_URL: https://service-b.yourcompany.com

# 良い例: 独立したチェックを並列実行
jobs:
- name: Service A Check  # 並列実行
  steps:
    - name: Service A Health
      id: service-a
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_A_URL}}/health"
      test: res.code == 200
      # 良い例: 効率的な出力
      outputs:
        healthy: res.code == 200          # ブール値フラグ
        response_time: (rt.sec * 1000)    # 特定のメトリクス
        # 避ける: レスポンス全体の保存: full_response: res.body

- name: Service B Check  # 並列実行
  steps:
    - name: Service B Health
      id: service-b
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_B_URL}}/health"
      test: res.code == 200
      outputs:
        healthy: res.code == 200
        response_time: (rt.sec * 1000)
```

### 4. 文書化と保守

何を監視していて、失敗が何を意味するかはdescriptionに記録します。

```yaml
name: Well-Documented Monitor
description: |
  E-commerceプラットフォーム用の監視ワークフロー。

  チェック:
  - ユーザーサービスのヘルスとパフォーマンス
  - 注文処理サービス
  - ペイメントゲートウェイの接続性
  - データベースパフォーマンス

  アラート:
  - 重大: サービス完全停止
  - 警告: パフォーマンス劣化
  - 情報: すべてのシステム正常

  予想実行時間: 30-60秒

  保守:
  - しきい値を月次レビュー
  - サービス移行時にサービスURLを更新
  - アラートチャネルを四半期毎にテスト
```

## 一般的な問題のトラブルシューティング

監視ワークフローが実際には起きていない障害を報告するとき、原因はサービス側ではなく、サービスへの到達方法にあることがほとんどです。

### 1. サービス発見の問題

接続先のアドレス自体が誤っていると、障害と見分けのつかない失敗になります。

```yaml
- name: Service Discovery Check
  uses: http
  with:
    method: GET
    url: "{{vars.SERVICE_REGISTRY_URL}}/services"
  test: res.code == 200 && len(res.body.services) > 0
  outputs:
    available_services: 'map(res.body.services, #.name)'
    service_count: len(res.body.services)
```

### 2. ネットワーク接続の問題

タイムアウトを短くすれば、応答が遅いだけの状態と、そもそも返ってこない状態を区別できます。

```yaml
- name: Network Connectivity Test
  uses: http
  timeout: 5s
  with:
    method: GET
    url: "{{vars.EXTERNAL_HEALTH_CHECK_URL}}"
  test: res.code == 200
  outputs:
    external_connectivity: res.code == 200
```

### 3. 認証の問題

認証情報が失効していると、サービスの健全性とは無関係な失敗が出ます。

```yaml
- name: Authentication Health Check
  uses: http
  with:
    method: GET
    url: "{{vars.AUTH_SERVICE_URL}}/health"
    headers:
      Authorization: "Bearer {{vars.HEALTH_CHECK_TOKEN}}"
  test: res.code == 200
  outputs:
    auth_service_healthy: res.code == 200
```

## 次のステップ

監視ワークフローの構築ができるようになったので、次を探索してください：

- **[APIテスト](/ja/guide/how-tos/api-testing)** - 包括的なAPIテスト戦略
- **[エラーハンドリング戦略](/ja/guide/how-tos/error-handling-strategies)** - 堅牢なエラーハンドリングパターン
- **[パフォーマンステスト](/ja/guide/how-tos/performance-testing)** - 応答時間への上限と、専用のツールによる負荷試験

信頼性の高いシステムには監視が欠かせません。これらのパターンを使用して、ユーザーに影響が出る前に問題を検出する包括的な監視を構築しましょう。
