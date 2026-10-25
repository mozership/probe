---
title: Understanding Probe
description: Learn the core concepts and architecture of Probe
weight: 30
---

# Understanding Probe

Probe is a YAML-based workflow automation tool designed for monitoring, testing, and automation tasks. This guide explains the core concepts you need to understand to effectively use Probe.

Probe reads a workflow written in YAML, checks it, and runs it with its workflow engine. The engine schedules the jobs, executes their steps, evaluates expressions and prints the results, and each step calls an action: one built into Probe, or an external action from a repository of its own.

<img class="diagram diagram--light" src="/architecture.svg" alt="Probe's architecture: a YAML workflow goes through the parser and validator to the workflow engine, made of the scheduler, the executor, actions, expressions and the printer, which calls the built-in actions and the external actions" />
<img class="diagram diagram--dark" src="/architecture-dark.svg" alt="" />

## Core Concepts

Probe has four building blocks. A workflow contains jobs, a job contains steps, and a step invokes an action.

### Workflows

A **workflow** is the top-level container that defines what Probe should execute. It consists of:

- **Metadata**: Name and description of the workflow
- **Jobs**: One or more jobs that can run in parallel or sequentially
- **Global configuration**: Shared settings that apply to all jobs

```yaml
name: My Workflow
description: What this workflow does
# Jobs go here...
```

### Jobs

A **job** is a collection of steps that execute together. Jobs can:

- Run in parallel with other jobs
- Have dependencies on other jobs
- Share outputs with other jobs
- Have their own configuration and context

```yaml fragment
jobs:
- id: job-name
  name: Human-readable job name
  needs: [other-job]  # Optional: wait for other jobs
  steps:
    # Steps go here...
```

### Steps

A **step** is the smallest unit of execution. Each step can:

- Execute an action (HTTP request, email, etc.)
- Run tests to validate results
- Echo messages to the output
- Set outputs for use by other steps
- Have conditional execution logic

```yaml
steps:
  - name: Step Name
    uses: http            # The action to execute
    with:                 # Parameters for the action
      url: https://api.example.com
      method: GET
    test: res.code == 200  # Test condition
    outputs:              # Data to pass to other steps
      response_time: (rt.sec * 1000)
```

### Actions

**Actions** are the building blocks that actually do the work. Probe comes with built-in actions:

- **`http`**: Make HTTP/HTTPS requests
- **`shell`**: Execute shell commands and scripts securely
- **`smtp`**: Send email notifications and alerts
- **`hello`**: Simple greeting action (mainly for testing)

Actions run as separate processes behind a common interface, so you can extend Probe with custom actions.

## Workflow Execution Model

Jobs run at the same time unless a dependency says otherwise, and the data one job produces is what the next one reads.

### Parallel Execution

By default, jobs run in parallel for maximum efficiency:

```yaml
jobs:
- id: frontend-check
  name: frontend-check
  steps:            # at the same time
    - name: Top page
      uses: http
      with:
        method: GET
        url: https://www.example.com/
      test: res.code == 200
- id: backend-check
  name: backend-check
  steps:
    - name: API health
      uses: http
      with:
        method: GET
        url: https://api.example.com/health
      test: res.code == 200
- id: database-check
  name: database-check
  steps:
    - name: Database health
      uses: http
      with:
        method: GET
        url: https://api.example.com/health/db
      test: res.code == 200
```

### Sequential Execution with Dependencies

Use the `needs` keyword to create dependencies:

```yaml
jobs:
- id: setup
  name: Setup Environment
  steps:
    - name: Prepare
      uses: hello
      echo: Preparing the test data

- id: test
  name: Run Tests
  needs: [setup]     # Wait for 'setup' to complete
  steps:
    - name: API health
      uses: http
      with:
        method: GET
        url: https://api.example.com/health
      test: res.code == 200

- id: cleanup
  name: Clean Up
  needs: [test]      # Wait for 'test' to complete
  steps:
    - name: Clean up
      uses: hello
      echo: Removing the test data
```

### Data Flow

Data flows through the workflow using **outputs**:

```yaml
jobs:
- id: data-fetch
  name: data-fetch
  steps:
    - name: Get User Info
      id: data-fetch
      uses: http
      with:
        method: GET
        url: https://api.example.com/user/123
      test: res.code == 200
      outputs:
        user_id: res.body.id
        user_name: res.body.name

- id: notification
  name: notification
  needs: [data-fetch]
  steps:
    - name: Send Welcome Email
      uses: hello
      echo: "Welcome {{outputs['data-fetch'].user_name}}!"
```

## Expression System

