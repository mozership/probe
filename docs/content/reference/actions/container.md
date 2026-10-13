# Container Action

The Container action starts a container for the steps after it, such as the database or the mail server an API under test needs, and removes it once the run is over.

It is an [external action](/guide/concepts/actions#external-actions), published in [mozership/probe-container](https://github.com/mozership/probe-container). Probe downloads it the first time a workflow uses it, and runs the executable whose SHA-256 the `action.yml` at the pinned commit names. It needs Probe v1.27.0 or later, which [calls an action once a run is over](/guide/concepts/actions#cleaning-up-after-a-run), and the `docker` command, or [another runtime](#runtimes) that takes the same arguments.

## Basic Syntax

A workflow pins the action by a full commit SHA. The notes of each [release](https://github.com/mozership/probe-container/releases) start with the action and its commit to copy. The examples here give it a name once, under [`actions`](/reference/yaml-configuration#actions).

```yaml
name: Orders
actions:
  container: github.com/mozership/probe-container@cd5c071a029cc1c6d4afe31750bf33ec6d9aa5b8 # v0.1.0
jobs:
  - name: orders
    steps:
      - name: Start Postgres
        id: pg
        uses: container
        with:
          image: postgres:17
          env:
            POSTGRES_PASSWORD: "{{vars.pg_password}}"
          ports: [5432]
          wait:
            port: 5432
        outputs:
          addr: res.addr

      - name: The schema is empty to begin with
        uses: db
        with:
          dsn: "postgres://postgres:{{vars.pg_password}}@{{outputs.pg.addr}}/postgres?sslmode=disable"
          query: SELECT count(*) AS n FROM pg_tables WHERE schemaname = 'public'
        test: res.rows[0].n == 0
```

A step is over once its container is started, and ready if the step has a `wait`. The container keeps running for the steps after it, in this job and in the others, and is removed, with its anonymous volumes, when the run is over: whether it passed, failed, or was ended by Ctrl+C. Nothing has to be written for that.

## Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `image` | String | Yes | - | The image to start |
| `command` | List | No | the command of the image | The command to run in the container, with one entry for each word, as `[redis-server, --appendonly, "yes"]` |
| `env` | Map | No | - | The environment of the container, as names and values |
| `ports` | List | No | - | The ports of the container to reach from the machine. See [Ports](#ports) |
| `volumes` | List | No | - | What to mount, each as the runtime takes it for `--volume`, such as `/abs/path/init.sql:/docker-entrypoint-initdb.d/init.sql:ro` |
| `network` | String | No | the default of the runtime | The network to connect the container to. It is one that exists; the action makes none |
| `name` | String | No | one the runtime makes up | The name of the container. Two runs at once cannot both have a container of one name, so leave it out unless something needs it |
| `pull` | String | No | `missing` | When to pull the image: `missing`, `always` or `never` |
| `wait` | Map | No | - | What the container has to do before the step is over. See [Waiting](#waiting) |
| `keep` | Boolean | No | `false` | Leave the container once the run is over. See [Keeping Containers](#keeping-containers) |

A key of `with` that is not one of these fails the step before anything is started. Its `action.yml` declares them as `params`, so `probe check` reports such a key with its line.

The action leaves the values of `env` out of its result and its log, which name the variables alone. The values are given to the runtime as arguments, so they can be seen in the list of processes of the machine while the container is being started, and in what the runtime tells of the container afterwards.

## Ports

Each entry of `ports` is a port of the container.

| Entry | Published at |
|-------|--------------|
| `5432` | A free port of `127.0.0.1`, which the result tells |
| `53/udp` | The same, for a protocol other than TCP: `udp` or `sctp` |
| `"8080:80"`, `"0.0.0.0:8080:80"` | Where the entry says, as the runtime takes it for `--publish` |

A port alone is the form to use: the runtime picks a port that is free, so two runs on one machine, or two jobs of one run, do not get in each other's way, and the port is not open to the network. Read where it is from the [result](#response-object).

## Waiting

Without `wait`, a step is over as soon as the container is started, which is before what runs in it is ready. `wait` holds the step until it is.

| Key | Type | Description |
|-----|------|-------------|
| `port` | Number | A TCP port of `ports` that has to take a connection and keep it. A runtime takes a connection on a published port before anything in the container listens, and closes it at once, so a connection that is only taken does not count |
| `log` | String | A [regular expression](https://pkg.go.dev/regexp/syntax) that has to match what the container wrote so far, to stdout and stderr together. It is matched against the whole log, so `(?s)ready.*ready` waits for a line that is written twice |
| `healthcheck` | Boolean | `true` waits for the health check of the image to pass. An image without one fails the step |
| `timeout` | Duration | How long to wait, as `90s` or a number of seconds. Defaults to `60s` |

When more than one is given, every one of them has to hold. The time it takes to pull the image is not part of the timeout.

The step fails when the container stops while it is waited for, or is not ready in time, and the error ends with the last lines of its log. The container is removed with the others once the run is over.

A server that takes connections before it can answer, as a database may, is better waited for by its log or its health check.

```yaml
steps:
  - name: Start Postgres, and wait until it is up for good
    uses: container
    with:
      image: postgres:17
      env:
        POSTGRES_PASSWORD: "{{vars.pg_password}}"
      ports: [5432]
      wait:
        # The image starts the server once to set it up, and then again.
        log: "(?s)ready to accept connections.*ready to accept connections"
        timeout: 90s
```

## Response Object

| Property | Type | Description |
|----------|------|-------------|
| `res.id` | String | The ID of the container |
| `res.name` | String | Its name, which another container on the same `network` reaches it by |
| `res.ports` | Object | The port on the machine for each entry of `ports`, by the port of the container: `res.ports["5432"]`, or `res.ports["53/udp"]` for a protocol other than TCP |
| `res.host` | String | The address the first entry of `ports` is reached at: `127.0.0.1`, unless the entry names another |
| `res.port` | Integer | The port on the machine for the first entry of `ports` |
| `res.addr` | String | `res.host` and `res.port` together, as `127.0.0.1:51110` |
| `req` | Object | What the container was started with, with `env` as the names of its variables |
| `rt` | Duration | How long the step took |
| `status` | Integer | `0` |

`res.host`, `res.port` and `res.addr` are there only when the step has `ports`.

A container that cannot be started, or that is not ready, is an error and not a result, so the step fails whatever its `test` is.

## Keeping Containers

To look into a container after a run, such as one a test failed against, have it kept:

- `PROBE_CONTAINER_KEEP=1 probe workflow.yml` keeps every container of the run, without a change to the workflow.
- `keep: true` on a step keeps the container of that step.

Once the run is over, the action tells which containers it kept and the command that removes them:

```
[WARN]  actions.probe-container: kept containers of the run: run_id=88041310b4926f7b count=1 remove="docker rm --force --volumes $(docker ps --all --quiet --filter label=probe.run_id=88041310b4926f7b)"
[WARN]  actions.probe-container: kept container: name=sleepy_dubinsky image=postgres:17 ports=127.0.0.1:51110->5432/tcp
```

Every container the action starts has the label `probe.run_id`, with the ID of the run, and one that is kept has `probe.keep=true` as well. `docker ps --all --filter label=probe.run_id` lists what is left of every run.

## What Is Left Behind

Probe calls the action once a run is over, and the action then removes every container with the label of that run. A container is left only when:

- the step or the environment asks for it to be [kept](#keeping-containers);
- Probe is killed outright, as by `kill -9`, so that it never gets to clean up;
- the runtime does not remove the container within 25 seconds, which Probe tells as a warning.

Volumes with a name, and networks, are not the action's, and stay.

## Runtimes

The action runs `docker`. `PROBE_CONTAINER_RUNTIME` names another command that takes the same arguments, such as `podman` or `nerdctl`, or the path of one:

```bash
PROBE_CONTAINER_RUNTIME=podman probe workflow.yml
```

It is read from the environment and not from a step, as it belongs to the machine and not to the workflow, and the cleanup of a run has to use the runtime its steps did.

The action builds no image and runs nothing in a container that is already there. A [`shell`](/reference/actions/shell) step can do either with the runtime itself.

## Under a Guard

The action keeps to no guard: what runs in a container, and the hosts it connects to, are not the action's to limit. A run under `--read-only` or `--allow-host` refuses its steps, unless `--allow-action` names the action.

See [Guard](/guide/concepts/guard).

## See Also

- **[External Actions](/guide/concepts/actions#external-actions)** - How Probe resolves and checks an external action
- **[Cleaning Up After a Run](/guide/concepts/actions#cleaning-up-after-a-run)** - How Probe calls an action once a run is over
- **[Database](/reference/actions/db)** - Queries on MySQL, PostgreSQL and SQLite
- **[Redis](/reference/actions/redis)** - Commands on a Redis or Valkey server
