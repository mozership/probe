# Testing and Assertions

Testing and assertions are the quality gates of Probe workflows. They validate that your actions produce expected results and ensure system reliability. This guide explores test expressions, assertion patterns, and strategies for building robust validation into your workflows.

## Testing Fundamentals

Every step in Probe can include a `test` condition that validates the action's result. Tests are boolean expressions that determine whether a step succeeded or failed.

### Basic Test Structure

A test is one expression on the step, and the step passes when it evaluates to true.

```yaml
- name: API Health Check
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/health"
  test: res.code == 200
```

The test expression evaluates the response (`res`) and returns true for success or false for failure.

### What a Failed Test Shows

A test usually joins several conditions with `&&`. When it is false, the report names the first condition that did not hold, and what each side of it evaluated to, before the request and the response:

```yaml
test: |
  res.code == 200 &&
  res.body.status == "healthy"
```

```
⏺ Health (Failed in 0.12s)
  ⎿ 0. ✗  API Health Check
           failed: res.body.status == "healthy"
             res.body.status = "degraded"
           request:
             headers:
               Accept: */*
               User-Agent: probe-http/1.0.0
             method: GET
             timeout: 30s
             url: https://api.example.com/health
           response:
             body:
               status: degraded
             code: 200
             headers:
               Content-Type: application/json
             status: 200 OK
```

The condition is shown as Probe read it, so its spacing and quotes may differ from the workflow's. The values are shown for a comparison, such as `==`, `<`, `in` or `contains`, and for what a `!` negates; a value written in the test itself, such as `"healthy"`, is not repeated. Conditions joined with `||` are one condition, and are shown together. A test that starts with `let` is taken apart after what it declares, and each condition reads it. A test that calls a function whose value changes from one call to the next, `random_int`, `random_str`, `unixtime` or `now`, is not taken apart, as evaluating it again could name another condition; it shows the request and the response only. The same line is the message of the failure in the files `--report` writes, as `test evaluated to false: res.body.status == "healthy" (res.body.status = "degraded")`.

The request and the response are laid out key by key, in the order of their names, as `--verbose` shows them. Their own fields that are empty, such as a body a `GET` did not send, are left out; a zero or a `false` is kept, and what a field holds, such as the keys of a JSON body or the columns of a row, is shown whole, empty values and all. A value is cut after 512 bytes or 20 lines, and a map or a list after 30 entries, with how much was cut, as `… (1234 more bytes)` or `… (80 more lines)`, so that a large body cannot bury why the step failed; `--verbose` shows them whole. A text of several lines, such as the output of a command, is written under its name, a line at a time, after `|`, or `|-` when it does not end in a newline. A text of one line is quoted when it could be read as something else: `"1"` is a string, `1` a number, and `""` is empty. Binary data is shown as its size only. Secrets are hidden in both, as everywhere else. An action whose response sets `dump` to `false` keeps both out.

### Test Expression Context

Test expressions have access to comprehensive response data:

```yaml
# HTTP Response Testing Context
test: |
  res.code == 200 &&           # HTTP status code
  (rt.sec * 1000) < 1000 &&             # Response time in milliseconds
  res.body_size > 0 &&           # Response body size in bytes
  res.headers["Content-Type"] == "application/json" &&  # Response headers
  res.body.status == "healthy" && # Parsed JSON response
  res.body contains "success"   # Response body as text
```

## HTTP Response Testing

An HTTP response offers four things worth asserting on: the status code, how long it took, how much came back, and the headers.

### Status Code Validation

`res.code` can be compared to a single value, to a range, or to a set of codes that are all acceptable.

```yaml
# Exact status code
test: res.code == 200

# Status code ranges
test: res.code >= 200 && res.code < 300

# Multiple acceptable codes
test: res.status in [200, 201, 202]

# Client vs server errors
test: res.code < 400  # Success or redirect
test: res.code >= 400 && res.code < 500  # Client error
test: res.code >= 500  # Server error
```

### Response Time Testing

`rt.sec` holds the round trip in seconds, so a time budget is written as an ordinary comparison.

