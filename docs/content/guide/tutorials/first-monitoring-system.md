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
- Email account for notifications (Gmail, Office 365, or SMTP server)
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
  API_TOKEN: "{{vars.API_AUTH_TOKEN}}"

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
# Add this to your monitoring.yml file after the health-check job

  endpoint-monitoring:
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
# Add this job to your monitoring.yml file

  performance-analysis:
    name: "Performance Analysis"
    needs: [endpoint-monitoring]
    steps:
      - name: "Calculate Performance Metrics"
        id: metrics
        uses: hello
        with:
          message: "Analyzing performance metrics"
        outputs:
          # Calculate average response time across all endpoints
          avg_response_time: |
            {{(outputs.health.response_time +
               outputs.products.response_time +
               outputs.auth.response_time +
               outputs.orders.response_time +
               outputs.search.response_time) / 5}}
          
          # Count successful endpoints
          successful_endpoints: |
            {{(outputs.health.healthy ? 1 : 0) +
               (outputs.products.available ? 1 : 0) +
               (outputs.auth.authenticated ? 1 : 0) +
               (outputs.orders.available ? 1 : 0) +
               (outputs.search.available ? 1 : 0)}}
          
          # Calculate success rate percentage
          success_rate: |
            {{((outputs.metrics.successful_endpoints / 5) * 100)}}
          
          # Determine overall health status
          overall_status: |
            {{outputs.metrics.success_rate == 100 ? "healthy" :
              outputs.metrics.success_rate >= 80 ? "degraded" : "critical"}}

      - name: "Performance Report"
        uses: hello
        echo: |
          === MONITORING REPORT ===
          
          Overall Status: {{outputs.metrics.overall_status | upper}}
          Success Rate: {{outputs.metrics.success_rate}}%
          Average Response Time: {{outputs.metrics.avg_response_time}}ms
          
          Endpoint Details:
          ✓ Health Check: {{outputs.health.healthy ? "✅ UP" : "❌ DOWN"}} ({{outputs.health.response_time}}ms)
          ✓ Products API: {{outputs.products.available ? "✅ UP" : "❌ DOWN"}} ({{outputs.products.response_time}}ms)
          ✓ Authentication: {{outputs.auth.authenticated ? "✅ UP" : "❌ DOWN"}} ({{outputs.auth.response_time}}ms)
          ✓ Orders API: {{outputs.orders.available ? "✅ UP" : "❌ DOWN"}} ({{outputs.orders.response_time}}ms)
          ✓ Search API: {{outputs.search.available ? "✅ UP" : "❌ DOWN"}} ({{outputs.search.response_time}}ms)
          
          Data Summary:
          - Products Available: {{outputs.products.product_count}}
          - Recent Orders: {{outputs.orders.order_count}}
          - Search Results: {{outputs.search.result_count}}
          
          Generated: {{now().Format('2006-01-02T15:04:05Z07:00')}}
```

## Step 5: Adding Error Handling and Retries

Now let's make our monitoring more robust with error handling:

```yaml
# Replace the endpoint-monitoring job with this enhanced version

  endpoint-monitoring:
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
          product_count: len(res.body.products || [])
          response_time: (rt.sec * 1000)
          available: res.code == 200
          status_code: res.status

      - name: "Retry Product Catalog (if failed)"
        id: products-retry
        uses: http
        timeout: "30s"  # Longer timeout for retry
        with:
          method: GET
          url: "{{vars.API_BASE_URL}}/api/{{vars.API_VERSION}}/products"
        test: res.code == 200
        outputs:
          retry_successful: res.code == 200
          retry_time: (rt.sec * 1000)

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
          user_id: res.body.user.id || "unknown"
          response_time: (rt.sec * 1000)
          status_code: res.status

      # Continue with other endpoints using similar patterns...
