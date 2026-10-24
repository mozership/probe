# 環境管理

このガイドでは、設定の構成、環境固有の設定、およびデプロイメント戦略を使用して、複数の環境（開発、ステージング、プロダクション）間でProbeワークフローを管理する方法について説明します。

## 基本的な環境設定

目指すのは、1つのワークフローファイルを開発、ステージング、本番のいずれに対しても動かし、変わるのは値だけという状態です。

### 単一ワークフロー、複数環境

複数の環境で動作するベースワークフローを作成します：

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

**使用方法:**
```bash
# 開発環境
probe base-workflow.yml,development.yml

# ステージング環境  
probe base-workflow.yml,staging.yml

# プロダクション環境
probe base-workflow.yml,production.yml
```

環境ごとのファイルには`vars`だけを書きます。後のファイルのトップレベルのキーは前のファイルのキーを置き換えます。そのため、環境ごとのファイルに`jobs`を書くとベースワークフローのジョブがすべて置き換わり、`needs`に書いたジョブもなくなります。前のファイルの`jobs`を置き換えるファイルがあると、Probeは警告を出します。1つの環境のためのジョブはベースワークフローに置き、`skipif`でほかの環境では飛ばします。

環境ごとの違いがいくつかの値だけなら、ワークフローは1ファイルのままにして、値を実行時に与えられます。`--env-file`は`vars`で参照する環境変数を、環境にない場合にファイルから読み、`--var`はワークフローに書かれた値の代わりにvarを与えます：

```bash
# 環境ごとの変数を NAME=value の行で書いたファイルに置く
probe --env-file staging.env workflow.yml

# 1回の実行だけ、1つの値を変える
probe --env-file staging.env --var api_url=http://localhost:8080 workflow.yml
```