```yaml
# Performance validation
test: (rt.sec * 1000) < 1000                    # Must respond within 1 second
test: (rt.sec * 1000) >= 100 && (rt.sec * 1000) <= 500  # Response time range
test: (rt.sec * 1000) < {{vars.MAX_RESPONSE_TIME || 2000}}  # Configurable threshold

# Performance categories
test: |
  res.code == 200 && (
    (rt.sec * 1000) < 200 ? "excellent" :
    (rt.sec * 1000) < 500 ? "good" :
    (rt.sec * 1000) < 1000 ? "acceptable" : "poor"
  ) != "poor"
```

### Response Size Validation

`res.body_size` catches an empty body and a response that came back far larger than expected.

```yaml
# Content presence
test: res.body_size > 0                    # Has content
test: res.body_size > 100                  # Minimum content size
test: res.body_size < 1048576             # Maximum 1MB response

# Size-based validation
test: |
  res.code == 200 &&
  res.body_size > 50 &&                   # Not empty error message
  res.body_size < 100000                  # Not unexpectedly large
```

### Header Validation

`res.headers` is indexed by header name, which covers content type and the security headers a response is expected to carry.

```yaml
# Content type checking
test: res.headers["Content-Type"] == "application/json"
test: res.headers["Content-Type"] startsWith "text/"
test: res.headers["Content-Type"] contains "charset=utf-8"

# Security headers
test: |
  "X-Frame-Options" in res.headers &&
  "X-Content-Type-Options" in res.headers &&
  res.headers["X-Frame-Options"] == "DENY"

# Cache control
test: res.headers["Cache-Control"] contains "no-cache"

# Rate limiting
test: res.headers["X-Rate-Limit-Remaining"] > "10"

# Custom headers
test: |
  "X-Request-Id" in res.headers &&
  len(res.headers["X-Request-Id"]) == 36  # UUID format
```

## JSON Response Testing

When the body is JSON, Probe parses it, so a test can address fields directly instead of matching text.

### Basic JSON Validation

Once the body is parsed, a test walks into it by field name.

```yaml
# JSON structure validation
test: |
  res.code == 200 &&
  res.body != null &&
  res.body.status == "success" &&
  res.body.data != null

# Required fields presence
test: |
  "id" in res.body &&
  "name" in res.body &&
  "email" in res.body &&
  "created_at" in res.body
```

### Data Type Validation

`typeof` asserts that a field came back as the type the client expects, not merely that it is present.

```yaml
# Type checking
test: |
  typeof(res.body.id) == "number" &&
  typeof(res.body.name) == "string" &&
  typeof(res.body.active) == "boolean" &&
  typeof(res.body.tags) == "array" &&
  typeof(res.body.metadata) == "object"

# Value constraints
test: |
  res.body.id > 0 &&
  len(res.body.name) >= 2 &&
  res.body.score >= 0 && res.body.score <= 100
```

### Array and Collection Testing

`len` gives the number of elements, and elements are addressed by index to check their contents.

```yaml
# Array validation
test: |
  res.body.users != null &&
  len(res.body.users) > 0 &&
  len(res.body.users) <= 100

# Array content validation
test: |
  all(res.body.users, #.id != null && 
    #.email != null)

# Specific element checks
test: |
  any(res.body.users, #.role == "admin") &&
  len(filter(res.body.users, #.active == true)) > 0

# Array uniqueness
test: |
  len(res.body.user_ids) == len(uniq(res.body.user_ids))
```

### Nested Data Validation

Reaching a nested field means every level above it has to exist, so each one is checked on the way down.

```yaml
# Deep object validation
test: |
  res.body.user != null &&
  res.body.user.profile != null &&
  res.body.user.profile.preferences != null &&
  res.body.user.profile.preferences.notifications == true

# Complex nested structures
test: |
  all(res.body.data.orders, #.id != null &&
    len(#.items) > 0 &&
    #all(.items, #.product_id != null && 
      #.quantity > 0 && 
      #.price > 0) &&
    #.total == sum(map(#.items, #.quantity * #.price)))
```

## Text Response Testing

When the body is not JSON, it is a string. Tests on it work by substring, by regular expression, or by length.

### Pattern Matching

`contains`, `startsWith` and `endsWith` work on the body as a string, and `lower` makes the comparison case-insensitive.

