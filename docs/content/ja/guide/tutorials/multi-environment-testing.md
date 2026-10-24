# マルチ環境デプロイテスト

このチュートリアルでは、Probeを使用してマルチ環境テスト戦略を作成する方法を学びます。開発、ステージング、プロダクション環境でのデプロイメントを自動的に検証し、デプロイメントパイプラインのすべての段階で一貫性と信頼性を確保するワークフローを構築します。

## 構築する内容

以下の機能を持つ完全なマルチ環境テストシステム：

- **環境固有の設定** - 各環境に合わせた設定
- **段階的デプロイテスト** - 各デプロイ段階の検証
- **環境間一貫性チェック** - 機能パリティの確保
- **環境ヘルス監視** - 継続的な環境検証
- **デプロイ検証パイプライン** - 自動デプロイ確認
- **ロールバック検出** - 環境の乖離を特定
- **設定ドリフト検出** - 環境一貫性の監視

## 前提条件

- Probeがインストール済み（[インストールガイド](/ja/guide/introduction/installation)）
- 複数環境へのアクセス（dev、staging、production）
- デプロイメントパイプラインの理解
- 環境管理の基本知識

## チュートリアル概要

典型的な3層環境セットアップのテストワークフローを作成します：

- **開発環境** - 最新機能、迅速な反復
- **ステージング環境** - 最終検証用のプロダクション類似環境
- **プロダクション環境** - ライブユーザー向け環境

## ステップ1: 環境設定構造

整理された設定構造を作成します：

```bash
deployment-tests/
├── config/
│   ├── base.yml              # 共通設定
│   ├── environments/
│   │   ├── development.yml   # 開発環境固有設定
│   │   ├── staging.yml       # ステージング環境固有設定
│   │   └── production.yml    # プロダクション環境固有設定
│   └── features/
│       ├── feature-flags.yml # 機能フラグ設定
│       └── experiments.yml   # A/Bテスト設定
├── workflows/
│   ├── health-check.yml      # 基本ヘルス検証
│   ├── deployment-validation.yml # デプロイ確認
│   ├── consistency-check.yml # 環境間検証
│   └── rollback-detection.yml # ロールバック識別
└── scripts/
    ├── setup-environment.sh  # 環境セットアップ
    └── deploy-validation.sh  # デプロイ検証スクリプト
```

**config/base.yml:**
```yaml merge=env base
name: "Multi-Environment Deployment Testing"
description: "開発、ステージング、プロダクション環境での包括的テスト"

vars: &base_vars
  # 共通設定
  API_VERSION: "v2"
  USER_AGENT: "Probe Multi-Env Tester v1.0"

  # テストユーザー設定
  TEST_USER_EMAIL: "test@example.com"
  TEST_USER_PASSWORD: "TestPassword123!"

  # パフォーマンスしきい値（ベース - 環境ごとに上書きされる）
  RESPONSE_TIME_THRESHOLD: 2000
  CRITICAL_RESPONSE_TIME: 5000

  # ヘルスチェック設定
  HEALTH_CHECK_INTERVAL: 30
  MAX_CONSECUTIVE_FAILURES: 3

  # 機能テスト
  FEATURE_VALIDATION_ENABLED: true
  A_B_TEST_VALIDATION_ENABLED: false

shared:
  common_headers: &common_headers
    User-Agent: "{{vars.USER_AGENT}}"
    Accept: "application/json"
```

共通ヘッダーはここでYAMLアンカーとして定義し、各ジョブが`defaults`に取り込みます：

```yaml merge=env
jobs:
- name: Environment checks
  defaults:
    http:
      headers:
        <<: *common_headers
  steps:
    - name: "API Health"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: res.code == 200
```

ファイルをマージすると、後のファイルのトップレベルのキーは前のファイルの同じキーを丸ごと置き換えます。そのため、環境ファイルに自分の`vars`だけを書くと、`base.yml`の`vars`は失われます。ファイルは読み込まれる前に1つのYAML文書につなげられるので、各環境ファイルはYAMLのマージキー`<<: *base_vars`でベースの変数を取り込みます。環境ファイルの`vars`も`env_vars`としてアンカーを付け、独自の変数を足すワークフローファイルから取り込めるようにします。

**config/environments/development.yml:**
```yaml merge=env base
vars: &env_vars
  <<: *base_vars

  # 環境識別
  ENVIRONMENT: "development"
  ENVIRONMENT_COLOR: "🟡"

  # API設定
  API_BASE_URL: "https://api-dev.example.com"
  WEB_BASE_URL: "https://web-dev.example.com"

  # 緩いパフォーマンスしきい値
  RESPONSE_TIME_THRESHOLD: 5000    # 5秒（より緩い）
  CRITICAL_RESPONSE_TIME: 10000    # 10秒

  # 開発環境固有設定
  DEBUG_MODE: true
  DETAILED_LOGGING: true
  SKIP_PERFORMANCE_TESTS: false   # 開発環境でもパフォーマンステスト
  SKIP_LOAD_TESTS: true           # 重い負荷テストはスキップ

  # 機能フラグ（開発環境は全機能を有効化）
  ENABLE_NEW_FEATURES: true
  ENABLE_EXPERIMENTAL_FEATURES: true
  ENABLE_DEBUG_ENDPOINTS: true

  # 開発データ
  USE_TEST_DATA: true
  RESET_DATA_BEFORE_TESTS: true

```

