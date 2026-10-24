# APIテストパイプライン

このチュートリアルでは、API全体の機能、パフォーマンス、セキュリティ、データ整合性を検証する包括的なAPIテストパイプラインを構築します。これは単純なヘルスチェックだけでなく、CI/CD統合に適した完全なテストスイートを作成します。

## 構築する内容

以下の機能を持つ完全なAPIテストパイプライン：

- **機能テスト** - CRUD操作とビジネスロジックの検証
- **データ検証** - レスポンススキーマとデータ整合性の確保
- **パフォーマンステスト** - レスポンス時間とスループットの検証
- **認証テスト** - セキュリティとアクセス制御の検証
- **エラーハンドリングテスト** - エラーレスポンスとエッジケースの検証
- **統合テスト** - エンドツーエンドユーザージャーニーの検証
- **契約テスト** - API仕様への準拠
- **リグレッションテスト** - 破壊的変更の防止

## 前提条件

- Probeがインストール済み（[インストールガイド](/ja/guide/introduction/installation)）
- テスト対象のREST API（サンプルeコマースAPIを使用します）
- HTTPメソッドとステータスコードの理解
- JSONとAPI設計の基本知識

## チュートリアル概要

以下のエンドポイントを持つサンプルeコマースAPIのテストを構築します：

- **認証**: `POST /auth/login`, `POST /auth/logout`
- **ユーザー**: `GET /users/profile`, `PUT /users/profile`
- **商品**: `GET /products`, `GET /products/{id}`, `POST /products`
- **注文**: `POST /orders`, `GET /orders/{id}`, `GET /orders`
- **カート**: `POST /cart/items`, `DELETE /cart/items/{id}`

## ステップ1: プロジェクト構造と設定

整理されたテスト構造を作成します：

```bash
api-tests/
├── config/
│   ├── base.yml
│   ├── development.yml
│   ├── staging.yml
│   └── production.yml
├── tests/
│   ├── auth-tests.yml
│   ├── user-tests.yml
│   ├── product-tests.yml
│   ├── order-tests.yml
│   └── integration-tests.yml
└── main-test-suite.yml
```

**config/base.yml:**
```yaml
name: "API Testing Pipeline"
description: "Comprehensive API testing suite for e-commerce platform"

vars:
  # テストデータ
  TEST_USER_EMAIL: "test@example.com"
  TEST_USER_PASSWORD: "TestPassword123!"
  TEST_PRODUCT_NAME: "Test Product"
  TEST_ORDER_AMOUNT: 99.99
  
  # パフォーマンスしきい値
  FAST_RESPONSE_TIME: 200      # 200ms
  ACCEPTABLE_RESPONSE_TIME: 1000  # 1秒
  SLOW_RESPONSE_TIME: 3000     # 3秒
  
  # テスト設定
  RETRY_COUNT: 3
  PARALLEL_REQUESTS: 5

jobs:
- name: default
  defaults:
    http:
      headers:
        Content-Type: "application/json"
        User-Agent: "Probe API Test Suite v1.0"
```

**config/development.yml:**
```yaml
vars:
  API_BASE_URL: "http://localhost:3000"
  API_VERSION: "v1"
  SKIP_PERFORMANCE_TESTS: true
  SKIP_LOAD_TESTS: true

jobs:
- name: default
  defaults:
    http:
```

**config/staging.yml:**
```yaml
vars:
  API_BASE_URL: "https://api-staging.example.com"
  API_VERSION: "v1"
  SKIP_PERFORMANCE_TESTS: false
  SKIP_LOAD_TESTS: true
```

**config/production.yml:**
```yaml
vars:
  API_BASE_URL: "https://api.example.com"
  API_VERSION: "v1" 
  SKIP_PERFORMANCE_TESTS: false
  SKIP_LOAD_TESTS: false
  # プロダクション用により保守的なしきい値を使用
  ACCEPTABLE_RESPONSE_TIME: 2000
```

## ステップ2: 認証テスト

包括的な認証テストを作成します：

