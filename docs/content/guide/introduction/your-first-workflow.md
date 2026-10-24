---
title: Your First Workflow
description: Learn to build practical workflows with real-world examples
weight: 40
---

# Your First Workflow

Now that you understand the [core concepts](/guide/introduction/understanding-probe), let's build a practical workflow from scratch. This guide will walk you through creating a comprehensive monitoring workflow for a web application.

## The Scenario

We'll create a workflow that monitors a complete web application stack:

1. **Frontend**: Check if the web application loads correctly
2. **API**: Verify that the REST API is responding
3. **Database**: Test database connectivity through the API
4. **External Services**: Check third-party service integrations

## Step 1: Basic Structure

Let's start with the basic workflow structure:

```yaml
name: Web Application Health Check
description: Comprehensive monitoring for our web application stack

jobs:
  # We'll add jobs here
```

## Step 2: Frontend Monitoring

Add a job to check the frontend application:

```yaml
name: Web Application Health Check
description: Comprehensive monitoring for our web application stack

jobs:
- id: frontend-check
  name: Frontend Application Check
  steps:
    - name: Check Homepage
      id: frontend-check
      uses: http
      with:
        url: https://myapp.example.com
        method: GET
        headers:
          User-Agent: "Probe Health Check"
      test: res.code == 200 && (rt.sec * 1000) < 3000
      outputs:
        homepage_response_time: (rt.sec * 1000)

    - name: Check Critical Page
      uses: http
      with:
        url: https://myapp.example.com/dashboard
        method: GET
      test: res.code == 200 || res.code == 302

    - name: Report Frontend Status
      uses: hello
      echo: "✅ Frontend is healthy ({{outputs.homepage_response_time}}ms)"
```

## Step 3: API Monitoring

Add a separate job for API monitoring:

```yaml
  api-check:
    name: API Health Check
    steps:
      - name: Check API Health Endpoint
        id: health-check
        uses: http
        with:
          url: https://api.myapp.example.com/health
          method: GET
          headers:
            Accept: "application/json"
        test: res.code == 200 && res.body.status == "healthy"
        outputs:
          api_version: res.body.version
          database_status: res.body.database

      - name: Test User Authentication
        uses: http
        with:
          url: https://api.myapp.example.com/auth/login
          method: POST
          headers:
            Content-Type: "application/json"
          body: |
            {
              "username": "healthcheck",
              "password": "{{vars.HEALTH_CHECK_PASSWORD}}"
            }
        test: res.code == 200 && res.body.token != nil
        outputs:
          auth_token: res.body.token

      - name: Test Authenticated Endpoint
        uses: http
        with:
          url: https://api.myapp.example.com/user/profile
          method: GET
          headers:
            Authorization: "Bearer {{outputs.auth_token}}"
        test: res.code == 200

      - name: Report API Status
        uses: hello
        echo: "✅ API v{{outputs.api_version}} is healthy"
```

## Step 4: Adding Dependencies

Make the API check depend on successful frontend check:

```yaml
  api-check:
    name: API Health Check
    needs: [frontend-check]  # Wait for frontend to be healthy
    steps:
      # ... existing steps
```

## Step 5: External Service Checks

Add checks for external services:

```yaml
  external-services:
    name: External Services Check
    steps:
      - name: Check Email Service
        uses: http
        with:
          url: https://api.sendgrid.com/v3/mail/send
          method: POST
          headers:
            Authorization: "Bearer {{vars.SENDGRID_API_KEY}}"
            Content-Type: "application/json"
          body: |
            {
              "from": {"email": "health@myapp.example.com"},
              "subject": "Health Check Test",
              "content": [{"type": "text/plain", "value": "Test"}],
              "personalizations": [{"to": [{"email": "test@myapp.example.com"}]}]
            }
        test: res.code == 202

      - name: Check Payment Gateway
        uses: http
        with:
          url: https://api.stripe.com/v1/charges
          method: GET
          headers:
            Authorization: "Bearer {{vars.STRIPE_SECRET_KEY}}"
        test: res.code == 200

      - name: Report External Services
        uses: hello
        echo: "✅ All external services are responding"
```

## Step 6: Error Handling and Notifications

Add error handling and notification logic:

```yaml
  notification:
    name: Send Notifications
    needs: [frontend-check, api-check, external-services]
    steps:
      - name: Success Notification
        uses: hello
        echo: |
          🎉 All systems are healthy!
          
          Frontend: ✅ ({{outputs['frontend-check'].homepage_response_time}}ms)
          API: ✅ v{{outputs['api-check'].api_version}}
          External Services: ✅
          
          Monitoring completed at {{unixtime()}}

      - name: Failure Notification
        uses: hello
        echo: |
          🚨 ALERT: System health check failed!
          
          
          Please investigate immediately.
```

## Complete Workflow

Here's the complete workflow file (`health-check.yml`):

