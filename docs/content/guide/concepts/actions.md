# Actions

Actions are the core execution units in Probe that perform actual work. Each one runs in a process of its own, which makes Probe extensible and modular. This guide explores the action system, built-in actions, and how Probe runs an action.

## Action System Overview

The action system in Probe provides:

- **Modularity**: Each action stands on its own, behind the same gRPC interface
- **Extensibility**: Custom actions can be added easily
- **Isolation**: Actions run in separate processes for stability
- **Standardization**: All actions follow the same interface

### Action Execution Flow

1. **Resolution**: Probe finds the action a step names, a built-in one or an external action resolved before the first job starts
2. **Start**: The action is started in a process of its own
3. **Communication**: Probe communicates with the action via gRPC
4. **Execution**: The action does the work of the step
5. **Response**: Results are returned to Probe for processing
6. **Cleanup**: The action's process is stopped once the step has its result

## Built-in Actions

Probe comes with several built-in actions that cover common use cases.

### HTTP Action

The `http` action is the most versatile and commonly used action for making HTTP/HTTPS requests.

#### Basic Usage

A GET request needs nothing but the URL.

```yaml
- name: Simple GET Request
  uses: http
  with:
    url: https://api.example.com/users
    method: GET
  test: res.code == 200
```

#### Complete HTTP Action Reference

Every field the action accepts, in one step.

```yaml
- name: Comprehensive HTTP Request
  uses: http
  timeout: 30s                                  # Optional: Request timeout
  with:
    url: https://api.example.com/users/123        # Required: Target URL
    method: POST                                  # Optional: HTTP method (default: GET)
    headers:                                      # Optional: Request headers
      Content-Type: "application/json"
      Authorization: "Bearer {{vars.api_token}}"
      X-Request-ID: "{{random_str(16)}}"
    body: |                                       # Optional: Request body
      {
        "name": "John Doe",
        "email": "john@example.com",
        "active": true
      }
  test: res.code == 200 && res.body.success == true
  outputs:
    user_id: res.body.user.id
    created_at: res.body.user.created_at
    response_time: (rt.sec * 1000)
```

#### HTTP Response Object

The HTTP action provides a rich response object:

```yaml
# Available response properties:
test: |
  res.code == 200 &&                    # HTTP status code
  (rt.sec * 1000) < 1000 &&                      # Response time in milliseconds
  res.body_size < 10000 &&               # Response body size in bytes
  res.headers["Content-Type"] == "application/json" &&  # Response headers
  res.body.success == true &&            # Parsed JSON body (if applicable)
  res.body contains "success"           # Response body as text
```

#### Common HTTP Patterns

**API Authentication:**
```yaml
jobs:
- id: api-test
  name: api-test
  steps:
    - name: Authenticate
      id: auth
      uses: http
      with:
        url: "{{vars.api_base_url}}/auth/login"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "username": "{{vars.api_username}}",
            "password": "{{vars.api_password}}"
          }
      test: res.code == 200
      outputs:
        access_token: res.body.access_token
        refresh_token: res.body.refresh_token

    - name: Make Authenticated Request
      uses: http
      with:
        url: "{{vars.api_base_url}}/protected/resource"
        method: GET
        headers:
          Authorization: "Bearer {{outputs.auth.access_token}}"
      test: res.code == 200
```

**File Upload:**
```yaml
- name: Upload File
  uses: http
  with:
    url: "{{vars.api_url}}/upload"
    method: POST
    headers:
      Content-Type: "multipart/form-data"
    body: |
      --boundary123
      Content-Disposition: form-data; name="file"; filename="test.txt"
      Content-Type: text/plain
      
      This is test file content
      --boundary123--
  test: res.code == 201
```

**GraphQL Queries:**
```yaml
- name: GraphQL Query
  uses: http
  with:
    url: "{{vars.graphql_endpoint}}"
    method: POST
    headers:
      Content-Type: "application/json"
      Authorization: "Bearer {{vars.graphql_token}}"
    body: |
      {
        "query": "query GetUser($id: ID!) { user(id: $id) { name email active } }",
        "variables": { "id": "{{vars.test_user_id}}" }
      }
  test: res.code == 200 && res.body.data.user != null
  outputs:
    user_name: res.body.data.user.name
    user_email: res.body.data.user.email
```

### Shell Action