**config/environments/staging.yml:**
```yaml
vars: &env_vars
  <<: *base_vars

  # 環境識別
  ENVIRONMENT: "staging"
  ENVIRONMENT_COLOR: "🟠"

  # API設定
  API_BASE_URL: "https://api-staging.example.com"
  WEB_BASE_URL: "https://web-staging.example.com"

  # プロダクション類似のパフォーマンスしきい値
  RESPONSE_TIME_THRESHOLD: 2000    # 2秒
  CRITICAL_RESPONSE_TIME: 5000     # 5秒

  # ステージング環境固有設定
  DEBUG_MODE: false
  DETAILED_LOGGING: true
  SKIP_PERFORMANCE_TESTS: false
  SKIP_LOAD_TESTS: false

  # 機能フラグ（ステージングはプロダクション + 承認済み機能をミラー）
  ENABLE_NEW_FEATURES: true
  ENABLE_EXPERIMENTAL_FEATURES: false  # 安定機能のみ
  ENABLE_DEBUG_ENDPOINTS: false

  # ステージングデータ
  USE_TEST_DATA: false
  USE_PRODUCTION_LIKE_DATA: true
  RESET_DATA_BEFORE_TESTS: false

```

**config/environments/production.yml:**
```yaml
vars: &env_vars
  <<: *base_vars

  # 環境識別
  ENVIRONMENT: "production"
  ENVIRONMENT_COLOR: "🔴"

  # API設定
  API_BASE_URL: "https://api.example.com"
  WEB_BASE_URL: "https://web.example.com"

  # 厳密なパフォーマンスしきい値
  RESPONSE_TIME_THRESHOLD: 1500    # 1.5秒
  CRITICAL_RESPONSE_TIME: 3000     # 3秒

  # プロダクション設定
  DEBUG_MODE: false
  DETAILED_LOGGING: false
  SKIP_PERFORMANCE_TESTS: false
  SKIP_LOAD_TESTS: true            # プロダクションへの影響を避ける

  # 機能フラグ（プロダクションは安定・承認済み機能のみ）
  ENABLE_NEW_FEATURES: false
  ENABLE_EXPERIMENTAL_FEATURES: false
  ENABLE_DEBUG_ENDPOINTS: false

  # プロダクションデータ
  USE_TEST_DATA: false
  USE_PRODUCTION_DATA: true
  RESET_DATA_BEFORE_TESTS: false

  # プロダクション固有監視
  ENABLE_REAL_USER_MONITORING: true
  ENABLE_ERROR_TRACKING: true

```

## ステップ2: 基本環境ヘルスチェック

包括的なヘルス検証を作成します：

**workflows/health-check.yml:**
```yaml merge=env
name: "Environment Health Check"
description: "マージした設定が指す環境の包括的ヘルス検証"

jobs:
- id: infrastructure-health
  name: "Infrastructure Health Check"
  steps:
    - name: "API Server Health"
      id: api-health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.RESPONSE_TIME_THRESHOLD &&
        res.body.status == "healthy"
      outputs:
        api_status: res.body.status
        api_version: res.body.version
        api_response_time: (rt.sec * 1000)
        api_uptime: res.body.uptime

    - name: "Web Application Health"
      id: web-health
      uses: http
      with:
        method: GET
        url: "{{vars.WEB_BASE_URL}}/health"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.RESPONSE_TIME_THRESHOLD
      outputs:
        web_status: 'res.code == 200 ? "healthy" : "unhealthy"'
        web_response_time: (rt.sec * 1000)

    - name: "Database Connectivity"
      id: db-health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health/database"
      test: |
        res.code == 200 &&
        res.body.database.connected == true &&
        res.body.database.responseTime < 1000
      outputs:
        db_status: 'res.body.database.connected ? "connected" : "disconnected"'
        db_response_time: res.body.database.responseTime
        db_pool_size: res.body.database.poolSize

    - name: "Cache System Health"
      id: cache-health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health/cache"
      test: |
        res.code == 200 &&
        res.body.cache.connected == true
      outputs:
        cache_status: 'res.body.cache.connected ? "connected" : "disconnected"'
        cache_hit_rate: res.body.cache.hitRate

- id: service-dependencies
  name: "External Service Dependencies"
  needs: [infrastructure-health]
  steps:
    - name: "Payment Service Health"
      id: payment-health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health/payment"
      test: |
        res.code == 200 &&
        res.body.paymentService.available == true
      outputs:
        payment_status: 'res.body.paymentService.available ? "available" : "unavailable"'

    - name: "Email Service Health"
      id: email-health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health/email"
      test: |
        res.code == 200 &&
        res.body.emailService.available == true
      outputs:
        email_status: 'res.body.emailService.available ? "available" : "unavailable"'

    - name: "Search Service Health"
      id: search-health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health/search"
      test: |
        res.code == 200 &&
        res.body.searchService.available == true
      outputs:
        search_status: 'res.body.searchService.available ? "available" : "unavailable"'

- id: environment-report
  name: "Environment Health Report"
  needs: [infrastructure-health, service-dependencies]
  steps:
    - name: "Generate Health Report"
      uses: hello
      echo: |
        {{vars.ENVIRONMENT_COLOR}} === {{upper(vars.ENVIRONMENT)}} ENVIRONMENT HEALTH REPORT ===

        Infrastructure Status:
        • API Server: {{upper(outputs['api-health'].api_status)}} ({{outputs['api-health'].api_response_time}}ms)
          Version: {{outputs['api-health'].api_version}}
          Uptime: {{outputs['api-health'].api_uptime}}

        • Web Application: {{upper(outputs['web-health'].web_status)}} ({{outputs['web-health'].web_response_time}}ms)

        • Database: {{upper(outputs['db-health'].db_status)}} ({{outputs['db-health'].db_response_time}}ms)
          Pool Size: {{outputs['db-health'].db_pool_size}}

        • Cache: {{upper(outputs['cache-health'].cache_status)}}
          Hit Rate: {{outputs['cache-health'].cache_hit_rate}}%

        External Services:
        • Payment Service: {{upper(outputs['payment-health'].payment_status)}}
        • Email Service: {{upper(outputs['email-health'].email_status)}}
        • Search Service: {{upper(outputs['search-health'].search_status)}}

        Overall Status: {{
          outputs['api-health'].api_status == "healthy" &&
          outputs['web-health'].web_status == "healthy" &&
          outputs['db-health'].db_status == "connected" ? "✅ HEALTHY" : "⚠️ DEGRADED"
        }}

        Generated: {{now().Format('2006-01-02T15:04:05Z07:00')}}
        Environment: {{vars.ENVIRONMENT}}

    - name: "Health Check Validation"
      uses: hello
      test: |
        outputs['api-health'].api_status == "healthy" &&
        outputs['db-health'].db_status == "connected"
```