```

## Step 6: Adding Email Notifications

Let's add email alerts when issues are detected:

```yaml
# Add this job to send alerts when monitoring detects issues

  alerting:
    name: "Alert Notifications"
    needs: [performance-analysis]
    steps:
      - name: "Send Critical Alert Email"
        uses: smtp
        with:
          addr: "{{vars.SMTP_HOST || 'smtp.gmail.com'}}:{{vars.SMTP_PORT || 587}}"
          from: "{{vars.ALERT_FROM_EMAIL}}"
          to: "{{vars.ALERT_TO_EMAIL}}"
          subject: "🚨 CRITICAL: API Monitoring Alert - {{outputs.metrics.overall_status | upper}}"
          session: 1
          message: 1
          length: 500
        echo: |
          <html>
          <head><title>API Monitoring Alert</title></head>
          <body style="font-family: Arial, sans-serif; margin: 20px;">
            
          <h1 style="color: #d32f2f;">🚨 Critical API Monitoring Alert</h1>
            
          <div style="background: #ffebee; border-left: 4px solid #d32f2f; padding: 15px; margin: 20px 0;">
            <h2>Alert Summary</h2>
            <p><strong>Overall Status:</strong> {{outputs.metrics.overall_status | upper}}</p>
            <p><strong>Success Rate:</strong> {{outputs.metrics.success_rate}}%</p>
            <p><strong>Average Response Time:</strong> {{outputs.metrics.avg_response_time}}ms</p>
            <p><strong>Time:</strong> {{now().Format('2006-01-02T15:04:05Z07:00')}}</p>
          </div>
            
          <h2>Endpoint Status</h2>
          <table border="1" style="border-collapse: collapse; width: 100%;">
            <tr style="background: #f5f5f5;">
              <th style="padding: 10px; text-align: left;">Endpoint</th>
              <th style="padding: 10px; text-align: left;">Status</th>
              <th style="padding: 10px; text-align: left;">Response Time</th>
              <th style="padding: 10px; text-align: left;">Details</th>
            </tr>
            <tr>
              <td style="padding: 10px;">Health Check</td>
              <td style="padding: 10px; color: {{outputs.health.healthy ? 'green' : 'red'}};">
                {{outputs.health.healthy ? "✅ UP" : "❌ DOWN"}}
              </td>
              <td style="padding: 10px;">{{outputs.health.response_time}}ms</td>
              <td style="padding: 10px;">{{outputs.health.status}}</td>
            </tr>
            <tr>
              <td style="padding: 10px;">Products API</td>
              <td style="padding: 10px; color: {{outputs.products.available ? 'green' : 'red'}};">
                {{outputs.products.available ? "✅ UP" : "❌ DOWN"}}
              </td>
              <td style="padding: 10px;">{{outputs.products.response_time}}ms</td>
              <td style="padding: 10px;">{{outputs.products.product_count}} products</td>
            </tr>
            <tr>
              <td style="padding: 10px;">Authentication</td>
              <td style="padding: 10px; color: {{outputs.auth.authenticated ? 'green' : 'red'}};">
                {{outputs.auth.authenticated ? "✅ UP" : "❌ DOWN"}}
              </td>
              <td style="padding: 10px;">{{outputs.auth.response_time}}ms</td>
              <td style="padding: 10px;">User ID: {{outputs.auth.user_id}}</td>
            </tr>
            <tr>
              <td style="padding: 10px;">Orders API</td>
              <td style="padding: 10px; color: {{outputs.orders.available ? 'green' : 'red'}};">
                {{outputs.orders.available ? "✅ UP" : "❌ DOWN"}}
              </td>
              <td style="padding: 10px;">{{outputs.orders.response_time}}ms</td>
              <td style="padding: 10px;">{{outputs.orders.order_count}} recent orders</td>
            </tr>
            <tr>
              <td style="padding: 10px;">Search API</td>
              <td style="padding: 10px; color: {{outputs.search.available ? 'green' : 'red'}};">
                {{outputs.search.available ? "✅ UP" : "❌ DOWN"}}
              </td>
              <td style="padding: 10px;">{{outputs.search.response_time}}ms</td>
              <td style="padding: 10px;">{{outputs.search.result_count}} search results</td>
            </tr>
          </table>
            
          <div style="background: #f5f5f5; padding: 15px; margin: 20px 0;">
            <h3>Recommended Actions</h3>
            <ul>
              {{if not outputs.health.healthy}}<li>Check API server health and connectivity</li>{{end}}
              {{if not outputs.products.available}}<li>Investigate product catalog service</li>{{end}}
              {{if not outputs.auth.authenticated}}<li>Verify authentication service and token validity</li>{{end}}
              {{if not outputs.orders.available}}<li>Check order processing system</li>{{end}}
              {{if not outputs.search.available}}<li>Investigate search service functionality</li>{{end}}
              {{if gt outputs.metrics.avg_response_time 3000}}<li>Performance issue detected - investigate slow responses</li>{{end}}
            </ul>
          </div>
            
          <p style="font-size: 12px; color: #666;">
            This alert was generated by Probe Monitoring System.<br>
            Workflow: {{workflow.name}}<br>
            Generated: {{now().Format('2006-01-02T15:04:05Z07:00')}}
          </p>
            
          </body>
          </html>

      - name: "Log Alert Sent"
        uses: hello
        echo: |
          🚨 CRITICAL ALERT SENT
          Status: {{outputs.metrics.overall_status}}
          Success Rate: {{outputs.metrics.success_rate}}%
          Alert sent to: {{vars.ALERT_TO_EMAIL}}
