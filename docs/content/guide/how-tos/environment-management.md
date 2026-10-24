# Environment Management

This guide shows you how to manage Probe workflows across multiple environments (development, staging, production) using configuration composition, environment-specific settings, and deployment strategies.

## Basic Environment Configuration

The goal is one workflow file that runs against development, staging and production, with only the values changing.

### Single Workflow, Multiple Environments

Create a base workflow that works across environments:

**base-workflow.yml:**
```yaml
name: Multi-Environment API Test
description: API testing workflow that adapts to different environments

vars:
  api_base_url: "{{API_BASE_URL}}"
  environment: "{{ENVIRONMENT ?? 'Unknown'}}"
  default_timeout: "{{DEFAULT_TIMEOUT ?? '30s'}}"

jobs:
- id: api-health-check
  name: API Health Check
  defaults:
    http:
      headers:
        User-Agent: "Probe Test Agent"
        Accept: "application/json"
  steps:
    - name: Health Endpoint Test
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health"
      test: res.code == 200
      outputs:
        api_healthy: res.code == 200
        response_time: (rt.sec * 1000)
        api_version: res.body.version

    - name: Database Health Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health/database"
      test: res.code == 200
      outputs:
        database_healthy: res.code == 200
        db_response_time: (rt.sec * 1000)

- id: environment-report
  name: Environment Report
  needs: [api-health-check]
  defaults:
    http:
      headers:
        User-Agent: "Probe Test Agent"
        Accept: "application/json"
  steps:
    - name: Environment Summary
      uses: hello
      echo: |
        🌍 Environment Test Report
        =========================

        Environment: {{vars.environment}}
        API Base URL: {{vars.api_base_url}}

        Health Check Results:
        API Health: {{outputs.api_healthy ? "✅ Healthy" : "❌ Down"}} ({{outputs.response_time}}ms)
        Database: {{outputs.database_healthy ? "✅ Healthy" : "❌ Down"}} ({{outputs.db_response_time}}ms)
        API Version: {{outputs.api_version}}

        Environment-Specific Notes:
        {{vars.environment == "development" ? "• Development environment - extended timeouts enabled" : ""}}
        {{vars.environment == "staging" ? "• Staging environment - production-like testing" : ""}}
        {{vars.environment == "production" ? "• Production environment - strict validation" : ""}}

- id: dev-specific-checks
  name: Development Environment Checks
  needs: [api-health-check]
  skipif: vars.environment != "development"
  steps:
    - name: Hot Reload Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/dev/hot-reload-status"
      test: res.code == 200
      outputs:
        hot_reload_enabled: res.body.enabled

    - name: Debug Endpoints Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/debug/info"
      test: res.code == 200
      outputs:
        debug_info_available: res.code == 200

    - name: Development Summary
      uses: hello
      echo: |
        🛠️ Development Environment Status:
        Hot Reload: {{outputs.hot_reload_enabled ? "✅ Enabled" : "❌ Disabled"}}
        Debug Info: {{outputs.debug_info_available ? "✅ Available" : "❌ Not Available"}}

- id: staging-specific-checks
  name: Staging Environment Checks
  needs: [api-health-check]
  skipif: vars.environment != "staging"
  defaults:
    http:
      headers:
        X-Environment: staging
  steps:
    - name: Load Balancer Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health/load-balancer"
      test: res.code == 200
      outputs:
        load_balancer_healthy: res.code == 200
        backend_count: res.body.active_backends

    - name: Cache Layer Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health/cache"
      test: res.code == 200
      outputs:
        cache_healthy: res.code == 200
        cache_hit_rate: res.body.hit_rate

    - name: Integration Tests
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/test/integration"
      test: res.code == 200 && res.body.all_tests_passed == true
      outputs:
        integration_tests_passed: res.body.all_tests_passed

    - name: Staging Summary
      uses: hello
      echo: |
        🧪 Staging Environment Status:
        Load Balancer: {{outputs.load_balancer_healthy ? "✅ Healthy" : "❌ Issues"}} ({{outputs.backend_count}} backends)
        Cache Layer: {{outputs.cache_healthy ? "✅ Healthy" : "❌ Issues"}} ({{(outputs.cache_hit_rate * 100)}}% hit rate)
        Integration Tests: {{outputs.integration_tests_passed ? "✅ Passed" : "❌ Failed"}}

- id: production-specific-checks
  name: Production Environment Checks
  needs: [api-health-check]
  skipif: vars.environment != "production"
  defaults:
    http:
      headers:
        X-Environment: production
  steps:
    - name: SSL Certificate Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health/ssl"
      test: |
        res.code == 200 &&
        res.body.certificate_valid == true &&
        res.body.days_until_expiry > 30
      outputs:
        ssl_valid: res.body.certificate_valid
        ssl_days_remaining: res.body.days_until_expiry

    - name: Performance SLA Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health/performance"
      test: |
        res.code == 200 &&
        res.body.avg_response_time < 500 &&
        res.body.success_rate > 0.999
      outputs:
        sla_met: res.body.avg_response_time < 500 && res.body.success_rate > 0.999
        avg_response_time: res.body.avg_response_time
        success_rate: res.body.success_rate

    - name: Security Compliance Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health/security"
      test: |
        res.code == 200 &&
        res.body.security_score >= 0.95
      outputs:
        security_compliant: res.body.security_score >= 0.95
        security_score: res.body.security_score

    - name: Production Summary
      uses: hello
      echo: |
        🏭 Production Environment Status:
        SSL Certificate: {{outputs.ssl_valid ? "✅ Valid" : "❌ Invalid"}} ({{outputs.ssl_days_remaining}} days remaining)
        Performance SLA: {{outputs.sla_met ? "✅ Met" : "❌ Violated"}}
          - Avg Response: {{outputs.avg_response_time}}ms
          - Success Rate: {{(outputs.success_rate * 100)}}%
        Security Compliance: {{outputs.security_compliant ? "✅ Compliant" : "❌ Non-Compliant"}} ({{(outputs.security_score * 100)}}%)
```