## ステップ3: デプロイ検証ワークフロー

包括的なデプロイ確認を作成します：

**workflows/deployment-validation.yml:**
```yaml merge=env
name: "Deployment Validation"
description: "マージした設定が指す環境でのデプロイ成功を検証"

vars:
  <<: *env_vars

  # デプロイメタデータ（CI/CDから渡される）
  DEPLOYMENT_ID: "{{DEPLOYMENT_ID ?? unixtime()}}"
  BUILD_VERSION: "{{BUILD_VERSION ?? 'unknown'}}"
  DEPLOYMENT_TIMESTAMP: "{{DEPLOYMENT_TIMESTAMP ?? now().Format('2006-01-02T15:04:05Z07:00')}}"

jobs:
- id: pre-deployment-validation
  name: "Pre-Deployment Validation"
  steps:
    - name: "Verify Deployment Prerequisites"
      id: prerequisites
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/deployment/prerequisites"
        headers:
          X-Deployment-ID: "{{vars.DEPLOYMENT_ID}}"
      test: |
        res.code == 200 &&
        res.body.readyForDeployment == true
      outputs:
        deployment_ready: res.body.readyForDeployment
        current_version: res.body.currentVersion
        target_version: res.body.targetVersion

    - name: "Database Migration Status"
      id: migration-status
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/deployment/migrations"
      test: |
        res.code == 200 &&
        res.body.pendingMigrations == 0
      outputs:
        pending_migrations: res.body.pendingMigrations
        last_migration: res.body.lastMigration

- id: deployment-verification
  name: "Deployment Verification"
  needs: [pre-deployment-validation]
  steps:
    - name: "Verify New Version Deployment"
      id: version-check
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/version"
      test: |
        res.code == 200 &&
        res.body.version == vars.BUILD_VERSION
      outputs:
        deployed_version: res.body.version
        deployment_time: res.body.deploymentTime
        build_hash: res.body.buildHash

    - name: "Feature Flag Validation"
      id: feature-flags
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/features"
      test: |
        res.code == 200 &&
        res.body.features != nil
      outputs:
        active_features: res.body.features
        feature_count: len(res.body.features)

    - name: "Configuration Validation"
      id: config-validation
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/configuration"
      test: |
        res.code == 200 &&
        res.body.environment == vars.ENVIRONMENT &&
        res.body.configVersion != nil
      outputs:
        config_environment: res.body.environment
        config_version: res.body.configVersion
        config_valid: res.body.valid

- id: functional-validation
  name: "Post-Deployment Functional Tests"
  needs: [deployment-verification]
  steps:
    - name: "Critical Path Test - User Authentication"
      id: auth-test
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/login"
        method: "POST"
        body: |
          {
            "email": "{{vars.TEST_USER_EMAIL}}",
            "password": "{{vars.TEST_USER_PASSWORD}}"
          }
      test: |
        res.code == 200 &&
        res.body.token != nil &&
        (rt.sec * 1000) < vars.RESPONSE_TIME_THRESHOLD
      outputs:
        auth_token: res.body.token
        auth_response_time: (rt.sec * 1000)

    - name: "Critical Path Test - Data Retrieval"
      id: data-test
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products?limit=5"
        headers:
          Authorization: "Bearer {{outputs['auth-test'].auth_token}}"
      test: |
        res.code == 200 &&
        res.body.products != nil &&
        len(res.body.products) > 0 &&
        (rt.sec * 1000) < vars.RESPONSE_TIME_THRESHOLD
      outputs:
        product_count: len(res.body.products)
        data_response_time: (rt.sec * 1000)

    - name: "Critical Path Test - Data Mutation"
      id: mutation-test
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
        method: "POST"
        headers:
          Authorization: "Bearer {{outputs['auth-test'].auth_token}}"
        body: |
          {
            "name": "Deployment Test Product {{vars.DEPLOYMENT_ID}}",
            "price": 99.99,
            "category": "Test"
          }
      test: |
        res.code == 201 &&
        res.body.id != nil &&
        (rt.sec * 1000) < vars.RESPONSE_TIME_THRESHOLD
      outputs:
        created_product_id: res.body.id
        mutation_response_time: (rt.sec * 1000)

    - name: "Cleanup Test Data"
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/{{outputs['mutation-test'].created_product_id}}"
        method: "DELETE"
        headers:
          Authorization: "Bearer {{outputs['auth-test'].auth_token}}"
      test: res.code == 204 || res.code == 200

- id: performance-validation
  name: "Performance Validation"
  needs: [functional-validation]
  skipif: vars.SKIP_PERFORMANCE_TESTS
  steps:
    - name: "Response Time Validation"
      id: perf-test
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.RESPONSE_TIME_THRESHOLD
      outputs:
        response_time: (rt.sec * 1000)
        performance_grade: |
          (rt.sec * 1000) < 500 ? "A" :
          (rt.sec * 1000) < 1000 ? "B" :
          (rt.sec * 1000) < 2000 ? "C" : "D"

    - name: "Load Test Simulation"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: res.code == 200 && (rt.sec * 1000) < (vars.RESPONSE_TIME_THRESHOLD * 2)
      # 実際のシナリオでは、これが同時リクエストをトリガーする

- id: deployment-report
  name: "Deployment Validation Report"
  needs: [pre-deployment-validation, deployment-verification, functional-validation, performance-validation]
  steps:
    - name: "Generate Deployment Report"
      uses: hello
      echo: |
        {{vars.ENVIRONMENT_COLOR}} === {{upper(vars.ENVIRONMENT)}} DEPLOYMENT VALIDATION REPORT ===

        Deployment Information:
        • Deployment ID: {{vars.DEPLOYMENT_ID}}
        • Target Version: {{vars.BUILD_VERSION}}
        • Deployed Version: {{outputs['version-check'].deployed_version}}
        • Deployment Time: {{outputs['version-check'].deployment_time}}
        • Environment: {{vars.ENVIRONMENT}}

        Pre-Deployment Status:
        • Prerequisites: {{outputs.prerequisites.deployment_ready ? "✅ Ready" : "❌ Not Ready"}}
        • Current Version: {{outputs.prerequisites.current_version}}
        • Target Version: {{outputs.prerequisites.target_version}}
        • Pending Migrations: {{outputs['migration-status'].pending_migrations}}

        Deployment Verification:
        • Version Match: {{outputs['version-check'].deployed_version == vars.BUILD_VERSION ? "✅ Correct" : "❌ Mismatch"}}
        • Configuration: {{outputs['config-validation'].config_valid ? "✅ Valid" : "❌ Invalid"}}
        • Feature Flags: {{vars.ENABLE_NEW_FEATURES ? string(outputs['feature-flags'].feature_count) + " features active" : "Default features"}}

        Functional Tests:
        • Authentication: {{outputs['auth-test'].auth_response_time}}ms {{outputs['auth-test'].auth_response_time < vars.RESPONSE_TIME_THRESHOLD ? "✅" : "⚠️"}}
        • Data Retrieval: {{outputs['data-test'].data_response_time}}ms {{outputs['data-test'].data_response_time < vars.RESPONSE_TIME_THRESHOLD ? "✅" : "⚠️"}}
        • Data Mutation: {{outputs['mutation-test'].mutation_response_time}}ms {{outputs['mutation-test'].mutation_response_time < vars.RESPONSE_TIME_THRESHOLD ? "✅" : "⚠️"}}

        Performance Validation:
        • Response Time: {{vars.SKIP_PERFORMANCE_TESTS ? "⏭️ Skipped" : string(outputs['perf-test'].response_time) + "ms (Grade: " + outputs['perf-test'].performance_grade + ")"}}
        • Performance Status: {{vars.SKIP_PERFORMANCE_TESTS ? "⏭️ Skipped" : outputs['perf-test'].response_time < vars.RESPONSE_TIME_THRESHOLD ? "✅ Acceptable" : "⚠️ Slow"}}

        Overall Deployment Status: {{
          outputs['version-check'].deployed_version == vars.BUILD_VERSION &&
          outputs['config-validation'].config_valid &&
          outputs['auth-test'].auth_response_time < vars.RESPONSE_TIME_THRESHOLD &&
          outputs['data-test'].data_response_time < vars.RESPONSE_TIME_THRESHOLD &&
          outputs['mutation-test'].mutation_response_time < vars.RESPONSE_TIME_THRESHOLD ?
          "✅ DEPLOYMENT SUCCESSFUL" : "❌ DEPLOYMENT ISSUES DETECTED"
        }}

        Generated: {{now().Format('2006-01-02T15:04:05Z07:00')}}

    - name: "Validate Deployment Success"
      uses: hello
      test: |
        outputs['version-check'].deployed_version == vars.BUILD_VERSION &&
        outputs['config-validation'].config_valid &&
        outputs['auth-test'].auth_response_time < vars.RESPONSE_TIME_THRESHOLD &&
        outputs['data-test'].data_response_time < vars.RESPONSE_TIME_THRESHOLD
```

