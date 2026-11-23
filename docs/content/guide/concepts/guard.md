# Guard

The guard limits what a run may do: `--read-only` refuses what writes, and `--allow-host` refuses connecting to any host but those named. Whoever runs Probe sets it, on the command line or by environment variables, and nothing in a workflow can loosen it. It lets a workflow, such as one a coding agent wrote, be run against a system it must not change.

```bash
probe --read-only --allow-host api.staging.example.com workflow.yml
```

The guard is kept by the actions. Each action is the one that knows what it is about to send, so each action decides what the guard allows, and Probe runs an action under a guard only when the action declares that it keeps to it.

## Who Does What

**The action** decides. It is told the guard of the run, and before it sends anything it refuses what the guard does not allow, returning a refusal that names the reason. The `http` action refuses a `POST` under `--read-only`, and a redirect to a host `--allow-host` does not name; the `db` action refuses a statement that may write. How an action tells a write from a read is its own, and is described on its page.

**Probe** does not look at what an action sends. It:

- builds the guard from the flags and the environment, and tells it to each action a step runs
- reads which kinds of guard each action declares it keeps to, and refuses a step whose action does not declare every kind the run is under, before the action runs, unless `--allow-action` names the action
- fails a refused step with the kind `refused`, which exits with status 2, and does not retry it
- runs a job of the `embedded` action under the same guard; a step of that job that is refused refuses the step that embeds it

## Declaring the Guard

An action declares the kinds of guard it keeps to:

| Kind | Flag | What the action refuses |
|---|---|---|
| `read-only` | `--read-only` | What may write |
| `allow-host` | `--allow-host` | Connecting to a host the run does not allow |

A step runs under a guard only when its action declares every kind the run is under. An action that declares `read-only` alone runs under `--read-only`, and is refused under `--allow-host`. A kind Probe does not know is ignored, so an action that names one is refused under a guard it does not name.

A built-in action declares the kinds in its package. An [external action](/guide/concepts/actions#external-actions) declares them in `guard` in its `action.yml`:

```yaml
name: greet
description: Say hello over HTTP
guard: [read-only, allow-host]
runs:
  using: binary
  url: https://github.com/<owner>/probe-greet/releases/download/v0.1.0/probe-greet_{os}_{arch}
  checksums:
    linux_amd64: <SHA-256 of probe-greet_linux_amd64>
```

Probe reads `action.yml` before the first job starts, for every external action the workflow uses. It downloads the executable of one only when the action runs under the guard, so a step that is refused fetches no executable.

## Trusting a Declaration

Probe takes an action at its word: it cannot tell whether an action that declares a kind of guard keeps to it. The guard therefore holds as far as the actions a workflow uses keep their word, and a workflow can name an external action that declares a guard it does not keep. Before running a workflow under a guard, look at the external actions it uses. A remote one is pinned by commit, so the action that runs is the one that was looked at. A local one, named by a path, is not pinned: it runs the files at that path, so look at those files, and at who can change them.

The guard is not a sandbox. It keeps a workflow from writing to, or reaching, what it was not meant to, as far as each action can tell what it is about to do. A write the database refuses, such as `WITH x AS (DELETE ...) SELECT ...`, fails as the database reports it rather than as `refused`. To bound what a run can reach whatever its actions do, run Probe where the network allows only that, such as in a container under a network policy.

## Built-in Actions

| Action | `--read-only` | `--allow-host` |
|---|---|---|
| `http` | Sends only `GET`, `HEAD` and `OPTIONS` | The host of the URL, and of each redirect; a URL without a port is taken at the port of its scheme |
| `db` | Runs one statement that starts with `SELECT`, `SHOW`, `DESCRIBE`, `DESC`, `EXPLAIN` or `WITH` and holds no semicolon but at its end, over a DSN that runs no statement on connecting, in a read-only transaction, or on a SQLite connection that only queries, so that the database refuses a write the statement hides | Each server the driver may connect to, as the driver reads the DSN: for MySQL the address go-sql-driver dials; for PostgreSQL those lib/pq resolves the DSN to, with its parameters, a service file and `PGHOST`, `PGHOSTADDR`, `PGPORT` and the like, every host of a list, and the address of `hostaddr` when it is given; the driver's port when none is named; a SQLite file names no host |
| `embedded` | Runs the job under the guard | Runs the job under the guard |
| `grpc` | Calls only a method that every definition at hand, the server's reflection and the `.proto` files of `proto`, declares `idempotency_level = NO_SIDE_EFFECTS`; a Connect call without `proto` has none, and is refused | The host and port of `addr`, port 443 when none is named, and the DNS server of a `dns://server/` target, port 53 when none is named, or with `protocol: connect` the host of the URL at the port of its scheme; a target that names no host, such as a Unix socket, is refused |
| `dns` | Nothing to refuse: a query writes nothing | The server the query is sent to, at port 53 when `server` names none, or 853 with `protocol: tls`; without `server`, every resolver of the machine |
| `file` | Only reads: `write`, `append` and `delete` are refused | Nothing to reach |
| `hello` | Nothing to refuse | Nothing to reach |

The built-in `shell`, `ssh`, `smtp` and `imap` cannot tell what a command or a script will do, and declare no guard, nor does the external [browser](/reference/actions/browser) action, which cannot tell what a page will do. A step using one is refused under a guard unless `--allow-action` names it, and it then runs as it is, without the guard.

```bash
probe --read-only --allow-action shell workflow.yml
```

## Keeping to the Guard in an External Action

An external action is given the guard of the run in `Call.Guard`, as a built-in one is. It keeps to it by returning `actionrpc.Refuse(...)` for what the guard does not allow, before sending anything:

```go
func (a *Action) RunStep(call actionrpc.Call) (map[string]any, map[string]any, error) {
    req, err := parse(call.With)
    if err != nil {
        return nil, nil, err
    }
    if call.Guard.ReadOnly && req.writes() {
        return nil, nil, actionrpc.Refuse("%s may write, and the run is read-only", req.Name)
    }
    if err := call.Guard.CheckHost(req.Host); err != nil {
        return nil, nil, err // already a refusal
    }
    // ...
}
```

`Guard.ReadOnly`, `Guard.AllowsHost` and `Guard.CheckHost` tell what the guard allows. Check every host the action connects to, a redirect's included. Then declare in `action.yml` the kinds the action keeps to, and only those.

## See Also

- **[CLI Reference](/reference/cli-reference#--read-only)** - `--read-only`, `--allow-host` and `--allow-action`
- **[Actions](/guide/concepts/actions#external-actions)** - External actions and `action.yml`
