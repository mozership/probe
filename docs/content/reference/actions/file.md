# File Action

The `file` action looks at what is at a path, and reads it when it is a regular file. Use it to check what a server wrote to its log, the content of a file that what is tested produced, its permissions and owner, or, with `retry`, to wait for a file or a socket to come or to go. With `op`, it writes, appends to or deletes a file, so that a workflow makes the files it tests with, and removes them, without `shell`.

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
| `path` | Yes | | The path to look at, relative to the working directory, as the paths of other actions are. A symbolic link is followed, except by `delete` |
| `op` | No | `read` | What to do at the path: `read`, `write`, `append` or `delete`. See [Write, Append and Delete](#write-append-and-delete) |
| `content` | No | `""` | What `write` writes, or `append` adds to the end |
| `mode` | No | | The permissions `write` and `append` set, as `chmod` takes them in octal, in quotes: `"0600"` |
| `recursive` | No | `false` | With `delete`, deletes a directory with all it holds |

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
| `req.op` | String | The op done, `read` when none is given |
| `req.content` | String | The content given, `""` when none is |
| `req.mode` | String | The mode given, `""` when none is |
| `req.recursive` | Boolean | Whether `recursive` was given true |
| `status` | Integer | `0` when there is something at the path, and `1` otherwise |

Whatever the op, `res` is what is at the path once it is done, as `read` gives it: after `write`, the file as it was written, and after `delete`, nothing.

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

## Write, Append and Delete

`op` changes what is at the path before the step looks at it.

| `op` | What it does |
|---|---|
| `read` | Nothing: it only looks |
| `write` | Writes `content` to the file, in place of what it held, and makes the file, and the directories to hold it, when they are not there |
| `append` | Adds `content` to the end of the file, and makes it, and the directories to hold it, when they are not there |
| `delete` | Deletes what is at the path. A directory is deleted only when it is empty, or with `recursive: true` along with all it holds |

A file that `write` or `append` makes has the mode `0644`, and a directory `0755`, less the umask of the run, as a shell gives them. `mode` sets the permissions of the file, whether it was made or was there; without `mode`, a file that was there keeps its own. Write `mode` in quotes: a number such as `0600` is read as decimal by some YAML parsers and as octal by others, and is an error. `write` and `append` follow a symbolic link, as a shell's `>` does, and write only to a regular file: a directory, a pipe or anything else at the path is an error. `content` is text: a number or a boolean is written as it reads, and a map or a list, such as a whole JSON `body` of an earlier step, is an error rather than written in a form of Probe's own. A file given a mode the run cannot read, such as `"0200"`, is told without its content.

`delete` deletes a symbolic link, not what it points to, with a trailing slash too. `..` is the directory that holds the one named before it, as the path is written, even when that one is a symbolic link. Nothing at the path is not an error, as with `rm -f`, so a step can remove what may not be there. The working directory, and a directory that holds it, such as `/`, are never deleted, whether a path names it as it is or through a symbolic link, so that a path made of a variable that turns out empty does not remove them.

`content`, `mode` and `recursive` are taken only by the ops that use them: `content` to `read` is an error, not ignored.

### Make a File, and Remove It

```yaml
jobs:
- name: Start with a config
  id: config
  steps:
  - name: Write the config
    uses: file
    with:
      path: ./tmp/app.yml
      op: write
      content: |
        port: 8080
        log: ./tmp/app.log
      mode: "0600"
    test: res.exists && res.mode == "0600"

- name: Clean up
  needs: [config]
  always: true
  steps:
  - name: Remove what the run made
    uses: file
    with:
      path: ./tmp
      op: delete
      recursive: true
    test: '!res.exists'
```

### A Log a Server Is Watching

```yaml
- name: A line comes into the log
  uses: file
  with:
    path: ./watched.log
    op: append
    content: |
      level=ERROR msg="disk full"
  test: res.exists
```

## Under a Guard

The action connects to nothing, so it runs under `--allow-host` as it is. Under `--read-only`, it only reads: a step with `write`, `append` or `delete` is refused, with the kind `refused`, before it changes anything. See [Guard](/guide/concepts/guard).

## Error Handling

Nothing at the path, or a path under a regular file, gives a result with `res.exists` false and `status` `1`. A file replaced while it is read, as a rotated log is, is looked at again, up to three times. A step fails with an action error when `path` is not given, when what is at the path cannot be looked at or read, such as a file the run has no permission to read, and when a regular file is larger than 1,000,000 bytes, as much as the `file` function of expressions reads. A larger file is not cut short, and a file that `write` or `append` makes larger is written, and then fails the step as it is read.

It fails as well when `op` is not one of the four, when `content`, `mode` or `recursive` is given to an op that does not take it, when `mode` is not octal digits in quotes, when `content` is a map or a list, when `write` or `append` is given a path that ends with a separator, or meets what is not a regular file, or cannot write, when `delete` meets a directory that is not empty without `recursive`, or names the working directory or one that holds it.