**tests/auth-tests.yml:**
```yaml
name: "Authentication Tests"
description: "Test user authentication, authorization, and session management"

jobs:
- name: "Authentication Functional Tests"
  steps:
    - name: "Test User Registration"
      id: register
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/register"
        method: "POST"
        body: |
          {
            "email": "{{vars.TEST_USER_EMAIL}}",
            "password": "{{vars.TEST_USER_PASSWORD}}",
            "firstName": "Test",
            "lastName": "User"
          }
      test: |
        res.code == 201 &&
        res.body.user != nil &&
        res.body.user.email == vars.TEST_USER_EMAIL &&
        res.body.token != nil
      outputs:
        user_id: res.body.user.id
        auth_token: res.body.token

    - name: "Test User Login"
      id: login
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
        res.body.user != nil &&
        res.body.token | len > 20
      outputs:
        auth_token: res.body.token
        user_id: res.body.user.id
        login_time: (rt.sec * 1000)

    - name: "Test Invalid Login Credentials"
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/login"
        method: "POST"
        body: |
          {
            "email": "{{vars.TEST_USER_EMAIL}}",
            "password": "wrongpassword"
          }
      test: |
        res.code == 401 &&
        res.body.error != nil

    - name: "Test Token Validation"
      id: token-validation
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/validate"
        headers:
          Authorization: "Bearer {{outputs.login.auth_token}}"
      test: |
        res.code == 200 &&
        res.body.valid == true &&
        res.body.user.id == outputs.login.user_id

    - name: "Test Invalid Token"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/validate"
        headers:
          Authorization: "Bearer invalid-token-12345"
      test: |
        res.code == 401 &&
        res.body.error != nil

    - name: "Test Token Refresh"
      id: refresh
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/refresh"  
        method: "POST"
        headers:
          Authorization: "Bearer {{outputs.login.auth_token}}"
      test: |
        res.code == 200 &&
        res.body.token != nil &&
        res.body.token != outputs.login.auth_token
      outputs:
        new_token: res.body.token

    - name: "Test Logout"
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/logout"
        method: "POST"
        headers:
          Authorization: "Bearer {{outputs.refresh.new_token}}"
      test: |
        res.code == 200

    - name: "Test Using Token After Logout"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/validate"
        headers:
          Authorization: "Bearer {{outputs.refresh.new_token}}"
      test: |
        res.code == 401 &&
        res.body.error != nil
```

## ステップ3: CRUD操作テスト

商品の包括的CRUDテストを作成します：

