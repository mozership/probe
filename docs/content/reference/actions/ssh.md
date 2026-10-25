# SSH Action

The SSH Action allows you to connect to remote servers via SSH and execute commands. It's ideal for deployment, remote monitoring, and server management tasks.

## Basic Usage

An SSH step needs a host, a user, a way to authenticate, and the command to run.

```yaml
- name: Check server status
  uses: ssh
  with:
    host: "example.com"
    user: "deploy"
    password: "your_password"
    cmd: "uptime && df -h"
  test: res.code == 0
```

## Parameters

The parameters fall into four groups: the ones every step needs, the authentication parameters of which at least one must be given, the optional ones that tune the connection, and the environment variables that can supply values instead.

### Required Parameters

These three have no default and must be given on every step.

| Parameter | Type | Description |
|-----------|------|-------------|
| `host` | string | Target hostname or IP address |
| `user` | string | SSH username |
| `cmd` | string | Command to execute |

### Authentication Parameters (one required)

Give `password`, `key_file`, or both; with both, the server may accept either. An encrypted key needs its passphrase alongside it.

| Parameter | Type | Description |
|-----------|------|-------------|
| `password` | string | Password authentication |
| `key_file` | string | Private key file path (supports `~` expansion) |
| `key_passphrase` | string | Private key passphrase (for encrypted keys) |

### Optional Parameters

The rest tune the connection and fall back to the defaults below when omitted.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `port` | int | 22 | SSH port number |
| `timeout` | string | "30s" | Limit for connecting and for running the command, as a duration such as `"30s"` or `"5m"`, or a number of seconds such as `30`. A value that is neither, or not above zero, is rejected. See below for a command that runs past it |
| `workdir` | string | - | Working directory on remote server |
| `strict_host_check` | bool | true | Strict host key verification |
| `known_hosts` | string | `~/.ssh/known_hosts` and `/etc/ssh/ssh_known_hosts` | Known hosts file path. By default both files are read, as many of them as exist; with neither, a strict host check fails |

A command that runs past `timeout` is sent `SIGTERM`, and the step still gets a result to test: `res.timed_out` is `true`, `res.stdout` and `res.stderr` hold what the command wrote until then, and `res.code` is the status it exits with once signalled, often `143`. A command that has not stopped 5 seconds later is given up on, with `res.code` `-1`.

### Environment Variables

Set environment variables for the remote command with `env`:

```yaml
with:
  env:
    NODE_ENV: production
    APP_VERSION: v1.2.3
```

Numbers and booleans are passed as their text. The flat form `env__NODE_ENV: production` is accepted as well; when a name is given both ways, the value in `env` wins.

The variables are sent through the SSH session, and the server accepts only the names its `sshd_config` allows with `AcceptEnv`. A name the server refuses is skipped: Probe logs a warning that gives the name, never the value, and the command runs without it. Add the names the command needs to `AcceptEnv` on the server.

Do not work around a refusal by writing the value into `cmd`. The remote shell interprets whatever is embedded in the command, so a value from an untrusted source, such as a response, can inject commands, and a secret there is visible in the remote host's process list.

## Return Values

The SSH Action returns the following values:

| Field | Type | Description |
|-------|------|-------------|
| `res.code` | int | Exit code (0 = success) |
| `res.stdout` | string | Standard output |
| `res.json` | any | `res.stdout` decoded, when the whole of it is one JSON object or array; unset otherwise |
| `res.stderr` | string | Standard error output |
| `res.timed_out` | bool | `true` when the command was stopped at `timeout` |
| `status` | int | Execution status (0 = success, 1 = failure) |
| `rt.duration` | string | How long the step took, such as `"1.2s"` |
| `rt.sec` | float | The same in seconds |

## Examples

The examples below cover each authentication method in turn, then the cases that need more than a single command: environment variables, multi-line scripts, and failures you want to inspect.

### Password Authentication

The simplest form, though the password should come from the environment rather than the file.

```yaml
- name: Login with password
  uses: ssh
  with:
    host: "server.example.com"
    port: 22
    user: "admin"
    password: "secure_password"
    cmd: "systemctl status nginx"
    timeout: "60s"
  test: res.code == 0
```

### Public Key Authentication

`key_file` points at the private key and expands `~`.

```yaml
- name: Login with SSH key
  uses: ssh
  with:
    host: "prod.example.com"
    user: "deploy"
    key_file: "~/.ssh/deploy_key"
    cmd: "git pull origin main && systemctl restart app"
    workdir: "/opt/myapp"
    timeout: "300s"
  test: res.code == 0
```

### Encrypted Private Key

A key protected by a passphrase needs `key_passphrase` as well as the file.

```yaml
- name: Login with passphrase-protected key
  uses: ssh
  with:
    host: "secure.example.com"
    user: "admin"
    key_file: "~/.ssh/id_rsa"
    key_passphrase: "key_passphrase"
    cmd: "docker ps"
  test: res.code == 0
```

### Using Environment Variables

Every parameter accepts a template, so the host and the credentials can come from outside the file.

