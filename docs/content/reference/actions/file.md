# File Action

The `file` action looks at what is at a path, and reads it when it is a regular file. Use it to check what a server wrote to its log, the content of a file that what is tested produced, or, with `retry`, to wait for a file or a socket to come or to go.

## Basic Syntax

A file step gives the path, and its test reads the file from `res`.

```yaml
steps:
  - name: No error is logged
    uses: file
    with:
      path: ./server.log
    test: res.exists && !(res.body contains "level=ERROR")
```

## Parameters

| Key | Required | Default | Description |
|---|---|---|---|
| `path` | Yes | | The path to look at, relative to the working directory, as the paths of other actions are. A symbolic link is followed |

## Response

| Field | Type | Description |
|---|---|---|
| `res.exists` | Boolean | `true` when there is something at the path. A symbolic link to nothing is nothing |
| `res.type` | String | What is at the path: `file`, `dir`, `socket`, `pipe`, `device` or `other`, and `""` when there is nothing |
| `res.body` | String | The content of a regular file, and `""` for anything else |
| `req.path` | String | The path given |
| `status` | Integer | `0` when there is something at the path, and `1` otherwise |

Only a regular file is read. A directory, a socket, a pipe or a device is told to be there, and its `res.body` is empty: a pipe is not opened, which would wait for a writer.

## Examples

### Wait for a Server to Start

The step is tried again until the log says the server has started:

```yaml
- name: Wait for the server to start
  uses: file
  with:
    path: ./server.log
  retry:
    max_attempts: 60
    interval: 1s
  test: res.body contains "server started"
```

### Count Lines

`lines` splits `res.body` into its lines, which the functions of expressions over lists take:

```yaml
- name: Two workers are started
  uses: file
  with:
    path: ./server.log
  test: 'count(lines(res.body), # contains "Worker started") == 2'
```

A test that starts with a quote or holds ` #` is written in quotes, so that YAML does not take `#` for a comment.

### A File Written by What Is Tested

`res.body` is a string. A file of JSON is read with `parse_json`:

```yaml
- name: The export holds every order
  uses: file
  with:
    path: ./out/orders.json
  test: len(parse_json(res.body).orders) == 3
```

### A Socket Is Removed on Shutdown

Nothing at the path is a result the test can check, not a failed step, so a step can wait for a file to go:

```yaml
- name: The socket is removed
  uses: file
  with:
    path: /run/app.sock
  retry:
    max_attempts: 15
    interval: 2s
  test: '!res.exists'
```

## Under a Guard

The action reads and connects to nothing, so it runs under `--read-only` and `--allow-host` as it is. See [Guard](/guide/concepts/guard).

## Error Handling

Nothing at the path, or a path under a regular file, gives a result with `res.exists` false and `status` `1`. A step fails with an action error when `path` is not given, when what is at the path cannot be looked at or read, such as a file the run has no permission to read, and when a regular file is larger than 1,000,000 bytes, as much as the `file` function of expressions reads. A larger file is not cut short.
