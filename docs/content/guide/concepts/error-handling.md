# Error Handling

Error handling is crucial for building resilient workflows that can gracefully manage failures and unexpected conditions. This guide explores error handling strategies, recovery patterns, and techniques for building fault-tolerant automation.

## Error Handling Fundamentals

Probe provides several mechanisms for handling errors at different levels of your workflow:

1. **Step-level**: Control how individual steps respond to failures
2. **Job-level**: Manage job failure behavior and recovery
3. **Workflow-level**: Handle overall workflow failure scenarios
4. **Conditional execution**: Route execution based on success/failure states

### Error Types in Probe

Probe recognizes several types of errors:

```yaml
# Test failure - assertion fails
- name: API Health Check
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/health"
  test: res.code == 200  # This can fail

# Action failure - HTTP timeout, connection error, etc.
- name: Timeout Example
  uses: http
  timeout: 5s  # May timeout
  with:
    method: GET
    url: "{{vars.SLOW_SERVICE}}"

# Configuration error - invalid URL, missing environment variable
- name: Configuration Error
  uses: http
  with:
    method: GET
    url: "{{vars.MISSING_VAR}}/endpoint"  # May be undefined
```

## Step-Level Error Handling

At the level of a single step, there are two choices: let the run go on past the failure, or fall back to something that still produces a usable result.

### Continue on Error

Control whether workflow execution continues when a step fails:

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

