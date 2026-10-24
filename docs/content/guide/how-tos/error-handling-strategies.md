# Error Handling Strategies

This guide shows you how to implement robust error handling in Probe workflows. You'll learn to handle failures gracefully, implement recovery patterns, and build resilient automation that can cope with unexpected conditions.

## Basic Error Handling Patterns

The first decision is what a failure should do to the rest of the run: stop it, or let it continue with a reduced result.

### Fail Fast vs Continue

Choose the right error handling strategy based on the criticality of operations:

```yaml
name: Error Handling Strategy Examples
description: Demonstrate different error handling approaches

vars:
  CRITICAL_SERVICE_URL: https://critical.yourcompany.com
  OPTIONAL_SERVICE_URL: https://optional.yourcompany.com
  NOTIFICATION_URL: https://notifications.yourcompany.com

jobs:
- id: critical-operations
  name: Critical Operations
  steps:
    # Fail fast for critical operations
    - name: Critical Database Check
      uses: http
      with:
        method: GET
        url: "{{vars.CRITICAL_SERVICE_URL}}/database/health"
      test: res.code == 200
      outputs:
        database_healthy: res.code == 200

    # This step only runs if database check passes
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
    # Continue on error for optional services
    - name: Optional Analytics Service
      uses: http
      with:
        method: GET
        url: "{{vars.OPTIONAL_SERVICE_URL}}/analytics"
      test: res.code == 200
      outputs:
        analytics_available: res.code == 200
        analytics_error: 'res.code != 200 ? res.status : nil'

    # This step always runs regardless of previous step
    - name: Optional Notification Service
      uses: http
      with:
        method: GET
        url: "{{vars.NOTIFICATION_URL}}/health"
      test: res.code == 200
      outputs:
        notifications_available: res.code == 200

    # Conditional logic based on service availability
    - name: Service Availability Report
      uses: hello
      echo: |
        🔧 Service Availability Report:
          
        Analytics Service: {{outputs.analytics_available ? "✅ Available" : "❌ Unavailable"}}
        {{outputs.analytics_error ? "Error Code: " + outputs.analytics_error : ""}}
          
        Notification Service: {{outputs.notifications_available ? "✅ Available" : "❌ Unavailable"}}
          
        Impact Assessment:
        {{!outputs.analytics_available ? "• Analytics features may be limited" : ""}}
        {{!outputs.notifications_available ? "• User notifications may be delayed" : ""}}
        {{outputs.analytics_available && outputs.notifications_available ? "• All optional services operational" : ""}}
```

### Graceful Degradation

Implement fallback mechanisms when primary services fail:

```yaml
name: Graceful Degradation Pattern
description: Implement fallback services and graceful degradation

vars:
  PRIMARY_API_URL: https://primary.api.yourcompany.com
  SECONDARY_API_URL: https://secondary.api.yourcompany.com
  CACHE_API_URL: https://cache.yourcompany.com
  FALLBACK_API_URL: https://fallback.api.yourcompany.com

jobs:
- id: service-with-fallbacks
  name: Service with Multiple Fallbacks
  steps:
    # Try primary service first
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

    # Try secondary service if primary fails or is slow
    - name: Secondary Service Attempt
      id: secondary
      uses: http
      timeout: 15s
      with:
        method: GET
        url: "{{vars.SECONDARY_API_URL}}/data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        response_time: (rt.sec * 1000)
        data: res.body

    # Try cache if both primary and secondary fail
    - name: Cache Fallback
      id: cache
      uses: http
      timeout: 5s
      with:
        method: GET
        url: "{{vars.CACHE_API_URL}}/cached-data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        response_time: (rt.sec * 1000)
        data: res.body
        cached_data: true

    # Final fallback to static data
    - name: Static Fallback
      id: fallback
      uses: http
      with:
        method: GET
        url: "{{vars.FALLBACK_API_URL}}/static-data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        response_time: (rt.sec * 1000)
        data: res.body
        static_data: true

    - name: Service Resolution Summary
      uses: hello
      echo: |
        🎯 Service Resolution Summary:
          
        Resolution Path:
        {{outputs.primary.success ? "✅ Primary Service (optimal)" : "❌ Primary Service failed/slow (" + outputs.primary.response_time + "ms)"}}
        {{outputs.secondary.success ? "✅ Secondary Service (backup)" : (!outputs.primary.success ? "❌ Secondary Service failed" : "")}}
        {{outputs.cache.success ? "✅ Cache Service (degraded)" : (!outputs.primary.success && !outputs.secondary.success ? "❌ Cache Service failed" : "")}}
        {{outputs.fallback.success ? "✅ Static Fallback (minimal)" : (!outputs.primary.success && !outputs.secondary.success && !outputs.cache.success ? "❌ All services failed" : "")}}
          
        Final Status: {{
          outputs.primary.success ? "🟢 Optimal Performance" :
          outputs.secondary.success ? "🟡 Backup Service Active" :
          outputs.cache.success ? "🟠 Degraded Mode (cached data)" :
          outputs.fallback.success ? "🔴 Minimal Functionality (static data)" :
          "🚨 Total Service Failure"
        }}
          
        Data Source: {{
          outputs.primary.success ? "Live Primary" :
          outputs.secondary.success ? "Live Secondary" :
          outputs.cache.success ? "Cached (may be stale)" :
          outputs.fallback.success ? "Static Fallback" :
          "None Available"
        }}
```

## Retry Patterns

Retrying only helps if the failure is temporary. Backing off between attempts and stopping after a run of failures keep it from making things worse.

### Exponential Backoff Retry

Implement retry logic with increasing delays:

```yaml
name: Retry with Exponential Backoff
description: Implement retry patterns for transient failures

vars:
  UNRELIABLE_SERVICE_URL: https://api.unreliable.service.com
  MAX_RETRIES: 3

jobs:
- id: retry-pattern
  name: Exponential Backoff Retry Pattern
  steps:
    # First attempt
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
        error_code: 'res.code != 200 ? res.status : nil'

    # Second attempt (2-second delay)
    - name: Retry Attempt 1 (2s delay)
      id: attempt2
      uses: http
      timeout: 15s
      with:
        method: GET
        url: "{{vars.UNRELIABLE_SERVICE_URL}}/data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        attempt_number: 2
        response_time: (rt.sec * 1000)
        error_code: 'res.code != 200 ? res.status : nil'

    # Third attempt (4-second delay)
    - name: Retry Attempt 2 (4s delay)
      id: attempt3
      uses: http
      timeout: 20s
      with:
        method: GET
        url: "{{vars.UNRELIABLE_SERVICE_URL}}/data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        attempt_number: 3
        response_time: (rt.sec * 1000)
        error_code: 'res.code != 200 ? res.status : nil'

    # Final attempt (8-second delay)
    - name: Final Attempt (8s delay)
      id: attempt4
      uses: http
      timeout: 30s
      with:
        method: GET
        url: "{{vars.UNRELIABLE_SERVICE_URL}}/data"
      test: res.code == 200
      outputs:
        success: res.code == 200
        attempt_number: 4
        response_time: (rt.sec * 1000)
        error_code: 'res.code != 200 ? res.status : nil'

    - name: Retry Summary
      uses: hello
      echo: |
        🔄 Retry Pattern Results:
          
        Attempt History:
        1. Initial: {{outputs.attempt1.success ? "✅ Success (" + outputs.attempt1.response_time + "ms)" : "❌ Failed (HTTP " + outputs.attempt1.error_code + ")"}}
        {{outputs.attempt2 ? "2. Retry 1: " + (outputs.attempt2.success ? "✅ Success (" + outputs.attempt2.response_time + "ms)" : "❌ Failed (HTTP " + outputs.attempt2.error_code + ")") : ""}}
        {{outputs.attempt3 ? "3. Retry 2: " + (outputs.attempt3.success ? "✅ Success (" + outputs.attempt3.response_time + "ms)" : "❌ Failed (HTTP " + outputs.attempt3.error_code + ")") : ""}}
        {{outputs.attempt4 ? "4. Final: " + (outputs.attempt4.success ? "✅ Success (" + outputs.attempt4.response_time + "ms)" : "❌ Failed (HTTP " + outputs.attempt4.error_code + ")") : ""}}
          
        Final Result: {{
          outputs.attempt1.success ? "✅ Success on first attempt" :
          outputs.attempt2.success ? "✅ Success on retry 1" :
          outputs.attempt3.success ? "✅ Success on retry 2" :
          outputs.attempt4.success ? "✅ Success on final attempt" :
          "❌ All attempts failed"
        }}
          
        {{
          outputs.attempt1.success ? "" :
          outputs.attempt2.success ? "Service recovered after transient failure" :
          outputs.attempt3.success ? "Service required multiple retries" :
          outputs.attempt4.success ? "Service barely recoverable" :
          "Service appears to be down"
        }}
```