```yaml
# Simple text matching
test: res.body contains "success"
test: res.body startsWith "<!DOCTYPE html>"
test: res.body endsWith "</html>"

# Case-insensitive matching
test: lower(res.body) contains "error"

# Multiple patterns
test: |
  res.body contains "status" &&
  res.body contains "healthy" &&
  !res.body contains "error"
```

### Regular Expression Testing

`matches` applies a regular expression, which is what to reach for when the value has a format rather than a fixed text.

```yaml
# Email validation in response
test: res.body matches "user-\\d+@example\\.com"

# URL pattern validation
test: res.body matches "https://[a-zA-Z0-9.-]+/api/v\\d+/"

# Data format validation
test: |
  res.body matches "\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}Z"  # ISO timestamp

# Extract and validate data
test: |
  res.body matches "Version: v\\d+\\.\\d+\\.\\d+" &&
  res.body.extract("v(\\d+)\\.(\\d+)\\.(\\d+)")[1] >= "2"  # Major version >= 2
```

### Content Length and Quality

`len` on the body bounds how much came back, and the same expression can assert that the content is not an error page.

```yaml
# Content length validation
test: |
  len(res.body) > 100 &&
  len(res.body) < 10000

# Content quality checks
test: |
  len(split(res.body, "\\n")) > 5 &&           # Multi-line content
  !res.body contains "Lorem ipsum" &&          # Not placeholder text
  len(split(res.body, " ")) > 20               # Substantial content
```

## Advanced Testing Patterns

Some checks cannot be written against a single response: they depend on what the run is doing, on an earlier step, or on a rule the application is supposed to enforce.

### Conditional Testing

A test is an expression, so it can pick its own threshold from the environment it is running against.

```yaml
# Environment-specific tests
test: |
  res.code == 200 &&
  (vars.NODE_ENV == "development" ? 
    (rt.sec * 1000) < 5000 :           # More lenient for dev
    (rt.sec * 1000) < 1000             # Strict for production
  )

# Feature flag testing
test: |
  res.code == 200 &&
  (res.body.features.beta_enabled == true ?
    res.body.beta_data != null :    # Beta features should have data
    res.body.beta_data == null      # Beta features should be absent
  )
```

### Cross-Step Validation

A step can assert against an earlier step's outputs, which is how two endpoints are checked for agreement.

```yaml
jobs:
- id: data-consistency-test
  name: data-consistency-test
  steps:
    - name: Get User Count
      id: user-count
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/count"
      test: res.code == 200
      outputs:
        total_users: res.body.count

    - name: Get User List
      id: user-list
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users"
      test: |
        res.code == 200 &&
        len(res.body.users) == outputs['user-count'].total_users  # Consistency check
      outputs:
        user_list: res.body.users

    - name: Validate User Data Integrity
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/{{outputs['user-list'].user_list[0].id}}"
      test: |
        res.code == 200 &&
        res.body.user.id == outputs['user-list'].user_list[0].id &&
        res.body.user.email == outputs['user-list'].user_list[0].email
```

### Business Logic Testing

Some rules hold between fields rather than in any one of them, such as a total that has to match its line items.

```yaml
- name: E-commerce Business Logic Test
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/orders/{{vars.TEST_ORDER_ID}}"
  test: |
    res.code == 200 &&
    res.body.order != null &&
    
    # Order total equals sum of line items
    res.body.order.total == 
      sum(map(res.body.order.line_items, #.quantity * #.price)) &&
    
    # Tax calculation is correct (assuming 8% tax rate)
    res.body.order.tax_amount == 
      round(Math) / 100 &&
    
    # Shipping is applied correctly
    (res.body.order.subtotal >= 100 ? 
      res.body.order.shipping_cost == 0 :     # Free shipping over $100
      res.body.order.shipping_cost == 9.99    # Standard shipping
    ) &&
    
    # Final total calculation
    res.body.order.total == 
      res.body.order.subtotal + res.body.order.tax_amount + res.body.order.shipping_cost
```

## Error Testing and Negative Cases

A workflow that only exercises the successful path leaves the error handling untested. The tests below assert that bad input is rejected the way it should be.

### Expected Error Scenarios

Here the failing response is the expected one, so the test asserts on the error code and the message that comes with it.

