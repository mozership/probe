---
title: What is Probe?
lang: en-US
---

# What is Probe?

Probe is a free, open-source tool that allows you to declaratively define workflows in YAML. It operates as a lightweight, single binary without requiring complex installation or configuration.

## What Can It Do?

Probe can be utilized for the following purposes:

- **API Testing**: Automated end-to-end testing of REST APIs
- **Website Monitoring**: Monitoring site uptime and performance
- **Health Checks**: System and service health verification
- **Data Processing**: Periodic data retrieval and transformation
- **Notification Sending**: Automated notifications to email, Slack, etc.
- **Task Automation**: Automation of repetitive tasks

## Key Features

Here are the five main features that make Probe the preferred choice.

### Simple and Intuitive
Create workflows easily with YAML-based configuration, no programming knowledge required.

### Lightweight and Fast
Developed in Go as a single binary, runs without dependencies.

### Extensible
Custom actions can be added without changing Probe itself.

### Concurrent Execution
Efficiently processes workflows by running multiple jobs in parallel.

### Rich Features
Equipped with practical features including conditional branching, data sharing, error handling, and environment variable support.

## Simple Workflow Examples

Let's understand how to use Probe through actual code examples.

### Basic Hello World Workflow

Let's start with the simplest example. Create the following YAML file:

```yaml
name: Hello World Workflow

jobs:
- name: My First Job
  steps:
  - name: Say Hello
    uses: hello
    wait: 1s
    echo: "Hello, World!"
```

Save this workflow to a file and run it:

```bash
$ probe hello-world.yml
Hello World Workflow

⏺ My First Job (Completed in 1.04s)
  ⎿  0. ▲  1s → Say Hello
           Hello, World!

Total workflow time: 1.07s ✔︎ All jobs succeeded
```

### More Practical Example: API Monitoring

Here's a real-world example of a workflow that monitors API uptime:

```yaml
name: API Health Check

jobs:
- name: Monitor API Health
  defaults:
    http:
      url: https://httpbin.org
  steps:
  - name: Check Homepage
    uses: http
    with:
      get: /status/200
    test: res.code == 200

  - name: Check API Response
    uses: http
    with:
      get: /json
    test: res.code == 200 && res.body != null
```

As you can see, Probe enables you to create and execute workflows in a **simple**, **quick**, and **enjoyable** way.

## Available Actions

Probe comes with the following built-in actions that you can use immediately. Here are the currently available actions and future development plans.

### HTTP Action
Used for REST API calls, website monitoring, and API testing.
- Supports HTTP methods like GET, POST, PUT, DELETE
- Configurable request headers and body
- Response validation and testing capabilities

### SMTP Action
Used for notifications and reporting via email sending.
- Support for HTML/text emails
- Email sending via SMTP server
- Attachment sending capabilities

### Shell Action
Enables integration with external software through shell command execution.
- Execute commands in any shell such as bash or zsh
- Configurable working directory and environment variables
- Retrieve exit codes, stdout, and stderr
- Control execution time with timeout settings

### Browser Action
An external action, published in [mozership/probe-browser](https://github.com/mozership/probe-browser); it was built in up to v1.21.0. See [Browser](/reference/actions/browser).
Control Chrome using Chrome DevTools Protocol.
- Automate page loading, element clicking, text input, etc.
- Screenshot capture functionality
- Wait for element visibility/invisibility
- Choose between headless and full browser modes

### Embedded Job Action
Simplify workflow management by sharing and reusing jobs.
- Call jobs defined in other workflows
- Parameter passing capabilities
- Split and reuse complex workflows
- Efficient workflow design based on DRY principles

### Database Action
Perform database operations.
- Support for MySQL, PostgreSQL, and SQLite
- Execute SQL operations like SELECT, INSERT, UPDATE, DELETE
- Safe query execution using prepared statements
- Retrieve result row count and affected row count

> [!TIP]
>
> **Planned Actions for Development**
>
> - **SSH Action** - Execute commands on remote servers
> - **gRPC Action** - Communication with gRPC services
> - **FTP Action** - File transfer with FTP servers
> - **IMAP Action** - Email reception and processing

### Custom Actions

When built-in actions don't meet your requirements, you can develop **custom actions**. They can be implemented in Go, as external actions or built into your own build of Probe.

## Let's Get Started

Getting started with Probe is easy. You can create and run workflows immediately with these three steps:

1. **[Installation](/guide/introduction/installation)** - Install Probe using the method appropriate for your environment
2. **[Quick Start](/guide/introduction/quickstart)** - Create and run your first workflow in 5 minutes
3. **[Understanding Probe](/guide/introduction/understanding-probe)** - Learn about workflows, jobs, and steps

## Use Cases

Probe can be applied across various business domains. Here are the main use cases.

### DevOps & SRE
- Smoke testing in CI/CD pipelines
- Service monitoring and alerting
- Infrastructure health checks

### API Development & Testing
- REST API integration testing
- Performance testing
- End-to-end test automation

### Operations & Maintenance
- Regular health checks
- Inter-system integration verification
- Automated recovery processes during failures

## Need Help?

Here are the support resources available when you encounter problems or have questions while using Probe.

### Documentation
This website provides comprehensive documentation covering everything from basic usage to advanced features.

### Community Support
Use [GitHub Discussions](https://github.com/mozership/probe/discussions) for questions, feedback, and idea discussions. Feel free to start a discussion.

### Bug Reports
If you discover bugs, please report them at [GitHub Issues](https://github.com/mozership/probe/issues).

## Support the Project

If this project has been helpful, please consider supporting it! Your support provides great encouragement to the developers.

- Give a [star on GitHub](https://github.com/mozership/probe)
- Tweet about it on X and introduce it to friends
- Write about it in blogs or articles
- Suggest ideas for feature improvements

## Contributing to Open Source

Interested in contributing to open source projects? Now's your chance! We welcome all forms of contribution including code contributions, documentation improvements, and bug reports.