### Circuit Breaker Pattern

Implement circuit breaker to prevent cascading failures:

```yaml
name: Circuit Breaker Pattern
description: Implement circuit breaker for fault isolation

vars:
  MONITORED_SERVICE_URL: https://api.monitored.service.com
  CIRCUIT_BREAKER_THRESHOLD: 5
  CIRCUIT_RECOVERY_TIME: 300  # 5 minutes

jobs:
- id: circuit-breaker-check
  name: Circuit Breaker Health Check
  steps:
    # Check current circuit breaker state
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

    # Evaluate circuit breaker state
    - name: Circuit Breaker Decision
      uses: hello
      id: decision
      echo: "Evaluating circuit breaker state"
      outputs:
        # Circuit is open if too many recent failures
        circuit_open: "{{outputs['circuit-status'].failure_count >= vars.CIRCUIT_BREAKER_THRESHOLD}}"
        # Allow probe if circuit has been open long enough
        time_since_failure: "{{unixtime() - outputs['circuit-status'].last_failure_time}}"
        should_probe: "{{(unixtime() - outputs['circuit-status'].last_failure_time) > vars.CIRCUIT_RECOVERY_TIME}}"

- id: service-test
  name: Service Test with Circuit Breaker
  needs: [circuit-breaker-check]
  steps:
    # Normal operation when circuit is closed
    - name: Normal Service Test
      id: normal-test
      uses: http
      timeout: 10s
      with:
        method: GET
        url: "{{vars.MONITORED_SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        test_successful: res.code == 200
        response_time: (rt.sec * 1000)
        error_code: 'res.code != 200 ? res.status : nil'

    # Probe test when circuit is open but recovery time has passed
    - name: Circuit Recovery Probe
      id: probe-test
      uses: http
      timeout: 5s
      with:
        method: GET
        url: "{{vars.MONITORED_SERVICE_URL}}/ping"  # Lighter probe
      test: res.code == 200
      outputs:
        probe_successful: res.code == 200
        response_time: (rt.sec * 1000)

    # Update circuit breaker state
    - name: Update Circuit Breaker
      uses: http
      with:
        url: "{{vars.MONITORING_API_URL}}/circuit-breaker/{{vars.SERVICE_NAME}}/update"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "test_result": {{
              outputs['normal-test'] ? outputs['normal-test'].test_successful :
              outputs['probe-test'] ? outputs['probe-test'].probe_successful : false
            }},
            "response_time": {{
              outputs['normal-test'] ? outputs['normal-test'].response_time :
              outputs['probe-test'] ? outputs['probe-test'].response_time : nil
            }},
            "timestamp": {{unixtime()}}
          }
      test: res.code == 200

    - name: Circuit Breaker Status Report
      uses: hello
      echo: |
        ⚡ Circuit Breaker Status Report:
          
        Previous State:
        Circuit State: {{outputs['circuit-breaker-check'].circuit_state}}
        Failure Count: {{outputs['circuit-breaker-check'].failure_count}}
        Time Since Last Failure: {{outputs['circuit-breaker-check'].time_since_failure}} seconds
          
        Current Test:
        {{outputs['normal-test'] ? "Normal Test: " + (outputs['normal-test'].test_successful ? "✅ Passed" : "❌ Failed (HTTP " + outputs['normal-test'].error_code + ")") : ""}}
        {{outputs['probe-test'] ? "Recovery Probe: " + (outputs['probe-test'].probe_successful ? "✅ Passed" : "❌ Failed") : ""}}
        {{outputs['circuit-breaker-check'].circuit_open && !outputs['circuit-breaker-check'].should_probe ? "⏸️ Circuit Open - Skipping test (recovery time not reached)" : ""}}
          
        Circuit Action: {{
          outputs['normal-test'].test_successful ? "✅ Circuit remains closed" :
          outputs['probe-test'].probe_successful ? "🟢 Circuit should close (service recovered)" :
          outputs['probe-test'] && !outputs['probe-test'].probe_successful ? "🔴 Circuit remains open (service still failing)" :
          outputs['normal-test'] && !outputs['normal-test'].test_successful ? "🔴 Circuit should open (service failing)" :
          "⏸️ No test performed"
        }}
```

