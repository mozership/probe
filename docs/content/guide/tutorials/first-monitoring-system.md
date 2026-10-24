# Building Your First Monitoring System

In this tutorial, you'll build a complete monitoring system using Probe to monitor a web application's health, performance, and availability. By the end, you'll have a production-ready monitoring workflow that checks multiple endpoints, validates responses, and sends alerts when issues are detected.

## What You'll Build

A comprehensive monitoring system that:

- **Health checks** multiple API endpoints
- **Performance monitoring** with response time tracking
- **Data validation** to ensure API responses are correct
- **Error handling** with automatic retries and fallback checks
- **Alert notifications** via email when issues are detected
- **Environment configuration** for development, staging, and production

## Prerequisites

- Probe installed ([Installation Guide](/guide/introduction/installation))
- Basic understanding of HTTP APIs
- An SMTP server for notifications that accepts mail from the machine running Probe without logging in, such as your company's mail relay
- A web application or API to monitor (we'll provide examples if you don't have one)

## Step 1: Planning Your Monitoring Strategy

Before writing code, let's plan what we want to monitor:

### Target Application

For this tutorial, we'll monitor a fictional e-commerce API with these endpoints:

- `GET /health` - Basic health check
- `GET /api/products` - Product catalog availability  
- `GET /api/users/me` - User authentication system
- `GET /api/orders/recent` - Order processing system
- `POST /api/search` - Search functionality

### Success Criteria

Our monitoring should verify:

- **Availability**: All endpoints return successful responses
- **Performance**: Response times are within acceptable limits
- **Data Integrity**: Responses contain expected data structures
- **Authentication**: Protected endpoints require proper authentication

## Step 2: Creating the Basic Monitoring Workflow

Let's start with a simple monitoring workflow:

```yaml
# monitoring.yml
name: "E-commerce API Monitoring"
description: |
  Comprehensive monitoring of e-commerce API endpoints including
  health checks, performance monitoring, and data validation.

vars:
  # API Configuration
  API_BASE_URL: "https://api.example.com"
  API_VERSION: "v1"
  
  # Performance Thresholds
  MAX_RESPONSE_TIME: 2000  # 2 seconds
  CRITICAL_RESPONSE_TIME: 5000  # 5 seconds
  
  # Authentication
  API_TOKEN: "{{API_AUTH_TOKEN}}"

jobs:
- id: health-check
  name: "Basic Health Check"
  defaults:
    http:
      headers:
        User-Agent: "Probe Monitor v1.0"
        Accept: "application/json"
  steps:
    - name: "Check API Health Endpoint"
      id: health
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/health"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME
      outputs:
        status: res.body.status
        response_time: (rt.sec * 1000)
        healthy: res.code == 200 && res.body.status == "healthy"
```

**Save this as `monitoring.yml` and test it:**

```bash
# Set your API token
export API_AUTH_TOKEN="your-api-token-here"

# Run the basic monitoring
probe monitoring.yml
```

## Step 3: Adding Comprehensive Endpoint Monitoring

Now let's expand to monitor all critical endpoints:

```yaml
# Add this job to the jobs list of monitoring.yml, after the health-check job

- id: endpoint-monitoring
  name: "API Endpoint Monitoring"
  needs: [health-check]
  steps:
    - name: "Test Product Catalog"
      id: products
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.products != nil &&
        len(res.body.products) > 0
      outputs:
        product_count: len(res.body.products)
        response_time: (rt.sec * 1000)
        available: res.code == 200

    - name: "Test User Authentication"
      id: auth
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/users/me"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.user != nil &&
        res.body.user.id != nil
      outputs:
        authenticated: res.code == 200
        user_id: res.body.user.id
        response_time: (rt.sec * 1000)

    - name: "Test Order System"
      id: orders
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/orders/recent"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.orders != nil
      outputs:
        order_count: len(res.body.orders)
        response_time: (rt.sec * 1000)
        available: res.code == 200

    - name: "Test Search Functionality"
      id: search
      uses: http
      with:
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/search"
        method: "POST"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
          Content-Type: "application/json"
        body: |
          {
            "query": "test product",
            "limit": 10
          }
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.results != nil
      outputs:
        result_count: len(res.body.results)
        response_time: (rt.sec * 1000)
        available: res.code == 200
```

