# Data Flow

Data flow is the mechanism by which information moves through Probe workflows. Understanding data flow patterns enables you to build sophisticated workflows that pass information between steps, jobs, and even external systems. This guide explores the complete data flow system in Probe.

## Data Flow Overview

Probe uses a structured approach to data flow:

1. **Input Sources**: Environment variables, configuration files, user inputs
2. **Processing**: Actions generate responses and outputs
3. **Storage**: Outputs are stored for later use
4. **Propagation**: Data flows between steps and jobs
5. **Consumption**: Other steps use the data for dynamic configuration

## Data Sources

The values a workflow works with come from outside it, from the environment and from the configuration files it was given.

### Environment Variables

Environment variables provide external configuration and runtime context.

```yaml
# Access environment variables
steps:
  - name: Environment-based Configuration
    uses: http
    with:
      method: GET
      url: "{{vars.API_BASE_URL}}/{{vars.API_VERSION}}/users"
      headers:
        Authorization: "Bearer {{vars.API_TOKEN}}"
        X-Environment: "{{vars.DEPLOYMENT_ENV}}"
    test: res.code == 200
```

### Configuration Merging

Data can come from merged configuration files:

**vars.yml:**
```yaml
vars:
  api_base_url: "{{API_BASE_URL ?? 'https://api.example.com'}}"
  api_token: "{{API_TOKEN}}"
```

**production.yml:**
```yaml
vars:
  api_base_url: https://api.production.example.com
  api_token: "{{PROD_API_TOKEN}}"
```

Usage:
```bash
probe workflow.yml,production.yml
```

A top-level key defined in more than one file takes the value from the last file, and the key is replaced as a whole. See [File Merging](/guide/concepts/file-merging) for the details.

## Step Outputs

Steps generate outputs that can be consumed by subsequent steps and jobs.

### Basic Output Definition

`outputs` names the values a step publishes and the expressions that produce them.

```yaml
steps:
  - name: User Authentication
    id: auth
    uses: http
    with:
      url: "{{vars.API_URL}}/auth/login"
      method: POST
      body: |
        {
          "username": "{{vars.USERNAME}}",
          "password": "{{vars.PASSWORD}}"
        }
    test: res.code == 200
    outputs:
      access_token: res.body.access_token
      refresh_token: res.body.refresh_token
      user_id: res.body.user.id
      expires_at: res.body.expires_at
      user_roles: res.body.user.roles
```

### Output Data Types

Outputs can contain various data types:

```yaml
- name: Comprehensive Data Collection
  id: data-collection
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/comprehensive-data"
  test: res.code == 200
  outputs:
    # Simple values
    user_count: res.body.stats.user_count
    server_version: res.body.version
    is_healthy: res.body.health.status == "healthy"

    # Complex objects
    user_profile: res.body.user
    configuration: res.body.config
    metrics: res.body.metrics

    # Arrays
    active_users: 'filter(res.body.users, #.active == true)'
    error_codes: 'map(res.body.errors, #.code)'

    # Computed values
    success_rate: (res.body.successful_requests / res.body.total_requests) * 100
    avg_response_time: sum(res.body.response_times) / len(res.body.response_times)

    # Response metadata
    response_time: (rt.sec * 1000)
    response_size: len(res.rawbody ?? res.body)
    content_type: res.headers["Content-Type"]
```

### Output Scoping

Outputs are scoped to their containing step and can be referenced by ID:

```yaml
steps:
  - name: Database Setup
    id: db-setup
    uses: http
    with:
      method: GET
      url: "{{vars.DB_API}}/initialize"
    outputs:
      db_session_id: res.body.session_id
      db_host: res.body.host
      db_port: res.body.port

  - name: Application Test
    id: app-test
    uses: http
    with:
      method: GET
      url: "{{vars.APP_URL}}/test"
      headers:
        X-DB-Session: "{{outputs['db-setup'].db_session_id}}"
        X-DB-Host: "{{outputs['db-setup'].db_host}}"
    outputs:
      test_result: res.body.result
      test_duration: (rt.sec * 1000)

  - name: Performance Analysis
    uses: hello
    echo: |
      Performance Analysis:
      Database: {{outputs['db-setup'].db_host}}:{{outputs['db-setup'].db_port}}
      Test Result: {{outputs['app-test'].test_result}}
      Test Duration: {{outputs['app-test'].test_duration}}ms
```

