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
  # Fail fast for critical operations
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

    # Conditional logic based on service availability.
    # A step whose request did not get through publishes no outputs,
    # so each one is read with a default.
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
        primary_success: res.code == 200 && (rt.sec * 1000) < 5000
        primary_time: (rt.sec * 1000)
        primary_data: res.body

    # Try secondary service if primary fails or is slow
    - name: Secondary Service Attempt
      id: secondary
      uses: http
      timeout: 15s
      skipif: outputs.primary_success ?? false
      with:
        method: GET
        url: "{{vars.SECONDARY_API_URL}}/data"
      test: res.code == 200
      outputs:
        secondary_success: res.code == 200
        secondary_time: (rt.sec * 1000)
        secondary_data: res.body

    # Try cache if both primary and secondary fail
    - name: Cache Fallback
      id: cache
      uses: http
      timeout: 5s
      skipif: (outputs.primary_success ?? false) || (outputs.secondary_success ?? false)
      with:
        method: GET
        url: "{{vars.CACHE_API_URL}}/cached-data"
      test: res.code == 200
      outputs:
        cache_success: res.code == 200
        cache_time: (rt.sec * 1000)
        cache_data: res.body
        cached_data: true

    # Final fallback to static data
    - name: Static Fallback
      id: fallback
      uses: http
      skipif: (outputs.primary_success ?? false) || (outputs.secondary_success ?? false) || (outputs.cache_success ?? false)
      with:
        method: GET
        url: "{{vars.FALLBACK_API_URL}}/static-data"
      test: res.code == 200
      outputs:
        fallback_success: res.code == 200
        fallback_time: (rt.sec * 1000)
        fallback_data: res.body
        static_data: true

    # A skipped step, or one whose request did not get through,
    # publishes no outputs, so they read as nil
    - name: Service Resolution Summary
      uses: hello
      echo: |
        🎯 Service Resolution Summary:

        Resolution Path:
        {{(outputs.primary_success ?? false) ? "✅ Primary Service (optimal)" : "❌ Primary Service failed/slow (" + (outputs.primary_time != nil ? string(int(outputs.primary_time)) + "ms" : "no response") + ")"}}
        {{outputs.secondary_success == nil ? "" : outputs.secondary_success ? "✅ Secondary Service (backup)" : "❌ Secondary Service failed"}}
        {{outputs.cache_success == nil ? "" : outputs.cache_success ? "✅ Cache Service (degraded)" : "❌ Cache Service failed"}}
        {{outputs.fallback_success == nil ? "" : outputs.fallback_success ? "✅ Static Fallback (minimal)" : "❌ All services failed"}}

        Final Status: {{
          (outputs.primary_success ?? false) ? "🟢 Optimal Performance" :
          (outputs.secondary_success ?? false) ? "🟡 Backup Service Active" :
          (outputs.cache_success ?? false) ? "🟠 Degraded Mode (cached data)" :
          (outputs.fallback_success ?? false) ? "🔴 Minimal Functionality (static data)" :
          "🚨 Total Service Failure"
        }}

        Data Source: {{
          (outputs.primary_success ?? false) ? "Live Primary" :
          (outputs.secondary_success ?? false) ? "Live Secondary" :
          (outputs.cache_success ?? false) ? "Cached (may be stale)" :
          (outputs.fallback_success ?? false) ? "Static Fallback" :
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
        attempt1_success: res.code == 200
        attempt1_time: (rt.sec * 1000)
        attempt1_error_code: 'res.code != 200 ? res.code : nil'

    # Second attempt (2-second delay), skipped once an attempt succeeded
    - name: Retry Attempt 1 (2s delay)
      id: attempt2
      uses: http
      wait: 2s
      skipif: outputs.attempt1_success ?? false
      timeout: 15s
      with:
        method: GET
        url: "{{vars.UNRELIABLE_SERVICE_URL}}/data"
      test: res.code == 200
      outputs:
        attempt2_success: res.code == 200
        attempt2_time: (rt.sec * 1000)
        attempt2_error_code: 'res.code != 200 ? res.code : nil'

    # Third attempt (4-second delay)
    - name: Retry Attempt 2 (4s delay)
      id: attempt3
      uses: http
      wait: 4s
      skipif: (outputs.attempt1_success ?? false) || (outputs.attempt2_success ?? false)
      timeout: 20s
      with:
        method: GET
        url: "{{vars.UNRELIABLE_SERVICE_URL}}/data"
      test: res.code == 200
      outputs:
        attempt3_success: res.code == 200
        attempt3_time: (rt.sec * 1000)
        attempt3_error_code: 'res.code != 200 ? res.code : nil'

    # Final attempt (8-second delay)
    - name: Final Attempt (8s delay)
      id: attempt4
      uses: http
      wait: 8s
      skipif: (outputs.attempt1_success ?? false) || (outputs.attempt2_success ?? false) || (outputs.attempt3_success ?? false)
      timeout: 30s
      with:
        method: GET
        url: "{{vars.UNRELIABLE_SERVICE_URL}}/data"
      test: res.code == 200
      outputs:
        attempt4_success: res.code == 200
        attempt4_time: (rt.sec * 1000)
        attempt4_error_code: 'res.code != 200 ? res.code : nil'

    # An attempt that was skipped, or whose request did not get through,
    # publishes no outputs, so they read as nil
    - name: Retry Summary
      uses: hello
      echo: |
        🔄 Retry Pattern Results:

        Attempt History:
        1. Initial: {{outputs.attempt1_success == nil ? "❌ No response" : outputs.attempt1_success ? "✅ Success (" + string(int(outputs.attempt1_time)) + "ms)" : "❌ Failed (HTTP " + string(outputs.attempt1_error_code) + ")"}}
        {{outputs.attempt2_success == nil ? "" : "2. Retry 1: " + (outputs.attempt2_success ? "✅ Success (" + string(int(outputs.attempt2_time)) + "ms)" : "❌ Failed (HTTP " + string(outputs.attempt2_error_code) + ")")}}
        {{outputs.attempt3_success == nil ? "" : "3. Retry 2: " + (outputs.attempt3_success ? "✅ Success (" + string(int(outputs.attempt3_time)) + "ms)" : "❌ Failed (HTTP " + string(outputs.attempt3_error_code) + ")")}}
        {{outputs.attempt4_success == nil ? "" : "4. Final: " + (outputs.attempt4_success ? "✅ Success (" + string(int(outputs.attempt4_time)) + "ms)" : "❌ Failed (HTTP " + string(outputs.attempt4_error_code) + ")")}}

        Final Result: {{
          (outputs.attempt1_success ?? false) ? "✅ Success on first attempt" :
          (outputs.attempt2_success ?? false) ? "✅ Success on retry 1" :
          (outputs.attempt3_success ?? false) ? "✅ Success on retry 2" :
          (outputs.attempt4_success ?? false) ? "✅ Success on final attempt" :
          "❌ All attempts failed"
        }}

        {{
          (outputs.attempt1_success ?? false) ? "" :
          (outputs.attempt2_success ?? false) ? "Service recovered after transient failure" :
          (outputs.attempt3_success ?? false) ? "Service required multiple retries" :
          (outputs.attempt4_success ?? false) ? "Service barely recoverable" :
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
  MONITORING_API_URL: "{{MONITORING_API_URL}}"
  SERVICE_NAME: "{{SERVICE_NAME}}"
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
        circuit_open: outputs['circuit-status'].failure_count >= int(vars.CIRCUIT_BREAKER_THRESHOLD)
        # Allow probe if circuit has been open long enough
        time_since_failure: unixtime() - outputs['circuit-status'].last_failure_time
        should_probe: (unixtime() - outputs['circuit-status'].last_failure_time) > int(vars.CIRCUIT_RECOVERY_TIME)

- id: service-test
  name: Service Test with Circuit Breaker
  needs: [circuit-breaker-check]
  steps:
    # Normal operation when circuit is closed
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
        test_response_time: (rt.sec * 1000)
        error_code: 'res.code != 200 ? res.code : nil'

    # Probe test when circuit is open but recovery time has passed
    - name: Circuit Recovery Probe
      id: probe-test
      uses: http
      timeout: 5s
      skipif: '!outputs.decision.circuit_open || !outputs.decision.should_probe'
      with:
        method: GET
        url: "{{vars.MONITORED_SERVICE_URL}}/ping"  # Lighter probe
      test: res.code == 200
      outputs:
        probe_successful: res.code == 200
        probe_response_time: (rt.sec * 1000)

    # Update circuit breaker state, unless the circuit is open and no probe was due.
    # A test whose request did not get through publishes no outputs, and counts as failed.
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
              outputs['normal-test']?.test_response_time ??
              outputs['probe-test']?.probe_response_time ?? "null"
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
  SMTP_HOST: "{{SMTP_HOST}}"
  ENVIRONMENT: "{{ENVIRONMENT}}"
  DASHBOARD_URL: "{{DASHBOARD_URL}}"
  RUNBOOK_URL: "{{RUNBOOK_URL}}"

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
        status_code: res.code
        response_time: (rt.sec * 1000)
        error_details: 'res.code != 200 ? (res.rawbody ?? res.body) : nil'

    # Step 2: Detailed diagnostics if unhealthy
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

# The health check fails when the service is unhealthy, which would skip the
# jobs that need it, so they run with always and skip themselves instead
- id: automated-recovery
  name: Automated Recovery Procedures
  needs: [health-monitoring]
  always: true
  skipif: outputs['health-check']?.healthy ?? false
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

    # Recovery Attempt 2: Force restart if graceful failed
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

    # Verify force restart
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

    # Recovery Attempt 3: Scale up new instances
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

    # Final health check
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

    # Escalation notification if recovery failed
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

## Comprehensive Error Context

What gets collected at the moment of failure decides whether the report is actionable later.

### Error Information Collection

Collect comprehensive error information for debugging:

```yaml
name: Comprehensive Error Context Collection
description: Collect detailed error information for effective debugging

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
        # Success indicators
        operation_successful: res.code == 200 && res.body.success == true

        # Response metadata
        status_code: res.code
        response_time: (rt.sec * 1000)
        response_size: len(res.rawbody ?? res.body)
        content_type: res.headers["Content-Type"]

        # Error context (only populated on failure)
        error_message: 'res.code != 200 ? res.body.error?.message : nil'
        error_code: 'res.code != 200 ? res.body.error?.code : nil'
        error_details: 'res.code != 200 ? res.body.error?.details : nil'
        trace_id: res.headers["X-Trace-Id"]
        request_id: res.headers["X-Request-Id"]

        # Performance context
        server_response_time: res.headers["X-Response-Time"]
        database_time: res.body.debug?.database_time_ms
        cache_hit: res.body.debug?.cache_hit

        # Business context
        affected_user: res.body.error?.affected_user
        operation_id: res.body.operation_id
        retry_after: res.headers["Retry-After"]

    # Runs only when the request got a response that was not a success
    - name: Error Analysis and Enrichment
      uses: hello
      id: error-analysis
      skipif: outputs['api-test']?.operation_successful ?? true
      echo: "Analyzing error context"
      outputs:
        # Classify error type
        error_category: |
          outputs['api-test'].status_code >= 500 ? "server_error" :
            outputs['api-test'].status_code == 429 ? "rate_limit" :
            outputs['api-test'].status_code >= 400 && outputs['api-test'].status_code < 500 ? "client_error" :
            "unknown"

        # Determine severity
        severity_level: |
          outputs['api-test'].status_code >= 500 ? "high" :
            outputs['api-test'].status_code == 429 ? "medium" :
            outputs['api-test'].status_code >= 400 && outputs['api-test'].status_code < 500 ? "low" :
            "critical"

        # Generate troubleshooting hints
        troubleshooting_hints: |
          outputs['api-test'].status_code == 401 ? "Check authentication token expiry and permissions" :
            outputs['api-test'].status_code == 403 ? "Verify user has required permissions for this operation" :
            outputs['api-test'].status_code == 404 ? "Confirm API endpoint exists and user/resource exists" :
            outputs['api-test'].status_code == 409 ? "Resource conflict - check for duplicate operations" :
            outputs['api-test'].status_code == 429 ? "Rate limit exceeded - implement backoff or check quota" :
            outputs['api-test'].status_code >= 500 ? "Server error - check application logs and infrastructure" :
            "Unexpected response - compare it with the API contract"

        # Context for debugging
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

## Best Practices

The points below cover how failures are classified, what is recorded with them, and how the response escalates.

### 1. Error Classification

What to do about a failure depends on what kind it is, so the kind is derived first.

```yaml
# Good: Classify errors by type and severity
outputs:
  error_type: |
    res.code >= 500 ? "server_error" :
      res.code == 429 ? "rate_limit" :
      res.code >= 400 ? "client_error" : "none"

  severity: |
    res.code >= 500 ? "critical" :
      res.code == 429 ? "warning" : "error"
```

### 2. Contextual Information

Record what identifies the failing request, because that is what makes it findable in the server's logs.

```yaml
# Good: Capture comprehensive context
outputs:
  error_context: |
    "Request ID: " + (res.headers["X-Request-Id"] ?? "-") + "\n" +
    "Timestamp: " + string(unixtime()) + "\n" +
    "User: " + vars.TEST_USER_ID + "\n" +
    "Operation: " + vars.operation_name
```

### 3. Recovery Strategy Selection

Once the failure is classified, the response follows from the class.

```yaml
# Good: Choose recovery strategy based on error type
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

### 4. Progressive Error Handling

Each job tries something cheaper than the next, and only the last one gives up.

```yaml fragment
# Good: Progressive error handling
jobs:
- id: quick-retry        # Try immediate retry
- id: fallback-service   # Try alternative service
- id: cache-fallback     # Use cached data
- id: manual-escalation  # Alert humans
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
```

### Authentication Failures

A 401 is rarely transient, so retrying it wastes time that reporting it would not.

```yaml
- name: Authentication Error Handler
  uses: hello
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

## What's Next?

Now that you can handle errors effectively, explore:

- **[Performance Testing](/guide/how-tos/performance-testing)** - Limits on response times, and load tests with a tool made for them
- **[Environment Management](/guide/how-tos/environment-management)** - Manage configurations across environments
- **[Monitoring Workflows](/guide/how-tos/monitoring-workflows)** - Build comprehensive monitoring systems

Error handling is your safety net. Master these patterns to build workflows that gracefully handle the unexpected and recover automatically when possible.
