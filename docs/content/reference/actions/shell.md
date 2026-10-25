# Shell Action

The `shell` action executes shell commands and scripts securely, providing comprehensive output capture and error handling.

## Basic Syntax

A shell step needs the command; everything else has a default.

```yaml
steps:
  - name: "Execute Build Script"
    uses: shell
    with:
      cmd: "npm run build"
    test: res.code == 0
```

## Parameters

A shell step needs a command, and optionally the interpreter, the directory and the environment it runs in, plus a limit on how long it may take.

### `cmd` (required)

**Type:** String  
**Description:** The shell command to execute  
**Supports:** Template expressions

```yaml
vars:
  api_url: "{{API_URL}}"

with:
  cmd: "echo 'Hello World'"
  cmd: "npm run {{vars.build_script}}"
  cmd: "curl -f {{vars.api_url}}/health"
```

### `shell` (optional)

**Type:** String  
**Default:** `/bin/sh`  
**Allowed Values:** `/bin/sh`, `/bin/bash`, `/bin/zsh`, `/bin/dash`, `/usr/bin/sh`, `/usr/bin/bash`, `/usr/bin/zsh`, `/usr/bin/dash`

```yaml
with:
  cmd: "echo $0"
  shell: "/bin/bash"
```

### `workdir` (optional)

**Type:** String  
**Description:** Working directory for command execution  
**Supports:** Template expressions

```yaml
with:
  cmd: "pwd && ls -la"
  workdir: "/app/src"
  workdir: "{{vars.project_path}}"
```

A relative path is resolved against the directory Probe is run from, not the directory of the workflow file. The directory must exist.

### `timeout` (optional)

**Type:** String or Duration  
**Default:** `30s`  
**Format:** Go duration format (`30s`, `5m`, `1h`) or a number of seconds (`30`, `1.5`). A value that is neither, or not above zero, such as `0`, is rejected

```yaml
with:
  cmd: "npm test"
  timeout: "10m"
  timeout: "300"  # 300 seconds
```

A command that runs past `timeout` is stopped, and the step still gets a result to test: `res.timed_out` is `true`, `res.code` is `-1`, and `res.stdout` and `res.stderr` hold what the command wrote until then. A process the command started in the background is not waited for: once the command has exited or been stopped, its output is read for at most one more second.

### `env` (optional)

**Type:** Object  
**Description:** Environment variables to set for the command  
**Supports:** Template expressions in values

```yaml
vars:
  production_api_url: "{{PRODUCTION_API_URL}}"

with:
  cmd: "npm run build"
  env:
    NODE_ENV: "production"
    API_URL: "{{vars.production_api_url}}"
    BUILD_VERSION: "{{vars.version}}"
```

Numbers and booleans are passed as their text, so `PORT: 8080` sets `PORT` to `8080`. The variables are added to the environment Probe itself runs with.

### `background` (optional)

**Type:** Boolean  
**Default:** `false`  
**Description:** Start the command and move on without waiting for it to finish

```yaml
with:
  cmd: "python3 -m http.server 8080 --bind 127.0.0.1"
  background: true
```

A background command is for something the following steps need running, such as a server under test. The step returns as soon as the command has started, so `res.code` is `-1` and `res.stdout` and `res.stderr` are empty. `timeout` does not apply, unless `ready` is given.

- **Output:** stdout and stderr both go to a log file of its own, whose path is in `res.log`. Starting the same command twice gives two files.
- **Lifetime:** the command keeps running after its step and after its job, so steps in later jobs can use it too. When the workflow is over, Probe sends `SIGTERM` to the command and everything it started, sends `SIGKILL` to whatever is left after 3 seconds, and removes the log file. Read the log in a step if it is needed afterwards.
- **Interruption:** when Probe is interrupted with Ctrl+C, or receives `SIGTERM` or `SIGHUP`, it stops the command the same way before it exits, and a second Ctrl+C ends Probe at once. Only a Probe killed outright, such as with `SIGKILL`, leaves the command and its log file behind.

