# Browser Action

The browser action drives a real Chrome through [chromedp](https://github.com/chromedp/chromedp): it opens pages, reads and types into them, waits for elements, and takes screenshots. Chrome or Chromium has to be installed where Probe runs.

It is an [external action](/guide/concepts/actions#external-actions), published in [mozership/probe-browser](https://github.com/mozership/probe-browser); it was built into Probe up to v1.21.0. Probe downloads it the first time a workflow uses it, and runs the executable whose SHA-256 the `action.yml` at the pinned commit names. It needs Probe v1.21.0 or later. The notes of each [release](https://github.com/mozership/probe-browser/releases) start with the `uses` line to copy.

The action cannot tell what a page will do, so it keeps to no [guard](/guide/concepts/guard): under `--read-only` or `--allow-host`, a step that uses it is refused unless `--allow-action` names it.

## Basic Syntax

A browser step lists what to do under `actions`, in order. Every step starts a fresh browser, runs its actions, and closes it, so cookies and a login do not carry over to the next step on their own: put a whole flow in one step, or save the login with `save_storage_state` and start the next step's browser from it, as described in [Carrying a login over](#carrying-a-login-over).

```yaml
steps:
  - name: Read the heading
    uses: github.com/mozership/probe-browser@634baa8dff2b273ed326996fec4d91c2dba9d38b # v0.2.0
    with:
      actions:
        - name: navigate
          url: "{{vars.url}}/"
        - name: text
          id: heading
          selector: h1
    test: res.code == 0 && res.results.heading == "Welcome"
```

## Parameters

These go directly under `with`.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `actions` | List | required | The actions to run, in order. See [Actions](#actions) |
| `timeout` | Duration string or number of seconds | `5s` | Limit for the whole step: starting the browser and every action. `30s`, `30` and `"30"` all mean 30 seconds; any other value, zero or less, is refused |
| `headless` | Boolean | `true` | Run Chrome without a window |
| `window_w` | Integer | `1920` | Window width in pixels |
| `window_h` | Integer | `1080` | Window height in pixels |
| `evidence_dir` | String | the temporary directory | Where the page is saved when the actions fail. See [Page at failure](#page-at-failure) |
| `storage_state` | String | none | A file `save_storage_state` wrote, whose cookies and localStorage the browser starts with. See [Carrying a login over](#carrying-a-login-over) |

Starting Chrome takes part of `timeout`, often a second or more, so a short limit can run out before the first page opens.

## Actions

Each entry of `actions` names its action in `name` and takes these fields:

| Field | Used by | Description |
|-------|---------|-------------|
| `name` | all | The action, from the tables below |
| `id` | actions that return something | The key for the result in `res.results` or `res.filepaths`. Without it the action's name is the key, so a later action of the same kind replaces the earlier result |
| `url` | `navigate` | The address to open |
| `selector` | actions on an element | A CSS selector |
| `value` | `type`, `send_keys`, `select` | The text to use |
| `attribute` | `get_attribute` | The attribute to read, as `href` or `[href]`. Of a list, only the first entry is read |
| `quality` | `full_screenshot` | `1` to `99` saves JPEG at that quality. Anything else, including unset, `0`, `100` and above, or a negative number, saves PNG |
| `path` | screenshots, `save_storage_state` | Of a screenshot, deprecated: also write the image to this path; use `res.filepaths` instead. Of `save_storage_state`, where to write the file, in place of a new one in the temporary directory |
| `equals` | `wait_text` | Wait until the text is exactly this. `equals: ""` waits for an empty text |
| `contains` | `wait_text` | Wait until the text contains this |
| `matches` | `wait_text` | Wait until the text matches this regular expression, in [Go's syntax](https://pkg.go.dev/regexp/syntax) |

A `wait_text` takes at most one of `equals`, `contains` and `matches`, and no other action takes them. An empty `contains` or `matches` is refused too, since any text would pass it. A workflow that sets them otherwise is refused before the browser starts.

An action that looks for an element waits until the element appears, and the step fails when `timeout` runs out first. Most of them also wait for it to be visible.

### Navigation and waiting

These open a page or hold the step until the page is in the state the next action needs.

| Action | What it does |
|--------|--------------|
| `navigate` | Opens `url` and waits for the page to load. A network error the browser reports, such as `net::ERR_CONNECTION_REFUSED`, fails it at once; an address that never answers keeps it waiting until `timeout` |
| `wait_visible` | Waits until `selector` is visible |
| `wait_not_visible` | Waits until `selector` is not visible |
| `wait_enabled` | Waits until `selector` is visible and enabled |
| `wait_ready` | Waits until the page's `body` is ready. `selector` is ignored |

### Reading the page

These put a string in `res.results`, under `id` or the action's name.

| Action | Result |
|--------|--------|
| `text` | The text content of `selector` |
| `wait_text` | Waits until `selector` is visible, then its text content. With `equals`, `contains` or `matches`, it also waits until the text is the one expected. See [Waiting for a text](#waiting-for-a-text) |
| `value` | The value of a form field, such as an `input` |
| `get_attribute` | The first attribute in `attribute` of `selector`, or an empty string when the element does not have it |
| `get_html` | The outer HTML of `selector`, once it is visible |

### Interacting

These act on the page the way a user would. They return nothing.

| Action | What it does |
|--------|--------------|
| `click` | Clicks `selector` once it is visible |
| `double_click` | Double-clicks `selector` once it is visible |
| `type`, `send_keys` | Clears `selector`, then types `value` into it |
| `submit` | Submits the form `selector` belongs to |
| `focus` | Focuses `selector` |
| `scroll` | Scrolls `selector` into view |
| `select` | Chooses the option of the `<select>` at `selector` whose value is `value`, or else whose label is, once it is visible, and fires `input` and `change` as a user's choice does. An option that is not there or is disabled, a `<select>` that is disabled, and an element that is not a `<select>`, fail the step with the reason, a `disabled` on an `<optgroup>` or a `<fieldset>` around them included; the values of the options are in the message. Of a `<select multiple>`, the option is added to those chosen |
| `hover` | Sends a `mouseover` event to `selector`. It does not wait, and does nothing when nothing matches |
| `right_click` | Sends a `contextmenu` event to `selector`. It does not wait, and does nothing when nothing matches |

### Screenshots

These save an image file and put its path in `res.filepaths`, under `id` or the action's name. The file is named for its format, `.png` or `.jpg`.

| Action | Image |
|--------|-------|
| `capture_screenshot` | The visible part of the window, as PNG |
| `full_screenshot` | The whole page, as PNG, or as JPEG when `quality` is `1` to `99` |
| `screenshot` | Only `selector`, once it is visible, as PNG |

### Storage state

| Action | What it does |
|--------|--------------|
| `save_storage_state` | Writes the cookies of the browser and the localStorage of the page open into a JSON file, and puts its path in `res.filepaths`, under `id` or the action's name. The file goes to `path`, or to a new `probe-browser-state-*.json` in the temporary directory. See [Carrying a login over](#carrying-a-login-over) |

## Response Object

When every action succeeds, the step sees:

| Field | Type | Description |
|-------|------|-------------|
| `res.code` | Integer | `0` |
| `res.results` | Object | What the reading actions returned, by `id` or action name |
| `res.filepaths` | Object | Where the screenshots and the storage states were saved, by `id` or action name |
| `res.console_errors` | List | The errors the page wrote to its console while the actions ran, in order. See [Checking the console](#checking-the-console) |
| `rt.duration` | String | How long the step took, such as `"663.268333ms"` |
| `status` | Integer | `0` |

When an action fails there is no response to test: the step fails with an action error, as described in [Error Handling](#error-handling).

## Examples

Each example is one step, since a step's actions share one browser and the next step starts a new one. [Carrying a login over](#carrying-a-login-over) is the exception: it has two steps, as it hands the login from one to the next.

### Logging in

A login and the check that it worked have to share one browser, so they are one step.

```yaml
secrets:
  - PASSWORD
vars:
  url: "{{APP_URL}}"
  user: "{{USERNAME}}"
  password: "{{PASSWORD}}"

jobs:
- name: Sign in
  steps:
  - name: Log in and reach the dashboard
    uses: github.com/mozership/probe-browser@634baa8dff2b273ed326996fec4d91c2dba9d38b # v0.2.0
    with:
      timeout: 30s
      actions:
        - name: navigate
          url: "{{vars.url}}/login"
        - name: type
          selector: "#username"
          value: "{{vars.user}}"
        - name: type
          selector: "#password"
          value: "{{vars.password}}"
        - name: click
          selector: "button[type='submit']"
        - name: wait_visible
          selector: "#dashboard"
        - name: text
          id: greeting
          selector: "#dashboard h1"
    test: res.code == 0 && res.results.greeting contains vars.user
```

### Reading several values

Give each reading action an `id` so the results do not replace each other.

```yaml
  - name: Product page
    uses: github.com/mozership/probe-browser@634baa8dff2b273ed326996fec4d91c2dba9d38b # v0.2.0
    with:
      actions:
        - name: navigate
          url: "{{vars.url}}/products/42"
        - name: text
          id: title
          selector: "h1"
        - name: text
          id: price
          selector: ".price"
        - name: get_attribute
          id: image
          selector: "img.product"
          attribute: [src]
    test: |
      res.code == 0 &&
      res.results.title != "" &&
      res.results.price startsWith "$"
    outputs:
      image_url: res.results.image
```

### Waiting for a text

A page that fetches its data shows it a moment after it loads, so a `text` read as soon as the element is visible can get `Loading` and fail the test. A `wait_text` with `equals`, `contains` or `matches` reads the text again until it is the one expected.

```yaml
  - name: Order list
    uses: github.com/mozership/probe-browser@634baa8dff2b273ed326996fec4d91c2dba9d38b # v0.2.0
    with:
      timeout: 30s
      actions:
        - name: navigate
          url: "{{vars.url}}/orders"
        - name: wait_text
          id: status
          selector: "#status"
          matches: '^\d+ orders$'
    test: res.code == 0
    outputs:
      status: res.results.status
```

When the step's `timeout` runs out first, the step fails with an action error that gives the last text read, and the page is saved, as described in [Page at failure](#page-at-failure):

```
wait_text "#status": the text was "Loading", want it to match "^\\d+ orders$": context deadline exceeded (page at failure: ...)
```

### Carrying a login over

A step that signs in can save the login with `save_storage_state`, and the steps after it start their browsers signed in by naming the file in `storage_state`, rather than each signing in again.

```yaml
  - name: Sign in
    id: login
    uses: github.com/mozership/probe-browser@634baa8dff2b273ed326996fec4d91c2dba9d38b # v0.2.0
    with:
      timeout: 30s
      actions:
        - name: navigate
          url: "{{vars.url}}/login"
        - name: type
          selector: "#username"
          value: "{{vars.user}}"
        - name: type
          selector: "#password"
          value: "{{vars.password}}"
        - name: click
          selector: "button[type='submit']"
        - name: wait_visible
          selector: "#dashboard"
        - name: save_storage_state
          id: state
    test: res.code == 0
    outputs:
      state: res.filepaths.state

  - name: Orders
    uses: github.com/mozership/probe-browser@634baa8dff2b273ed326996fec4d91c2dba9d38b # v0.2.0
    with:
      storage_state: "{{outputs.login.state}}"
      actions:
        - name: navigate
          url: "{{vars.url}}/orders"
        - name: wait_text
          selector: "h1"
          equals: "Orders"
    test: res.code == 0
```

The file holds the cookies of every site the browser has, `HttpOnly` ones included, and the localStorage of the origin of the page open when it is saved; to keep the localStorage of another origin, save the state while a page of it is open. It has the shape of [Playwright's storage state](https://playwright.dev/docs/auth), so that either tool reads what the other writes.

Before the first action, a browser given `storage_state` takes its cookies, and opens a page of each origin with items in it to write the localStorage there. Those pages are answered with an empty one inside the browser, so they reach no server and run none of the app's scripts, and the browser is back on `about:blank` when the first action runs. A file that is missing or is not a storage state fails the step before the browser starts, as when the step that writes it failed.

The cookies sign in as the user, so the file is readable only by the user who ran Probe. Without `path` it goes to the temporary directory, which keeps it out of the repository; with `path`, keep it out of version control, and out of the files a CI run uploads.

### Checking the console

A page can break behind what it shows, such as a click handler that throws. `res.console_errors` lists what the page wrote to its console as an error while the actions ran: each call of `console.error`, each `console.assert` that failed, and each exception or promise rejection nothing caught. Each has these fields:

| Field | Description |
|-------|-------------|
| `type` | `error`, `assert` or `exception` |
| `text` | What the DevTools console shows. An exception's begins with `Uncaught`, or `Uncaught (in promise)` for a rejection |
| `url`, `line`, `column` | Where it came from, the line and the column counted from 1 as in DevTools. Empty and `0` when the browser does not say |

The browser's own messages, such as a resource that failed to load, are not among them, nor are logs and warnings. The errors are read from the page's tab, so those of a popup, or of an iframe from another site, are not either.

To fail the step on any error, test that the list is empty:

```yaml
  - name: Checkout page
    uses: github.com/mozership/probe-browser@634baa8dff2b273ed326996fec4d91c2dba9d38b # v0.2.0
    with:
      actions:
        - name: navigate
          url: "{{vars.url}}/checkout"
        - name: click
          selector: "#pay"
        - name: wait_visible
          selector: "#receipt"
    test: res.code == 0 && len(res.console_errors) == 0
    echo: "{{res.console_errors}}"
```

When an action fails, the failure message names the first few of them, as one is often why it failed:

```
action error in step_execute: action execution failed (caused by: ... context deadline exceeded (console errors: "Uncaught TypeError: Cannot read properties of null (reading 'total') (http://localhost:8090/checkout.js:42:17)") (page at failure: ...))
```

### Keeping a screenshot

The path of the saved image is in `res.filepaths`, ready to print or pass on.

```yaml
  - name: Checkout page
    uses: github.com/mozership/probe-browser@634baa8dff2b273ed326996fec4d91c2dba9d38b # v0.2.0
    with:
      window_w: 1280
      window_h: 800
      actions:
        - name: navigate
          url: "{{vars.url}}/checkout"
        - name: full_screenshot
          id: page
    test: res.code == 0
    echo: "Saved {{res.filepaths.page}}"
```

## Error Handling

An action that cannot finish makes the whole step fail with an action error, which exits the run with status `3`. A selector that matches nothing is not an empty result: `wait_visible`, `text`, `click` and the other actions that look for an element keep waiting until the step's `timeout`. A navigation fails as soon as the browser reports a network error, such as a refused connection, while an address that never answers also waits until `timeout`. In every case there is no `res` to test, and the failure message carries the browser's error, along with the errors the page wrote to its console, as [Checking the console](#checking-the-console) shows.

### Page at failure

When the actions fail, Probe saves the page as it was at that moment: a full-page PNG screenshot and the HTML of the document, under one shared name such as `probe-browser-failure-1947177907.png` and `.html`. The failure message names both files along with the page's URL:

```
action error in step_execute: action execution failed (caused by: ... context deadline exceeded (page at failure: url http://localhost:8090/, screenshot /tmp/probe-browser-failure-1947177907.png, html /tmp/probe-browser-failure-1947177907.html))
```

The same message is the failure's `message` in the files `--report` writes. A test that evaluates to false is not an action failure, so the page is not saved then. To have it saved when a text is not the one expected, check the text with a [`wait_text`](#waiting-for-a-text) and `equals`, `contains` or `matches` rather than in `test`, or add a `capture_screenshot` action to keep the page in any case.

The files go to the system's temporary directory unless `evidence_dir` names another. The directory is created when needed, which suits uploading it as a CI artifact. The page can show private data, so the files are readable only by the user who ran Probe:

```yaml
- name: Checkout page
  uses: github.com/mozership/probe-browser@634baa8dff2b273ed326996fec4d91c2dba9d38b # v0.2.0
  with:
    evidence_dir: out/browser
    actions:
    - name: navigate
      url: "{{vars.url}}/checkout"
    - name: wait_visible
      selector: "#pay"
```

The browser is kept for up to 10 seconds after the timeout so that the page can still be read. If it cannot be, for example because the browser never started or the page was still loading when time ran out, the message says so and nothing is saved.

## Running in CI

A CI runner needs a browser and a little more time than a laptop.

- Install Chrome or Chromium on the runner.
- Keep `headless: true`, or provide a display such as Xvfb when a window is needed.
- Chrome is started with `--no-sandbox`, as containers usually require, so point it only at sites you trust.
- Give `timeout` room for Chrome to start, which is slower on a cold runner.
