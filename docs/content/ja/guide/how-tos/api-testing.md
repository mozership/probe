# APIテスト

このガイドでは、Probeを使用して包括的なAPIテストワークフローを構築する方法を説明します。REST APIの徹底的なテスト、レスポンスの検証、認証の処理、および高度なテストパターンの実装について学習します。

## 基本的なAPIテスト

APIテストは1つのリクエストとその検証から始め、レコードの作成、取得、更新、削除を順に行う形へ広げます。

### シンプルなGETリクエストテスト

基本的なAPIエンドポイントテストから始めましょう：

```yaml
name: Basic API Test
description: Test a simple REST API endpoint

vars:
  api_base_url: "{{API_BASE_URL ?? 'https://jsonplaceholder.typicode.com'}}"
  timeout: "{{TIMEOUT ?? '30s'}}"

jobs:
- id: basic-api-test
  name: Basic API Test
  defaults:
    http:
      timeout: "{{vars.timeout}}"
      headers:
        Accept: "application/json"
        User-Agent: "Probe API Tester v1.0"
  steps:
    - name: Get Posts
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/posts"
      test: |
        res.code == 200 &&
        res.headers["Content-Type"] contains "application/json" &&
        res.body != nil &&
        len(res.body) > 0
      outputs:
        post_count: len(res.body)
        first_post_id: res.body[0].id
        response_time: (rt.sec * 1000)

    - name: Get Single Post
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/posts/{{outputs.first_post_id}}"
      test: |
        res.code == 200 &&
        res.body.id == outputs.first_post_id &&
        res.body.title != nil &&
        res.body.body != nil
      outputs:
        post_title: res.body.title
        post_body: res.body.body

    - name: Test Results Summary
      uses: hello
      echo: |
        📊 API Test Results:

        Posts Retrieved: {{outputs.post_count}}
        First Post ID: {{outputs.first_post_id}}
        Post Title: "{{outputs.post_title}}"
        Response Time: {{outputs.response_time}}ms

        ✅ Basic API tests completed successfully
```

### CRUD操作のテスト

Create、Read、Update、Delete操作をテストします：

```yaml
name: CRUD API Testing
description: Test complete CRUD operations on a REST API

vars:
  api_base_url: "{{API_BASE_URL ?? 'https://jsonplaceholder.typicode.com'}}"
  test_user_id: "{{TEST_USER_ID ?? '1'}}"

jobs:
- id: crud-operations
  name: CRUD Operations Test
  steps:
    # CREATE - POST Request
    - name: Create New Post
      id: create
      uses: http
      with:
        url: "{{vars.api_base_url}}/posts"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "title": "Test Post {{random_str(6)}}",
            "body": "This is a test post created by Probe at {{unixtime()}}",
            "userId": {{vars.test_user_id}}
          }
      test: |
        res.code == 201 &&
        res.body.id != nil &&
        res.body.title != nil &&
        res.body.userId == int(vars.test_user_id)
      outputs:
        created_post_id: res.body.id
        created_title: res.body.title
        created_body: res.body.body

    # READ - GET Request
    - name: Read Created Post
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/posts/{{outputs.create.created_post_id}}"
      test: |
        res.code == 200 &&
        res.body.id == outputs.create.created_post_id &&
        res.body.title == outputs.create.created_title
      outputs:
        read_success: true

    # UPDATE - PUT Request
    - name: Update Post
      id: update
      uses: http
      with:
        url: "{{vars.api_base_url}}/posts/{{outputs.create.created_post_id}}"
        method: PUT
        headers:
          Content-Type: "application/json"
        body: |
          {
            "id": {{outputs.create.created_post_id}},
            "title": "Updated: {{outputs.create.created_title}}",
            "body": "This post was updated by Probe at {{unixtime()}}",
            "userId": {{vars.test_user_id}}
          }
      test: |
        res.code == 200 &&
        res.body.id == outputs.create.created_post_id &&
        res.body.title startsWith "Updated:"
      outputs:
        updated_title: res.body.title

    # PARTIAL UPDATE - PATCH Request
    - name: Partial Update Post
      uses: http
      with:
        url: "{{vars.api_base_url}}/posts/{{outputs.create.created_post_id}}"
        method: PATCH
        headers:
          Content-Type: "application/json"
        body: |
          {
            "title": "Patched: {{outputs.update.updated_title}}"
          }
      test: |
        res.code == 200 &&
        res.body.title startsWith "Patched:"
      outputs:
        patched_title: res.body.title

    # DELETE - DELETE Request
    - name: Delete Post
      uses: http
      with:
        url: "{{vars.api_base_url}}/posts/{{outputs.create.created_post_id}}"
        method: DELETE
      test: res.code == 200
      outputs:
        deleted: true

    # VERIFY DELETION
    - name: Verify Deletion
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/posts/{{outputs.create.created_post_id}}"
      test: res.code == 404
      outputs:
        deletion_verified: res.code == 404

    - name: CRUD Test Summary
      uses: hello
      echo: |
        🔄 CRUD Operations Test Summary:

        ✅ CREATE: Post ID {{outputs.create.created_post_id}} created
           Title: "{{outputs.create.created_title}}"

        ✅ READ: Successfully retrieved created post

        ✅ UPDATE: Title updated to "{{outputs.update.updated_title}}"

        ✅ PATCH: Title patched to "{{outputs.patched_title}}"

        ✅ DELETE: Post deletion {{outputs.deleted ? "successful" : "failed"}}

        ✅ VERIFY: Deletion {{outputs.deletion_verified ? "verified (404)" : "not verified"}}

        All CRUD operations completed successfully!
```

