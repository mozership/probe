# Environment Variables Reference

This page provides comprehensive documentation for all environment variables that control Probe's behavior, configuration, and execution.

## Overview

Probe uses environment variables for:

- **Runtime Configuration** - Control logging, timeouts, and behavior
- **Authentication** - API keys, tokens, and credentials
- **Integration** - CI/CD systems, monitoring tools
- **Customization** - Limits and default configurations

Environment variables can be set at the system level, in CI/CD pipelines, or defined within workflow files using the `env` section.

## Runtime Configuration Variables

These variables are read by Probe itself.

### `PROBE_OUTPUT`

**Type:** String  
**Values:** `auto`, `spinner`, `stream`  
**Default:** `auto`  
**Description:** Selects how the report is rendered. `auto` picks `spinner` on an interactive terminal and `stream` otherwise. The `--output` flag overrides this variable.

```bash
# Stream results as they complete, which suits CI logs
export PROBE_OUTPUT=stream
probe workflow.yml
```

### `PROBE_REPORT`

**Type:** String  
**Values:** a comma separated list of `format[=path]`, where `format` is `json`, `junit`, `markdown` or `github-summary`  
**Default:** none  
**Description:** Report files to write once the run has finished. A format without a path goes to its default file. The `--report` flag overrides this variable.

```bash
# Leave JUnit XML for the CI system to pick up
export PROBE_REPORT=junit=out/junit.xml
probe workflow.yml
```

### `PROBE_BASELINE`

**Type:** String  
**Values:** the path of a report an earlier run wrote with `--report json`  
**Default:** none  
**Description:** The report of an earlier run, which the report files compare the response times of the repeated steps to. The `--baseline` flag overrides this variable.

```bash
# Compare to the report kept from the main branch
export PROBE_BASELINE=main.json
probe --report github-summary workflow.yml
```

### `PROBE_MAX_REPEAT_COUNT`

**Type:** Integer  
**Default:** `10000`  
**Description:** Upper limit for a step's `repeat.count`. A workflow asking for more is rejected before it runs.

```bash
export PROBE_MAX_REPEAT_COUNT=50000
probe load-test.yml
```

### `PROBE_MAX_ATTEMPTS`

**Type:** Integer  
**Default:** `10000`  
**Description:** Upper limit for a step's retry `max_attempts`.

```bash
export PROBE_MAX_ATTEMPTS=100
probe workflow.yml
```

### `FORCE_COLOR`

**Type:** String  
**Values:** `1`  
**Description:** Forces colored output even when standard output is not a terminal.

```bash
FORCE_COLOR=1 probe workflow.yml | tee run.log
```

Everything else on this page is a variable you define yourself and read from a workflow through `vars`.

## Authentication Variables

Credentials do not belong in the workflow file. These variables hold them instead, and the workflow reads them through `env`.

### API Authentication

Common patterns for API authentication in workflows:

#### `API_TOKEN` / `API_KEY`

A bearer token is exported once and read from the workflow's authorization header.

```bash
# Bearer token authentication
export API_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

```yaml
# workflow.yml
vars:
  api_token: "{{API_TOKEN}}"

jobs:
- name: API checks
  defaults:
    http:
      headers:
        Authorization: "Bearer {{vars.api_token}}"
  steps:
    - name: Check
      uses: http
      with:
        get: /health
      test: res.code == 200
```

#### `USERNAME` / `PASSWORD`

Basic authentication needs both values, and neither should appear in the workflow file.

```bash
# Basic authentication credentials
export API_USERNAME="admin"
export API_PASSWORD="secret123"
```

```yaml
# workflow.yml
vars:
  username: "{{API_USERNAME}}"
  password: "{{API_PASSWORD}}"

steps:
  - name: "Authenticated Request"
    uses: http
    with:
      method: GET
      url: "https://api.example.com/protected"
      headers:
        Authorization: "Basic {{encode_base64(vars.username + ':' + vars.password)}}"
```

### Email Authentication

Mail steps need the server, the port and the account to authenticate with.

#### SMTP Configuration

A mail step needs the host, the port and the account it authenticates with.

```bash
# Gmail with app password
export SMTP_HOST="smtp.gmail.com"
export SMTP_PORT=587
export SMTP_USERNAME="alerts@example.com"
export SMTP_PASSWORD="app-specific-password"

