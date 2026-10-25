# Jobs and Steps

Jobs and steps are the building blocks of Probe workflows. Understanding their mechanics, execution model, and interaction patterns is crucial for building effective automation. This guide explores the detailed behavior and advanced use cases.

## Job Fundamentals

A **job** is a logical grouping of related steps that execute together as a unit. Jobs provide:

- **Isolation**: Each job runs in its own context
- **Parallelism**: Jobs can run simultaneously unless dependencies exist
- **State Management**: Jobs track their execution state and results
- **Output Sharing**: Jobs can produce outputs for other jobs to consume

### Job Structure

A job definition takes the properties below.

```yaml
jobs:
- id: other-job
  name: Other Job
  steps:
    - name: Prepare
      uses: hello

- id: job-id               # Optional: Identifier that needs refers to
  name: Human Readable Name # Required: Display name
  needs: [other-job]       # Optional: Job dependencies
  steps:                   # Required: Array of steps
    - name: Say hello
      uses: hello
```

### Job Lifecycle

Jobs progress through several states during execution:

1. **Pending**: Job is queued for execution
2. **Running**: Job is actively executing steps
3. **Success**: All steps completed successfully
4. **Failed**: One or more steps failed
5. **Skipped**: Job was skipped due to conditions
6. **Cancelled**: Job was cancelled due to timeout or error

### Job Dependencies

Use the `needs` keyword to create execution dependencies:

```yaml
vars:
  DB_API: "{{DB_API}}"
  API_URL: "{{API_URL}}"

jobs:
- id: setup
  name: Environment Setup
  steps:
    - name: Initialize Database
      id: setup
      uses: http
      with:
        method: GET
        url: "{{vars.DB_API}}/init"
      test: res.code == 200
      outputs:
        db_session_id: res.body.session_id

- id: test-suite-a
  name: API Test Suite A
  needs: [setup]           # Wait for setup to complete
  steps:
    - name: Test User API
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users"
        headers:
          X-Session-ID: "{{outputs.setup.db_session_id}}"
      test: res.code == 200

- id: test-suite-b
  name: API Test Suite B
  needs: [setup]           # Also depends on setup
  steps:
    - name: Test Order API
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/orders"
        headers:
          X-Session-ID: "{{outputs.setup.db_session_id}}"
      test: res.code == 200

- id: cleanup
  name: Environment Cleanup
  needs: [test-suite-a, test-suite-b]  # Wait for both test suites
  steps:
    - name: Clean Database
      uses: http
      with:
        method: GET
        url: "{{vars.DB_API}}/cleanup"
        headers:
          X-Session-ID: "{{outputs.setup.db_session_id}}"
      test: res.code == 200
```

### Conditional Job Execution

A job runs unless its `skipif` expression is true. The expression can read `vars` and the `outputs` published by the jobs it depends on.