詳しくは[`--var`](/ja/reference/cli-reference#--var)と[`--env-file`](/ja/reference/cli-reference#--env-file)を参照してください。

## 高度な環境管理

環境ごとに異なるのは接続先だけではありません。有効になっている機能、認証情報の取得元、そして環境自体が正しく設定されているかも異なります。

### 環境固有の機能フラグ

異なる環境でテストされる機能を制御します。このファイルは独自の`jobs`を持つ1つのワークフローなので、ベースワークフローにはマージせず、別に実行します：

**feature-flags.yml:**
```yaml
name: Feature Flag Tests

# 機能フラグ設定
vars:
  api_base_url: "{{API_BASE_URL}}"
  admin_token: "{{ADMIN_TOKEN}}"
  environment: "{{ENVIRONMENT}}"
  # コア機能（常に有効）
  feature_user_management: true
  feature_basic_api: true
  # 環境固有の機能
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
        {{vars.feature_beta_api == true ? "Beta API: " + (outputs['beta-feature-tests'].beta_api_working ? "✅ Working" : "❌ Failed") : "Beta API: ⏸️ Disabled"}}

        ADMIN FEATURES:
        {{vars.feature_admin_tools == true ? "Admin Tools: " + (outputs['admin-feature-tests'].admin_tools_working ? "✅ Working" : "❌ Failed") : "Admin Tools: ⏸️ Disabled"}}

        PERFORMANCE TESTING:
        {{vars.feature_performance_testing == true ? "Load Testing: " + (outputs['performance-tests'].load_test_passed ? "✅ Passed" : "❌ Failed") : "Performance Testing: ⏸️ Disabled"}}

        SECURITY TESTING:
        {{vars.feature_security_scanning == true ? "Security Scan: " + (outputs['security-tests'].security_scan_clean ? "✅ Clean" : "❌ Vulnerabilities Found") : "Security Testing: ⏸️ Disabled"}}

        Environment Configuration:
        Features enabled: {{
          (vars.feature_user_management == true ? 1 : 0) +
          (vars.feature_basic_api == true ? 1 : 0) +
          (vars.feature_beta_api == true ? 1 : 0) +
          (vars.feature_admin_tools == true ? 1 : 0) +
          (vars.feature_performance_testing == true ? 1 : 0) +
          (vars.feature_security_scanning == true ? 1 : 0)
        }} / 6
```

### 認証情報とシークレット管理

環境固有の認証情報を安全に管理します：

**credentials-development.yml:**
```yaml
vars:
  # 開発認証情報（機密性が低い）
  API_TOKEN: dev_token_12345
  DB_PASSWORD: dev_password
  ADMIN_TOKEN: dev_admin_token

  # 開発サービスURL
  API_BASE_URL: http://localhost:3000
  DB_URL: localhost:5432
  CACHE_URL: localhost:6379

  # 開発固有設定
  LOG_LEVEL: debug
  RATE_LIMIT_DISABLED: true
  SECURITY_CHECKS_RELAXED: true
```

**credentials-staging.yml:**
```yaml
vars:
  # ステージング認証情報（環境変数から）
  api_token: "{{STAGING_API_TOKEN}}"
  db_password: "{{STAGING_DB_PASSWORD}}"
  admin_token: "{{STAGING_ADMIN_TOKEN}}"

  # ステージングサービスURL
  API_BASE_URL: https://api.staging.yourcompany.com
  DB_URL: staging-db.yourcompany.com:5432
  CACHE_URL: staging-cache.yourcompany.com:6379

  # ステージング固有設定
  LOG_LEVEL: info
  RATE_LIMIT_DISABLED: false
  SECURITY_CHECKS_RELAXED: false
```

**credentials-production.yml:**
```yaml
vars:
  # プロダクション認証情報（セキュアな環境変数から）
  api_token: "{{PROD_API_TOKEN}}"
  db_password: "{{PROD_DB_PASSWORD}}"
  admin_token: "{{PROD_ADMIN_TOKEN}}"

  # プロダクションサービスURL
  API_BASE_URL: https://api.yourcompany.com
  DB_URL: prod-db.yourcompany.com:5432
  CACHE_URL: prod-cache.yourcompany.com:6379

  # プロダクション固有設定
  LOG_LEVEL: warn
  RATE_LIMIT_DISABLED: false
  SECURITY_CHECKS_RELAXED: false

  # プロダクション専用設定
  MONITORING_ENABLED: true
  ALERTS_ENABLED: true
  AUDIT_LOGGING: true
```

### 環境検証ワークフロー

テスト実行前に環境設定を検証します：

**environment-validation.yml:**
```yaml
name: Environment Validation
description: Validate environment configuration and prerequisites

vars:
  environment: "{{ENVIRONMENT}}"
  api_base_url: "{{API_BASE_URL}}"
  api_token: "{{API_TOKEN}}"
  log_level: "{{LOG_LEVEL}}"
  default_timeout: "{{DEFAULT_TIMEOUT}}"

jobs:
- name: Environment Configuration Validation
  steps:
    - name: Required Environment Variables Check
      uses: hello
      echo: |
        🔍 Environment Variables Validation:

        Required Variables:
        ENVIRONMENT: {{vars.environment ? "✅ Set (" + vars.environment + ")" : "❌ Missing"}}
        API_BASE_URL: {{vars.api_base_url ? "✅ Set (" + vars.api_base_url + ")" : "❌ Missing"}}
        API_TOKEN: {{vars.api_token ? "✅ Set (***)" : "❌ Missing"}}

        Optional Variables:
        LOG_LEVEL: {{vars.log_level ? "✅ Set (" + vars.log_level + ")" : "⚠️ Using default"}}
        DEFAULT_TIMEOUT: {{vars.default_timeout ? "✅ Set (" + vars.default_timeout + ")" : "⚠️ Using default"}}

        Validation Status: {{
          vars.environment && vars.api_base_url && vars.api_token ? "✅ Valid" : "❌ Invalid"
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
        {{outputs.auth_valid ? "Token Expires In: " + outputs.token_expires_in + " seconds" : ""}}

        Environment Ready: {{
          outputs.connectivity_ok && 
          outputs.auth_valid && 
          outputs.environment_confirmed == vars.environment
          ? "🟢 YES" : "🔴 NO"
        }}
```

## CI/CD統合

どの環境に対する実行かは、CI側の環境変数にすでに入っています。その情報を渡せば、同じワークフローで値とテストスイートを切り替えられます。

### GitHub Actions統合

自動環境テスト用にCI/CDパイプラインと統合します：

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
        env:
          DEV_API_TOKEN: ${{ secrets.DEV_API_TOKEN }}
        run: |
          probe workflows/base-workflow.yml,environments/development.yml,credentials/development.yml

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
          STAGING_API_TOKEN: ${{ secrets.STAGING_API_TOKEN }}
          STAGING_DB_PASSWORD: ${{ secrets.STAGING_DB_PASSWORD }}
        run: |
          probe workflows/environment-validation.yml,environments/staging.yml,credentials/staging.yml

      - name: Run Staging Tests
        env:
          STAGING_API_TOKEN: ${{ secrets.STAGING_API_TOKEN }}
          STAGING_DB_PASSWORD: ${{ secrets.STAGING_DB_PASSWORD }}
        run: |
          probe workflows/base-workflow.yml,environments/staging.yml,credentials/staging.yml

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
          PROD_API_TOKEN: ${{ secrets.PROD_API_TOKEN }}
          PROD_DB_PASSWORD: ${{ secrets.PROD_DB_PASSWORD }}
        run: |
          probe workflows/smoke-test.yml,environments/production.yml,credentials/production.yml
```

### 環境固有のテストスイート

異なる環境に対応する様々なテストスイートを作成します：

**smoke-test.yml（プロダクション用）:**
```yaml
name: Production Smoke Test
description: Minimal smoke test for production environment

vars:
  api_base_url: "{{API_BASE_URL}}"

jobs:
- name: Critical Endpoints Check
  steps:
    - name: Health Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health"
      test: res.code == 200
      outputs:
        api_healthy: res.code == 200

    - name: Authentication Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/auth/health"
      test: res.code == 200
      outputs:
        auth_healthy: res.code == 200

    - name: Database Check
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/health/database"
      test: res.code == 200
      outputs:
        db_healthy: res.code == 200

- name: Smoke Test Summary
  needs: [critical-endpoints-check]
  steps:
    - name: Production Health Summary
      uses: hello
      echo: |
        🏭 Production Smoke Test Results:

        Critical Systems:
        API: {{outputs['critical-endpoints-check'].api_healthy ? "✅ Healthy" : "🚨 DOWN"}}
        Authentication: {{outputs['critical-endpoints-check'].auth_healthy ? "✅ Healthy" : "🚨 DOWN"}}
        Database: {{outputs['critical-endpoints-check'].db_healthy ? "✅ Healthy" : "🚨 DOWN"}}

        Overall Status: {{
          outputs['critical-endpoints-check'].api_healthy &&
          outputs['critical-endpoints-check'].auth_healthy &&
          outputs['critical-endpoints-check'].db_healthy
          ? "🟢 ALL SYSTEMS OPERATIONAL" : "🔴 CRITICAL ISSUES DETECTED"
        }}
```

**comprehensive-test.yml（ステージング用）:**
```yaml
name: Comprehensive Staging Test
description: Full test suite for staging environment validation

vars:
  api_base_url: "{{API_BASE_URL}}"

jobs:
- name: API Test Suite
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

- name: Integration Tests
  needs: [api-test-suite]
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

- name: Performance Validation
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

## 環境モニタリングとアラート

テストで使う環境設定はそのまま監視にも使えます。これにより、各環境をその環境の基準で確認できます。

### 環境ヘルスモニタリング

各環境のヘルスを継続的に監視します：

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
  monitoring_interval: "{{MONITORING_INTERVAL ?? '300'}}"  # 5分
  alert_threshold: "{{ALERT_THRESHOLD ?? '2'}}"        # 連続2回失敗後にアラート

jobs:
- name: Environment Health Check
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
      uses: hello
      echo: |
        Environment-specific validation for {{vars.environment}}
      outputs:
        env_specific_checks: |
          {{vars.environment == "production" ? "SSL, Security, Performance" :
            vars.environment == "staging" ? "Integration, Load Testing" :
            "Development Tools, Debug Features"}}

- name: Environment Alerting
  needs: [environment-health-check]
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
      echo: |
        ENVIRONMENT HEALTH ALERT
        ========================

        Environment: {{vars.environment}}
        Time: {{unixtime()}}

        RESOURCE STATUS:
        {{outputs['environment-health-check'].resources_healthy ? "✅ Resources Healthy" : "❌ Resource Issues"}}
        {{!outputs['environment-health-check'].resources_healthy ? "CPU Usage: " + outputs['environment-health-check'].cpu_usage + "%" : ""}}
        {{!outputs['environment-health-check'].resources_healthy ? "Memory Usage: " + outputs['environment-health-check'].memory_usage + "%" : ""}}
        {{!outputs['environment-health-check'].resources_healthy ? "Disk Usage: " + outputs['environment-health-check'].disk_usage + "%" : ""}}

        DEPENDENCIES STATUS:
        {{outputs['environment-health-check'].dependencies_healthy ? "✅ All Dependencies Healthy" : "❌ Dependency Issues"}}
        {{!outputs['environment-health-check'].dependencies_healthy ? "Unhealthy Services: " + outputs['environment-health-check'].unhealthy_services : ""}}

        ACTION REQUIRED: Investigate {{vars.environment}} environment immediately

- name: Health Summary
  needs: [environment-health-check]
  steps:
    - name: All Systems Healthy
      uses: hello
      echo: |
        ✅ {{vars.environment}} Environment Health Check

        All systems operational:
        • Resources: CPU {{outputs['environment-health-check'].cpu_usage}}%, Memory {{outputs['environment-health-check'].memory_usage}}%, Disk {{outputs['environment-health-check'].disk_usage}}%
        • Dependencies: All healthy
        • Environment: {{vars.environment}}

        Next check in {{vars.monitoring_interval}} seconds
```

## ベストプラクティス

以下では、環境を分離すること、変更を環境の順に確認していくこと、設定を検証すること、秘密情報を扱うことを扱います。

### 1. 環境分離

環境ごとにファイルを分ければ値が混ざらず、差分として違いを確認できます。

```text
# 良い例: 明確な環境分離
environments/
├── development.yml
├── staging.yml
└── production.yml

# 良い例: 環境固有の認証情報
credentials/
├── development.yml
├── staging.yml
└── production.yml（環境変数のみ使用）
```

### 2. 段階的テスト

各環境にはそこで意味のある確認を置き、重い確認は本番より前の環境で実行します。

```yaml
# 良い例: テストパイプラインの進行
Development → Unit Tests
Staging → Integration Tests + Performance Tests  
Production → Smoke Tests + Monitoring
```

### 3. 設定検証

設定が不完全な環境に対して実行すると、アプリケーションの障害に見える失敗が出ます。そのため先に設定を確認します。

```yaml
# 良い例: テスト実行前の検証
vars:
  environment: "{{ENVIRONMENT}}"
  api_base_url: "{{API_BASE_URL}}"
  api_token: "{{API_TOKEN}}"
```

```yaml
- name: Pre-Test Validation
  echo: |
    Environment: {{vars.environment ? "✅" : "❌"}}
    API URL: {{vars.api_base_url ? "✅" : "❌"}}
    Credentials: {{vars.api_token ? "✅" : "❌"}}
```

### 4. セキュアなシークレット管理

秘密情報は環境変数から読みます。そうすれば環境設定のファイルはコミットでき、値そのものは残りません。

```yaml
# 良い例: シークレットに環境変数を使用
vars:
  api_token: "{{PROD_API_TOKEN}}"  # セキュアな環境変数から

# 避ける: ハードコードされたシークレット
vars:
  api_token: "hardcoded_secret_123"  # これは絶対にしない
```

あわせて`secrets`にも変数を列挙します。そうすると、CIのログやアップロードしたアーティファクトに残る端末の出力、アクションのログ、レポートファイルで、値が`<secret:NAME>`に置き換わります。

```yaml
secrets:
  - PROD_API_TOKEN
vars:
  api_token: "{{PROD_API_TOKEN}}"
```

## 次のステップ

効果的に環境を管理できるようになったので、次を探索してください：

- **[モニタリングワークフロー](/ja/guide/how-tos/monitoring-workflows)** - 全環境の包括的モニタリングを構築
- **[APIテスト](/ja/guide/how-tos/api-testing)** - 環境固有のAPIテストスイートを作成
- **[エラーハンドリング戦略](/ja/guide/how-tos/error-handling-strategies)** - 環境固有の障害を処理

環境管理は、ソフトウェア開発ライフサイクル全体にわたる信頼性の高いテストにとって重要です。これらのパターンを使用して、一貫性があり、セキュアで、保守しやすい環境設定を作成しましょう。