## Step 4: Adding Performance Analysis

Let's add a job that analyzes overall performance:

```yaml
# Add this job to the jobs list of monitoring.yml

- id: performance-analysis
  name: "Performance Analysis"
  needs: [health-check, endpoint-monitoring]
  always: true  # Report even when a check failed
  steps:
    - name: "Calculate Performance Metrics"
      id: metrics
      uses: hello
      with:
        # Average response time of the endpoints that answered
        avg_response_time: >-
          {{ mean(filter([
            outputs.health?.response_time,
            outputs.products?.response_time,
            outputs.auth?.response_time,
            outputs.orders?.response_time,
            outputs.search?.response_time
          ], # != nil)) }}
        # Count successful endpoints
        successful_endpoints: >-
          {{ count([
            outputs.health?.healthy,
            outputs.products?.available,
            outputs.auth?.authenticated,
            outputs.orders?.available,
            outputs.search?.available
          ], # == true) }}
      outputs:
        avg_response_time: res.avg_response_time
        successful_endpoints: res.successful_endpoints
        # Calculate success rate percentage
        success_rate: res.successful_endpoints * 100 / 5
        # Determine overall health status
        overall_status: |
          res.successful_endpoints == 5 ? "healthy" :
          res.successful_endpoints >= 4 ? "degraded" : "critical"

    - name: "Performance Report"
      uses: hello
      echo: |
        === MONITORING REPORT ===

        Overall Status: {{upper(outputs.metrics.overall_status)}}
        Success Rate: {{outputs.metrics.success_rate}}%
        Average Response Time: {{outputs.metrics.avg_response_time}}ms

        Endpoint Details:
        ✓ Health Check: {{outputs.health?.healthy == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.health?.response_time ?? "-"}}ms)
        ✓ Products API: {{outputs.products?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.products?.response_time ?? "-"}}ms)
        ✓ Authentication: {{outputs.auth?.authenticated == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.auth?.response_time ?? "-"}}ms)
        ✓ Orders API: {{outputs.orders?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.orders?.response_time ?? "-"}}ms)
        ✓ Search API: {{outputs.search?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.search?.response_time ?? "-"}}ms)

        Data Summary:
        - Products Available: {{outputs.products?.product_count ?? 0}}
        - Recent Orders: {{outputs.orders?.order_count ?? 0}}
        - Search Results: {{outputs.search?.result_count ?? 0}}

        Generated: {{now().Format('2006-01-02T15:04:05Z07:00')}}
```

`always: true` runs this job even when a job it needs failed, which is when the report matters most. An output that a failed or skipped job did not publish reads as nil, so the job reads each one with `?.`. The outputs of one step cannot read each other, so the counts are computed in `with`: `hello` returns what `with` gave it in `res`, and the outputs build on that.

## Step 5: Adding Error Handling and Retries

Now let's make our monitoring more robust with error handling:

```yaml
# Replace the endpoint-monitoring job with this enhanced version

- id: endpoint-monitoring
  name: "API Endpoint Monitoring"
  needs: [health-check]
  steps:
    - name: "Test Product Catalog"
      id: products
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.products != nil &&
        len(res.body.products) > 0
      retry:
        max_attempts: 3  # Try up to 3 times before the step fails
        interval: 2s
      outputs:
        product_count: len(res.body.products ?? [])
        response_time: (rt.sec * 1000)
        available: res.code == 200
        status_code: res.code

    - name: "Test User Authentication"
      id: auth
      uses: http
      with:
        method: GET
        url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/users/me"
        headers:
          Authorization: "Bearer {{vars.API_TOKEN}}"
      test: |
        res.code == 200 &&
        (rt.sec * 1000) < vars.MAX_RESPONSE_TIME &&
        res.body.user != nil &&
        res.body.user.id != nil
      retry:
        max_attempts: 3
        interval: 2s
      outputs:
        authenticated: res.code == 200
        user_id: res.body.user?.id ?? "unknown"
        response_time: (rt.sec * 1000)
        status_code: res.code

    # Continue with the orders and search steps using the same pattern...
```

