# Agent Action

The agent action tests a web application from a task written in natural language. A model, the tester, uses the application through a real Chrome to carry out the task; another call of the model, the judge, then decides from what the pages showed whether the expectation holds. Chrome or Chromium has to be installed where Probe runs, and the model is reached with the API key of its provider.

It is an [external action](/guide/concepts/actions#external-actions), published in [mozership/probe-agent](https://github.com/mozership/probe-agent). Probe downloads it the first time a workflow uses it, and runs the executable whose SHA-256 the `action.yml` at the pinned commit names. It needs Probe v1.21.0 or later.

The action cannot tell what a page will do, nor what the model will click, so it keeps to no [guard](/guide/concepts/guard): under `--read-only` or `--allow-host`, a step that uses it is refused unless `--allow-action` names it. The tester is kept to `allow_hosts`: a page on another host is not opened, and the browser goes back from one a click led to.

It has two modes:

- **Scenario**, with `task` and `expect`: the tester does the task, and the judge decides whether the expectation holds. For checks that should pass, as before a merge.
- **Persona**, with `persona` and `goal`: the tester plays the persona pursuing the goal, and notes what is wrong on the way; the reviewer decides whether the persona got there, and which of the notes, and of what the browser recorded, are findings. For looking for what nobody wrote a test of, as on a staging environment again and again.

It does not replace deterministic steps. Check what can be stated exactly, such as a status, a row or a mail, with them, and let them gate a merge. The agent action is for what they cannot state: whether a user gets through, or whether the application still holds up on staging once data and settings pile up. Run its persona tests with [`repeat`](/reference/yaml-configuration#repeat) against staging, and they keep using the application as people do.

## Basic Syntax

A workflow pins the action by a full commit SHA. The notes of each [release](https://github.com/mozership/probe-agent/releases) start with the action and its commit to copy. The examples here give it a name once, under [`actions`](/reference/yaml-configuration#actions), which needs Probe v1.24.0 or later; with an earlier one, write the action in full in each `uses`.

```yaml
name: Sign-up
actions:
  agent: github.com/mozership/probe-agent@4d0d6ccee6165624283b22fa0541b02d17e50ac5 # v0.2.0
secrets:
  - ANTHROPIC_API_KEY
  - TEST_PASSWORD
vars:
  test_password: "{{TEST_PASSWORD}}"
jobs:
- name: sign-up
  steps:
  - name: A new user signs up
    uses: agent
    with:
      url: https://staging.example.com/
      task: |
        As a new user of this service, sign up with the email address
        {secret:EMAIL} and the password {secret:PASSWORD}.
      expect: |
        The account is created, and the dashboard greets the user.
      secrets:
        EMAIL: "qa+{{random_str(8)}}@example.com"
        PASSWORD: "{{vars.test_password}}"
    timeout: 5m
    test: res.verdict == "pass"
```

A persona test takes `persona` and `goal` in place of `task` and `expect`:

```yaml
  - name: A first-time visitor writes a first note
    uses: agent
    with:
      url: https://staging.example.com/
      persona: |
        Carol, who has never used a notes app and is not technical. She signs
        up with {secret:EMAIL} and the password {secret:PASSWORD}.
      goal: Create an account, and write a first note that says "Buy milk".
      secrets:
        EMAIL: "qa+{{random_str(8)}}@example.com"
        PASSWORD: "{{vars.test_password}}"
    timeout: 5m
    test: len(filter(res.findings, .severity == "high" && .verified)) == 0
```

A test in natural language is not deterministic: the same step can take another path, or come to another verdict, from one run to the next. Check what matters with a deterministic step after it where you can, such as an `http` or `db` step that the account exists, and count the passes over runs with [`repeat`](/reference/yaml-configuration#repeat) rather than trusting one run.

## How It Works

1. Chrome opens `url`.
2. The tester is given `task` and `expect`, and browser tools: it reads the page as a tree of elements with refs, clicks, types, fills in forms and takes screenshots, until it calls `finish`.
3. The judge is given the task, the expectation, the steps, the texts the pages showed, the final page and its screenshot, and the console errors and failed requests. It gives `pass` or `fail`, quoting the pages.
4. Each quote is looked for in what the pages showed. A `pass` that quotes nothing the pages showed is not taken, and the verdict is `blocked`.

The tester is told to describe what it saw, not to judge, so that the verdict comes from a call that did not do the work.

In a persona test, the tester also notes what is wrong as soon as it comes across it. The reviewer, in place of the judge, is given the notes besides the record, and the slowest requests. It keeps a note only where the record bears it out, drops those that come from the tester's own slips, and adds what the record shows that the tester did not note, such as an error of the server. Each finding quotes the record, and is `verified` when a quote is found in it.

## Parameters

These go directly under `with`.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `url` | String | required | The page the browser opens first, `http` or `https` |
| `task` | String | required in a scenario test | What the tester does, in natural language |
| `expect` | String | required in a scenario test | What has to hold afterwards, in natural language |
| `persona` | String | required in a persona test | Who the tester plays, in natural language: what they know, and how they go about things |
| `goal` | String | required in a persona test | What the persona sets out to do, in natural language |
| `secrets` | Object | none | Values the model never sees, by name. Write them as `{secret:NAME}` in `task` or `persona`. See [Secrets](#secrets) |
| `allow_hosts` | List or string | the host of `url` | The hosts the tester may open pages of: `host`, `host:port`, or `*.domain`. A list, or a string apart by commas |
| `provider` | String | `anthropic` | `anthropic`, `openai`, `xai`, `gemini` or `sakura`. See [Models](#models) |
| `model` | String | `claude-opus-5-5` with `anthropic`, required with the others | The model's ID |
| `effort` | String | `medium` | `low`, `medium`, `high`, `xhigh` or `max` |
| `base_url` | String | the provider's | The URL the provider's API is under, as for a proxy |
| `vision` | Boolean | `false` with `sakura`, else `true` | Whether the model sees images. Not with `anthropic` |
| `max_steps` | Integer | `40` | The calls the tester may make to the browser |
| `timeout` | Duration string or number of seconds | `4m` | Limit for the whole test, the judge included. Keep it below the step's `timeout`, which is `5m` unless set |
| `headless` | Boolean | `true` | Run Chrome without a window |
| `window_w`, `window_h` | Integer | `1280`, `800` | The viewport, in pixels |
| `cache` | String | none | A file to keep the steps of a scenario test that passed in, and to replay them from. See [Replay](#replay) |
| `cache_mode` | String | `auto` | `auto`, `read`, `strict` or `refresh`. See [Replay](#replay) |
| `evidence_dir` | String | the temporary directory | Where the screenshots and the report are written, each test in a directory of its own |

## Models

The action is built for Claude, which is trained to use a browser through the browser toolset of the Claude API. The models of the other providers are offered the same browser tools as tools of the action's own, so that they run the same tests, the judge, persona tests and replay included; how well they carry out a task depends on the model.

The action takes the API key from Probe's environment. List it under [`secrets`](/reference/yaml-configuration#secrets) of the workflow so that Probe hides it in its output.

| `provider` | API key | Notes |
|---|---|---|
| `anthropic` | `ANTHROPIC_API_KEY` | The default |
| `openai` | `OPENAI_API_KEY` | Reached through the Responses API, keeping nothing on OpenAI's side. `max` is sent as `xhigh` |
| `xai` | `XAI_API_KEY` | Reached through Chat Completions. `max` is sent as `xhigh` |
| `gemini` | `GEMINI_API_KEY` | Reached through the API compatible with Chat Completions. `xhigh` and `max` are sent as `high` |
| `sakura` | `SAKURA_AI_ENGINE_TOKEN` | The Sakura AI Engine, reached through Chat Completions. Its models differ in what they take, so `effort` is not sent, and they see no images unless `vision` is `true`. Choose one that calls tools, such as `gpt-oss-120b` |

```yaml
secrets:
  - OPENAI_API_KEY
jobs:
- name: sign-up
  steps:
  - name: A new user signs up
    uses: agent
    with:
      provider: openai
      model: gpt-5.4
      url: https://staging.example.com/
      task: Sign up with {secret:EMAIL} and the password {secret:PASSWORD}.
      expect: The dashboard greets the user.
```

A model that sees no images reads the pages by their elements and text alone: it is not offered screenshots, acts on elements rather than points of the page, and the judge is not shown the final page as an image. The screenshots are still kept as evidence.

## Response Object

| Field | Type | Description |
|-------|------|-------------|
| `res.mode` | String | `scenario` or `persona` |
| `res.verdict` | String | `pass`, `fail`, or `blocked` when the test could not be judged. In a persona test, whether the persona reached the goal |
| `res.findings` | List | In a persona test, what is wrong with the application, the most severe first: `title`, `severity` (`high`, `medium` or `low`), `category` (`error`, `functional`, `usability`, `performance`, `content` or `accessibility`), `expected`, `observed`, `evidence`, `steps`, `url`, `screenshot`, and `verified`, whether any of the evidence was found in the record |
| `res.reason` | String | Why, in the judge's words |
| `res.evidence` | List | What the judge quoted: `quote`, and `verified`, whether a page showed it |
| `res.stopped` | String | How the tester stopped: `done`, `blocked`, `max_steps`, `timeout`, `no_finish`, `refused`, or `stale` for a recording that no longer matched under `cache_mode: strict` |
| `res.summary` | String | What the tester said it did and saw |
| `res.steps` | List | Each call to the browser: `index`, `tool`, `input`, `result` or `error`, `url`, `screenshot` and the tester's `notes` |
| `res.url`, `res.title` | String | The page the tester left the browser on |
| `res.console_errors` | List | The errors the pages wrote to their console |
| `res.network_errors` | List | The requests that failed or were answered with 400 or more: `method`, `url`, `status`, `error` |
| `res.cache` | Object | With `cache`, how the test used it: `path`, `mode`, `status` (`miss`, `hit` or `partial`), `replayed`, the steps replayed, `diverged_at` and `reason`, where and why the recording stopped, `stale`, whether it stopped because the application no longer matches it, and `saved`, whether the run was recorded |
| `res.metrics` | Object | Measures that do not depend on the model, to compare runs by: `steps`, `duration_ms`, `requests`, `failed_requests`, `console_errors`, and `slowest_requests`, the ten requests that took longest, with `method`, `url`, `status` and `duration_ms` |
| `res.usage` | Object | The `requests` made to the model and the tokens they used |
| `res.model` | String | The model's ID |
| `res.filepaths` | Object | `report`, a Markdown report with a screenshot of each step; `trace`, the result as JSON; `screenshot`, the final page |
| `res.report` | String | A few lines Probe prints under the step |
| `status` | Integer | `0`; the verdict is for the step's `test` |

`blocked` is kept apart from `fail`: it is what the tester could not get past, such as a CAPTCHA or information the task does not give, rather than what the application did wrong.

## Replay

A scenario test that passes the same way every run need not ask the model how to do it every run. With `cache`, the steps of a run that passed are kept in a file, and the next run replays them without the model; only the judge is asked, so that what the pages show is still judged.

```yaml
    with:
      url: https://staging.example.com/
      task: Sign up with {secret:EMAIL} and the password {secret:PASSWORD}.
      expect: The dashboard greets the new user.
      cache: .probe-agent/signup.json
      cache_mode: "{{CACHE_MODE ?? 'auto'}}"
```

A replay:

1. Begins only on the page the recording began on, as the start URL may redirect elsewhere, such as to a login.
2. Finds each element a step acted on by its role and its name, such as `button "Create account"`. Where other elements of the page had both, it counts them too, and takes the same one of as many; another number of them is ambiguous, and the replay stops rather than guess.
3. Checks after each step that the page is on the route it reached when recorded, and shows no alert the recording did not. A route is a page's path and query, with what looks minted per record, such as an ID, a token or a date, read as one: `/orders/42` and `/orders/7` are one route, and `/companies` and `/companies?tab=notes` are two.
4. Ends without the model, or hands the test to the tester where it stopped, telling it what was replayed and why it stopped. Once the test passes, the run is recorded again.

A value the tester typed that the task spells out, or that it made up, such as a name, is typed again. A value it may have read off the screen, such as a code the application showed, or a date, belongs to that run: its step is recorded without the value, and the replay hands the test to the tester there.

The recording is of the task, the expectation and the start URL; when one of them changes, it is not replayed. It keeps the secrets as placeholders, and the URLs on the host of `url` as paths, so that a recording made against one host is replayed against another. It is of the steps, not of the model, so a test recorded with one model is replayed with another. It is written only when it changes, so that a committed one stays clean while it replays. A persona test takes no `cache`: it is to take another way each run.

| `cache_mode` | Replays | Records | Where the recording stops matching |
|---|---|---|---|
| `auto` | yes | a run that passed, if it changes the recording | the tester carries on |
| `read` | yes | never | the tester carries on |
| `strict` | yes | never | the test is blocked without the model, as `stopped: stale` |
| `refresh` | no | a run that passed | |

Commit the file, and run CI with `strict`, so that a recording that no longer matches fails the test rather than paying the tester on every run until someone records it again. Record it again with `auto` where someone looks at the change.

## Secrets

The model reads `{secret:NAME}` where a secret is. When it types the placeholder, the browser is given the value; what the pages show the model, and what the action returns, have each value replaced with its placeholder again, in any case, as an application can show an email address in lower case. A placeholder of a secret that was not given is an error the model is told of, not text it types.

## Running in CI

- Install Chrome or Chromium on the runner, and keep `headless: true`.
- Set the API key from a secret of the CI, and list it under `secrets` of the workflow.
- Upload `evidence_dir` as an artifact, so that the report and the screenshots of a failed test can be read.
- Run a test with a `cache` under `cache_mode: strict`, so that the model is asked only to judge while the recording matches.