```yaml
- name: Deploy with environment variables
  uses: ssh
  with:
    host: "app.example.com"
    user: "deploy"
    key_file: "~/.ssh/deploy_key"
    cmd: |
      echo "Deploying version: $APP_VERSION"
      echo "Environment: $DEPLOY_ENV"
      ./deploy.sh
    env:
      APP_VERSION: "v2.1.0"
      DEPLOY_ENV: "production"
    workdir: "/opt/myapp"
    timeout: "600s"
  test: res.code == 0 && res.stdout contains "Deploy completed"
```

### Multi-line Commands

A block scalar sends several commands in one session, and the combined output comes back as `res.stdout`.

```yaml
- name: Collect system information
  uses: ssh
  with:
    host: "monitor.example.com"
    user: "admin"
    password: "admin_pass"
    cmd: |
      echo "=== System Information ==="
      uname -a
      echo "=== Disk Usage ==="
      df -h
      echo "=== Memory Usage ==="
      free -h
      echo "=== Process List ==="
      ps aux | head -10
    timeout: "120s"
  test: res.code == 0
  echo: "{{res.stdout}}"
```

### Error Handling

A remote command can fail after the connection succeeds, so the test examines the exit code and the error output.

```yaml
- name: Restart service with error handling
  uses: ssh
  with:
    host: "app.example.com"
    user: "deploy"
    key_file: "~/.ssh/deploy_key"
    cmd: |
      if systemctl is-active --quiet myapp; then
        echo "Stopping service..."
        systemctl stop myapp
      fi
      echo "Starting service..."
      systemctl start myapp
      systemctl is-active myapp
    timeout: "180s"
  test: |
    res.code == 0 && 
    res.stdout contains "active" &&
    !(res.stderr contains "failed")
```

## Security Settings

Host key verification decides whether Probe trusts the server it reaches. It is controlled by `strict_host_check` and `known_hosts`.

### Host Key Verification

With `strict_host_check` on, the server's key must already be in `known_hosts`.

```yaml
- name: Strict host key verification
  uses: ssh
  with:
    host: "secure.example.com"
    user: "admin"
    key_file: "~/.ssh/id_rsa"
    cmd: "whoami"
    strict_host_check: true
    known_hosts: "~/.ssh/known_hosts"
  test: res.code == 0
```

### Disable Host Key Verification (Development Only)

Turning the check off accepts any key, which is only acceptable against a host you control.

```yaml
- name: Development environment connection
  uses: ssh
  with:
    host: "dev.example.com"
    user: "developer"
    password: "dev_password"
    cmd: "npm test"
    strict_host_check: false  # Use only in development
  test: res.code == 0
```

## Best Practices

The following points come up whenever an SSH step runs outside a local experiment: which credential to use, how long to wait, what to do with a failure, and what ends up in the log.

### 1. Authentication Method Selection

**Prefer public key authentication**:
```yaml
# Recommended
key_file: "~/.ssh/deploy_key"

# Avoid if possible
password: "plain_password"
```

### 2. Timeout Configuration

Set appropriate timeout values:
```yaml
# Short commands
timeout: "30s"

# Long operations like deployment
timeout: "600s"
```

### 3. Error Handling

A command can exit zero and still have reported a problem, so check `res.stderr` as well.

```yaml
test: |
  res.code == 0 && 
  !(res.stderr contains "error") &&
  !(res.stderr contains "failed")
```

### 4. Log Safety

Password and private key information is automatically excluded from logs.

### 5. Environment Variable Usage

Use environment variables for sensitive information:
```yaml
vars:
  ssh_password: "{{vars.SSH_PASSWORD}}"
  deploy_key: "{{vars.DEPLOY_KEY_PATH}}"

steps:
- uses: ssh
  with:
    password: "{{vars.ssh_password}}"
    key_file: "{{vars.deploy_key}}"
```

## Limitations

Two things the SSH action deliberately does not do.

### No SSH Config File Support

To maintain portability and reproducibility, SSH config files (`~/.ssh/config`) are not supported. All connection parameters must be explicitly defined in the workflow.

### Interactive Commands

Interactive commands that require user input are not supported. All commands must run non-interactively.

## Troubleshooting

Most SSH failures come from the handshake rather than the command. The symptoms below list what to check first.

### Connection Errors

**Host key verification failed**:
```yaml
# Check known_hosts
known_hosts: "~/.ssh/known_hosts"

# Or disable for development
strict_host_check: false
```

**Authentication failed**:
```yaml
# Check key file path and permissions
key_file: "/home/user/.ssh/id_rsa"  # Absolute path
# or
key_file: "~/.ssh/id_rsa"          # Tilde expansion

# Add passphrase if needed
key_passphrase: "your_passphrase"
```

**Timeout errors**:
```yaml
# Increase timeout
timeout: "300s"
```

### Consecutive Execution Issues

If you experience issues with consecutive connections to the same host, add appropriate delays:

```yaml
- name: First connection
  uses: ssh
  with:
    cmd: "command1"
  
- name: Brief wait
  uses: hello
  with:
    name: "wait"
  wait: 1s
  
- name: Second connection
  uses: ssh
  with:
    cmd: "command2"
```

## Related Actions

- [Shell Action](/reference/actions/shell) - Local command execution
- [HTTP Action](/reference/actions/http) - REST API communication
- [Environment Variables](/reference/environment-variables)
