# Embedded Action

The `embedded` action runs a **job file** as a single step, which lets shared setup or checks live in their own file and be reused from several workflows.

The file is a job, not a workflow: it holds `name`, `steps` and optionally `defaults` - there is no `jobs` key in it.

## Basic Syntax

**auth.yml:**
```yaml
name: Authentication
steps:
  - name: Get token
    id: get_token
    uses: shell
    with:
      cmd: echo "0123456789"
    test: res.code == 0
    outputs:
      mytoken: replace(res.stdout, '\n', '')
```

**workflow.yml:**
```yaml
name: Main
vars:
  environment: staging
  api_url: "{{API_URL}}"
jobs:
- name: Main
  steps:
    - name: Authenticate
      id: auth
      uses: embedded
      with:
        path: "./auth.yml"
        vars:
          environment: "{{vars.environment}}"
      test: res.code == 0
      outputs:
        token: res.outputs.mytoken

    - name: Call the API
      uses: http
      with:
        method: GET
        url: "{{vars.api_url}}/me"
        headers:
          authorization: "Bearer {{outputs.auth.token}}"
      test: res.code == 200
```

## Parameters

An embedded step names the job file to run and the variables to hand it.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `path` | String | Yes | - | Path to the job file. Resolved against the current working directory, not the workflow file |
| `vars` | Object | No | `{}` | Variables passed to the embedded job, read there as `vars.<name>` |

Inside the job file, a local [external action](/guide/concepts/actions#external-actions) such as `uses: ./greet` is found relative to the job file itself, the way one in a workflow is found relative to the workflow file.


### Named External Actions

A step of the job file can use a name the workflow gives an external action under [`actions`](/reference/yaml-configuration#actions), in `uses` and as a key of `defaults`. The job file has no `actions` of its own: the names are those of the workflow whose step embeds it.

**workflow.yml:**
```yaml
actions:
  redis: github.com/mozership/probe-redis@7d60e0699a914e3c987ed5f2403ed8a7f3d176fa # v0.1.0

jobs:
- name: Session
  steps:
    - name: Check the session store
      uses: embedded
      with:
        path: "./jobs/session.yml"
      test: res.code == 0
```

**jobs/session.yml:**
```yaml
name: Session store
defaults:
  redis:
    url: redis://localhost:6379
steps:
  - name: The store answers
    uses: redis
    with:
      commands: [PING]
    test: res.results[0] == "PONG"
```

- The commit is written once, in the workflow, for the job files it embeds as well. A job file embedded by two workflows runs the action each of them names.
- A job file embedded by a job file is read by the same names, those of the workflow.
- A name that stands for a local path, as `greet: ./greet` does, stands for the directory next to the workflow file, wherever the job file is.
- Under a [guard](/guide/concepts/guard), `--allow-action` takes the action in full, not the name. For a name that stands for a local path, that is the absolute path of the directory.
- A name the workflow does not give is not an action. The run stops before its first job, with exit status 2, and says which step of which job file uses it, for the job file a step embeds and the ones that job embeds in turn. When a `path` is only known as the run goes, as one read from `outputs` is, the step that embeds the job fails instead, saying the same. `probe check` reports the name on the line of the `path` of the step that embeds the job, unless the path is a template.

## Response Object

After the embedded job finishes, `res` carries its result and its outputs.

| Field | Type | Description |
|-------|------|-------------|
| `res.code` | Integer | `0` when every step of the embedded job passed |
| `res.outputs` | Object | The outputs published by the embedded job's steps, keyed by output name |
| `res.report` | String | The embedded job's report, which is also nested into the parent report |
| `res.error` | String | Error message, when the embedded job failed |
| `res.dump` | Boolean | Always `false` |
| `status` | Integer | Same as `res.code` |
| `rt` | Object | Time spent running the embedded job |

`res.outputs` is keyed by output name, so a value published as `mytoken` is read as `res.outputs.mytoken`. The same value is also under the id of the step that published it, as `res.outputs.get_token.mytoken`. When two steps publish the same name, `res.outputs.<name>` holds the value of the step that published it first, and the step-keyed form holds each one.