```yaml
- name: Test Invalid Authentication
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/protected"
    headers:
      Authorization: "Bearer invalid-token"
  test: |
    res.code == 401 &&
    res.body.error == "invalid_token" &&
    res.body.message contains "authentication"

- name: Test Rate Limiting
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/rate-limited-endpoint"
  test: |
    res.status in [200, 429] &&  # Either success or rate limited
    (res.code == 429 ? 
      "Retry-After" in res.headers && 
      res.body.error == "rate_limit_exceeded" :
      res.body.status == "success"
    )

- name: Test Malformed Request
  uses: http
  with:
    url: "{{vars.API_URL}}/users"
    method: POST
    body: '{"invalid": json}'  # Intentionally malformed
  test: |
    res.code == 400 &&
    res.body.error contains "json" &&
    res.body.details != null
```

### Boundary Testing

Input at the edge of what is allowed is where validation tends to break, so it is sent deliberately.

```yaml
- name: Test Input Boundaries
  uses: http
  with:
    url: "{{vars.API_URL}}/users"
    method: POST
    body: |
      {
        "name": "{{random_str(255)}}",  # Maximum length
        "age": 150,                     # Upper boundary
        "score": 0                      # Lower boundary
      }
  test: |
    res.status in [201, 400] &&  # Either created or validation error
    (res.code == 400 ? 
      res.body.validation_errors != null :
      res.body.user.id != null
    )
```

## Test Organization Patterns

As a workflow grows, the question becomes what to check where. The patterns below layer the checks and group them into suites.

### Layered Testing Strategy

Splitting the checks into jobs lets the cheap ones run first and stop the run before the expensive ones start.

```yaml
jobs:
- id: smoke-tests
  name: Smoke Tests
  steps:
    - name: Basic Connectivity
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/ping"
      test: res.code == 200

- id: functional-tests
  name: Functional Tests
  needs: [smoke-tests]
  steps:
    - name: User Management
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users"
      test: |
        res.code == 200 &&
        res.body.users != null &&
        res.body.pagination != null

- id: integration-tests
  name: Integration Tests
  needs: [functional-tests]
  steps:
    - name: Cross-Service Integration
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/integration/full-flow"
      test: |
        res.code == 200 &&
        res.body.all_services_connected == true &&
        res.body.data_consistency_check == true
```

### Comprehensive Test Suites

A full suite groups the cases by what they cover and carries the credentials obtained at the start through the rest of the run.

```yaml
jobs:
- id: api-test-suite
  name: Comprehensive API Test Suite
  steps:
    # Authentication Tests
    - name: Valid Login
      id: login
      uses: http
      with:
        url: "{{vars.API_URL}}/auth/login"
        method: POST
        body: |
          {
            "username": "{{vars.TEST_USERNAME}}",
            "password": "{{vars.TEST_PASSWORD}}"
          }
      test: |
        res.code == 200 &&
        res.body.access_token != null &&
        res.body.refresh_token != null &&
        res.body.expires_in > 0
      outputs:
        access_token: res.body.access_token

    - name: Invalid Login
      uses: http
      with:
        url: "{{vars.API_URL}}/auth/login"
        method: POST
        body: |
          {
            "username": "invalid",
            "password": "wrong"
          }
      test: |
        res.code == 401 &&
        res.body.error == "invalid_credentials"

    # CRUD Operations Tests
    - name: Create User
      id: create-user
      uses: http
      with:
        url: "{{vars.API_URL}}/users"
        method: POST
        headers:
          Authorization: "Bearer {{outputs.login.access_token}}"
        body: |
          {
            "name": "Test User {{random_str(6)}}",
            "email": "test{{random_str(8)}}@example.com",
            "role": "user"
          }
      test: |
        res.code == 201 &&
        res.body.user.id != null &&
        res.body.user.name != null &&
        res.body.user.email != null
      outputs:
        user_id: res.body.user.id
        user_email: res.body.user.email

    - name: Read User
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/{{outputs['create-user'].user_id}}"
        headers:
          Authorization: "Bearer {{outputs.login.access_token}}"
      test: |
        res.code == 200 &&
        res.body.user.id == outputs['create-user'].user_id &&
        res.body.user.email == "{{outputs['create-user'].user_email}}"

    - name: Update User
      uses: http
      with:
        url: "{{vars.API_URL}}/users/{{outputs['create-user'].user_id}}"
        method: PUT
        headers:
          Authorization: "Bearer {{outputs.login.access_token}}"
        body: |
          {
            "name": "Updated Test User"
          }
      test: |
        res.code == 200 &&
        res.body.user.name == "Updated Test User"

    - name: Delete User
      uses: http
      with:
        url: "{{vars.API_URL}}/users/{{outputs['create-user'].user_id}}"
        method: DELETE
        headers:
          Authorization: "Bearer {{outputs.login.access_token}}"
      test: res.code == 204

    - name: Verify Deletion
      uses: http
      with:
        method: GET
        url: "{{vars.API_URL}}/users/{{outputs['create-user'].user_id}}"
        headers:
          Authorization: "Bearer {{outputs.login.access_token}}"
      test: res.code == 404
```