## 認証テスト

多くのAPIは認証情報を必要とします。以下のワークフローでは、認証情報を取得してステップ間で受け渡し、認証のないリクエストが拒否されることを確認します。

### Bearer Token認証

トークンはログインのステップで取得し、それ以降のリクエストのauthorizationヘッダーに載せます。

```yaml
name: Bearer Token API Testing
description: Test APIs with Bearer token authentication

vars:
  api_base_url: "{{API_BASE_URL ?? 'https://api.yourapp.com'}}"
  auth_url: "{{AUTH_URL ?? 'https://auth.yourapp.com'}}"
  test_username: "{{TEST_USERNAME ?? 'test@example.com'}}"
  test_password: "{{TEST_PASSWORD ?? 'test_password_123'}}"
  client_id: "{{CLIENT_ID}}"
  client_secret: "{{CLIENT_SECRET}}"
  expired_token: "{{EXPIRED_TOKEN ?? ''}}"

jobs:
- id: authentication-flow
  name: Authentication Flow Test
  steps:
    # Step 1: Obtain Bearer Token
    - name: Login and Get Token
      id: login
      uses: http
      with:
        url: "{{vars.auth_url}}/oauth/token"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "grant_type": "password",
            "username": "{{vars.test_username}}",
            "password": "{{vars.test_password}}",
            "client_id": "{{vars.client_id}}",
            "client_secret": "{{vars.client_secret}}"
          }
      test: |
        res.code == 200 &&
        res.body.access_token != nil &&
        res.body.token_type == "Bearer" &&
        res.body.expires_in > 0
      outputs:
        access_token: res.body.access_token
        refresh_token: res.body.refresh_token
        expires_in: res.body.expires_in
        token_type: res.body.token_type

    # Step 2: Test Authenticated Endpoint
    - name: Get User Profile
      id: profile
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/user/profile"
        headers:
          Authorization: "{{outputs.login.token_type}} {{outputs.login.access_token}}"
          Accept: "application/json"
      test: |
        res.code == 200 &&
        res.body.id != nil &&
        res.body.email == vars.test_username
      outputs:
        user_id: res.body.id
        user_email: res.body.email
        user_name: res.body.name

    # Step 3: Test Protected Resource
    - name: Access Protected Resource
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/user/{{outputs.profile.user_id}}/data"
        headers:
          Authorization: "{{outputs.login.token_type}} {{outputs.login.access_token}}"
      test: |
        res.code == 200 &&
        res.body.user_id == outputs.profile.user_id
      outputs:
        protected_data_accessible: true

    # Step 4: Test Token Refresh
    - name: Refresh Token
      id: refresh
      uses: http
      with:
        url: "{{vars.auth_url}}/oauth/token"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "grant_type": "refresh_token",
            "refresh_token": "{{outputs.login.refresh_token}}",
            "client_id": "{{vars.client_id}}",
            "client_secret": "{{vars.client_secret}}"
          }
      test: |
        res.code == 200 &&
        res.body.access_token != nil &&
        res.body.access_token != outputs.login.access_token
      outputs:
        new_access_token: res.body.access_token

    # Step 5: Test with New Token
    - name: Test New Token
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/user/profile"
        headers:
          Authorization: "Bearer {{outputs.refresh.new_access_token}}"
      test: res.code == 200
      outputs:
        new_token_valid: true

- id: unauthorized-access-test
  name: Unauthorized Access Test
  steps:
    # Test without token
    - name: Test No Authorization
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/user/profile"
      test: res.code == 401
      outputs:
        no_auth_rejected: res.code == 401

    # Test with invalid token
    - name: Test Invalid Token
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/user/profile"
        headers:
          Authorization: "Bearer invalid_token_123"
      test: res.code == 401
      outputs:
        invalid_token_rejected: res.code == 401

    # Test with expired token (if available)
    - name: Test Expired Token
      uses: http
      skipif: vars.expired_token == ""
      with:
        method: GET
        url: "{{vars.api_base_url}}/user/profile"
        headers:
          Authorization: "Bearer {{vars.expired_token}}"
      test: res.code == 401
      outputs:
        expired_token_rejected: res.code == 401

- id: security-test-summary
  name: Security Test Summary
  needs: [authentication-flow, unauthorized-access-test]
  steps:
    - name: Authentication Summary
      uses: hello
      echo: |
        🔐 Authentication & Authorization Test Results:

        AUTHENTICATION FLOW:
        ✅ Login: {{outputs.login.access_token != nil ? "Token obtained" : "Failed"}}
        ✅ Profile Access: {{outputs.profile.user_email != nil ? "Success" : "Failed"}}
        ✅ Protected Resource: {{outputs.protected_data_accessible ? "Accessible" : "Failed"}}
        ✅ Token Refresh: {{outputs.refresh.new_access_token != nil ? "Success" : "Failed"}}
        ✅ New Token Valid: {{outputs.new_token_valid ? "Yes" : "No"}}

        SECURITY VALIDATION:
        ✅ No Auth Rejected: {{outputs.no_auth_rejected ? "Yes (401)" : "Security Issue!"}}
        ✅ Invalid Token Rejected: {{outputs.invalid_token_rejected ? "Yes (401)" : "Security Issue!"}}
        {{vars.expired_token != "" ? "✅ Expired Token Rejected: " + (outputs.expired_token_rejected ? "Yes (401)" : "Security Issue!") : ""}}

        USER INFORMATION:
        User ID: {{outputs.profile.user_id}}
        Email: {{outputs.profile.user_email}}
        Name: {{outputs.profile.user_name}}
        Token Expires In: {{outputs.login.expires_in}} seconds
```