# Office 365
export SMTP_HOST="smtp.office365.com"
export SMTP_PORT=587
export SMTP_USERNAME="alerts@company.com"
export SMTP_PASSWORD="account-password"
```

```yaml
# workflow.yml
vars:
  smtp_addr: "{{SMTP_ADDR ?? 'localhost:25'}}"
  mail_from: "{{MAIL_FROM}}"
  mail_to: "{{MAIL_TO}}"

jobs:
- name: Delivery check
  defaults:
    smtp:
      addr: "{{vars.smtp_addr}}"
      from: "{{vars.mail_from}}"
  steps:
    - name: Send a probe mail
      uses: smtp
      with:
        to: "{{vars.mail_to}}"
        subject: "Delivery probe"
        session: 1
        message: 1
        length: 500
      test: res.code == 0
```

## Application Configuration Variables

These variables carry the values that change between deployments, such as the addresses a workflow targets and the features it expects to find enabled.

### Service URLs

Addresses change per environment, so the workflow reads them instead of naming them.

```bash
# API endpoints
export API_BASE_URL="https://api.production.com"
export API_VERSION="v2"
export HEALTH_CHECK_URL="${API_BASE_URL}/${API_VERSION}/health"

# Database connections
export DATABASE_URL="postgresql://user:pass@localhost:5432/db"
export REDIS_URL="redis://localhost:6379/0"

# External services
export SLACK_WEBHOOK_URL="https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
export MONITORING_URL="https://monitoring.example.com/api/alerts"
```

### Feature Flags

A flag lets one workflow skip the checks that do not apply where it is running.

```bash
# Enable/disable features
export ENABLE_MONITORING=true
export ENABLE_SLACK_NOTIFICATIONS=false
export ENABLE_DETAILED_LOGGING=true
export ENABLE_PERFORMANCE_TRACKING=true
```

```yaml
# workflow.yml
vars:
  enable_monitoring: "{{ENABLE_MONITORING}}"
  enable_performance_tracking: "{{ENABLE_PERFORMANCE_TRACKING}}"
  enable_slack_notifications: "{{ENABLE_SLACK_NOTIFICATIONS}}"
  api_base_url: "{{API_BASE_URL}}"
  slack_webhook_url: "{{SLACK_WEBHOOK_URL}}"

jobs:
- id: monitoring
  name: monitoring
  steps:
    - name: "Performance Check"
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/metrics"

- id: notifications
  name: notifications
  needs: [monitoring]
  steps:
    - name: "Slack Alert"
      uses: http
      with:
        url: "{{vars.slack_webhook_url}}"
        method: "POST"
        body: |
          {
          }
```

## CI/CD Integration Variables

CI systems already expose the branch, commit and run identifiers as environment variables. A workflow can read them to label its results or to decide what to run.

### GitHub Actions

Secrets are passed into the step's environment, and the run is a single command.

```yaml
# .github/workflows/probe.yml
name: API Tests
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Run Probe Tests
        env:
          API_TOKEN: ${{ secrets.API_TOKEN }}
          ENVIRONMENT: ${{ github.ref == 'refs/heads/main' && 'production' || 'staging' }}
          BRANCH_NAME: ${{ github.ref_name }}
          COMMIT_SHA: ${{ github.sha }}
          RUN_ID: ${{ github.run_id }}
        run: probe workflow.yml
```

**Available in workflow:**

```bash
export GITHUB_REPOSITORY="owner/repo"
export GITHUB_REF="refs/heads/main"
export GITHUB_SHA="abc123def456"
export GITHUB_RUN_ID="123456789"
export GITHUB_ACTOR="username"
```

### GitLab CI

Project variables reach the job's environment, so the script itself stays one line.

```yaml
# .gitlab-ci.yml
probe-test:
  stage: test
  script:
    - probe workflow.yml
  variables:
    API_TOKEN: $API_TOKEN
    ENVIRONMENT: $CI_ENVIRONMENT_NAME
    BRANCH_NAME: $CI_COMMIT_REF_NAME
    COMMIT_SHA: $CI_COMMIT_SHA
    PIPELINE_ID: $CI_PIPELINE_ID