## Performance Testing

`res.time` makes response time an ordinary assertion, so a workflow can hold a service to a time budget as well as a correctness one.

### Response Time Benchmarks

A benchmark pairs the correctness assertion with the time it is allowed to take.

```yaml
- name: Performance Benchmark Test
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/performance-test"
  test: |
    res.code == 200 &&
    
    # Tiered performance expectations
    (vars.NODE_ENV == "production" ? 
      (rt.sec * 1000) < 500 :              # Production: < 500ms
      (rt.sec * 1000) < 2000               # Non-production: < 2s
    ) &&
    
    # Additional performance metrics
    res.body.query_time < 100 &&    # Database query time
    res.body.render_time < 50       # Template render time
  outputs:
    response_time: (rt.sec * 1000)
    query_time: res.body.query_time
    render_time: res.body.render_time
```

### Load Testing Validation

When load is generated elsewhere, Probe reads the results back and asserts on them.

```yaml
- name: Load Test Results Validation
  uses: http
  with:
    method: GET
    url: "{{vars.LOAD_TEST_URL}}/results"
  test: |
    res.code == 200 &&
    res.body.test_completed == true &&
    
    # Success rate requirements
    res.body.success_rate >= 0.95 &&
    
    # Performance percentiles
    res.body.percentiles.p50 < 1000 &&
    res.body.percentiles.p95 < 2000 &&
    res.body.percentiles.p99 < 5000 &&
    
    # Error rate limits
    res.body.error_rate < 0.05 &&
    
    # No critical errors
    res.body.critical_errors == 0
```

## Security Testing

Two things are worth asserting on every API: that it refuses a request without valid credentials, and that it rejects input it should not accept.

### Authentication and Authorization

A request without credentials has to be refused, and the test asserts on that refusal.

```yaml
- name: Test Unauthorized Access
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/admin/users"
  test: |
    res.code == 401 &&
    res.body.error == "authentication_required"

- name: Test Insufficient Permissions
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/admin/users"
    headers:
      Authorization: "Bearer {{vars.USER_TOKEN}}"  # Regular user token
  test: |
    res.code == 403 &&
    res.body.error == "insufficient_permissions"

- name: Test Token Expiration
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/protected"
    headers:
      Authorization: "Bearer {{vars.EXPIRED_TOKEN}}"
  test: |
    res.code == 401 &&
    res.body.error == "token_expired"
```

### Input Validation Security

Hostile input should be rejected or neutralised, and either outcome is acceptable as long as it is not executed.

```yaml
- name: Test SQL Injection Protection
  uses: http
  with:
    method: GET
    url: "{{vars.API_URL}}/users?search='; DROP TABLE users; --"
  test: |
    res.status in [200, 400] &&  # Either filtered or rejected
    !res.body contains "sql" && # No SQL error messages
    !res.body contains "syntax" &&
    res.body.error != "internal_server_error"  # Should not cause server error

- name: Test XSS Protection
  uses: http
  with:
    url: "{{vars.API_URL}}/comments"
    method: POST
    body: |
      {
        "content": "<script>alert('xss')</script>"
      }
  test: |
    res.status in [201, 400] &&
    (res.code == 201 ? 
      !res.body.comment.content contains "<script>" :  # Should be sanitized
      res.body.validation_errors != null               # Or rejected
    )
```