A command started inside an [embedded](/reference/actions/embedded) job is stopped when that job is over.

### `ready` (optional)

**Type:** Object  
**Description:** For a background command, wait until it writes `log`, the text it writes once it is ready, before the step returns

```yaml
- name: Start the server
  uses: shell
  with:
    cmd: python3 -u -m http.server 8080 --bind 127.0.0.1
    background: true
    ready:
      log: Serving HTTP
  test: status == -1

- name: Use it at once
  uses: http
  with:
    get: http://127.0.0.1:8080/
  test: res.code == 200
```

Without `ready`, a step after a background command has to find out on its own when the command is ready, usually by retrying a request; and when the command fails to start, the retries run out with nothing saying why. With `ready`, the step itself waits, and fails at once with what the command wrote:

| What happens first | `status` | `res.code` | `res.timed_out` | `res.stdout` |
|---|---|---|---|---|
| The command writes the text, to stdout or stderr | `-1` | `-1` | `false` | Empty, as for any background command |
| The command exits, and nothing it started is left running | `1` | Its exit code | `false` | What it wrote |
| `timeout` passes | `1` | `-1` | `true` | What it wrote |

- **Waiting:** the text is looked for in the log as the command writes it, as plain text and not a pattern. `timeout`, `30s` unless it is given, bounds the wait. A command that is not ready by then is stopped as the workflow would stop it, so a retry of the step does not find the last one still holding its port.
- **A shell that exits early:** a command such as `server &` ends its shell while what it started goes on running. The wait goes on while anything it started is left in the shell's process group, since that may still write the text. A process that leaves the group, as a daemon that starts a session of its own does, is not followed: once the rest is gone the command counts as exited, and the process is not stopped with the others. Run such a program in the foreground, as with a `--foreground` or `-f` option, if it has one.
- **Buffered output:** a program that buffers what it writes when it is not writing to a terminal, as Python does, may hold the text back; make it write at once, as with `python3 -u`.

`ready` is refused before the command runs when the command is not in the background, when it is not a map, when it has a key other than `log`, and when `log` is missing, empty or not a string; quote a number, as `log: "8080"`.

## Response Format

The result carries the exit code and both output streams.

```yaml
res:
  code: 0                    # Exit code (0 = success)
  stdout: "Build successful" # Standard output
  stderr: ""                 # Standard error output
  pid: 12345                 # Process ID of the shell
  timed_out: false           # true when the command was stopped at timeout

req:
  cmd: "npm run build"       # Original command
  shell: "/bin/sh"          # Shell used
  workdir: "/app"           # Working directory
  timeout: "30s"            # Timeout setting
  env:                      # Environment variables
    NODE_ENV: "production"
  background: false         # Background setting
  ready: {}                 # Ready setting
```

With `background: true`, the command is still running when the result is made:

```yaml
res:
  code: -1                   # Not finished yet
  stdout: ""                 # Empty: the output goes to the log
  stderr: ""
  pid: 12345                 # Process ID of the shell
  log: "/tmp/probe-shell-action.1234567890.log" # Log file for stdout and stderr
```

When the whole of `stdout` is one JSON object or array, surrounding whitespace aside, it is also decoded into `res.json`, so a command that prints JSON is read as a response body is, without `parse_json(res.stdout)` in each expression. `res.stdout` keeps the text.

```yaml
- name: Find the email
  id: found
  uses: shell
  with:
    cmd: ./driver find-emails -subject 'inbound'   # prints {"count":1,"emails":[{"id":"M1"}]}
  test: res.code == 0 && res.json.count == 1
  outputs:
    id: res.json.emails[0].id
```

Anything else leaves `res.json` unset, which reads as nil: plain text, a scalar such as `42`, JSON Lines, and output that is not valid JSON. A background command has no `stdout`, so it has no `res.json` either; read its log file with `parse_json` instead.

