# File Action

The `file` action looks at what is at a path, and reads it when it is a regular file. Use it to check what a server wrote to its log, the content of a file that what is tested produced, its permissions and owner, or, with `retry`, to wait for a file or a socket to come or to go.

## Basic Syntax

A file step gives the path, and its test reads the file from `res`.

```yaml
steps:
  - name: No error is logged
    uses: file
    with:
      path: ./server.log
    test: res.exists && !(res.content contains "level=ERROR")
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
| `res.content` | String | The content of a regular file, as text, and `""` for anything else |
| `res.mode` | String | The permissions in octal, as `chmod` takes them, with the setuid, setgid and sticky bits: `0644`, `0600`, `4755` |
| `res.owner` | String | The name of the user that owns it, or the user's number when it has no name; see below |
| `res.group` | String | The name of the group that owns it, or the group's number when it has no name; see below |
| `res.size` | Integer | The size in bytes, as the system gives it; that of a directory depends on the file system |
| `res.modified` | String | When it was last modified, as RFC 3339 with the time zone of the machine, such as `2026-11-01T09:30:00.5+09:00`. `date` reads it as a time |
| `req.path` | String | The path given |
| `status` | Integer | `0` when there is something at the path, and `1` otherwise |

Only a regular file is read. A directory, a socket, a pipe or a device is told to be there, and its `res.content` is empty: a pipe is not opened, which would wait for a writer.

With nothing at the path, `res.mode`, `res.owner`, `res.group` and `res.modified` are `""`, and `res.size` is `0`. A symbolic link is followed, so they are those of what it points to.

On Linux, Probe reads the names of users and groups from `/etc/passwd` and `/etc/group` only. One that a directory service such as LDAP gives a name, through NSS, is given by its number, though `ls -l` shows the name: compare with the number on such a host. On macOS, Probe asks the system, and gets the names of a directory service too.

`res.content` is the text of the file as it is, even when it looks like JSON, unlike the `body` of an HTTP response: a log of JSON lines is text to search. Read a file of JSON with `parse_json`.

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
  test: res.content contains "server started"
```

### Count Lines

`lines` splits `res.content` into its lines, which the functions of expressions over lists take:

```yaml
- name: Two workers are started
  uses: file
  with:
    path: ./server.log
  test: 'count(lines(res.content), # contains "Worker started") == 2'
```

A test that starts with a quote or holds ` #` is written in quotes, so that YAML does not take `#` for a comment.

### A File Written by What Is Tested

`res.content` is text. A file of JSON is read with `parse_json`:

```yaml
- name: The export holds every order
  uses: file
  with:
    path: ./out/orders.json
  test: len(parse_json(res.content).orders) == 3
```

### Permissions and Owner

```yaml
- name: The key is private to the app
  uses: file
  with:
    path: /etc/app/key.pem
  test: res.mode == "0600" && res.owner == "app" && res.group == "app"
```

### Written Recently

`res.modified` is read as a time by `date`, and a time minus a time is a duration:

```yaml
- name: The backup ran in the last day
  uses: file
  with:
    path: /var/backups/db.sql.gz
  test: res.size > 0 && now() - date(res.modified) < duration("24h")
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

Nothing at the path, or a path under a regular file, gives a result with `res.exists` false and `status` `1`. A file replaced while it is read, as a rotated log is, is looked at again, up to three times. A step fails with an action error when `path` is not given, when what is at the path cannot be looked at or read, such as a file the run has no permission to read, and when a regular file is larger than 1,000,000 bytes, as much as the `file` function of expressions reads. A larger file is not cut short.
