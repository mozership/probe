# Performance Testing

Every step records how long its action took, and a test can hold that time to a limit. This guide shows how to put a limit on one response, how to take several samples of the same operation and put a limit on what they come to, and how to run a load test from a workflow with a tool made for it. Probe is not a load testing tool: it can send requests at a low, steady rate to take samples, and the last section says why it goes no further.

## What a Step Measures

Each step exposes the time its action took as `rt`:

| Field | Type | Description |
|-------|------|-------------|
| `rt.duration` | String | The time as written, such as `"120ms"` |
| `rt.sec` | Float | The same in seconds |

What the time spans is up to the action. Each step runs its action in a process of its own, so no connection is kept from one step to the next. The `http` action measures from sending the request to receiving the headers of the response: connecting, and the TLS handshake for `https`, are part of it, and reading the body is not. The `db` action measures from opening the connection to reading the last row. The `shell` action measures the command from start to exit. Compare times taken by the same action only.

## A Limit on One Response

A test can hold `rt.sec` to a limit like any other value of the response:

```yaml
name: Search Latency
vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"

jobs:
- name: Search
  steps:
  - name: Search by keyword
    id: search
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /search?q=probe
    test: res.code == 200 && rt.sec < 0.5
    outputs:
      elapsed_ms: rt.sec * 1000
```

One request is one sample. A request that waited on a cold cache, a garbage collection or a busy network takes many times as long as the one before it, so a limit close to the usual time fails now and then for no reason in the code. Set the limit well above the usual time, so that it catches a change that makes the operation slower many times over, such as a query that lost its index. To see how often the operation goes over a limit, rather than whether one request did, and how its time is spread, take more than one sample.

## Several Samples of One Operation

`repeat` runs a job again, and the test of each step holds in each run:

```yaml
name: Search Latency
vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"

jobs:
- name: Search
  repeat:
    count: 50
    interval: 100ms
  steps:
  - name: Search by keyword
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /search?q=probe
    test: res.code == 200 && rt.sec < 0.3
```

The result says in how many runs the test held, and how the times the action took are spread over the runs:

```
⏺ Search (Completed in 6.42s)
  ⎿ 0. Search by keyword (repeating 50 times)
        ✓  50/50 success (100.0%)
          min 8.1ms, p50 12.4ms, p90 31.0ms, p95 44.2ms, p99 118ms, max 118ms
```

