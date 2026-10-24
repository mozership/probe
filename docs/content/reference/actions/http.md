# HTTP Action

The `http` action performs an HTTP request and exposes the response for assertions and outputs.

## Basic Syntax

An HTTP step needs a URL and a method, and asserts on the response in `test`.

```yaml
steps:
  - name: Check the API
    uses: http
    with:
      url: "https://api.example.com/health"
      method: GET
    test: res.code == 200
```

## Parameters

The fields below describe the request. All of them accept template expressions.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `url` | String | Yes | - | Request URL, or the base URL when a method shorthand carries a path |
| `method` | String | Yes | - | HTTP method. Supplied by a method shorthand when one is used |
| `headers` | Object | No | - | Request headers |
| `body` | String or Object | No | - | Request body. An object is serialized as JSON when `content-type` is a JSON type: `application/json`, with or without parameters such as `charset`, or one ending in `+json` |
| `timeout` | Duration | No | `30s` | Time limit for the whole request, including reading the response |
| `basic_auth` | Object | No | - | `username` and `password` for HTTP Basic authentication, sent as the `Authorization` header |
| `form` | Object | No | - | Form fields, sent as an `application/x-www-form-urlencoded` body. See [Sending a Form](#sending-a-form) |
| `multipart` | Object | No | - | Form fields and files, sent as a `multipart/form-data` body. See [Uploading Files](#uploading-files) |
| `cookies` | Object | No | - | Cookies to send, by name. See [Cookies](#cookies) |
| `trace_header` | Boolean or String | No | `false` | Send a header that names the run, job and step of the request: `X-Probe-Trace` for `true`, or the header named. See [Tracing Requests](#tracing-requests) |
| `keep_cookies` | Boolean | No | `false` | Keep the cookies the server sets in the job, and send them in the following steps that keep cookies. See [Cookies](#cookies) |
| `openapi` | Object or `false` | No | - | Check the request and the response against the OpenAPI document whose path is `spec`, and fail the step when either breaks it. `request: false` checks the response alone, `strict: true` also fails a property the document does not declare, and `false` leaves out a check the job's `defaults` ask for. See [Checking Against OpenAPI](#checking-against-openapi) |

There are no parameters for redirects or TLS verification. Redirects are followed by default.

Every request sends `Accept: */*` and `User-Agent: probe-http/1.0.0` unless `headers` sets them. Header names are compared without regard to case, so `user-agent: my-agent` replaces the default.

### `timeout`

`timeout` accepts a duration string such as `10s` or `1m30s`, or a plain number of seconds. `0` removes the limit.

```yaml
  - name: Slow endpoint
    uses: http
    with:
      url: "{{vars.api_url}}/report"
      method: GET
      timeout: 60s
    test: res.code == 200
```

A request that runs out of time fails the step with a `Client.Timeout exceeded` error.

Set it once for a job through `defaults`:

```yaml
jobs:
  - name: API checks
    defaults:
      http:
        timeout: 5s
    steps:
      - name: Health
        uses: http
        with:
          get: /health
        test: res.code == 200
```

The step's own `timeout` is a separate, outer limit on each attempt of the action, defaulting to 5m. `with.timeout` bounds the HTTP request; the step `timeout` bounds the action call that wraps it, and is what stops an action that hangs without returning.

### Method Shorthands

`get`, `head`, `post`, `put`, `patch`, `delete`, `connect`, `options` and `trace` set the method and the path in one key. The value is either a full URL or a path resolved against `url`, which makes it convenient with a job's `defaults`.

```yaml
name: API
vars:
  api_url: "{{API_URL}}"
  user_name: alice
jobs:
  - name: API checks
    defaults:
      http:
        url: "{{vars.api_url}}"
        headers:
          accept: application/json
    steps:
      - name: List users
        uses: http
        with:
          get: /users
        test: res.code == 200

      - name: Create a user
        uses: http
        with:
          post: /users
          headers:
            content-type: application/json
          body:
            name: "{{vars.user_name}}"
        test: res.code == 201
```

### Sending a Form

`form` sends its fields as an `application/x-www-form-urlencoded` body, as an HTML form does. A field takes a string, a number or a boolean, and a list sends the field once for each of its values.

```yaml
  - name: Log in with a form
    uses: http
    with:
      post: /login
      form:
        user: "{{vars.user}}"
        password: "{{vars.password}}"
        scope: [read, write]
    test: res.code == 302
```

The fields are sorted by name and percent-encoded, and `req.body` shows them as they were sent.

### Uploading Files

`multipart` sends text fields and files as a `multipart/form-data` body. A field written as a string, a number or a boolean is a text field. A field written as a map is a file, read from `file` or given inline as `content`. A list sends the field once for each of its values, so several files can go under one name.

```yaml
  - name: Upload an avatar
    uses: http
    with:
      post: /api/images
      multipart:
        title: avatar
        image:
          file: ./fixtures/logo.png
        attachments:
          - file: ./fixtures/terms.pdf
          - content: "a,b\n1,2\n"
            filename: data.csv
            content_type: text/csv
    test: res.code == 201
```

A file takes these keys:

| Key | Description |
|-----|-------------|
| `file` | Path to the file to send, relative to the working directory, as the paths of other actions are |
| `content` | The content to send, in place of `file` |
| `filename` | Filename sent with the file. Defaults to the base name of `file`, or the field name for `content` |
| `content_type` | Media type of the file. Defaults to the one the extension of `filename` names, or `application/octet-stream` |

Text fields are sent first and files after them, each sorted by name, since some servers, such as S3 for a POST upload, read only the fields that come before the file. The values of a list keep their order among the text fields or among the files, so a list that holds both sends its text values first. A file is read each time the step runs, so a retried step sends the file as it is then.

The body is sent with its length, and is not shown: `req.body` is empty and `req.multipart` shows what was written instead.

`form` and `multipart` each set the `Content-Type` header, which replaces one given in `headers` or in a job's `defaults`, such as `application/json`. Neither can be given together with `body`, nor with each other.

## Response Object

After the request, `res` holds what came back.

| Field | Type | Description |
|-------|------|-------------|
| `res.code` | Integer | Status code, such as `200` |
| `res.status` | String | Status line, such as `"200 OK"` |
| `res.headers` | Object | Response headers, keyed by canonical name such as `Content-Type` |
| `res.body` | Any | Response body. Parsed into an object or array when the response is JSON, otherwise the raw string |
| `res.rawbody` | String | The unparsed body, present when the body was parsed as JSON |
| `res.filepath` | String | Path to the saved file when the response is binary |
| `res.cookies` | Object | The cookies the server set, by name, on redirects included |
| `res.violations` | Array | What the OpenAPI document does not allow in the request and the response, empty when it allows all of it. Present only when `openapi` is given |
| `res.contract` | Object | What the response was matched to in the OpenAPI document: `spec`, `operation` such as `GET /users/{id}`, and `response` such as `200`, `2XX` or `default`, left out when the operation declares none for the status. Present only when the document has an operation for the request |
| `rt.duration` | String | Round-trip time, such as `"120ms"` |
| `rt.sec` | Float | Round-trip time in seconds |
| `status` | Integer | `0` when the status code is 2xx, `1` otherwise |

## Response Examples

For a JSON response, the fields are read straight off `res.body`:

```yaml
    test: |
      res.code == 200 &&
      res.headers["Content-Type"] contains "application/json" &&
      res.body.status == "ok" &&
      len(res.body.items) > 0
    outputs:
      first_id: res.body.items[0].id
      elapsed_ms: rt.sec * 1000
```

For a text or HTML response, `res.body` is the string itself:

```yaml
    test: |
      res.code == 200 &&
      res.body contains "<title>" &&
      len(res.body) > 100
```

## Common HTTP Patterns

The fields above combine into a few shapes that come up in most workflows: carrying a token between steps, asserting on an error response, and retrying a request that is not yet consistent.

### Authentication

The token is captured as an output of the login step and read by the steps that follow.

```yaml
  - name: Log in
    id: auth
    uses: http
    with:
      url: "{{vars.api_url}}/login"
      method: POST
      headers:
        content-type: application/json
      body:
        user: "{{vars.user}}"
        password: "{{vars.password}}"
    test: res.code == 200
    outputs:
      token: res.body.access_token

  - name: Call a protected endpoint
    uses: http
    with:
      url: "{{vars.api_url}}/me"
      method: GET
      headers:
        authorization: "Bearer {{outputs.auth.token}}"
    test: res.code == 200
```

A server that takes HTTP Basic authentication is given the username and password with `basic_auth`, which builds the `Authorization` header from them. Set in a job's `defaults`, it applies to every request of the job.

```yaml
- name: Admin API
  defaults:
    http:
      url: "{{vars.api_url}}"
      basic_auth:
        username: "{{vars.admin_user}}"
        password: "{{vars.admin_password}}"
  steps:
    - name: List users
      uses: http
      with:
        get: /admin/users
      test: res.code == 200 && len(res.body) > 0
```

A username must not contain a colon, which would end it early, and a password may be empty. `basic_auth` cannot be given together with an `authorization` header, since only one of them could be sent. The password and the header built from it are hidden in the output as other credentials are.

### Cookies

A cookie a server sets is sent on to where a redirect leads, as a browser sends it, so a login that sets a session cookie and redirects reaches the page it redirects to. The cookies set are in `res.cookies` by name, those set on a redirect included.

With `keep_cookies: true`, the job keeps the cookies the server sets, and sends them in the following steps of the job that keep cookies too. Which cookies go to which URL follows their domain, path, expiry and `Secure` flag, as in a browser, and a cookie the server removes or that expires is no longer sent. Set in a job's `defaults`, it applies to every request of the job.

```yaml
- name: Logged-in pages
  defaults:
    http:
      url: "{{vars.api_url}}"
      keep_cookies: true
  steps:
    - name: Log in
      uses: http
      with:
        post: /login
        form:
          user: "{{vars.user}}"
          password: "{{vars.password}}"
      test: res.code == 200 && res.cookies.session != ""

    - name: My page
      uses: http
      with:
        get: /me
      test: res.code == 200
```

Each job keeps its own cookies, so jobs that run at the same time do not see each other's. Each run of a repeated job, and a job run by the [embedded](/reference/actions/embedded) action, starts with none. A cookie is passed to another job as an output, and sent there with `cookies`.

`cookies` sends cookies by name, with or without `keep_cookies`, in place of a kept cookie of the same name. They are sent only to the host of the request, not to another host a redirect leads to. A `cookie` header is sent as well.

```yaml
    - name: Someone else's session
      uses: http
      with:
        get: /me
        cookies:
          session: "{{outputs.other.session}}"
      test: res.code == 200
```

The values of `cookies` and `res.cookies` are hidden in the output as other credentials are, with their names shown.

### Tracing Requests

With `trace_header: true`, each request carries an `X-Probe-Trace` header that says where it comes from, so that it can be told apart in the server's access log:

```
X-Probe-Trace: run=7f3a9c21e4b05d68; job=login; step=auth; repeat=0; attempt=1
```

| Field | Value |
|-------|-------|
| `run` | The ID of the run of probe, the same for every request of the run. It is the `run_id` of the JSON report, and a job run by the embedded action shares it |
| `job` | The ID of the job |
| `step` | The ID of the step, which a step without one is given as `step_<index>` |
| `repeat` | Which run of a repeated job, from 0 |
| `attempt` | Which attempt of a retried step, from 1 |

A value that holds a space, a semicolon or a letter outside ASCII is escaped as in a URL query. Set in a job's `defaults`, it applies to every request of the job. A header name in place of `true` sends the same value in that header, such as one the server already logs:

```yaml
- name: Traced checks
  defaults:
    http:
      url: "{{vars.api_url}}"
      trace_header: X-Correlation-Id
  steps:
    - name: List users
      id: users
      uses: http
      with:
        get: /users
      test: res.code == 200
```

The header is sent as written in `headers` is, and shows in `req.headers`. It cannot be given together with a header of the same name in `headers`. The option is not named `trace`, which is the shorthand of the TRACE method.

### Checking Against OpenAPI

With `openapi`, each request and its response are checked against an OpenAPI document, which says what is right independently of the `test` written for the step. Set in a job's `defaults`, it checks every request of the job:

```yaml
- name: Users
  defaults:
    http:
      url: "{{vars.api_url}}"
      headers:
        content-type: application/json
      openapi:
        spec: ./openapi.yml
  steps:
    - name: Create a user
      uses: http
      with:
        post: /users
        body:
          name: probe
      test: res.body.name == "probe"
    - name: Reject a user without a name
      uses: http
      with:
        post: /users
        body: {}
        openapi:
          request: false
      test: res.code == 400
    - name: Health
      uses: http
      with:
        get: /health
        openapi: false
      test: res.code == 200
```

The request and the response are matched to an operation by the method and path of the request, with the path of the document's `servers` taken off the front. The request is checked as the step sent it, with the cookies sent from `cookies` or kept by `keep_cookies`, and the step fails with the kind `contract_request` when:

- a path, query, header or cookie parameter is missing where it is required, or does not keep to its schema
- the body is missing where it is required, its `Content-Type` is not one the operation declares, or it does not keep to its schema
- the credentials the operation's `security` requires are missing

The response is checked by the last request when it was redirected, and the step fails with the kind `contract_response` when:

- the document has no operation for the method and path
- the operation declares neither the status code nor a `default` response
- the response's `Content-Type` is not one the response declares
- a header the response declares, or the body, does not keep to its schema
- a JSON body is empty, or is `null` where its schema does not allow null

Bodies are checked as JSON, text, XML, YAML, CSV and forms. A body of another type, such as an image or a PDF, is checked for its `Content-Type` alone.

When both break the document, the kind is `contract_request`: the workflow sent what the document does not allow, which may be why the response breaks it too. The step fails even when its `test` holds, and the `test` is not evaluated. A step without a `test` whose request and response keep to the document passes, since the document checked them, and with `retry` it is retried until they keep to the document, as a step is until its `test` holds. Each violation is in `res.violations`, the terminal and the reports, as `{in, field, reason, message}`, where `in` is `request` or `response` and `field` names the field of the body, such as `$.id`, when there is one.

A step that sends what the document does not allow on purpose, to see it rejected, writes `request: false`, which still checks the response, so a rejection the document does not declare fails the step. A request without the credentials `security` requires is one, as when checking for a `401`.

#### Strict

A schema usually allows properties it does not declare, so a response that also returns, say, `password_hash` keeps to it. With `strict: true`, the step fails on:

- a property of a JSON body that its schema does not declare, where the schema writes properties or `patternProperties` and leaves `additionalProperties` out
- a query parameter the operation does not declare
- a `readOnly` property in the request, and a `writeOnly` property in the response

```yaml
      openapi:
        spec: ./openapi.yml
        strict: true
```

A schema that writes `additionalProperties` says itself what more it allows: `true` or a schema, as a map such as `labels` has, allows more, and `false` fails without `strict`. A schema that declares no properties, such as one that says only `type: object`, allows any. The properties declared by the schemas of `allOf`, `oneOf` and `anyOf` count as declared. Headers and cookies are not checked strictly, since proxies, servers and clients add ones a document rarely declares, such as `Server` or a load balancer's cookie. A violation is reported as `contract_request` or `contract_response` by where it is found, with the property in `field`, such as `$.owner.email`.

`spec` is a path from the directory Probe runs in. The document is read before the request is sent, so a step whose document cannot be read or parsed fails as an action error without sending anything. `openapi: false` on a step leaves out the check, such as for an endpoint the document does not cover.

Which operation and response each step's response was matched to is in `res.contract` and in the JSON report, and [`probe coverage`](/reference/cli-reference#coverage) tells from the report which ones of the document no step checked.

### Under a Guard

Run with `--read-only`, the action sends only `GET`, `HEAD` and `OPTIONS`, and refuses any other method before sending it. Run with `--allow-host`, it refuses a URL, or a redirect, to a host the run does not allow, taking a URL without a port at the port of its scheme. A refused step fails with the kind `refused`. See [`--read-only`](/reference/cli-reference#--read-only).

### Checking an Error Response

Here the error is the expected result, so the test asserts on the status and the error body.

```yaml
  - name: Unknown id returns 404
    uses: http
    with:
      url: "{{vars.api_url}}/users/does-not-exist"
      method: GET
    test: res.code == 404 && res.body.error != nil
```

### Retrying a Flaky Endpoint

`retry` repeats the step until the test passes or the attempts run out.

```yaml
  - name: Eventually consistent read
    uses: http
    retry:
      max_attempts: 5
      interval: 2s
    with:
      url: "{{vars.api_url}}/orders/{{outputs.create.order_id}}"
      method: GET
    test: res.code == 200
```
