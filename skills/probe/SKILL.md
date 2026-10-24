---
name: probe
description: Write, run and debug Probe workflows, the YAML files the `probe` CLI runs to test and monitor HTTP APIs, gRPC services, databases, mail, SSH hosts, shell commands and browsers. Use when creating or changing a Probe workflow, when a `probe` run fails, or when asked to add an API test, a health check or a monitor with Probe.
---

# Probe

Probe runs a YAML workflow: jobs that run in parallel unless they declare `needs`, each a list of steps that call an action and check the result with a `test` expression. The same file serves as a test in CI and, with `repeat`, as a monitor.

## Read the docs from the binary first

The installed `probe` prints its own documentation, matching its version. Read the pages you need before writing YAML instead of relying on memory:

```bash
probe guide                       # list the topics
probe guide yaml                  # every workflow, job and step field
probe guide http                  # one action's parameters and response (also: shell, db, grpc, ssh, ...)
probe guide concepts/expressions  # expressions, templates and the functions available
probe guide cli                   # flags, reports, exit codes
```

Before writing a new workflow, look for existing ones in the repository and follow their layout and naming.

## A workflow

```yaml
name: API check

secrets:
- API_TOKEN # its value never appears in output or reports

vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"
  token: "{{API_TOKEN}}"

jobs:
- name: Users API
  id: users
  defaults:
    http:
      url: "{{vars.api_url}}"
      headers:
        authorization: "Bearer {{vars.token}}"
  steps:
  - name: Create a user
    id: create
    uses: http
    with:
      post: /users
      headers:
        content-type: application/json
      body:
        name: probe
    test: res.code == 201 && res.body.name == "probe"
    outputs:
      user_id: res.body.id
  - name: Read it back
    uses: http
    with:
      get: "/users/{{outputs.create.user_id}}"
    test: res.code == 200
    echo: "fetched {{res.body.name}}"

- name: After the users job
  needs: [users]
  steps:
  - name: Hello
    uses: hello
```

## Rules that are easy to get wrong

- `jobs` and `steps` are lists. `needs` lists job `id`s, so a job others depend on needs an `id`.
- Environment variables are read only in the top-level `vars`, by bare name: `"{{API_URL}}"`, with a fallback as `"{{API_URL ?? 'default'}}"`. Expressions in steps see `vars`, `res`, `req`, `rt`, `status`, `outputs` and `repeat_index`, and nothing else; there is no `env`.
- `test` and `outputs` values are expressions, written without braces. `with` values, `echo`, step and job names, and `vars` are templates with `{{ }}`.
- `test` must evaluate to a boolean. A number or string fails the step with `test_type`.
- `outputs` are read by name, as `outputs.<name>`, and through the step's `id` when it has one, as `outputs.<step-id>.<name>`; give the step an `id` when another step publishes the same name. An id with a hyphen needs brackets: `outputs['create-user'].user_id`.
- Templates are evaluated in keys as well as values, at any depth of `with` and of a map under `vars`: `"{{vars.header}}": x` sends the key it comes to. Two keys that come to the same key fail the step. The name of a var itself is not evaluated: `vars: {"{{vars.name}}": x}` defines a var named as written.
- A template ends at the `}}` that closes it: braces of a map literal nest, and `}}` inside a quoted string does not end it. Start a template that begins with a map literal with a space, `{{ {'a': 1} }}`, since `{{{` is read as a literal `{` followed by a template. A `{{` that is never closed is left as text.
- Defaults shared by the steps of a job go in that job's `defaults`, keyed by action name; those every job shares, such as a base URL or a DSN, go in a top-level `defaults`, which a job's own come before. There is no top-level `env`.
- The steps of a job all run, also after one that failed. When later steps only work on what an earlier one made, set `fail_fast: true` on the job, and the steps after a failed one are skipped.
- `iteration` runs a step once per entry of a list of objects, read as `vars`. In place of the list it takes one template that comes to one when the step is reached: cases kept in a file, `"{{ parse_json(file('cases.json')) }}"`, or what an earlier step published, `"{{ outputs.users.rows }}"`.
- A job that removes what others made, such as a test database, lists them in `needs` and sets `always: true`: it then runs when one of them failed too, where any other job is skipped.
- An external action, `github.com/<owner>/<repo>@<commit SHA>`, used in more than one step is given a name once under the top-level `actions`, as `actions: {graphql: github.com/…@<SHA>}`, and `uses` and `defaults` then take the name. A name cannot be that of a built-in action. A job file run by `embedded` uses the names of the workflow that embeds it.
- List secrets under `secrets` by environment variable name. Never write a credential into the YAML.
- Do not guess parameters. If a field is not on the action's `probe guide <action>` page, it does not exist.