An output can also be read by its name alone, as `outputs.test_result`. When more than one step publishes the same name, the name belongs to the step that published it first: `outputs.<name>` keeps that step's value, and Probe warns about each later one, whose value is read through its step's id. Steps in a job run in order, so which one comes first is fixed; between jobs that run at the same time it is not, so read a name that such jobs share through the step's id. A name that is also a step's id stays that step's outputs, and a step publishing it is warned the same way.

## Cross-Job Data Flow

Data can flow between jobs through job-level outputs and dependencies.

### Job Dependencies and Data Sharing

`needs` both orders the jobs and makes the earlier job's outputs readable.

```yaml
vars:
  SETUP_API: "{{SETUP_API}}"

jobs:
- id: initialization
  name: System Initialization
  steps:
    - name: Create Test Environment
      id: env-setup
      uses: http
      with:
        url: "{{vars.SETUP_API}}/create-environment"
        method: POST
        body: |
          {
            "environment_name": "test_{{random_str(8)}}",
            "configuration": "standard"
          }
      test: res.code == 201
      outputs:
        environment_id: res.body.environment.id
        environment_name: res.body.environment.name
        database_url: res.body.environment.database_url
        api_endpoint: res.body.environment.api_endpoint

- id: api-tests
  name: API Testing Suite
  needs: [initialization]  # Wait for initialization to complete
  steps:
    - name: Test User API
      id: user-api
      uses: http
      with:
        method: GET
        url: "{{outputs['env-setup'].api_endpoint}}/users"
        headers:
          X-Environment: "{{outputs['env-setup'].environment_id}}"
      test: res.code == 200
      outputs:
        user_count: res.body.total_users
        api_response_time: (rt.sec * 1000)

    - name: Test Database Connectivity
      id: db-check
      uses: http
      with:
        method: GET
        url: "{{outputs['env-setup'].database_url}}/ping"
      test: res.code == 200
      outputs:
        db_response_time: (rt.sec * 1000)

- id: reporting
  name: Test Reporting
  needs: [initialization, api-tests]  # Wait for both jobs
  steps:
    - name: Generate Test Report
      uses: hello
      echo: |
        Test Execution Report
        =====================

        Environment: {{outputs['env-setup'].environment_name}}
        Environment ID: {{outputs['env-setup'].environment_id}}

        API Tests:
        - User Count: {{outputs['user-api'].user_count}}
        - API Response Time: {{outputs['user-api'].api_response_time}}ms

        Database Tests:
        - DB Response Time: {{outputs['db-check'].db_response_time}}ms

        Overall Status: All tests completed successfully

- id: cleanup
  name: Environment Cleanup
  needs: [reporting]  # Run after reporting completes
  steps:
    - name: Destroy Test Environment
      uses: http
      with:
        url: "{{vars.SETUP_API}}/environments/{{outputs['env-setup'].environment_id}}"
        method: DELETE
      test: res.code == 204
```

### Cross-Job Output References

An output of another job is read through the id of the step that published it, as `outputs['step-id'].output_name`, not through the job's id. Here the job and its step share the id `data-collection`:

```yaml
vars:
  API_URL: "{{API_URL}}"

jobs:
- id: data-collection
  name: data-collection
  steps:
    - name: Collect User Data
      id: data-collection
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/stats"
      test: res.code == 200
      outputs:
        total_users: res.body.count
        active_users: res.body.active_count

- id: analysis
  name: analysis
  needs: [data-collection]
  steps:
    - name: Analyze User Metrics
      uses: hello
      echo: |
        User Analysis:
        Total Users: {{outputs['data-collection'].total_users}}
        Active Users: {{outputs['data-collection'].active_users}}
        Activity Rate: {{(outputs['data-collection'].active_users / outputs['data-collection'].total_users) * 100}}%
```

## Advanced Data Flow Patterns

Data does not always move straight from one step to the next. It can be transformed along the way, routed by a condition, or accumulated across several steps.

### Data Transformation Chains