**development.yml:**
```yaml
vars:
  environment: "{{ENVIRONMENT ?? 'development'}}"
  api_base_url: "{{API_BASE_URL ?? 'http://localhost:3000'}}"
  default_timeout: "{{DEFAULT_TIMEOUT ?? '60s'}}"
```

**staging.yml:**
```yaml
vars:
  environment: "{{ENVIRONMENT ?? 'staging'}}"
  api_base_url: "{{API_BASE_URL ?? 'https://api.staging.yourcompany.com'}}"
  default_timeout: "{{DEFAULT_TIMEOUT ?? '30s'}}"
```

**production.yml:**
```yaml
vars:
  environment: "{{ENVIRONMENT ?? 'production'}}"
  api_base_url: "{{API_BASE_URL ?? 'https://api.yourcompany.com'}}"
  default_timeout: "{{DEFAULT_TIMEOUT ?? '10s'}}"
```

**Usage:**
```bash
# Development environment
probe base-workflow.yml,development.yml

# Staging environment  
probe base-workflow.yml,staging.yml

# Production environment
probe base-workflow.yml,production.yml
```

The environment files hold only `vars`. A top-level key in a later file replaces the one in an earlier file, so `jobs` in an environment file would replace every job of the base workflow, and its `needs` would name jobs that are no longer there; Probe warns of a file whose `jobs` replace those of an earlier one. The jobs for one environment are kept in the base workflow instead, and `skipif` skips them in the others.

When the environments differ only in a few values, the workflow can stay one file, and the values be given as it is run. `--env-file` reads the variables the workflow reads into its `vars` from a file, where the environment has none, and `--var` gives a var in place of what the workflow writes for it:

```bash
# The variables of an environment, kept in a file of NAME=value lines
probe --env-file staging.env workflow.yml

# One value changed for one run
probe --env-file staging.env --var api_url=http://localhost:8080 workflow.yml
```