A run in which the test does not hold fails the job, so a test on `rt.sec` here still demands that every sample be within the limit; [A Limit on the Runs](#a-limit-on-the-runs) shows how to allow a share of them over it. What the runs add is the count: `48/50 success (96.0%)` says that the test held in 48 of the 50 runs, where a single request could only pass or fail. The count is of the whole test, which a run fails either by getting a status other than `200` or by going over the time; the count does not say which, and the reason the first failing run failed is shown under the step.

The times are those of every run in which the action returned one, the runs whose test failed included, so that the slow runs are not left out of them. A percentile is the time that the given share of the runs took at most: `p95 44.2ms` says that 95 in 100 runs took 44.2ms or less. It is always one of the times taken, so with fewer than 100 runs `p99` is the slowest run, as `max` is. Unlike the count, the times show a change that makes the operation slower by a fraction while it stays within the limit: compare the `p50` and `p95` of runs before the change with those after it, made from the same machine against the same environment, since the time includes the network between them. `--report json` records the times as `latency` of the step, and `--report markdown` as a table of the repeated steps, which puts them in a pull request beside the result.

A service that has just started, or has had no requests for a while, tends to answer the first few slowly, since its caches are empty and its connection pools not yet filled. Those runs push up `max` and `p99`, and may fail the test. `warmup` runs that many first and counts them in neither the results nor the times:

```yaml
  repeat:
    count: 50
    interval: 100ms
    warmup: 5
```

A warmup run whose test fails does not fail the job; if the service does not answer at all, the runs counted after them fail. Only the service warms up: each step connects anew, so Probe reuses no connection.

By default the runs follow one another, and `interval` is the wait from the end of one run to the start of the next. With `async: true`, a run starts every `interval` whether or not the one before has ended, so the requests are sent at a steady rate, as clients that do not wait for each other send them:

```yaml
name: Search Latency
vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"

jobs:
- name: Search at 10 requests a second
  repeat:
    count: 100
    interval: 100ms
    async: true
  steps:
  - name: Search by keyword
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /search?q=probe
    test: res.code == 200 && rt.sec < 0.3
```

A run that waits for the one before measures each request on a service that is idle otherwise. At a steady rate, a slow response does not delay the requests behind it, so a service that falls behind shows it in the times of the requests that follow. Keep the rate low, for the reasons given under [Load Tests](#load-tests).

## A Limit on the Runs

`repeat.test` puts a limit on what the runs came to, rather than on each run. It is checked once every run is over, and decides alone whether the job passed:

```yaml
name: Search Latency
vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"

jobs:
- name: Search
  repeat:
    count: 100
    interval: 100ms
    warmup: 5
    test: steps.search.success_rate >= 0.99 && steps.search.rt.p95 < 0.3
  steps:
  - name: Search by keyword
    id: search
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /search?q=probe
    test: res.code == 200
```

`steps.search` is what the runs of the step with the id `search` came to: the counts `runs`, `success`, `failure` and `skipped`, the share `success_rate`, and under `rt` the times in seconds, as `rt.sec` is, with `min`, `mean`, `p50`, `p90`, `p95`, `p99` and `max`. This job passes when the step passed in at least 99 of 100 runs and 95 of 100 runs took less than 300ms. A run that failed is counted, and fails nothing by itself. The test of the step still decides whether a run counts as a success, so it checks the response, and the limit on the time is in `repeat.test`, where it holds a share of the runs rather than each of them.

The result shows the test under the steps, and when it is false, which condition is false and what it compared:

```
⏺ Search (Failed in 11.20s)
  ⎿ 0. Search by keyword (repeating 100 times, after 5 to warm up)
        △  99/100 success (99.0%)
          min 8.3ms, p50 12.1ms, p90 30.2ms, p95 312ms, p99 640ms, max 702ms
        ✗  repeat test: steps.search.success_rate >= 0.99 && steps.search.rt.p95 < 0.3
           failed: steps.search.rt.p95 < 0.3
             steps.search.rt.p95 = 0.312
```

The job fails, and the exit status is 1, only when the test does not hold, whatever the failed runs failed for. A limit read from `vars`, as `steps.search.rt.p95 < vars.p95_limit`, can be set for each environment. `probe check` reports a `repeat.test` that reads a step the job does not have, or a field a step does not have. See [Test over the Runs](/reference/yaml-configuration#test-over-the-runs) for every field.

## Compared to an Earlier Run

A limit in seconds is hard to set for CI. The runners differ in speed from one run to the next, so a strict limit fails changes that made nothing slower, and a loose one lets through a change that did. What a pull request needs to show is that it is no slower than the branch it goes into, and `--baseline` shows that: it compares the times of this run to those of the report an earlier run wrote with `--report json`.

```bash
# on the main branch
probe --report json=main.json search.yml
# on the change
probe --report json,markdown --baseline main.json search.yml
```

The Markdown report then has a table of how the times changed:

```
## Compared to the Baseline

Compared to the run started at 2026-10-15T09:00:00Z (2f9159ffd442ca6c), before → now.

| Job | Step | Runs | p50 | p95 | p99 | Success |
|---|---|---|---|---|---|---|
| Search | 0. Search by keyword | 100 → 100 | 12.1ms → 13.0ms (+7.4%) | 30.2ms → 41.5ms (+37.4%) | 64.0ms → 70.2ms (+9.7%) | 100.0% → 100.0% |
```

A step is compared to the step of the same `id`, in the job of the same `id`, so give both an `id`: one without is known by where it is written in its job, and adding a step before it makes it another. A step the earlier run has none of is marked `no baseline`.

The comparison is only shown. It fails no job, because the times of two runs differ even when nothing changed, and how much they differ depends on the service and the runners. Two runs on the same machine, one after the other, differ the least, so in CI, run the main branch in the same job as the change rather than keeping its report from another run. With `--report github-summary` the table is on the summary page of the run. See [`--baseline`](/reference/cli-reference#--baseline) for what the JSON report records.

## The Query Behind an Endpoint

When an endpoint is slow, the query it runs is a common cause. The `db` action runs that query by itself, which tells whether the time is spent in the database:

```yaml
name: Order History
vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"
  db_dsn: "{{DB_DSN}}"

jobs:
- name: Order history of one user
  steps:
  - name: Endpoint
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /users/42/orders
    test: res.code == 200 && rt.sec < 1
  - name: Query behind it
    uses: db
    with:
      dsn: "{{vars.db_dsn}}"
      query: SELECT * FROM orders WHERE user_id = ? ORDER BY created_at DESC LIMIT 50
      params: [42]
    test: res.code == 0 && rt.sec < 0.2
```

The `?` placeholder is that of MySQL and SQLite; PostgreSQL takes `$1`.

The two times are not of the same thing, so each step has a limit of its own rather than one being compared with the other. The time of the `http` step runs from connecting to receiving the headers of the response, and that of the `db` step from connecting to the database to reading the last row, as [What a Step Measures](#what-a-step-measures) says. The step connects anew each time, which the service may not do for its own queries; for a query that takes a few milliseconds, connecting is most of the time of the step. If the query stays within its limit while the endpoint does not, the time is likely spent elsewhere in the service.

## Response Times over Days

A limit checked once a day by cron or CI shows a service getting slower over time, which no single run shows. A workflow run on a schedule is described in [Monitoring Workflows](/guide/how-tos/monitoring-workflows). Its exit status tells the scheduler whether the limit held. A report file is replaced by the next run that writes to the same path, so to keep a record of each run, give each its own path, as `--report json=reports/$(date +%F).json` does.

## Load Tests

Probe starts a process for the action of every step, a built-in one included, and evaluates the templates, tests and outputs of each run in the process that starts them. A step that sends one request takes tens of milliseconds of its own, and `repeat` takes fewer than 10000 runs unless `PROBE_MAX_REPEAT_COUNT` raises the limit. Probe reaches its own limit at a rate a service usually handles, and the times it records do not say whether it or the service was the limit. A load test needs a tool that is made to send many requests at once and to record their times, such as [k6](https://k6.io/).

A workflow can run that tool as a step, and check the service before and after it:

```yaml
name: Checkout under Load
vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"

jobs:
- name: Checkout under load
  steps:
  - name: Service is up
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /health
    test: res.code == 200
  - name: Load with k6
    uses: shell
    timeout: 15m
    with:
      cmd: k6 run --env API_URL="$API_URL" load/checkout.js
      env:
        API_URL: "{{vars.api_url}}"
    test: res.code == 0
  - name: Orders were all written
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /orders/count
    test: res.code == 200 && res.body.failed == 0
```

k6 exits with a status other than `0` when one of the thresholds of the script does not hold, which fails the step. The steps around it check what the load did to the data, which k6 does not see; they run whether or not k6 passed, since a failed threshold is when that is most worth knowing. The URL is given to k6 through the environment of the command rather than written into it, so that no value of `API_URL` can change the command.

## What's Next?

- **[Monitoring Workflows](/guide/how-tos/monitoring-workflows)** - Run a workflow on a schedule
- **[Environment Management](/guide/how-tos/environment-management)** - Point the same workflow at another environment
- **[Error Handling Strategies](/guide/how-tos/error-handling-strategies)** - Retries, timeouts and what to do when a step fails
