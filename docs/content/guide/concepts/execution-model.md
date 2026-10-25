# Execution Model

Understanding Probe's execution model is crucial for designing efficient workflows and troubleshooting execution issues. This guide explores how Probe schedules, executes, and manages workflow components from start to finish.

## Execution Overview

Probe follows a structured execution model that processes workflows in a predictable, deterministic manner:

1. **Workflow Parsing**: Parse and validate YAML configuration
2. **Dependency Resolution**: Build execution graph based on job dependencies
3. **Job Scheduling**: Schedule jobs for execution based on dependencies
4. **Step Execution**: Execute steps sequentially within each job
5. **State Management**: Track execution state and results
6. **Resource Cleanup**: Clean up resources after execution

### Execution Hierarchy

The nesting decides what runs at the same time: jobs side by side, steps in order within each one.

```
Workflow
├── Job 1 (independent)
│   ├── Step 1.1 (sequential)
│   ├── Step 1.2 (sequential)
│   └── Step 1.3 (sequential)
├── Job 2 (independent, parallel to Job 1)
│   ├── Step 2.1 (sequential)
│   └── Step 2.2 (sequential)
└── Job 3 (depends on Job 1 and Job 2)
    ├── Step 3.1 (sequential)
    └── Step 3.2 (sequential)
```

## Job Execution Model

Jobs without a `needs` declaration run at the same time. Adding `needs` is what turns that set into an ordered graph. A job starts as soon as the jobs it needs are done, without waiting for the other jobs that are running.

### Independent Job Execution

Jobs without dependencies execute in parallel:

```yaml
name: Parallel Service Check
description: Check multiple services simultaneously
vars:
  DB_URL: "{{DB_URL}}"
  API_URL: "{{API_URL}}"
  CACHE_URL: "{{CACHE_URL}}"

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

**Execution Timeline:**
```
Time 0: Start database-check, api-check, cache-check simultaneously
Time T: All jobs complete (T = max execution time of all jobs)
```

### Dependent Job Execution

Jobs with dependencies wait for prerequisite jobs to complete:

```yaml
name: Staged Deployment Validation
description: Validate deployment in dependency order
vars:
  DB_URL: "{{DB_URL}}"
  API_URL: "{{API_URL}}"

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

**Execution Timeline:**
```
Time 0: Start infrastructure job
Time T1: infrastructure completes → start services job
Time T2: services completes → start integration job
Time T3: integration completes → start notification job
Time T4: notification completes → workflow done
```

### Complex Dependency Graphs

Jobs can have multiple dependencies and form complex execution graphs:

```yaml
name: Layered Service Test
description: Run setup, services, integration and a report in layers

jobs:
# Foundation layer (parallel)
- id: database-setup
  name: Database Setup
  steps:
    - name: Initialize Database
      id: database-setup
      uses: hello
      with:
        session_id: "{{random_str(16)}}"
      outputs:
        db_session_id: res.session_id

- id: cache-setup
  name: Cache Setup
  steps:
    - name: Initialize Cache
      id: cache-setup
      uses: hello
      with:
        session_id: "{{random_str(16)}}"
      outputs:
        cache_session_id: res.session_id

# Service layer (depends on foundation)
- id: user-service
  name: User Service Test
  needs: [database-setup, cache-setup]
  steps:
    - name: Test User Service
      id: user-service
      uses: hello
      with:
        db_session_id: "{{outputs['database-setup'].db_session_id}}"
        cache_session_id: "{{outputs['cache-setup'].cache_session_id}}"
      outputs:
        user_service_ready: res.db_session_id != "" && res.cache_session_id != ""

- id: order-service
  name: Order Service Test
  needs: [database-setup]  # Only needs database
  steps:
    - name: Test Order Service
      id: order-service
      uses: hello
      with:
        db_session_id: "{{outputs['database-setup'].db_session_id}}"
      outputs:
        order_service_ready: res.db_session_id != ""

# Integration layer (depends on services)
- id: integration-test
  name: Integration Test
  needs: [user-service, order-service]
  steps:
    - name: Test Service Integration
      uses: hello
      echo: "Testing integration between user and order services"

# Reporting layer (depends on everything, through integration-test)
- id: final-report
  name: Final Report
  needs: [integration-test]
  steps:
    - name: Generate Report
      uses: hello
      echo: |
        Execution Report:
        Database Setup: {{outputs['database-setup'].db_session_id != nil ? "✅" : "❌"}}
        Cache Setup: {{outputs['cache-setup'].cache_session_id != nil ? "✅" : "❌"}}
        User Service: {{outputs['user-service'].user_service_ready ? "✅" : "❌"}}
        Order Service: {{outputs['order-service'].order_service_ready ? "✅" : "❌"}}
```

