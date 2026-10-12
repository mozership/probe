# YAML Configuration Reference

This page documents every key Probe reads from a workflow file, the expression context each field is evaluated in, and the validation rules that apply.

## Workflow Structure

The skeleton below shows every property a workflow file can carry and where each one belongs.

```yaml
name: string                  # Required: workflow name
description: string           # Optional: what the workflow does
vars:                         # Optional: workflow variables
  key: value
secrets:                      # Optional: environment variables to hide in output
  - NAME
defaults:                     # Optional: default `with` values per action, for every job
  http:
    url: string
actions:                      # Optional: names for external actions
  name: string
jobs:                         # Required: a list of jobs
  - name: string              # Required: job name
    id: string                # Optional: job id, used by needs
    needs: [job-id, ...]      # Optional: job dependencies
    always: boolean           # Optional: run even when a needed job failed
    skipif: expression        # Optional: skip the job when true
    fail_fast: boolean        # Optional: skip the steps after one that failed
    defaults:                 # Optional: default `with` values per action
      http:
        url: string
    repeat:                   # Optional: run the job repeatedly
      count: integer
      interval: duration
    steps:                    # Required: a list of steps
      - name: string          # Optional: step name
        id: string            # Optional: step id, required to publish outputs
        uses: string          # Required: action name, or a name given under actions
        with:                 # Optional: action parameters
          key: value
        test: expression      # Optional: assertion
        echo: string          # Optional: text added to the report
        vars:                 # Optional: step variables
          key: value
        outputs:              # Optional: values published for later steps
          key: expression
        skipif: expression    # Optional: skip the step when true
        wait: duration        # Optional: wait before running the step
        timeout: duration     # Optional: step timeout
        iteration:            # Optional: run the step once per entry;
          - key: value        # a list, or a template that comes to one
        retry:                # Optional: retry on failure
          max_attempts: integer
          interval: duration
          initial_delay: duration
```

`jobs` is a **list**, not a mapping. A workflow that writes `jobs:` as a mapping of job ids fails to load.

There is no top-level `env` key. Environment variables are read through `vars`.

## Top-Level Properties

Six properties sit at the root of a workflow file alongside `jobs`: what to call it, what it does, the values it starts with, which of those values must not be shown, the `with` values its steps share, and the names it gives to external actions.

### `name`

**Type:** String (required)  
**Description:** Name of the workflow, shown at the top of the report.

```yaml
name: "API Health Check"
```

### `description`

**Type:** String (optional)  
**Description:** Longer explanation of what the workflow does.

```yaml
description: |
  Checks the production API:
  - endpoint availability
  - response times
```

### `vars`

**Type:** Object (optional)  
**Description:** Variables available to every job and step as `vars.<name>`.

`vars` is the only place where environment variables are visible, and they are referenced by their bare name. Values are evaluated once, before the first job starts.

```yaml
vars:
  # Read an environment variable
  api_url: "{{API_URL}}"

  # With a fallback
  timeout: "{{REQUEST_TIMEOUT ?? '30s'}}"

  # Computed at load time
  run_id: "{{random_str(8)}}"

  # Nested values are supported
  auth:
    user: "{{API_USER}}"
```

Expressions inside a step cannot read environment variables directly - there is no `env` in the expression context. Put the variable in `vars` and read `vars.<name>`.

A var can read another var as `vars.<name>`, wherever the other one is written in the block. Each var is evaluated after the vars it reads, so a value computed once, such as a random password, is the same in every var built from it:

```yaml
vars:
  http_port: "{{HTTP_PORT ?? '18080'}}"
  base: "http://localhost:{{vars.http_port}}"
  password: "{{PASSWORD ?? random_str(24)}}"
  basic_auth: "Basic {{encode_base64('alice:' + vars.password)}}"
```

A var that reads itself, directly or through other vars, stops the workflow before the first job starts with an error naming the chain, such as `vars: circular reference: a -> b -> a`. A name that is not a var reads as nil, so `vars.<name> ?? 'default'` gives the default. A var that reads `vars` by a key known only when it runs, as `vars[KEY]`, is evaluated after all the others. A var that calls `template`, such as one built from a template file, is evaluated after all the vars that do not, since what the template reads is known only when it runs. When the template reads another var that calls `template` and has not been evaluated yet, that var is evaluated first, and a cycle between them stops the workflow as one between other vars does.