The `shell` action enables secure execution of shell commands and scripts within workflows. It provides comprehensive output capture, timeout protection, and environment variable support.

#### Basic Usage

A shell step needs nothing but the command.

```yaml
- name: Build Application
  uses: shell
  with:
    cmd: "npm run build"
    workdir: "/app"
    timeout: "5m"
  test: res.code == 0
```

#### Complete Shell Action Reference

Every field the action accepts, in one step.

```yaml
- name: Deploy Application
  uses: shell
  with:
    cmd: "./deploy.sh production"              # Required: Command to execute
    shell: "/bin/bash"                        # Optional: Shell to use (default: /bin/sh)
    workdir: "/deploy"                        # Optional: Working directory (absolute path)
    timeout: "15m"                           # Optional: Execution timeout (default: 30s)
    env:                                     # Optional: Environment variables
      DEPLOY_ENV: "production"
      API_KEY: "{{vars.production_api_key}}"
      BUILD_VERSION: "{{vars.version}}"
  test: res.code == 0 && res.stdout contains "Deploy successful"
  outputs:
    deploy_time: res.rt
    deploy_log: res.stdout
```

#### Shell Response Object

The exit code and both output streams are available to the test.

```yaml
# Available response properties:
test: |
  res.code == 0 &&                         # Exit code (0 = success)
  res.stdout contains "success" &&        # Standard output
  res.stderr == "" &&                      # Standard error (empty = no errors)
  req.cmd == "npm run build" &&           # Original command
  req.shell == "/bin/bash"                # Shell used for execution
```

#### Common Shell Patterns

**Build and Test Pipeline:**
```yaml
jobs:
- id: build-and-test
  name: build-and-test
  steps:
    - name: Install Dependencies
      uses: shell
      with:
        cmd: "npm ci"
        workdir: "/app"
        timeout: "5m"
      test: res.code == 0

    - name: Run Tests
      uses: shell
      with:
        cmd: "npm test"
        workdir: "/app"
        env:
          NODE_ENV: "test"
          CI: "true"
      test: res.code == 0

    - name: Build Application
      uses: shell
      with:
        cmd: "npm run build"
        workdir: "/app"
        env:
          NODE_ENV: "production"
      test: res.code == 0
```

**System Health Monitoring:**
```yaml
- name: Check System Health
  uses: shell
  with:
    cmd: |
      echo "=== System Health Report ===" &&
      echo "CPU Usage: $(top -bn1 | grep Cpu | cut -d' ' -f2)" &&
      echo "Memory: $(free -h | grep Mem)" &&
      echo "Disk: $(df -h /)"
  test: res.code == 0
  outputs:
    health_report: res.stdout
```

#### Security Features

The shell action implements multiple security layers:

- **Shell Restriction**: Only allows approved shell executables
- **Path Validation**: Working directories must be absolute paths
- **Timeout Protection**: Prevents runaway processes
- **Environment Isolation**: Safe environment variable handling
- **Output Sanitization**: Secure capture of command output

### Hello Action

The `hello` action is primarily used for testing and demonstrations. It provides a simple way to verify that Probe can start and call an action.

```yaml
- name: Test Hello Action
  id: hello
  uses: hello
  with:
    message: "Test message"           # Echoed back on res
  echo: "{{res.message}}"
  outputs:
    greeting: res.message
```

**Hello Action Response:** the action takes no parameters of its own. Every key given in `with` comes back on `res`, and `status` is always `0`.

```yaml
test: status == 0 && res.message != null
```

### SMTP Action

The `smtp` action enables email sending capabilities for notifications and alerts.

```yaml
- name: Send Email Notification
  uses: smtp
  with:
    addr: "smtp.gmail.com              # SMTP server host:587                         # SMTP server port"
    from: alerts@mycompany.com        # Sender email address
    to: ["admin@mycompany.com", "team@mycompany.com"]  # Recipients
    subject: "System Alert: {{vars.alert_type}}"       # Email subject
    session: 1
    message: 1
    length: 500
  echo: |                           # Email body (plain text or HTML)
  test: res.code == "sent"
  outputs:
    message_id: res.message_id
    recipients_count: res.recipients_count
```

**SMTP Configuration Examples:**

**Gmail:**
```yaml
with:
  host: smtp.gmail.com
  port: 587
  username: "your-email@gmail.com"
  password: "your-app-password"
  tls: true
```