```yaml
name: Web Application Health Check
description: Comprehensive monitoring for our web application stack

jobs:
- id: frontend-check
  name: Frontend Application Check
  steps:
    - name: Check Homepage
      id: frontend-check
      uses: http
      with:
        url: https://myapp.example.com
        method: GET
        headers:
          User-Agent: "Probe Health Check"
      test: res.code == 200 && (rt.sec * 1000) < 3000
      outputs:
        homepage_response_time: (rt.sec * 1000)

    - name: Check Critical Page
      uses: http
      with:
        url: https://myapp.example.com/dashboard
        method: GET
      test: res.code == 200 || res.code == 302

    - name: Report Frontend Status
      uses: hello
      echo: "✅ Frontend is healthy ({{outputs.homepage_response_time}}ms)"

- id: api-check
  name: API Health Check
  needs: [frontend-check]
  steps:
    - name: Check API Health Endpoint
      id: health-check
      uses: http
      with:
        url: https://api.myapp.example.com/health
        method: GET
        headers:
          Accept: "application/json"
      test: res.code == 200 && res.body.status == "healthy"
      outputs:
        api_version: res.body.version
        database_status: res.body.database

    - name: Test User Authentication
      id: api-check
      uses: http
      with:
        url: https://api.myapp.example.com/auth/login
        method: POST
        headers:
          Content-Type: "application/json"
        body: |
          {
            "username": "healthcheck",
            "password": "{{vars.HEALTH_CHECK_PASSWORD}}"
          }
      test: res.code == 200 && res.body.token != nil
      outputs:
        auth_token: res.body.token

    - name: Test Authenticated Endpoint
      uses: http
      with:
        url: https://api.myapp.example.com/user/profile
        method: GET
        headers:
          Authorization: "Bearer {{outputs.auth_token}}"
      test: res.code == 200

    - name: Report API Status
      uses: hello
      echo: "✅ API v{{outputs.api_version}} is healthy"

- id: external-services
  name: External Services Check
  steps:
    - name: Check Email Service
      uses: http
      with:
        url: https://api.sendgrid.com/v3/mail/send
        method: POST
        headers:
          Authorization: "Bearer {{vars.SENDGRID_API_KEY}}"
          Content-Type: "application/json"
        body: |
          {
            "from": {"email": "health@myapp.example.com"},
            "subject": "Health Check Test",
            "content": [{"type": "text/plain", "value": "Test"}],
            "personalizations": [{"to": [{"email": "test@myapp.example.com"}]}]
          }
      test: res.code == 202

    - name: Check Payment Gateway
      uses: http
      with:
        url: https://api.stripe.com/v1/charges
        method: GET
        headers:
          Authorization: "Bearer {{vars.STRIPE_SECRET_KEY}}"
      test: res.code == 200

    - name: Report External Services
      uses: hello
      echo: "✅ All external services are responding"

- id: notification
  name: Send Notifications
  needs: [frontend-check, api-check, external-services]
  steps:
    - name: Success Notification
      uses: hello
      echo: |
        🎉 All systems are healthy!
          
        Frontend: ✅ ({{outputs['frontend-check'].homepage_response_time}}ms)
        API: ✅ v{{outputs['api-check'].api_version}}
        External Services: ✅
          
        Monitoring completed at {{unixtime()}}

    - name: Failure Notification
      uses: hello
      echo: |
        🚨 ALERT: System health check failed!
          
          
        Please investigate immediately.
```

## Running the Workflow

The workflow reads its values from the environment, so those are set first, and the run can then be repeated with more detail when something needs checking.

### Set Environment Variables

First, set up your environment variables:

```bash
export HEALTH_CHECK_PASSWORD="your-test-password"
export SENDGRID_API_KEY="your-sendgrid-key"
export STRIPE_SECRET_KEY="your-stripe-key"
```

### Execute the Workflow

Run the workflow:

```bash
probe health-check.yml
```

### Use Verbose Mode for Debugging

For detailed output during development:

```bash
probe -v health-check.yml
```

## Making It Production-Ready

Three things separate this workflow from one that can be left running: configuration per environment, tolerance for transient failures, and a schedule.

### 1. Environment-Specific Configuration

Create environment-specific config files:

**production.yml:**
```yaml
# Override URLs for production
variables:
  frontend_url: https://app.mycompany.com
  api_url: https://api.mycompany.com
```

**staging.yml:**
```yaml
# Override URLs for staging
variables:
  frontend_url: https://staging.mycompany.com
  api_url: https://api-staging.mycompany.com
```

Run with environment-specific config:
```bash
probe health-check.yml,production.yml
```

### 2. Add Retry Logic

A check that runs unattended should tolerate a single transient failure.

```yaml
- name: Check Critical Service
  uses: http
  with:
    url: https://critical-service.example.com
    method: GET
    retry_count: 3
    retry_delay: 5s
  test: res.code == 200
```

### 3. Set up Monitoring Schedule

Use cron to run regularly:
```bash
# Add to crontab - run every 5 minutes
*/5 * * * * /usr/local/bin/probe /path/to/health-check.yml
```

## What You've Learned

In this guide, you've learned how to:

- ✅ Structure a multi-job workflow
- ✅ Use job dependencies with `needs`
- ✅ Pass data between steps using `outputs`
- ✅ Handle authentication in API calls
- Implement conditional logic with `skipif`
- ✅ Use environment variables for configuration
- ✅ Create comprehensive error handling
- ✅ Merge configuration files for different environments

## Next Steps

Ready to dive deeper? Here are your next steps:

1. **[Master the CLI](/guide/introduction/cli-basics)** - Learn all command-line options
2. **[Explore How-tos](/guide/how-tos/api-testing)** - See specific use cases and patterns
3. **[Browse the Reference](/reference/cli-reference)** - Deep dive into all available features

The workflow you've built is a solid foundation. You can extend it by adding more checks, integrating with monitoring systems, or customizing it for your specific application stack.
