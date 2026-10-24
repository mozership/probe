---
title: CLI Basics
description: Master the Probe command-line interface
weight: 50
---

# CLI Basics

The Probe command-line interface (CLI) is your primary way to interact with Probe. This guide covers all the essential commands, options, and techniques you need to master.

## Basic Usage

The most basic way to run Probe is:

```bash
probe workflow.yml
```

This executes the workflow defined in `workflow.yml` and displays the results.

## Command Syntax

Options come first, then the workflow file.

```bash
probe [options] <workflow-file>
```

- **`workflow-file`**: Path to your YAML workflow file (required)
- **`options`**: Various flags to modify behavior (optional)

## Core Options

The options below change what a run prints and in what form. None of them change which steps run.

### Help and Information

Get help about available options:

```bash
probe --help
# or
probe -h
```

Check the installed version:

```bash
probe --version
```

### Verbose Output

Enable detailed logging to see what's happening under the hood:

```bash
probe --verbose workflow.yml
# or
probe -v workflow.yml
```

Verbose mode shows:
- Detailed HTTP request/response information
- Step execution timing
- Variable resolution details
- Log records from actions

**Example verbose output** (`…` marks what is left out here):
```
[DEBUG] --- Step 0: Check Homepage
[DEBUG] Request:
[DEBUG]   body: ""
[DEBUG]   headers:
[DEBUG]     Accept: */*
[DEBUG]     User-Agent: probe-http/1.0.0
[DEBUG]   method: GET
[DEBUG]   timeout: 30s
[DEBUG]   url: https://example.com/
[DEBUG] Response:
[DEBUG]   body: <!doctype html><html lang=en>…
[DEBUG]   code: 200
[DEBUG]   cookies: {}
[DEBUG]   filepath: ""
[DEBUG]   headers:
[DEBUG]     Content-Type: text/html; charset=utf-8
[DEBUG]     …
[DEBUG]   status: 200 OK
[DEBUG] RT: 245ms
[DEBUG] Test: Success (input: res.code == 200)
[DEBUG] Env:
[DEBUG]   outputs: {}
[DEBUG]   repeat_index: 0
[DEBUG]   report: ""
[DEBUG]   rt:
[DEBUG]     duration: 245ms
[DEBUG]     sec: 0.245
[DEBUG]   status: 0
[DEBUG]   vars: {}
```

### Timing Display

Show timing information (start time and response time):

```bash
probe --timing workflow.yml
```

This adds timing information to the output without the full verbosity of `--verbose`.

### Report Output Mode

Control how the report reaches you while jobs are running:

```bash
probe --output stream workflow.yml
```

- `auto` (default): spinner on an interactive terminal, stream otherwise
- `spinner`: show a spinner and print the whole report once the run finishes
- `stream`: print each job block to stdout as soon as that job is final, and log
  step progress to stderr while jobs are still running

`stream` is what you want on CI, where the spinner is never rendered and the log
would otherwise stay silent until the whole workflow finishes. Job blocks are
held back until every job declared before them has been printed, so the report
on stdout is identical to the one `spinner` produces. The step progress lines go
to stderr, so redirecting stdout still gives you a clean report.

The mode can also be set with the `PROBE_OUTPUT` environment variable; the flag
takes precedence over it.

### Report Files

The terminal report is for reading. To keep the result for a CI system, a
script or a later look, write it to files as well:

```bash
probe --report junit,markdown workflow.yml
```

- `json`: the whole run, including the request and response of every failed
  step, written to `probe-report.json`
- `junit`: JUnit XML that CI systems display as test results, written to
  `probe-junit.xml`
- `markdown`: a summary, a table of jobs and a section per failed step, written
  to `probe-report.md`
- `github-summary`: the same page, appended to the GitHub Actions job summary;
  outside GitHub Actions it is skipped with a warning

Give a path after `=` to write a format somewhere else, for example
`--report junit=out/junit.xml`. The files are written once every job has
finished, and the terminal report does not change. The list can also come from
the `PROBE_REPORT` environment variable; the flag takes precedence over it.