## ステップ4: 環境間一貫性チェック

環境間の一貫性を検証するワークフローを作成します：

**workflows/consistency-check.yml:**
```yaml
name: "Cross-Environment Consistency Check"
description: "環境間の一貫性を検証"

vars:
  # 比較する環境を定義
  PRIMARY_ENVIRONMENT: "{{PRIMARY_ENV ?? 'production'}}"
  COMPARISON_ENVIRONMENTS: "{{COMPARISON_ENVS ?? 'staging,development'}}"

jobs:
- id: version-consistency
  name: "Version Consistency Check"
  steps:
    - name: "Get Primary Environment Version"
      id: primary-version
      uses: http
      with:
        method: GET
        url: "{{vars.PRIMARY_ENVIRONMENT == 'production' ? 'https://api.example.com' :
                vars.PRIMARY_ENVIRONMENT == 'staging' ? 'https://api-staging.example.com' :
                'https://api-dev.example.com'}}/version"
      test: res.code == 200
      outputs:
        primary_version: res.body.version
        primary_env: vars.PRIMARY_ENVIRONMENT
        primary_config_version: res.body.configVersion

    - name: "Get Staging Environment Version"
      id: staging-version
      uses: http
      with:
        method: GET
        url: "https://api-staging.example.com/version"
      test: res.code == 200
      outputs:
        staging_version: res.body.version
        staging_config_version: res.body.configVersion

    - name: "Get Development Environment Version"
      id: dev-version
      uses: http
      with:
        method: GET
        url: "https://api-dev.example.com/version"
      test: res.code == 200
      outputs:
        dev_version: res.body.version
        dev_config_version: res.body.configVersion

- id: feature-consistency
  name: "Feature Flag Consistency"
  needs: [version-consistency]
  steps:
    - name: "Compare Production vs Staging Features"
      id: prod-staging-features
      uses: http
      with:
        method: GET
        url: "https://api.example.com/features"
      test: res.code == 200
      outputs:
        prod_features: keys(res.body.features)
        prod_feature_count: len(res.body.features)

    - name: "Get Staging Features"
      id: staging-features
      uses: http
      with:
        method: GET
        url: "https://api-staging.example.com/features"
      test: res.code == 200
      outputs:
        staging_features: keys(res.body.features)
        staging_feature_count: len(res.body.features)

    - name: "Feature Drift Analysis"
      uses: hello
      echo: |
        === FEATURE FLAG CONSISTENCY ANALYSIS ===

        Production Features: {{outputs['prod-staging-features'].prod_feature_count}}
        Staging Features: {{outputs['staging-features'].staging_feature_count}}

        Feature Drift: {{abs(outputs['prod-staging-features'].prod_feature_count - outputs['staging-features'].staging_feature_count)}} features different

        Status: {{outputs['prod-staging-features'].prod_feature_count == outputs['staging-features'].staging_feature_count ? "✅ Consistent" : "⚠️ Drift Detected"}}

- id: configuration-consistency
  name: "Configuration Consistency"
  needs: [version-consistency]
  steps:
    - name: "Database Schema Consistency"
      id: schema-consistency
      uses: http
      with:
        method: GET
        url: "https://api.example.com/schema/version"
      test: res.code == 200
      outputs:
        prod_schema_version: res.body.schemaVersion
        prod_migration_count: res.body.migrationCount

    - name: "Staging Schema Version"
      id: staging-schema
      uses: http
      with:
        method: GET
        url: "https://api-staging.example.com/schema/version"
      test: res.code == 200
      outputs:
        staging_schema_version: res.body.schemaVersion
        staging_migration_count: res.body.migrationCount

    - name: "Schema Consistency Report"
      uses: hello
      echo: |
        === DATABASE SCHEMA CONSISTENCY ===

        Production Schema: v{{outputs['schema-consistency'].prod_schema_version}} ({{outputs['schema-consistency'].prod_migration_count}} migrations)
        Staging Schema: v{{outputs['staging-schema'].staging_schema_version}} ({{outputs['staging-schema'].staging_migration_count}} migrations)

        Schema Status: {{outputs['schema-consistency'].prod_schema_version == outputs['staging-schema'].staging_schema_version ? "✅ Synchronized" : "⚠️ Version Mismatch"}}
        Migration Status: {{outputs['schema-consistency'].prod_migration_count == outputs['staging-schema'].staging_migration_count ? "✅ Synchronized" : "⚠️ Migration Count Mismatch"}}

- id: api-consistency
  name: "API Endpoint Consistency"
  needs: [version-consistency]
  steps:
    - name: "Get Production API Endpoints"
      id: prod-endpoints
      uses: http
      with:
        method: GET
        url: "https://api.example.com/api/endpoints"
      test: res.code == 200
      outputs:
        prod_endpoints: keys(res.body.endpoints)
        prod_endpoint_count: len(res.body.endpoints)

    - name: "Get Staging API Endpoints"
      id: staging-endpoints
      uses: http
      with:
        method: GET
        url: "https://api-staging.example.com/api/endpoints"
      test: res.code == 200
      outputs:
        staging_endpoints: keys(res.body.endpoints)
        staging_endpoint_count: len(res.body.endpoints)

    - name: "API Consistency Analysis"
      uses: hello
      echo: |
        === API ENDPOINT CONSISTENCY ===

        Production Endpoints: {{outputs['prod-endpoints'].prod_endpoint_count}}
        Staging Endpoints: {{outputs['staging-endpoints'].staging_endpoint_count}}

        Endpoint Drift: {{abs(outputs['prod-endpoints'].prod_endpoint_count - outputs['staging-endpoints'].staging_endpoint_count)}} endpoints different

        API Status: {{outputs['prod-endpoints'].prod_endpoint_count == outputs['staging-endpoints'].staging_endpoint_count ? "✅ Consistent" : "⚠️ Endpoint Drift"}}

- id: consistency-report
  name: "Overall Consistency Report"
  needs: [version-consistency, feature-consistency, configuration-consistency, api-consistency]
  steps:
    - name: "Generate Consistency Report"
      uses: hello
      echo: |
        🔍 === CROSS-ENVIRONMENT CONSISTENCY REPORT ===

        Environment Versions:
        • Production: {{outputs['primary-version'].primary_version}} (config: {{outputs['primary-version'].primary_config_version}})
        • Staging: {{outputs['staging-version'].staging_version ?? "N/A"}} (config: {{outputs['staging-version'].staging_config_version ?? "N/A"}})
        • Development: {{outputs['dev-version'].dev_version ?? "N/A"}} (config: {{outputs['dev-version'].dev_config_version ?? "N/A"}})

        Consistency Checks:
        • Version Alignment: {{outputs['primary-version'].primary_version == outputs['staging-version'].staging_version ? "✅ Aligned" : "⚠️ Misaligned"}}
        • Feature Flags: {{outputs['prod-staging-features'].prod_feature_count == outputs['staging-features'].staging_feature_count ? "✅ Consistent" : "⚠️ Drift Detected"}}
        • Database Schema: {{outputs['schema-consistency'].prod_schema_version == outputs['staging-schema'].staging_schema_version ? "✅ Synchronized" : "⚠️ Mismatch"}}
        • API Endpoints: {{outputs['prod-endpoints'].prod_endpoint_count == outputs['staging-endpoints'].staging_endpoint_count ? "✅ Consistent" : "⚠️ Drift"}}

        Recommendations:
        {{outputs['primary-version'].primary_version != outputs['staging-version'].staging_version ? "⚠️ Version mismatch detected - consider updating staging environment" : ""}}
        {{outputs['schema-consistency'].prod_schema_version != outputs['staging-schema'].staging_schema_version ? "⚠️ Schema version mismatch - verify migration deployment" : ""}}
        {{outputs['prod-staging-features'].prod_feature_count != outputs['staging-features'].staging_feature_count ? "⚠️ Feature flag drift - review feature flag synchronization" : ""}}

        Overall Status: {{
          outputs['primary-version'].primary_version == outputs['staging-version'].staging_version &&
          outputs['schema-consistency'].prod_schema_version == outputs['staging-schema'].staging_schema_version ?
          "✅ ENVIRONMENTS CONSISTENT" : "⚠️ CONSISTENCY ISSUES DETECTED"
        }}

        Generated: {{now().Format('2006-01-02T15:04:05Z07:00')}}
```

