# Slurm Action

The Slurm action submits jobs to [Slurm](https://slurm.schedmd.com/), waits for them to end and reads what they printed, cancels them, and reads the state of the partitions and the nodes of the cluster. It runs the commands of Slurm, `sbatch`, `squeue`, `sacct`, `scancel` and `scontrol`, on a login node over SSH, or on the host Probe runs on, and reads what they print with `--json`.

It is an [external action](/guide/concepts/actions#external-actions), published in [mozership/probe-slurm](https://github.com/mozership/probe-slurm). Probe downloads it the first time a workflow uses it, and runs the executable whose SHA-256 the `action.yml` at the pinned commit names. It is tested with Probe v1.40.0 and later. The examples write `<commit SHA>` for the commit to pin: `probe guide slurm` prints the `uses` line of the latest release, and so do the notes of each release.

## Basic Syntax

A workflow pins the action by a full commit SHA, and gives it a name once, under [`actions`](/reference/yaml-configuration#actions):

```yaml
name: Canary job
actions:
  slurm: github.com/mozership/probe-slurm@<commit SHA>
jobs:
- name: canary
  defaults:
    slurm:
      host: login.hpc.example.com
      user: probe
      key_file: ~/.ssh/id_ed25519
  steps:
  - name: The partition is up, with a node to spare
    uses: slurm
    with:
      partitions: debug
    test: res.partitions[0].state == "UP" && res.partitions[0].idle > 0

  - name: Submit a job
    id: submit
    uses: slurm
    with:
      submit:
        partition: debug
        time: 5m
        script: |
          #!/bin/bash
          srun hostname
    test: res.job_id > 0
    outputs:
      job_id: res.job_id

  - name: It ends well, soon enough
    uses: slurm
    with:
      job: "{{outputs.submit.job_id}}"
      wait: 30m
      output: true
    test: |
      res.state == "COMPLETED" && res.exit_code == 0 &&
      res.wait_sec < 600 && res.stdout != ""
```

A job is submitted by one step and waited for by another, so that a `retry` of the step that waits never submits the job again.

## Parameters

A step does one thing, by one of these keys of `with`:

| Key | What it does | Value |
|---|---|---|
| [`submit`](#submit) | Submits a job with `sbatch` | The job |
| [`job`](#job) | Reads the state of a job, waiting for it to end with `wait` | The ID of the job |
| [`jobs`](#jobs) | Lists the jobs in the queue | What the jobs have, or nothing for every job |
| [`cancel`](#cancel) | Cancels a job with `scancel` | The ID of the job |
| [`partitions`](#partitions) | Reads the state of a partition, and counts its nodes by their state | The name of a partition, or nothing for every one |
| [`nodes`](#nodes) | Reads the state of nodes | Nodes, as `c1` or `c[1-4]`, or nothing for every one |

The others say where the commands run:

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `host` | String | No | - | The login node to run the commands on, over SSH. Without it, they run on the host Probe runs on |
| `port` | Integer | No | `22` | The port of SSH |
| `user` | String | No | `USER` | The user to log in as |
| `key_file` | String | No | See [Logging in](#logging-in) | The private key to log in with. `~/` is the home directory |
| `key_passphrase` | String | No | - | The passphrase of `key_file` |
| `known_hosts` | String | No | `~/.ssh/known_hosts` and `/etc/ssh/ssh_known_hosts` | The file the host key of the login node is checked against |
| `bin_dir` | String | No | The `PATH` | The directory of the commands of Slurm, as `/opt/slurm/bin`, where the `PATH` of a command run over SSH does not find them |
| `timeout` | Duration | No | `30s`, and as long as `wait` besides | Time limit for the whole step, as `10s` or a number of seconds |

A key of `with` that is not one of these fails the step before anything is run. Its `action.yml` declares them as `params`, and the fields of the result as `res`, so `probe check` reports such a key, and a field of `res` the action does not return, with its line.

The job's `defaults`, keyed by the name the workflow gave the action, are merged key by key, so a job can keep its login node, and what its jobs are submitted with, out of each step:

```yaml
defaults:
  slurm:
    host: login.hpc.example.com
    submit:
      partition: debug
      account: lab
```

A `submit` without `script` or `file`, as those defaults are, is left out of a step that does something else.

### Logging in

The action logs in with `key_file` when it is given. Without it, it tries the keys of the agent at `SSH_AUTH_SOCK`, then those of `~/.ssh/id_ed25519`, `~/.ssh/id_ecdsa` and `~/.ssh/id_rsa` that have no passphrase. It does not log in with a password, nor with keyboard-interactive authentication, such as the one-time passwords some sites ask for.

The host key is always checked, against `known_hosts` or the files OpenSSH keeps, so a login node the files do not know fails the step. Add it first, as `ssh-keyscan login.hpc.example.com >> ~/.ssh/known_hosts` does once you have checked what it prints.

Each command runs as the line of a non-interactive shell of the user, as `ssh user@host 'squeue --json'` would. Where the commands of Slurm are only found once a login shell has set the `PATH`, give their directory as `bin_dir`.

`key_passphrase` is not shown in the result or in the action's own log. Declaring the variable it is read from under [`secrets`](/reference/yaml-configuration#secrets) hides its value anywhere else it could appear.

## Submit

`submit` sends the script to `sbatch --parsable` on its standard input, with the options below. It has `script` or `file`, and not both.

| Parameter | Type | Default | sbatch option | Description |
|---|---|---|---|---|
| `script` | String | - | - | The batch script, starting with `#!` |
| `file` | String | - | - | Path to the batch script on the host Probe runs on, relative to the working directory. It is read there and sent, as `script` is |
| `name` | String | `probe` | `--job-name` | The name of the job |
| `partition` | String | The cluster's default | `--partition` | |
| `account` | String | - | `--account` | |
| `qos` | String | - | `--qos` | |
| `time` | String or Integer | The partition's | `--time` | The time limit: a duration as `1h30m`, rounded up to whole minutes, a time as `sbatch` takes it as `1-12:00:00`, or a number of minutes, as `sbatch` reads one |
| `nodes` | Integer or String | - | `--nodes` | As `2`, or `1-4` |
| `ntasks` | Integer | - | `--ntasks` | |
| `cpus_per_task` | Integer | - | `--cpus-per-task` | |
| `mem` | String | - | `--mem` | As `4G` |
| `gres` | String | - | `--gres` | As `gpu:1` |
| `chdir` | String | The home directory | `--chdir` | The working directory of the job, on the cluster |
| `output` | String | `slurm-%j.out` | `--output` | Where the job writes its standard output, on the cluster |
| `error` | String | With `output` | `--error` | Where the job writes its standard error, on the cluster |
| `options` | Array | - | - | Other options of `sbatch`, each as `--exclusive` or `--constraint=a100` |
| `keep` | Boolean | `false` | - | Keeps the job out of the [cleanup](#cleanup) of the run |

Options in the script, as `#SBATCH --partition=long`, are taken too, and those given here win over them, as `sbatch` takes options on its command line.

### Response Object

| Property | Type | Description |
|---|---|---|
| `res.job_id` | Integer | The ID of the job, or `0` when `sbatch` refused it |
| `res.cluster` | String | The cluster the job went to, when `sbatch` names one, or empty |
| `res.error` | String | What `sbatch` said when it refused the job, as `sbatch: error: invalid partition specified: nope`, or empty |
| `req` | Object | What was done: `operation`, `host`, `port`, `user`, `bin_dir`, the `script` or `file`, `keep`, and the `command` that was run. It never has `key_passphrase` |
| `rt` | Duration | Time the step took |
| `status` | Integer | `0` when the job was submitted; `1` otherwise |

`sbatch` refusing the job is a result, so a test can assert on a partition or an account that should be closed. Only a command that cannot be run, such as on a login node that cannot be reached, fails the step as an error.

## Job

`job` reads the state of the job, from `squeue`, or from `sacct` once `slurmctld` has forgotten it. A job neither knows fails the step.

| Parameter | Type | Default | Description |
|---|---|---|---|
| `wait` | Duration | `0s` | How long to wait for the job to end. The step returns as soon as it has ended, and otherwise returns it as it is once `wait` is over |
| `poll` | Duration | `10s` | How often the job is looked at while the step waits, `1s` or longer |
| `output` | Boolean | `false` | Reads the standard output of the job, once it has started, into `res.stdout` |

The output is read on the host the commands run on, from the path `squeue` or `sacct` tells, so it must be on a file system the login node shares with the compute nodes, as the home directory usually is.

### Response Object

| Property | Type | Description |
|---|---|---|
| `res.job_id` | Integer | The ID of the job |
| `res.name` | String | The name of the job |
| `res.user` | String | The user it runs as |
| `res.account` | String | Its account |
| `res.partition` | String | Its partition |
| `res.state` | String | Its state, as `PENDING`, `RUNNING`, `COMPLETED`, `FAILED`, `CANCELLED`, `TIMEOUT`, `OUT_OF_MEMORY` or `NODE_FAIL` |
| `res.reason` | String | Why it waits, or why it ended, as `Priority`, `Resources` or `PartitionDown`, or empty |
| `res.nodes` | String | The nodes it runs on, as `c[1-2]`, or empty |
| `res.exit_code` | Integer | The exit code of its script |
| `res.signal` | Integer | The signal it was ended by, or `0` |
| `res.submit_time` | String | When it was submitted, as `2026-11-27T01:02:03Z` |
| `res.start_time` | String | When it started, or empty until it does |
| `res.end_time` | String | When it ended, or empty until it does |
| `res.wait_sec` | Integer | Seconds it waited in the queue, or has waited so far |
| `res.elapsed_sec` | Integer | Seconds it ran, or has run so far |
| `res.done` | Boolean | Whether it has ended |
| `res.stdout_path` | String | Where it writes its standard output, on the cluster |
| `res.stdout` | String | With `output`, the first 1 MiB of its standard output; empty until it starts |
| `res.stdout_truncated` | Boolean | With `output`, whether the output is larger than 1 MiB |
| `req` | Object | `operation`, the connection, `job_id`, `wait`, `output` and the `command` |
| `rt` | Duration | Time the step took |
| `status` | Integer | `0` when the job has completed with the exit code `0`; `1` otherwise, while it waits or runs too |

## Jobs

`jobs` lists the jobs `squeue` lists, by what they have. Each key narrows the list, and none lists every job in the queue.

| Parameter | Type | Description |
|---|---|---|
| `user` | String | The user they run as |
| `partition` | String | Their partition |
| `state` | String | Their state, as `pending` or `RUNNING`. Without it, `squeue` lists those pending and running |
| `name` | String | Their name |
| `account` | String | Their account |

`res.jobs` has each job, with the fields `job` returns but `stdout`, and `res.count` how many there are.

## Cancel

`cancel` cancels the job with `scancel`. The job is looked up first, as `scancel` exits with `0` for a job it does not know, or one that has ended: a job neither `squeue` nor `sacct` knows fails the step, and one that has ended is left as it is.

| Property | Type | Description |
|---|---|---|
| `res.job_id` | Integer | The ID of the job |
| `res.state` | String | The state of the job before it was cancelled |
| `res.cancelled` | Boolean | Whether it was cancelled; `false` for one that had ended |
| `res.error` | String | What `scancel` said when it refused, as for the job of another user, or empty |
| `status` | Integer | `0` unless `scancel` refused |

## Partitions

`partitions` reads the partition, or every partition, from `scontrol show partition`, and counts its nodes by their state, from `scontrol show node`. `res.partitions` has each, and `res.count` how many there are. A partition the cluster does not have fails the step.

| Property | Type | Description |
|---|---|---|
| `name` | String | The name of the partition |
| `state` | String | Its state, as `UP`, `DOWN`, `DRAIN` or `INACTIVE` |
| `total_nodes` | Integer | How many nodes it has |
| `idle`, `mixed`, `allocated` | Integer | How many of them are idle, partly allocated, and wholly allocated |
| `down` | Integer | How many are down, drained or not |
| `drained` | Integer | How many are drained, or being drained, and up |
| `other` | Integer | How many are in any other state, as `FUTURE` or `UNKNOWN` |

## Nodes

`nodes` reads the nodes from `scontrol show node`. `res.nodes` has each, and `res.count` how many there are; nodes the cluster does not have are not among them.

| Property | Type | Description |
|---|---|---|
| `name` | String | The name of the node |
| `state` | String | Its state, as `IDLE`, `MIXED`, `ALLOCATED` or `DOWN` |
| `flags` | Array | The flags of its state, as `DRAIN` or `NOT_RESPONDING` |
| `partitions` | Array | The partitions it is in |
| `reason` | String | Why it is down or drained, or empty |
| `cpus` | Integer | Its CPUs |
| `alloc_cpus` | Integer | How many of them are allocated |

## Cleanup

Once the run is over, whether it passed, failed or was interrupted, Probe calls the action, as [Cleaning Up After a Run](/guide/concepts/actions#cleaning-up-after-a-run) tells, to cancel the jobs its steps submitted, those that have ended aside, so a run that stops waiting leaves no job in the queue. A job submitted with `keep: true` is left.

The jobs are noted, with where they were submitted, in a file under the temporary directory, which the cleanup removes. The passphrase of a key is not noted, so the jobs submitted with `key_passphrase` are cancelled by logging in with the agent or the default keys. A cleanup that cannot log in is told as a warning, which names the jobs left.

## Under a Guard

The action keeps to the guard of the run, and its `action.yml` declares `guard: [read-only, allow-host]`, so Probe runs it under `--read-only` and `--allow-host` without `--allow-action`.

- Under `--read-only`, `job`, `jobs`, `partitions` and `nodes` are run, and `submit` and `cancel` are refused before anything is run.
- Under `--allow-host`, the commands run only on a `host` the run allows, as `login.hpc.example.com` or `login.hpc.example.com:2222`. Without `host`, the commands reach whatever `slurmctld` the host Probe runs on is set up for, which the guard cannot check, so a step without `host` is refused.

A refused step fails with the kind `refused`. See [Guard](/guide/concepts/guard).

## Slurm Versions

The action reads what the commands print with `--json`, which differs between the versions of Slurm, and is tested against what Slurm 23.02 to 24.11 print. The [end-to-end tests](https://github.com/mozership/probe-slurm/blob/main/e2e/workflow.yml) run it against Slurm 24.11 in a container.

Job arrays are not told apart: `job` and `cancel` take the ID of a job, and `jobs` lists each task of an array as a job of its own.

## See Also

- **[External Actions](/guide/concepts/actions#external-actions)** - How Probe resolves and checks an external action
- **[SSH](/reference/actions/ssh)** - Commands on a remote server over SSH
- **[Container](/reference/actions/container)** - Containers started for a run, and removed after it