## Error Recovery Strategies

A workflow can do more than report a failure. It can attempt the repair and then confirm that the service came back.

### Self-Healing Workflows

Implement workflows that can automatically recover from failures:

```yaml
name: Self-Healing Service Monitor
description: Monitor services and automatically attempt recovery

vars:
  SERVICE_NAME: user-service
  SERVICE_HEALTH_URL: https://user-service.yourcompany.com/health
  ADMIN_API_URL: https://admin.yourcompany.com/api
  RECOVERY_ATTEMPTS: 3

jobs:
- id: health-monitoring
  name: Health Monitoring and Recovery
  steps:
    # Step 1: Check service health
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
        status_code: res.status
        response_time: (rt.sec * 1000)
        error_details: 'res.code != 200 ? res.body : nil'

    # Step 2: Detailed diagnostics if unhealthy
    - name: Service Diagnostics
      id: diagnostics
      uses: http
      timeout: 45s
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

- id: automated-recovery
  name: Automated Recovery Procedures
  needs: [health-monitoring]
  steps:
    # Recovery Attempt 1: Graceful restart
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

    # Wait and verify first restart
    - name: Verify Graceful Restart
      uses: http
      timeout: 60s
      with:
        method: GET
        url: "{{vars.SERVICE_HEALTH_URL}}"
      test: res.code == 200
      outputs:
        restart_successful: res.code == 200

    # Recovery Attempt 2: Force restart if graceful failed
    - name: Force Service Restart
      id: restart-attempt-2
      uses: http
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

    # Verify force restart
    - name: Verify Force Restart
      uses: http
      timeout: 60s
      with:
        method: GET
        url: "{{vars.SERVICE_HEALTH_URL}}"
      test: res.code == 200
      outputs:
        force_restart_successful: res.code == 200

    # Recovery Attempt 3: Scale up new instances
    - name: Scale Up Service
      id: scale-up
      uses: http
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

    # Final health check
    - name: Final Health Verification
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
  steps:
    - name: Recovery Status Report
      uses: hello
      echo: |
        🏥 Service Recovery Report for {{vars.SERVICE_NAME}}:
        ================================================
          
        INITIAL HEALTH CHECK:
        Status: {{outputs['health-monitoring'].healthy ? "✅ Healthy" : "❌ Unhealthy (HTTP " + outputs['health-monitoring'].status_code + ")"}}
        Response Time: {{outputs['health-monitoring'].response_time}}ms
        {{outputs['health-monitoring'].error_details ? "Error Details: " + outputs['health-monitoring'].error_details : ""}}
          
        {{outputs['health-monitoring'].diagnostics_available ? "DIAGNOSTICS:" : ""}}
        {{outputs['health-monitoring'].diagnostics_available ? "Memory Usage: " + outputs['health-monitoring'].memory_usage + "%" : ""}}
        {{outputs['health-monitoring'].diagnostics_available ? "CPU Usage: " + outputs['health-monitoring'].cpu_usage + "%" : ""}}
        {{outputs['health-monitoring'].diagnostics_available ? "Active Connections: " + outputs['health-monitoring'].active_connections : ""}}
        {{outputs['health-monitoring'].diagnostics_available ? "Error Rate: " + outputs['health-monitoring'].error_rate + "/min" : ""}}
          
        RECOVERY ACTIONS:
        {{outputs['automated-recovery'].restart_initiated ? "1. Graceful Restart: " + (outputs['automated-recovery'].restart_successful ? "✅ Successful" : "❌ Failed") : "1. Graceful Restart: ⏸️ Not attempted"}}
        {{outputs['automated-recovery'].force_restart_initiated ? "2. Force Restart: " + (outputs['automated-recovery'].force_restart_successful ? "✅ Successful" : "❌ Failed") : "2. Force Restart: ⏸️ Not attempted"}}
        {{outputs['automated-recovery'].scale_up_initiated ? "3. Scale Up: ✅ Initiated" : "3. Scale Up: ⏸️ Not attempted"}}
          
        FINAL STATUS:
        Service Health: {{outputs['automated-recovery'].final_health_status ? "✅ Healthy" : "❌ Still Unhealthy"}}
          
        RECOVERY RESULT: {{
          outputs['health-monitoring'].healthy ? "ℹ️ No recovery needed - service was healthy" :
          outputs['automated-recovery'].restart_successful ? "🟢 Recovered via graceful restart" :
          outputs['automated-recovery'].force_restart_successful ? "🟡 Recovered via force restart" :
          outputs['automated-recovery'].final_health_status ? "🟢 Recovered via scaling" :
          "🔴 Recovery failed - manual intervention required"
        }}
          
        {{!outputs['automated-recovery'].final_health_status && !outputs['health-monitoring'].healthy ? "🚨 ALERT: Service recovery failed - escalating to on-call team" : ""}}

    # Escalation notification if recovery failed
    - name: Escalation Alert
      uses: smtp
      with:
        addr: "{{vars.SMTP_HOST}}:587"
        from: "alerts@yourcompany.com"
        to: "oncall@yourcompany.com"
        subject: "🚨 CRITICAL: Service Recovery Failed - {{vars.SERVICE_NAME}}"
        session: 1
        message: 1
        length: 500
      echo: |
        CRITICAL SERVICE RECOVERY FAILURE
        =================================
          
        Service: {{vars.SERVICE_NAME}}
        Time: {{unixtime()}}
        Environment: {{vars.ENVIRONMENT}}
          
        Initial Problem:
        - Health Check: Failed (HTTP {{outputs['health-monitoring'].status_code}})
        - Response Time: {{outputs['health-monitoring'].response_time}}ms
          
        Recovery Attempts:
        {{outputs['automated-recovery'].restart_initiated ? "- Graceful Restart: " + (outputs['automated-recovery'].restart_successful ? "Success" : "Failed") : "- Graceful Restart: Not attempted"}}
        {{outputs['automated-recovery'].force_restart_initiated ? "- Force Restart: " + (outputs['automated-recovery'].force_restart_successful ? "Success" : "Failed") : "- Force Restart: Not attempted"}}
        {{outputs['automated-recovery'].scale_up_initiated ? "- Scale Up: Initiated" : "- Scale Up: Not attempted"}}
          
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

## Comprehensive Error Context

What gets collected at the moment of failure decides whether the report is actionable later.

### Error Information Collection

Collect comprehensive error information for debugging:

```yaml
name: Comprehensive Error Context Collection
description: Collect detailed error information for effective debugging