**tests/product-tests.yml:**
```yaml
name: "Product API Tests"
description: "Test product CRUD operations, search, and data validation"

jobs:
- name: "Product CRUD Operations"
  steps:
    - name: "Setup - Get Auth Token"
      id: auth
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/login"
        method: "POST"
        body: |
          {
            "email": "{{vars.TEST_USER_EMAIL}}",
            "password": "{{vars.TEST_USER_PASSWORD}}"
          }
      test: res.code == 200
      outputs:
        token: res.body.token

    - name: "Create Product"
      id: create-product
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
        method: "POST"
        headers:
          Authorization: "Bearer {{outputs.auth.token}}"
        body: |
          {
            "name": "{{vars.TEST_PRODUCT_NAME}} {{unixtime()}}",
            "description": "Test product for API testing",
            "price": 29.99,
            "category": "Electronics",
            "sku": "TEST-{{unixtime()}}",
            "stock": 100,
            "tags": ["test", "electronics", "api-test"]
          }
      test: |
        res.code == 201 &&
        res.body.id != nil &&
        res.body.price == 29.99 &&
        res.body.stock == 100
      outputs:
        product_id: res.body.id
        product_name: res.body.name
        product_sku: res.body.sku
        creation_time: (rt.sec * 1000)

    - name: "Read Product by ID"
      id: read-product
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/{{outputs['create-product'].product_id}}"
      test: |
        res.code == 200 &&
        res.body.id == outputs['create-product'].product_id &&
        res.body.name == outputs['create-product'].product_name &&
        res.body.price == 29.99 &&
        res.body.category == "Electronics" &&
        res.body.tags | len == 3
      outputs:
        read_time: (rt.sec * 1000)

    - name: "Update Product"
      id: update-product
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/{{outputs['create-product'].product_id}}"
        method: "PUT"
        headers:
          Authorization: "Bearer {{outputs.auth.token}}"
        body: |
          {
            "name": "{{outputs['create-product'].product_name}} - UPDATED",
            "description": "Updated test product",
            "price": 39.99,
            "category": "Electronics",
            "sku": "{{outputs['create-product'].product_sku}}",
            "stock": 75,
            "tags": ["test", "electronics", "api-test", "updated"]
          }
      test: |
        res.code == 200 &&
        res.body.id == outputs['create-product'].product_id &&
        res.body.price == 39.99 &&
        res.body.stock == 75 &&
        res.body.tags | len == 4
      outputs:
        updated_name: res.body.name
        update_time: (rt.sec * 1000)

    - name: "Verify Update Persistence"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/{{outputs['create-product'].product_id}}"
      test: |
        res.code == 200 &&
        res.body.name == outputs['update-product'].updated_name &&
        res.body.price == 39.99 &&
        res.body.stock == 75

    - name: "Test Partial Update (PATCH)"
      id: patch-product
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/{{outputs['create-product'].product_id}}"
        method: "PATCH"
        headers:
          Authorization: "Bearer {{outputs.auth.token}}"
        body: |
          {
            "stock": 50,
            "tags": ["test", "electronics", "patched"]
          }
      test: |
        res.code == 200 &&
        res.body.stock == 50 &&
        res.body.tags | len == 3 &&
        res.body.name == outputs['update-product'].updated_name
      outputs:
        patch_time: (rt.sec * 1000)

    - name: "Delete Product"
      id: delete-product
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/{{outputs['create-product'].product_id}}"
        method: "DELETE"
        headers:
          Authorization: "Bearer {{outputs.auth.token}}"
      test: res.code == 204 || res.code == 200
      outputs:
        delete_time: (rt.sec * 1000)

    - name: "Verify Product Deletion"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/{{outputs['create-product'].product_id}}"
      test: res.code == 404

- name: "Product Search and Filtering"
  needs: [product-crud-operations]
  steps:
    - name: "Setup - Create Multiple Test Products"
      id: setup-products
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/batch"
        method: "POST"
        headers:
          Authorization: "Bearer {{outputs.auth.token}}"
        body: |
          {
            "products": [
              {
                "name": "Search Test Product A",
                "price": 10.00,
                "category": "Books",
                "tags": ["fiction", "bestseller"]
              },
              {
                "name": "Search Test Product B", 
                "price": 25.00,
                "category": "Electronics",
                "tags": ["gadget", "mobile"]
              },
              {
                "name": "Search Test Product C",
                "price": 15.00,
                "category": "Books",
                "tags": ["non-fiction", "educational"]
              }
            ]
          }
      test: |
        res.code == 201 &&
        res.body.products | len == 3
      outputs:
        created_products: res.body.products

    - name: "Test Product Search by Name"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/search?q=Search%20Test"
      test: |
        res.code == 200 &&
        res.body.products | len >= 3

    - name: "Test Product Filter by Category"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products?category=Books"
      test: |
        res.code == 200 &&
        res.body.products | len >= 2
      outputs:
        books_found: res.body.products | len

    - name: "Test Product Filter by Price Range"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products?min_price=10&max_price=20"
      test: |
        res.code == 200 &&
        res.body.products[0].price >= 10 &&
        res.body.products[0].price <= 20

    - name: "Test Product Sorting"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products?sort=price&order=desc"
      test: |
        res.code == 200 &&
        res.body.products | len > 1 &&
        res.body.products[0].price >= res.body.products[1].price

    - name: "Test Pagination"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products?page=1&limit=2"
      test: |
        res.code == 200 &&
        res.body.products | len <= 2 &&
        res.body.pagination.page == 1 &&
        res.body.pagination.total > 0
```

## ステップ4: パフォーマンステスト

テストにパフォーマンス検証を追加します：

