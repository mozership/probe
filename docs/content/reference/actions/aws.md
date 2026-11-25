# AWS Action

The AWS action calls AWS services, and the services that speak their protocols, signing each request with Signature Version 4: objects in S3, MinIO or Cloudflare R2, messages in SQS or ElasticMQ, and any other API of AWS sent over HTTP, such as API Gateway, Lambda function URLs or OpenSearch.

It is an [external action](/guide/concepts/actions#external-actions), published in [mozership/probe-aws](https://github.com/mozership/probe-aws). Probe downloads it the first time a workflow uses it, and runs the executable whose SHA-256 the `action.yml` at the pinned commit names. It needs Probe v1.24.0 or later. The examples write `<commit SHA>` for the commit to pin: `probe guide aws` prints the `uses` line of the latest release, and so do the notes of each release.

## Basic Syntax

A workflow pins the action by a full commit SHA, and gives it a name once, under [`actions`](/reference/yaml-configuration#actions):

```yaml
name: Export
actions:
  aws: github.com/mozership/probe-aws@<commit SHA>
vars:
  queue_url: "{{QUEUE_URL}}"
jobs:
- name: export
  defaults:
    aws:
      region: ap-northeast-1
  steps:
  - name: Ask the API for an export
    uses: http
    with:
      post: https://api.example.com/exports
    test: res.code == 202

  - name: Wait for the job it queues
    id: job
    uses: aws
    with:
      sqs:
        receive: "{{vars.queue_url}}"
        wait: 20s
    test: res.count > 0
    retry:
      max_attempts: 30
    outputs:
      key: res.messages[0].body.key

  - name: The export is in the bucket
    uses: aws
    with:
      s3:
        bucket: example-exports
        head: "{{outputs.job.key}}"
    test: res.code == 200 && res.size > 0
```

## Parameters

A step calls one service, by one of these keys of `with`, which holds what to do:

| Key | What it calls |
|---|---|
| [`s3`](#s3) | Objects in S3, and in storage that speaks its protocol |
| [`sqs`](#sqs) | Messages in SQS, and in a queue that speaks its JSON protocol |
| [`request`](#request) | Any API of AWS sent over HTTP, by a request you write |

The others say where the service is and how to sign for it, for every service:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `region` | String | No | `AWS_REGION`, then `AWS_DEFAULT_REGION` | The region the request is signed for, and on AWS the one it is sent to. S3 falls back to `us-east-1`, and SQS to the region in the URL of the queue |
| `endpoint` | String | No | AWS | Where a service compatible with AWS is served, as `http://localhost:9000` |
| `access_key_id` | String | No | `AWS_ACCESS_KEY_ID` | The access key |
| `secret_access_key` | String | No | `AWS_SECRET_ACCESS_KEY` | The secret access key |
| `session_token` | String | No | `AWS_SESSION_TOKEN` | The session token of temporary credentials |
| `timeout` | Duration | No | `30s` | Time limit for the whole step, as `10s` or a number of seconds. `0` removes it |

A key of `with` that is not one of these fails the step before anything is sent. Its `action.yml` declares them as `params`, so `probe check` reports such a key with its line.

The job's `defaults`, keyed by the name the workflow gave the action, are merged key by key, those of `s3` and the others included, so a job can keep its region, its endpoint and its bucket out of each step:

```yaml
defaults:
  aws:
    endpoint: http://localhost:9000
    s3:
      bucket: fixtures
```

### Credentials

When `with` has none of `access_key_id`, `secret_access_key` and `session_token`, the action reads `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_SESSION_TOKEN` from the environment Probe runs in. The action reads no profile, no instance metadata and no other source of credentials, so it connects to nothing but the service. Export the credentials of a profile or a role before running Probe, as `eval "$(aws configure export-credentials --format env)"` does.

Without credentials in either place, an S3 request is sent unsigned, as a public bucket takes it; `sqs` and `request` fail the step.

Leave the credentials to the environment where you can. A secret access key written in `with` is not shown in the result, in a report or in the action's own log, and Probe shows the values of `secret_access_key` and `session_token` as `<redacted>` wherever it shows the parameters of a step. Declaring the variable under [`secrets`](/reference/yaml-configuration#secrets) hides its value anywhere else it could appear, such as an `echo`.

## S3

`s3` names its operation by one of these keys, and has exactly one of them.

| Key | Value | What it does |
|---|---|---|
| `get` | The key of an object | Reads the object: its content, its digest and what its headers tell |
| `head` | The key of an object | Reads what the headers tell of the object, without its content |
| `list` | A prefix, or nothing | Lists the objects whose keys start with the prefix, or every object |
| `put` | The key of an object | Writes the object, from `body` or from `file` |
| `delete` | The key of an object | Deletes the object |

The others say where the bucket is and what to write.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `bucket` | String | Yes | - | The bucket |
| `path_style` | Boolean | No | See below | Addresses the bucket in the path, as `<endpoint>/<bucket>/<key>`, rather than in the host, as `<bucket>.<endpoint>/<key>` |
| `body` | String, or any other value | For `put`, unless `file` | - | What a `put` writes: a string as it is, anything else as JSON, with the content type `application/json` unless `content_type` says otherwise |
| `file` | String | For `put`, unless `body` | - | Path to the file a `put` writes, relative to the working directory |
| `content_type` | String | No | - | The media type a `put` stores with the object |
| `metadata` | Object | No | - | The metadata a `put` stores with the object, sent as `x-amz-meta-<name>`. Names are lowercased, and values must be printable ASCII |
| `max_keys` | Integer | No | The storage's, `1000` on S3 | The most objects a `list` returns |

A key of `s3` that is not one of these fails the step before anything is sent. A parameter given to an operation that does not take it, such as `body` with `get`, fails the step the same way.

`path_style` defaults to `true` with an `endpoint`, as MinIO and the like are served under one name, and to `false` on AWS, except for a bucket with a dot in its name, which the certificate of `*.s3.<region>.amazonaws.com` does not cover.

A `list` returns one page: at most `max_keys` objects, with `res.truncated` telling whether the storage has more. Multipart uploads are not supported.

### Response Object

| Property | Type | Description |
|---|---|---|
| `res.code` | Integer | HTTP status code, such as `200`, or `204` for a `delete` |
| `res.status` | String | HTTP status line, such as `"200 OK"` |
| `res.headers` | Object | Response headers, keyed by canonical name such as `Content-Type` |
| `res.error_code` | String | The error the storage answered with, such as `NoSuchKey`, `NoSuchBucket`, `AccessDenied` or `SignatureDoesNotMatch`, or empty. A `head` has no body to carry it, so it is empty for one |
| `res.error_message` | String | What the storage said of the error, or empty |
| `req` | Object | What was done: `operation`, `bucket`, `region`, `endpoint`, `path_style`, the `url` the answer came from, and `signed`, with the `key` of the object, or the `prefix` and `max_keys` of a list. A `put` also has `body`, `file`, `content_type`, `metadata`, and the `size` and `sha256` of what it sent. It never has the credentials |
| `rt` | Duration | Time the step took |
| `status` | Integer | `0` when the status code is 2xx; `1` otherwise |

A `get` and a `head` add what the headers tell of the object.

| Property | Type | Description |
|---|---|---|
| `res.etag` | String | The ETag, without its quotes |
| `res.size` | Integer | Size in bytes |
| `res.content_type` | String | The media type stored with the object |
| `res.last_modified` | String | When it was last written, as `2026-10-10T01:02:03Z` |
| `res.metadata` | Object | The metadata stored with the object, by lowercased name, without `x-amz-meta-` |

A `get` adds the content.

| Property | Type | Description |
|---|---|---|
| `res.body` | Any | The content as text. Parsed into an object or array when `res.content_type` says JSON and it is JSON. Empty for a binary object |
| `res.rawbody` | String | The unparsed content, present when it was parsed as JSON |
| `res.sha256` | String | SHA-256 digest of the whole object, in hex |
| `res.truncated` | Boolean | Whether the object is larger than 1 MiB, of which `res.body` holds the first |
| `res.binary` | Boolean | Whether the content is not UTF-8 text, and so not in `res.body` |

`res.sha256` and `res.size` are of the whole object however large it is, so a binary or large object is checked by them, against the `req.sha256` of the step that wrote it or a digest you know.

A `put` adds `res.etag`. A `list` adds these.

| Property | Type | Description |
|---|---|---|
| `res.objects` | Array | The objects, in the order of their keys, each with `key`, `size`, `etag` and `last_modified` |
| `res.count` | Integer | How many objects `res.objects` has |
| `res.truncated` | Boolean | Whether the storage has more objects than it returned |

Once the storage answers, what it answered is a result, so a test can assert on a `404` for a key that should be gone or a `403` for a bucket that should be closed. Only a request that gets no answer, such as a refused connection or a timeout, fails the step as an error.

### Redirects

On AWS, a bucket that is not in the region the request was signed for is read from the region AWS names in its answer. A `Location` the storage answers with is followed too, up to 5 times, when it stays within the storage: under `amazonaws.com`, or the host of `endpoint` and the names under it. Each request is signed again for where it goes. A redirect to anywhere else is not followed, and is the result.

## SQS

`sqs` names its operation by one of these keys, each holding the URL of the queue, as `https://sqs.us-east-1.amazonaws.com/123456789012/jobs`:

| Key | What it does | Takes |
|---|---|---|
| `receive` | Receives messages, waiting for them up to `wait` | `wait`, `max`, `visibility_timeout`, `delete_received` |
| `send` | Sends a message | `body`, `delay`, `group_id`, `deduplication_id`, `attributes` |
| `delete` | Deletes a message received before | `receipt_handle` |

| Parameter | Type | Default | Description |
|---|---|---|---|
| `wait` | Duration | `0s` | How long a `receive` waits for a message to arrive, up to `20s`: a long poll |
| `max` | Integer | `1` | The most messages a `receive` returns, up to `10` |
| `visibility_timeout` | Duration | The queue's | How long the messages received are hidden from other consumers, up to `12h`. `0` leaves them in the queue, to look at |
| `delete_received` | Boolean | `false` | Deletes each message a `receive` returns, once it has them |
| `body` | String, or any other value | - | What a `send` sends: a string as it is, anything else as JSON |
| `delay` | Duration | The queue's | How long the message waits before it can be received, up to `15m` |
| `group_id`, `deduplication_id` | String | - | The group and the deduplication ID of a message to a FIFO queue |
| `attributes` | Object | - | The message attributes, each sent as a `String` |
| `receipt_handle` | String | - | The receipt handle of the message a `delete` deletes, from `res.messages[].receipt_handle` |

A duration is written as `20s` or as a number of seconds, and must be whole seconds, as SQS takes it. The region is the one in the URL of the queue on AWS, unless `region` names another; with an `endpoint`, the requests go there and `region` or the environment must name one.

A step waits for a message as long as `wait` says, and `retry` repeats it until the message is there:

```yaml
- name: Wait for a job
  uses: aws
  with:
    sqs:
      receive: "{{vars.queue_url}}"
      wait: 20s
      delete_received: true
  test: res.count > 0 && res.messages[0].body.status == "done"
  retry:
    max_attempts: 180   # an hour
```

### Response Object

| Property | Type | Description |
|---|---|---|
| `res.code` | Integer | HTTP status code, `200` for a success |
| `res.status` | String | HTTP status line |
| `res.headers` | Object | Response headers |
| `res.error_code` | String | The error the queue answered with, such as `QueueDoesNotExist` or `AccessDenied`, or empty |
| `res.error_message` | String | What the queue said of the error, or empty |
| `req` | Object | What was done: `operation`, `queue_url`, `region`, the `url` it was sent to, `signed`, and what the operation was given. It never has the credentials |
| `rt` | Duration | Time the step took |
| `status` | Integer | `0` when the status code is 2xx; `1` otherwise |

A `receive` adds these.

| Property | Type | Description |
|---|---|---|
| `res.messages` | Array | The messages received, empty when none came within `wait` |
| `res.messages[].id` | String | The message ID |
| `res.messages[].body` | Any | The body. Parsed into an object or array when it is JSON |
| `res.messages[].rawbody` | String | The unparsed body, present when it was parsed as JSON |
| `res.messages[].receipt_handle` | String | What a `delete` of this message takes |
| `res.messages[].md5` | String | The MD5 digest of the body |
| `res.messages[].attributes` | Object | The message attributes, by name |
| `res.messages[].system` | Object | The attributes SQS keeps, such as `SentTimestamp` and `ApproximateReceiveCount` |
| `res.count` | Integer | How many messages `res.messages` has |
| `res.deleted` | Integer | How many were deleted, with `delete_received` |

A `send` adds `res.message_id`, `res.md5` and, to a FIFO queue, `res.sequence_number`.

## Request

`request` sends a request you write, signed for the service it names, so the action reaches any API of AWS sent over HTTP:

```yaml
- name: The queue has no messages left
  uses: aws
  with:
    region: ap-northeast-1
    request:
      service: sqs
      post: https://sqs.ap-northeast-1.amazonaws.com/
      headers:
        content-type: application/x-amz-json-1.0
        x-amz-target: AmazonSQS.GetQueueAttributes
      body:
        QueueUrl: "{{vars.queue_url}}"
        AttributeNames: [ApproximateNumberOfMessages]
  test: res.code == 200 && res.body.Attributes.ApproximateNumberOfMessages == "0"
```

| Parameter | Type | Required | Description |
|---|---|---|---|
| `service` | String | Yes | The name of the service the endpoint signs for, such as `sqs`, `execute-api` for API Gateway, `lambda` for a function URL or `es` for OpenSearch |
| `get`, `head`, `post`, `put`, `patch`, `delete`, `options` | String | One of them, or `method` and `url` | The URL to send to with that method. A path, starting with `/`, is taken under `endpoint` |
| `method`, `url` | String | | The method and the URL, in place of one of the keys above |
| `headers` | Object | No | Request headers. `authorization` cannot be given, as the signature is sent in it |
| `body` | String, or any other value | No | The body: a string as it is, anything else as JSON, with the content type `application/json` unless `headers` says otherwise |

`region`, or the environment, must name the region. The signature covers the method, the URL, the body and the headers, but not `User-Agent`, `Accept-Encoding`, `Expect` and `X-Amzn-Trace-Id`. A redirect is not followed, and is the result.

The result has `res.code`, `res.status`, `res.headers` and `res.body`, which Probe parses when it is JSON, whatever its content type, with `res.rawbody` the unparsed body. `res.binary` tells a body that is not UTF-8 text, which `res.body` then leaves out. `req` has what was sent, without the credentials, and `status` is `0` for a 2xx.

Once a service answers, what it answered is a result, so a test can assert on a `404` for something that should be gone or a `403` for something that should be closed. Only a request that gets no answer, such as a refused connection or a timeout, fails the step as an error.

## Under a Guard

The action keeps to the guard of the run, and its `action.yml` declares `guard: [read-only, allow-host]`, so Probe runs it under `--read-only` and `--allow-host` without `--allow-action`.

- Under `--read-only`, only what changes nothing is run: `get`, `head` and `list` of `s3`, a `receive` of `sqs` with `visibility_timeout: 0` and without `delete_received`, which leaves the messages in the queue, and a `request` with `GET`, `HEAD` or `OPTIONS`. Anything else is refused before anything is sent.
- Under `--allow-host`, every host a request goes to must be one the run allows, a redirect of S3 included. A URL without a port is taken at the port of its scheme: `80` for `http` and `443` for `https`. `--allow-host '*.amazonaws.com'` allows AWS in whichever region.

A refused step fails with the kind `refused`. See [Guard](/guide/concepts/guard).

## Coming from probe-s3

probe-aws takes the place of [probe-s3](https://github.com/mozership/probe-s3). A step of probe-s3 keeps its operation and the keys of the bucket under `s3`, and the region, the endpoint, the credentials and the timeout where they were:

```yaml
# probe-s3
uses: s3
with:
  region: ap-northeast-1
  bucket: example-avatars
  head: avatars/42.png
```

```yaml
# probe-aws
uses: aws
with:
  region: ap-northeast-1
  s3:
    bucket: example-avatars
    head: avatars/42.png
```

The result of an `s3` step is the one of probe-s3.

## See Also

- **[External Actions](/guide/concepts/actions#external-actions)** - How Probe resolves and checks an external action
- **[HTTP](/reference/actions/http)** - Requests over plain HTTP
- **[Redis](/reference/actions/redis)** - Commands on a Redis or Valkey server