vars:
  API_BASE_URL: https://api.yourservice.com
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
        # Success indicators
        operation_successful: res.code == 200 && res.body.success == true
          
        # Response metadata
        status_code: res.status
        response_time: (rt.sec * 1000)
        response_size: res.body_size
        content_type: res.headers["Content-Type"]
          
        # Error context (only populated on failure)
        error_message: 'res.code != 200 ? res.body.error.message : nil'
        error_code: 'res.code != 200 ? res.body.error.code : nil'
        error_details: 'res.code != 200 ? res.body.error.details : nil'
        trace_id: res.headers["X-Trace-Id"]
        request_id: res.headers["X-Request-Id"]
          
        # Performance context
        server_response_time: res.headers["X-Response-Time"]
        database_time: 'res.body.debug ? res.body.debug.database_time_ms : nil'
        cache_hit: 'res.body.debug ? res.body.debug.cache_hit : nil'
          
        # Business context
        affected_user: 'res.body.error ? res.body.error.affected_user : nil'
        operation_id: res.body.operation_id
        retry_after: res.headers["Retry-After"]

    - name: Error Analysis and Enrichment
      uses: hello
      id: error-analysis
      echo: "Analyzing error context"
      outputs:
        # Classify error type
        error_category: |
          {{outputs['api-test'].status_code >= 500 ? "server_error" :
            outputs['api-test'].status_code == 429 ? "rate_limit" :
            outputs['api-test'].status_code >= 400 && outputs['api-test'].status_code < 500 ? "client_error" :
            outputs['api-test'].status_code == 0 ? "network_error" : "unknown"}}
          
        # Determine severity
        severity_level: |
          {{outputs['api-test'].status_code >= 500 ? "high" :
            outputs['api-test'].status_code == 429 ? "medium" :
            outputs['api-test'].status_code >= 400 && outputs['api-test'].status_code < 500 ? "low" :
            "critical"}}
          
        # Generate troubleshooting hints
        troubleshooting_hints: |
          {{outputs['api-test'].status_code == 401 ? "Check authentication token expiry and permissions" :
            outputs['api-test'].status_code == 403 ? "Verify user has required permissions for this operation" :
            outputs['api-test'].status_code == 404 ? "Confirm API endpoint exists and user/resource exists" :
            outputs['api-test'].status_code == 409 ? "Resource conflict - check for duplicate operations" :
            outputs['api-test'].status_code == 429 ? "Rate limit exceeded - implement backoff or check quota" :
            outputs['api-test'].status_code >= 500 ? "Server error - check application logs and infrastructure" :
            "Network or timeout issue - verify connectivity and service availability"}}
          
        # Context for debugging
        debug_context: |
          Correlation ID: {{vars.CORRELATION_ID}}
          Test User ID: {{vars.TEST_USER_ID}}
          Request Timestamp: {{unixtime()}}
          Environment: {{vars.ENVIRONMENT}}

    - name: Detailed Error Report
      uses: hello
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
        {{outputs['api-test'].database_time ? "Database Time: " + outputs['api-test'].database_time + "ms" : ""}}
        {{outputs['api-test'].cache_hit ? "Cache Hit: " + outputs['api-test'].cache_hit : ""}}
        {{outputs['api-test'].retry_after ? "Retry After: " + outputs['api-test'].retry_after + " seconds" : ""}}
          
        BUSINESS CONTEXT:
        {{outputs['api-test'].affected_user ? "Affected User: " + outputs['api-test'].affected_user : ""}}
          
        TROUBLESHOOTING:
        {{outputs['error-analysis'].troubleshooting_hints}}
          
        DEBUG CONTEXT:
        {{outputs['error-analysis'].debug_context}}
          
        NEXT STEPS:
        1. Review application logs with Trace ID: {{outputs['api-test'].trace_id}}
        2. Check infrastructure metrics around {{unixtime()}}
        3. Validate request parameters and authentication
        {{outputs['api-test'].retry_after ? "4. Retry after " + outputs['api-test'].retry_after + " seconds" : ""}}
        5. Escalate to development team if issue persists

    - name: Success Report
      uses: hello
      echo: |
        ✅ Operation Completed Successfully
          
        Correlation ID: {{vars.CORRELATION_ID}}
        Response Time: {{outputs['api-test'].response_time}}ms
        Operation ID: {{outputs['api-test'].operation_id}}
          
        Performance Metrics:
        Server Response Time: {{outputs['api-test'].server_response_time}}ms
        {{outputs['api-test'].database_time ? "Database Time: " + outputs['api-test'].database_time + "ms" : ""}}
        {{outputs['api-test'].cache_hit ? "Cache Hit: " + outputs['api-test'].cache_hit : ""}}