### APIキー認証

APIキーの場合はログインのステップが不要です。環境変数から読み、各リクエストに付けて送ります。

```yaml
name: API Key Authentication Testing
description: Test APIs using API key authentication

vars:
  api_base_url: "{{API_BASE_URL ?? 'https://api.yourservice.com'}}"
  api_key: "{{API_KEY}}"
  rate_limit_tier: "{{RATE_LIMIT_TIER ?? 'premium'}}"

jobs:
- id: api-key-tests
  name: API Key Authentication Tests
  steps:
    # Header-based API Key
    - name: Test API Key in Header
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/data"
        headers:
          X-API-Key: "{{vars.api_key}}"
          Accept: "application/json"
      test: |
        res.code == 200 &&
        res.body.authenticated == true &&
        res.body.rate_limit != nil
      outputs:
        header_auth_success: true
        rate_limit_remaining: res.body.rate_limit.remaining
        rate_limit_reset: res.body.rate_limit.reset_time

    # Query Parameter API Key
    - name: Test API Key in Query
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/data?api_key={{vars.api_key}}"
      test: res.code == 200
      outputs:
        query_auth_success: true

    # Test Rate Limiting
    - name: Test Rate Limit Info
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/rate-limit-status"
        headers:
          X-API-Key: "{{vars.api_key}}"
      test: |
        res.code == 200 &&
        res.body.tier == vars.rate_limit_tier &&
        res.body.requests_remaining > 0
      outputs:
        requests_remaining: res.body.requests_remaining
        requests_per_hour: res.body.limits.per_hour
        current_usage: res.body.current_usage

    # Test Invalid API Key
    - name: Test Invalid API Key
      uses: http
      with:
        method: GET
        url: "{{vars.api_base_url}}/data"
        headers:
          X-API-Key: "invalid_key_123"
      test: res.code == 401 || res.code == 403
      outputs:
        invalid_key_rejected: res.code == 401 || res.code == 403

    - name: API Key Test Summary
      uses: hello
      echo: |
        🔑 API Key Authentication Results:

        Header Authentication: {{outputs.header_auth_success ? "✅ Success" : "❌ Failed"}}
        Query Authentication: {{outputs.query_auth_success ? "✅ Success" : "❌ Failed"}}
        Invalid Key Rejected: {{outputs.invalid_key_rejected ? "✅ Yes" : "❌ Security Issue"}}

        Rate Limiting:
        Tier: {{vars.rate_limit_tier}}
        Requests Remaining: {{outputs.requests_remaining}}/{{outputs.requests_per_hour}}
        Current Usage: {{outputs.current_usage}}
        Reset Time: {{outputs.rate_limit_reset}}
```