Besides `res` and `req`, the step can test these:

| Field | Type | Description |
|-------|------|-------------|
| `status` | Integer | `0` when the exit code is `0`, `1` otherwise, `-1` for a background command |
| `rt.duration` | String | How long the command took, such as `"4.7ms"` |
| `rt.sec` | Float | The same in seconds |

## Usage Examples

The examples below go from a single command to a pipeline that builds, tests, deploys per environment, and reports what failed.

### Basic Command Execution

A single command, with its exit code as the test.

```yaml
- name: "System Information"
  uses: shell
  with:
    cmd: "uname -a"
  test: res.code == 0
```

### Build and Test Pipeline

Each stage is its own step, so a failure names the stage that broke.

```yaml
- name: "Install Dependencies"
  uses: shell
  with:
    cmd: "npm ci"
    workdir: "/app"
    timeout: "5m"
  test: res.code == 0

- name: "Run Tests"
  uses: shell
  with:
    cmd: "npm test"
    workdir: "/app"
    env:
      NODE_ENV: "test"
      CI: "true"
  test: res.code == 0 && res.stdout contains "All tests passed"
```

### Environment-specific Deployment

The command itself can be assembled from variables, which is how one step deploys to different targets.

```yaml
vars:
  target_env: "{{TARGET_ENV}}"
  deploy_key: "{{DEPLOY_KEY}}"
```

```yaml
- name: "Deploy to Environment"
  uses: shell
  with:
    cmd: "./deploy.sh {{vars.target_env}}"
    workdir: "/deploy"
    shell: "/bin/bash"
    timeout: "15m"
    env:
      DEPLOY_KEY: "{{vars.deploy_key}}"
      TARGET_ENV: "{{vars.target_env}}"
  test: res.code == 0
```

### Server for Later Steps

A server started in the background is left running for the steps after it. The next step retries until the server answers, and the last one reads what the server logged before Probe removes the log.

```yaml
- name: "Start the Server"
  id: server
  uses: shell
  with:
    cmd: "python3 -m http.server 8080 --bind 127.0.0.1"
    background: true
  test: res.code == -1 && res.pid > 0
  outputs:
    log: res.log

- name: "Wait Until It Answers"
  uses: http
  with:
    url: "http://127.0.0.1:8080"
    get: "/"
  retry:
    max_attempts: 20
    interval: "500ms"
  test: res.code == 200

- name: "Show the Server Log"
  uses: shell
  with:
    cmd: "cat {{outputs.server.log}}"
  test: res.code == 0
```

### Error Handling and Debugging

When the command's own exit code is not enough, the test reads `res.stdout` and `res.stderr`.

```yaml
- name: "Service Health Check"
  uses: shell
  with:
    cmd: "curl -sS http://localhost:8080/health"
  test: res.code == 0 && res.stdout contains "ok" && res.stderr == ""

- name: "Debug Failed Build"
  uses: shell
  with:
    cmd: "npm run build:debug"
  # Allow failure to capture debug output
  outputs:
    debug_info: res.stderr
```

## Security Features

The shell action implements several security measures:

- **Shell Path Restriction**: Only allows approved shell executables
- **Working Directory Validation**: Rejects a directory that does not exist
- **Timeout Protection**: Stops a command that runs longer than `timeout`

The command runs with Probe's own environment plus `env`, and its output is returned as it is.

## Error Handling

Common exit codes and their meanings:

- **0**: Success
- **1**: General error
- **2**: Misuse of shell builtins
- **126**: Command cannot execute (permission denied)
- **127**: Command not found
- **130**: Script terminated by Ctrl+C
- **255**: Exit status out of range

```yaml
- name: "Handle Different Exit Codes"
  uses: shell
  with:
    cmd: "some_command_that_might_fail"
  test: |
    res.code == 0 ? true :
    res.code == 127 ? res.stderr contains "not found" :
    res.code < 128
```