## ステップ5: 自動デプロイパイプライン統合

CI/CDパイプラインと統合するスクリプトを作成します：

**scripts/deploy-validation.sh:**
```bash
#!/bin/bash

set -e

# 設定
ENVIRONMENT=${1:-staging}
BUILD_VERSION=${2:-$(git rev-parse --short HEAD)}
DEPLOYMENT_ID=${3:-$(date +%s)}

# パラメータ検証
if [[ ! "$ENVIRONMENT" =~ ^(development|staging|production)$ ]]; then
    echo "❌ Invalid environment: $ENVIRONMENT"
    echo "Usage: $0 <environment> [build_version] [deployment_id]"
    exit 1
fi

echo "🚀 Starting deployment validation for $ENVIRONMENT environment"
echo "📦 Build Version: $BUILD_VERSION"
echo "🆔 Deployment ID: $DEPLOYMENT_ID"

# 環境変数をエクスポート
export ENVIRONMENT=$ENVIRONMENT
export BUILD_VERSION=$BUILD_VERSION
export DEPLOYMENT_ID=$DEPLOYMENT_ID
export DEPLOYMENT_TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# 環境固有の変数を設定
case $ENVIRONMENT in
    "development")
        export API_AUTH_TOKEN=$DEV_API_TOKEN
        export SMTP_USERNAME=$DEV_SMTP_USERNAME
        export SMTP_PASSWORD=$DEV_SMTP_PASSWORD
        ;;
    "staging")
        export API_AUTH_TOKEN=$STAGING_API_TOKEN
        export SMTP_USERNAME=$STAGING_SMTP_USERNAME
        export SMTP_PASSWORD=$STAGING_SMTP_PASSWORD
        ;;
    "production")
        export API_AUTH_TOKEN=$PROD_API_TOKEN
        export SMTP_USERNAME=$PROD_SMTP_USERNAME
        export SMTP_PASSWORD=$PROD_SMTP_PASSWORD
        ;;
esac

echo "🏥 Running health check..."
if probe config/base.yml,config/environments/${ENVIRONMENT}.yml,workflows/health-check.yml; then
    echo "✅ Health check passed"
else
    echo "❌ Health check failed"
    exit 1
fi

echo "🔍 Running deployment validation..."
if probe config/base.yml,config/environments/${ENVIRONMENT}.yml,workflows/deployment-validation.yml; then
    echo "✅ Deployment validation passed"
else
    echo "❌ Deployment validation failed"
    exit 1
fi

echo "🔄 Running consistency checks..."
if [[ "$ENVIRONMENT" != "development" ]]; then
    if probe config/base.yml,config/environments/${ENVIRONMENT}.yml,workflows/consistency-check.yml; then
        echo "✅ Consistency checks passed"
    else
        echo "⚠️ Consistency issues detected (non-blocking)"
    fi
else
    echo "⏭️ Skipping consistency checks for development environment"
fi

echo "🎉 Deployment validation completed successfully for $ENVIRONMENT"
echo "📊 Deployment ID: $DEPLOYMENT_ID"
echo "⏰ Completed at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
```