## データ検証とレスポンステスト

ステータスが200でも、ボディが正しいとは限りません。以下ではレスポンスの構造と、不正なリクエストに対して返る内容を検証します。

### JSONスキーマ検証

Probeにスキーマ言語はありません。レスポンスの構造はフィールドごとに検証します。

```yaml
name: JSON Response Validation
description: Validate API responses against expected schemas

vars:
  API_BASE_URL: https://api.yourservice.com
  api_token: "{{API_TOKEN}}"

jobs:
- id: response-validation
  name: Response Schema Validation
  steps:
    - name: Get User List
      id: users
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/users"
        headers:
          Authorization: "Bearer {{vars.api_token}}"
      test: |
        res.code == 200 &&
        res.body != nil &&
        res.body.data != nil &&
        len(res.body.data) > 0 &&

        // Validate response structure
        res.body.meta != nil &&
        res.body.meta.total != nil &&
        res.body.meta.page != nil &&
        res.body.meta.per_page != nil &&

        // Validate first user object
        res.body.data[0].id != nil &&
        res.body.data[0].email != nil &&
        res.body.data[0].name != nil &&
        res.body.data[0].created_at != nil &&

        // Validate data types
        type(res.body.data[0].id) == "float" &&
        type(res.body.data[0].email) == "string" &&
        type(res.body.data[0].active) == "bool"
      outputs:
        total_users: res.body.meta.total
        users_per_page: res.body.meta.per_page
        first_user_id: res.body.data[0].id
        first_user_email: res.body.data[0].email

    - name: Get Single User Details
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/users/{{outputs.users.first_user_id}}"
        headers:
          Authorization: "Bearer {{vars.api_token}}"
      test: |
        res.code == 200 &&
        res.body.user != nil &&

        // Validate required fields
        res.body.user.id == outputs.users.first_user_id &&
        res.body.user.email == outputs.users.first_user_email &&
        res.body.user.profile != nil &&

        // Validate nested objects
        res.body.user.profile.first_name != nil &&
        res.body.user.profile.last_name != nil &&
        res.body.user.preferences != nil &&

        // Validate arrays
        res.body.user.roles != nil &&
        len(res.body.user.roles) > 0 &&
        res.body.user.permissions != nil
      outputs:
        user_roles: res.body.user.roles
        user_permissions_count: len(res.body.user.permissions)
        profile_complete: res.body.user.profile.first_name != nil && res.body.user.profile.last_name != nil

    - name: Test Data Consistency
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/users/{{outputs.users.first_user_id}}/orders"
        headers:
          Authorization: "Bearer {{vars.api_token}}"
      test: |
        res.code == 200 &&
        res.body.orders != nil &&

        // Validate all orders belong to the user
        all(res.body.orders, #.user_id == outputs.users.first_user_id) &&

        // Validate order structure
        all(res.body.orders, #.id != nil &&
          #.total != nil &&
          #.status != nil &&
          #.created_at != nil) &&

        // Validate business logic
        all(res.body.orders, #.total >= 0) &&
        all(filter(res.body.orders, #.status == "completed"), #.completed_at != nil)
      outputs:
        order_count: len(res.body.orders)
        completed_orders: 'len(filter(res.body.orders, #.status == "completed"))'
        total_spent: 'sum(map(filter(res.body.orders, #.status == "completed"), #.total))'

    - name: Validation Summary
      uses: hello
      echo: |
        📋 Data Validation Results:

        USER LIST VALIDATION:
        ✅ Response Structure: Valid pagination metadata
        ✅ User Objects: All required fields present
        ✅ Data Types: Correct types for all fields
        Total Users: {{outputs.users.total_users}}
        Users Per Page: {{outputs.users.users_per_page}}

        USER DETAILS VALIDATION:
        ✅ User Profile: {{outputs.profile_complete ? "Complete" : "Incomplete"}}
        ✅ Security: {{len(outputs.user_roles)}} roles, {{outputs.user_permissions_count}} permissions

        DATA CONSISTENCY:
        ✅ Orders: {{outputs.order_count}} total orders
        ✅ Completed: {{outputs.completed_orders}} completed orders
        ✅ Business Logic: All orders have valid totals and timestamps
        Total Customer Value: ${{outputs.total_spent}}
```