`retry` runs a step again until its test passes, so a single slow or dropped response does not fail the check. `??` supplies a value when the response lacks the field, so the outputs are published even when the API answers with an error.

## Step 6: Adding Email Notifications

Let's add email alerts when issues are detected. The alert job reads the mail settings from the environment, so add them to the `vars` of `monitoring.yml`:

```yaml
vars:
  # Email Configuration
  SMTP_HOST: "{{SMTP_HOST ?? 'localhost'}}"
  SMTP_PORT: "{{SMTP_PORT ?? '25'}}"
  ALERT_FROM_EMAIL: "{{ALERT_FROM_EMAIL}}"
  ALERT_TO_EMAIL: "{{ALERT_TO_EMAIL}}"
```

Then add the job:

```yaml
# Add this job to send alerts when monitoring detects issues

- id: alerting
  name: "Alert Notifications"
  needs: [performance-analysis]
  skipif: outputs.metrics.overall_status == "healthy"
  steps:
    - name: "Send Alert Email"
      uses: smtp
      with:
        addr: "{{vars.SMTP_HOST}}:{{vars.SMTP_PORT}}"
        from: "{{vars.ALERT_FROM_EMAIL}}"
        to: "{{vars.ALERT_TO_EMAIL}}"
        subject: "🚨 API Monitoring Alert - {{upper(outputs.metrics.overall_status)}}"
        session: 1
        message: 1
        length: 500
      test: res.code == 0 && res.sent > 0
      echo: |
        API MONITORING ALERT
        ====================

        Alert Summary:
        Overall Status: {{upper(outputs.metrics.overall_status)}}
        Success Rate: {{outputs.metrics.success_rate}}%
        Average Response Time: {{outputs.metrics.avg_response_time}}ms
        Time: {{now().Format('2006-01-02T15:04:05Z07:00')}}

        Endpoint Status:
        Health Check: {{outputs.health?.healthy == true ? "✅ UP" : "❌ DOWN"}} (status: {{outputs.health?.status ?? "-"}})
        Products API: {{outputs.products?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.products?.product_count ?? 0}} products)
        Authentication: {{outputs.auth?.authenticated == true ? "✅ UP" : "❌ DOWN"}} (user ID: {{outputs.auth?.user_id ?? "-"}})
        Orders API: {{outputs.orders?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.orders?.order_count ?? 0}} recent orders)
        Search API: {{outputs.search?.available == true ? "✅ UP" : "❌ DOWN"}} ({{outputs.search?.result_count ?? 0}} search results)

        Recommended Actions:
        {{outputs.health?.healthy != true ? "- Check API server health and connectivity" : ""}}
        {{outputs.products?.available != true ? "- Investigate product catalog service" : ""}}
        {{outputs.auth?.authenticated != true ? "- Verify authentication service and token validity" : ""}}
        {{outputs.orders?.available != true ? "- Check order processing system" : ""}}
        {{outputs.search?.available != true ? "- Investigate search service functionality" : ""}}
        {{outputs.metrics.avg_response_time > 3000 ? "- Performance issue detected - investigate slow responses" : ""}}

        This alert was generated by Probe Monitoring System.

    - name: "Log Alert Sent"
      uses: hello
      echo: |
        🚨 ALERT SENT
        Status: {{outputs.metrics.overall_status}}
        Success Rate: {{outputs.metrics.success_rate}}%
        Alert sent to: {{vars.ALERT_TO_EMAIL}}
```