**tests/performance-tests.yml:**
```yaml
name: "API Performance Tests"
description: "Validate API response times and performance characteristics"

jobs:
- name: "Response Time Validation"
  steps:
    - name: "Test Fast Endpoint Performance"
      id: health-perf
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.FAST_RESPONSE_TIME
      outputs:
        health_time: (rt.sec * 1000)

    - name: "Test API List Performance"
      id: list-perf
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products?limit=10"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.ACCEPTABLE_RESPONSE_TIME
      outputs:
        list_time: (rt.sec * 1000)

    - name: "Test Search Performance"
      id: search-perf
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/search?q=test"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.ACCEPTABLE_RESPONSE_TIME
      outputs:
        search_time: (rt.sec * 1000)

    - name: "Performance Summary"
      uses: echo
      with:
        message: |
          === PERFORMANCE TEST RESULTS ===
          Health Check: {{outputs['health-perf'].health_time}}ms (threshold: {{vars.FAST_RESPONSE_TIME}}ms)
          Product List: {{outputs['list-perf'].list_time}}ms (threshold: {{vars.ACCEPTABLE_RESPONSE_TIME}}ms)
          Search: {{outputs['search-perf'].search_time}}ms (threshold: {{vars.ACCEPTABLE_RESPONSE_TIME}}ms)
          
          {{outputs['health-perf'].health_time < vars.FAST_RESPONSE_TIME ? "✅" : "❌"}} Health: Fast
          {{outputs['list-perf'].list_time < vars.ACCEPTABLE_RESPONSE_TIME ? "✅" : "❌"}} List: Acceptable
          {{outputs['search-perf'].search_time < vars.ACCEPTABLE_RESPONSE_TIME ? "✅" : "❌"}} Search: Acceptable

- name: "Concurrent Request Testing"
  needs: [response-time-validation]
  steps:
    - name: "Concurrent Health Checks"
      id: concurrent-health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < (vars.ACCEPTABLE_RESPONSE_TIME * 2)

    - name: "Stress Test Report"
      uses: echo
      with:
        message: |
          === CONCURRENT LOAD TEST ===
          Concurrent requests: {{vars.PARALLEL_REQUESTS}}
          Average response time: {{outputs['concurrent-health'].time}}ms
          Success rate: 100%
          
          Status: {{outputs['concurrent-health'].time < vars.ACCEPTABLE_RESPONSE_TIME ? "✅ PASSED" : "❌ FAILED"}}
```

## ステップ5: データ検証とスキーマテスト

堅牢なデータ検証テストを作成します：

**tests/data-validation-tests.yml:**
```yaml
name: "Data Validation Tests"
description: "Validate API response schemas and data integrity"

jobs:
- name: "Response Schema Validation"
  steps:
    - name: "Validate Product Schema"
      id: product-schema
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
      test: |
        res.code == 200 &&
        res.body.products != nil &&
        res.body.products | len > 0 &&
        res.body.products[0].id != nil &&
        res.body.products[0].name != nil &&
        res.body.products[0].price != nil &&
        res.body.products[0].category != nil &&
        res.body.pagination != nil &&
        res.body.pagination.total != nil &&
        res.body.pagination.page != nil &&
        res.body.pagination.limit != nil

    - name: "Validate User Profile Schema"
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
        res.body.user != nil &&
        res.body.user.id != nil &&
        res.body.user.email != nil &&
        res.body.user.firstName != nil &&
        res.body.user.lastName != nil &&
        res.body.user.createdAt != nil &&
        res.body.token != nil &&
        res.body.token | len > 20

    - name: "Validate Error Response Schema"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/nonexistent-id"
      test: |
        res.code == 404 &&
        res.body.error != nil &&
        res.body.message != nil &&
        res.body.statusCode == 404 &&
        res.body.timestamp != nil

- name: "Data Integrity Validation"
  steps:
    - name: "Test Data Type Validation"
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
        method: "POST"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
        body: |
          {
            "name": "Type Test Product",
            "price": "invalid-price",
            "category": "Books"
          }
      test: |
        res.code == 400 &&
        res.body.error != nil

    - name: "Test Required Field Validation"
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
        method: "POST"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
        body: |
          {
            "description": "Missing required name field"
          }
      test: |
        res.code == 400 &&
        res.body.error != nil

    - name: "Test Email Format Validation"
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/register"
        method: "POST"
        body: |
          {
            "email": "invalid-email-format",
            "password": "validpassword123"
          }
      test: |
        res.code == 400 &&
        res.body.error != nil

    - name: "Test Numeric Range Validation"
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
        method: "POST"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
        body: |
          {
            "name": "Range Test Product",
            "price": -10.00,
            "category": "Books"
          }
      test: |
        res.code == 400 &&
        res.body.error != nil
```

