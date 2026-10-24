# Multi-Environment Deployment Testing

In this tutorial, you'll learn how to create a comprehensive multi-environment testing strategy using Probe. You'll build workflows that automatically validate deployments across development, staging, and production environments, ensuring consistency and reliability at every stage of your deployment pipeline.

## What You'll Build

A complete multi-environment testing system featuring:

- **Environment-Specific Configurations** - Tailored settings for each environment
- **Progressive Deployment Testing** - Validate each deployment stage
- **Cross-Environment Consistency Checks** - Ensure feature parity
- **Environment Health Monitoring** - Continuous environment validation
- **Deployment Validation Pipelines** - Automated deployment verification
- **Rollback Detection** - Identify when environments diverge
- **Configuration Drift Detection** - Monitor environment consistency

## Prerequisites

- Probe installed ([Installation Guide](/guide/introduction/installation))
- Access to multiple environments (dev, staging, production)
- Understanding of deployment pipelines
- Basic knowledge of environment management

## Tutorial Overview

We'll create testing workflows for a typical three-tier environment setup:

- **Development** - Latest features, rapid iteration
- **Staging** - Production-like environment for final validation
- **Production** - Live user-facing environment

## Step 1: Environment Configuration Structure

Create a well-organized configuration structure:

```bash
deployment-tests/
├── config/
│   ├── base.yml              # Common configuration
│   ├── environments/
│   │   ├── development.yml   # Dev-specific config
│   │   ├── staging.yml       # Staging-specific config
│   │   └── production.yml    # Prod-specific config
│   └── features/
│       ├── feature-flags.yml # Feature flag configurations
│       └── experiments.yml   # A/B test configurations
├── workflows/
│   ├── health-check.yml      # Basic health validation
│   ├── deployment-validation.yml # Deployment verification
│   ├── consistency-check.yml # Cross-environment validation
│   └── rollback-detection.yml # Rollback identification
└── scripts/
    ├── setup-environment.sh  # Environment setup
    └── deploy-validation.sh  # Deployment validation script
```

**config/base.yml:**
```yaml
name: "Multi-Environment Deployment Testing"
description: "Comprehensive testing across development, staging, and production environments"

vars: &base_vars
  # Common Configuration
  API_VERSION: "v2"
  USER_AGENT: "Probe Multi-Env Tester v1.0"
  
  # Test User Configuration
  TEST_USER_EMAIL: "test@example.com"
  TEST_USER_PASSWORD: "TestPassword123!"
  
  # Performance Thresholds (Base - will be overridden per environment)
  RESPONSE_TIME_THRESHOLD: 2000
  CRITICAL_RESPONSE_TIME: 5000
  
  # Health Check Configuration
  HEALTH_CHECK_INTERVAL: 30
  MAX_CONSECUTIVE_FAILURES: 3
  
  # Feature Testing
  FEATURE_VALIDATION_ENABLED: true
  A_B_TEST_VALIDATION_ENABLED: false

shared:
  common_headers: &common_headers
    User-Agent: "{{vars.USER_AGENT}}"
    Accept: "application/json"
```

The shared headers are defined here as a YAML anchor, and each job pulls them into its `defaults`:

```yaml
jobs:
- name: Environment checks
  defaults:
    http:
      headers:
        <<: *common_headers
  steps:
    # ...
```

When files are merged, a top-level key in a later file replaces the same key of an earlier file as a whole, so an environment file that wrote only its own `vars` would drop those of `base.yml`. The files are joined into one YAML document before they are read, so each environment file pulls the base vars in with the YAML merge key `<<: *base_vars`. It is anchored in turn as `env_vars`, for a workflow file that adds vars of its own.

**config/environments/development.yml:**
```yaml
vars: &env_vars
  <<: *base_vars

  # Environment Identification
  ENVIRONMENT: "development"
  ENVIRONMENT_COLOR: "🟡"
  
  # API Configuration
  API_BASE_URL: "https://api-dev.example.com"
  WEB_BASE_URL: "https://web-dev.example.com"
  
  # Relaxed Performance Thresholds
  RESPONSE_TIME_THRESHOLD: 5000    # 5 seconds (more lenient)
  CRITICAL_RESPONSE_TIME: 10000    # 10 seconds
  
  # Development-Specific Settings
  DEBUG_MODE: true
  DETAILED_LOGGING: true
  SKIP_PERFORMANCE_TESTS: false   # Still test performance in dev
  SKIP_LOAD_TESTS: true           # Skip heavy load testing
  
  # Feature Flags (Development gets all features)
  ENABLE_NEW_FEATURES: true
  ENABLE_EXPERIMENTAL_FEATURES: true
  ENABLE_DEBUG_ENDPOINTS: true
  
  # Development Data
  USE_TEST_DATA: true
  RESET_DATA_BEFORE_TESTS: true

```

