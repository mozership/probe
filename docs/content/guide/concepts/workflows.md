# Workflows

A workflow is the top-level container in Probe that defines a complete automation or monitoring process. This guide explores workflow structure, design patterns, and best practices for creating maintainable and effective workflows.

## Workflow Anatomy

Every Probe workflow consists of several key components:

```yaml
name: Workflow Name                    # Required: Human-readable name
description: What this workflow does   # Optional: Detailed description
vars:                                  # Optional: Workflow variables
  API_BASE_URL: https://api.example.com
jobs:                                  # Required: One or more jobs
- id: api-check                        # Optional: ID that other jobs refer to
  name: API Check                      # Required: Job name
  steps:                               # Required: One or more steps
    - name: Check API Health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: res.code == 200
```

### Required Components

**Name**: Every workflow must have a descriptive name that clearly identifies its purpose.

```yaml
# Good examples
name: Production API Health Check
name: E-commerce Checkout Flow Test
name: Database Migration Validation

# Avoid generic names
name: Test
name: Workflow
name: Check
```

**Jobs**: At least one job must be defined. Jobs contain the actual work to be performed.

### Optional Components

**Description**: Provides detailed context about the workflow's purpose, scope, and expected outcomes.

```yaml
description: |
  Comprehensive health check for the production API including:
  - Authentication endpoint validation
  - Core business logic verification
  - Database connectivity testing
  - Third-party service integration checks
```

**Environment Variables**: Define environment-specific or sensitive configuration.

```yaml
vars:
  API_BASE_URL: https://api.production.example.com
  TIMEOUT_SECONDS: 30
  MAX_RETRY_COUNT: 3
```

**Defaults**: Set common `with` values for the steps of a job, keyed by action name. A top-level `defaults` sets them for every job, and a job's own come before it.

```yaml
name: Health Check
vars:
  api_url: "{{API_URL}}"

jobs:
- name: Health Check
  defaults:
    http:
      url: "{{vars.api_url}}"
      headers:
        Accept: "application/json"
        User-Agent: "Probe Health Monitor v1.0"
  steps:
    - name: Ping
      uses: http
      with:
        get: /health
      test: res.code == 200
```

## Workflow Design Patterns

The shape of a workflow follows from its dependencies. Four shapes cover most cases: a straight line, independent jobs side by side, jobs grouped into stages, and a fan-out that is gathered back in.

### 1. Linear Workflow

Steps execute sequentially, each depending on the previous one's success.

```yaml
name: Database Migration
description: Execute database schema changes in order
vars:
  DB_API: "{{DB_API}}"

jobs:
- id: migration
  name: Run Migration Steps
  steps:
    - name: Backup Current Schema
      uses: http
      with:
        url: "{{vars.DB_API}}/backup"
        method: POST
      test: res.code == 200

    - name: Apply Schema Changes
      uses: http
      with:
        url: "{{vars.DB_API}}/migrate"
        method: POST
      test: res.code == 200

    - name: Verify Migration
      uses: http
      with:
        url: "{{vars.DB_API}}/schema/version"
        method: GET
      test: res.body.version == "2.1.0"

    - name: Update Documentation
      uses: hello
      echo: "Migration to v2.1.0 completed successfully"
```

**Use cases:**
- Database migrations
- Deployment pipelines
- Setup/teardown processes

### 2. Parallel Workflow

Multiple independent checks run simultaneously for efficiency.

```yaml
name: Multi-Service Health Check
description: Check health of all microservices in parallel
vars:
  USER_SERVICE_URL: "{{USER_SERVICE_URL}}"
  PAYMENT_SERVICE_URL: "{{PAYMENT_SERVICE_URL}}"
  NOTIFICATION_SERVICE_URL: "{{NOTIFICATION_SERVICE_URL}}"

jobs:
- id: user-service
  name: User Service Health
  steps:
    - name: Check User API
      uses: http
      with:
        method: GET
        url: "{{vars.USER_SERVICE_URL}}/health"
      test: res.code == 200

- id: payment-service
  name: Payment Service Health
  steps:
    - name: Check Payment API
      uses: http
      with:
        method: GET
        url: "{{vars.PAYMENT_SERVICE_URL}}/health"
      test: res.code == 200

- id: notification-service
  name: Notification Service Health
  steps:
    - name: Check Notification API
      uses: http
      with:
        method: GET
        url: "{{vars.NOTIFICATION_SERVICE_URL}}/health"
      test: res.code == 200
```

**Use cases:**
- Multi-service monitoring
- Independent feature testing
- Resource validation

### 3. Staged Workflow

Combines parallel and sequential execution with dependencies.

