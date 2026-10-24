# Monitoring Workflows

This guide shows you how to build comprehensive monitoring systems using Probe. You'll learn to create workflows that check service health, monitor performance, and alert on issues.

## Basic Service Monitoring

Monitoring starts with a single health check, then grows to cover several services in one run.

### Simple Health Check

Start with a basic health check workflow:

```yaml
name: Basic Service Health Check
description: Monitor essential service endpoints

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

**Usage:**
```bash
probe health-check.yml
```

### Multi-Service Health Monitoring

Monitor multiple services in parallel. The summary job has `always: true`, so it reports even when a check fails, and it reads each output with a default because a check that could not connect publishes none:

```yaml
name: Multi-Service Health Monitor
description: Check health of all critical services

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
        user_healthy: res.code == 200
        user_response_time: (rt.sec * 1000)
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
        order_healthy: res.code == 200
        order_response_time: (rt.sec * 1000)
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
        payment_healthy: res.code == 200
        payment_response_time: (rt.sec * 1000)
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
        notification_healthy: res.code == 200
        notification_response_time: (rt.sec * 1000)
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

        User Service: {{(outputs['user-service']?.user_healthy ?? false) ? "✅" : "❌"}} ({{outputs['user-service']?.user_response_time ?? "-"}}ms)
          Active Users: {{outputs['user-service']?.user_count ?? "-"}}

        Order Service: {{(outputs['order-service']?.order_healthy ?? false) ? "✅" : "❌"}} ({{outputs['order-service']?.order_response_time ?? "-"}}ms)
          Pending Orders: {{outputs['order-service']?.pending_orders ?? "-"}}

        Payment Service: {{(outputs['payment-service']?.payment_healthy ?? false) ? "✅" : "❌"}} ({{outputs['payment-service']?.payment_response_time ?? "-"}}ms)
          Transaction Queue: {{outputs['payment-service']?.transaction_queue ?? "-"}}

        Notification Service: {{(outputs['notification-service']?.notification_healthy ?? false) ? "✅" : "❌"}} ({{outputs['notification-service']?.notification_response_time ?? "-"}}ms)
          Notification Queue: {{outputs['notification-service']?.queue_size ?? "-"}}

        Overall System Status: {{
          (outputs['user-service']?.user_healthy ?? false) &&
          (outputs['order-service']?.order_healthy ?? false) &&
          (outputs['payment-service']?.payment_healthy ?? false) &&
          (outputs['notification-service']?.notification_healthy ?? false) ?
          "🟢 ALL SYSTEMS OPERATIONAL" : "🔴 ISSUES DETECTED"
        }}

        Timestamp: {{unixtime()}}
```

## Database and Infrastructure Monitoring

A service can answer its health endpoint while the database behind it is failing. Checking that layer directly catches what an HTTP probe misses.

### Database Health Monitoring

A query that returns is the only proof that the database is reachable and answering.

```yaml
name: Database Health Monitor
description: Monitor database connectivity and performance

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

## Comprehensive System Monitoring

The workflow below puts the previous checks together and covers the stack in one run.

### Full-Stack Monitoring Workflow

Each layer is its own job, so the report says which layer is down rather than that something is.

```yaml
name: Full-Stack System Monitor
description: Comprehensive monitoring of all system components

vars:
  # Service URLs
  FRONTEND_URL: https://app.yourcompany.com
  API_GATEWAY_URL: https://api.yourcompany.com
  LOAD_BALANCER_URL: "{{LOAD_BALANCER_URL}}"
  CDN_URL: "{{CDN_URL}}"
  SECURITY_API_URL: "{{SECURITY_API_URL}}"

  # Monitoring thresholds
  MAX_RESPONSE_TIME: 2000
  MIN_SUCCESS_RATE: 0.95
  MAX_ERROR_RATE: 0.05

jobs:
  # Tier 1: Infrastructure Layer
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

# Tier 2: Application Layer
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

# Tier 3: Business Logic Layer
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

# Tier 4: Performance Validation
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

# Tier 5: Security and Compliance
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

# Final Report
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

## Alerting and Notification Integration

A check that nobody reads is not monitoring. Adding notification steps makes the result reach someone.

### Monitoring with Email Alerts

A job that `needs` a failed job is skipped, so the alerts are steps of the same job as the checks. A step after a failed one still runs, so the alerts are reached, and each runs only when the summary of the checks calls for it.

```yaml
name: Monitoring with Email Alerts
description: Health monitoring with automated email notifications

vars:
  # Services
  USER_SERVICE_URL: https://users.yourcompany.com
  PAYMENT_SERVICE_URL: https://payments.yourcompany.com
  ORDER_SERVICE_URL: https://orders.yourcompany.com

  # SMTP Configuration
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
        user_healthy: res.code == 200
        user_status_code: res.code
        user_response_time: rt.sec * 1000

    - name: Payment Service Check
      id: payment_service
      uses: http
      with:
        method: GET
        url: "{{vars.PAYMENT_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        payment_healthy: res.code == 200
        payment_status_code: res.code
        payment_response_time: rt.sec * 1000

    - name: Order Service Check
      id: order_service
      uses: http
      with:
        method: GET
        url: "{{vars.ORDER_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        order_healthy: res.code == 200
        order_status_code: res.code
        order_response_time: rt.sec * 1000

    # A check whose request failed, such as a refused connection, publishes
    # no outputs, so the fallbacks count it as down.
    - name: Summarize the checks
      id: summary
      uses: hello
      outputs:
        all_healthy: (outputs.user_service?.user_healthy ?? false) && (outputs.payment_service?.payment_healthy ?? false) && (outputs.order_service?.order_healthy ?? false)
        slow: (outputs.user_service?.user_response_time ?? 0) > 2000 || (outputs.payment_service?.payment_response_time ?? 0) > 2000 || (outputs.order_service?.order_response_time ?? 0) > 2000

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
        User Service: {{outputs.user_service?.user_healthy ?? false ? "healthy" : "DOWN"}} (HTTP {{outputs.user_service?.user_status_code ?? "-"}})
        Payment Service: {{outputs.payment_service?.payment_healthy ?? false ? "healthy" : "DOWN"}} (HTTP {{outputs.payment_service?.payment_status_code ?? "-"}})
        Order Service: {{outputs.order_service?.order_healthy ?? false ? "healthy" : "DOWN"}} (HTTP {{outputs.order_service?.order_status_code ?? "-"}})

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
        User Service: {{outputs.user_service?.user_response_time ?? "-"}}ms
        Payment Service: {{outputs.payment_service?.payment_response_time ?? "-"}}ms
        Order Service: {{outputs.order_service?.order_response_time ?? "-"}}ms

    - name: All Clear Notification
      uses: hello
      skipif: '!outputs.summary.all_healthy || outputs.summary.slow'
      echo: All critical services are healthy; no alert sent.