```

## Step 7: Environment Configuration

Create environment-specific configuration files:

**development.yml:**
```yaml
vars:
  API_BASE_URL: "http://localhost:3000"
  API_VERSION: "v1"
  MAX_RESPONSE_TIME: 5000  # More lenient for dev
  CRITICAL_RESPONSE_TIME: 10000

```

**staging.yml:**
```yaml
vars:
  API_BASE_URL: "https://api-staging.example.com"
  API_VERSION: "v1"
  MAX_RESPONSE_TIME: 3000
  CRITICAL_RESPONSE_TIME: 7000

```

**production.yml:**
```yaml
vars:
  API_BASE_URL: "https://api.example.com"
  API_VERSION: "v1"
  MAX_RESPONSE_TIME: 2000
  CRITICAL_RESPONSE_TIME: 5000

```

## Step 8: Setting Up Environment Variables

Create a script to set up your environment variables:

**setup-monitoring.sh:**
```bash
#!/bin/bash

# API Configuration
export API_AUTH_TOKEN="your-api-token-here"

# Email Configuration
export SMTP_HOST="smtp.gmail.com"
export SMTP_PORT=587
export SMTP_USERNAME="your-email@gmail.com"
export SMTP_PASSWORD="your-app-password"
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
# Run every 5 minutes during business hours
*/5 9-17 * * 1-5 /path/to/setup-monitoring.sh production && probe /path/to/monitoring.yml,/path/to/production.yml >> /var/log/probe-monitoring.log 2>&1

# Run every 15 minutes outside business hours
*/15 0-8,18-23 * * * /path/to/setup-monitoring.sh production && probe /path/to/monitoring.yml,/path/to/production.yml >> /var/log/probe-monitoring.log 2>&1

# Run every hour on weekends
0 * * * 0,6 /path/to/setup-monitoring.sh production && probe /path/to/monitoring.yml,/path/to/production.yml >> /var/log/probe-monitoring.log 2>&1
```

## Testing and Validation

Before leaving the system to run on its own, check that each part works: the individual jobs, and the alert that is supposed to arrive when one of them fails.

### Test Individual Components

Running one job at a time confirms each part before the whole workflow is left to run unattended.

```bash
# Test health check only
probe -v monitoring.yml --jobs=health-check

# Test without email alerts
probe monitoring.yml,production.yml --skip-jobs=alerting

# Test with fake failures (modify URLs to non-existent endpoints)
probe monitoring.yml,development.yml
```

### Validate Email Alerts

1. Temporarily set success thresholds very high to trigger alerts
2. Run monitoring and verify email delivery
3. Check email formatting and content
4. Restore normal thresholds

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
telnet smtp.gmail.com 587

# Check app password (for Gmail)
# Generate new app password in Google Account settings
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