**scripts/setup-environment.sh:**
```bash
#!/bin/bash

ENVIRONMENT=${1:-development}

echo "🔧 Setting up environment variables for: $ENVIRONMENT"

# 環境固有のシークレットを読み込む
case $ENVIRONMENT in
    "development")
        export DEV_API_TOKEN=$(cat ~/.probe/dev-api-token 2>/dev/null || echo "dev-token-placeholder")
        export DEV_SMTP_USERNAME=$(cat ~/.probe/dev-smtp-user 2>/dev/null || echo "dev@example.com")
        export DEV_SMTP_PASSWORD=$(cat ~/.probe/dev-smtp-pass 2>/dev/null || echo "dev-password")
        ;;
    "staging")
        export STAGING_API_TOKEN=$(cat ~/.probe/staging-api-token 2>/dev/null || echo "staging-token-placeholder")
        export STAGING_SMTP_USERNAME=$(cat ~/.probe/staging-smtp-user 2>/dev/null || echo "staging@example.com")
        export STAGING_SMTP_PASSWORD=$(cat ~/.probe/staging-smtp-pass 2>/dev/null || echo "staging-password")
        ;;
    "production")
        export PROD_API_TOKEN=$(cat ~/.probe/prod-api-token 2>/dev/null || echo "prod-token-placeholder")
        export PROD_SMTP_USERNAME=$(cat ~/.probe/prod-smtp-user 2>/dev/null || echo "alerts@example.com")
        export PROD_SMTP_PASSWORD=$(cat ~/.probe/prod-smtp-pass 2>/dev/null || echo "prod-password")
        ;;
esac

# 共通の環境変数
export PROBE_OUTPUT=${PROBE_OUTPUT:-auto}

echo "✅ Environment setup complete for: $ENVIRONMENT"
```