Transform data through multiple steps:

```yaml
vars:
  DATA_API: "{{DATA_API}}"

jobs:
- id: data-processing-pipeline
  name: Data Processing Pipeline
  steps:
    - name: Fetch Raw Data
      id: raw-data
      uses: http
      with:
        method: GET
        url: "{{vars.DATA_API}}/raw-data"
      test: res.code == 200
      outputs:
        raw_records: res.body.records
        total_count: res.body.total
        fetch_time: (rt.sec * 1000)

    - name: Filter Data
      uses: hello
      id: filtered-data
      echo: "Filtering data..."
      outputs:
        # Filter active records
        active_records: "filter(outputs['raw-data'].raw_records, #.status == 'active')"
        active_count: "len(filter(outputs['raw-data'].raw_records, #.status == 'active'))"

    - name: Aggregate Data
      uses: hello
      id: aggregated-data
      echo: "Aggregating data..."
      outputs:
        # Group by category and calculate metrics
        categories: "groupBy(outputs['filtered-data'].active_records, #.category)"
        avg_score: "sum(map(outputs['filtered-data'].active_records, #.score)) / outputs['filtered-data'].active_count"

    - name: Generate Summary
      uses: hello
      echo: |
        Data Processing Summary:

        Raw Records: {{outputs['raw-data'].total_count}}
        Active Records: {{outputs['filtered-data'].active_count}}
        Processing Rate: {{(outputs['filtered-data'].active_count / outputs['raw-data'].total_count) * 100}}%
        Average Score: {{outputs['aggregated-data'].avg_score}}
        Fetch Time: {{outputs['raw-data'].fetch_time}}ms
```

### Conditional Data Flow

Control data flow based on conditions:

```yaml
vars:
  API_URL: "{{API_URL}}"
  PROCESSING_API: "{{PROCESSING_API}}"

jobs:
- id: adaptive-processing
  name: adaptive-processing
  steps:
    - name: Assess Data Quality
      id: quality-check
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/data-quality"
      test: res.code == 200
      outputs:
        quality_score: res.body.quality_score
        has_errors: res.body.error_count > 0
        record_count: res.body.record_count

    - name: Standard Processing
      id: standard-processing
      uses: http
      skipif: outputs['quality-check'].quality_score < 0.8
      with:
        url: "{{vars.PROCESSING_API}}/standard"
        method: POST
        body: |
          {
            "record_count": {{outputs['quality-check'].record_count}},
            "quality_mode": "standard"
          }
      test: res.code == 200
      outputs:
        standard_result: res.body.result
        standard_time: (rt.sec * 1000)

    - name: Enhanced Processing
      id: enhanced-processing
      uses: http
      skipif: outputs['quality-check'].quality_score >= 0.8
      with:
        url: "{{vars.PROCESSING_API}}/enhanced"
        method: POST
        body: |
          {
            "record_count": {{outputs['quality-check'].record_count}},
            "quality_mode": "enhanced",
            "error_correction": true
          }
      test: res.code == 200
      outputs:
        enhanced_result: res.body.result
        enhanced_time: (rt.sec * 1000)
        corrections_applied: res.body.corrections

    - name: Processing Summary
      uses: hello
      echo: |
        Data Processing Complete:

        Quality Score: {{outputs['quality-check'].quality_score}}
        Processing Mode: {{outputs['quality-check'].quality_score >= 0.8 ? "Standard" : "Enhanced"}}

        {{outputs.standard_time != nil ? "Standard Processing Time: " + string(outputs.standard_time) + "ms" : ""}}
        {{outputs.enhanced_time != nil ? "Enhanced Processing Time: " + string(outputs.enhanced_time) + "ms" : ""}}
        {{outputs.corrections_applied != nil ? "Corrections Applied: " + string(outputs.corrections_applied) : ""}}
```

### Data Accumulation Patterns

Collect data from multiple sources:

```yaml
vars:
  SOURCE_A_URL: "{{SOURCE_A_URL}}"
  SOURCE_B_URL: "{{SOURCE_B_URL}}"
  SOURCE_C_URL: "{{SOURCE_C_URL}}"

jobs:
- id: multi-source-data-collection
  name: multi-source-data-collection
  steps:
    - name: Source A Data
      id: source-a
      uses: http
      with:
        method: GET
        url: "{{vars.SOURCE_A_URL}}/data"
      test: res.code == 200
      outputs:
        source_a_count: res.body.count
        source_a_data: res.body.data
        source_a_time: (rt.sec * 1000)

    - name: Source B Data
      id: source-b
      uses: http
      with:
        method: GET
        url: "{{vars.SOURCE_B_URL}}/data"
      test: res.code == 200
      outputs:
        source_b_count: res.body.count
        source_b_data: res.body.data
        source_b_time: (rt.sec * 1000)

    - name: Source C Data
      id: source-c
      uses: http
      with:
        method: GET
        url: "{{vars.SOURCE_C_URL}}/data"
      test: res.code == 200
      outputs:
        source_c_count: res.body.count
        source_c_data: res.body.data
        source_c_time: (rt.sec * 1000)

    - name: Aggregate All Sources
      uses: hello
      echo: |
        Multi-Source Data Summary:

        Source A: {{outputs['source-a'].source_a_count}} records ({{outputs['source-a'].source_a_time}}ms)
        Source B: {{outputs['source-b'].source_b_count}} records ({{outputs['source-b'].source_b_time}}ms)
        Source C: {{outputs['source-c'].source_c_count}} records ({{outputs['source-c'].source_c_time}}ms)

        Total Records: {{outputs['source-a'].source_a_count + outputs['source-b'].source_b_count + outputs['source-c'].source_c_count}}
        Average Response Time: {{(outputs['source-a'].source_a_time + outputs['source-b'].source_b_time + outputs['source-c'].source_c_time) / 3}}ms

        Fastest Source: {{
          outputs['source-a'].source_a_time <= outputs['source-b'].source_b_time && outputs['source-a'].source_a_time <= outputs['source-c'].source_c_time ? "Source A" :
          outputs['source-b'].source_b_time <= outputs['source-c'].source_c_time ? "Source B" : "Source C"
        }}
```

## Data Validation and Quality

An output that is empty or malformed breaks the step that reads it, not the step that produced it. Validating and sanitising outputs keeps that from happening.

### Output Validation

Ensure data quality in outputs:

```yaml
- name: Data Collection with Validation
  id: validated-data
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/user-data"
  test: |
    res.code == 200 &&
    res.body.users != nil &&
    len(res.body.users) > 0 &&
    all(res.body.users, #.id != nil && #.email != nil)
  outputs:
    # Validated outputs
    user_count: len(res.body.users)
    valid_users: 'filter(res.body.users, #.id != nil && #.email != nil)'
    admin_users: 'filter(res.body.users, #.role == "admin")'

    # Data quality metrics
    data_completeness: 'len(filter(res.body.users, #.id != nil && #.email != nil)) / len(res.body.users)'
    has_admin_users: 'any(res.body.users, #.role == "admin")'

    # Response metadata
    data_freshness: res.headers["Last-Modified"]
    cache_status: res.headers["X-Cache-Status"]
```

### Data Sanitization

Clean and sanitize data before use:

```yaml
- name: Sanitize User Input
  id: sanitized-input
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/user-input"
  outputs:
    # Raw data
    raw_input: res.body.input

    # Sanitized data
    clean_email: trim(lower(res.body.input.email))
    clean_name: trim(res.body.input.name)
    safe_description: res.body.input.description[0:500]  # Limit length

    # Validation flags
    email_valid: res.body.input.email matches "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}"
    name_valid: len(res.body.input.name) >= 2 && len(res.body.input.name) <= 50
```

## Performance Considerations

Outputs are held for the length of the run, so what a step stores and how later steps reach it both matter.

### Efficient Data Access

Optimize data access patterns:

```yaml
# Good: Direct property access
outputs:
  user_id: res.body.user.id
  user_name: res.body.user.name

# Good: Single computation with reuse
outputs:
  active_users: 'filter(res.body.users, #.active == true)'
  active_user_count: 'len(filter(res.body.users, #.active == true))'

# Avoid: Repeated expensive computations
# outputs:
#   user_count: len(filter(res.body.users, expensive_validation(#)))
#   user_list: filter(res.body.users, expensive_validation(#))
```