### Combining Options

You can combine multiple options:

```bash
probe -v --timing workflow.yml
```

## Multiple File Merging

One of Probe's powerful features is the ability to merge multiple YAML files:

```bash
probe base.yml,overrides.yml
```

### Use Cases for File Merging

**1. Environment-specific configuration:**

**base-workflow.yml:**
```yaml
name: API Health Check
jobs:
- id: health-check
  name: health-check
  steps:
    - name: Check API
      uses: http
      with:
        url: "{{vars.API_URL}}"
        method: GET
      test: res.code == 200
```

**production.yml:**
```yaml
# Production-specific settings
vars:
  API_URL: https://api.production.example.com
```

**staging.yml:**
```yaml
# Staging-specific settings
vars:
  API_URL: https://api.staging.example.com
```

Run for different environments:
```bash
# Production
probe base-workflow.yml,production.yml

# Staging
probe base-workflow.yml,staging.yml
```

**2. Shared configuration:**

**common-config.yml:**
```yaml
# Shared header block, referenced from the workflow by its anchor
shared:
  common_headers: &common_headers
    User-Agent: "Probe Health Check v1.0"
```

**api-check.yml:**
```yaml
name: API Monitoring
jobs:
- name: API checks
  defaults:
    http:
      headers:
        <<: *common_headers
  steps:
    # ... step definitions
```

```bash
probe common-config.yml,api-check.yml
```

**3. Override specific values:**

```bash
probe workflow.yml,local-overrides.yml
```

The merge order matters - later files override values from earlier files.

## Working with Environment Variables

Probe can access environment variables in your workflows using the `env` object:

```yaml
steps:
  - name: Connect to Database
    uses: http
    with:
      method: GET
      url: "{{vars.DATABASE_URL}}"
      headers:
        Authorization: "Bearer {{vars.API_TOKEN}}"
```

Set environment variables before running:

```bash
export DATABASE_URL="https://db.example.com"
export API_TOKEN="your-secret-token"
probe workflow.yml
```

## Exit Codes

The exit code tells apart what went wrong:

- **`0`**: Success - all jobs completed successfully
- **`1`**: A test failed
- **`2`**: The workflow or the command line is wrong, such as a missing file, invalid YAML or an unknown flag
- **`3`**: An action returned an error, such as a refused connection or a timeout

When several kinds of failure happen in one run, `2` wins over `3`, and `3` over `1`.

This makes Probe perfect for use in scripts and CI/CD pipelines:

```bash
#!/bin/bash
if probe health-check.yml; then
    echo "✅ Health check passed"
    deploy_application
else
    echo "❌ Health check failed"
    exit 1
fi
```

## Real-World Examples

The same binary covers CI, scheduled monitoring, local development and load checks. The only difference is how it is invoked.

### 1. CI/CD Integration

The exit status is what the pipeline branches on.

```bash
# In your CI/CD pipeline
probe smoke-tests.yml
if [ $? -eq 0 ]; then
    echo "Smoke tests passed, proceeding with deployment"
else
    echo "Smoke tests failed, aborting deployment"
    exit 1
fi
```

### 2. Cron Job Monitoring

The same workflow, run on a schedule, becomes monitoring.

```bash
# In crontab - run every 5 minutes
*/5 * * * * /usr/local/bin/probe /opt/monitoring/health-check.yml >> /var/log/probe.log 2>&1
```

### 3. Development Testing

During development a local configuration is merged over the shared one, and `-v` shows each step.

```bash
# Quick test during development
probe -v api-tests.yml,local-config.yml
```

### 4. Load Testing

`--timing` is what makes the per-step durations visible.

```bash
# Run performance tests with timing
probe --timing --verbose load-test.yml
```

## Output Interpretation

Understanding Probe's output helps you quickly identify issues:

### Successful Execution

Each job is listed with its steps and the time it took.