```

## Best Practices

The points below cover how failures are classified, what is recorded with them, and how the response escalates.

### 1. Error Classification

What to do about a failure depends on what kind it is, so the kind is derived first.

```yaml
# Good: Classify errors by type and severity
outputs:
  error_type: |
    {{res.code >= 500 ? "server_error" :
      res.code == 429 ? "rate_limit" :
      res.code >= 400 ? "client_error" : "network_error"}}
  
  severity: |
    {{res.code >= 500 ? "critical" :
      res.code == 429 ? "warning" : "error"}}
```

### 2. Contextual Information

Record what identifies the failing request, because that is what makes it findable in the server's logs.

```yaml
# Good: Capture comprehensive context
outputs:
  error_context: |
    Request ID: {{res.headers["X-Request-Id"]}}
    Timestamp: {{unixtime()}}
    User: {{vars.TEST_USER_ID}}
    Operation: {{operation_name}}
```

### 3. Recovery Strategy Selection

Once the failure is classified, the response follows from the class.

```yaml
# Good: Choose recovery strategy based on error type
- name: Recovery Strategy
  echo: |
    Recovery Strategy: {{
      error_type == "rate_limit" ? "Wait and retry" :
      error_type == "server_error" ? "Switch to backup service" :
      error_type == "client_error" ? "Fix request and retry" :
      "Investigate and escalate"
    }}