### Memory Management

Be mindful of large data sets:

```yaml
# Good: Extract essential data only
outputs:
  user_ids: 'map(res.body.users, #.id)'
  user_count: len(res.body.users)
  first_user: res.body.users[0]

# Avoid: Storing large objects unnecessarily
# outputs:
#   all_user_data: res.body.users  # Could be very large
#   complete_response: res.body     # Entire response body
```

### Selective Data Extraction

Extract only needed data:

```yaml
- name: Efficient Data Extraction
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/large-dataset"
  outputs:
    # Extract summary information only
    record_count: res.body.metadata.total_records
    last_updated: res.body.metadata.last_updated
    status: res.body.metadata.status

    # Extract specific records by criteria
    critical_items: 'filter(res.body.data, #.priority == "critical")'
    error_items: 'filter(res.body.data, #.status == "error")'

    # Compute aggregates
    avg_score: 'sum(map(res.body.data, #.score)) / len(res.body.data)'
    max_score: 'max(map(res.body.data, #.score))'

    # Don't store the entire dataset
    # full_dataset: res.body.data  # Avoid this for large datasets
```

## Best Practices

The points below cover naming outputs, keeping their types stable, reading them safely, and recording which step depends on which.

### 1. Clear Output Naming

Use descriptive names for outputs:

```yaml
# Good: Descriptive names
outputs:
  user_authentication_token: res.body.access_token
  session_expiry_timestamp: res.body.expires_at
  user_permission_level: res.body.user.role

# Avoid: Generic names
outputs:
  token: res.body.access_token
  time: res.body.expires_at
  level: res.body.user.role
```

### 2. Type-Consistent Outputs

Maintain consistent data types:

```yaml
# Good: Consistent types
outputs:
  user_count: len(res.body.users)          # Always number
  is_admin: res.body.user.role == "admin"    # Always boolean
  user_email: res.body.user.email ?? ""      # Always string (with default)

# Avoid: Inconsistent types
outputs:
  user_count: res.body.user_count ?? "unknown"  # Number or string
```

### 3. Error-Safe Data Access

Handle potential null/undefined values:

```yaml
# Good: Safe data access
outputs:
  user_id: 'res.body.user != nil ? res.body.user.id : nil'
  email_verified: res.body.user != nil && res.body.user.email_verified == true
  profile_complete: res.body.user?.profile != nil && res.body.user.profile.complete == true

# Good: Using safe navigation
test: res.body.user?.id != nil && res.body.user?.email != nil
```

### 4. Document Data Dependencies

Document what data flows where:

```yaml
vars:
  api_url: "{{API_URL}}"
  smtp_addr: "{{SMTP_ADDR}}"

jobs:
- id: user-setup
  name: User Account Setup
  steps:
    - name: Create User Account
      id: user-setup
      uses: http
      with:
        method: POST
        url: "{{vars.api_url}}/users"
        body: |
          {"username": "probe_{{random_str(8)}}"}
      test: res.code == 201
      # Produces: user_id, username, email
      outputs:
        user_id: res.body.user.id
        username: res.body.user.username
        email: res.body.user.email

- id: user-verification
  name: User Account Verification
  needs: [user-setup]
  steps:
    - name: Send Verification Email
      # Consumes: user_id, email from user-setup
      uses: smtp
      with:
        addr: "{{vars.smtp_addr}}"
        from: "probe@example.com"
        to: "{{outputs['user-setup'].email}}"
        subject: "Verify your account"
        session: 1
        message: 1
        length: 500
      test: res.code == 0
      echo: "Click here to verify user {{outputs['user-setup'].user_id}}"
```

## What's Next?

Now that you understand data flow, explore:

1. **[Testing and Assertions](/guide/concepts/testing-and-assertions)** - Learn validation techniques
2. **[Error Handling](/guide/concepts/error-handling)** - Handle data flow failures gracefully  
3. **[How-tos](/guide/how-tos/api-testing)** - See practical data flow patterns

Data flow is the circulatory system of your Probe workflows. Master these patterns to build sophisticated, data-driven automation processes.
