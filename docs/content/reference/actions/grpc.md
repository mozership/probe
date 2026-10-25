# gRPC Action

The `grpc` action calls a gRPC method. The service definition is resolved through server reflection, so no `.proto` file is needed at run time. A server without reflection, as many in production are, is called with the definition of the `.proto` files given in [`proto`](#checking-against-proto-files). With `protocol: connect`, the method is called with the [Connect protocol](#connect-protocol) over HTTP instead.

## Basic Syntax

A gRPC step names the server, the service, the method, and the request body.

```yaml
- name: Get a user
  uses: grpc
  with:
    addr: "grpc.example.com:443"
    service: "user.v1.UserService"
    method: "GetUser"
    tls: true
    body: |
      {"id": "123"}
  test: res.status_code == "OK"
```

## Parameters

The fields below describe the call. All of them accept template expressions.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `protocol` | String | No | `grpc` | `grpc`, or `connect` for the [Connect protocol](#connect-protocol) |
| `addr` | String | Yes | - | Host and port of the gRPC server. With `protocol: connect`, the URL the service is served under |
| `service` | String | Yes | - | Fully qualified service name |
| `method` | String | Yes | - | Method name |
| `body` | String, Object or List | No | `""` | Request message as JSON. An object is serialized as JSON. A list is the messages of a stream of requests; see [Streaming Methods](#streaming-methods) |
| `metadata` | Object | No | `{}` | Request metadata (the gRPC equivalent of headers) |
| `timeout` | String | No | `30s` | Time limit for the call, including connecting and the reflection lookup. A duration such as `"10s"`, or a number of seconds such as `10`. A value that is neither, or not above zero, is an error |
| `tls` | Boolean | No | `false` | Use TLS |
| `insecure` | Boolean | No | `false` | Skip verifying the server's certificate. `ca_file` has no effect then; a client certificate is still sent |
| `cert_file` | String | No | - | Client certificate for mutual TLS. Give it with `key_file` |
| `key_file` | String | No | - | Client key for mutual TLS. Give it with `cert_file` |
| `ca_file` | String | No | - | CA certificate used to verify the server |
| `codec` | String | No | `json` | How a Connect call encodes its messages: `json` or `proto`. Only with `protocol: connect` |
| `max_messages` | Integer | No | - | Leave a stream of responses once this many have come. See [Streaming Methods](#streaming-methods) |
| `proto` | Object or `false` | No | - | Check the call against the `.proto` files listed in `files`, and fail the step when it breaks them. `false` leaves out a check the job's `defaults` ask for. See [Checking Against .proto Files](#checking-against-proto-files) |

## Response Object

After the call, `res` holds the reply and the gRPC status.

| Field | Type | Description |
|-------|------|-------------|
| `res.body` | Any | Response message, parsed from its JSON form into an object. Field names are in lowerCamelCase, such as `createdAt` |
| `res.rawbody` | String | The response message as unparsed JSON |
| `res.status_code` | String | gRPC status code, such as `OK` or `NOT_FOUND` |
| `res.status_message` | String | Status message |
| `res.metadata` | Object | Headers and trailers the server sent, with the first value of each. A trailer wins over a header of the same name |
| `rt.duration` | String | Round-trip time, such as `"1.2ms"` |
| `rt.sec` | Float | Round-trip time in seconds |
| `status` | Integer | `0` when the status is `OK`, otherwise `1` |
| `req` | Object | The request as it was sent |
| `res.violations` | Array | What the `.proto` files do not allow in the call, empty when they allow all of it. Present only when `proto` is given |
| `res.messages` | Array | Every response of a method that streams its responses, each an object as `res.body` is. Present only for such a method |
| `res.complete` | Boolean | Whether the server ended the stream of responses, rather than the call leaving it. Present only for a method that streams its responses |
| `res.contract` | Object | What the call was matched to in the `.proto` files: `spec`, the file that declares the service as `proto.files` names it, and `operation`, such as `users.v1.UserService/GetUser`. Present only when the files declare the method; [`probe coverage`](/reference/cli-reference#coverage) counts it |

`res.status_code` is the canonical name of the status the call ended with: `OK`, `CANCELLED`, `UNKNOWN`, `INVALID_ARGUMENT`, `DEADLINE_EXCEEDED`, `NOT_FOUND`, `ALREADY_EXISTS`, `PERMISSION_DENIED`, `RESOURCE_EXHAUSTED`, `FAILED_PRECONDITION`, `ABORTED`, `OUT_OF_RANGE`, `UNIMPLEMENTED`, `INTERNAL`, `UNAVAILABLE`, `DATA_LOSS` or `UNAUTHENTICATED`. A status other than `OK` is the server's answer, so the step goes on to its test, which can expect it. Only a call that gets no status from the server ends the step with an error: one to a server that cannot be reached, or whose reflection does not list the service when no `.proto` files declare it, or one that runs out of `timeout` or loses its connection before the server answers.

## Usage Examples

### Expecting an Error Status

When an error is the expected outcome, the test checks the status code.

```yaml
steps:
  - name: Unknown user returns NOT_FOUND
    uses: grpc
    with:
      addr: "{{vars.grpc_addr}}"
      service: "user.v1.UserService"
      method: "GetUser"
      tls: true
      body: |
        {"id": "does-not-exist"}
    test: res.status_code == "NOT_FOUND"
```

## Checking Against .proto Files

The call is made with the server's own definition, as its reflection tells it, which says nothing of whether the server keeps to the definition it was meant to. With `proto`, the call is also checked against `.proto` files, such as those in the repository the service is built from, and a server without reflection, or one whose definition lacks the method, is called with the files' definition, as a client built from them would call it:

```yaml
- name: Users
  defaults:
    grpc:
      addr: "{{vars.grpc_addr}}"
      service: user.v1.UserService
      proto:
        files: [./proto/user/v1/user.proto]
        import_paths: [./proto]
  steps:
    - name: Get a user
      uses: grpc
      with:
        method: GetUser
        body:
          id: "123"
      test: res.status_code == "OK" && res.body.user.name != ""
```

`files` are compiled as `protoc` would with `import_paths`, the directory Probe runs in when none is given; each file is named by its path under the first import path that holds it, as an import of it names it. The files are compiled before the call is made, so a step whose files do not compile fails as an action error without calling anything.

The step fails with the kind `contract_request` when the request body, as JSON, is not one the request message of the files takes, and with the kind `contract_response` when:

- the files declare no such method in the service
- the server's request or response message, or a message in them at any depth, has another name than the files', lacks a field the files declare, or declares a field by the same number with another name, type or cardinality
- a field of the response is not encoded as the files declare its number, such as a string where they declare an `int32`

With `strict: true`, a field the server declares, or sends, that the files do not declare fails the step too. Without it such a field is let through, as protobuf lets a newer peer's fields through. Called with the files' definition, the call is checked as far as the files go without the server's: the request, the response read again and the constraints below, but not the definitions compared. A method the server's reflection lists the service without fails the step as `contract_response`, and the server answers the call with `UNIMPLEMENTED`.

A request body that the server's own definition cannot take either cannot be sent, so nothing is sent, and the step fails as `contract_request`, with `res.status_code` empty, rather than as an action error. `request: false` checks the server and its response alone, for a step that sends what the files do not allow on purpose. A response with a status other than `OK` has no message to check. Each violation is in `res.violations`, the terminal and the reports, with the field, such as `$.user.email`.

### Constraints the Files Annotate

proto3 declares no required fields and no ranges, so the shape is all the files say unless they annotate their fields. Two kinds of annotation are checked when the files use them:

- [protovalidate](https://protovalidate.com) rules, `[(buf.validate.field)...]` and the message and oneof rules, are checked on the request and the response, and a field that breaks one fails the step with the rule, such as `string.email`, and the field.
- `google.api.field_behavior`, as [AIP-203](https://google.aip.dev/203) has it: a request that lacks a field that is `REQUIRED`, at any depth of the messages it holds, fails the step as `contract_request`, and a response that holds a field that is `INPUT_ONLY` fails it as `contract_response`, as a field a response must never carry.

```protobuf
syntax = "proto3";
import "buf/validate/validate.proto";
import "google/api/field_behavior.proto";

message CreateUserRequest {
  string email = 1 [(buf.validate.field).string.email = true, (google.api.field_behavior) = REQUIRED];
  string password = 2 [(google.api.field_behavior) = INPUT_ONLY];
}
```

`buf/validate/validate.proto` and `google/api/field_behavior.proto` are built into Probe, so the files can import them without having them in an import path; a copy in one is not read.

## Connect Protocol

With `protocol: connect`, the method is called with the [Connect protocol](https://connectrpc.com/docs/protocol) rather than gRPC: a POST over HTTP, which an HTTP/1.1 proxy or load balancer passes, as it passes the calls a [connect-web](https://connectrpc.com/docs/web/getting-started) client in a browser makes. Servers built with connect-go or connect-es answer it.

```yaml
- name: Get a user
  uses: grpc
  with:
    protocol: connect
    addr: https://api.example.com/rpc
    service: users.v1.UserService
    method: GetUser
    body:
      id: "123"
    metadata:
      authorization: "Bearer {{vars.token}}"
  test: res.status_code == "OK" && res.body.user.name != ""
```

`addr` is the URL the service is served under, with the path it is mounted at, if any; a host and port without a scheme is taken as `http`, or `https` with `tls: true`. The call goes to `<addr>/<service>/<method>`. `metadata` is sent as headers, and `timeout` is told to the server as `Connect-Timeout-Ms` too. The TLS parameters apply to an `https` URL as they do to a gRPC call.

`codec` is how the messages are encoded: `json`, the default, or `proto`. Reflection is not used, as it needs a stream an HTTP/1.1 connection cannot carry:

- With `codec: json` and no `proto`, `body` is sent as it is written, and `res.body` is the JSON the server answered. `service` must then be the full name, such as `users.v1.UserService`, and the body's field names are those the server reads, lowerCamelCase such as `userId` with connect-go.
- With `proto`, the request is built from the files' definition, so a short service name and snake_case field names work as they do for a gRPC call, and `res.body` has the form a gRPC call gives. The call is checked against the files as described above, except that there is no server definition to compare them with. With `codec: json`, a value in the response of another type than the files declare fails the step as `contract_response`, as a field they do not declare does under `strict`.
- `codec: proto` needs `proto`, whose definition encodes the request and reads the response.

`res` has the fields a gRPC call gives. A Connect error, such as `{"code": "not_found", "message": "..."}` with HTTP status 404, gives `res.status_code` `NOT_FOUND` and its message, as the Connect codes are the gRPC ones. An answer that holds no Connect error is still the server's answer, and its code is the one the protocol implies, as connect-go reads it: from the HTTP status, such as `UNAVAILABLE` for a proxy's 502 page, with the status line as the message, or `UNKNOWN` or `INTERNAL` for a 200 whose content type is not the codec's. `res.metadata` holds the response headers; those the server sent after the message, under `Trailer-`, are named without it and win over a header of the same name. Only a call that gets no answer, such as one to a server that cannot be reached or that runs out of `timeout`, is an action error. Redirects are not followed.

A streaming method is called with `protocol: connect` when `proto` declares it, as [Streaming Methods](#streaming-methods) describes, over HTTP/1.1 too; one that streams both ways is not called.

## Streaming Methods

A method that streams its responses or its requests is called as one that does not, with the same parameters, by gRPC or by `protocol: connect`. The call sends every request, closes its side, and then reads the responses.

```yaml
- name: Watch users
  uses: grpc
  with:
    method: WatchUsers
    body:
      filter: active
    max_messages: 3
  test: res.status_code == "" && len(res.messages) == 3 && !res.complete

- name: Import users
  uses: grpc
  with:
    method: ImportUsers
    body:
      - name: Ada
      - name: Grace
  test: res.status_code == "OK" && res.body.imported == 2
```

- A method that streams its requests takes a list in `body`, each item of which is one message of the stream, in order; one object is the only message, and `[]` sends none. A list to any other method is an error.
- A method that streams its responses has every message in `res.messages`, in the order they came, and the last in `res.body`. `res.complete` is `true` when the server ended the stream, with the status it ended with in `res.status_code`.
- `max_messages` leaves the call once that many responses have come, for a stream that does not end by itself. The server has then told no status, so `res.status_code` is empty and `res.complete` is `false`. It is an error with a method that does not stream its responses.
- A stream that breaks off after some responses came, such as one `timeout` cuts, gives those responses, the status it broke with, such as `DEADLINE_EXCEEDED`, and `res.complete` `false`. With no response at all it is an action error, as a call that gets no answer is. The server is told `timeout` too, and may end the stream itself when it runs out, with `DEADLINE_EXCEEDED` and `res.complete` `true`.
- A method that streams both ways is not called, and fails the step as an action error before anything is sent.

What streams is told by the definition the call is made with: the server's reflection, or the `.proto` files of `proto`. A Connect call has no reflection, so it calls a streaming method only with `proto`; without it, a list `body` and `max_messages` are errors, and the method is called as one that does not stream, which the server refuses. With `proto`, each message of a stream is checked against the files, and a violation names the message by its index, such as `$[1].user.email`; a server whose definition streams another way than the files declare fails the step as `contract_response`.

## Under a Guard

The grpc action keeps to the [guard](/reference/cli-reference#--read-only) of a run, and refuses a call it does not allow before anything is sent, failing the step with the kind `refused`.

- `--allow-host` is checked against the host and port of `addr`, with port 443 when none is named, as grpc-go takes it. `dns:///` and `passthrough:///` before it are read through, and the DNS server of `dns://server/` is checked too, at port 53 when none is named; a target that names no host, such as `unix:///tmp/grpc.sock`, is refused. With `protocol: connect`, the host of the URL is checked, at the port of its scheme when it names none.
- Under `--read-only`, a method is called only when every definition of it at hand declares it free of side effects with `option idempotency_level = NO_SIDE_EFFECTS;`: the server's, as its reflection tells it, and the `.proto` files', when `proto` is given. The reflection lookup reads, and is made. A Connect call has no reflection, so it needs `proto`, and one without is refused.

```protobuf
service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}
```