```yaml
name: Application Deployment Validation
description: Validate deployment across multiple stages
vars:
  DB_HEALTH_URL: "{{DB_HEALTH_URL}}"
  REDIS_HEALTH_URL: "{{REDIS_HEALTH_URL}}"
  API_URL: "{{API_URL}}"

jobs:
  # Stage 1: Infrastructure checks (parallel)
- id: database-check
  name: Database Connectivity
  steps:
    - name: Test Database Connection
      uses: http
      with:
        method: GET
        url: "{{vars.DB_HEALTH_URL}}"
      test: res.code == 200

- id: cache-check
  name: Cache Service Check
  steps:
    - name: Test Redis Connection
      uses: http
      with:
        method: GET
        url: "{{vars.REDIS_HEALTH_URL}}"
      test: res.code == 200

# Stage 2: Application checks (depends on infrastructure)
- id: api-validation
  name: API Service Validation
  needs: [database-check, cache-check]
  steps:
    - name: Test Core API Endpoints
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health"
      test: res.code == 200

# Stage 3: End-to-end testing (depends on API)
- id: e2e-tests
  name: End-to-End Tests
  needs: [api-validation]
  steps:
    - name: Test User Registration Flow
      uses: http
      with:
        url: "{{vars.API_URL}}/auth/register"
        method: POST
        body: |
          {
            "email": "test@example.com",
            "password": "testpass123"
          }
      test: res.code == 201
```

**Use cases:**
- Deployment validation
- Complex system testing
- Multi-tier application monitoring

### 4. Fan-out/Fan-in Workflow

Parallel execution followed by aggregation.

```yaml
name: Regional Service Check
description: Check services across multiple regions and aggregate results

jobs:
  # Fan-out: Check each region in parallel
- id: us-east-check
  name: US East Region Check
  steps:
    - name: Check US East API
      id: us-east-check
      uses: http
      with:
        method: GET
        url: https://us-east.api.example.com/health
      test: res.code == 200
      outputs:
        us_east_status: res.body.status
        us_east_time: (rt.sec * 1000)

- id: us-west-check
  name: US West Region Check
  steps:
    - name: Check US West API
      id: us-west-check
      uses: http
      with:
        method: GET
        url: https://us-west.api.example.com/health
      test: res.code == 200
      outputs:
        us_west_status: res.body.status
        us_west_time: (rt.sec * 1000)

- id: eu-check
  name: Europe Region Check
  steps:
    - name: Check EU API
      id: eu-check
      uses: http
      with:
        method: GET
        url: https://eu.api.example.com/health
      test: res.code == 200
      outputs:
        eu_status: res.body.status
        eu_time: (rt.sec * 1000)

# Fan-in: Aggregate results
- id: summary
  name: Regional Summary
  needs: [us-east-check, us-west-check, eu-check]
  steps:
    - name: Generate Report
      uses: hello
      echo: |
        Regional Health Check Results:

        US East: {{outputs.us_east_status}} ({{outputs.us_east_time}}ms)
        US West: {{outputs.us_west_status}} ({{outputs.us_west_time}}ms)
        Europe: {{outputs.eu_status}} ({{outputs.eu_time}}ms)

        Total regions healthy: {{
          (outputs.us_east_status == "healthy" ? 1 : 0) +
          (outputs.us_west_status == "healthy" ? 1 : 0) +
          (outputs.eu_status == "healthy" ? 1 : 0)
        }}/3
```

**Use cases:**
- Multi-region monitoring
- Load testing across environments
- Distributed system validation

## Workflow Organization Strategies

Once there is more than one workflow, the question is where to draw the file boundaries and how each file adapts to the environment it runs against.

### 1. Single-Purpose Workflows

Keep workflows focused on a single, well-defined purpose.

```yaml
# Good: Focused on API health checking
name: API Health Check
description: Monitor the health of our REST API endpoints

# Good: Focused on database operations
name: Database Maintenance
description: Perform routine database maintenance tasks

# Avoid: Mixed responsibilities
name: API and Database and Email Check
description: Check everything
```

### 2. Layered Workflows

Organize workflows by architectural layers.

```yaml
# infrastructure-health.yml
name: Infrastructure Health Check
description: Check foundational infrastructure components

# application-health.yml  
name: Application Health Check
description: Check application-level services

# business-logic-tests.yml
name: Business Logic Validation
description: Test core business functionality
```

### 3. Environment-Aware Workflows

Design workflows that work across different environments using configuration merging.

**base-monitoring.yml:**
```yaml merge=monitoring base
name: Service Monitoring
description: Monitor critical services
vars:
  # Replaced by the vars of production.yml or staging.yml
  API_BASE_URL: "{{API_BASE_URL}}"

jobs:
- id: api-check
  name: API Check
  steps:
    - name: Check API Health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: res.code == 200
```