The templates in a list are evaluated as those in a map are, at any depth, so `ports: ["{{vars.http_port}}"]` holds the port.

A var whose template cannot be evaluated, such as `{{nothing.here}}`, which reads a field of nil, stops the workflow before the first job starts with exit status 2. The error names the var and the template, as `vars.a: {{nothing.here}}: cannot fetch here from <nil>`. A var that reads one that failed is not evaluated, so its error is not repeated.

### `secrets`

**Type:** Array of strings (optional)  
**Description:** Names of environment variables whose values must not appear in anything Probe prints or writes.

```yaml
secrets:
  - API_TOKEN
  - DB_PASSWORD
vars:
  api_token: "{{API_TOKEN}}"
  db_password: "{{DB_PASSWORD}}"
```

Wherever the value of a listed variable would appear, Probe writes `<secret:NAME>` instead. That covers the report on the terminal, step names, `echo`, the request and response shown for a failed step, `--verbose` output, the log records of actions, and the files written by `--report`. The value is also hidden in the escaped forms it takes when it is quoted in an error message or encoded as JSON. Steps, tests and outputs still see the real value; only what is shown changes.

A name that is not set, or set to an empty string, has nothing to hide and is skipped. A short value is replaced wherever it appears, so a secret such as `1` would hide every `1` in the output.

Independently of `secrets`, the values of the `Authorization`, `Proxy-Authorization`, `Cookie` and `Set-Cookie` headers are always shown as `<redacted>`. A token obtained while the workflow runs, such as one returned by a login step and sent in a later request, is never listed in `secrets`, but it travels in one of these headers. Probe learns these values as an action is about to send or has received them, and hides them from then on, including in the action's own log records.

The same goes for credentials passed to an action: the value of any `password`, `key_passphrase`, `secret_access_key` or `session_token` field, at any depth of `with`, is shown as `<redacted>`, and so is the password in a `dsn` or a `url`, such as `redis://user:password@host`, while the rest of the URL stays visible. A password written straight into a step therefore does not appear in `--verbose` output or an action's log records, even though it is not listed in `secrets`. When the template of such a value cannot be evaluated, the error names the value, as `with.password`, but does not say why, since the template and the error may each quote the credential. A `url` is treated so only when the template is in its user info, as in `redis://app:{{vars.pw}}@host`; the error of a template elsewhere in a `url` is shown.

### `defaults`

**Type:** Object (optional)  
**Description:** Default `with` values for the steps of every job, keyed by action as a job's [`defaults`](#defaults-1) are.

A connection that every job uses, such as a base URL or a DSN, is written once here rather than in each job:

```yaml
defaults:
  db:
    dsn: "postgres://{{vars.user}}@{{vars.host}}/app"
  http:
    url: "{{vars.api_url}}"
    headers:
      accept: application/json

jobs:
  - name: Rows
    steps:
      - name: Count users
        uses: db
        with:
          query: SELECT count(*) AS n FROM users
        test: res.rows[0].n > 0

  - name: API
    defaults:
      http:
        headers:
          authorization: "Bearer {{vars.token}}"
    steps:
      - name: List users          # sent to the default url, with both headers
        uses: http
        with:
          get: /users
        test: res.code == 200
```

The closest value wins: what a step writes in `with`, then the `defaults` of its job, then those of the workflow. Objects are merged key by key, so the step above sends the `accept` header of the workflow and the `authorization` header of its job. A template in a default is evaluated when the step runs, with the step's own `vars` and `outputs`.

The defaults of a workflow do not reach a job file run by the [embedded](/reference/actions/embedded) action, which is a file of its own with its own `defaults`.

### `actions`