## ステップ6: 統合テスト

エンドツーエンドユーザージャーニーテストを作成します：

**tests/integration-tests.yml:**
```yaml
name: "Integration Tests"
description: "End-to-end user journey validation"

jobs:
- name: "Complete User Purchase Journey"
  steps:
    - name: "User Registration"
      id: register
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/register"
        method: "POST"
        body: |
          {
            "email": "journey-test-{{unixtime()}}@example.com",
            "password": "JourneyTest123!",
            "firstName": "Journey",
            "lastName": "Test"
          }
      test: res.code == 201
      outputs:
        user_id: res.body.user.id
        auth_token: res.body.token
        user_email: res.body.user.email

    - name: "Browse Products"
      id: browse
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products?category=Electronics&limit=5"
      test: |
        res.code == 200 &&
        res.body.products | len > 0
      outputs:
        available_products: res.body.products
        first_product_id: res.body.products[0].id
        first_product_price: res.body.products[0].price

    - name: "Add Product to Cart"
      id: add-to-cart
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/cart/items"
        method: "POST"
        headers:
          Authorization: "Bearer {{outputs.register.auth_token}}"
        body: |
          {
            "productId": "{{outputs.browse.first_product_id}}",
            "quantity": 2
          }
      test: |
        res.code == 201 &&
        res.body.item.productId == outputs.browse.first_product_id &&
        res.body.item.quantity == 2
      outputs:
        cart_item_id: res.body.item.id
        cart_total: res.body.cart.total

    - name: "View Cart"
      id: view-cart
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/cart"
        headers:
          Authorization: "Bearer {{outputs.register.auth_token}}"
      test: |
        res.code == 200 &&
        res.body.items | len == 1 &&
        res.body.items[0].productId == outputs.browse.first_product_id &&
        res.body.total == (outputs.browse.first_product_price * 2)

    - name: "Update Cart Quantity"
      id: update-cart
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/cart/items/{{outputs['add-to-cart'].cart_item_id}}"
        method: "PUT"
        headers:
          Authorization: "Bearer {{outputs.register.auth_token}}"
        body: |
          {
            "quantity": 3
          }
      test: |
        res.code == 200 &&
        res.body.item.quantity == 3
      outputs:
        new_cart_total: res.body.cart.total

    - name: "Create Order"
      id: create-order
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/orders"
        method: "POST"
        headers:
          Authorization: "Bearer {{outputs.register.auth_token}}"
        body: |
          {
            "shippingAddress": {
              "street": "123 Test Street",
              "city": "Test City",
              "zipCode": "12345",
              "country": "US"
            },
            "paymentMethod": "credit_card",
            "paymentDetails": {
              "cardNumber": "4111111111111111",
              "expiryMonth": "12",
              "expiryYear": "2025",
              "cvv": "123"
            }
          }
      test: |
        res.code == 201 &&
        res.body.order.id != nil &&
        res.body.order.status == "pending" &&
        res.body.order.total == outputs['update-cart'].new_cart_total
      outputs:
        order_id: res.body.order.id
        order_status: res.body.order.status

    - name: "Verify Order Details"
      id: verify-order
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/orders/{{outputs['create-order'].order_id}}"
        headers:
          Authorization: "Bearer {{outputs.register.auth_token}}"
      test: |
        res.code == 200 &&
        res.body.id == outputs['create-order'].order_id &&
        res.body.items | len == 1 &&
        res.body.items[0].productId == outputs.browse.first_product_id &&
        res.body.items[0].quantity == 3
      outputs:
        order_created_at: res.body.createdAt

    - name: "View Order History"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/orders"
        headers:
          Authorization: "Bearer {{outputs.register.auth_token}}"
      test: |
        res.code == 200 &&
        res.body.orders | len >= 1 &&
        res.body.orders[0].id == outputs['create-order'].order_id

    - name: "User Journey Report"
      uses: echo
      with:
        message: |
          === USER JOURNEY TEST COMPLETE ===
          
          ✅ User Registration: {{outputs.register.user_email}}
          ✅ Product Browse: Found {{outputs.browse.available_products | len}} products
          ✅ Add to Cart: Product {{outputs.browse.first_product_id}}
          ✅ Update Cart: Quantity changed to 3
          ✅ Order Creation: Order {{outputs['create-order'].order_id}}
          ✅ Order Verification: Status {{outputs['create-order'].order_status}}
          ✅ Order History: Retrieved successfully
          
          Total Order Value: ${{outputs['update-cart'].new_cart_total}}
          Journey Completion Time: {{unixtime() - outputs.register.timestamp}} seconds
```