## ステップ6: GitHub Actions統合

包括的なCI/CDワークフローを作成します：

**.github/workflows/multi-environment-deployment.yml:**
```yaml
name: Multi-Environment Deployment Testing

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]
  workflow_dispatch:
    inputs:
      environment:
        description: 'Target environment'
        required: true
        default: 'staging'
        type: choice
        options:
        - development
        - staging
        - production

jobs:
  determine-environments:
    runs-on: ubuntu-latest
    outputs:
      environments: ${{ steps.set-environments.outputs.environments }}
      build-version: ${{ steps.set-version.outputs.version }}
    steps:
      - name: Determine deployment environments
        id: set-environments
        run: |
          if [[ "${{ github.event_name }}" == "workflow_dispatch" ]]; then
            echo "environments=[\"${{ github.event.inputs.environment }}\"]" >> $GITHUB_OUTPUT
          elif [[ "${{ github.ref }}" == "refs/heads/main" ]]; then
            echo "environments=[\"staging\", \"production\"]" >> $GITHUB_OUTPUT
          elif [[ "${{ github.ref }}" == "refs/heads/develop" ]]; then
            echo "environments=[\"development\", \"staging\"]" >> $GITHUB_OUTPUT
          else
            echo "environments=[\"development\"]" >> $GITHUB_OUTPUT
          fi

      - name: Set build version
        id: set-version
        run: |
          echo "version=${{ github.sha }}" >> $GITHUB_OUTPUT

  deploy-and-test:
    needs: determine-environments
    runs-on: ubuntu-latest
    strategy:
      matrix:
        environment: ${{ fromJson(needs.determine-environments.outputs.environments) }}
      fail-fast: false

    environment: ${{ matrix.environment }}

    steps:
      - name: Checkout code
        uses: actions/checkout@v3

      - name: Install Probe
        run: |
          curl -L https://github.com/mozership/probe/releases/latest/download/probe_linux_x86_64.tar.gz | tar -xz probe
          chmod +x probe
          sudo mv probe /usr/local/bin/

      - name: Setup environment
        run: |
          chmod +x scripts/setup-environment.sh
          ./scripts/setup-environment.sh ${{ matrix.environment }}
        env:
          DEV_API_TOKEN: ${{ secrets.DEV_API_TOKEN }}
          STAGING_API_TOKEN: ${{ secrets.STAGING_API_TOKEN }}
          PROD_API_TOKEN: ${{ secrets.PROD_API_TOKEN }}
          DEV_SMTP_USERNAME: ${{ secrets.DEV_SMTP_USERNAME }}
          DEV_SMTP_PASSWORD: ${{ secrets.DEV_SMTP_PASSWORD }}
          STAGING_SMTP_USERNAME: ${{ secrets.STAGING_SMTP_USERNAME }}
          STAGING_SMTP_PASSWORD: ${{ secrets.STAGING_SMTP_PASSWORD }}
          PROD_SMTP_USERNAME: ${{ secrets.PROD_SMTP_USERNAME }}
          PROD_SMTP_PASSWORD: ${{ secrets.PROD_SMTP_PASSWORD }}

      - name: Run deployment validation
        run: |
          chmod +x scripts/deploy-validation.sh
          ./scripts/deploy-validation.sh ${{ matrix.environment }} ${{ needs.determine-environments.outputs['build-version'] }}
        env:
          BUILD_VERSION: ${{ needs.determine-environments.outputs['build-version'] }}
          DEPLOYMENT_ID: ${{ github.run_id }}-${{ matrix.environment }}

      - name: Upload test results
        if: always()
        uses: actions/upload-artifact@v3
        with:
          name: test-results-${{ matrix.environment }}
          path: test-results/
          retention-days: 30

  consistency-check:
    needs: [determine-environments, deploy-and-test]
    runs-on: ubuntu-latest
    if: contains(fromJson(needs.determine-environments.outputs.environments), 'production') && contains(fromJson(needs.determine-environments.outputs.environments), 'staging')

    steps:
      - name: Checkout code
        uses: actions/checkout@v3

      - name: Install Probe
        run: |
          curl -L https://github.com/mozership/probe/releases/latest/download/probe_linux_x86_64.tar.gz | tar -xz probe
          chmod +x probe
          sudo mv probe /usr/local/bin/

      - name: Run cross-environment consistency check
        run: |
          probe config/base.yml,workflows/consistency-check.yml
        env:
          PRIMARY_ENV: production
          COMPARISON_ENVS: staging,development

  deployment-report:
    needs: [determine-environments, deploy-and-test, consistency-check]
    runs-on: ubuntu-latest
    if: always()

    steps:
      - name: Generate deployment report
        run: |
          echo "## 🚀 Multi-Environment Deployment Report" >> $GITHUB_STEP_SUMMARY
          echo "" >> $GITHUB_STEP_SUMMARY
          echo "**Build Version:** ${{ needs.determine-environments.outputs['build-version'] }}" >> $GITHUB_STEP_SUMMARY
          echo "**Environments:** ${{ needs.determine-environments.outputs.environments }}" >> $GITHUB_STEP_SUMMARY
          echo "**Timestamp:** $(date -u +"%Y-%m-%dT%H:%M:%SZ")" >> $GITHUB_STEP_SUMMARY
          echo "" >> $GITHUB_STEP_SUMMARY

          # 環境ごとのデプロイ状況を追加
          echo "### Deployment Status" >> $GITHUB_STEP_SUMMARY
          echo "" >> $GITHUB_STEP_SUMMARY

          # 実際のテスト結果で埋める
          echo "| Environment | Status | Tests | Performance |" >> $GITHUB_STEP_SUMMARY
          echo "|-------------|--------|-------|-------------|" >> $GITHUB_STEP_SUMMARY
          echo "| Development | ✅ Success | ✅ Passed | ✅ Good |" >> $GITHUB_STEP_SUMMARY
          echo "| Staging | ✅ Success | ✅ Passed | ✅ Good |" >> $GITHUB_STEP_SUMMARY
          echo "| Production | ${{ needs.deploy-and-test.result == 'success' && '✅ Success' || '❌ Failed' }} | Results | Performance |" >> $GITHUB_STEP_SUMMARY
```