## Run

```bash
probe workflow.yml                    # run; the report goes to stdout
probe base.yml staging.yml            # merge files, later ones override; base.yml,staging.yml too
probe --env-file .env workflow.yml    # variables for vars to read, where the environment has none
probe --var url=http://localhost:8080 workflow.yml  # give a var in place of the one written
probe check workflow.yml              # find what is wrong or weak without running
probe fmt workflow.yml                # lay the file out the one way, in place
probe dag workflow.yml                # check the job graph without running
probe -v workflow.yml                 # also print every request and response
probe --report json=probe.json,markdown=probe.md workflow.yml
probe coverage openapi.yml probe.json # what of the OpenAPI document, or a .proto file, no step checked
```

After writing or changing a workflow, run `probe fmt` on it, which lays it out the one way every workflow is: it changes blank lines, indentation and comments, never a value. Then run `probe check` before running it, and fix every error it reports: a key it calls unknown is one a run ignores without a word, such as a misspelled `test`, or `bdoy` for an action's `body`. Each warning is a step that checks less than it seems to: give a step that nothing checks a `test`, and replace a `test` that reads nothing with one that reads `res`.

The exit code says what to look at:

| Code | Meaning | Where to look |
|---|---|---|
| `0` | Every job succeeded | |
| `1` | A `test` was false, could not be evaluated, or was not a boolean, a step's template could not be evaluated, or a request or response broke its OpenAPI document | The response, or the test expression or template |
| `2` | The workflow or the command line is wrong, or the guard of the run refused a step | The `[ERROR]` line on stderr: YAML, an unknown `needs`, a step id, a flag; or a step that failed as `refused` |
| `3` | An action returned an error, such as a refused connection or a timeout | Whether the target is reachable, and the action's parameters |

When the http steps give `openapi`, or the grpc steps `proto`, run `probe coverage` after the workflow passes. An operation or a response marked `-` is one no step checked: add a step for it when it matters, such as an error response a client relies on.

## Debug a failure

1. Rerun with `--report markdown=probe-report.md` and read that file. Each failed step has its test, the failure kind, and the request and response it saw.
2. Act on the kind:
   - `assertion`: the test was false. Compare the response with what the test expects; fix whichever is wrong.
   - `test_error` or `test_type`: the expression itself is wrong. Check field names against `probe guide <action>` and the syntax against `probe guide concepts/expressions`.
   - `template`: a template in the step's `with`, `vars` or `name` could not be evaluated, so the action did not run. The message names the value, as `with.headers.authorization`. Usually an earlier step did not publish the output it reads; check that step, or fall back with `?.` and `??`.
   - `action`: the action could not run. Check the URL or host, credentials, and `timeout`.
   - `refused`: the guard the run was started with, `--read-only` or `--allow-host`, does not allow what the step asks for. The guard is the person's to set: do not drop it, widen it or add `--allow-action` yourself, and do not write or pick an external action whose `action.yml` declares a `guard` it does not keep to. Change the step to keep to it, such as reading rather than writing, or tell the person what the step needs.
   - `contract_request`: the step sent what the OpenAPI document given in `openapi`, or the `.proto` files given in `proto`, does not allow. Fix the step to send what the document or the files ask for. Only when the step means to send it, to see it rejected, add `request: false` under `openapi` or `proto`.
   - `contract_response`: the response broke the OpenAPI document given in `openapi`, or the `.proto` files given in `proto`. Each violation names what is wrong, and the field when there is one. The document or the files say what is right, so fix the server, or them when they are the ones out of date; do not drop `openapi` or `proto` to make the step pass. A property or a field reported as not declared under `strict: true` is one the server returns but the document or the files do not promise, which may be a leak such as `password_hash`: remove it from the server, or declare it when it is meant to be there.
3. For more detail, run with `-v`, which prints every request and response. Declared secrets and credential headers are masked there too.
4. After a fix, run the workflow again and confirm the exit code is `0` before calling it done.