**config/environments/staging.yml:**
```yaml
vars: &env_vars
  <<: *base_vars

  # Environment Identification
  ENVIRONMENT: "staging"
  ENVIRONMENT_COLOR: "🟠"
  
  # API Configuration
  API_BASE_URL: "https://api-staging.example.com"
  WEB_BASE_URL: "https://web-staging.example.com"
  
  # Production-Like Performance Thresholds
  RESPONSE_TIME_THRESHOLD: 2000    # 2 seconds
  CRITICAL_RESPONSE_TIME: 5000     # 5 seconds
  
  # Staging-Specific Settings
  DEBUG_MODE: false
  DETAILED_LOGGING: true
  SKIP_PERFORMANCE_TESTS: false
  SKIP_LOAD_TESTS: false
  
  # Feature Flags (Staging mirrors production + approved features)
  ENABLE_NEW_FEATURES: true
  ENABLE_EXPERIMENTAL_FEATURES: false  # Only stable features
  ENABLE_DEBUG_ENDPOINTS: false
  
  # Staging Data
  USE_TEST_DATA: false
  USE_PRODUCTION_LIKE_DATA: true
  RESET_DATA_BEFORE_TESTS: false

```

**config/environments/production.yml:**
```yaml
vars: &env_vars
  <<: *base_vars

  # Environment Identification
  ENVIRONMENT: "production"
  ENVIRONMENT_COLOR: "🔴"
  
  # API Configuration
  API_BASE_URL: "https://api.example.com"
  WEB_BASE_URL: "https://web.example.com"
  
  # Strict Performance Thresholds
  RESPONSE_TIME_THRESHOLD: 1500    # 1.5 seconds
  CRITICAL_RESPONSE_TIME: 3000     # 3 seconds
  
  # Production Settings
  DEBUG_MODE: false
  DETAILED_LOGGING: false
  SKIP_PERFORMANCE_TESTS: false
  SKIP_LOAD_TESTS: true            # Avoid impacting production
  
  # Feature Flags (Production only has stable, approved features)
  ENABLE_NEW_FEATURES: false
  ENABLE_EXPERIMENTAL_FEATURES: false
  ENABLE_DEBUG_ENDPOINTS: false
  
  # Production Data
  USE_TEST_DATA: false
  USE_PRODUCTION_DATA: true
  RESET_DATA_BEFORE_TESTS: false
  
  # Production-Specific Monitoring
  ENABLE_REAL_USER_MONITORING: true
  ENABLE_ERROR_TRACKING: true

```

## Step 2: Basic Environment Health Checks

Create comprehensive health validation:

**workflows/health-check.yml:**
```yaml fragment
name: "Environment Health Check"
description: "Comprehensive health validation for the environment given by the merged config"

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

## Step 3: Deployment Validation Workflow

Create comprehensive deployment verification:

**workflows/deployment-validation.yml:**
```yaml fragment
name: "Deployment Validation"
description: "Validate deployment success in the environment given by the merged config"

vars:
  <<: *env_vars

  # Deployment metadata (passed from CI/CD)
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
      # In real scenario, this would trigger concurrent requests

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

## Step 4: Cross-Environment Consistency Checks

Create workflows to validate consistency across environments:

**workflows/consistency-check.yml:**
```yaml
name: "Cross-Environment Consistency Check"
description: "Validate consistency between environments"

vars:
  # Define environments to compare
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

## Step 5: Automated Deployment Pipeline Integration

Create scripts to integrate with your CI/CD pipeline:

**scripts/deploy-validation.sh:**
```bash
#!/bin/bash

set -e

# Configuration
ENVIRONMENT=${1:-staging}
BUILD_VERSION=${2:-$(git rev-parse --short HEAD)}
DEPLOYMENT_ID=${3:-$(date +%s)}

# Validate parameters
if [[ ! "$ENVIRONMENT" =~ ^(development|staging|production)$ ]]; then
    echo "❌ Invalid environment: $ENVIRONMENT"
    echo "Usage: $0 <environment> [build_version] [deployment_id]"
    exit 1
fi

echo "🚀 Starting deployment validation for $ENVIRONMENT environment"
echo "📦 Build Version: $BUILD_VERSION"
echo "🆔 Deployment ID: $DEPLOYMENT_ID"