```

**Available in workflow:**

```bash
export CI_PROJECT_NAME="project-name"
export CI_COMMIT_REF_NAME="main"
export CI_COMMIT_SHA="abc123"
export CI_PIPELINE_ID="123456"
export CI_JOB_ID="789012"
```

### Jenkins

The `environment` block pulls each credential from the Jenkins credential store.

```groovy
// Jenkinsfile
pipeline {
    agent any
    environment {
        API_TOKEN = credentials('api-token')
        ENVIRONMENT = "${BRANCH_NAME == 'main' ? 'production' : 'staging'}"
        BUILD_NUMBER = "${BUILD_NUMBER}"
        JOB_NAME = "${JOB_NAME}"
    }
    stages {
        stage('Test') {
            steps {
                sh 'probe workflow.yml'
            }
        }
    }
}
```

**Available in workflow:**

```bash
export BUILD_NUMBER="123"
export JOB_NAME="api-tests"
export WORKSPACE="/var/jenkins_home/workspace/api-tests"
export JENKINS_URL="https://jenkins.example.com"
```

## Environment-Specific Configuration

One workflow usually has to run against development, staging and production. The setups below select the values per environment rather than duplicating the file.

### Multi-Environment Setup

The values shared by every environment are set once, and only the ones that differ are selected per environment.

```bash
# Base configuration (always set)
export API_VERSION="v1"
export DEFAULT_TIMEOUT="30s"

# Environment-specific (set per environment)
case $ENVIRONMENT in
  "production")
    export API_BASE_URL="https://api.prod.com"
    export DATABASE_URL="postgresql://prod-db:5432/app"
    export LOG_LEVEL="warn"
    export ENABLE_MONITORING=true
    ;;
  "staging")  
    export API_BASE_URL="https://api.staging.com"
    export DATABASE_URL="postgresql://staging-db:5432/app"
    export LOG_LEVEL="info"
    export ENABLE_MONITORING=true
    ;;
  "development")
    export API_BASE_URL="http://localhost:3000"
    export DATABASE_URL="postgresql://localhost:5432/app_dev"
    export LOG_LEVEL="debug"
    export ENABLE_MONITORING=false
    ;;
esac
```

### Docker Configuration

Since Probe is a single binary, the image only has to fetch it and pass the variables through at run time.

```dockerfile
# Dockerfile
FROM alpine:latest

# Install Probe
RUN apk add --no-cache curl && \
    curl -L https://github.com/mozership/probe/releases/latest/download/probe_linux_x86_64.tar.gz | tar -xz -C /usr/local/bin probe

# Set default environment variables
ENV PROBE_OUTPUT=stream

COPY workflows/ /workflows/
WORKDIR /workflows

ENTRYPOINT ["probe"]
CMD ["workflow.yml"]
```

```yaml
# docker-compose.yml
version: '3.8'

services:
  probe:
    build: .
    environment:
      - API_TOKEN=${API_TOKEN}
      - API_BASE_URL=https://api.example.com
      - ENVIRONMENT=production
      - PROBE_OUTPUT=stream
    volumes:
      - ./workflows:/workflows
      - ./reports:/reports
    command: monitoring.yml
```

## Security Considerations

Environment variables keep secrets out of the workflow file, but they are still readable by the process and anything that inspects it. The points below limit that exposure.

### Sensitive Variables

**Never commit sensitive values to version control:**

```bash
# Bad: hardcoded in the workflow file
vars:
  api_token: "secret-token-here"

# Good: read from the environment
vars:
  api_token: "{{API_TOKEN}}"
```

### Variable Management

A secret is better fetched from a secret store at the moment of use than stored in a file that persists.

```bash
# Use secure secret management
export API_TOKEN=$(aws ssm get-parameter --name "/app/api-token" --with-decryption --query 'Parameter.Value' --output text)
export DB_PASSWORD=$(vault kv get -field=password secret/database)

