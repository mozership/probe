# GraphQL Action

The GraphQL action sends a GraphQL query or mutation over HTTP and returns the `data` and `errors` of the response apart.

It is an [external action](/guide/concepts/actions#external-actions), published in [mozership/probe-graphql](https://github.com/mozership/probe-graphql). Probe downloads it the first time a workflow uses it, and runs the executable whose SHA-256 the `action.yml` at the pinned commit names. It needs Probe v1.17.0 or later, and v1.21.0 or later to run it under a guard and to have `probe check` check its `with`.

## Basic Syntax

A step pins the action by a full commit SHA. The notes of each [release](https://github.com/mozership/probe-graphql/releases) start with the `uses` line to copy.

```yaml
steps:
  - name: Look up Japan
    uses: github.com/mozership/probe-graphql@49da1d40b2e4f25a241aba4783abaeeb03c1f470 # v0.2.1
    with:
      url: https://countries.trevorblades.com/graphql
      query: |
        query Country($code: ID!) {
          country(code: $code) { name capital currency }
        }
      variables:
        code: JP
    test: res.code == 200 && len(res.errors) == 0 && res.data.country.capital == "Tokyo"
```

## Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `url` | String | Yes | - | GraphQL endpoint, `http` or `https` |
| `query` | String | Yes | - | The query or mutation document |
| `variables` | Object | No | - | Values for the variables the document declares |
| `operation_name` | String | No | - | The operation to run when the document has more than one |
| `headers` | Object | No | - | Request headers, such as `Authorization`. They override the defaults below |
| `timeout` | Duration | No | `30s` | Time limit for the request, as `10s` or a number of seconds. `0` removes it |

A key of `with` that is not one of these fails the step before anything is sent. Its `action.yml` declares them as `params`, so `probe check` reports such a key with its line.

The request is a `POST` with a JSON body, sent with `Content-Type: application/json`, `Accept: application/graphql-response+json, application/json` and `User-Agent: probe-graphql/<version>`.

## Response Object

| Property | Type | Description |
|----------|------|-------------|
| `res.code` | Integer | HTTP status code |
| `res.status` | String | HTTP status line, such as `"200 OK"` |
| `res.headers` | Object | Response headers, keyed by canonical name |
| `res.data` | Any | The `data` of the response, or `null` |
| `res.errors` | Array | The `errors` of the response; empty when there are none |
| `res.body` | Any | The whole response body, parsed when it is JSON, otherwise the raw string |
| `res.rawbody` | String | The unparsed body, present when the body is JSON |
| `req` | Object | The `url`, `query`, `variables`, `operation_name` and `headers` that were sent |
| `rt` | Duration | Round-trip time |
| `status` | Integer | `0` when the status code is 2xx, the body is JSON and `errors` is empty; `1` otherwise |

Any response the server sends is a result, so a test can assert on a GraphQL error or a 500. Only a request that gets no response, such as a refused connection or a timeout, fails the step as an error.

```yaml
steps:
  - name: An unknown field is reported in res.errors
    uses: github.com/mozership/probe-graphql@49da1d40b2e4f25a241aba4783abaeeb03c1f470 # v0.2.1
    with:
      url: https://countries.trevorblades.com/graphql
      query: '{ country(code: "JP") { nope } }'
    test: status == 1 && len(res.errors) > 0
```

## Under a Guard

The action keeps to the guard of the run, and its `action.yml` declares `guard: [read-only, allow-host]`, so Probe runs it under either without `--allow-action`. A step it refuses fails with the kind `refused` before anything is sent.

- Under `--read-only`, only a query is sent. The operation to run, the one `operation_name` names or the only one in the document, is read with a GraphQL parser, and a mutation or a subscription is refused. So is a document that does not parse, one with several operations and no `operation_name`, and an `operation_name` the document does not have.
- Under `--allow-host`, the host of `url`, and of each redirect, must be one the run allows. A URL without a port is taken at the port of its scheme.

See [Guard](/guide/concepts/guard).

## See Also

- **[External Actions](/guide/concepts/actions#external-actions)** - How Probe resolves and checks an external action
- **[HTTP](/reference/actions/http)** - Requests of any other kind
- **[JMAP](/reference/actions/jmap)** - The other external action published alongside Probe