# Export environment variables
export ENVIRONMENT=$ENVIRONMENT
export BUILD_VERSION=$BUILD_VERSION
export DEPLOYMENT_ID=$DEPLOYMENT_ID
export DEPLOYMENT_TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Set environment-specific variables
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

# Load environment-specific secrets
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

# Common environment variables
export PROBE_OUTPUT=${PROBE_OUTPUT:-auto}

echo "✅ Environment setup complete for: $ENVIRONMENT"
```

## Step 6: GitHub Actions Integration

Create a comprehensive CI/CD workflow:

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
          
          # Add deployment status for each environment
          echo "### Deployment Status" >> $GITHUB_STEP_SUMMARY
          echo "" >> $GITHUB_STEP_SUMMARY
          
          # This would be populated by the actual test results
          echo "| Environment | Status | Tests | Performance |" >> $GITHUB_STEP_SUMMARY
          echo "|-------------|--------|-------|-------------|" >> $GITHUB_STEP_SUMMARY
          echo "| Development | ✅ Success | ✅ Passed | ✅ Good |" >> $GITHUB_STEP_SUMMARY
          echo "| Staging | ✅ Success | ✅ Passed | ✅ Good |" >> $GITHUB_STEP_SUMMARY
          echo "| Production | ${{ needs.deploy-and-test.result == 'success' && '✅ Success' || '❌ Failed' }} | Results | Performance |" >> $GITHUB_STEP_SUMMARY
```

## Step 7: Running Multi-Environment Tests

Execute your comprehensive multi-environment testing:

```bash
# Test individual environments
probe config/base.yml,config/environments/development.yml,workflows/health-check.yml
probe config/base.yml,config/environments/staging.yml,workflows/deployment-validation.yml
probe config/base.yml,config/environments/production.yml,workflows/health-check.yml

# Run full deployment validation
./scripts/deploy-validation.sh staging v2.1.0
./scripts/deploy-validation.sh production v2.1.0

# Run consistency checks
probe config/base.yml,workflows/consistency-check.yml

# Run all environments in sequence
for env in development staging production; do
    echo "Testing $env environment..."
    probe config/base.yml,config/environments/${env}.yml,workflows/health-check.yml
done
```

## Step 8: Advanced Multi-Environment Patterns

Deployment strategies that run two versions side by side need the tests to know which one they are talking to.

### Blue-Green Deployment Testing

Create tests for blue-green deployments:

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
        time: rt.sec * 1000
        
    - name: "Test Green Environment"
      id: green-test
      uses: http
      with:
        method: GET
        url: "{{vars.GREEN_URL}}/health"
      test: res.code == 200
      outputs:
        time: rt.sec * 1000
        
    - name: "Compare Environment Performance"
      uses: hello
      echo: |
        Blue Environment: {{outputs['blue-test'].time}}ms
        Green Environment: {{outputs['green-test'].time}}ms
        Performance Difference: {{abs(outputs['blue-test'].time - outputs['green-test'].time)}}ms
```

### Canary Deployment Testing

Monitor canary deployments:

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

## Troubleshooting

If the workflows above do not run as described, the causes below are the ones to check first.

### Common Issues

**Environment Configuration Mismatch:**
```bash
# Validate configuration before deployment
probe config/base.yml,config/environments/staging.yml --validate-only
```

**Version Drift Detection:**
```bash
# Check version consistency across environments
probe workflows/consistency-check.yml --verbose
```

**Performance Regression:**
```bash
# Compare performance across environments
probe config/base.yml,config/environments/production.yml,workflows/performance-validation.yml
```

## Next Steps

Your multi-environment deployment testing system is complete! Consider these enhancements:

1. **Advanced Monitoring** - Add business metrics validation
2. **Automated Rollback** - Trigger rollbacks on failure
3. **Progressive Deployment** - Implement progressive rollout testing
4. **Compliance Testing** - Add security and compliance validation
5. **Performance Benchmarking** - Historical performance comparison

## Related Resources

- **[First Monitoring System Tutorial](/guide/tutorials/first-monitoring-system)** - Basic monitoring setup
- **[API Testing Pipeline Tutorial](/guide/tutorials/api-testing-pipeline)** - Comprehensive API testing
- **[How-tos: Monitoring Workflows](/guide/how-tos/monitoring-workflows)** - Advanced monitoring patterns
- **[How-tos: Environment Management](/guide/how-tos/environment-management)** - Environment configuration strategies
