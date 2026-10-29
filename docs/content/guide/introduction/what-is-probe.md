---
title: What is Probe?
lang: en-US
---

# What is Probe?

Probe is a free, open-source tool that checks that software works end to end. A workflow written in YAML drives a running service over the protocols its clients use, such as HTTP, gRPC, SQL and mail, and checks every response. Probe is a single binary that runs the same on your machine, in CI and from cron.

## Why It Exists Now

With AI agents writing code, changes come faster and in greater numbers. Code and unit tests that pass do not show that a user can do what they came to do, because the path a user takes, the customer journey, crosses pages, APIs, databases and mail. Whether a change keeps that journey working has to be checked on the running service, along the same path: the form a user fills in, the row it writes, the mail it sends.

Probe is made to repeat that check on every change, and to be written by an agent as well:

- `probe skill` teaches the agent to write a workflow for the version installed, and `probe check` finds its mistakes before anything runs.
- A step can be checked against your OpenAPI document or `.proto` files, so the spec decides what is right, not the `test` the agent wrote.
- `--read-only` and `--allow-host` refuse writes, and connections to hosts you did not list, before they are sent.
- The report says, step by step, what the service did, for the person who reviews the change.

Here an end-to-end test that an AI agent wrote as a workflow is checked, then run against a sign-up API and its database, then run again under `--read-only`:

<img class="diagram" src="/demo.svg" alt="A terminal in three scenes: probe check reports the mistakes in an end-to-end test an AI agent wrote, the fixed test calls an API and queries the row it wrote, and under --read-only its POST is refused." />

[Working with coding agents](/guide/how-tos/coding-agents) sets a project up this way.

## What Can It Do?

A workflow is a file, so the one that checks a change is also the one that runs later:

- **End-to-end testing**: call an endpoint, query the row it wrote and read the mail it sent, in one run
- **API testing**: check requests and responses against assertions and against an OpenAPI document
- **Monitoring**: run the same workflow on an interval with `repeat`, and report a success rate
- **Smoke tests and health checks**: stop a deployment with the exit status
- **Reproducible experiments**: write down a procedure across many protocols, and run it again the same way

## Key Features

These are what set Probe apart from other tools of its kind; [Compared with similar tools](/guide/introduction/comparison) says where another one is the better answer.

### Many Protocols in One Run
HTTP, gRPC, MySQL, PostgreSQL, SQLite, SMTP, IMAP, DNS, SSH and shell are built in, and jobs run in parallel unless `needs` orders them.

### Readable
A workflow is YAML close to GitHub Actions: `jobs`, `steps`, `uses` and `with`. A person can review what an agent wrote.

### Ready for Coding Agents
The skill, `probe check`, the guard and contract checks are part of the binary.

### Single Binary
Developed in Go, with no runtime dependencies. One command installs it.

### Extensible
A new protocol is added as an action of its own, without changing Probe.

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

⏺ My First Job (Completed in 1.07s)
  ⎿  0. ✓  🕐︎1s → Say Hello
           Hello, World!

Total workflow time: 1.07s ✓ All jobs succeeded
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
    test: res.code == 200 && res.body != nil
```

As you can see, Probe enables you to create and execute workflows in a **simple**, **quick**, and **enjoyable** way.

## Available Actions

These actions are built in. Each has a page in the [reference](/reference/actions-reference).

| Action | Use it for |
|---|---|
| [`http`](/reference/actions/http) | HTTP requests and their responses |
| [`grpc`](/reference/actions/grpc) | gRPC and Connect calls, streaming included |
| [`db`](/reference/actions/db) | Queries against MySQL, PostgreSQL and SQLite |
| [`smtp`](/reference/actions/smtp) | Delivering mail |
| [`imap`](/reference/actions/imap) | Reading a mailbox |
| [`dns`](/reference/actions/dns) | The records a DNS server answers for a name |
| [`ssh`](/reference/actions/ssh) | Commands on a remote host |
| [`shell`](/reference/actions/shell) | Commands on the machine running Probe |
| [`embedded`](/reference/actions/embedded) | Running another job file from a step |
| [`hello`](/reference/actions/hello) | Printing a report line |

External actions live in repositories of their own, and a step pins one by commit: [agent](/reference/actions/agent), [android](/reference/actions/android), [browser](/reference/actions/browser), [container](/reference/actions/container), [graphql](/reference/actions/graphql), [ios](/reference/actions/ios), [jmap](/reference/actions/jmap), [mail-latency](/reference/actions/mail-latency), [redis](/reference/actions/redis), [s3](/reference/actions/s3) and [websocket](/reference/actions/websocket).

### Custom Actions

When no action speaks the protocol you need, you can write one in Go and publish it as an external action. See [Actions](/guide/concepts/actions).

## Let's Get Started

Getting started with Probe is easy. You can create and run workflows immediately with these three steps:

1. **[Installation](/guide/introduction/installation)** - Install Probe with one command
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