See [`--var`](/reference/cli-reference#--var) and [`--env-file`](/reference/cli-reference#--env-file).

## Advanced Environment Management

Environments differ in more than their addresses: which features are enabled, where the credentials come from, and whether the environment is configured correctly at all.

### Environment-Specific Feature Flags

Control which features are tested in different environments. The file is a workflow of its own, with its own `jobs`, so it is run apart from the base workflow rather than merged into it:

**feature-flags.yml:**
```yaml
name: Feature Flag Tests

# Feature flags configuration
vars:
  api_base_url: "{{API_BASE_URL}}"
  admin_token: "{{ADMIN_TOKEN}}"
  environment: "{{ENVIRONMENT}}"
  # Core features (always enabled)
  feature_user_management: true
  feature_basic_api: true
  # Environment-specific features (a template gives a string, "true" or "false")
  feature_beta_api: "{{vars.environment != 'production'}}"
  feature_admin_tools: "{{vars.environment == 'development'}}"
  feature_performance_testing: "{{vars.environment != 'development'}}"
  feature_security_scanning: "{{vars.environment == 'production'}}"

jobs:
- id: core-feature-tests
  name: Core Feature Tests
  steps:
    - name: User Management Test
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/users"
      test: res.code == 200
      outputs:
        user_management_working: res.code == 200

    - name: Basic API Test
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/api/basic"
      test: res.code == 200
      outputs:
        basic_api_working: res.code == 200

- id: beta-feature-tests
  name: Beta Feature Tests
  skipif: vars.feature_beta_api != "true"
  steps:
    - name: Beta API Test
      id: beta-feature-tests
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/api/beta"
      test: res.code == 200
      outputs:
        beta_api_working: res.code == 200

- id: admin-feature-tests
  name: Admin Feature Tests
  skipif: vars.feature_admin_tools != "true"
  steps:
    - name: Admin Tools Test
      id: admin-feature-tests
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/admin/tools"
        headers:
          Authorization: "Bearer {{vars.admin_token}}"
      test: res.code == 200
      outputs:
        admin_tools_working: res.code == 200

- id: performance-tests
  name: Performance Tests
  skipif: vars.feature_performance_testing != "true"
  steps:
    - name: Load Test
      id: performance-tests
      uses: http
      with:
        url: "{{vars.api_base_url}}/test/load"
        method: POST
        body: |
          {
            "concurrent_users": {{vars.environment == "staging" ? 10 : 50}},
            "duration_seconds": {{vars.environment == "staging" ? 60 : 300}}
          }
      test: res.code == 200
      outputs:
        load_test_passed: res.body.success

- id: security-tests
  name: Security Tests
  skipif: vars.feature_security_scanning != "true"
  steps:
    - name: Security Scan
      id: security-tests
      uses: http
      with:
        url: "{{vars.api_base_url}}/security/scan"
        method: POST
      test: res.code == 200 && res.body.vulnerabilities_found == 0
      outputs:
        security_scan_clean: res.body.vulnerabilities_found == 0

- id: feature-summary
  name: Feature Test Summary
  needs: [core-feature-tests, beta-feature-tests, admin-feature-tests, performance-tests, security-tests]
  steps:
    - name: Environment Feature Report
      uses: hello
      echo: |
        🚀 Feature Test Summary for {{vars.environment}}:
        ================================================

        CORE FEATURES:
        {{vars.feature_user_management == true ? "User Management: " + (outputs.user_management_working ? "✅ Working" : "❌ Failed") : "User Management: ⏸️ Disabled"}}
        {{vars.feature_basic_api == true ? "Basic API: " + (outputs.basic_api_working ? "✅ Working" : "❌ Failed") : "Basic API: ⏸️ Disabled"}}

        BETA FEATURES:
        {{vars.feature_beta_api == "true" ? "Beta API: " + (outputs['beta-feature-tests'].beta_api_working ? "✅ Working" : "❌ Failed") : "Beta API: ⏸️ Disabled"}}

        ADMIN FEATURES:
        {{vars.feature_admin_tools == "true" ? "Admin Tools: " + (outputs['admin-feature-tests'].admin_tools_working ? "✅ Working" : "❌ Failed") : "Admin Tools: ⏸️ Disabled"}}

        PERFORMANCE TESTING:
        {{vars.feature_performance_testing == "true" ? "Load Testing: " + (outputs['performance-tests'].load_test_passed ? "✅ Passed" : "❌ Failed") : "Performance Testing: ⏸️ Disabled"}}

        SECURITY TESTING:
        {{vars.feature_security_scanning == "true" ? "Security Scan: " + (outputs['security-tests'].security_scan_clean ? "✅ Clean" : "❌ Vulnerabilities Found") : "Security Testing: ⏸️ Disabled"}}

        Environment Configuration:
        Features enabled: {{
          (vars.feature_user_management == true ? 1 : 0) +
          (vars.feature_basic_api == true ? 1 : 0) +
          (vars.feature_beta_api == "true" ? 1 : 0) +
          (vars.feature_admin_tools == "true" ? 1 : 0) +
          (vars.feature_performance_testing == "true" ? 1 : 0) +
          (vars.feature_security_scanning == "true" ? 1 : 0)
        }} / 6
```

### Credential and Secret Management

Credentials are not written in the YAML, and are not kept in a YAML file of their own either: given as one more file on the command line, its `vars` would replace those of the environment file. A workflow reads them from environment variables in its `vars`, and lists them under `secrets`, so that their values are hidden in the output and the reports:

```yaml
secrets:
  - API_TOKEN
vars:
  api_token: "{{API_TOKEN}}"
```

On a development machine, the values can be kept in a file of `NAME=value` lines that is not committed, and given with [`--env-file`](/reference/cli-reference#--env-file):

**development.env:**
```text
ENVIRONMENT=development
API_BASE_URL=http://localhost:3000
API_TOKEN=dev_token_12345
ADMIN_TOKEN=dev_admin_token
```

```bash
probe --env-file development.env environment-validation.yml
```

In CI, the environment of the job gives them from the secrets of the CI, as in the [GitHub Actions example](#github-actions-integration) below.

### Environment Validation Workflows

Validate environment configuration before running tests:

**environment-validation.yml:**
```yaml
name: Environment Validation
description: Validate environment configuration and prerequisites

vars:
  environment: "{{ENVIRONMENT ?? ''}}"
  api_base_url: "{{API_BASE_URL ?? ''}}"
  api_token: "{{API_TOKEN ?? ''}}"
  log_level: "{{LOG_LEVEL ?? ''}}"
  default_timeout: "{{DEFAULT_TIMEOUT ?? ''}}"

jobs:
- id: environment-validation
  name: Environment Configuration Validation
  steps:
    - name: Required Environment Variables Check
      uses: hello
      echo: |
        🔍 Environment Variables Validation:

        Required Variables:
        ENVIRONMENT: {{vars.environment != "" ? "✅ Set (" + vars.environment + ")" : "❌ Missing"}}
        API_BASE_URL: {{vars.api_base_url != "" ? "✅ Set (" + vars.api_base_url + ")" : "❌ Missing"}}
        API_TOKEN: {{vars.api_token != "" ? "✅ Set (***)" : "❌ Missing"}}

        Optional Variables:
        LOG_LEVEL: {{vars.log_level != "" ? "✅ Set (" + vars.log_level + ")" : "⚠️ Using default"}}
        DEFAULT_TIMEOUT: {{vars.default_timeout != "" ? "✅ Set (" + vars.default_timeout + ")" : "⚠️ Using default"}}

        Validation Status: {{
          vars.environment != "" && vars.api_base_url != "" && vars.api_token != "" ? "✅ Valid" : "❌ Invalid"
        }}

    - name: Environment-Specific Validation
      uses: hello
      echo: |
        📋 Environment-Specific Validation:

        {{vars.environment == "development" ? "Development Environment:" : ""}}
        {{vars.environment == "development" ? "• Extended timeouts enabled" : ""}}
        {{vars.environment == "development" ? "• Debug features available" : ""}}
        {{vars.environment == "development" ? "• Security checks relaxed" : ""}}

        {{vars.environment == "staging" ? "Staging Environment:" : ""}}
        {{vars.environment == "staging" ? "• Production-like configuration" : ""}}
        {{vars.environment == "staging" ? "• Integration testing enabled" : ""}}
        {{vars.environment == "staging" ? "• Performance testing included" : ""}}

        {{vars.environment == "production" ? "Production Environment:" : ""}}
        {{vars.environment == "production" ? "• Strict timeouts enforced" : ""}}
        {{vars.environment == "production" ? "• Security scanning enabled" : ""}}
        {{vars.environment == "production" ? "• Full monitoring active" : ""}}

    - name: Service Connectivity Pre-Check
      uses: http
      timeout: 10s
      with:
        method: GET
        url: "{{vars.api_base_url}}/health"
      test: res.code == 200
      outputs:
        connectivity_ok: res.code == 200
        api_version: res.body.version
        environment_confirmed: res.body.environment

    - name: Authentication Pre-Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/auth/validate"
        headers:
          Authorization: "Bearer {{vars.api_token}}"
      test: res.code == 200
      outputs:
        auth_valid: res.code == 200
        token_expires_in: res.body.expires_in

    - name: Validation Summary
      uses: hello
      echo: |
        ✅ Environment Validation Results:

        Connectivity: {{outputs.connectivity_ok ? "✅ Connected" : "❌ Failed"}}
        API Version: {{outputs.api_version}}
        Environment Match: {{outputs.environment_confirmed == vars.environment ? "✅ Confirmed" : "⚠️ Mismatch"}}
        Authentication: {{outputs.auth_valid ? "✅ Valid" : "❌ Invalid"}}
        {{outputs.auth_valid ? "Token Expires In: " + string(outputs.token_expires_in) + " seconds" : ""}}

        Environment Ready: {{
          outputs.connectivity_ok && 
          outputs.auth_valid && 
          outputs.environment_confirmed == vars.environment
          ? "🟢 YES" : "🔴 NO"
        }}
```

## CI/CD Integration

CI already knows which environment a run targets. Passing that through lets the same workflow select its values and its test suite.

### GitHub Actions Integration

Integrate with CI/CD pipelines for automated environment testing:

**.github/workflows/probe-tests.yml:**
```yaml
name: Probe Environment Tests

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  development-tests:
    name: Development Environment Tests
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Install Probe
        run: |
          curl -L https://github.com/mozership/probe/releases/latest/download/probe_linux_x86_64.tar.gz | tar -xz probe
          chmod +x probe
          sudo mv probe /usr/local/bin/

      - name: Run Development Tests
        run: |
          probe workflows/base-workflow.yml,environments/development.yml

  staging-tests:
    name: Staging Environment Tests
    runs-on: ubuntu-latest
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    steps:
      - uses: actions/checkout@v3

      - name: Install Probe
        run: |
          curl -L https://github.com/mozership/probe/releases/latest/download/probe_linux_x86_64.tar.gz | tar -xz probe
          chmod +x probe
          sudo mv probe /usr/local/bin/

      - name: Validate Staging Environment
        env:
          ENVIRONMENT: staging
          API_BASE_URL: https://api.staging.yourcompany.com
          API_TOKEN: ${{ secrets.STAGING_API_TOKEN }}
        run: |
          probe workflows/environment-validation.yml

      - name: Run Staging Tests
        run: |
          probe workflows/base-workflow.yml,environments/staging.yml

      - name: Run Feature Tests
        env:
          API_BASE_URL: https://api.staging.yourcompany.com
          ADMIN_TOKEN: ${{ secrets.STAGING_ADMIN_TOKEN }}
        run: |
          probe --var environment=staging features/feature-flags.yml

  production-smoke-tests:
    name: Production Smoke Tests
    runs-on: ubuntu-latest
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    steps:
      - uses: actions/checkout@v3

      - name: Install Probe
        run: |
          curl -L https://github.com/mozership/probe/releases/latest/download/probe_linux_x86_64.tar.gz | tar -xz probe
          chmod +x probe
          sudo mv probe /usr/local/bin/

      - name: Production Smoke Tests
        env:
          API_BASE_URL: https://api.yourcompany.com
        run: |
          probe workflows/smoke-test.yml
```

### Environment-Specific Test Suites

Create different test suites for different environments:

**smoke-test.yml (for production):**
```yaml
name: Production Smoke Test
description: Minimal smoke test for production environment

vars:
  api_base_url: "{{API_BASE_URL}}"

jobs:
- id: critical-endpoints
  name: Critical Endpoints Check
  steps:
    - name: Health Check
      id: api
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health"
      test: res.code == 200
      outputs:
        api_healthy: res.code == 200

    - name: Authentication Check
      id: auth
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/auth/health"
      test: res.code == 200
      outputs:
        auth_healthy: res.code == 200

    - name: Database Check
      id: database
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health/database"
      test: res.code == 200
      outputs:
        db_healthy: res.code == 200

- id: smoke-test-summary
  name: Smoke Test Summary
  needs: [critical-endpoints]
  steps:
    - name: Production Health Summary
      uses: hello
      echo: |
        🏭 Production Smoke Test Results:

        Critical Systems:
        API: {{outputs.api.api_healthy ? "✅ Healthy" : "🚨 DOWN"}}
        Authentication: {{outputs.auth.auth_healthy ? "✅ Healthy" : "🚨 DOWN"}}
        Database: {{outputs.database.db_healthy ? "✅ Healthy" : "🚨 DOWN"}}

        Overall Status: {{
          outputs.api.api_healthy &&
          outputs.auth.auth_healthy &&
          outputs.database.db_healthy
          ? "🟢 ALL SYSTEMS OPERATIONAL" : "🔴 CRITICAL ISSUES DETECTED"
        }}
```

**comprehensive-test.yml (for staging):**
```yaml
name: Comprehensive Staging Test
description: Full test suite for staging environment validation

vars:
  api_base_url: "{{API_BASE_URL}}"

jobs:
- id: api-tests
  name: API Test Suite
  steps:
    - name: User Management API
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/users"
      test: res.code == 200

    - name: Order Management API
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/orders"
      test: res.code == 200

    - name: Product Catalog API
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/products"
      test: res.code == 200

- id: integration-tests
  name: Integration Tests
  needs: [api-tests]
  steps:
    - name: User-Order Integration
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/test/user-order-flow"
      test: res.code == 200 && res.body.test_passed == true

    - name: Payment Integration
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/test/payment-flow"
      test: res.code == 200 && res.body.test_passed == true

- id: performance-tests
  name: Performance Validation
  needs: [integration-tests]
  steps:
    - name: Load Test
      uses: http
      with:
        url: "{{vars.api_base_url}}/test/load"
        method: POST
        body: |
          {
            "concurrent_users": 10,
            "duration_seconds": 60
          }
      test: res.code == 200 && res.body.success_rate > 0.95
```

## Environment Monitoring and Alerting

The same environment configuration that drives the tests can drive the monitoring, so each environment is checked on its own terms.

### Environment Health Monitoring

Monitor the health of each environment continuously:

**environment-monitor.yml:**
```yaml
name: Environment Health Monitor
description: Continuous monitoring of environment health

vars:
  api_base_url: "{{API_BASE_URL}}"
  environment: "{{ENVIRONMENT}}"
  smtp_host: "{{SMTP_HOST}}"
  smtp_username: "{{SMTP_USERNAME}}"
  smtp_password: "{{SMTP_PASSWORD}}"
  monitoring_interval: "{{MONITORING_INTERVAL ?? '300'}}"  # 5 minutes
  alert_threshold: "{{ALERT_THRESHOLD ?? '2'}}"        # Alert after 2 consecutive failures

jobs:
- id: environment-health-check
  name: Environment Health Check
  steps:
    - name: System Resources Check
      id: resources
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health/resources"
      test: |
        res.code == 200 &&
        res.body.cpu_usage < 80 &&
        res.body.memory_usage < 80 &&
        res.body.disk_usage < 90
      outputs:
        resources_healthy: |
          res.code == 200 &&
          res.body.cpu_usage < 80 &&
          res.body.memory_usage < 80 &&
          res.body.disk_usage < 90
        cpu_usage: res.body.cpu_usage
        memory_usage: res.body.memory_usage
        disk_usage: res.body.disk_usage

    - name: Service Dependencies Check
      id: dependencies
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health/dependencies"
      test: |
        res.code == 200 &&
        res.body.all_dependencies_healthy == true
      outputs:
        dependencies_healthy: res.body.all_dependencies_healthy
        unhealthy_services: res.body.unhealthy_services

    - name: Environment-Specific Checks
      id: env-checks
      uses: hello
      echo: |
        Environment-specific validation for {{vars.environment}}
      outputs:
        env_specific_checks: |
          vars.environment == "production" ? "SSL, Security, Performance" :
          vars.environment == "staging" ? "Integration, Load Testing" :
          "Development Tools, Debug Features"

- id: alerting
  name: Environment Alerting
  needs: [environment-health-check]
  always: true
  skipif: outputs.resources.resources_healthy == true && outputs.dependencies.dependencies_healthy == true
  steps:
    - name: Environment Alert
      uses: smtp
      with:
        addr: "{{vars.smtp_host}}:587"
        from: "environment-alerts@yourcompany.com"
        to: "devops@yourcompany.com"
        subject: "🚨 Environment Health Alert - {{vars.environment}}"
        session: 1
        message: 1
        length: 500
      test: res.code == 0
      echo: |
        ENVIRONMENT HEALTH ALERT
        ========================

        Environment: {{vars.environment}}
        Time: {{unixtime()}}

        RESOURCE STATUS:
        {{outputs.resources.resources_healthy == true ? "✅ Resources Healthy" : "❌ Resource Issues"}}
        CPU Usage: {{outputs.resources.cpu_usage ?? "unknown"}}%
        Memory Usage: {{outputs.resources.memory_usage ?? "unknown"}}%
        Disk Usage: {{outputs.resources.disk_usage ?? "unknown"}}%

        DEPENDENCIES STATUS:
        {{outputs.dependencies.dependencies_healthy == true ? "✅ All Dependencies Healthy" : "❌ Dependency Issues"}}
        Unhealthy Services: {{join(outputs.dependencies.unhealthy_services ?? [], ", ")}}

        ACTION REQUIRED: Investigate {{vars.environment}} environment immediately

- id: health-summary
  name: Health Summary
  needs: [environment-health-check]
  steps:
    - name: All Systems Healthy
      uses: hello
      echo: |
        ✅ {{vars.environment}} Environment Health Check

        All systems operational:
        • Resources: CPU {{outputs.resources.cpu_usage}}%, Memory {{outputs.resources.memory_usage}}%, Disk {{outputs.resources.disk_usage}}%
        • Dependencies: All healthy
        • Environment: {{vars.environment}}

        Next check in {{vars.monitoring_interval}} seconds
```

## Best Practices

The points below cover keeping environments apart, moving changes through them in order, validating the configuration, and handling secrets.

### 1. Environment Isolation

One file per environment keeps the values apart and makes the differences visible in a diff.

```text
# Good: Clear environment separation
environments/
├── development.yml
├── staging.yml
└── production.yml

# Good: Credentials in the environment, not in YAML
development.env (not committed)
CI secrets → the environment of the job
```

### 2. Progressive Testing

Each environment carries the checks that make sense there, and the heavier ones run before production.

```yaml
# Good: Test pipeline progression
Development → Unit Tests
Staging → Integration Tests + Performance Tests  
Production → Smoke Tests + Monitoring
```

### 3. Configuration Validation

A run against a half-configured environment fails in ways that look like application failures, so the configuration is checked first.

```yaml
# Good: Validate before running tests
vars:
  environment: "{{ENVIRONMENT ?? ''}}"
  api_base_url: "{{API_BASE_URL ?? ''}}"
  api_token: "{{API_TOKEN ?? ''}}"
```

```yaml
- name: Pre-Test Validation
  uses: hello
  echo: |
    Environment: {{vars.environment != "" ? "✅" : "❌"}}
    API URL: {{vars.api_base_url != "" ? "✅" : "❌"}}
    Credentials: {{vars.api_token != "" ? "✅" : "❌"}}
```

### 4. Secure Secret Management

Secrets are read from the environment, so the environment file can be committed while the values are not.

```yaml
# Good: Use environment variables for secrets
vars:
  api_token: "{{PROD_API_TOKEN}}"  # From secure env var

# Avoid: Hardcoded secrets
vars:
  api_token: "hardcoded_secret_123"  # Never do this
```

List the variables in `secrets` as well, so that their values are replaced by `<secret:NAME>` in the terminal output, the action logs and the report files, where a CI log or an uploaded artifact would otherwise keep them:

```yaml
secrets:
  - PROD_API_TOKEN
vars:
  api_token: "{{PROD_API_TOKEN}}"
```

## What's Next?

Now that you can manage environments effectively, explore:

- **[Monitoring Workflows](/guide/how-tos/monitoring-workflows)** - Build comprehensive monitoring for all environments
- **[API Testing](/guide/how-tos/api-testing)** - Create environment-specific API test suites
- **[Error Handling Strategies](/guide/how-tos/error-handling-strategies)** - Handle environment-specific failures

Environment management is crucial for reliable testing across the software development lifecycle. Use these patterns to create consistent, secure, and maintainable environment configurations.