```
My Health Check
Monitoring application health

⏺ Frontend Check (Completed in 0.45s)
  ⎿ 0. ✔︎  Check Homepage (234ms)
     1. ✔︎  Check API Health (156ms)

Total workflow time: 0.45s ✔︎ All jobs succeeded
```

**Key indicators:**
- **⏺**: Job completed
- **✔︎**: Step succeeded
- **Green text**: Success status
- **Total time**: Overall execution time

### Failed Execution

A failed step is marked, and the test that did not hold is printed with it.

```
My Health Check
Monitoring application health

⏺ Frontend Check (Failed in 1.23s)
  ⎿ 0. ✘  Check Homepage (1.23s)
              failed: res.code == 200
                res.code = 500
              request:
                method: GET
                url: https://example.com
              response:
                body: Internal Server Error
                code: 500
     1. ⏭  Check API Health (skipped)

Total workflow time: 1.23s ✘ 1 job(s) failed
```

**Key indicators:**
- **✘**: Step failed
- **⏭**: Step skipped (due to previous failure)
- **Red text**: Failure status
- **Request/Response**: Debug information for failed HTTP requests

### Partial Success

Jobs are reported separately, so one failing job does not hide the ones that passed.

```
Multi-Service Check
Checking multiple services

⏺ Critical Services (Completed in 0.67s)
  ⎿ 0. ✔︎  Database Check (234ms)
     1. ✔︎  API Check (156ms)

⏺ Optional Services (Failed in 2.34s)
  ⎿ 0. ✘  External API Check (2.34s)

Total workflow time: 2.34s ✘ 1 job(s) failed
```

## Troubleshooting Common Issues

Most problems at the command line come from the file, not the workflow: where it is, how it parses, and what it is allowed to reach.

### File Not Found

This appears when no file was given, or the path does not exist.

```
[ERROR] workflow is required
```

**Solution:** Make sure you're providing a valid file path:
```bash
probe ./workflows/health-check.yml
```

### YAML Syntax Errors

The message names the line the parser stopped at.

```
[ERROR] yaml: line 5: mapping values are not allowed in this context
```

**Solution:** Check your YAML syntax:
- Use spaces, not tabs for indentation
- Ensure proper key-value syntax with colons
- Quote strings containing special characters

### Permission Errors

The file exists but the current user cannot read it.

```
[ERROR] permission denied
```

**Solution:** Ensure the workflow file is readable:
```bash
chmod +r workflow.yml
```

### Network Timeouts

If steps hang or timeout:
- Use `--verbose` to see detailed network information
- Check network connectivity to target URLs
- Consider adding timeout values in your HTTP actions

## Best Practices

The points below cover naming and arranging workflow files so that the command line stays short and the run is reproducible.

### 1. Use Descriptive File Names

The file name is what appears in cron entries and CI logs, so it should say what the workflow checks.

```bash
# Good
probe api-health-check.yml
probe database-migration-test.yml

# Not so good
probe test.yml
probe workflow.yml
```

### 2. Organize with Directories

Grouping by purpose keeps the merge arguments short.

```bash
# Organize workflows by purpose
probe monitoring/health-check.yml
probe deployment/smoke-tests.yml
probe maintenance/cleanup.yml
```

### 3. Use Configuration Files

Keep secrets and environment-specific values in separate files:

```bash
probe workflow.yml,configs/production.yml
```

### 4. Validate Before Production

Always test with verbose mode first:

```bash
# Test thoroughly
probe -v new-workflow.yml,test-config.yml

# Deploy to production
probe new-workflow.yml,production-config.yml
```

## What's Next?

Now that you've mastered the CLI basics, you can:

1. **[Explore How-tos](/guide/how-tos/api-testing)** - Learn specific patterns and use cases
2. **[Check the Reference](/reference/cli-reference)** - Deep dive into all available options
3. **[Try the Tutorials](/guide/tutorials/api-testing-pipeline)** - Follow step-by-step guides for common scenarios

The CLI is your gateway to Probe's power. With these basics mastered, you're ready to build sophisticated monitoring and automation workflows.