### エラーレスポンス検証

エラー時の経路も成功時と同じだけ検証します。ステータスが適切であることと、ボディに失敗の理由が含まれることです。

```yaml
name: Error Response Validation
description: Test error handling and response formats

vars:
  API_BASE_URL: https://api.yourservice.com
  api_token: "{{API_TOKEN}}"
  USER_TOKEN: "{{USER_TOKEN}}"
  LIMITED_TOKEN: "{{LIMITED_TOKEN}}"

jobs:
- id: error-response-tests
  name: Error Response Tests
  steps:
    # Test 400 - Bad Request
    - name: Test Bad Request
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/users"
        method: POST
        headers:
          Content-Type: "application/json"
          Authorization: "Bearer {{vars.api_token}}"
        body: |
          {
            "email": "invalid-email",
            "name": ""
          }
      test: |
        res.code == 400 &&
        res.body.error != nil &&
        res.body.error.code == "validation_error" &&
        res.body.error.message != nil &&
        res.body.error.details != nil &&
        len(res.body.error.details) > 0
      outputs:
        validation_errors: res.body.error.details
        error_code: res.body.error.code

    # Test 401 - Unauthorized
    - name: Test Unauthorized Access
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/admin/users"
      test: |
        res.code == 401 &&
        res.body.error != nil &&
        res.body.error.code == "unauthorized" &&
        res.body.error.message contains "authentication"
      outputs:
        auth_error_proper: true

    # Test 403 - Forbidden
    - name: Test Forbidden Access
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/admin/users"
        headers:
          Authorization: "Bearer {{vars.USER_TOKEN}}"  # Non-admin token
      test: |
        res.code == 403 &&
        res.body.error != nil &&
        res.body.error.code == "forbidden" &&
        res.body.error.message contains "permission"
      outputs:
        permission_error_proper: true

    # Test 404 - Not Found
    - name: Test Not Found
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/users/99999999"
        headers:
          Authorization: "Bearer {{vars.api_token}}"
      test: |
        res.code == 404 &&
        res.body.error != nil &&
        res.body.error.code == "not_found" &&
        res.body.error.resource == "user"
      outputs:
        not_found_error_proper: true

    # Test 422 - Unprocessable Entity
    - name: Test Unprocessable Entity
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/users"
        method: POST
        headers:
          Content-Type: "application/json"
          Authorization: "Bearer {{vars.api_token}}"
        body: |
          {
            "email": "existing@example.com",
            "name": "Test User"
          }
      test: |
        res.code == 422 &&
        res.body.error != nil &&
        res.body.error.code == "unprocessable_entity" &&
        res.body.error.details != nil
      outputs:
        duplicate_email_handled: true

    # Test Rate Limiting - 429
    - name: Test Rate Limiting
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/high-rate-endpoint"
        headers:
          Authorization: "Bearer {{vars.LIMITED_TOKEN}}"
      test: |
        res.code in [200, 429] &&
        (res.code == 429 ? 
          res.body.error.code == "rate_limit_exceeded" &&
          res.headers["Retry-After"] != nil :
          true
        )
      outputs:
        rate_limiting_works: res.code == 429

    - name: Error Handling Summary
      uses: hello
      echo: |
        🚨 Error Response Validation Results:

        400 Bad Request: ✅ Proper validation error format
          Error Code: {{outputs.error_code}}
          Validation Issues: {{len(outputs.validation_errors)}}

        401 Unauthorized: {{outputs.auth_error_proper ? "✅ Proper format" : "❌ Issues detected"}}

        403 Forbidden: {{outputs.permission_error_proper ? "✅ Proper format" : "❌ Issues detected"}}

        404 Not Found: {{outputs.not_found_error_proper ? "✅ Proper format" : "❌ Issues detected"}}

        422 Unprocessable: {{outputs.duplicate_email_handled ? "✅ Business logic validated" : "❌ Issues detected"}}

        429 Rate Limited: {{outputs.rate_limiting_works ? "✅ Rate limiting active" : "ℹ️ No rate limit hit"}}

        All error responses follow consistent format and provide helpful information.
```

## 高度なAPIテストパターン

