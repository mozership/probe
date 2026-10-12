# Working with Coding Agents

When a coding agent changes a service, it also writes the unit tests that say the change is right. Code and tests then come from the same reading of the task, and a wrong reading passes both. A Probe workflow checks the change from outside: it drives the running service over the protocols its clients use, and looks at what comes back, the row that was written and the mail that was sent.

This guide sets a project up so that an agent writes and runs such a workflow as part of every change, and so that you can trust the result. These are the three things it comes to: the workflow is checked before it runs, it is run against the service and its database, and it can be run where it must not write.

<img class="diagram" src="/demo.svg" alt="A terminal in three scenes: probe check reports the mistakes in a workflow an agent wrote, the fixed workflow calls an API and queries the row it wrote, and under --read-only its POST is refused." />

## Install Probe Where the Agent Works

An agent often works in a fresh container that does not have Probe. Install it there with whichever of these the container has:

```bash
go install github.com/mozership/probe/cmd/probe@latest
brew install mozership/tap/probe
```

Put the command in the setup script of the sandbox when the agent has one, and otherwise in its instructions, as below. Where there is neither Homebrew nor Go, [Installation](/guide/introduction/installation#3-download-a-release) has the commands that download a release and verify its checksum.

## Install the Skill

The skill teaches the agent to write, check and debug a workflow. It is built into the binary, and it tells the agent to read `probe guide`, the reference that is built in as well, so what the agent writes fits the version installed rather than what it remembers of another one.

```bash
probe skill install                        # .claude/skills/probe/SKILL.md, for Claude Code
probe skill install .agents/skills/probe   # any other directory an agent reads skills from
probe skill                                # print it, to put it somewhere else
```

Commit the file, and run the command again after upgrading Probe.

## Say When to Use It

The skill says how to use Probe. When to use it is yours to say, in the file your agent reads its instructions from, such as `AGENTS.md` or `CLAUDE.md`:

```markdown
## End-to-end checks

- After changing what the service does over the network (an endpoint, a query, a mail it sends), add or update a Probe workflow under `probe/` that exercises the change against the running service.
- Read `probe guide` before writing YAML, and do not guess parameters.
- Run `probe check <file>` and fix every error, then run `probe --report markdown=probe-report.md <file>`.
- The change is done when the workflow passes. Put `probe-report.md` in the pull request.
- If `probe` is not installed: `brew install mozership/tap/probe`, or `go install github.com/mozership/probe/cmd/probe@latest`
```

Change the directory and the conditions to fit the project. Say also how the service is started for the run, such as `docker compose up -d`, since a workflow needs something to talk to.

## What the Agent Does

With the skill and the instructions in place, a change goes through these steps.

1. The agent reads the pages of `probe guide` it needs, and writes or updates a workflow.
2. `probe check` reads the workflow without running it. A key a run would ignore, such as `tset` for `test`, is an error with its line, and a step that nothing checks is a warning.
3. `probe` runs it. The exit status says where to look: `1` is a failed test or a broken contract, `2` the workflow itself, `3` a target that did not answer.
4. On a failure the agent reads the Markdown report, which has the test, the kind of failure, and the request and response of each failed step, and fixes the service or the workflow.

```bash
probe check probe/signup.yml
probe --report markdown=probe-report.md probe/signup.yml
```

## Let the Spec Judge, Not the Agent

A `test` the agent wrote can agree with a response the agent got wrong. With `openapi`, the http action checks each request and response against your OpenAPI document, so the document decides what is right; `proto` does the same for the grpc action from your `.proto` files.

```yaml
- name: Users
  defaults:
    http:
      url: "{{vars.api_url}}"
      openapi:
        spec: ./openapi.yml
  steps:
  - name: Read a user
    uses: http
    with:
      get: /users/1
    test: res.body.name != ""
```

After the run, `probe coverage` tells which operations and responses of the document a step checked, and which none reached:

```bash
probe --report json=probe-report.json probe/users.yml
probe coverage openapi.yml probe-report.json
```

See [Checking Against OpenAPI](/reference/actions/http#checking-against-openapi) for the parameters.

## Run What the Agent Wrote Safely

To run a workflow you have not read against a system it must not change, set the guard. `--read-only` refuses what writes, and `--allow-host` refuses connecting to any host but those named, both before anything is sent:

```bash
probe --read-only --allow-host api.staging.example.com probe/users.yml
```

A refused step fails with the kind `refused`, and the run exits with status 2. The guard is set by whoever runs Probe, and nothing in a workflow can loosen it; the skill tells the agent not to drop or widen it. Actions that cannot tell what they are about to do, such as `shell`, are refused under a guard unless `--allow-action` names them. [Guard](/guide/concepts/guard) says what each action refuses, and what the guard does not cover.

## Run the Same Workflow in CI

The workflow the agent ran is a file in the repository, so CI runs it again on the pull request, and the report goes to the job summary for the reviewer:

```yaml
name: E2E
on: [pull_request]
jobs:
  probe:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    - run: docker compose up -d --wait
    - uses: mozership/probe-action@v1.3.0
      with:
        path: probe/signup.yml
        options: --report github-summary
```

A reviewer then reads what the service did, step by step, beside the diff of the code.