# Use temporary files for complex secrets
echo "$GOOGLE_CREDENTIALS_JSON" > /tmp/gcp-key.json
export GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp-key.json
```

### Environment Isolation

Prefixing variable names per environment keeps a production credential from being picked up by a staging run.

```bash
# Prefix variables by environment to avoid conflicts
export PROD_API_TOKEN="prod-token"
export STAGING_API_TOKEN="staging-token"  
export DEV_API_TOKEN="dev-token"

# Use environment-specific selection
export API_TOKEN="${ENVIRONMENT}_API_TOKEN"
export API_TOKEN="${!API_TOKEN}"  # Indirect variable expansion
```

## Debugging Environment Variables

When a template resolves to nothing, the variable is usually absent rather than wrong. These commands show what the process actually received.

### Viewing Available Variables

Before debugging the workflow, confirm which variables the shell is actually exporting.

```bash
# List all environment variables
env | grep -E '^(PROBE_|API_|SMTP_)' | sort

# Check specific variables
echo "API_TOKEN: $API_TOKEN"
echo "PROBE_OUTPUT: $PROBE_OUTPUT"

# Debug in workflow (be careful with sensitive data)
probe -v workflow.yml 2>&1 | grep -i "environment"
```

### Template Debugging

A workflow that only echoes its variables shows what the templates resolved to.

```yaml
# workflow.yml - Debug environment variable expansion
vars:
  api_base_url: "{{API_BASE_URL}}"
  environment: "{{ENVIRONMENT}}"
  default_timeout: "{{DEFAULT_TIMEOUT ?? 'not set'}}"

steps:
  - name: "Debug Environment"
    uses: hello
    with:
      message: |
        Environment Debug:
        API_URL: "{{vars.api_base_url}}"
        Environment: "{{vars.environment}}"
        Timeout: "{{vars.default_timeout}}"
        
  - name: "Test Variable Access"
    uses: hello
    echo: |
      Available variables:
      {{range $key, $value := vars}}
        {{$key}}: {{$value}}
      {{end}}
```

## Common Patterns

The patterns below combine defaults, environment values and conditions so that a single workflow adapts to where it runs.

### Configuration Cascading

Layering the configuration files lets a more specific one override a more general one without copying it.

```bash
# System defaults
export SYSTEM_CONFIG=/etc/probe/system.yml

# Team defaults  
export TEAM_CONFIG=/opt/team/defaults.yml

# Project-specific
export PROJECT_CONFIG=./probe-defaults.yml

# Runtime execution with cascading
probe ${SYSTEM_CONFIG},${TEAM_CONFIG},${PROJECT_CONFIG},workflow.yml
```

### Dynamic Configuration

The file to run can be chosen from the environment, with a fallback when no environment-specific file exists.

```bash
# Generate configuration based on environment
WORKFLOW_FILE="workflow-${ENVIRONMENT}.yml"
if [[ ! -f "$WORKFLOW_FILE" ]]; then
  WORKFLOW_FILE="workflow.yml"
fi

probe "$WORKFLOW_FILE"
```

### Conditional Execution

Deriving a flag in the shell lets the workflow's conditions stay simple comparisons.

```bash
# Skip certain jobs based on environment
export SKIP_PERFORMANCE_TESTS=$([[ "$ENVIRONMENT" == "development" ]] && echo "true" || echo "false")
export ENABLE_SLACK_ALERTS=$([[ "$ENVIRONMENT" == "production" ]] && echo "true" || echo "false")
```

```yaml
# workflow.yml
vars:
  skip_performance_tests: "{{SKIP_PERFORMANCE_TESTS}}"
  enable_slack_alerts: "{{ENABLE_SLACK_ALERTS}}"

jobs:
- id: performance-tests
  name: performance-tests
  # Performance test steps
    
- id: alerts
  name: alerts
  needs: [performance-tests]
  # Alert steps
```

## See Also

- **[CLI Reference](/reference/cli-reference)** - Command-line environment variable usage
- **[YAML Configuration](/reference/yaml-configuration)** - Using environment variables in workflows
- **[How-tos: Environment Management](/guide/how-tos/environment-management)** - Practical environment management strategies
- **[Concepts: File Merging](/guide/concepts/file-merging)** - Configuration composition patterns