A failing job skips everything that depends on it, so Detailed Health Check needs no `skipif` of its own. A job with [`always`](/reference/yaml-configuration#always) runs once its `needs` are over, however they ended, and its `skipif` decides from what was published. `outputs.ping` is missing when the request itself failed, such as a refused connection, so `??` counts that as unresponsive.

```yaml
vars:
  service_url: "{{SERVICE_URL}}"

jobs:
- id: health-check
  name: Basic Health Check
  steps:
    - name: Ping Service
      id: ping
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/ping"
      test: res.code == 200
      outputs:
        service_responsive: res.code == 200

- name: Detailed Health Check
  needs: [health-check]
  steps:
    - name: Deep Health Check
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/health/detailed"
      test: res.code == 200

- name: Send Notification
  needs: [health-check]
  always: true
  skipif: outputs.ping?.service_responsive ?? false
  steps:
    - name: Alert Team
      uses: hello
      echo: "{{vars.service_url}} did not respond to ping"
```

A job can also be skipped on configuration alone:

```yaml
- name: Production smoke test
  skipif: vars.environment != "production"
  steps:
    - name: Check
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/health"
      test: res.code == 200
```


## Step Fundamentals

A **step** is the smallest unit of execution in Probe. Each step performs a specific action and can:

- Execute actions (HTTP requests, commands, etc.)
- Test results with assertions
- Produce outputs for other steps
- Echo messages to the console
- Execute conditionally

### Step Structure

A step definition takes the properties below.

```yaml
steps:
  - name: Step Name          # Optional: Descriptive name
    id: step-id             # Optional: Unique identifier for referencing
    uses: http              # Required: Action to execute
    with:                   # Optional: Action parameters
      url: https://api.example.com
      method: GET
    test: res.code == 200 # Optional: Test condition
    outputs:                # Optional: Data to pass to other steps
      response_time: (rt.sec * 1000)
      user_count: res.body.total_users
    echo: "Message"         # Optional: Display message
    timeout: 30s            # Optional: Step timeout
```

### Step Types

A step either invokes an action, prints something, or does both in one.

#### 1. Action Steps

Execute specific actions like HTTP requests:

```yaml
- name: Check User API
  uses: http
  with:
    url: "{{vars.API_URL}}/users/{{vars.TEST_USER_ID}}"
    method: GET
    headers:
      Authorization: "Bearer {{vars.API_TOKEN}}"
      Accept: "application/json"
  test: res.code == 200 && res.body.user.active == true
  outputs:
    user_id: res.body.user.id
    user_email: res.body.user.email
    last_login: res.body.user.last_login
```

#### 2. Echo Steps

Display messages or computed values. Every step names an action, so a step that only prints uses `hello`:

```yaml
- name: Display Results
  uses: hello
  echo: |
    Test Results Summary:

    User ID: {{outputs['previous-step'].user_id}}
    Email: {{outputs['previous-step'].user_email}}
    Last Login: {{outputs['previous-step'].last_login}}

    Response Time: {{outputs['previous-step'].response_time}}ms
    Test Completed: {{unixtime()}}
```

#### 3. Hybrid Steps

Combine actions with echo messages:

```yaml
- name: Test and Report
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/status"
  test: res.code == 200
  echo: |
    API Status Check:
    Status Code: {{res.status}}
    Response Time: {{rt.duration}}
    API Version: {{res.body.version}}
```

### Step Execution Flow

Steps within a job execute sequentially by default:

```yaml
vars:
  API_URL: "{{API_URL}}"

jobs:
- id: sequential-test
  name: Sequential Step Execution
  steps:
    - name: Step 1 - Setup
      id: setup
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/setup"
      test: res.code == 200
      outputs:
        session_id: res.body.session_id

    - name: Step 2 - Execute Test
      id: test
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/test"
        headers:
          X-Session-ID: "{{outputs.setup.session_id}}"
      test: res.code == 200
      outputs:
        test_result: res.body.result

    - name: Step 3 - Cleanup
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/cleanup"
        headers:
          X-Session-ID: "{{outputs.setup.session_id}}"
      test: res.code == 200

    - name: Step 4 - Report
      uses: hello
      echo: "Test completed with result: {{outputs.test.test_result}}"
```

### Conditional Step Execution

A step is skipped when its `skipif` expression is true. The expression sees the same context as `test`, including the `outputs` of earlier steps.

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


## Advanced Patterns

The patterns below go beyond running steps in order: recovering from a failure, collecting results across steps, and configuring a step from values computed earlier.

### 1. Error Recovery Patterns

Implement robust error handling with recovery steps. Each endpoint is tried only when those before it failed, and the report shows `-` for one that was not tried:

```yaml
vars:
  SERVICE_URL: "{{SERVICE_URL}}"

jobs:
- id: resilient-check
  name: Resilient Service Check
  steps:
    - name: Attempt Primary Connection
      id: primary-attempt
      uses: http
      timeout: 10s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/api/v1/health"
      test: res.code == 200
      outputs:
        primary_success: res.code == 200

    - name: Try Alternative Endpoint
      id: alt-attempt
      uses: http
      skipif: outputs.primary_success ?? false
      timeout: 15s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/api/v2/health"
      test: res.code == 200
      outputs:
        alt_success: res.code == 200

    - name: Fallback to Legacy Endpoint
      id: legacy-attempt
      uses: http
      skipif: (outputs.primary_success ?? false) || (outputs.alt_success ?? false)
      timeout: 20s
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/health"
      test: res.code == 200
      outputs:
        legacy_success: res.code == 200

    - name: Final Status Report
      uses: hello
      echo: |
        Service Health Check Results:

        Primary API (v1): {{(outputs.primary_success ?? false) ? "✅" : "❌"}}
        Alternative API (v2): {{outputs.alt_success == nil ? "-" : outputs.alt_success ? "✅" : "❌"}}
        Legacy API: {{outputs.legacy_success == nil ? "-" : outputs.legacy_success ? "✅" : "❌"}}

        Overall Status: {{
          (outputs.primary_success ?? false) ||
          (outputs.alt_success ?? false) ||
          (outputs.legacy_success ?? false) ? "HEALTHY" : "DOWN"
        }}
```

### 2. Data Collection and Aggregation

Collect data across multiple steps for analysis:

```yaml
vars:
  BASE_URL: "{{BASE_URL}}"

jobs:
- id: performance-analysis
  name: Performance Analysis
  steps:
    - name: Test Homepage
      id: homepage
      uses: http
      with:
        method: GET
        url: "{{vars.BASE_URL}}/"
      test: res.code == 200
      outputs:
        homepage_time: (rt.sec * 1000)
        # rawbody is a JSON body as received; any other body is res.body itself
        homepage_size: len(res.rawbody ?? res.body)

    - name: Test API Endpoint
      id: api
      uses: http
      with:
        method: GET
        url: "{{vars.BASE_URL}}/api/users"
      test: res.code == 200
      outputs:
        api_time: (rt.sec * 1000)
        api_size: len(res.rawbody ?? res.body)

    - name: Test Search Function
      id: search
      uses: http
      with:
        method: GET
        url: "{{vars.BASE_URL}}/search?q=test"
      test: res.code == 200
      outputs:
        search_time: (rt.sec * 1000)
        search_size: len(res.rawbody ?? res.body)

    - name: Performance Summary
      uses: hello
      echo: |
        Performance Analysis Results:

        Homepage:
          Response Time: {{outputs.homepage.homepage_time}}ms
          Size: {{outputs.homepage.homepage_size}} bytes

        API Endpoint:
          Response Time: {{outputs.api.api_time}}ms
          Size: {{outputs.api.api_size}} bytes

        Search Function:
          Response Time: {{outputs.search.search_time}}ms
          Size: {{outputs.search.search_size}} bytes

        Average Response Time: {{
          (outputs.homepage.homepage_time + 
           outputs.api.api_time + 
           outputs.search.search_time) / 3
        }}ms

        Total Data Transfer: {{
          outputs.homepage.homepage_size + 
          outputs.api.api_size + 
          outputs.search.search_size
        }} bytes
```

### 3. Dynamic Step Configuration

Configure steps based on runtime conditions. The first step reads the service's configuration, and the `skipif` of each later step decides from it whether that step applies:

```yaml
vars:
  SERVICE_URL: "{{SERVICE_URL}}"

jobs:
- id: adaptive-monitoring
  name: Adaptive Monitoring
  steps:
    - name: Determine Environment
      id: env-detect
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/config"
      test: res.code == 200
      outputs:
        environment: res.body.environment
        feature_flags: res.body.features
        monitoring_level: res.body.monitoring.level

    - name: Basic Health Check
      uses: http
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/health"
      test: res.code == 200

    - name: Detailed Monitoring
      id: detailed
      uses: http
      skipif: outputs.monitoring_level != "detailed"
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/metrics"
      test: res.code == 200
      outputs:
        cpu_usage: res.body.system.cpu_percent
        memory_usage: res.body.system.memory_percent

    - name: Feature-Specific Tests
      uses: http
      skipif: '!("beta" in (outputs.feature_flags ?? []))'
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/beta/features"
      test: res.code == 200

    - name: Production Alerts
      uses: hello
      skipif: |
        outputs.environment != "production" ||
        ((outputs.detailed?.cpu_usage ?? 0) < 80 && (outputs.detailed?.memory_usage ?? 0) < 80)
      echo: |
        🚨 PRODUCTION ALERT: High resource usage detected!
        CPU: {{outputs.detailed.cpu_usage}}%
        Memory: {{outputs.detailed.memory_usage}}%
```

## Step and Job Identification

Referring to a result requires naming the thing that produced it. Step IDs and job names are what those references use.

### Step IDs

Use `id` to reference steps from other parts of the workflow:

```yaml
steps:
  - name: User Authentication Test
    id: auth-test                    # Define ID for referencing
    uses: http
    with:
      url: "{{vars.API_URL}}/auth/login"
      method: POST
      body: |
        {
          "username": "testuser",
          "password": "{{vars.TEST_PASSWORD}}"
        }
    test: res.code == 200
    outputs:
      auth_token: res.body.token
      user_id: res.body.user.id

  - name: User Profile Test
    uses: http
    with:
      method: GET
      url: "{{vars.API_URL}}/users/{{outputs['auth-test'].user_id}}"  # Reference by ID
      headers:
        Authorization: "Bearer {{outputs['auth-test'].auth_token}}"   # Reference by ID
    test: res.code == 200
```

### Job References

Reference job results from other jobs. API Functionality runs only once Database Connectivity has passed, and reads what its step published:

```yaml
vars:
  DB_API: "{{DB_API}}"
  API_URL: "{{API_URL}}"

jobs:
- id: database-check
  name: Database Connectivity
  steps:
    - name: Test Database
      id: db
      uses: http
      with:
        method: GET
        url: "{{vars.DB_API}}/ping"
      test: res.code == 200
      outputs:
        db_version: res.body.version

- id: api-check
  name: API Functionality
  needs: [database-check]
  steps:
    - name: Test API
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health"
      test: res.code == 200

    - name: Report
      uses: hello
      echo: "API checked against database {{outputs.db.db_version}}"
```

## Performance Optimization

A run is as slow as its longest chain of dependencies. Shortening that chain, and keeping each step cheap, is where the time is won.

### 1. Parallel Job Execution

Structure jobs to run in parallel when possible:

```yaml
vars:
  FRONTEND_URL: "{{FRONTEND_URL}}"
  BACKEND_URL: "{{BACKEND_URL}}"
  DB_URL: "{{DB_URL}}"
  APP_URL: "{{APP_URL}}"

jobs:
  # These jobs can run in parallel (no dependencies)
- id: frontend-test
  name: Frontend Tests
  steps:
    - name: Test UI Components
      uses: http
      with:
        method: GET
        url: "{{vars.FRONTEND_URL}}"
      test: res.code == 200

- id: backend-test
  name: Backend Tests
  steps:
    - name: Test API Endpoints
      uses: http
      with:
        method: GET
        url: "{{vars.BACKEND_URL}}/api"
      test: res.code == 200

- id: database-test
  name: Database Tests
  steps:
    - name: Test Database Connection
      uses: http
      with:
        method: GET
        url: "{{vars.DB_URL}}/health"
      test: res.code == 200

# This job waits for all parallel jobs to complete
- id: integration-test
  name: Integration Tests
  needs: [frontend-test, backend-test, database-test]
  steps:
    - name: End-to-End Test
      uses: http
      with:
        method: GET
        url: "{{vars.APP_URL}}/integration-test"
      test: res.code == 200
```

### 2. Efficient Resource Usage

Optimize step execution for better resource utilization:

```yaml
vars:
  SERVICE_URL: "{{SERVICE_URL}}"

jobs:
- id: efficient-monitoring
  name: Efficient Resource Monitoring
  steps:
    # Use timeouts to prevent hanging
    - name: Quick Health Check
      uses: http
      timeout: 5s                    # Short timeout for ping
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/ping"
      test: res.code == 200
      outputs:
        responsive: res.code == 200

    # Conditional expensive operations
    - name: Detailed Analysis
      uses: http
      skipif: "!(outputs.responsive ?? false)"
      timeout: 30s                   # Longer timeout for detailed analysis
      with:
        method: GET
        url: "{{vars.SERVICE_URL}}/detailed-metrics"
      test: res.code == 200

    # Batch related operations
    - name: Batch Status Check
      uses: http
      with:
        url: "{{vars.SERVICE_URL}}/batch-status"
        method: POST
        body: |
          {
            "checks": [
              {"type": "health", "endpoint": "/health"},
              {"type": "metrics", "endpoint": "/metrics"},
              {"type": "version", "endpoint": "/version"}
            ]
          }
      test: res.code == 200 && res.body.all_passed == true
```

## Best Practices

The points below cover how much to put in one job, how to name steps, and how failures are handled.

### 1. Job Granularity

Strike the right balance in job size:

```yaml fragment
# Good: Focused, cohesive jobs
jobs:
- id: authentication-tests
  name: Authentication System Tests
  steps:
    - name: Test Login
    - name: Test Logout
    - name: Test Token Refresh
    - name: Test Password Reset

- id: user-management-tests
  name: User Management Tests
  steps:
    - name: Test User Creation
    - name: Test User Update
    - name: Test User Deletion

# Avoid: Overly granular jobs
jobs:
- id: test-login
  name: test-login
  steps:
    - name: Test Login
- id: test-logout
  name: test-logout
  steps:
    - name: Test Logout

# Avoid: Monolithic jobs
jobs:
- id: all-tests
  name: all-tests
  steps:
    - name: Test Login
    - name: Test Database
    - name: Test Email
    - name: Test Files
    # ... 50 more unrelated steps
```

### 2. Clear Step Names

Use descriptive, action-oriented step names:

```yaml
steps:
  # Good: Clear, specific names
  - name: Verify User Registration API Returns 201
  - name: Test Database Connection Pool Health
  - name: Validate JWT Token Expiration Logic
  - name: Check Email Service Rate Limiting

  # Avoid: Vague or generic names
  - name: Test API           # Too vague
  - name: Check Thing        # Not descriptive
  - name: Step 1             # No context
```

### 3. Proper Error Handling

Implement appropriate error handling strategies:

```yaml
steps:
  # Critical step - fail fast
  - name: Verify Database Connectivity
    uses: http
    with:
      method: GET
      url: "{{vars.DB_URL}}/ping"
    test: res.code == 200

  # Non-critical step - continue on failure
  - name: Update Usage Analytics
    uses: http
    with:
      method: GET
      url: "{{vars.ANALYTICS_URL}}/update"
    test: res.code == 200

  # Recovery step
  - name: Log Failure Details
    uses: hello
    echo: "Analytics update failed, but continuing with main workflow"
```

## What's Next?

Now that you understand jobs and steps in detail, explore:

1. **[Actions](/guide/concepts/actions)** - Learn about the action system and available actions
2. **[Expressions and Templates](/guide/concepts/expressions-and-templates)** - Master dynamic configuration and testing
3. **[Data Flow](/guide/concepts/data-flow)** - Understand how data moves through workflows

Jobs and steps are the execution engine of Probe. Master these concepts to build efficient, reliable, and maintainable automation workflows.