**AWS SES:**
```yaml
with:
  host: email-smtp.us-east-1.amazonaws.com
  port: 587
  username: "{{vars.aws_ses_username}}"
  password: "{{vars.aws_ses_password}}"
  tls: true
```

**Office 365:**
```yaml
with:
  host: smtp.office365.com
  port: 587
  username: "your-email@company.com"
  password: "{{vars.o365_password}}"
  tls: true
```

## Advanced Action Usage

Beyond a single call, actions are used in combination: handling their failures, chaining them, and configuring them from values computed during the run.

### Error Handling in Actions

Implement robust error handling for action failures:

```yaml
jobs:
- id: resilient-http-check
  name: resilient-http-check
  steps:
    - name: Primary Endpoint Check
      id: primary
      uses: http
      timeout: 10s
      with:
        url: "{{vars.primary_url}}/health"
        method: GET
      test: res.code == 200
      outputs:
        primary_healthy: res.code == 200
        primary_response_time: (rt.sec * 1000)

    - name: Secondary Endpoint Check
      id: secondary
      uses: http
      timeout: 15s
      with:
        url: "{{vars.secondary_url}}/health"
        method: GET
      test: res.code == 200
      outputs:
        secondary_healthy: res.code == 200
        secondary_response_time: (rt.sec * 1000)

    - name: Alert on Total Failure
      uses: smtp
      with:
        addr: "{{vars.smtp_host}}:587"
        from: "alerts@company.com"
        to: "ops-team@company.com"
        subject: "CRITICAL: All endpoints down"
        session: 1
        message: 1
        length: 500
      echo: |
        CRITICAL ALERT: All monitored endpoints are down
          
        Primary Endpoint: FAILED
        Secondary Endpoint: FAILED
          
        Time: {{unixtime()}}
          
        Immediate investigation required!
```

### Action Composition Patterns

Combine actions to create complex workflows:

```yaml
jobs:
- id: comprehensive-api-test
  name: Comprehensive API Testing
  steps:
    # 1. Health check
    - name: Verify API Health
      id: health
      uses: http
      with:
        method: GET
        url: "{{vars.api_url}}/health"
      test: res.code == 200
      outputs:
        api_version: res.body.version
        database_connected: res.body.database.connected

    # 2. Authentication test
    - name: Test Authentication
      id: auth
      uses: http
      with:
        url: "{{vars.api_url}}/auth/token"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "client_id": "{{vars.client_id}}",
            "client_secret": "{{vars.client_secret}}",
            "grant_type": "client_credentials"
          }
      test: res.code == 200
      outputs:
        access_token: res.body.access_token
        token_expires: res.body.expires_in

    # 3. Functional test
    - name: Test Core Functionality
      id: functional
      uses: http
      with:
        url: "{{vars.api_url}}/api/test"
        method: GET
        headers:
          Authorization: "Bearer {{outputs.auth.access_token}}"
      test: res.code == 200 && res.body.test_passed == true
      outputs:
        test_duration: (rt.sec * 1000)
        test_results: res.body.results

    # 4. Performance validation
    - name: Validate Performance
      uses: smtp
      with:
        addr: "{{vars.smtp_host}}:587"
        from: "performance@company.com"
        to: "dev-team@company.com"
        subject: "Performance Alert: Slow API Response"
        session: 1
        message: 1
        length: 500
      echo: |
        Performance Alert
          
        API Version: {{outputs.health.api_version}}
        Response Time: {{outputs.functional.test_duration}}ms
        Expected: < 2000ms
          
        Please investigate performance degradation.

        uccess notification
    - name: Success Report
      uses: hello
      echo: |
        ✅ API Test Suite Completed Successfully
          
        Health Check: ✅ (v{{outputs.health.api_version}})
        Authentication: ✅ (expires in {{outputs.auth.token_expires}}s)
        Functionality: ✅ ({{outputs.functional.test_duration}}ms)
        Performance: ✅ (within acceptable limits)
```

### Dynamic Action Configuration

Configure actions dynamically based on runtime conditions:

```yaml
jobs:
- id: adaptive-monitoring
  name: adaptive-monitoring
  steps:
    - name: Determine Environment
      id: env
      uses: http
      with:
        method: GET
        url: "{{vars.config_service_url}}/environment"
      test: res.code == 200
      outputs:
        environment: res.body.environment
        notification_level: res.body.notifications.level
        smtp_config: res.body.smtp

    - name: Environment-Specific Health Check
      id: health
      uses: http
      timeout: "{{outputs.vars.environment == 'production' ? '5s' : '30s'}}"
      with:
        method: GET
        url: "{{vars.service_url}}/health"
      test: res.code == 200
      outputs:
        service_status: res.body.status
        error_count: res.body.errors

    - name: Conditional Alert
      uses: smtp
      with:
        addr: "{{outputs.vars.smtp_config.host}}:{{outputs.vars.smtp_config.port}}"
        from: "monitoring@company.com"
        to: "{{outputs.vars.environment == 'production' ? ['ops@company.com', 'management@company.com'] : ['dev@company.com']}}"
        subject: "{{outputs.vars.environment == 'production' ? 'PRODUCTION' : 'NON-PROD'}} Alert: Service Errors Detected"
        session: 1
        message: 1
        length: 500
      echo: |
        Service Error Alert
          
        Environment: {{outputs.vars.environment}}
        Service Status: {{outputs.health.service_status}}
        Error Count: {{outputs.health.error_count}}
          
        {{outputs.vars.environment == "production" ? "IMMEDIATE ACTION REQUIRED" : "Please investigate when convenient"}}
```

## Action Architecture Deep Dive

Every action runs in a process of its own. How Probe talks to an action, when it starts and stops, and how the built-in ones are run are described below.

### Communication with Actions

Probe uses gRPC to communicate with actions, providing:

- **Type Safety**: Strong typing with Protocol Buffers
- **Performance**: Efficient binary serialization
- **Cross-Language**: Actions can be written in any language supporting gRPC
- **Reliability**: Built-in error handling and timeouts

### Action Lifecycle

