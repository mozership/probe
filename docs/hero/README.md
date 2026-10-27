# The hero run of the home page

The workflow and the report at the top of the home page, `heroWorkflow` and `heroReport` in [`components/home-data.ts`](../components/home-data.ts), are a real run of [`signup-flow.yml`](signup-flow.yml) against the stubs here. Change one, and run it again.

| What | Where |
|------|-------|
| A sign-up service: the form, the API, SQLite, the confirmation mail, and the pages the agent visits | [`stub/main.go`](stub/main.go) |
| A SwiftUI app that signs in to it | [`app/Acme/AcmeApp.swift`](app/Acme/AcmeApp.swift), built from [`app/project.yml`](app/project.yml) |
| The mailbox | GreenMail, in Docker |

## Running it

It needs Go, Docker, Xcode with an iOS simulator runtime, xcodegen, Chrome, Probe, and an Anthropic API key for the agent action. Run it from this directory:

```sh
ANTHROPIC_API_KEY=... probe run.yml
```

[`run.yml`](run.yml) is a workflow of its own. It builds the stub and the app, starts the stub and GreenMail in the background, boots the simulator and installs the app, runs `signup-flow.yml` and writes what Probe prints to `report.txt`. Probe stops the stub once the run is over, and the last job removes the GreenMail container, however the run went. The simulator is `iPhone 17`, or the one `DEVICE` names.

## Copying it to the home page

- In the workflow, swap `web` and `api` for `https://app.example.com` and `https://api.example.com`, and the `host`, `port` and `tls` of the IMAP defaults for `host: imap.example.com`. Nothing else changes.
- In the report, leave out the lines Probe prints as a step starts and ends, and its warnings.
