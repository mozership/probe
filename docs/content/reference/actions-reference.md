# Actions Reference

Every step names an action in `uses`. Each action has its own page below, with
its parameters, the `res` object it returns, and worked examples. The rules that
hold for all of them are at the end of this page.

## Built-in actions

- **[http](/reference/actions/http)** - Make HTTP/HTTPS requests and validate responses
- **[db](/reference/actions/db)** - Execute database queries on MySQL, PostgreSQL, and SQLite
- **[shell](/reference/actions/shell)** - Execute shell commands and scripts securely
- **[ssh](/reference/actions/ssh)** - Run commands on a remote host over SSH
- **[smtp](/reference/actions/smtp)** - Send email notifications and alerts
- **[imap](/reference/actions/imap)** - Connect to IMAP servers and manage email operations
- **[grpc](/reference/actions/grpc)** - Call gRPC services by reflection
- **[dns](/reference/actions/dns)** - Ask a DNS server for the records of a name
- **[embedded](/reference/actions/embedded)** - Run another workflow as a step
- **[hello](/reference/actions/hello)** - Simple test action for development and debugging

## External actions

These are published alongside Probe, each in a repository of its own. A step names one by repository and commit, and Probe downloads it the first time a workflow uses it. See [External Actions](/guide/concepts/actions#external-actions).

- **[agent](/reference/actions/agent)** - Test a web application from a task written in natural language, with a model that uses it through a real Chrome
- **[android](/reference/actions/android)** - Drive an app on an Android emulator or device through adb
- **[browser](/reference/actions/browser)** - Drive a real Chrome with chromedp
- **[container](/reference/actions/container)** - Start a container for the steps of a run, and remove it once the run is over
- **[graphql](/reference/actions/graphql)** - Send a GraphQL query over HTTP
- **[ios](/reference/actions/ios)** - Drive an app on an iOS simulator through XCUITest
- **[jmap](/reference/actions/jmap)** - Call JMAP methods
- **[mail-latency](/reference/actions/mail-latency)** - Measure delivery latency from a Maildir
- **[redis](/reference/actions/redis)** - Run commands on a Redis or Valkey server
- **[s3](/reference/actions/s3)** - Read and write objects in S3 and S3-compatible storage
- **[websocket](/reference/actions/websocket)** - Send and receive messages over a WebSocket connection

## Action Error Handling

A step fails when its `test` is false or when the action itself returns an error. The remaining steps of the job still run, the job is marked failed, and jobs that list it in `needs` are skipped.

There is no switch to ignore a failure. When a check should not fail the workflow, record its result as an output instead of asserting it.

```yaml
steps:
  - name: Required check
    uses: http
    with:
      method: GET
      url: "{{vars.api_url}}/health"
    test: res.code == 200

  - name: Optional check
    id: optional
    uses: http
    with:
      method: GET
      url: "{{vars.api_url}}/experimental"
    outputs:
      available: res.code == 200
      detail: 'res.code >= 400 ? res.status : ""'

  - name: Report
    uses: hello
    echo: "Experimental endpoint: {{outputs.optional.available ? \"available\" : outputs.optional.detail}}"
```

Use `retry` for a transient failure and `timeout` for a step that may hang:

```yaml
  - name: Flaky endpoint
    uses: http
    timeout: 10s
    retry:
      max_attempts: 3
      interval: 2s
    with:
      method: GET
      url: "{{vars.api_url}}/flaky"
    test: res.code == 200
```

## Performance Considerations

- Jobs without a `needs` relation run in parallel, so independent checks do not queue behind each other.
- `rt.sec` and `rt.duration` measure the action's round trip, not the whole step. Every step runs its action in a process of its own, so the round trip of an `http` step includes connecting.
- A large response body is held in memory; a binary body is written to a file and reported as `res.filepath`.
- `repeat` with `async: true` starts a run of a job every `interval`, without waiting for the one before, which sends requests at a steady rate. It is for taking samples, not for load tests; see [Performance Testing](/guide/how-tos/performance-testing#load-tests).

```yaml
vars:
  api_url: "{{API_URL}}"
jobs:
  - name: Ping at 10 requests a second
    repeat:
      count: 50
      interval: 100ms
      async: true
    steps:
      - name: Ping
        uses: http
        timeout: 2s
        with:
          method: GET
          url: "{{vars.api_url}}/ping"
        test: res.code == 200 && rt.sec < 0.5
```

## See Also

- **[YAML Configuration](/reference/yaml-configuration)** - Complete YAML syntax reference
- **[Built-in Functions](/reference/built-in-functions)** - Expression functions for use with actions
- **[SSH Action](/reference/actions/ssh)** - Running commands on a remote host
- **[Concepts: Actions](/guide/concepts/actions)** - Action system architecture
- **[How-tos: API Testing](/guide/how-tos/api-testing)** - Practical HTTP action examples