```

The smtp action sends a generated test message and has no parameter for a body, so the details go to `echo`, which puts them in the run's report.

## Environment-Specific Monitoring

Thresholds and endpoints differ between environments, so one monitoring file has to select them rather than hardcode them.

### Multi-Environment Configuration

**base-monitoring.yml:**
```yaml merge=env-monitoring base
name: Service Health Monitor
description: Base monitoring workflow for all environments

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

  # Production-specific security monitoring
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
  SECURITY_API_URL: http://localhost:4000  # unused: the security checks run only in production
```

**production.yml:**
```yaml merge=env-monitoring
vars:
  ENVIRONMENT: production
  API_URL: https://api.yourcompany.com
  DB_API_URL: https://db-api.yourcompany.com
  SECURITY_API_URL: https://security-api.yourcompany.com
```

**Usage:**
```bash
# Development monitoring
probe base-monitoring.yml,development.yml

# Production monitoring (includes security checks)
probe base-monitoring.yml,production.yml
```

A later file that has `jobs` replaces every job of the earlier file, so the production-only check is in `base-monitoring.yml`, and its `skipif` runs it only when `ENVIRONMENT` is `production`. `development.yml` still defines `SECURITY_API_URL`, so every variable the jobs read is set in both runs. `monitoring-report` has `always: true` so that it reports a failed check rather than being skipped.

## Best Practices

The points below cover what to monitor, how to keep alerts meaningful, what the checks themselves cost, and how they stay maintained.

### 1. Monitoring Strategy

- **Layer your monitoring**: Infrastructure → Application → Business Logic
- **Set appropriate timeouts**: Strict for production, lenient for development
- **Publish results as outputs**: For non-critical checks, record the result instead of asserting it with `test`
- **Implement gradual alerting**: Info → Warning → Critical

### 2. Alert Fatigue Prevention

An alert that fires on every blip stops being read, so the condition decides what is worth sending.

```yaml
# Good: Conditional alerting
- name: Smart Alerting
  uses: smtp
  # ...

# Avoid: Alert on every issue
- name: Noisy Alerting
  uses: smtp
  # Creates alert fatigue
```

### 3. Performance Considerations

Independent checks belong in separate jobs so the whole run takes as long as the slowest one, not their sum.

```yaml
vars:
  SERVICE_A_URL: https://service-a.yourcompany.com
  SERVICE_B_URL: https://service-b.yourcompany.com

# Good: Parallel independent checks
jobs:
- name: Service A Check  # Runs in parallel
  steps:
    - name: Service A Health
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_A_URL}}/health"
      test: res.code == 200
      # Good: Efficient outputs
      outputs:
        service_a_healthy: res.code == 200     # Boolean flag
        service_a_time: (rt.sec * 1000)        # Specific metric
        # Avoid storing entire response: full_response: res.body

- name: Service B Check  # Runs in parallel
  steps:
    - name: Service B Health
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_B_URL}}/health"
      test: res.code == 200
      outputs:
        service_b_healthy: res.code == 200
        service_b_time: (rt.sec * 1000)
```

### 4. Documentation and Maintenance

The description is where the workflow records what it watches and what a failure means.

```yaml
name: Well-Documented Monitor
description: |
  Monitoring workflow for the e-commerce platform.

  Checks:
  - User service health and performance
  - Order processing service
  - Payment gateway connectivity
  - Database performance

  Alerting:
  - Critical: Service completely down
  - Warning: Performance degradation
  - Info: All systems normal

  Expected execution time: 30-60 seconds

  Maintenance:
  - Review thresholds monthly
  - Update service URLs when services move
  - Test alert channels quarterly
```

## Troubleshooting Common Issues

When a monitoring workflow reports a failure that is not real, the cause is usually in how it reaches the service rather than in the service itself.

### 1. Service Discovery Problems

When the address itself is wrong, the check fails in a way that looks like an outage.

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

### 2. Network Connectivity Issues

A short timeout separates a slow response from one that is never coming.

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

### 3. Authentication Problems

An expired credential produces a failure that has nothing to do with the service's health.

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

## What's Next?

Now that you can build monitoring workflows, explore:

- **[API Testing](/guide/how-tos/api-testing)** - Comprehensive API testing strategies
- **[Error Handling Strategies](/guide/how-tos/error-handling-strategies)** - Robust error handling patterns
- **[Performance Testing](/guide/how-tos/performance-testing)** - Limits on response times, and load tests with a tool made for them

Monitoring is the foundation of reliable systems. Use these patterns to build comprehensive monitoring that catches issues before they impact users.
