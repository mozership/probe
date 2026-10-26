# Android Action

The Android action drives an app on an Android emulator or device: it launches the app, taps and types into it, presses back, waits for elements, reads their text and takes screenshots. It runs on Linux and macOS.

It is an [external action](/guide/concepts/actions#external-actions), published in [mozership/probe-android](https://github.com/mozership/probe-android). Probe downloads it the first time a workflow uses it, and runs the executable whose SHA-256 the `action.yml` at the pinned commit names.

## How It Works

The action works through adb alone, so there is nothing to build, and nothing to install on the device but the app under test. Each action on an element reads the screen with `uiautomator dump`, again until the element is on it, and acts on it with `input`: `input tap` at the middle of the element, and `input text` to type. Text that `input text` cannot type, such as Japanese, goes through [ADBKeyBoard](#typing).

| What | How |
|------|-----|
| adb | The Android SDK Platform-Tools, on PATH or in `$ANDROID_HOME/platform-tools` |
| A booted emulator or a device | `emulator -avd <name>`, or a device with USB debugging on. `adb devices` lists it as `device` |
| The app under test | Installed on it: `adb install path/to/app.apk` |

The action does not boot the emulator or install the app. Do that in a step of its own, such as a [`shell`](/reference/actions/shell) step, so that a failed boot is not taken for a failed test.

Turn the animations of the emulator off, as `adb shell settings put global animator_duration_scale 0` and its `window_` and `transition_` siblings do: uiautomator cannot read a screen that is still moving, so the action waits for it.

## Basic Syntax

A workflow pins the action by a full commit SHA. The notes of each [release](https://github.com/mozership/probe-android/releases) start with the action and its commit to copy.

```yaml
name: Settings
actions:
  android: github.com/mozership/probe-android@bd855d4c4c0a62c662f7e059e4ff5b0586c63732 # v0.2.0
jobs:
  - name: settings
    steps:
      - name: Open Network & internet
        uses: android
        with:
          app: com.android.settings
          actions:
            - name: launch
            - name: tap
              selector: text="Network & internet"
            - name: text
              id: airplane
              selector: text="Airplane mode"
        test: res.code == 0 && res.results.airplane == "Airplane mode"
```

A later step goes on from the screen the step before left, as the device keeps it.

## Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `app` | String | Yes | - | The package name of the app the actions drive |
| `actions` | List | Yes | - | The actions to take, in order. See [Actions](#actions) |
| `device` | String | No | the one device | The device, by its serial, such as `emulator-5554`, or its model. It has to be given when adb lists more than one |
| `timeout` | Duration string or number of seconds | No | `30s` | Limit for the actions, from the first to the last |

## Actions

Each entry of `actions` names its action in `name`:

| Action | Fields | What it does |
|--------|--------|--------------|
| `launch` | | Launches the app afresh, as its launcher icon does, ending it first if it runs |
| `activate` | | Brings the app to the front as it was, launching it if it is not running |
| `terminate` | | Ends the app |
| `back` | | Presses the back button |
| `tap` | `selector` | Taps the middle of the element |
| `type` | `selector`, `value` | Taps the element and types `value` into it. See [Typing](#typing) |
| `wait` | `selector` | Waits until the element is on the screen |
| `text` | `selector`, `id` | Reads the element's text, or its content description when it has no text, into `res.results` |
| `tree` | `id` | Reads the screen into `res.results`, an element a line, with what a selector can name |
| `screenshot` | `id` | Saves the screen as PNG, and puts its path in `res.filepaths` |

`id` is the key of the result; without it, the action's name is. An action on an element reads the screen until the element is on it, for what is left of `timeout`, and the step fails when it does not come. An entry with a key an action does not take, such as `args` of the iOS action, is refused before the step runs.

`tree` lists the screen with one element a line, indented by depth:

```
TextView id=android:id/title text="Network & internet" [200,540][700,600]
EditText id=com.android.settings:id/ssid text="Wi-Fi" [20,230][256,278] focused
```

### Selectors

A selector names an element by key=value pairs, separated by spaces. A value with a space goes in double quotes.

| Key | Matches |
|-----|---------|
| `id` | The resource id, whole as `com.example:id/login` or by its name as `login` |
| `text` | The text shown |
| `desc` | The content description, which a screen reader reads, as for an icon |
| `class` | The class, whole as `android.widget.Button` or by its name as `Button` |
| `index` | Which of the matching elements, from 0, in the order `tree` lists them. Without it, the first |

A selector with no `=` is a resource id, so `login` is `id=login`.

## Typing

`type` types printable ASCII with `input text`, which any device has. `input text` cannot type other text, such as Japanese, nor `%s`, which it reads as a space, so `type` types such text with [ADBKeyBoard](https://github.com/senzhk/ADBKeyBoard), an input method that types the text adb sends it.

```yaml fragment
- name: type
  selector: id=ssid
  value: 日本語
```

ADBKeyBoard has to be installed on the device, as `adb install keyboardservice-debug.apk` from its [releases](https://github.com/senzhk/ADBKeyBoard/releases) does; the action does not install it, and fails a step that needs it before the step's first action when it is not there. It needs no setting up beyond that. A step that types such text makes it the input method before its first action, enabling it when it is not, and puts the input method back as it was once the step is over, even when the step fails. The action reads the screen until the focused element has the text, and sends it again when it does not, as ADBKeyBoard drops what it is sent before it is up.

Reading the screen with uiautomator hides the soft keyboard. An element keeps the focus, and `type` types into it all the same, but an app that acts on the keyboard going cannot be driven past it: the search of Settings closes.

## Response Object

| Field | Description |
|-------|-------------|
| `res.code` | `0` when every action succeeded |
| `res.results` | What `text` and `tree` read, by key |
| `res.filepaths` | The files `screenshot` saved, by key |
| `res.device` | The serial of the device the actions ran on |

An action that cannot finish fails the step with an action error, which exits the run with status `3`, and there is no `res` to test:

```
actions[1] tap: no element matches text="No Such Row" within 5.0s
```

## CI

On GitHub Actions, an Ubuntu runner runs an emulator with KVM. [android-emulator-runner](https://github.com/ReactiveCircus/android-emulator-runner) boots one, turns its animations off, and runs a script while it is up:

```yaml
name: E2E
on: [pull_request]
jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - name: Install Probe
        uses: mozership/probe-action@v1.3.0
        with:
          install-only: true
      - name: Enable KVM
        run: |
          echo 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' | sudo tee /etc/udev/rules.d/99-kvm4all.rules
          sudo udevadm control --reload-rules
          sudo udevadm trigger --name-match=kvm
      - uses: reactivecircus/android-emulator-runner@v2
        with:
          api-level: 35
          target: google_apis
          arch: x86_64
          script: probe e2e.yml
```