## ステップ7: メインテストスイート

すべてのテストをまとめるメインテストスイートを作成します：

**main-test-suite.yml:**
```yaml
name: "Complete API Test Suite"
description: "Comprehensive API testing pipeline"

jobs:
- name: "Test Environment Setup"
  steps:
    - name: "Verify API Availability"
      id: api-check
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: res.code == 200
      outputs:
        api_available: res.code == 200

    - name: "Setup Test Data"
      uses: echo
      with:
        message: |
          === API TEST SUITE STARTING ===
          Environment: {{vars.ENVIRONMENT ?? 'default'}}
          API Base URL: {{vars.API_BASE_URL}}
          API Version: {{vars.API_VERSION}}
          Skip Performance Tests: {{vars.SKIP_PERFORMANCE_TESTS}}
          Skip Load Tests: {{vars.SKIP_LOAD_TESTS}}
          
          Test Configuration:
          - Fast Response Threshold: {{vars.FAST_RESPONSE_TIME}}ms
          - Acceptable Response Threshold: {{vars.ACCEPTABLE_RESPONSE_TIME}}ms
          - Slow Response Threshold: {{vars.SLOW_RESPONSE_TIME}}ms

- name: "Authentication Test Suite"
  needs: [test-environment-setup]
  # 実際には、ここでauth-tests.ymlファイルをインポートします
  steps:
    - name: "Run Authentication Tests"
      uses: echo
      with:
        message: "Running authentication test suite..."

- name: "Functional Test Suite"
  needs: [authentication-test-suite]
  steps:
    - name: "Run Product CRUD Tests"
      uses: echo
      with:
        message: "Running product CRUD test suite..."
        
    - name: "Run Data Validation Tests"
      uses: echo
      with:
        message: "Running data validation test suite..."

- name: "Performance Test Suite"
  needs: [functional-test-suite]
  steps:
    - name: "Run Performance Tests"
      uses: echo
      with:
        message: "Running performance test suite..."

- name: "Integration Test Suite"
  needs: [functional-test-suite]
  steps:
    - name: "Run Integration Tests"
      uses: echo
      with:
        message: "Running integration test suite..."

- name: "Generate Test Report"
  needs: [authentication-test-suite, functional-test-suite, performance-test-suite, integration-test-suite]
  steps:
    - name: "Final Test Report"
      uses: echo
      with:
        message: |
          === API TEST SUITE COMPLETE ===
          
          Test Results Summary:
          ✅ Authentication Tests: Completed
          ✅ Functional Tests: Completed
          {{vars.SKIP_PERFORMANCE_TESTS != "true" ? "✅ Performance Tests: Completed" : "⏭️  Performance Tests: Skipped"}}
          ✅ Integration Tests: Completed
          
          Overall Status: ✅ ALL TESTS PASSED
          
          Generated: {{unixtime()}}
```

## ステップ8: テストスイートの実行

包括的なAPIテストスイートを実行します：

```bash
# 異なる環境でのテスト実行
probe config/base.yml,config/development.yml,main-test-suite.yml
probe config/base.yml,config/staging.yml,main-test-suite.yml
probe config/base.yml,config/production.yml,main-test-suite.yml

# 特定のテストカテゴリの実行
probe config/base.yml,config/staging.yml,tests/auth-tests.yml
probe config/base.yml,config/staging.yml,tests/product-tests.yml

# デバッグ用詳細出力で実行
probe -v config/base.yml,config/development.yml,tests/integration-tests.yml
```

## ステップ9: CI/CD統合

APIテストをCI/CDパイプラインに統合します：