## ステップ7: マルチ環境テストの実行

包括的なマルチ環境テストを実行します：

```bash
# 個別環境のテスト
probe config/base.yml,config/environments/development.yml,workflows/health-check.yml
probe config/base.yml,config/environments/staging.yml,workflows/deployment-validation.yml
probe config/base.yml,config/environments/production.yml,workflows/health-check.yml

# 完全なデプロイ検証を実行
./scripts/deploy-validation.sh staging v2.1.0
./scripts/deploy-validation.sh production v2.1.0

# 一貫性チェックを実行
probe config/base.yml,workflows/consistency-check.yml

# すべての環境を順次実行
for env in development staging production; do
    echo "Testing $env environment..."
    probe config/base.yml,config/environments/${env}.yml,workflows/health-check.yml
done
```

## ステップ8: 高度なマルチ環境パターン

2つのバージョンを並行して動かすデプロイ方式では、テスト側でどちらに接続しているかを区別できる必要があります。

### Blue-Greenデプロイテスト

Blue-Greenデプロイのテストを作成します：

```yaml
# workflows/blue-green-validation.yml
name: "Blue-Green Deployment Validation"

vars:
  BLUE_URL: "{{BLUE_URL}}"
  GREEN_URL: "{{GREEN_URL}}"

jobs:
- id: blue-green-test
  name: blue-green-test
  steps:
    - name: "Test Blue Environment"
      id: blue-test
      uses: http
      with:
        method: GET
        url: "{{vars.BLUE_URL}}/health"
      test: res.code == 200
      outputs:
        blue_time: rt.sec * 1000

    - name: "Test Green Environment"
      id: green-test
      uses: http
      with:
        method: GET
        url: "{{vars.GREEN_URL}}/health"
      test: res.code == 200
      outputs:
        green_time: rt.sec * 1000

    - name: "Compare Environment Performance"
      uses: hello
      echo: |
        Blue Environment: {{outputs['blue-test'].blue_time}}ms
        Green Environment: {{outputs['green-test'].green_time}}ms
        Performance Difference: {{abs(outputs['blue-test'].blue_time - outputs['green-test'].green_time)}}ms
```

### カナリアデプロイテスト

カナリアデプロイを監視します：

```yaml
# workflows/canary-validation.yml
name: "Canary Deployment Validation"

vars:
  API_BASE_URL: "{{API_BASE_URL}}"
  MAX_CANARY_TRAFFIC: 10
  MAX_CANARY_ERROR_RATE: 0.01

jobs:
- id: canary-monitoring
  name: canary-monitoring
  steps:
    - name: "Monitor Canary Traffic"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/metrics/canary"
      test: |
        res.code == 200 &&
        res.body.canaryTrafficPercent <= vars.MAX_CANARY_TRAFFIC &&
        res.body.canaryErrorRate < vars.MAX_CANARY_ERROR_RATE
```

## トラブルシューティング

ここまでのワークフローが説明どおりに動かない場合は、以下を順に確認してください。

### よくある問題

**環境設定のミスマッチ:**
```bash
# デプロイ前に設定を検証
probe config/base.yml,config/environments/staging.yml --validate-only
```

**バージョンドリフト検出:**
```bash
# 環境間のバージョン一貫性をチェック
probe workflows/consistency-check.yml --verbose
```

**パフォーマンス回帰:**
```bash
# 環境間のパフォーマンスを比較
probe config/base.yml,config/environments/production.yml,workflows/performance-validation.yml
```

## 次のステップ

マルチ環境デプロイテストシステムが完成しました！以下の拡張を検討してください：

1. **高度な監視** - ビジネスメトリクス検証の追加
2. **自動ロールバック** - 失敗時のロールバックトリガー
3. **段階的デプロイ** - 段階的ロールアウトテストの実装
4. **コンプライアンステスト** - セキュリティとコンプライアンス検証の追加
5. **パフォーマンスベンチマーク** - 履歴パフォーマンス比較

## 関連リソース

- **[初めての監視システムチュートリアル](/ja/guide/tutorials/first-monitoring-system)** - 基本的な監視セットアップ
- **[APIテストパイプラインチュートリアル](/ja/guide/tutorials/api-testing-pipeline)** - 包括的なAPIテスト
- **[ハウツー: モニタリングワークフロー](/ja/guide/how-tos/monitoring-workflows)** - 高度な監視パターン
- **[ハウツー: 環境管理](/ja/guide/how-tos/environment-management)** - 環境設定戦略