**Execution Timeline:**
```
Time 0: Start database-setup, cache-setup (parallel)
Time T1: Both foundation jobs complete → start user-service, order-service
Time T2: Both service jobs complete → start integration-test
Time T3: integration-test completes → start final-report
Time T4: final-report completes → workflow done
```

## Step Execution Model

Within a job, steps run one after another in the order they are written, and a step can be skipped by its condition.

### Sequential Step Execution

Within a job, steps execute sequentially in the order defined:

```yaml
vars:
  API_URL: "{{API_URL}}"

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

**Step Execution Order:**
```
Step 1 → Step 2 → Step 3 → Step 4 → Step 5
```

Each step waits for the previous step to complete before starting.

### Conditional Step Execution

Steps still run in order; `skipif` only decides whether each one executes. The expression sees the same context as `test`, including the `outputs` of earlier steps.

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


## State Management

State is what one part of a run leaves for another: a job's status, a step's outputs, and the references that reach across jobs.

### Job State Tracking

Each job ends as succeeded, failed or skipped. That state is not a value an expression can read: it decides whether the jobs that need the job run, and what the job learned reaches them through its steps' outputs:

```yaml
jobs:
- id: example-job
  name: example-job
  steps:
    - name: Example Step
      id: example
      uses: hello
      with:
        message: ready
      outputs:
        message: res.message

- id: dependent-job
  name: dependent-job
  needs: [example-job]
  steps:
    - name: Check Job States
      uses: hello
      echo: |
        Job State Information:
        example-job succeeded, so this job runs
        Message from example-job: {{outputs.example.message}}
```

### Step State and Output Management

Each step produces state and output information:

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

### Cross-Job State References

Jobs can reference state from other jobs:

```yaml
vars:
  API_URL: "{{API_URL}}"

jobs:
- id: health-check
  name: Health Check
  steps:
    - name: Check Service
      id: health-check
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health"
      test: res.code == 200
      outputs:
        service_healthy: res.code == 200

- id: performance-test
  name: Performance Test
  needs: [health-check]
  steps:
    - name: Load Test
      id: performance-test
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/load"
      test: res.code == 200
      outputs:
        avg_response_time: (rt.sec * 1000)

- id: reporting
  name: Generate Report
  needs: [health-check, performance-test]
  steps:
    - name: Status Report
      uses: hello
      echo: |
        System Status Report:

        Service healthy: {{outputs['health-check'].service_healthy}}
        Average Response Time: {{outputs['performance-test'].avg_response_time}}ms
```

## Timing and Performance

How long a run takes is decided by three things: when each job starts, how long a step may wait before it is cut off, and how much of the graph can proceed at once.

### Execution Timing

Probe tracks timing information at multiple levels:

```yaml
vars:
  API_URL: "{{API_URL}}"

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

### Timeout Management

Each step sets its own `timeout`, which is 5 minutes when it is not given. A job has no timeout of its own:

```yaml
vars:
  API_URL: "{{API_URL}}"
  DB_API: "{{DB_API}}"
  EXTERNAL_API: "{{EXTERNAL_API}}"

jobs:
- id: timeout-management
  name: Timeout Management Example
  steps:
    - name: Quick API Call
      uses: http
      timeout: 10s  # Step-level timeout
      with:
        method: GET
        url: "{{vars.API_URL}}/quick"
      test: res.code == 200

    - name: Database Query
      uses: http
      timeout: 60s  # Longer timeout for complex operation
      with:
        method: GET
        url: "{{vars.DB_API}}/complex-query"
      test: res.code == 200

    - name: External Service Call
      uses: http
      timeout: 30s  # External services may be slower
      with:
        method: GET
        url: "{{vars.EXTERNAL_API}}/data"
      test: res.code == 200
```

### Parallel Execution Optimization

Optimize workflow execution through effective parallelization:

```yaml
name: Optimized Parallel Execution
description: Efficiently organize jobs for maximum parallelism
vars:
  DB_URL: "{{DB_URL}}"
  CACHE_URL: "{{CACHE_URL}}"
  EXTERNAL_API: "{{EXTERNAL_API}}"
  USER_API: "{{USER_API}}"
  ORDER_API: "{{ORDER_API}}"
  NOTIFICATION_API: "{{NOTIFICATION_API}}"
  API_URL: "{{API_URL}}"

jobs:
  # Tier 1: Independent foundation checks (all parallel)
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

# Tier 2: Service-level checks (parallel, depend on infrastructure)
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

# Tier 3: Integration tests (depend on services)
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

# Tier 4: Final validation (depends on integration)
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

**Execution Visualization:**
```
Time 0-T1: database-check, cache-check, network-check (parallel)
Time T1-T2: user-service-test, order-service-test, notification-service-test (parallel)
Time T2-T3: user-order-integration
Time T3-T4: end-to-end-test
```

## Error Propagation and Recovery

A failure does not stop at the step that produced it. What happens next is governed by how it propagates through the graph, and by what the workflow does to recover.

### Error Propagation Model

Understanding how errors propagate through the execution model:

```yaml
vars:
  CRITICAL_SERVICE: "{{CRITICAL_SERVICE}}"
  SERVICE_URL: "{{SERVICE_URL}}"
  INDEPENDENT_SERVICE: "{{INDEPENDENT_SERVICE}}"

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
      # A failing step marks the job failed; later steps of the job still run

- id: dependent-service
  name: Dependent Service
  needs: [critical-foundation]  # Won't execute if foundation fails
  steps:
    - name: Service Test
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/test"
      test: res.code == 200

- id: resilient-check
  name: Resilient Check
  # No dependencies - always executes
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
  always: true  # Runs even when a needed job failed
  steps:
    - name: Cleanup Failed State
      uses: hello
      echo: |
        Cleaning up after failures:
```

### Recovery Execution Model

A recovery job needs the job it recovers and sets `always: true`, so it runs even when that job failed. Its steps are skipped when the main process succeeded, and `?? false` covers a request that never connected and so published nothing:

```yaml
vars:
  API_URL: "{{API_URL}}"

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
  needs: [primary-workflow]
  always: true
  steps:
    - name: Diagnose Failure
      id: diagnose
      uses: http
      skipif: outputs.main?.process_successful ?? false
      with:
        method: GET
        url: "{{vars.API_URL}}/diagnostics"
      test: res.code == 200
      outputs:
        diagnosis: res.body.issue_type

    - name: Automated Recovery
      uses: http
      skipif: outputs.main?.process_successful ?? false
      with:
        url: "{{vars.API_URL}}/recovery/auto"
        method: POST
      test: res.code == 200

    - name: Manual Recovery Alert
      uses: hello
      skipif: outputs.main?.process_successful ?? false
      echo: "🚨 Critical failure detected - manual intervention required"

- id: validation-workflow
  name: Validation Workflow
  needs: [recovery-workflow]
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


        Recovery needed: {{outputs.main?.process_successful ? "no" : "yes"}}
        System healthy: {{outputs['validation-workflow'].system_healthy}}