```

### 4. Progressive Error Handling

Each job tries something cheaper than the next, and only the last one gives up.

```yaml
# Good: Progressive error handling
jobs:
  quick-retry:      # Try immediate retry
  fallback-service: # Try alternative service
  cache-fallback:   # Use cached data
  manual-escalation: # Alert humans
```

## Common Error Scenarios

Three failures account for most of what a workflow meets in practice, and each calls for a different response.

### Network Connectivity Issues

A timeout distinguishes an unreachable host from a slow one.

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
    network_error: res.code == 0
```

### Authentication Failures

A 401 is rarely transient, so retrying it wastes time that reporting it would not.

```yaml
- name: Authentication Error Handler
  echo: |
    Authentication failed:
    1. Check token expiry
    2. Verify credentials
    3. Refresh authentication
```

### Rate Limiting

A 429 says when to try again, so the wait comes from the response rather than a guess.

```yaml
- name: Rate Limit Handler
  echo: |
    Rate limit exceeded:
    Retry after: {{res.headers["Retry-After"]}} seconds
    Current quota: {{res.headers["X-Rate-Limit-Remaining"]}}
```

## What's Next?

Now that you can handle errors effectively, explore:

- **[Performance Testing](/guide/how-tos/performance-testing)** - Limits on response times, and load tests with a tool made for them
- **[Environment Management](/guide/how-tos/environment-management)** - Manage configurations across environments
- **[Monitoring Workflows](/guide/how-tos/monitoring-workflows)** - Build comprehensive monitoring systems

Error handling is your safety net. Master these patterns to build workflows that gracefully handle the unexpected and recover automatically when possible.