A step runs whether or not the one before it failed, so every check gets its own line in the report. When the later steps only work on what an earlier one made, set [`fail_fast`](/reference/yaml-configuration#fail_fast) on the job: the steps after a failed one are then skipped, and the report shows the one failure rather than those that follow from it.

```yaml
vars:
  API: "{{API_URL}}"

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

    - name: Place an order          # skipped when the login failed
      uses: http
      with:
        post: "{{vars.API}}/orders"
        headers:
          authorization: "Bearer {{outputs.login.token}}"
      test: res.code == 201
```

### Graceful Degradation

Handle non-critical failures gracefully:

```yaml
vars:
  PRIMARY_SERVICE_URL: "{{PRIMARY_SERVICE_URL}}"
  SECONDARY_SERVICE_URL: "{{SECONDARY_SERVICE_URL}}"
  CACHE_SERVICE_URL: "{{CACHE_SERVICE_URL}}"

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
      skipif: outputs.primary?.primary_healthy ?? false
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

        Primary Service: {{(outputs.primary?.primary_healthy ?? false) ? "✅ Healthy" : "❌ Down"}}
        {{(outputs.primary?.primary_healthy ?? false) ? "Response Time: " + string(outputs.primary.primary_response_time) + "ms" : ""}}

        {{outputs.secondary == nil ? "" : "Fallback Service: " + (outputs.secondary.secondary_healthy ? "✅ Healthy" : "❌ Down")}}
        {{(outputs.secondary?.secondary_healthy ?? false) ? "Response Time: " + string(outputs.secondary.secondary_response_time) + "ms" : ""}}

        Cache Service: {{(outputs.cache?.cache_healthy ?? false) ? "✅ Healthy" : "❌ Down"}}

        Overall Status: {{
          (outputs.primary?.primary_healthy ?? false) || (outputs.secondary?.secondary_healthy ?? false) ?
          "✅ Operational" : "❌ Service Unavailable"
        }}
```

## Job-Level Error Handling

A failing job affects the jobs that declare `needs` on it. Conditions decide whether those jobs still run.

### Job Dependencies and Failure Propagation

Control how job failures affect dependent jobs:

```yaml
vars:
  DB_URL: "{{DB_URL}}"
  QUEUE_URL: "{{QUEUE_URL}}"
  API_URL: "{{API_URL}}"

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
  needs: [infrastructure-check]  # Only runs if infrastructure is healthy
  steps:
    - name: API Tests
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/test"
      test: res.code == 200

- name: Performance Tests
  needs: [application-test]  # Skipped too when application-test is skipped
  steps:
    - name: API Response Time
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/test"
      test: res.code == 200 && rt.sec < 1
```

When the infrastructure check fails, Application Tests is skipped, and so is Performance Tests, which needs it. The run fails. A notification that has to run on a failure therefore cannot be a job that `needs` the checks; it is a step of the same job, as in [Error-Driven Notifications](#error-driven-notifications).

### Cleaning Up After a Failure

A job that removes what the others made has to run when they fail too. `always: true` runs a job once every job in its `needs` is over, however each ended:

```yaml
vars:
  API_URL: "{{API_URL}}"

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

The run still fails when `tests` did. See [`always`](/reference/yaml-configuration#always) for what the job can rely on.

### Conditional Job Execution

Apart from `always`, there is no way to branch on whether another job failed - a failing job skips everything that depends on it. To act on an outcome, publish it as an output and let the next job decide with `skipif`.

```yaml
vars:
  service_url: "{{SERVICE_URL}}"
  admin_api: "{{ADMIN_API}}"
  smtp_addr: "{{SMTP_ADDR}}"
  service_name: "{{SERVICE_NAME}}"

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
      # 503 is the service saying it is unhealthy, which the outputs report;
      # any other answer means the check itself is broken.
      test: res.code == 200 || res.code == 503
      outputs:
        service_healthy: res.code == 200
        error_body: 'res.code != 200 ? res.body : nil'

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
      test: res.code == 200
      outputs:
        restart_successful: res.code == 200

    - name: Verify Recovery
      id: verify
      uses: http
      skipif: "!(outputs.restart?.restart_successful ?? false)"
      with:
        method: GET
        url: "{{vars.service_url}}/health"
      test: res.code == 200
      outputs:
        recovery_successful: res.code == 200

- name: Escalate
  needs: [health-check, recovery-procedures]
  always: true
  skipif: (outputs.health?.service_healthy ?? false) || (outputs.recovery_successful ?? false)
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

The recovery steps test what they did, so a failed restart fails Recovery Procedures. Escalate has `always: true`, and runs then too; its `skipif` reads what was published, with `??` for a value that a failed or skipped step never published.

## Retry and Resilience Patterns

Some failures clear on their own, and some do not. Retrying suits the first; a circuit breaker keeps the second from being retried indefinitely.

### Implicit Retries with Fallbacks

Implement retry logic using conditional execution:

```yaml
vars:
  API_URL: "{{API_URL}}"
  FALLBACK_API_URL: "{{FALLBACK_API_URL}}"

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
      skipif: outputs.attempt1?.success ?? false
      wait: 2s
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
      skipif: (outputs.attempt1?.success ?? false) || (outputs.attempt2?.success ?? false)
      wait: 5s
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
      skipif: (outputs.attempt1?.success ?? false) || (outputs.attempt2?.success ?? false) || (outputs.attempt3?.success ?? false)
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

        Attempt 1: {{(outputs.attempt1?.success ?? false) ? "✅ Success (" + string(outputs.attempt1.response_time) + "ms)" : "❌ Failed"}}
        {{outputs.attempt2 == nil ? "" : "Attempt 2: " + (outputs.attempt2.success ? "✅ Success (" + string(outputs.attempt2.response_time) + "ms)" : "❌ Failed")}}
        {{outputs.attempt3 == nil ? "" : "Attempt 3: " + (outputs.attempt3.success ? "✅ Success (" + string(outputs.attempt3.response_time) + "ms)" : "❌ Failed")}}
        {{outputs.fallback == nil ? "" : "Fallback: " + (outputs.fallback.success ? "✅ Success (" + string(outputs.fallback.response_time) + "ms)" : "❌ Failed")}}

        Final Result: {{
          (outputs.attempt1?.success ?? false) || (outputs.attempt2?.success ?? false) ||
          (outputs.attempt3?.success ?? false) || (outputs.fallback?.success ?? false) ?
          "✅ API Accessible" : "❌ All attempts failed"
        }}
```

Each attempt runs only when those before it failed, and `wait` puts a pause before it. A failed attempt still fails the job even when a later one succeeds. To repeat one request until it passes, and fail only when every attempt did, use the step's [`retry`](/reference/yaml-configuration#retry) instead.

### Circuit Breaker Pattern

Implement circuit breaker logic to prevent cascading failures:

```yaml
vars:
  MONITORING_API: "{{MONITORING_API}}"
  SERVICE_NAME: "{{SERVICE_NAME}}"
  SERVICE_URL: "{{SERVICE_URL}}"

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
        circuit_open: outputs['health-history'].error_rate > 50 || outputs['health-history'].recent_failures > 10
        # An open circuit is tested again once five minutes have passed since the last success.
        should_test: |
          !(outputs['health-history'].error_rate > 50 || outputs['health-history'].recent_failures > 10) ||
          (unixtime() - outputs['health-history'].last_success) > 300

    - name: Service Test (Circuit Closed)
      id: normal-test
      uses: http
      skipif: outputs['circuit-decision'].circuit_open
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/test"
      test: res.code == 200
      outputs:
        test_successful: res.code == 200

    - name: Probe Test (Circuit Open)
      id: probe-test
      uses: http
      skipif: "!outputs['circuit-decision'].circuit_open || !outputs['circuit-decision'].should_test"
      timeout: 5s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/health"  # Lighter probe
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

        {{outputs['normal-test'] == nil ? "" : "Normal Test: " + (outputs['normal-test'].test_successful ? "✅ Passed" : "❌ Failed")}}
        {{outputs['probe-test'] == nil ? "" : "Probe Test: " + (outputs['probe-test'].probe_successful ? "✅ Passed" : "❌ Failed")}}

        {{outputs['circuit-decision'].circuit_open && !outputs['circuit-decision'].should_test ? "⏸️ Skipping tests - circuit breaker active" : ""}}
        {{(outputs['probe-test']?.probe_successful ?? false) ? "🟢 Service recovery detected - circuit may close" : ""}}
```

## Error Context and Debugging

A failure is only actionable if the run records what was happening when it occurred, and lets that record be tied back to the request that caused it.

### Comprehensive Error Information

Capture detailed error context for debugging:

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
    # rawbody is a JSON body as received; any other body is res.body itself
    response_size: len(res.rawbody ?? res.body)
    error_message: 'res.code != 200 ? res.body : nil'
    response_headers: res.headers
    partial_response: 'res.code != 200 ? (res.rawbody ?? res.body)[0:500] : nil'

- name: Error Analysis and Reporting
  uses: hello
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
    - Content Type: {{outputs['api-test'].response_headers["Content-Type"]}}

    Error Information:
    {{outputs['api-test'].error_message ?? "No error message available"}}

    Response Preview:
    {{outputs['api-test'].partial_response ?? "No response content"}}

    Troubleshooting Suggestions:
    {{outputs['api-test'].status_code == 404 ? "- Check if the endpoint URL is correct" : ""}}
    {{outputs['api-test'].status_code == 401 ? "- Verify authentication credentials" : ""}}
    {{outputs['api-test'].status_code == 403 ? "- Check user permissions" : ""}}
    {{outputs['api-test'].status_code == 500 ? "- Server error - check application logs" : ""}}
    {{outputs['api-test'].response_time > 10000 ? "- Request timed out - check network connectivity" : ""}}
```

### Error Correlation and Tracking

Track errors across multiple steps and jobs:

```yaml
vars:
  SERVICE_A_URL: "{{SERVICE_A_URL}}"
  SERVICE_B_URL: "{{SERVICE_B_URL}}"
  SERVICE_C_URL: "{{SERVICE_C_URL}}"

jobs:
- id: distributed-test
  name: Distributed System Test
  steps:
    - name: Initialize Correlation ID
      uses: hello
      id: init
      echo: "Starting distributed test"
      outputs:
        correlation_id: '"test_" + string(unixtime()) + "_" + random_str(8)'
        start_time: unixtime()

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
        error_code: 'res.code != 200 ? res.status : nil'
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
        error_code: 'res.code != 200 ? res.status : nil'
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
        error_code: 'res.code != 200 ? res.status : nil'
        trace_id: res.headers["X-Trace-Id"]

    - name: Error Correlation Report
      uses: hello
      echo: |
        🔍 Distributed Test Error Report

        Correlation ID: {{outputs.init.correlation_id}}
        Test Duration: {{unixtime() - outputs.init.start_time}} seconds

        Service Status Summary:
        - Service A: {{(outputs['service-a']?.success ?? false) ? "✅ Healthy" : "❌ Failed (" + (outputs['service-a']?.error_code ?? "no response") + ")"}}
          {{outputs['service-a']?.trace_id == nil ? "" : "Trace ID: " + outputs['service-a'].trace_id}}

        - Service B: {{(outputs['service-b']?.success ?? false) ? "✅ Healthy" : "❌ Failed (" + (outputs['service-b']?.error_code ?? "no response") + ")"}}
          {{outputs['service-b']?.trace_id == nil ? "" : "Trace ID: " + outputs['service-b'].trace_id}}

        - Service C: {{(outputs['service-c']?.success ?? false) ? "✅ Healthy" : "❌ Failed (" + (outputs['service-c']?.error_code ?? "no response") + ")"}}
          {{outputs['service-c']?.trace_id == nil ? "" : "Trace ID: " + outputs['service-c'].trace_id}}

        Error Pattern Analysis:
        {{!(outputs['service-a']?.success ?? false) && !(outputs['service-b']?.success ?? false) && !(outputs['service-c']?.success ?? false) ? "🚨 Total system failure - check infrastructure" : ""}}
        {{!(outputs['service-a']?.success ?? false) && (outputs['service-b']?.success ?? false) && (outputs['service-c']?.success ?? false) ? "⚠️ Service A isolated failure" : ""}}
        {{(outputs['service-a']?.success ?? false) && !(outputs['service-b']?.success ?? false) && (outputs['service-c']?.success ?? false) ? "⚠️ Service B isolated failure" : ""}}
        {{(outputs['service-a']?.success ?? false) && (outputs['service-b']?.success ?? false) && !(outputs['service-c']?.success ?? false) ? "⚠️ Service C isolated failure" : ""}}

        Investigation Steps:
        1. Check application logs with correlation ID: {{outputs.init.correlation_id}}
        2. Review distributed traces using trace IDs above
        3. Monitor system metrics for the test time window
        4. Verify network connectivity between services
```

## Error Recovery Strategies

Once a failure is detected, the workflow can act on it: run a recovery procedure, then verify step by step that the service is healthy again.

### Automated Recovery Procedures

Implement automated recovery for common failure scenarios:

```yaml
vars:
  SERVICE_URL: "{{SERVICE_URL}}"
  ADMIN_API: "{{ADMIN_API}}"
  SERVICE_NAME: "{{SERVICE_NAME}}"

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
        status_code: res.status

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
      skipif: outputs['health-check']?.healthy ?? false
      with:
        url: "{{vars.ADMIN_API}}/services/{{vars.SERVICE_NAME}}/restart"
        method: POST
      test: res.code == 200
      outputs:
        restart_initiated: res.code == 200

    # A service already restarted for its health is not restarted again.
    - name: Restart Service (Memory Issue)
      id: restart-memory
      uses: http
      skipif: (outputs['memory-check']?.memory_ok ?? true) || outputs['restart-health'] != nil
      with:
        url: "{{vars.ADMIN_API}}/services/{{vars.SERVICE_NAME}}/restart"
        method: POST
      test: res.code == 200
      outputs:
        restart_initiated: res.code == 200

    - name: Wait for Service Recovery
      id: recovery-wait
      uses: http
      skipif: outputs['restart-health'] == nil && outputs['restart-memory'] == nil
      retry:
        max_attempts: 6
        interval: 10s
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

        Initial Health Check: {{(outputs['health-check']?.healthy ?? false) ? "✅ Healthy" : "❌ Failed (" + (outputs['health-check']?.status_code ?? "no response") + ")"}}
        {{outputs['memory-check'] == nil ? "" : "Memory Usage: " + string(outputs['memory-check'].memory_usage) + "% " + (outputs['memory-check'].memory_ok ? "✅ Normal" : "⚠️ High")}}

        Recovery Actions:
        {{(outputs['restart-health']?.restart_initiated ?? false) ? "🔄 Service restarted due to health check failure" : ""}}
        {{(outputs['restart-memory']?.restart_initiated ?? false) ? "🔄 Service restarted due to high memory usage (" + string(outputs['memory-check'].memory_usage) + "%)" : ""}}

        Recovery Result:
        {{outputs['recovery-wait'] == nil ? "ℹ️ No recovery action needed" : (outputs['recovery-wait'].recovery_successful ? "✅ Service recovered successfully" : "❌ Service failed to recover")}}

        Next Actions:
        {{outputs['recovery-wait'] != nil && !outputs['recovery-wait'].recovery_successful ? "🚨 Manual intervention required - service did not recover" : ""}}
        {{(outputs['health-check']?.healthy ?? false) && (outputs['memory-check']?.memory_ok ?? true) ? "✅ All systems normal - monitoring continues" : ""}}
```

### Gradual Recovery Testing

Test recovery gradually to avoid overwhelming recovering systems:

```yaml
vars:
  SERVICE_URL: "{{SERVICE_URL}}"

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
      skipif: "!(outputs.connectivity ?? false)"
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
      skipif: "!(outputs.health_ok ?? false)"
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
      skipif: "!(outputs.light_test_passed ?? false)"
      timeout: 30s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/api/users"
      test: res.code == 200 && res.body.users != nil
      outputs:
        standard_test_passed: res.code == 200 && res.body.users != nil

    - name: Recovery Assessment
      uses: hello
      echo: |
        🏥 Recovery Assessment Report

        Recovery Progress:
        1. Basic Connectivity: {{(outputs.connectivity ?? false) ? "✅ Restored" : "❌ Failed"}}
        {{(outputs.connectivity ?? false) ? "2. Health Check: " + (outputs.health_ok ? "✅ Passed" : "❌ Failed") : "2. Health Check: ⏸️ Skipped"}}
        {{(outputs.health_ok ?? false) ? "3. Light Function Test: " + (outputs.light_test_passed ? "✅ Passed" : "❌ Failed") : "3. Light Function Test: ⏸️ Skipped"}}
        {{(outputs.light_test_passed ?? false) ? "4. Standard Load Test: " + (outputs.standard_test_passed ? "✅ Passed" : "❌ Failed") : "4. Standard Load Test: ⏸️ Skipped"}}

        Recovery Status: {{
          (outputs.standard_test_passed ?? false) ? "🟢 FULLY RECOVERED" :
          (outputs.light_test_passed ?? false) ? "🟡 PARTIALLY RECOVERED" :
          (outputs.health_ok ?? false) ? "🟡 BASIC FUNCTIONALITY RESTORED" :
          (outputs.connectivity ?? false) ? "🟡 CONNECTIVITY RESTORED" :
          "🔴 SERVICE STILL DOWN"
        }}

        Recommendations:
        {{!(outputs.connectivity ?? false) ? "- Check network connectivity and service deployment" : ""}}
        {{(outputs.connectivity ?? false) && !(outputs.health_ok ?? false) ? "- Service is starting but not ready - wait and retry" : ""}}
        {{(outputs.health_ok ?? false) && !(outputs.light_test_passed ?? false) ? "- Service health OK but functionality impaired - check dependencies" : ""}}
        {{(outputs.light_test_passed ?? false) && !(outputs.standard_test_passed ?? false) ? "- Service functional but may be under load - monitor performance" : ""}}
        {{(outputs.standard_test_passed ?? false) ? "- Service fully operational - resume normal monitoring" : ""}}
```

## Notification and Alerting

A workflow that runs unattended has to report failures itself. Notification steps run on the error path and carry the context collected there.

### Error-Driven Notifications

A job that `needs` a failed job is skipped, so an alert is a step of the same job as the checks it reports on. Each check publishes what it found, and the alert runs only when that says something is wrong. A step after a failed one still runs, so the alert is reached.

```yaml
vars:
  api_url: "{{API_URL}}"
  smtp_host: "{{SMTP_HOST}}"
  service_name: "{{SERVICE_NAME}}"
  environment: "{{ENVIRONMENT}}"

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

    # outputs.health is missing when the request itself failed, such as a
    # refused connection, so the fallback counts that as unhealthy.
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

The smtp action sends a generated test message and has no parameter for a body, so the details go to `echo`, which puts them in the run's report.

## Best Practices

The points below decide how much a workflow absorbs before it gives up, and what it leaves behind when it does.

### 1. Fail Fast vs. Resilience Balance

A step on the critical path should stop the run; one that only adds detail should not.

```yaml
# Critical path - fail fast
- name: Database Connection
  uses: http
  with:
    method: GET
    url: "{{vars.DB_URL}}/ping"
  test: res.code == 200

# Non-critical path - be resilient
- name: Analytics Tracking
  uses: http
  with:
    method: GET
    url: "{{vars.ANALYTICS_URL}}/track"
  test: res.code == 200
```

### 2. Error Context Preservation

Capture what the failing step saw, because the next step no longer has access to it.

```yaml
# Good: Preserve error context
outputs:
  success: res.code == 200
  error_details: |
    res.code != 200 ?
      "HTTP " + res.status + ": " + (res.rawbody ?? res.body)[0:200] :
      nil
  debug_info: |
    res.code != 200 ?
      "URL: " + vars.API_URL + ", Time: " + string(rt.sec * 1000) + "ms" :
      nil
```

### 3. Graduated Response

The response should match the severity, from a log line to an alert.

```yaml
# Good: Different responses for different error types
- name: Error Response Strategy
  uses: hello
  echo: |
    Error Response:
    {{outputs['api-test'].status_code == 500 ? "🚨 Server error - escalate immediately" : ""}}
    {{outputs['api-test'].status_code == 404 ? "⚠️ Endpoint not found - check configuration" : ""}}
    {{outputs['api-test'].status_code == 401 ? "🔐 Authentication failed - refresh credentials" : ""}}
```

### 4. Error Recovery Documentation

Printing the recovery procedure with the failure puts it where whoever is paged will see it.

```yaml
# Document recovery procedures in workflow
- name: Recovery Instructions
  uses: hello
  echo: |
    🛠️ Recovery Procedures for {{vars.SERVICE_NAME}}:

    1. Check service logs: kubectl logs -l app={{vars.SERVICE_NAME}}
    2. Verify configuration: check {{vars.CONFIG_PATH}}
    3. Restart service: kubectl rollout restart deployment/{{vars.SERVICE_NAME}}
    4. Monitor recovery: watch kubectl get pods -l app={{vars.SERVICE_NAME}}

    Escalation: If service doesn't recover in 10 minutes, contact on-call team.
```

## What's Next?

Now that you understand error handling, explore:

1. **[Execution Model](/guide/concepts/execution-model)** - Learn how Probe executes workflows
2. **[File Merging](/guide/concepts/file-merging)** - Understand configuration composition
3. **[How-tos](/guide/how-tos/api-testing)** - See practical error handling patterns

Error handling is your safety net. Master these patterns to build workflows that gracefully handle the unexpected and recover automatically when possible.