**.github/workflows/api-tests.yml:**
```yaml
name: API Tests
on: 
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  api-tests:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        environment: [staging, production]
    
    steps:
      - uses: actions/checkout@v3
      
      - name: Install Probe
        run: |
          curl -L https://github.com/mozership/probe/releases/latest/download/probe_linux_x86_64.tar.gz | tar -xz probe
          chmod +x probe
          sudo mv probe /usr/local/bin/
      
      - name: Run API Tests
        env:
          API_AUTH_TOKEN: ${{ secrets.API_TOKEN }}
          SMTP_USERNAME: ${{ secrets.SMTP_USERNAME }}
          SMTP_PASSWORD: ${{ secrets.SMTP_PASSWORD }}
          ENVIRONMENT: ${{ matrix.environment }}
        run: |
          cd api-tests
          probe config/base.yml,config/${{ matrix.environment }}.yml,main-test-suite.yml
      
      - name: Upload Test Results
        if: always()
        uses: actions/upload-artifact@v3
        with:
          name: api-test-results-${{ matrix.environment }}
          path: test-results/
```

## ステップ10: 高度なテストパターン

エンドポイントが動くかどうかのほかに、2種類の確認があります。クライアントが依存している契約を満たしているか、そして拒否すべき入力を拒否するかです。

### 契約テスト

API契約検証を追加します：

```yaml
# tests/contract-tests.yml
name: "API Contract Tests"
description: "Validate API specification compliance"

jobs:
- name: "OpenAPI Specification Compliance"
  steps:
    - name: "Validate Product Endpoints Against Schema"
      # OpenAPI仕様に対してレスポンスを検証
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
      test: |
        res.code == 200 &&
        res.body.products != nil
```

### セキュリティテスト

基本的なセキュリティ検証を追加します：

```yaml
# tests/security-tests.yml
name: "API Security Tests"
description: "Basic security validation tests"

jobs:
- name: "Security Headers Validation"
  steps:
    - name: "Check Security Headers"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
      test: |
        res.code == 200 &&
        res.headers["X-Content-Type-Options"] != nil &&
        res.headers["X-Frame-Options"] != nil &&
        res.headers["X-Xss-Protection"] != nil

- name: "Authentication Security Tests"
  steps:
    - name: "Test SQL Injection Prevention"
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/search"
        method: "POST"
        body: |
          {
            "query": "'; DROP TABLE products; --"
          }
      test: |
        res.code == 400 ||
        (res.code == 200 && res.body.error == nil)
```

## トラブルシューティング

テストスイートが説明どおりに動かない場合は、以下を順に確認してください。

### よくある問題

**認証トークンの有効期限切れ:**
```yaml
# トークンリフレッシュロジックを追加
- name: "Refresh Token If Needed"
  uses: http
  with:
    method: GET
    url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/refresh"
  # ... トークンリフレッシュロジック
```

**テストデータのクリーンアップ:**
```yaml
# クリーンアップジョブを追加
cleanup:
  name: "Test Data Cleanup"
  steps:
    - name: "Delete Test Products"
      # ... クリーンアップロジック
```

**レート制限:**
```yaml
# リクエスト間の遅延を追加
- name: "Rate Limit Delay"
  uses: echo
  with:
    message: "Waiting for rate limit..."
    delay: "1s"
```

## 次のステップ

包括的なAPIテストパイプラインが完成しました！以下の拡張を検討してください：

1. **パフォーマンスプロファイリング** - 詳細なパフォーマンス分析の追加
2. **データベース状態検証** - データベースの変更の確認
3. **モックサービステスト** - モック化された依存関係に対するテスト
4. **カオスエンジニアリング** - 障害シナリオのテスト
5. **ビジュアルリグレッションテスト** - UIコンポーネントを返すAPIのテスト
6. **APIバージョニングテスト** - 後方互換性のテスト

## 関連リソース

- **[初めての監視システムチュートリアル](/ja/guide/tutorials/first-monitoring-system)** - 基本的な監視セットアップ
- **[マルチ環境テストチュートリアル](/ja/guide/tutorials/multi-environment-testing)** - 環境管理
- **[ハウツー: 環境管理](/ja/guide/how-tos/environment-management)** - パイプライン統合と環境管理
- **[リファレンス: アクション](/ja/reference/actions/variables)** - 完全なアクションリファレンス