1. **Resolution**: Probe reads the `action.yml` of every external action before the first job starts, and fetches the executable of each one the guard of the run lets run; a step whose action the guard does not let run is refused without its executable, and a built-in one needs no resolving
2. **Per-Step Start**: An action is started when a step uses it
3. **Process Isolation**: Each action runs in its own process
4. **Per-Step Stop**: The process is stopped as soon as the step has its result, so no action is kept running between steps
5. **Cleanup**: Once the workflow is over, Probe starts each external action that asked for it once more, to remove what its steps left behind. See [Cleaning Up After a Run](#cleaning-up-after-a-run)
6. **Error Isolation**: An action that fails or exits fails its step, not Probe

### Built-in Action Management

The built-in actions are part of the Probe binary. Probe runs each one by starting its own executable again as `<executable> builtin-actions <name>`:

```bash
probe workflow.yml  # Starts the built-in actions its steps use

# No separate installation needed for built-in actions:
# http, db, shell, ssh, grpc, smtp, imap,
# dns, embedded, hello
```

## Action Best Practices

Whichever action a step uses, the same questions come up: how long to wait, what to do on failure, where the credentials come from, what to assert, and what to hand to the next step.

### 1. Timeout Configuration

Always set appropriate timeouts:

```yaml
# Good: Specific timeouts based on expected response time
- name: Quick Health Check
  uses: http
  timeout: 5s              # Quick ping should respond fast
  with:
    method: GET
    url: "{{vars.api_url}}/ping"

- name: Complex Query
  uses: http
  timeout: 60s             # Complex operations need more time
  with:
    method: GET
    url: "{{vars.api_url}}/complex-report"
```

### 2. Error Handling Strategy

Implement appropriate error handling:

```yaml
# Critical actions - fail fast
- name: Database Connectivity Check
  uses: http
  with:
    method: GET
    url: "{{vars.db_url}}/ping"
  test: res.code == 200

# Non-critical actions - continue on error
- name: Optional Analytics Update
  uses: http
  with:
    method: GET
    url: "{{vars.analytics_url}}/update"
  test: res.code == 200
```

### 3. Secure Configuration

Handle sensitive data properly:

```yaml
# Good: Use environment variables for secrets
- name: Authenticated Request
  uses: http
  with:
    method: GET
    url: "{{vars.api_url}}/secure"
    headers:
      Authorization: "Bearer {{vars.api_token}}"  # From vars

# Good: Use secure SMTP configuration
- name: Send Alert
  uses: smtp
  with:
    addr: "{{vars.smtp_host}}:25"
    from: "probe@example.com"
    to: "ops@example.com"
    session: 1
    message: 1
    length: 500
- name: Bad Example
  uses: http
  with:
    headers:
      Authorization: "Bearer secret-token-123"  # Never do this!
```

### 4. Response Validation

Validate action responses thoroughly:

```yaml
- name: Comprehensive API Test
  uses: http
  with:
    method: GET
    url: "{{vars.api_url}}/users"
  test: |
    res.code == 200 &&
    res.headers["Content-Type"] contains "application/json" &&
    res.body.users != null &&
    len(res.body.users) > 0 &&
    (rt.sec * 1000) < 1000
  outputs:
    user_count: len(res.body.users)
    response_time: (rt.sec * 1000)
```

### 5. Meaningful Outputs

Define useful outputs for other steps:

```yaml
- name: User Creation Test
  uses: http
  with:
    url: "{{vars.api_url}}/users"
    method: POST
    body: '{"name": "Test User", "email": "test@example.com"}'
  test: res.code == 201
  outputs:
    created_user_id: res.body.user.id
    created_user_email: res.body.user.email
    creation_timestamp: res.body.user.created_at
    response_time: (rt.sec * 1000)
```

## Custom Actions (Advanced)

While Probe comes with powerful built-in actions, you can extend it with custom actions for specialized needs.

### Custom Action Interface

A custom action implements the `actionrpc.Action` interface:

```go
type Action interface {
    Run(with map[string]any) (map[string]any, error)
}
```

`with` holds the step's `with` parameters. The returned map becomes the step's `req`, `res`, `rt` and `status`, as with the built-in actions. A map in the result may be keyed by strings, numbers or booleans; the keys are sent as text, so `map[int]string{404: "not found"}` arrives as `{"404": "not found"}`. A map with any other kind of key, or two keys that read the same, such as `1` and `"1"`, fails the step with an action error rather than losing entries.

#### Knowing the Step and Keeping State

An action that needs to know the step it runs for, or to keep something from one step to the next, as the http action does for its trace header and its cookies, also implements `actionrpc.StepAction`:

```go
type StepAction interface {
    Action
    RunStep(call Call) (result, newState map[string]any, err error)
}

type Call struct {
    With    map[string]any    // the step's with parameters
    State   map[string]any    // the state the action left in the job
    Step    Step              // the step it runs for
    Guard   Guard             // what the run allows the action to do
    Actions map[string]string // the names the workflow gives its external actions
    Cleanups *Cleanups        // not nil under a Probe that cleans up once the run is over
    Processes *Processes      // the processes the action leaves running in the background
}

type Step struct {
    RunID   string // the run of probe, the same for every step
    JobID   string
    JobName string
    Index   int    // the position of the step in the job, from 0
    ID      string
    Name    string
    Repeat  int    // which run of a repeated job, from 0
    Attempt int    // which attempt of a retried step, from 1
}
```

Probe tells every such action about the step it runs for; what the action does with it is up to the action. `RunID` is the same for every step of a run, and for a job run by the embedded action.

`Guard` is the guard of the run, set by `--read-only`, `--allow-host` and `--allow-action`. An action keeps to it by returning `actionrpc.Refuse(...)` for what it does not allow, which fails the step with the kind `refused`; `Guard.ReadOnly`, `Guard.AllowsHost` and `Guard.CheckHost` tell what it allows. An external action declares the kinds of guard it keeps to in `guard` in its `action.yml`, and is refused under a kind it does not declare unless `--allow-action` names it. See [Guard](/guide/concepts/guard).

`Actions` holds the names the workflow gives its external actions under [`actions`](/reference/yaml-configuration#actions), each with the action it stands for in full, and a local one as an absolute path. Probe has already replaced the names in the workflow itself, so an action needs them only to run steps of its own, as the embedded action does for a job file.

`State` is the state the action left in the job, or nil when it left none. Probe keeps `newState` without reading it, and passes it to the action in the next step of the same job that uses it. The state takes the form a result takes, maps keyed by strings, lists and plain values, and one that cannot take it fails the step with an action error. A nil `newState` keeps the state as it was, and so does a step that fails with an action error or times out. Each job keeps the state of each action apart, and each run of a repeated job, and a job run by the embedded action, starts with none. The state is not shown in the output, so it may hold credentials.

#### Cleaning Up After a Run

An action runs in a process that is stopped as soon as its step is done, so what a step leaves behind for the steps after it, such as a container it started, outlives the action. An action that leaves something behind sets `cleanup: true` in its `action.yml` and also implements `actionrpc.Cleaner`:

```go
type Cleaner interface {
    Action
    Cleanup(call CleanupCall) error
}

type CleanupCall struct {
    RunID string // the run that is over, as Step.RunID named it
}
```

Probe calls `Cleanup` once the workflow is over, whether it passed, failed, or was ended by Ctrl+C, `SIGTERM` or `SIGHUP`:

- **Once for each action**, however many steps and jobs used it, and by whichever name. It is not called for an action no step ran.
- **With the run alone.** The action has to find what to remove by `RunID`, so its steps mark what they make with it, as a container takes a label. Two runs at the same time have different IDs, and neither removes what the other made.
- **After the processes the steps left running in the background are stopped**, and the action first used last, so that what a later step started is gone before what it may depend on.
- **With 30 seconds for each action.** A cleanup that fails or takes longer is told as a warning and changes neither the result nor the exit status of the run.

`Cleanup` must not fail for what is already gone. It can be called for a run in which the step left nothing, more than once for the same run, and, when a signal ends the run, while a step of the action is still running.

What the steps of a job run by the embedded action leave behind stays until the workflow that embeds the job is over, as it does for the jobs of the workflow itself.

`Call.Cleanups` is not nil under a Probe that cleans up, which calls `Cleanup` of an action whose `action.yml` sets `cleanup`. It is nil under a Probe from before there was a cleanup; an action that would leave something behind for good can return an error then. An action that runs steps of its own, as the embedded action does, names the external actions those steps ran with `Call.Cleanups.Add`, for Probe to call them.

An action that leaves a process running in the background, as the shell action does for `background: true`, starts it in a session of its own, so that it outlives the action, and tells Probe of it with `Call.Processes.Add(actionrpc.Process{PID: pid, Log: logFile})`, also when the step then fails. Probe stops the process group it leads once the workflow is over, or when Ctrl+C, `SIGTERM` or `SIGHUP` ends it, and removes the log file, if any. A Probe from before there were processes does not hear of them, and leaves them running.

A Probe that is killed outright, as by `SIGKILL`, calls nothing. An action can make up for that by removing what earlier runs left when a step of a later run starts.

The [container](/reference/actions/container) action is written this way: it labels each container with `Step.RunID`, and its `Cleanup` removes the containers of that label.

### External Actions

An action can also live outside Probe, in a repository of its own. Probe downloads the executable that serves it and runs it as it runs a built-in action. A step names the action by repository and commit:

```yaml
- name: Ask the API who I am
  uses: github.com/mozership/probe-graphql@<40-character commit SHA>
  with:
    url: https://api.example.com/graphql
    query: '{ viewer { login } }'
  test: res.code == 200
```

`uses` names an external action in one of two forms. A name without a slash is a built-in action.

| Form | Example |
|---|---|
| `github.com/<owner>/<repo>[/<dir>]@<commit>` | `github.com/mozership/probe-graphql@3f2a…` |
| A path starting with `./`, `../` or `/` | `./actions/greet` |

A remote action must be pinned to a full 40-character commit SHA. Tags and branches are refused, because they can be moved to other code after the workflow was reviewed. Only GitHub is supported for now. A local path is taken relative to the workflow file, or, in a job run by the [embedded](/reference/actions/embedded) action, relative to that job file.

Probe resolves every external action before the first job starts. A reference that cannot be resolved fails the run with exit code 2, and a download does not count against a step's timeout. Executables are kept under `probe/actions` in the user's cache directory (`~/.cache` on Linux, `~/Library/Caches` on macOS), so each is downloaded once. `action.yml` is read from GitHub on every run instead, because it holds the digest the executable is checked against, and a copy on disk could have been changed.

#### Naming an External Action

A workflow that uses an external action in several steps can give it a name once, under the top-level `actions`, and use the name in `uses` and in `defaults`:

```yaml
actions:
  graphql: github.com/mozership/probe-graphql@<40-character commit SHA> # v0.2.0

jobs:
- name: Who am I
  defaults:
    graphql:
      url: https://api.example.com/graphql
  steps:
  - name: Ask the API who I am
    uses: graphql
    with:
      query: '{ viewer { login } }'
    test: res.code == 200
```

The commit is then written in one place, which is the line to change for a newer release. A name cannot be that of a built-in action, and `--allow-action` takes the action in full, not the name. See [`actions`](/reference/yaml-configuration#actions) for the rules.

#### Published Actions

These external actions are published alongside Probe, each in a repository of its own. Each page describes its parameters and result, and the notes of each release start with the `uses` line to copy.

| Action | What it does | Latest |
|---|---|---|
| [browser](/reference/actions/browser) | Drives a real Chrome through chromedp: opens pages, reads and types into them, and takes screenshots. It was built into Probe up to v1.21.0 | v0.1.0 |
| [container](/reference/actions/container) | Starts a container for the steps after it, on a free port and optionally waiting until it is ready, and removes the containers of a run once the run is over. It runs `docker`, or another runtime such as Podman | v0.1.0 |
| [graphql](/reference/actions/graphql) | Sends a GraphQL query over HTTP, and returns the `data` and `errors` of the response apart | v0.2.0 |
| [jmap](/reference/actions/jmap) | Calls [JMAP](https://jmap.io/) methods: fetches the session, fills in the account of each call, and gathers the method errors, which come with HTTP 200, in `res.errors` | v0.2.0 |
| [mail-latency](/reference/actions/mail-latency) | Reads messages from a Maildir, computes the delivery latency of each one from its `Received` headers, and writes a CSV. It was built into Probe up to v1.21.0 | v0.1.0 |
| [redis](/reference/actions/redis) | Runs commands on a Redis or Valkey server and returns their replies, with the errors the server answered gathered in `res.errors`. Under `--read-only` it runs only commands known to only read | v0.1.0 |
| [s3](/reference/actions/s3) | Reads, lists, writes and deletes objects in S3 and S3-compatible storage such as MinIO or Cloudflare R2, signing each request itself | v0.1.0 |
| [websocket](/reference/actions/websocket) | Connects to a WebSocket server, sends and receives messages in order, and returns those it received. A receive takes the next message, a number of them, or every message until the server closes, optionally only those that match | v0.1.0 |

#### action.yml

The action's directory holds an `action.yml` that says which executable serves it:

```yaml
name: graphql
description: Send a GraphQL query over HTTP
guard: [read-only, allow-host]
params: [url, query, variables, operation_name, headers, timeout]
runs:
  using: binary
  url: https://github.com/mozership/probe-graphql/releases/download/v0.2.0/probe-graphql_{os}_{arch}
  checksums:
    darwin_amd64: <SHA-256 of probe-graphql_darwin_amd64>
    darwin_arm64: <SHA-256 of probe-graphql_darwin_arm64>
    linux_amd64: <SHA-256 of probe-graphql_linux_amd64>
    linux_arm64: <SHA-256 of probe-graphql_linux_arm64>
```

| Key | Description |
|---|---|
| `runs.using` | Must be `binary` |
| `runs.url` | Where the executable is downloaded from. `{os}` and `{arch}` become Go's `GOOS` and `GOARCH`, such as `linux` and `arm64` |
| `runs.path` | The executable, relative to the action's directory, with the same placeholders. Only a local action can use it |
| `runs.checksums` | SHA-256 digest of the executable for each `<os>_<arch>`, in lowercase hex |
| `guard` | The kinds of guard the action keeps to itself: `read-only`, `allow-host` or both. Probe takes the action at its word and runs it under a guard of those kinds. Without it, the action is refused under any guard unless `--allow-action` names it. See [Guard](/guide/concepts/guard) |
| `params` | The keys the action takes in `with`, for `probe check` to report a key it does not take. Without it, `with` is not checked; `[]` takes no key |
| `cleanup` | `true` for an action that Probe is to call once the workflow is over, for it to remove what its steps left behind. Without it, the action is not called. See [Cleaning Up After a Run](#cleaning-up-after-a-run) |

`runs` takes exactly one of `url` and `path`. With `url`, a checksum for the running platform is required, and a download with another digest is refused. The digest is checked again each time the executable is started. The commit in `uses` fixes `action.yml`, and `action.yml` fixes the digest, so the commit decides exactly which executable runs.

A local action with `path` needs no checksum. Give one to have the executable checked as a remote one is.

#### Writing an External Action

The executable serves the action with `actionrpc.Serve`:

```go
package main

import (
    "fmt"
    "time"

    "github.com/hashicorp/go-hclog"
    "github.com/mozership/probe/actionrpc"
)

// Greet is served by an executable of its own.
type Greet struct {
    log hclog.Logger
}

func (g *Greet) Run(with map[string]any) (map[string]any, error) {
    start := time.Now()
    actionrpc.LogParams(g.log, "greet received parameters", with)

    name, _ := with["name"].(string)
    return map[string]any{
        "req":    with,
        "res":    map[string]any{"message": fmt.Sprintf("Hello, %s!", name)},
        "rt":     time.Since(start).String(),
        "status": 0,
    }, nil
}

func main() {
    actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
        return &Greet{log: log}
    })
}
```

Build it for every platform it supports, publish the executables, and commit an `action.yml` with their URLs and digests. Users then pin the commit that holds that `action.yml`. [mozership/probe-graphql](https://github.com/mozership/probe-graphql) does this with GoReleaser and a workflow that commits the digests after each release.

`probe manifest` writes the `action.yml` of a release. It reads the `action.yml` of the repository, puts the tag in `runs.url` and the digests of the checksums file in `runs.checksums`, and prints the result:

```bash
probe manifest v0.2.0 dist/checksums.txt > "$RUNNER_TEMP/action.yml"
cp "$RUNNER_TEMP/action.yml" action.yml
```

The name, the description, `guard`, `params` and `cleanup` are kept as `action.yml` has them, so `action.yml` is the one place they are written in.

For a new action, `probe manifest init` prints the `action.yml` to start from. Give it the GitHub repository that will publish the action, fill in the description, `guard`, `params` and `cleanup`, and commit it; the first release then puts its tag and digests in:

```bash
probe manifest init mozership/probe-greet > action.yml
```

See [`manifest`](/reference/cli-reference#manifest) for what both take.

While developing, point a local `action.yml` at the executable you build:

```yaml
runs:
  using: binary
  path: probe-greet
```

```yaml
- name: Say hello
  uses: ./greet
  with:
    name: probe
  test: res.message == "Hello, probe!"
```

### Building a Custom Action into Probe

A custom action can instead be built into Probe itself. Probe runs each built-in action in a process of its own, by starting its own executable again as `<executable> builtin-actions <name>`. Such an action therefore lives in your own build of Probe: a program that uses Probe as a library, serves your action under that subcommand, and hands the other names to the built-in actions.

```go
package main

import (
    "fmt"
    "os"
    "time"

    "github.com/hashicorp/go-hclog"
    "github.com/mozership/probe"
    "github.com/mozership/probe/actionrpc"
    "github.com/mozership/probe/actions"
)

// Greet is a custom action, used as `uses: greet`.
type Greet struct {
    log hclog.Logger
}

func (g *Greet) Run(with map[string]any) (map[string]any, error) {
    start := time.Now()
    actionrpc.LogParams(g.log, "greet received parameters", with)

    name, _ := with["name"].(string)
    return map[string]any{
        "req":    with,
        "res":    map[string]any{"message": fmt.Sprintf("Hello, %s!", name)},
        "rt":     time.Since(start).String(),
        "status": 0,
    }, nil
}

func main() {
    // Probe runs every action by starting this executable again
    // as `<executable> builtin-actions <name>`.
    if len(os.Args) == 3 && os.Args[1] == actionrpc.BuiltinCmd {
        name := os.Args[2]
        if name == "greet" {
            actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
                return &Greet{log: log}
            })
            return
        }
        if serve, ok := actions.Lookup(name); ok {
            serve()
            return
        }
        fmt.Fprintf(os.Stderr, "unknown action: %s\n", name)
        os.Exit(1)
    }

    p := probe.New(os.Args[1], false)
    if err := p.Do(); err != nil {
        fmt.Fprintln(os.Stderr, err)
    }
    os.Exit(p.ExitStatus())
}
```

Build it and run a workflow with the resulting binary in place of `probe`:

```yaml
- name: Say hello
  uses: greet
  with:
    name: probe
  test: res.message == "Hello, probe!"
```

## What's Next?

Now that you understand the action system, explore:

1. **[Expressions and Templates](/guide/concepts/expressions-and-templates)** - Learn dynamic configuration and testing
2. **[Data Flow](/guide/concepts/data-flow)** - Understand how data moves between actions
3. **[How-tos](/guide/how-tos/api-testing)** - See practical action usage patterns

Actions are the workhorses of Probe. Master the built-in actions and understand how Probe runs them to build powerful, extensible automation workflows.
