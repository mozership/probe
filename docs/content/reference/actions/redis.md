# Redis Action

The Redis action runs commands on a Redis or Valkey server and returns their replies, so a workflow can check what an API left in a cache or a session store.

It is an [external action](/guide/concepts/actions#external-actions), published in [mozership/probe-redis](https://github.com/mozership/probe-redis). Probe downloads it the first time a workflow uses it, and runs the executable whose SHA-256 the `action.yml` at the pinned commit names. It needs Probe v1.21.0 or later.

## Basic Syntax

A workflow pins the action by a full commit SHA. The notes of each [release](https://github.com/mozership/probe-redis/releases) start with the action and its commit to copy. The examples here give it a name once, under [`actions`](/reference/yaml-configuration#actions), which needs Probe v1.24.0 or later; with an earlier one, write the action in full in each `uses`.

```yaml
name: Session
actions:
  redis: github.com/mozership/probe-redis@7d60e0699a914e3c987ed5f2403ed8a7f3d176fa # v0.1.0
vars:
  password: "{{PASSWORD}}"
  redis_password: "{{REDIS_PASSWORD}}"
jobs:
  - name: session
    steps:
      - name: Sign in
        id: login
        uses: http
        with:
          url: https://api.example.com
          post: /login
          body: {user: ada, password: "{{vars.password}}"}
        test: res.code == 200
        outputs:
          session: res.body.session_id

      - name: The session is in the store, and expires
        uses: redis
        with:
          url: redis://cache.example.com:6379/0
          password: "{{vars.redis_password}}"
          commands:
            - [HGET, "session:{{outputs.login.session}}", user]
            - [TTL, "session:{{outputs.login.session}}"]
        test: status == 0 && res.results[0] == "ada" && res.results[1] > 0
```

Every step opens a new connection, runs its `commands` in order, and closes it, so what belongs to a connection does not carry over to the next step. Put a transaction, or a `HELLO 3` and the commands that follow it, in one step.

## Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `url` | String | Yes | - | The server, as `redis://[username:password@]host[:port][/db]`. `rediss` connects over TLS, and `valkey` and `valkeys` are other names for the two. The port defaults to `6379`, and `db` is the number of the database to select |
| `username` | String | No | - | The user to authenticate as, when the URL names none |
| `password` | String | No | - | The password, when the URL has none |
| `commands` | List | Yes | - | The commands to run, in order. See [Commands](#commands) |
| `timeout` | Duration | No | `30s` | Time limit for the whole step, from connecting to the last reply, as `10s` or a number of seconds. `0` removes it |
| `insecure_skip_tls` | Boolean | No | `false` | Accept the certificate of a `rediss` server without verifying it. Use it only for servers you control |

A key of `with` that is not one of these fails the step before anything is sent. Its `action.yml` declares them as `params`, so `probe check` reports such a key with its line.

When a username or a password is given, in the URL or in `with`, the action authenticates with `AUTH` before the commands, and then selects the database of the URL, if it names one. Probe hides the value of `password` wherever it shows a step, and from v1.24.0 the password inside `url` too.

The action connects to the one server the URL names. It does not follow the `MOVED` and `ASK` redirects of a cluster, and it does not ask a sentinel for a master: such a redirect is an error reply like any other.

## Commands

Each entry of `commands` is one command, in either of two forms.

| Form | Example | How it is read |
|------|---------|----------------|
| List | `[SET, greeting, hello world]` | Each entry is one word of the command: a string as it is, and a number as its digits. `true`, `false` and `null` are refused, as Redis has no such values; quote them to send them as strings |
| String | `SET greeting "hello world"` | Split into words at spaces, as `redis-cli` does. Double quotes keep spaces and take escapes such as `\n` and `\"`; single quotes keep everything but `\'` |

The list form needs no quoting, so it is the one to use for a value that comes from a template.

These commands are never run:

- `AUTH`, and `HELLO` with `AUTH`: the username and the password are given in `url`, or in `username` and `password`, so that they are not shown with the commands.
- `SUBSCRIBE`, `PSUBSCRIBE`, `SSUBSCRIBE`, `MONITOR`, `SYNC`, `PSYNC` and `CLIENT REPLY`: the server does not answer them with one reply, which is what the action reads for each command.

## Response Object

| Property | Type | Description |
|----------|------|-------------|
| `res.results` | Array | The reply to each command, in the order of `commands` |
| `res.errors` | Array | The errors the server answered with; empty when there are none |
| `req` | Object | The `url`, without its password and with its port, and the `commands`, each as the list of words that was sent |
| `rt` | Duration | Time from connecting to the last reply |
| `status` | Integer | `0` when the server answered no command with an error; `1` otherwise |

A reply becomes a plain value.

| Reply | Value |
|-------|-------|
| A string, such as that of `GET`, or a status, such as `OK` | String |
| An integer, such as that of `INCR` or `TTL` | Number |
| Nothing, such as `GET` of a key that does not exist | `null` |
| A list, such as that of `LRANGE` or `MGET`, or a set | Array, each entry a value of this table |
| A map, a double, a boolean or a big number, which a server sends after `HELLO 3` | Object, number, boolean, and the digits as a string |
| An error | `null`, and an entry in `res.errors` |

A string that is not UTF-8 becomes an object with its bytes in base64, as `{"base64": "//4="}`.

Without `HELLO 3` the server answers in RESP2, in which a map is a list of keys and values in turn: `HGETALL` of a hash with `name` and `age` is `["name", "Ada", "age", "36"]`. Put `HELLO 3` before it in the same step to have it as `{"name": "Ada", "age": "36"}`.

```yaml
steps:
  - name: A hash as an object
    uses: redis
    with:
      url: redis://localhost:6379
      commands:
        - HELLO 3
        - [HGETALL, "user:1"]
    test: res.results[1].name == "Ada"
```

Each entry of `res.errors` has these properties.

| Property | Type | Description |
|----------|------|-------------|
| `index` | Integer | Which entry of `commands` the server answered with the error, from `0`. `-1` for the `AUTH` or the `SELECT` the action sends itself |
| `command` | String | The name of the command, in upper case, such as `LPUSH` or `OBJECT ENCODING` |
| `code` | String | The first word of the error, which names its kind: `ERR`, `WRONGTYPE`, `NOAUTH`, `WRONGPASS`, `NOPERM`, `MOVED` and so on |
| `message` | String | The whole error, as the server sent it |

Once the server is connected to, what it answers is a result, so a test can assert on an error. An error does not stop the step: the commands after it are run, and their replies are in `res.results` at their own index.

```yaml
steps:
  - name: The key is not a list
    uses: redis
    with:
      url: redis://localhost:6379
      commands:
        - [SET, greeting, hello]
        - [LPUSH, greeting, x]
        - [GET, greeting]
    test: |
      status == 1 &&
      res.results == ["OK", nil, "hello"] &&
      res.errors[0].index == 1 && res.errors[0].code == "WRONGTYPE"
```

When the server refuses the `AUTH` or the `SELECT` of the action, the commands are not sent: `res.results` is empty and `res.errors` has the one error, with the index `-1`.

Only a step that cannot talk to the server fails as an error: a connection that is refused, a certificate that is not trusted, a server that closes the connection, a reply larger than the action reads (a string of 16 MiB, or a list of 1,048,576 entries), or a timeout.

## Under a Guard

The action keeps to the guard of the run, and its `action.yml` declares `guard: [read-only, allow-host]`, so Probe runs it under either without `--allow-action`. A step it refuses fails with the kind `refused` before connecting.

- Under `--read-only`, a step is run only when every one of its commands is known to only read. Otherwise the whole step is refused, and the refusal names the command.
- Under `--allow-host`, the host and the port of `url` must be one the run allows. A URL without a port is taken at `6379`. The action connects to no other host.

The commands known to only read are these. A command that reads in one form and writes in another, as `SORT` does with `STORE`, is not among them, and nor are scripts and functions, whose effect cannot be told from the command: use the `_RO` form where there is one.

| Group | Commands |
|-------|----------|
| Keys | `EXISTS`, `TYPE`, `TTL`, `PTTL`, `EXPIRETIME`, `PEXPIRETIME`, `KEYS`, `SCAN`, `DBSIZE`, `RANDOMKEY`, `DUMP`, `SORT_RO`, `OBJECT ENCODING`, `OBJECT FREQ`, `OBJECT IDLETIME`, `OBJECT REFCOUNT`, `MEMORY USAGE` |
| Strings and bits | `GET`, `MGET`, `STRLEN`, `GETRANGE`, `SUBSTR`, `LCS`, `GETBIT`, `BITCOUNT`, `BITPOS`, `BITFIELD_RO` |
| Hashes | `HGET`, `HMGET`, `HGETALL`, `HKEYS`, `HVALS`, `HLEN`, `HEXISTS`, `HSTRLEN`, `HSCAN`, `HRANDFIELD`, `HTTL`, `HPTTL`, `HEXPIRETIME`, `HPEXPIRETIME` |
| Lists | `LRANGE`, `LLEN`, `LINDEX`, `LPOS` |
| Sets | `SMEMBERS`, `SISMEMBER`, `SMISMEMBER`, `SCARD`, `SRANDMEMBER`, `SSCAN`, `SINTER`, `SUNION`, `SDIFF`, `SINTERCARD` |
| Sorted sets | `ZRANGE`, `ZRANGEBYSCORE`, `ZRANGEBYLEX`, `ZREVRANGE`, `ZREVRANGEBYSCORE`, `ZREVRANGEBYLEX`, `ZSCORE`, `ZMSCORE`, `ZCARD`, `ZCOUNT`, `ZLEXCOUNT`, `ZRANK`, `ZREVRANK`, `ZSCAN`, `ZRANDMEMBER`, `ZINTER`, `ZUNION`, `ZDIFF`, `ZINTERCARD` |
| Streams | `XRANGE`, `XREVRANGE`, `XLEN`, `XREAD`, `XPENDING`, `XINFO STREAM`, `XINFO GROUPS`, `XINFO CONSUMERS` |
| Geo | `GEOPOS`, `GEODIST`, `GEOHASH`, `GEOSEARCH`, `GEORADIUS_RO`, `GEORADIUSBYMEMBER_RO` |
| Connection and server | `PING`, `ECHO`, `TIME`, `INFO`, `SELECT`, `HELLO`, `CLIENT ID`, `CLIENT GETNAME`, `CLIENT INFO`, `CLIENT LIST`, `COMMAND` and its `COUNT`, `INFO`, `DOCS`, `LIST`, `GETKEYS` and `GETKEYSANDFLAGS`, `PUBSUB CHANNELS`, `PUBSUB NUMSUB`, `PUBSUB NUMPAT`, `PUBSUB SHARDCHANNELS`, `PUBSUB SHARDNUMSUB` |
| Transactions | `MULTI`, `EXEC`, `DISCARD`, `WATCH`, `UNWATCH`, each command between them checked like any other |

`OBJECT`, `MEMORY`, `XINFO`, `CLIENT`, `COMMAND` and `PUBSUB` also take `HELP`. The commands of a module, such as `JSON.GET` or `FT.SEARCH`, are not known to the action, so a step that uses one has to be allowed with `--allow-action`.

See [Guard](/guide/concepts/guard).

## See Also

- **[External Actions](/guide/concepts/actions#external-actions)** - How Probe resolves and checks an external action
- **[Database](/reference/actions/db)** - Queries on MySQL, PostgreSQL and SQLite
- **[S3](/reference/actions/s3)** - Objects in S3 and S3-compatible storage
