# API Testing Pipeline

In this tutorial, you'll build a comprehensive API testing pipeline that validates functionality, performance, security, and data integrity across your entire API surface. This goes beyond simple health checks to create a complete testing suite suitable for CI/CD integration.

## What You'll Build

A complete API testing pipeline featuring:

- **Functional Testing** - Validate CRUD operations and business logic
- **Data Validation** - Ensure response schemas and data integrity
- **Performance Testing** - Response time and throughput validation
- **Authentication Testing** - Security and access control verification
- **Error Handling Tests** - Validate error responses and edge cases
- **Integration Testing** - End-to-end user journey validation
- **Contract Testing** - API specification compliance
- **Regression Testing** - Prevent breaking changes

## Prerequisites

- Probe installed ([Installation Guide](/guide/introduction/installation))
- A REST API to test (we'll use a sample e-commerce API)
- Understanding of HTTP methods and status codes
- Basic knowledge of JSON and API design

## Tutorial Overview

We'll build tests for a sample e-commerce API with these endpoints:

- **Authentication**: `POST /auth/login`, `POST /auth/logout`
- **Users**: `GET /users/profile`, `PUT /users/profile`
- **Products**: `GET /products`, `GET /products/{id}`, `POST /products`
- **Orders**: `POST /orders`, `GET /orders/{id}`, `GET /orders`
- **Cart**: `POST /cart/items`, `DELETE /cart/items/{id}`

## Step 1: Project Structure and Configuration

Create a well-organized testing structure:

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

Probe concatenates the files it is given into one YAML document, so a top-level key such as `vars` defined in two files takes the value of the last one. The values every environment shares are therefore defined in `base.yml` as YAML anchors under `shared`, a key Probe ignores, and each environment file pulls them into its own `vars`.

**config/base.yml:**
```yaml merge=suite base
shared:
  base_vars: &base_vars
    # Test Data
    TEST_USER_EMAIL: "test@example.com"
    TEST_USER_PASSWORD: "TestPassword123!"
    TEST_PRODUCT_NAME: "Test Product"
    TEST_ORDER_AMOUNT: 99.99

    # Performance Thresholds
    FAST_RESPONSE_TIME: 200      # 200ms
    ACCEPTABLE_RESPONSE_TIME: 1000  # 1 second
    SLOW_RESPONSE_TIME: 3000     # 3 seconds

    # Test Configuration
    RETRY_COUNT: 3
    PARALLEL_REQUESTS: 5

    # Token for the endpoints that need one, from the environment
    API_TOKEN: "{{API_TOKEN}}"

  common_headers: &common_headers
    Content-Type: "application/json"
    User-Agent: "Probe API Test Suite v1.0"
```

The shared headers are pulled into a job's `defaults` the same way:

```yaml merge=suite
jobs:
- name: API checks
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

**config/development.yml:**
```yaml merge=suite variant
vars:
  <<: *base_vars
  ENVIRONMENT: "development"
  API_BASE_URL: "http://localhost:3000"
  API_VERSION: "v1"
  SKIP_PERFORMANCE_TESTS: true
  SKIP_LOAD_TESTS: true
```

**config/staging.yml:**
```yaml merge=suite variant
vars:
  <<: *base_vars
  ENVIRONMENT: "staging"
  API_BASE_URL: "https://api-staging.example.com"
  API_VERSION: "v1"
  SKIP_PERFORMANCE_TESTS: false
  SKIP_LOAD_TESTS: true
```

**config/production.yml:**
```yaml merge=suite variant
vars:
  <<: *base_vars
  ENVIRONMENT: "production"
  API_BASE_URL: "https://api.example.com"
  API_VERSION: "v1" 
  SKIP_PERFORMANCE_TESTS: false
  SKIP_LOAD_TESTS: false
  # Use more conservative thresholds for production
  ACCEPTABLE_RESPONSE_TIME: 2000
```

## Step 2: Authentication Testing

Create comprehensive authentication tests. Like every test file in this tutorial, it reads its settings from `vars`, which come from the configuration files it is run with (see [Step 8](#step-8-running-your-test-suite)):

**tests/auth-tests.yml:**
```yaml merge=suite
name: "Authentication Tests"
description: "Test user authentication, authorization, and session management"

jobs:
- id: auth-functional-tests
  name: "Authentication Functional Tests"
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
        len(res.body.token) > 20
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
        res.body.error != nil &&
        res.body.message contains "Invalid credentials"

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
        res.code == 200 &&
        res.body.message contains "logged out"

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

## Step 3: CRUD Operations Testing

Create comprehensive CRUD tests for products:

**tests/product-tests.yml:**
```yaml merge=suite
name: "Product API Tests"
description: "Test product CRUD operations, search, and data validation"

jobs:
- id: product-crud-tests
  name: "Product CRUD Operations"
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
        res.body.name contains vars.TEST_PRODUCT_NAME &&
        res.body.price == 29.99 &&
        res.body.sku startsWith "TEST-" &&
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
        len(res.body.tags) == 3
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
        res.body.name endsWith "UPDATED" &&
        res.body.price == 39.99 &&
        res.body.stock == 75 &&
        len(res.body.tags) == 4
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
        len(res.body.tags) == 3 &&
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

- id: product-search-tests
  name: "Product Search and Filtering"
  needs: [product-crud-tests]
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
        len(res.body.products) == 3
      outputs:
        created_products: res.body.products

    - name: "Test Product Search by Name"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products/search?q=Search%20Test"
      test: |
        res.code == 200 &&
        len(res.body.products) >= 3 &&
        res.body.products[0].name contains "Search Test"

    - name: "Test Product Filter by Category"
      id: product-search-tests
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products?category=Books"
      test: |
        res.code == 200 &&
        len(res.body.products) >= 2
      outputs:
        books_found: len(res.body.products)

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
        len(res.body.products) > 1 &&
        res.body.products[0].price >= res.body.products[1].price

    - name: "Test Pagination"
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products?page=1&limit=2"
      test: |
        res.code == 200 &&
        len(res.body.products) <= 2 &&
        res.body.pagination.page == 1 &&
        res.body.pagination.total > 0
```

## Step 4: Performance Testing

Add performance validation to your tests:

**tests/performance-tests.yml:**
```yaml merge=suite
name: "API Performance Tests"
description: "Validate API response times and performance characteristics"

jobs:
- id: response-time-tests
  name: "Response Time Validation"
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
      uses: hello
      echo: |
        === PERFORMANCE TEST RESULTS ===
        Health Check: {{outputs['health-perf'].health_time}}ms (threshold: {{vars.FAST_RESPONSE_TIME}}ms)
        Product List: {{outputs['list-perf'].list_time}}ms (threshold: {{vars.ACCEPTABLE_RESPONSE_TIME}}ms)
        Search: {{outputs['search-perf'].search_time}}ms (threshold: {{vars.ACCEPTABLE_RESPONSE_TIME}}ms)

        {{outputs['health-perf'].health_time < vars.FAST_RESPONSE_TIME ? "✅" : "❌"}} Health: Fast
        {{outputs['list-perf'].list_time < vars.ACCEPTABLE_RESPONSE_TIME ? "✅" : "❌"}} List: Acceptable
        {{outputs['search-perf'].search_time < vars.ACCEPTABLE_RESPONSE_TIME ? "✅" : "❌"}} Search: Acceptable

- id: concurrent-request-tests
  name: "Concurrent Request Testing"
  needs: [response-time-tests]
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
      outputs:
        time: (rt.sec * 1000)
      # Note: This would be repeated with different parallel execution
      # In a real scenario, you'd use a load testing tool or script

    - name: "Stress Test Report"
      uses: hello
      echo: |
        === CONCURRENT LOAD TEST ===
        Concurrent requests: {{vars.PARALLEL_REQUESTS}}
        Average response time: {{outputs['concurrent-health'].time}}ms
        Success rate: 100%

        Status: {{outputs['concurrent-health'].time < vars.ACCEPTABLE_RESPONSE_TIME ? "✅ PASSED" : "❌ FAILED"}}
```

## Step 5: Data Validation and Schema Testing

Create robust data validation tests:

**tests/data-validation-tests.yml:**
```yaml merge=suite
name: "Data Validation Tests"
description: "Validate API response schemas and data integrity"

jobs:
- id: schema-validation-tests
  name: "Response Schema Validation"
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
        len(res.body.products) > 0 &&
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
        len(res.body.token) > 20

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

- id: data-integrity-tests
  name: "Data Integrity Validation"
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
        res.body.error != nil &&
        res.body.message contains "price"

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
        res.body.error != nil &&
        res.body.message contains "name"

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
        res.body.error != nil &&
        res.body.message contains "email"

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
        res.body.error != nil &&
        res.body.message contains "price"
```

## Step 6: Integration Testing

Create end-to-end user journey tests:

**tests/integration-tests.yml:**
```yaml merge=suite
name: "Integration Tests"
description: "End-to-end user journey validation"

jobs:
- id: user-journey-test
  name: "Complete User Purchase Journey"
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
        timestamp: unixtime()

    - name: "Browse Products"
      id: browse
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products?category=Electronics&limit=5"
      test: |
        res.code == 200 &&
        len(res.body.products) > 0
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
        len(res.body.items) == 1 &&
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
        len(res.body.items) == 1 &&
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
        len(res.body.orders) >= 1 &&
        res.body.orders[0].id == outputs['create-order'].order_id

    - name: "User Journey Report"
      uses: hello
      echo: |
        === USER JOURNEY TEST COMPLETE ===

        ✅ User Registration: {{outputs.register.user_email}}
        ✅ Product Browse: Found {{len(outputs.browse.available_products)}} products
        ✅ Add to Cart: Product {{outputs.browse.first_product_id}}
        ✅ Update Cart: Quantity changed to 3
        ✅ Order Creation: Order {{outputs['create-order'].order_id}}
        ✅ Order Verification: Status {{outputs['create-order'].order_status}}
        ✅ Order History: Retrieved successfully

        Total Order Value: ${{outputs['update-cart'].new_cart_total}}
        Journey Completion Time: {{(unixtime() - outputs.register.timestamp)}} seconds
```

## Step 7: Main Test Suite

Create the main test suite that orchestrates all tests:

**main-test-suite.yml:**
```yaml merge=suite
name: "Complete API Test Suite"
description: "Comprehensive API testing pipeline"

# Import base configuration
# This file will be merged with environment-specific config

jobs:
- id: setup
  name: "Test Environment Setup"
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
      uses: hello
      echo: |
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

- id: authentication-tests
  name: "Authentication Test Suite"
  needs: [setup]
  # In practice, you would import the auth-tests.yml file here
  # For this example, we'll reference key auth tests
  steps:
    - name: "Run Authentication Tests"
      uses: hello
      echo: "Running authentication test suite..."
      # This would typically import tests/auth-tests.yml

- id: functional-tests
  name: "Functional Test Suite"
  needs: [authentication-tests]
  steps:
    - name: "Run Product CRUD Tests"
      uses: hello
      echo: "Running product CRUD test suite..."
      # This would typically import tests/product-tests.yml

    - name: "Run Data Validation Tests"
      uses: hello
      echo: "Running data validation test suite..."
      # This would typically import tests/data-validation-tests.yml

- id: performance-tests
  name: "Performance Test Suite"
  needs: [functional-tests]
  steps:
    - name: "Run Performance Tests"
      uses: hello
      echo: "Running performance test suite..."
      # This would typically import tests/performance-tests.yml

- id: integration-tests
  name: "Integration Test Suite"
  needs: [functional-tests]
  steps:
    - name: "Run Integration Tests"
      uses: hello
      echo: "Running integration test suite..."
      # This would typically import tests/integration-tests.yml

- id: test-report
  name: "Generate Test Report"
  needs: [authentication-tests, functional-tests, performance-tests, integration-tests]
  steps:
    - name: "Final Test Report"
      uses: hello
      echo: |
        === API TEST SUITE COMPLETE ===

        Test Results Summary:
        ✅ Authentication Tests: Completed
        ✅ Functional Tests: Completed
        {{vars.SKIP_PERFORMANCE_TESTS ? "⏭️  Performance Tests: Skipped" : "✅ Performance Tests: Completed"}}
        ✅ Integration Tests: Completed

        Overall Status: ✅ ALL TESTS PASSED

        Generated: {{now().Format('2006-01-02T15:04:05Z07:00')}}
```

## Step 8: Running Your Test Suite

Execute your comprehensive API test suite:

```bash
# Run tests for different environments
probe config/base.yml,config/development.yml,main-test-suite.yml
probe config/base.yml,config/staging.yml,main-test-suite.yml
probe config/base.yml,config/production.yml,main-test-suite.yml

# Run specific test categories
probe config/base.yml,config/staging.yml,tests/auth-tests.yml
probe config/base.yml,config/staging.yml,tests/product-tests.yml

# Run with verbose output for debugging
probe -v config/base.yml,config/development.yml,tests/integration-tests.yml
```

## Step 9: CI/CD Integration

Integrate your API tests into your CI/CD pipeline:

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
          API_TOKEN: ${{ secrets.API_TOKEN }}
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

## Step 10: Advanced Testing Patterns

Two kinds of check go beyond whether an endpoint works: whether it still matches the contract its clients rely on, and whether it rejects what it should.

### Contract Testing

Add API contract validation:

```yaml merge=suite
# tests/contract-tests.yml
name: "API Contract Tests"
description: "Validate API specification compliance"

jobs:
- id: openapi-compliance
  name: "OpenAPI Specification Compliance"
  steps:
    - name: "Validate Product Endpoints Against Schema"
      # This would validate responses against OpenAPI spec
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
      # Custom validation logic for OpenAPI compliance;
      # add further schema checks to the expression
      test: |
        res.code == 200 &&
        res.body.products != nil
```

### Security Testing

Add basic security validation:

```yaml merge=suite
# tests/security-tests.yml
name: "API Security Tests"
description: "Basic security validation tests"

jobs:
- id: security-headers
  name: "Security Headers Validation"
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

- id: authentication-security
  name: "Authentication Security Tests"
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

## Troubleshooting

If the test suite does not run as described, the causes below are the ones to check first.

### Common Issues

**Authentication Token Expiration:**
```yaml
# Add token refresh logic
- name: "Refresh Token If Needed"
  uses: http
  with:
    method: POST
    url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/auth/refresh"
    headers:
      Authorization: "Bearer {{outputs.login.auth_token}}"
  # ... token refresh logic
```

**Test Data Cleanup:**
```yaml
# Add cleanup job
- id: cleanup
  name: "Test Data Cleanup"
  steps:
    - name: "Delete Test Products"
      # ... cleanup logic
```

**Rate Limiting:**
```yaml
# Wait before a request with the step's wait
- name: "List Products After a Pause"
  wait: 1s
  uses: http
  with:
    method: GET
    url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
  test: res.code == 200
```

## Next Steps

Your comprehensive API testing pipeline is now complete! Consider these extensions:

1. **Performance Profiling** - Add detailed performance analysis
2. **Database State Validation** - Verify database changes
3. **Mock Service Testing** - Test against mocked dependencies
4. **Chaos Engineering** - Test failure scenarios
5. **Visual Regression Testing** - For APIs that return UI components
6. **API Versioning Tests** - Test backward compatibility

## Related Resources

- **[First Monitoring System Tutorial](/guide/tutorials/first-monitoring-system)** - Basic monitoring setup
- **[Multi-Environment Testing Tutorial](/guide/tutorials/multi-environment-testing)** - Environment management
- **[How-tos: Environment Management](/guide/how-tos/environment-management)** - Pipeline integration and environment management
- **[Reference: Actions](/reference/actions-reference)** - Complete action reference