壊れるのは単一のエンドポイントであることは少ないものです。以下のワークフローでは、互いに依存する一連の呼び出しを検証します。

### ワークフローテスト

利用者の操作に沿った流れは複数のエンドポイントにまたがり、各ステップが直前の結果に依存します。

```yaml
name: E-commerce Workflow Testing
description: Test complete e-commerce user workflow

vars:
  API_BASE_URL: https://api.ecommerce.com
  TEST_PRODUCT_ID: 123
  TEST_USER_EMAIL: test@example.com

jobs:
- id: user-registration-flow
  name: User Registration Workflow
  steps:
    - name: Register New User
      id: register
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/auth/register"
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "email": "test{{random_str(8)}}@example.com",
            "password": "TestPassword123!",
            "first_name": "Test",
            "last_name": "User",
            "phone": "+1234567890"
          }
      test: |
        res.code == 201 &&
        res.body.user.id != nil &&
        res.body.user.email != nil &&
        res.body.access_token != nil
      outputs:
        user_id: res.body.user.id
        user_email: res.body.user.email
        access_token: res.body.access_token

    - name: Email Verification Check
      id: verify-email
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/user/profile"
        headers:
          Authorization: "Bearer {{outputs.register.access_token}}"
      test: |
        res.code == 200 &&
        res.body.email_verified == false &&
        res.body.verification_email_sent == true
      outputs:
        verification_pending: true

- id: shopping-flow
  name: Shopping Workflow
  needs: [user-registration-flow]
  steps:
    - name: Browse Products
      id: browse
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/products?category=electronics&limit=10"
      test: |
        res.code == 200 &&
        len(res.body.products) > 0 &&
        res.body.products[0].id != nil &&
        res.body.products[0].price > 0
      outputs:
        available_products: len(res.body.products)
        first_product_id: res.body.products[0].id
        first_product_price: res.body.products[0].price

    - name: Add to Cart
      id: add-cart
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/cart/items"
        method: POST
        headers:
          Content-Type: "application/json"
          Authorization: "Bearer {{outputs.register.access_token}}"
        body: |
          {
            "product_id": {{outputs.browse.first_product_id}},
            "quantity": 2
          }
      test: |
        res.code == 201 &&
        res.body.cart_item.id != nil &&
        res.body.cart_item.quantity == 2 &&
        res.body.cart_total > 0
      outputs:
        cart_item_id: res.body.cart_item.id
        cart_total: res.body.cart_total

    - name: View Cart
      id: view-cart
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/cart"
        headers:
          Authorization: "Bearer {{outputs.register.access_token}}"
      test: |
        res.code == 200 &&
        len(res.body.items) == 1 &&
        res.body.items[0].product_id == outputs.browse.first_product_id &&
        res.body.total == outputs['add-cart'].cart_total
      outputs:
        cart_verified: true

- id: checkout-flow
  name: Checkout Workflow
  needs: [shopping-flow]
  steps:
    - name: Apply Discount Code
      id: discount
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/cart/discount"
        method: POST
        headers:
          Content-Type: "application/json"
          Authorization: "Bearer {{outputs.register.access_token}}"
        body: |
          {
            "code": "TESTDISCOUNT10"
          }
      test: |
        res.code == 200 &&
        res.body.discount_applied == true &&
        res.body.discount_amount > 0 &&
        res.body.new_total < outputs['add-cart'].cart_total
      outputs:
        discount_applied: res.code == 200
        discount_amount: res.body.discount_amount
        final_total: res.body.new_total

    - name: Add Payment Method
      id: payment
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/payment-methods"
        method: POST
        headers:
          Content-Type: "application/json"
          Authorization: "Bearer {{outputs.register.access_token}}"
        body: |
          {
            "type": "credit_card",
            "card_number": "4111111111111111",
            "expiry_month": 12,
            "expiry_year": 2025,
            "cvv": "123",
            "name": "Test User"
          }
      test: |
        res.code == 201 &&
        res.body.payment_method.id != nil &&
        res.body.payment_method.last_four == "1111"
      outputs:
        payment_method_id: res.body.payment_method.id

    - name: Create Order
      id: order
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/orders"
        method: POST
        headers:
          Content-Type: "application/json"
          Authorization: "Bearer {{outputs.register.access_token}}"
        body: |
          {
            "payment_method_id": {{outputs.payment.payment_method_id}},
            "shipping_address": {
              "street": "123 Test St",
              "city": "Test City",
              "state": "TS",
              "zip": "12345",
              "country": "US"
            }
          }
      test: |
        res.code == 201 &&
        res.body.order.id != nil &&
        res.body.order.status == "processing" &&
        res.body.order.total > 0
      outputs:
        order_id: res.body.order.id
        order_status: res.body.order.status
        order_total: res.body.order.total

    - name: Verify Order Processing
      id: verify-order
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/orders/{{outputs.order.order_id}}"
        headers:
          Authorization: "Bearer {{outputs.register.access_token}}"
      test: |
        res.code == 200 &&
        res.body.order.id == outputs.order.order_id &&
        res.body.order.user_id == outputs.register.user_id &&
        len(res.body.order.items) > 0
      outputs:
        order_verified: true

- id: workflow-summary
  name: Workflow Test Summary
  needs: [user-registration-flow, shopping-flow, checkout-flow]
  steps:
    - name: Complete Workflow Results
      uses: hello
      echo: |
        🛒 E-commerce Workflow Test Results:
        =====================================

        USER REGISTRATION:
        ✅ User Created: {{outputs.register.user_email}}
        ✅ Authentication: Token obtained
        ✅ Email Verification: {{outputs['verify-email'].verification_pending ? "Pending (as expected)" : "Issue detected"}}

        SHOPPING EXPERIENCE:
        ✅ Product Browse: {{outputs.browse.available_products}} products found
        ✅ Add to Cart: Product ID {{outputs.browse.first_product_id}} added
        ✅ Cart Verification: {{outputs['view-cart'].cart_verified ? "Confirmed" : "Failed"}}
        Cart Total: ${{outputs['add-cart'].cart_total}}

        CHECKOUT PROCESS:
        {{outputs.discount.discount_applied ? "✅ Discount Applied: $" + string(outputs.discount.discount_amount) + " off" : "ℹ️ No discount applied"}}
        ✅ Payment Method: Added (ending in 1111)
        ✅ Order Created: Order ID {{outputs.order.order_id}}
        ✅ Order Status: {{outputs.order.order_status}}
        ✅ Order Verified: {{outputs['verify-order'].order_verified ? "Confirmed" : "Failed"}}
        Final Total: ${{outputs.order.order_total}}

        🎉 Complete e-commerce workflow tested successfully!
        User can register, browse, shop, and checkout without issues.
```