`skipif` skips the job while every endpoint is up. The `smtp` action sends a generated message with the subject above. It has no parameters for a body of your own or for logging in to the server, so use a server that accepts mail from this host without authentication, such as your company's mail relay. The report goes in the step's `echo`, which appears in Probe's output.

## Step 7: Environment Configuration

Create environment-specific configuration files. Probe joins the files given to it into one document, so the `vars` of a later file replace those of `monitoring.yml` as a whole rather than entry by entry. Each file therefore repeats every var, including the ones that read the environment:

**development.yml:**
```yaml
vars:
  API_BASE_URL: "http://localhost:3000"
  API_VERSION: "v1"
  MAX_RESPONSE_TIME: 5000  # More lenient for dev
  CRITICAL_RESPONSE_TIME: 10000
  API_TOKEN: "{{API_AUTH_TOKEN}}"
  SMTP_HOST: "{{SMTP_HOST ?? 'localhost'}}"
  SMTP_PORT: "{{SMTP_PORT ?? '25'}}"
  ALERT_FROM_EMAIL: "{{ALERT_FROM_EMAIL}}"
  ALERT_TO_EMAIL: "{{ALERT_TO_EMAIL}}"
```

**staging.yml:**
```yaml
vars:
  API_BASE_URL: "https://api-staging.example.com"
  API_VERSION: "v1"
  MAX_RESPONSE_TIME: 3000
  CRITICAL_RESPONSE_TIME: 7000
  API_TOKEN: "{{API_AUTH_TOKEN}}"
  SMTP_HOST: "{{SMTP_HOST ?? 'localhost'}}"
  SMTP_PORT: "{{SMTP_PORT ?? '25'}}"
  ALERT_FROM_EMAIL: "{{ALERT_FROM_EMAIL}}"
  ALERT_TO_EMAIL: "{{ALERT_TO_EMAIL}}"
```

**production.yml:**
```yaml
vars:
  API_BASE_URL: "https://api.example.com"
  API_VERSION: "v1"
  MAX_RESPONSE_TIME: 2000
  CRITICAL_RESPONSE_TIME: 5000
  API_TOKEN: "{{API_AUTH_TOKEN}}"
  SMTP_HOST: "{{SMTP_HOST ?? 'localhost'}}"
  SMTP_PORT: "{{SMTP_PORT ?? '25'}}"
  ALERT_FROM_EMAIL: "{{ALERT_FROM_EMAIL}}"
  ALERT_TO_EMAIL: "{{ALERT_TO_EMAIL}}"
```

## Step 8: Setting Up Environment Variables

Create a script to set up your environment variables:

**setup-monitoring.sh:**
```bash
#!/bin/bash

# API Configuration
export API_AUTH_TOKEN="your-api-token-here"

# Email Configuration
export SMTP_HOST="mail.yourcompany.com"
export SMTP_PORT=25
export ALERT_FROM_EMAIL="alerts@yourcompany.com"
export ALERT_TO_EMAIL="admin@yourcompany.com"

# Environment Selection
export ENVIRONMENT=${1:-development}

echo "Environment configured for: $ENVIRONMENT"
echo "API Token: ${API_AUTH_TOKEN:0:10}..."
echo "Alert Email: $ALERT_TO_EMAIL"
```

## Step 9: Running Your Monitoring System

Now you can run your complete monitoring system:

```bash
# Set up environment
chmod +x setup-monitoring.sh
source setup-monitoring.sh production

# Run monitoring for specific environment
probe monitoring.yml,production.yml

# Run with verbose output to see details
probe -v monitoring.yml,staging.yml

# Run for development environment
source setup-monitoring.sh development
probe monitoring.yml,development.yml
```

## Step 10: Automating with Cron

Set up automated monitoring with cron:

```bash
# Edit crontab
crontab -e

# Add entries for regular monitoring
# setup-monitoring.sh exports the variables, so it is sourced in bash
SHELL=/bin/bash

# Run every 5 minutes during business hours
*/5 9-17 * * 1-5 source /path/to/setup-monitoring.sh production && probe /path/to/monitoring.yml,/path/to/production.yml >> /var/log/probe-monitoring.log 2>&1

# Run every 15 minutes outside business hours
*/15 0-8,18-23 * * * source /path/to/setup-monitoring.sh production && probe /path/to/monitoring.yml,/path/to/production.yml >> /var/log/probe-monitoring.log 2>&1

# Run every hour on weekends
0 * * * 0,6 source /path/to/setup-monitoring.sh production && probe /path/to/monitoring.yml,/path/to/production.yml >> /var/log/probe-monitoring.log 2>&1
```

## Testing and Validation

Before leaving the system to run on its own, check that each part works: the individual jobs, and the alert that is supposed to arrive when one of them fails.

### Test Individual Components

Running one job at a time confirms each part before the whole workflow is left to run unattended.

```bash
# Test the health check (the performance report, an always job, runs after it)
probe -v --job health-check monitoring.yml

# Test without email alerts (runs the jobs the analysis needs, not the alerting)
probe --job performance-analysis monitoring.yml,production.yml

# Test with fake failures (modify URLs to non-existent endpoints)
probe monitoring.yml,development.yml
```

### Validate Email Alerts

1. Point the checks at an address where nothing answers, so that every check fails: `probe --var API_BASE_URL=http://localhost:1 monitoring.yml`
2. Verify that the mail was delivered
3. Check the subject of the mail, and the report in Probe's output
4. Run without `--var` again, and verify that the alerting job is skipped

## Troubleshooting

If the monitoring system does not behave as described above, the causes below are the ones to check first.

### Common Issues

**Authentication failures:**
```bash
# Check API token
curl -H "Authorization: Bearer $API_AUTH_TOKEN" https://api.example.com/api/v1/users/me

# Verify token is not expired
echo $API_AUTH_TOKEN | base64 -d  # For JWT tokens
```

**Email delivery issues:**
```bash
# Test SMTP connection
telnet "$SMTP_HOST" "$SMTP_PORT"

# Probe does not log in to the server, so it has to accept
# mail from this host without authentication
```

**Performance issues:**
```bash
# Check network connectivity
ping api.example.com

# Test response times manually
curl -w "@curl-format.txt" -o /dev/null -s https://api.example.com/health
```

**JSON parsing errors:**
```bash
# Validate API responses manually
curl -H "Authorization: Bearer $API_AUTH_TOKEN" https://api.example.com/api/v1/products | jq .
```

## Next Steps

Congratulations! You've built a comprehensive monitoring system. Here are ways to extend it:

1. **Add Database Monitoring** - Monitor database connectivity and performance
2. **Integrate with Dashboards** - Send metrics to Grafana or similar tools
3. **Add More Alert Channels** - Slack, PagerDuty, or webhook notifications
4. **Implement Smart Alerting** - Only alert after multiple consecutive failures
5. **Add Business Logic Tests** - Test critical user journeys, not just endpoints
6. **Performance Regression Detection** - Compare response times over time
7. **SSL Certificate Monitoring** - Check certificate expiration dates

## Related Resources

- **[API Testing Pipeline Tutorial](/guide/tutorials/api-testing-pipeline)** - Advanced API testing patterns
- **[Multi-Environment Testing Tutorial](/guide/tutorials/multi-environment-testing)** - Test across multiple environments
- **[How-tos: Performance Testing](/guide/how-tos/performance-testing)** - Limits on response times, and load tests with a tool made for them
- **[How-tos: Monitoring Workflows](/guide/how-tos/monitoring-workflows)** - Additional monitoring patterns
- **[Reference: Actions](/reference/actions-reference)** - Complete action reference
