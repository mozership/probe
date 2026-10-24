# File Merging

Probe can build one workflow out of several files. This is how configuration is shared between workflows and how environment-specific settings are kept apart from the checks themselves.

## How Merging Works

Pass the files as separate arguments, or as one comma-separated argument. The two forms mean the same and can be mixed:

```bash
probe base.yml production.yml
probe base.yml,production.yml
probe common.yml,base.yml production.yml
```

Probe reads the files **in order and concatenates them into a single YAML document**, then parses that document. Nothing more clever happens: there is no per-key deep merge.

Two consequences follow from that, and both matter:

1. **A top-level key defined in more than one file takes the value from the last file.** Redefining `vars:` replaces the whole block rather than merging entry by entry, and redefining `jobs:` replaces every job.
2. **YAML anchors defined in one file can be referenced from a later file**, because they all end up in the same document.

A path may also be a directory or a glob. Every `.yml` and `.yaml` file found is concatenated in the order the paths are given:

```bash
probe common/,workflow.yml
probe "configs/*.yml"
```

## Splitting by Top-Level Key

The safe way to split a workflow is to let each file own different top-level keys.

**vars.yml:**
```yaml
vars:
  api_url: "{{API_URL ?? 'https://api.example.com'}}"
  timeout: "{{REQUEST_TIMEOUT ?? '30s'}}"
```

**workflow.yml:**
```yaml fragment
name: API Health Check
description: Basic API monitoring

jobs:
- name: Health Check
  defaults:
    http:
      url: "{{vars.api_url}}"
      headers:
        User-Agent: "Probe Monitor"
  steps:
    - name: API Health
      uses: http
      with:
        get: /health
      test: res.code == 200
```

```bash
probe vars.yml,workflow.yml
```

## Replacing a Whole Key per Environment

Because the last definition wins, an environment file can replace one key outright. Keep every value the workflow needs in that file - what it leaves out is gone, not inherited.

**workflow.yml:**
```yaml fragment
name: API Health Check

jobs:
- name: Health Check
  defaults:
    http:
      url: "{{vars.api_url}}"
  steps:
    - name: API Health
      uses: http
      with:
        get: /health
      test: res.code == 200
```

**production.yml:**
```yaml
vars:
  api_url: https://api.production.example.com
  environment: production
  timeout: 10s
```

```bash
probe workflow.yml,production.yml
```

The `vars` block from `production.yml` is the one that is used. If `workflow.yml` also declared `vars`, those entries would not survive.

## Sharing Values with Anchors

Since the files become one document, an anchor is the way to share a value without redefining a key.

**shared.yml:**
```yaml
shared:
  json_headers: &json_headers
    content-type: application/json
    accept: application/json
  auth_header: &auth_header
    authorization: "Bearer {{vars.token}}"
```

**workflow.yml:**
```yaml fragment
name: API Test
vars:
  api_url: "{{API_URL}}"
  token: "{{API_TOKEN}}"

jobs:
- name: Checks
  defaults:
    http:
      url: "{{vars.api_url}}"
      headers:
        <<: [*json_headers, *auth_header]
  steps:
    - name: List users
      uses: http
      with:
        get: /users
      test: res.code == 200
```

```bash
probe shared.yml,workflow.yml
```

The file holding the anchors has to come first, and keys that Probe does not know - `shared` here - are ignored.

## Practical Notes

- Put the anchor definitions first, then the workflow, then any environment override.
- Keep one owner per top-level key. Two files that both define `vars` is the most common surprise.
- Only one file can hold `jobs`. When a later file has `jobs` too, its jobs replace those of the earlier file, which do not run, and the run, `probe dag` and `probe check` warn of it with the line of each. This also happens with a glob such as `probe workflows/*.yml` over files that are each a workflow: run them one at a time.
- `probe dag workflow.yml,production.yml` loads the merged result without running it, which is the quickest way to check that the pieces fit together.

## What's Next?

- **[Workflows](/guide/concepts/workflows)** - Workflow structure
- **[Environment Management](/guide/how-tos/environment-management)** - Environment-specific configuration in practice
- **[YAML Configuration](/reference/yaml-configuration)** - Every key Probe reads