**Type:** Object of strings (optional)  
**Description:** Names for the [external actions](/guide/concepts/actions#external-actions) the workflow uses. A step's `uses` and a key of `defaults`, of a job or of the workflow, can then be the name in place of the repository and commit.

```yaml
actions:
  graphql: github.com/mozership/probe-graphql@41e4ffa222db58c63c7169117c919e6d252bdbf9 # v0.2.0

jobs:
  - name: Countries API
    defaults:
      graphql:
        url: https://countries.trevorblades.com/graphql
    steps:
      - name: Look up Japan
        uses: graphql
        with:
          query: '{ country(code: "JP") { capital } }'
        test: res.data.country.capital == "Tokyo"
```

The commit an action is pinned to is written once, so a newer release is a change to one line.

- A name is letters, digits, `_` and `-`, and starts with a letter or a digit.
- A name cannot be that of an action of Probe, such as `http` or `shell`: what `uses: http` runs does not depend on the workflow. A workflow that gives such a name fails to load.
- The value names an external action as `uses` does: `github.com/<owner>/<repo>[/<dir>]@<commit>` with a full 40-character commit SHA, or a local path starting with `./`, `../` or `/`. It is not a template, and it cannot be another name.
- A step can still name an action in full, and the `defaults` written by the name apply to it too. One `defaults` cannot hold both the name and the action it stands for.

Probe replaces each name by its action as it loads the workflow, so everything else sees the action in full. In particular, `--allow-action` takes the action, not the name: a workflow cannot choose what a name the person running Probe has allowed stands for.

A job file run by the [embedded](/reference/actions/embedded) action is read by the names of the workflow that embeds it, so its steps and its `defaults` can use them too. See [Named External Actions](/reference/actions/embedded#named-external-actions).

## Jobs

`jobs` is a map from job name to job definition. A job groups the steps that run in sequence, and declares what it depends on.

### Job Properties

A job definition takes the properties below.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `name` | String | Yes | Job name, shown in the report. Supports template expressions |
| `id` | String | No | Identifier used by another job's `needs`. Generated automatically when omitted |
| `needs` | Array | No | Ids of jobs that must finish first |
| `always` | Boolean | No | Run once the jobs in `needs` are over, even when one of them failed |
| `steps` | Array | Yes | The steps to run |
| `skipif` | Expression | No | Skip the whole job when the expression is true |
| `fail_fast` | Boolean | No | Skip the steps after one that failed |
| `defaults` | Object | No | Default `with` values, keyed by action name |
| `repeat` | Object | No | Run the job repeatedly |

There is no `if`, `continue_on_error` or `timeout` at the job level. A job that has to run after a failure is one with [`always`](#always).

#### `needs`

Jobs without dependencies start in parallel. `needs` refers to job **ids**, so a job that others depend on needs an explicit `id`.

```yaml
jobs:
  - id: setup
    name: Setup
    steps:
      - name: Prepare
        uses: hello
        echo: "ready"

  - name: Test
    needs: [setup]
    steps:
      - name: Run
        uses: hello
        echo: "testing"
```

#### `always`

A job is skipped when a job it `needs` failed or was skipped for the same reason. With `always: true`, it runs once every job in `needs` is over, however each ended. That is what a job that removes what the others made needs: left without it, a failed run leaves its test database or its files behind, and the next run finds them.

```yaml
jobs:
  - id: setup
    name: Setup
    steps:
      - name: Create the test database
        uses: db
        with:
          dsn: "{{vars.admin_dsn}}"
          query: CREATE DATABASE probe_test
        test: res.code == 0

  - id: tests
    name: Tests
    needs: [setup]
    steps:
      - name: Count users
        uses: db
        with:
          dsn: "{{vars.test_dsn}}"
          query: SELECT count(*) AS n FROM users
        test: res.rows[0].n > 0

  - name: Cleanup
    needs: [setup, tests]
    always: true
    steps:
      - name: Drop the test database
        uses: db
        with:
          dsn: "{{vars.admin_dsn}}"
          query: DROP DATABASE IF EXISTS probe_test
        test: res.code == 0
```

- The job still waits for every job in `needs`, a repeated one for all of its runs. List every job whose work it undoes, so that it does not run beside one of them.
- The run fails as it would have without it: `always` changes which jobs run, not the exit status.
- An output a failed or skipped job did not publish reads as nil, so write the steps to work without it, as `DROP DATABASE IF EXISTS` does.
- A job that needs the one with `always` is held to how that one ends, as it is to any other job.
- `skipif` still applies: a job with both is skipped when its expression is true.

#### `skipif`

A boolean expression. The job is skipped when it evaluates to true.

```yaml
jobs:
  - name: Production only
    skipif: vars.environment != "production"
    steps:
      - name: Check
        uses: http
        with:
          method: GET
          url: "{{vars.api_url}}/health"
```

#### `fail_fast`

The steps of a job run one after another, and a step runs whether or not the one before it failed. That suits steps that check separate things, each of which is worth its own line in the report. When the later steps work on what an earlier one made, they fail because it did, and the one failure that matters is buried under those that follow from it. With `fail_fast: true`, the steps after a failed one are not run, and are reported as skipped:

```yaml
jobs:
  - name: Orders
    fail_fast: true
    steps:
      - name: Create the table
        uses: db
        with:
          query: CREATE TABLE orders (id INTEGER PRIMARY KEY, total INTEGER)
        test: res.code == 0

      - name: Insert an order          # skipped when the table was not created
        uses: db
        with:
          query: INSERT INTO orders (total) VALUES (?)
          params: [100]
        test: res.rows_affected == 1
```

- A step fails when its `test` is false, its action returns an error, or a response breaks its contract. A step without a `test` that nothing checks does not fail, so it stops nothing.
- A skipped step does not wait for its `wait`, and its `outputs` are not published.
- The remaining entries of an [`iteration`](#iteration) are skipped as well, once one of them fails.
- Each run of a job with [`repeat`](#repeat) starts over: a failure skips the rest of that run only. A run in which a step was not run counts as neither a success nor a failure of it, and is told beside them, as `1/3 success (33.3%), 2 skipped`.
- Other jobs are not affected, except through [`needs`](#needs), as by any failed job. Steps that must run after a failure, such as a cleanup, belong in a job with [`always`](#always).

#### `defaults`

Default parameters merged into the `with` of every step in the job that uses the matching action. A value set on the step wins, and a value set here wins over the [`defaults` of the workflow](#defaults), which every job shares. The key is what a step writes in `uses`: the name of a built-in action, an external action in full, or a name given under [`actions`](#actions).

```yaml
jobs:
  - name: API checks
    defaults:
      http:
        url: "{{vars.api_url}}"
        headers:
          authorization: "Bearer {{vars.token}}"
          accept: application/json
    steps:
      - name: Health
        uses: http
        with:
          get: /health        # resolved against the default url
        test: res.code == 200
```

The `http` action requires a method. Either set `method` explicitly, or use the shorthand key `get` / `post` / `put` / `delete` / `patch`, whose value is a full URL or a path resolved against `url`.

```yaml
      - name: Explicit method
        uses: http
        with:
          url: "{{vars.api_url}}/health"
          method: GET
```

#### `repeat`

`repeat` runs the whole job again on an interval, which is what turns a test into a monitor.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `count` | Integer | Yes | Number of runs. Capped by `PROBE_MAX_REPEAT_COUNT` (default 10000) |
| `interval` | Duration | No | Wait between runs |
| `async` | Boolean | No | Run the repetitions concurrently |

```yaml
jobs:
  - name: Poll until ready
    repeat:
      count: 10
      interval: 5s
    steps:
      - name: Check
        uses: http
        with:
          method: GET
          url: "{{vars.api_url}}/status"
        test: res.code == 200
```

Each run's steps read the outputs that the earlier steps of the same run published, also when `async` runs them at the same time. The jobs after it read each output as the latest run that published it left it, so a step the last run skipped keeps the value of the run before.

## Steps

A step is one action invocation plus what surrounds it: the condition that decides whether it runs, the test that decides whether it passed, and the outputs it leaves for later steps.

### Step Properties

A step definition takes the properties below.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `uses` | String | Yes | Action to run: a built-in such as `http` or `shell`, or an [external action](/guide/concepts/actions#external-actions) |
| `name` | String | No | Step name, shown in the report |
| `id` | String | No | Identifier that namespaces this step's `outputs` |
| `with` | Object | No | Action parameters |
| `test` | Expression | No | Assertion. The step fails when it is false |
| `echo` | String | No | Text added to the report |
| `vars` | Object | No | Variables local to the step |
| `outputs` | Object | No | Values published for later steps and jobs |
| `skipif` | Expression | No | Skip the step when true |
| `wait` | Duration | No | Wait before running the step |
| `timeout` | Duration | No | Step timeout (default 5m) |
| `iteration` | Array or template | No | Run the step once per entry, exposed as `vars` |
| `retry` | Object | No | Retry the step on failure |

The key is `uses`, not `action`, and the conditional key is `skipif`, not `if`.

#### `outputs`

Values published for use by later steps and jobs. **A step only publishes outputs when it has an `id`** - without one the `outputs` block is discarded.

Each value is an expression, not a template, so it is written without <span v-pre>`{{ }}`</span>.

```yaml
steps:
  - name: Log in
    id: auth
    uses: http
    with:
      url: "{{vars.api_url}}/login"
      method: POST
    test: res.code == 200
    outputs:
      token: res.body.access_token
      user_id: res.body.user.id
```

Later steps read them either namespaced by step id or by the output name alone:

```yaml
      headers:
        Authorization: "Bearer {{outputs.auth.token}}"
        X-User: "{{outputs.user_id}}"
```

An id containing a hyphen is not a valid identifier in an expression, so it has to be read with brackets:

```yaml
    echo: "{{outputs['create-user'].user_id}}"
```

#### `retry`

`retry` repeats a single step until its test passes or the attempts run out.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `max_attempts` | Integer | Yes | Total attempts, at least 1. Capped by `PROBE_MAX_ATTEMPTS` (default 10000) |
| `interval` | Duration | No | Wait between attempts |
| `initial_delay` | Duration | No | Wait before the first attempt |

```yaml
steps:
  - name: Flaky endpoint
    uses: http
    with:
      method: GET
      url: "{{vars.api_url}}/slow"
    test: res.code == 200
    retry:
      max_attempts: 3
      interval: 2s
```

#### `iteration`

Runs the step once per entry. Each entry's keys are exposed through `vars`.

```yaml
steps:
  - name: Check {{vars.path}}
    uses: http
    iteration:
      - path: /health
      - path: /metrics
      - path: /version
    with:
      method: GET
      url: "{{vars.api_url}}{{vars.path}}"
    test: res.code == 200
```

In place of a list written in the workflow, `iteration` takes one template that comes to a list of objects. It is evaluated when the step is reached, with the workflow's and the step's `vars` and the `outputs` published so far, so the entries can be cases kept in a file or what an earlier step found:

```yaml
steps:
  # one run for each case of the file, which can grow without the workflow
  - name: "{{vars.name}} can sign in"
    uses: http
    iteration: "{{ parse_json(file('testdata/users.json')) }}"
    with:
      post: /login
      body:
        user: "{{vars.name}}"
        password: "{{vars.password}}"
    test: res.code == 200

  - name: List orders
    id: orders
    uses: db
    with:
      query: SELECT id, total FROM orders WHERE status = 'open'
    test: res.code == 0
    outputs:
      rows: res.rows

  # one run for each row the step above read
  - name: "Order {{vars.id}} has a total"
    uses: http
    iteration: "{{ outputs.orders.rows }}"
    with:
      get: "/orders/{{vars.id}}"
    test: res.body.total == vars.total
```

- The value is one `{{ }}` template and nothing else; text around it, or two templates, would come to text, and the workflow fails to load.
- Each entry is an object, whose keys the step reads as `vars`. A list of anything else, such as strings, fails the step; turn it into objects with `map`, as `"{{ map(vars.paths, ({path: #})) }}"`.
- A template that cannot be evaluated, or that does not come to a list, fails the step without running it.
- A list without entries skips the step, which is reported as skipped: there is nothing to run it with. An `iteration` written as an empty list, `[]`, runs the step once, as a step without `iteration` does.
- The entries are taken as they are: a `{{ }}` in a value of an entry is not evaluated.

## The Expression Context

Inside a step, an expression sees exactly these names:

| Name | Type | Description |
|------|------|-------------|
| `vars` | Object | Workflow variables merged with the step's own `vars` |
| `res` | Object | The action's response |
| `req` | Object | The request as it was sent |
| `rt` | Object | Response time: `rt.duration` (string) and `rt.sec` (float seconds) |
| `status` | Integer | Action exit status, `0` on success |
| `outputs` | Object | Outputs published by earlier steps |
| `repeat_index` | Integer | Current index when the job repeats |

There is no `env`, `jobs` or `steps` in this context.

### The `res` Object for `http`

Each action defines its own `res`. For `http` it holds the following.

| Field | Type | Description |
|-------|------|-------------|
| `res.code` | Integer | Status code, such as `200` |
| `res.status` | String | Status line, such as `"200 OK"` |
| `res.headers` | Object | Response headers, keyed by canonical name such as `Content-Type` |
| `res.body` | Any | Response body. Parsed into an object or array when the response is JSON, otherwise the raw string |
| `res.rawbody` | String | The unparsed body, present when the body was parsed as JSON |

```yaml
    test: |
      res.code == 200 &&
      res.headers["Content-Type"] contains "application/json" &&
      res.body.status == "ok" &&
      rt.sec < 1
```

Other actions publish their own `res` fields; see the [Actions Reference](/reference/actions-reference).

## Data Types

Two kinds of value in a workflow file have their own syntax: durations, and the expressions and templates that read the context.

### Duration

A duration is either a Go duration string or a plain number of seconds.

```yaml
timeout: "30s"
timeout: "5m"
interval: 10        # 10 seconds
```

### Expressions and Templates

A **template expression** appears inside a string and is replaced by its value:

```yaml
url: "{{vars.api_url}}/users/{{outputs.auth.user_id}}"
```

A **boolean expression** is written bare, without braces:

```yaml
test: res.code == 200 && rt.sec < 2
skipif: vars.environment == "local"
```

`outputs` values are expressions too, so they take no braces.

A template that cannot be evaluated is an error, never text in the value. A name that is not defined reads as nil and is not an error, but reading a field of nil is, as is an expression that does not parse:

| Where the template is | What happens |
|---|---|
| A step's `with`, `vars` or `name` | The step fails with the kind `template`, and the action does not run. The error names the value, as `with.headers.authorization` |
| A workflow's `vars` | The workflow stops before the first job, with exit status 2 |
| A job's `name` | The job fails, with exit status 2 |
| A step's `echo` | The error is shown in place of the text, and the step's result does not change |

```yaml
# Fails the step when no earlier step published outputs.login
headers:
  authorization: "Bearer {{outputs.login.token}}"

# Falls back instead
headers:
  authorization: "Bearer {{outputs.login?.token ?? 'none'}}"
```

See [Built-in Functions](/reference/built-in-functions) for what can be called inside an expression.

## Validation Rules

- `name` is required at the workflow level, and `jobs` must be a non-empty list.
- Every job needs a `name` and at least one step.
- Every step needs a `uses`, and it must name an action: one of Probe, an external action, or a name given under `actions`. One that names none stops the run before its first job, with exit status 2, even in a step that would be skipped.
- A name under `actions` must not be that of an action of Probe, and must stand for an external action pinned to a commit or at a local path.
- A job's `needs` must refer to ids that exist, and the dependency graph must be acyclic.
- `repeat.count` must be zero or greater, and `retry.max_attempts` at least 1.
- A step must have an `id` for its `outputs` to be published.

`probe check` reports these without running the workflow, together with keys a workflow does not take, expressions that do not parse and outputs read before they are published. See the [CLI Reference](/reference/cli-reference#check).

## File Merging

Several files can be combined by passing them comma-separated. They are concatenated in order, and a top-level key defined more than once takes the value from the last file:

```bash
probe base.yml,production.yml
```

See [File Merging](/guide/concepts/file-merging) for the merge rules.

## See Also

- **[CLI Reference](/reference/cli-reference)** - Command-line options
- **[Actions Reference](/reference/actions-reference)** - Action parameters and responses
- **[Built-in Functions](/reference/built-in-functions)** - Expression functions
- **[Environment Variables](/reference/environment-variables)** - Variables Probe reads