## パフォーマンスと負荷テスト

`rt.sec`はどのテストからも参照できるため、正しさと並べて応答時間も検証できます。

### レスポンス時間テスト

`rt.sec`に上限を加えるだけで、既存のテストがパフォーマンスの確認も兼ねます。

```yaml
name: API Performance Testing
description: Test API response times and performance characteristics

vars:
  API_BASE_URL: https://api.yourservice.com
  PERFORMANCE_THRESHOLD_MS: 1000
  ACCEPTABLE_THRESHOLD_MS: 2000
  api_token: "{{API_TOKEN}}"

jobs:
- id: response-time-tests
  name: Response Time Performance Tests
  steps:
    - name: Lightweight Endpoint Test
      id: ping
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/ping"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < 500
      outputs:
        ping_time: (rt.sec * 1000)
        ping_fast: (rt.sec * 1000) < 200

    - name: Database Query Test
      id: db-query
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/users?limit=100"
        headers:
          Authorization: "Bearer {{vars.api_token}}"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < int(vars.PERFORMANCE_THRESHOLD_MS)
      outputs:
        query_time: (rt.sec * 1000)
        query_performance: |
          (rt.sec * 1000) < 500 ? "excellent" :
            (rt.sec * 1000) < 1000 ? "good" :
            (rt.sec * 1000) < 2000 ? "acceptable" : "poor"

    - name: Complex Aggregation Test
      id: aggregation
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/analytics/summary"
        headers:
          Authorization: "Bearer {{vars.api_token}}"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < int(vars.ACCEPTABLE_THRESHOLD_MS)
      outputs:
        aggregation_time: (rt.sec * 1000)
        aggregation_acceptable: (rt.sec * 1000) < int(vars.ACCEPTABLE_THRESHOLD_MS)

    - name: File Upload Test
      id: upload
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/files/upload"
        method: POST
        headers:
          Authorization: "Bearer {{vars.api_token}}"
          Content-Type: "multipart/form-data"
        body: |
          --boundary123
          Content-Disposition: form-data; name="file"; filename="test.txt"
          Content-Type: text/plain

          This is a test file for upload performance testing.
          It contains multiple lines of text to simulate a real file.
          --boundary123--
      test: |
        res.code == 201 &&
        (rt.sec * 1000) < 5000
      outputs:
        upload_time: (rt.sec * 1000)
        upload_acceptable: (rt.sec * 1000) < 3000

    - name: Performance Summary
      uses: hello
      echo: |
        ⚡ API Performance Test Results:

        ENDPOINT PERFORMANCE:
        Ping: {{outputs.ping_time}}ms {{outputs.ping_fast ? "(🚀 Fast)" : "(⚡ OK)"}}
        Database Query: {{outputs.query_time}}ms ({{outputs.query_performance}})
        Complex Aggregation: {{outputs.aggregation_time}}ms {{outputs.aggregation_acceptable ? "(✅ Acceptable)" : "(⚠️ Slow)"}}
        File Upload: {{outputs.upload_time}}ms {{outputs.upload_acceptable ? "(✅ Acceptable)" : "(⚠️ Slow)"}}

        PERFORMANCE CLASSIFICATION:
        {{outputs.ping_time < 200 && outputs.query_time < 500 && outputs.aggregation_time < 1000 ? "🟢 EXCELLENT - All endpoints performing optimally" : ""}}
        {{outputs.ping_time < 500 && outputs.query_time < 1000 && outputs.aggregation_time < 2000 ? "🟡 GOOD - Performance within acceptable ranges" : ""}}
        {{outputs.aggregation_time > 2000 || outputs.upload_time > 5000 ? "🔴 NEEDS ATTENTION - Some endpoints are slow" : ""}}

        RECOMMENDATIONS:
        {{outputs.query_time > 800 ? "• Consider database query optimization" : ""}}
        {{outputs.aggregation_time > 1500 ? "• Review aggregation query efficiency" : ""}}
        {{outputs.upload_time > 3000 ? "• Optimize file upload handling" : ""}}
```