**production.yml:**
```yaml merge=monitoring
vars:
  API_BASE_URL: https://api.production.example.com
```

**staging.yml:**
```yaml merge=monitoring
vars:
  API_BASE_URL: https://api.staging.example.com
```

Usage:
```bash
# Production monitoring
probe base-monitoring.yml,production.yml

# Staging monitoring  
probe base-monitoring.yml,staging.yml
```

## Advanced Workflow Techniques

The techniques below let one file cover cases that would otherwise need several: running jobs conditionally, deriving configuration during the run, and composing workflows from shared parts.

### 1. Conditional Job Execution

A job is skipped when its `skipif` expression is true. The expression reads `vars` and the `outputs` of the jobs it depends on.

```yaml
name: Conditional Diagnostics
vars:
  service_url: "{{SERVICE_URL}}"

jobs:
- id: health-check
  name: Basic Health Check
  steps:
    - name: Check Service
      id: service-check
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/health"
      test: res.code == 200
      outputs:
        service_healthy: res.code == 200

- name: Deep Diagnostic
  needs: [health-check]
  always: true
  skipif: outputs['service-check']?.service_healthy ?? false
  steps:
    - name: Run Diagnostics
      id: diagnostics
      uses: http
      with:
        method: GET
        url: "{{vars.service_url}}/diagnostics"
      test: res.code == 200

- name: Send Alert
  needs: [health-check]
  always: true
  skipif: outputs['service-check']?.service_healthy ?? false
  steps:
    - name: Critical Alert
      uses: hello
      echo: "CRITICAL: {{vars.service_url}} is not healthy"
```

The health check asserts its result with `test` and also publishes it as an output. A job whose needed job failed is skipped, so the two jobs that are there for a failed check have `always: true`. They read the output with `?.` and `??`, because a check that could not connect publishes none.


### 2. Dynamic Configuration

Use expressions to make workflows adapt to runtime conditions.

```yaml
name: Load Test
vars:
  load_test_url: "{{LOAD_TEST_URL}}"
  load_test_users: "{{LOAD_TEST_USERS ?? 10}}"
  load_test_duration: "{{LOAD_TEST_DURATION ?? 60}}"

jobs:
- id: load-test
  name: Load Testing
  steps:
    - name: Determine Load Parameters
      uses: hello
      id: params
      echo: "Load test configuration determined"
      outputs:
        concurrent_users: int(vars.load_test_users)
        test_duration: int(vars.load_test_duration)

    - name: Execute Load Test
      uses: http
      with:
        url: "{{vars.load_test_url}}"
        method: POST
        body: |
          {
            "concurrent_users": {{outputs.params.concurrent_users}},
            "duration_seconds": {{outputs.params.test_duration}}
          }
      test: res.code == 200
```

### 3. Workflow Composition

Break complex workflows into reusable components. Only one file can hold `jobs`, so the shared file keeps its job under a YAML anchor, and the workflow lists it among its own jobs.

**common-setup.yml:**
```yaml merge=composition base
shared:
  setup_job: &setup_job
    id: setup
    name: Common Setup
    steps:
      - name: Initialize Environment
        uses: hello
        id: setup
        echo: "Environment initialized"
        outputs:
          timestamp: unixtime()
          session_id: random_str(8)
```

**main-workflow.yml:**
```yaml merge=composition
name: Complete System Test
description: Full system validation with common setup

vars:
  API_URL: "{{API_URL}}"

jobs:
- *setup_job    # Defined in common-setup.yml
- id: api-tests
  name: API Tests
  needs: [setup]
  steps:
    - name: Test API with Session
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/test"
        headers:
          X-Session-ID: "{{outputs.setup.session_id}}"
      test: res.code == 200
```

Usage:
```bash
probe common-setup.yml,main-workflow.yml
```

## Best Practices

The points below cover what makes a workflow readable to someone who did not write it, and what keeps it working as it grows.

### 1. Naming Conventions

Use consistent, descriptive naming:

```yaml
# Workflow names: Use Title Case
name: Production API Health Check
vars:
  API_URL: "{{API_URL}}"

# Job names: Descriptive and specific
jobs:
- id: user-authentication-test
  name: User Authentication Test
  steps:
    # Step names: Action-oriented
    - name: Verify User Login Endpoint
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/auth/login"
      test: res.code == 200

- id: database-connectivity-check
  name: Database Connectivity Check
  steps:
    - name: Test Database Connection Pool
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health/db-pool"
      test: res.code == 200

    - name: Validate Cache Expiration
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/health/cache"
      test: res.code == 200
```

### 2. Documentation

Include comprehensive documentation:

```yaml
name: E-commerce Checkout Flow Test
description: |
  Validates the complete e-commerce checkout process including:

  1. Product catalog browsing
  2. Shopping cart management  
  3. User authentication
  4. Payment processing
  5. Order confirmation
  6. Email notification delivery

  This workflow simulates a real user journey from product selection
  to order completion, ensuring all critical business logic functions
  correctly.

  Prerequisites:
  - Test user account with valid payment method
  - Product catalog populated with test data
  - Email service configured for notifications

  Expected duration: 2-3 minutes

  Failure scenarios tested:
  - Invalid payment information
  - Out of stock products
  - Email delivery failures
```

### 3. Error Handling Strategy

Plan for failure scenarios:

```yaml
name: Service Failover
vars:
  primary_service_url: "{{PRIMARY_SERVICE_URL}}"
  fallback_service_url: "{{FALLBACK_SERVICE_URL}}"

jobs:
- id: primary-check
  name: Primary Service Check
  steps:
    - name: Check Primary Service
      id: primary
      uses: http
      with:
        method: GET
        url: "{{vars.primary_service_url}}"
      test: res.code == 200
      outputs:
        primary_healthy: res.code == 200

- id: fallback-check
  name: Fallback Service Check
  steps:
    - name: Check Fallback Service
      id: fallback
      uses: http
      with:
        method: GET
        url: "{{vars.fallback_service_url}}"
      test: res.code == 200
      outputs:
        fallback_healthy: res.code == 200

- id: notification
  name: Send Notifications
  needs: [primary-check, fallback-check]
  always: true    # Run even when a check failed
  steps:
    - name: Success Notification
      uses: hello
      skipif: outputs.primary_healthy != true
      echo: "✅ Primary service is healthy"

    - name: Fallback Notification
      uses: hello
      skipif: outputs.primary_healthy == true || outputs.fallback_healthy != true
      echo: "⚠️ Primary service down, fallback operational"

    - name: Critical Alert
      uses: hello
      skipif: outputs.primary_healthy == true || outputs.fallback_healthy == true
      echo: "🚨 CRITICAL: Both primary and fallback services are down"
```

With `always: true` the notification job runs even when a check failed. A check that could not connect publishes no output, which reads as nil, so the conditions compare with `true`, and each notification is skipped unless its case applies.

### 4. Performance Considerations

Design workflows for optimal performance:

```yaml fragment
# Good: Parallel execution for independent operations
jobs:
- id: frontend-check   # These run in parallel
- id: backend-check    # for better performance
- id: database-check

# Good: Efficient job dependencies
jobs:
- id: infrastructure
  name: infrastructure
- id: application
  name: application
  needs: [infrastructure]
- id: integration
  name: integration
  needs: [application]

# Avoid: Unnecessary sequential dependencies
jobs:
- id: check-a
  name: check-a
- id: check-b
  name: check-b
  needs: [check-a]  # Only if B actually depends on A
```

## Common Anti-Patterns

The three shapes below are the ones that make a workflow hard to change, and each has a straightforward fix.

### 1. Monolithic Workflows

**Avoid:**
```yaml fragment
name: Everything Check
jobs:
- id: massive-job
  name: massive-job
  steps:
    - name: Check API
    - name: Check Database
    - name: Check Cache
    - name: Check Email
    - name: Check Files
    - name: Check Logs
    # ... 50 more steps
```

**Instead:**
```yaml
# Split into focused workflows
name: API Health Check
name: Database Health Check  
name: Infrastructure Health Check
```

### 2. Tight Coupling

**Avoid:**
```yaml
# Hard-coded values throughout
- name: Check Production API
  uses: http
  with:
    method: GET
    url: https://prod-api.company.com/health
```

**Instead:**
```yaml
# Use configuration and environment variables
- name: Check API
  uses: http
  with:
    method: GET
    url: "{{vars.API_BASE_URL}}/health"
```

### 3. Missing Error Handling

**Avoid:**
```yaml
steps:
  - name: Critical Operation
    uses: http
    with:
      method: GET
      url: "{{vars.CRITICAL_SERVICE}}"
    # No test condition or error handling
```

**Instead:**
```yaml
steps:
  - name: Critical Operation
    uses: http
    with:
      method: GET
      url: "{{vars.CRITICAL_SERVICE}}"
    test: res.code == 200
```

## What's Next?

Now that you understand workflow design and patterns, explore:

1. **[Jobs and Steps](/guide/concepts/jobs-and-steps)** - Deep dive into job and step mechanics
2. **[Actions](/guide/concepts/actions)** - Learn about the action system and the available actions
3. **[Expressions and Templates](/guide/concepts/expressions-and-templates)** - Master dynamic configuration

Workflows are the foundation of Probe automation. With solid workflow design skills, you can build maintainable, efficient, and reliable automation processes.