```

## Resource Management

Each action runs in a process of its own, so a run has processes to start and stop.

### Action Lifecycle Management

Probe starts an action for each step that uses it:

```yaml
vars:
  API_URL: "{{API_URL}}"
  SMTP_HOST: "{{SMTP_HOST}}"

jobs:
- id: multi-action-workflow
  name: Multi Action Workflow
  steps:
    # The http action is started for this step
    - name: API Test
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/test"
      test: res.code == 200

    # The smtp action is started for this step
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
      test: res.code == 0
      echo: "API test completed successfully"
    - name: Debug Message
      uses: hello
      with:
        message: "Debug checkpoint reached"

    # The http action is started again, in a new process
    - name: Follow-up API Test
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/follow-up"
      test: res.code == 200
```

Action lifecycle:
1. A process is started for the action when a step uses it
2. The step runs in it, and the process is stopped once the step has its result
3. A later step that uses the same action starts a new process; what the action keeps in the job, such as the cookies of `keep_cookies`, is handed to it by Probe

### Memory and Performance Optimization

Probe optimizes execution for performance and resource usage:

```yaml
vars:
  API_URL: "{{API_URL}}"

jobs:
- id: optimized-workflow
  name: Performance Optimized Workflow
  steps:
    # Efficient: Direct property access
    - name: User Data Collection
      id: user-data
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users"
      test: res.code == 200
      outputs:
        user_count: res.body.total_users    # Extract specific value
        first_user_id: res.body.users[0].id # Direct array access
        # Avoid: large_user_list: res.body.users (stores entire array)

    # Efficient: Conditional processing
    - name: Process Large Dataset
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/batch-process"
      test: res.code == 200

    # Efficient: Scoped outputs
    - name: Summary Generation
      uses: hello
      echo: |
        Processing Summary:
        Total users: {{outputs['user-data'].user_count}}
        First user: {{outputs['user-data'].first_user_id}}
```

## Best Practices

The points below follow from the execution model: how dependencies are shaped, what happens on failure, how much is carried in outputs, and how the flow is made legible.

### 1. Dependency Design

Declare a dependency only where the data or the ordering actually requires it; every extra one removes parallelism.

```yaml fragment
# Good: Logical dependency grouping
jobs:
- id: infrastructure
  name: infrastructure
- id: application
  name: application
  needs: [infrastructure]
- id: integration
  name: integration
  needs: [application]

# Avoid: Unnecessary dependencies
jobs:
- id: independent-check-1
  name: independent-check-1
- id: independent-check-2
  name: independent-check-2
  needs: [independent-check-1]  # Unnecessary if truly independent
```

### 2. Error Handling Strategy

Decide per step whether a failure should stop the run or be carried forward.

```yaml
# Good: Strategic error handling
- name: Critical Operation
  test: res.code == 200

- name: Optional Operation
  test: res.code == 200
```

### 3. Output Efficiency

Publish the values later steps read, not the whole response.

```yaml
# Good: Efficient outputs
outputs:
  essential_data: res.body.id
  computed_value: len(res.body.items)
  status_flag: res.code == 200

# Avoid: Storing large objects
outputs:
  # entire_response: res.body  # Could be very large
```

### 4. Execution Flow Documentation

The workflow's description is where the order it expects can be stated.

```yaml
name: Well-Documented Workflow
description: |
  Execution flow:
  1. Infrastructure validation (parallel)
  2. Service health checks (parallel, depends on infrastructure)
  3. Integration testing (sequential, depends on services)
  4. Reporting (depends on all previous stages)

  Expected execution time: 2-5 minutes
  Critical path: infrastructure → services → integration → reporting
```

## What's Next?

Now that you understand the execution model, explore:

1. **[File Merging](/guide/concepts/file-merging)** - Learn configuration composition techniques
2. **[How-tos](/guide/how-tos/api-testing)** - See practical execution patterns in action
3. **[Reference](/reference/cli-reference)** - Detailed syntax and configuration reference

Understanding the execution model helps you design efficient, predictable workflows that make optimal use of parallelism and handle failures gracefully.