Probe uses expressions for dynamic values and testing. Expressions are written using `{{}}` syntax:

### Template Expressions

Use template expressions to insert dynamic values:

```yaml
- name: Greet User
  uses: hello
  echo: "Hello {{outputs['previous-step'].username}}!"
```

### Test Expressions

Use test expressions to validate results:

```yaml
- name: Check API Response
  uses: http
  with:
    method: GET
    url: https://api.example.com/status
  test: res.code == 200 && res.body.healthy == true
```

### Available Variables

In expressions, you have access to:

- **`res`**: Response from the current action
- **`outputs`**: Outputs from previous steps/jobs
- **`env`**: Environment variables
- **Custom functions**: `random_int()`, `random_str()`, `unixtime()`

## File Merging

Probe supports merging multiple YAML files, which is useful for:

- Separating configuration from workflow logic
- Reusing common definitions across workflows
- Environment-specific overrides

```bash
# Merge base workflow with environment-specific config
probe base-workflow.yml,production-config.yml
```

The files are merged in order, with later files overriding values from earlier files.

## Error Handling

Probe provides several mechanisms for handling errors:

### Test Failures

When a test fails, the step is marked as failed:

```yaml
- name: Critical Check
  uses: http
  with:
    method: GET
    url: https://critical-api.example.com
  test: res.code == 200  # If this fails, step fails
```

### Conditional Execution

Use `skipif` to skip a step. The expression sees the outputs of earlier steps, so publish what the decision depends on rather than asserting it with `test`.

```yaml
- name: Primary Service Check
  id: primary
  uses: http
  with:
    method: GET
    url: https://primary-api.example.com
  outputs:
    primary_ok: res.code == 200

- name: Fallback Check
  uses: http
  skipif: outputs.primary.primary_ok
  with:
    method: GET
    url: https://backup-api.example.com
  test: res.code == 200
```


### When a Step Fails

A failing `test` marks the step and its job as failed, but the remaining steps of that job still run. Jobs that declare the failed job in `needs` are skipped, and the workflow exits with status `1`, or `3` when an action itself returned an error.

There is no per-step or per-job switch to ignore a failure. If a check is not meant to fail the workflow, publish its result as an output instead of asserting it:

```yaml
- name: Optional Service Check
  id: optional
  uses: http
  with:
    method: GET
    url: https://optional-service.example.com
  outputs:
    optional_ok: res.code == 200
```


## Best Practices

The points below cover naming, how steps are grouped, how data is passed, and what the tests assert.

### 1. Use Descriptive Names

Names appear in the report, so they are what a failure is read by.

```yaml
# Good
- name: Check Production API Health
  uses: http
  # ...

# Not so good  
- name: HTTP Check
  uses: http
  # ...
```

### 2. Group Related Steps into Jobs

Steps that share a subject belong in one job, because that is the unit that runs in order and fails together.

```yaml
vars:
  db_dsn: "{{DB_DSN}}"

jobs:
- id: infrastructure-check
  name: Infrastructure Health Check
  steps:
    - name: Check Database
      uses: db
      with:
        dsn: "{{vars.db_dsn}}"
        query: SELECT 1
      test: res.code == 0
    - name: Check Cache
      uses: http
      with:
        method: GET
        url: https://cache.example.com/health
      test: res.code == 200
    - name: Check Load Balancer
      uses: http
      with:
        method: GET
        url: https://lb.example.com/health
      test: res.code == 200
```

### 3. Use Outputs for Data Sharing

A value needed later is published as an output rather than fetched again.

```yaml
- name: Fetch Configuration
  id: config
  uses: http
  with:
    method: GET
    url: https://config-service.example.com
  outputs:
    database_url: res.body.database_url

- name: Test Database Connection
  uses: http
  with:
    method: GET
    url: "{{outputs.config.database_url}}/health"
  test: res.code == 200
```

### 4. Add Meaningful Test Conditions

A test should assert what the step was for, not merely that a response arrived.

```yaml
# Good - specific test conditions
test: res.code == 200 && res.body.status == "healthy" && (rt.sec * 1000) < 1000

# Not so good - generic test
test: res.code == 200
```

## What's Next?

Now that you understand the core concepts, you can:

1. **[Create your first workflow](/guide/introduction/your-first-workflow)** - Build a practical workflow
2. **[Learn CLI basics](/guide/introduction/cli-basics)** - Master the command-line interface
3. **[Explore the reference](/reference/cli-reference)** - Deep dive into all available options

The key to mastering Probe is practice. Start with simple workflows and gradually build more complex automation as you become comfortable with the concepts.