## ベストプラクティス

以下では、テストの構成、テストデータの調達、検証する対象、失敗時の扱いを扱います。

### 1. テスト構造

検証対象ごとにケースをジョブにまとめておけば、失敗した箇所を特定できます。

```yaml fragment
# 良い例: 整理されたテスト構造
jobs:
- name: authentication     # 関連テストをグループ化
- name: crud-operations    # 明確なテストカテゴリ
- name: error-handling     # 論理的な整理
- name: performance        # 関心事の分離
```

### 2. データ管理

データを生成して使えば、前回の実行が残したレコードと衝突しません。

```yaml
# 良い例: 分離のためにランダムデータを使用
body: |
  {
    "email": "test{{random_str(8)}}@example.com",
    "username": "user_{{unixtime()}}_{{random_str(4)}}"
  }
```

```yaml
# 良い例: テストデータのクリーンアップ
- name: Cleanup Test User
  uses: http
  with:
    url: "{{vars.api_base_url}}/users/{{outputs.create.user_id}}"
    method: DELETE
```

### 3. 包括的な検証

レスポンスはステータスコードだけではありません。退行が生じるのはそれ以外の部分です。

```yaml
# 良い例: 複数の側面を検証
test: |
  res.code == 200 &&                              // HTTP ステータス
  res.headers["Content-Type"] contains "json" &&  // コンテンツタイプ
  res.body.id != nil &&                           // 必須フィールド
  type(res.body.id) == "float" &&                 // データ型
  res.body.email contains "@" &&                  // データ形式
  (rt.sec * 1000) < 1000                          // パフォーマンス
```

### 4. エラーハンドリング

APIが拒否すべき入力を送って初めて、その検証処理自体をテストできます。

```yaml
# 良い例: エラーシナリオをテスト
- name: Test Invalid Input
  uses: http
  with:
    body: '{"invalid": "data"}'
  test: res.code == 400
```

```yaml
# 良い例: エラーレスポンスを検証
test: |
  res.code == 400 &&
  res.body.error.code == "validation_error" &&
  len(res.body.error.details) > 0
```

## 次のステップ

APIを包括的にテストできるようになったので、次を探索してください：

- **[エラーハンドリング戦略](/ja/guide/how-tos/error-handling-strategies)** - 堅牢なエラーハンドリングパターン
- **[パフォーマンステスト](/ja/guide/how-tos/performance-testing)** - 応答時間への上限と、専用のツールによる負荷試験
- **[環境管理](/ja/guide/how-tos/environment-management)** - テスト設定の管理

APIテストは信頼性の高いサービスに不可欠です。これらのパターンを使用して、問題をプロダクションに出す前に検出する包括的なテストスイートを構築しましょう。