## Test Documentation and Reporting

A test that fails is only useful if the output says what was expected. Naming and aggregation do that work.

### Self-Documenting Tests

The step name and the assertions together should say what the case was, so a failure needs no outside explanation.

```yaml
- name: User Registration Flow Test
  uses: http
  with:
    url: "{{vars.API_URL}}/auth/register"
    method: POST
    body: |
      {
        "email": "{{random_str(8)}}@example.com",
        "password": "TestPass123!",
        "confirm_password": "TestPass123!"
      }
  # Comprehensive test with clear validation points
  test: |
    res.code == 201 &&                                    # 1. Successful creation
    res.body.user.id != null &&                            # 2. User ID assigned
    res.body.user.email != null &&                         # 3. Email stored
    res.body.user.password == null &&                      # 4. Password not returned
    res.body.user.created_at != null &&                    # 5. Timestamp recorded
    res.body.user.email_verified == false &&               # 6. Email unverified initially
    res.body.verification_email_sent == true &&            # 7. Verification triggered
    "Location" in res.headers &&                         # 8. Location header present
    res.headers["Location"] contains "/users/"             # 9. Correct redirect path
  outputs:
    user_id: res.body.user.id
    user_email: res.body.user.email
    test_summary: |
      Registration test completed:
      - User ID: {{res.body.user.id}}
      - Email: {{res.body.user.email}}
      - Verification: {{res.body.verification_email_sent ? "Sent" : "Failed"}}
      - Response time: {{rt.duration}}
```

### Test Result Aggregation

A final job that depends on the others can read their status and print one summary.

```yaml
jobs:
- id: test-summary
  name: Test Results Summary
  needs: [smoke-tests, functional-tests, security-tests]
  steps:
    - name: Generate Test Report
      uses: hello
      echo: |
        Test Execution Summary
        =====================
          
          
        Overall Result: {{
        }}
          
        Execution Time: {{unixtime()}}
        Test Environment: {{vars.NODE_ENV || "development"}}
```

## Best Practices

The points below keep a growing test suite readable and keep its failures informative.

### 1. Clear Test Intentions

A test should say what it expects. Asserting only that something is non-null passes for reasons that have nothing to do with the case.

```yaml
# Good: Specific, testable conditions
test: |
  res.code == 200 &&
  len(res.body.users) >= 1 &&
  res.body.users[0].id != null

# Avoid: Vague or incomplete tests
test: res.code == 200  # What about response content?
```

### 2. Comprehensive Error Coverage

Every case worth testing has a failing counterpart, and both belong in the suite.

```yaml
# Good: Test both success and failure paths
- name: Valid Request Test
  test: res.code == 200 && res.body.success == true

- name: Invalid Request Test  
  test: res.code == 400 && res.body.error != null
```

### 3. Performance-Aware Testing

Adding a time bound to an existing test costs nothing and catches a regression that correctness checks miss.

```yaml
# Good: Include performance validation
test: |
  res.code == 200 &&
  (rt.sec * 1000) < 1000 &&
  res.body.data != null

# Good: Environment-specific performance thresholds
test: |
  res.code == 200 &&
  (rt.sec * 1000) < {{vars.MAX_RESPONSE_TIME || 2000}}
```

### 4. Maintainable Test Expressions

A long condition is easier to read split across lines, one assertion per line.

```yaml
# Good: Readable, well-structured tests
test: |
  res.code == 200 &&
  res.body.user != null &&
  res.body.user.id > 0 &&
  res.body.user.email contains "@"

# Avoid: Complex, hard-to-read tests
test: res.code == 200 && res.body.user != null && res.body.user.id > 0 && res.body.user.email contains "@" && res.body.user.active == true && (rt.sec * 1000) < 1000
```

## What's Next?

Now that you understand testing and assertions, explore:

1. **[Error Handling](/guide/concepts/error-handling)** - Learn to handle failures gracefully
2. **[Execution Model](/guide/concepts/execution-model)** - Understand workflow execution flow
3. **[How-tos](/guide/how-tos/api-testing)** - See practical testing patterns in action

Testing and assertions are your quality gates. Master these concepts to build reliable, robust automation that catches issues before they impact your systems.
