# iOS Action

The iOS action drives an app on an iOS simulator through XCUITest: it launches the app, taps and types into it, waits for elements, reads their text and takes screenshots. It runs on macOS alone, with Xcode.

It is an [external action](/guide/concepts/actions#external-actions), published in [mozership/probe-ios](https://github.com/mozership/probe-ios). Probe downloads it the first time a workflow uses it, and runs the executable whose SHA-256 the `action.yml` at the pinned commit names. It needs Probe v1.27.0 or later, which [calls an action once a run is over](/guide/concepts/actions#cleaning-up-after-a-run).

## How It Works

The action works through a test runner of XCUITest, which it builds from its source with the Mac's own Xcode the first time a step needs it, and keeps in a cache. The first step of a run that uses it starts the runner on the simulator with `xcodebuild test-without-building`, and sends it each action over HTTP on `127.0.0.1`; the runner finds the element with XCUITest and acts on it. The runner is kept for the run's other steps, and stopped once the run is over.

| What | How |
|------|-----|
| Xcode | `xcode-select -p` has to point at it, not at the Command Line Tools: `sudo xcode-select -s /Applications/Xcode.app` |
| An iOS simulator runtime | `xcodebuild -downloadPlatform iOS` |
| A booted simulator | `xcrun simctl boot "iPhone 17"`, or open Simulator.app |
| The app under test | Installed on the simulator: `xcrun simctl install booted path/to/MyApp.app` |

The action does not boot the simulator or install the app. Do that in a step of its own, such as a [`shell`](/reference/actions/shell) step, so that a failed boot is not taken for a failed test.

## Basic Syntax

A workflow pins the action by a full commit SHA. The notes of each [release](https://github.com/mozership/probe-ios/releases) start with the action and its commit to copy.

```yaml
name: Settings
actions:
  ios: github.com/mozership/probe-ios@aed5436143e9536754e2123b953831f60cd635eb # v0.2.0
jobs:
  - name: settings
    steps:
      - name: Open General
        uses: ios
        timeout: 15m
        with:
          app: com.apple.Preferences
          actions:
            - name: launch
              args: ["-AppleLanguages", "(en)", "-AppleLocale", "en_US"]
            - name: tap
              selector: com.apple.settings.general
            - name: wait
              selector: type=navigationbar identifier=General
            - name: text
              id: back
              selector: type=button identifier=BackButton
        test: res.code == 0 && res.results.back == "Settings"
```

The launch arguments start the app in English whatever language the simulator is set to, so that the labels a selector names do not change with it.

## The First Run

The first step that uses the action builds the runner, which takes from tens of seconds to a few minutes, and says so even without `-v`:

```
[WARN] ios: building the iOS test runner; the first run takes a few minutes, so give the step timeout: 10m if it runs out
```

A step has 5 minutes by default, which a first build can run out of, so the example gives the first step a [`timeout`](/reference/yaml-configuration#steps) of its own. Later steps take the runner from the cache, `~/Library/Caches/probe-ios`, or `PROBE_IOS_CACHE_DIR` when it is set; it is built again once the action or Xcode changes. Steps that start at once build it once, and the others wait for that build.

A simulator runs one runner at a time, so two runs of Probe at once need a simulator each. Steps of one run that use the same simulator take turns.

## Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `app` | String | Yes | - | The bundle identifier of the app the actions drive |
| `actions` | List | Yes | - | The actions to take, in order. See [Actions](#actions) |
| `device` | String | No | the one booted simulator | The simulator, by its name or UDID. It has to be booted, and has to be given when more than one is |
| `timeout` | Duration string or number of seconds | No | `30s` | Limit for the actions, from the first to the last. An action on an element that it runs out on waits up to a minute more for the runner to say why it failed, as XCUITest first waits for the app to be idle, which can take that long on a simulator just booted. It does not count building the runner or starting it, which the step's own `timeout` bounds |

## Actions

Each entry of `actions` names its action in `name`:

| Action | Fields | What it does |
|--------|--------|--------------|
| `launch` | `args` | Launches the app afresh, ending it first if it runs, with `args` as its launch arguments |
| `activate` | | Brings the app to the foreground, launching it if it is not running |
| `terminate` | | Ends the app |
| `tap` | `selector` | Taps the element |
| `type` | `selector`, `value` | Taps the element and types `value` into it |
| `wait` | `selector` | Waits until the element exists |
| `text` | `selector`, `id` | Reads the element's label, or its value when it has no label, into `res.results` |
| `tree` | `id` | Reads the app's accessibility tree, as XCUITest describes it, into `res.results`. It shows what a selector can name |
| `screenshot` | `id` | Saves the screen as PNG, and puts its path in `res.filepaths` |

`id` is the key of the result; without it, the action's name is. An action on an element waits for it to appear, for what is left of `timeout`, and the step fails when it does not. An entry with a key an action does not take, such as a misspelt `selector`, is refused before the step runs.

### Selectors

A selector names an element by key=value pairs, separated by spaces. A value with a space goes in double quotes.

| Key | Matches |
|-----|---------|
| `identifier` (or `id`) | The accessibility identifier, as `.accessibilityIdentifier("login")` in SwiftUI sets it |
| `label` | The accessibility label, which is mostly the text shown |
| `type` | The element type: `alert`, `any`, `button`, `cell`, `image`, `link`, `navigationbar`, `other`, `searchfield`, `securetextfield`, `slider`, `switch`, `tabbar`, `text`, `textfield`, `textview` |
| `index` | Which of the matching elements, from 0. Without it, the first |

A selector with no `=` is an identifier, so `login` is `identifier=login`.

## Response Object

| Field | Description |
|-------|-------------|
| `res.code` | `0` when every action succeeded |
| `res.results` | What `text` and `tree` read, by key |
| `res.filepaths` | The files `screenshot` saved, by key |
| `res.device` | The UDID of the simulator the actions ran on |

An action that cannot finish fails the step with an action error, which exits the run with status `3`, and there is no `res` to test:

```
actions[0] tap: no element matches label=No Such Row within 4.5s
```

## CI

On GitHub Actions, run the workflow on a macOS runner, and create and boot a simulator before Probe runs. [probe-action](https://github.com/mozership/probe-action) installs Probe on Linux alone, so the job installs it with `go install`. Keep the cache directory between runs, so that the runner is not built every time:

```yaml
name: E2E
on: [pull_request]
jobs:
  e2e:
    runs-on: macos-26
    env:
      PROBE_IOS_CACHE_DIR: ${{ github.workspace }}/.probe-ios
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version: stable
      - run: go install github.com/mozership/probe/cmd/probe@latest
      - uses: actions/cache@v5
        with:
          path: .probe-ios/runner
          key: probe-ios-runner-${{ runner.os }}
      - name: Boot a simulator
        run: |
          UDID=$(xcrun simctl create e2e "iPhone 17" com.apple.CoreSimulator.SimRuntime.iOS-26-5)
          xcrun simctl bootstatus "$UDID" -b
      - run: probe e2e.yml
```

The action names its cache by the Xcode and the runner it builds, so a cache that is out of date is built over, not used.
