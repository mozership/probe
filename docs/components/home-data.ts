/**
 * Every terminal block below is the literal output of the command shown with
 * it, captured from a real `probe` run of the workflow in `heroWorkflow`:
 * a stub service that serves the sign-up form, answers the API, keeps its
 * users in SQLite and sends the confirmation mail to a GreenMail server, a
 * SwiftUI app that signs in to it on an iOS simulator, and the agent action
 * with Claude playing Ada. Only the connection details in the workflow were
 * swapped for presentable ones — the web and API hosts, which were one local
 * stub, and the IMAP host, port and TLS — and the passwords are env vars here
 * as they would be anywhere. The lines Probe prints as a step starts and
 * ends, and its warnings, were left out. Keep it that way: the stubs, and
 * the workflow that runs this one against them, are in hero/.
 */

export type ReportTone = 'title' | 'job' | 'pass' | 'idle' | 'echo' | 'total' | 'error' | 'warning'

/**
 * A line of a terminal block. tail is the end of the line that Probe prints
 * in a color of its own, as the green of "✓ All jobs succeeded" after the
 * total time.
 */
export type ReportLine = {
  text: string
  tone?: ReportTone
  tail?: { text: string; tone: ReportTone }
}

export const heroWorkflow = `name: Sign-up Flow

actions:
  browser: github.com/mozership/probe-browser@634baa8dff2b273ed326996fec4d91c2dba9d38b # v0.2.0
  ios: github.com/mozership/probe-ios@aed5436143e9536754e2123b953831f60cd635eb # v0.2.0
  agent: github.com/mozership/probe-agent@4d0d6ccee6165624283b22fa0541b02d17e50ac5 # v0.2.0

secrets:
- ANTHROPIC_API_KEY

vars:
  web: https://app.example.com
  api: https://api.example.com
  mail_user: ada@example.com
  mail_pass: "{{MAIL_PASS}}"
  password: "{{PASSWORD}}"

jobs:
- name: Sign up
  id: signup
  steps:
  - name: Fill in the form
    uses: browser
    with:
      timeout: 15s
      actions:
      - name: navigate
        url: "{{vars.web}}/signup"
      - name: type
        selector: "#email"
        value: "{{vars.mail_user}}"
      - name: type
        selector: "#password"
        value: "{{vars.password}}"
      - name: click
        selector: button[type=submit]
      - name: wait_text
        id: notice
        selector: "#notice"
    test: res.code == 0 && res.results.notice startsWith "Check your inbox"

- name: API
  id: api
  needs: [signup]
  defaults:
    http:
      url: "{{vars.api}}"
      headers:
        content-type: application/json
  steps:
  - name: Sign-in is refused
    uses: http
    with:
      post: /login
      body:
        email: "{{vars.mail_user}}"
        password: "{{vars.password}}"
    test: res.code == 403 && res.body.error == "confirm your address first"

- name: Database
  id: db
  needs: [signup]
  defaults:
    db:
      dsn: file:./app.db
  steps:
  - name: Row is pending
    uses: db
    with:
      query: SELECT status FROM users WHERE email = ?
      params: ["{{vars.mail_user}}"]
    test: res.code == 0 && res.rows[0].status == "pending"

- name: Mailbox
  id: mailbox
  needs: [signup]
  defaults:
    imap:
      host: imap.example.com
      username: "{{vars.mail_user}}"
      password: "{{vars.mail_pass}}"
  steps:
  - name: Confirmation arrived
    id: mail
    uses: imap
    with:
      commands:
      - name: select
        mailbox: INBOX
      - name: fetch
        sequence: "1"
        dataitem: ENVELOPE BODY[TEXT]
    test: |
      res.code == 0 &&
      res.data.fetch.messages[0].subject == "Confirm your address" &&
      res.data.fetch.messages[0].to == vars.mail_user
    outputs:
      link: last(split(trim(res.data.fetch.messages[0].body), "\\r\\n"))

- name: Activate
  id: activate
  needs: [api, db, mailbox]
  steps:
  - name: Follow the link
    uses: browser
    with:
      timeout: 15s
      actions:
      - name: navigate
        url: "{{outputs.mail.link}}"
      - name: text
        id: heading
        selector: h1
    test: res.code == 0 && res.results.heading == "Your account is active"

- name: iOS app
  id: ios
  needs: [activate]
  steps:
  - name: Sign in on iPhone
    uses: ios
    timeout: 10m
    with:
      app: com.example.acme
      actions:
      - name: launch
        args: ["-api", "{{vars.api}}"]
      - name: type
        selector: email
        value: "{{vars.mail_user}}"
      - name: type
        selector: type=securetextfield identifier=password
        value: "{{vars.password}}"
      - name: tap
        selector: signin
      - name: text
        id: greeting
        selector: greeting
    test: res.code == 0 && res.results.greeting == "Welcome, ada@example.com"

- name: First visit
  id: visit
  needs: [activate]
  steps:
  - name: Ada names herself
    uses: agent
    with:
      url: "{{vars.web}}/signin"
      persona: |
        Ada, who has just confirmed her address and has
        never used the app. She signs in with {secret:EMAIL}
        and the password {secret:PASSWORD}.
      goal: |
        Sign in, and set the display name of the account
        to "Ada Lovelace".
      secrets:
        EMAIL: "{{vars.mail_user}}"
        PASSWORD: "{{vars.password}}"
      evidence_dir: out
    test: res.verdict == "pass"
`

export const heroReport: ReportLine[] = [
  { text: 'Sign-up Flow', tone: 'title' },
  { text: '' },
  { text: '⏺ Sign up (Completed in 3.25s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Fill in the form', tone: 'pass' },
  { text: '' },
  { text: '⏺ API (Completed in 0.03s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Sign-in is refused', tone: 'pass' },
  { text: '' },
  { text: '⏺ Database (Completed in 0.04s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Row is pending', tone: 'pass' },
  { text: '' },
  { text: '⏺ Mailbox (Completed in 0.05s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Confirmation arrived', tone: 'pass' },
  { text: '' },
  { text: '⏺ Activate (Completed in 1.02s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Follow the link', tone: 'pass' },
  { text: '' },
  { text: '⏺ iOS app (Completed in 53.90s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Sign in on iPhone', tone: 'pass' },
  { text: '' },
  { text: '⏺ First visit (Completed in 29.86s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Ada names herself', tone: 'pass' },
  { text: '         ✅ pass: Signing in led to /home, so the persona got into the app. On the Account page she typed "Ada Lovelace" and saved, and the page showed "Saved." at /account?saved=1. The tester then opened /account again as a new page load. The page\'s form data shows the Display name field holding value="Ada Lovelace", and the screenshot shows the same name, so the change was kept. No server errors or slow requests turned up. The only failed request was the site icon (favicon.ico, 404), which the persona would not notice.' },
  { text: '           evidence: "Home · Account Your projects You have no projects yet."' },
  { text: '           evidence: "Saved."' },
  { text: '           evidence: "textbox \\"Display name\\" value=\\"Ada Lovelace\\" [ref_7]"' },
  { text: '           0 findings' },
  { text: '           21 steps, stopped: done, 28s' },
  { text: '           report: out/probe-agent-1662083251/report.md' },
  { text: '' },
  { text: 'Total workflow time: 58.23s ', tone: 'total', tail: { text: '✓ All jobs succeeded', tone: 'pass' } },
]

/*
 * The pair below was run the same way: `probe orders.yml` against a stub that
 * answers POST /login with a token when the credentials match and refuses
 * GET /orders without one. Only the API host was swapped for a presentable
 * one, and the password is an env var here as it would be anywhere.
 */

export const sharedWorkflow = `name: Orders

vars:
  password: "{{PASSWORD}}"

jobs:
- name: Order history
  defaults:
    http:
      url: https://api.example.com

  steps:
  - name: Log in
    id: auth
    uses: embedded
    with:
      path: ./login-job.yml
      vars:
        username: ada
        password: "{{vars.password}}"
    test: res.code == 0
    outputs:
      token: res.outputs.token

  - name: List orders
    uses: http
    with:
      get: /orders
      headers:
        authorization: "Bearer {{outputs.auth.token}}"
    test: res.code == 200`

/**
 * The lines of sharedWorkflow that run the job file and hand data across,
 * written with their indent, as the workflow's own vars are not one of them.
 */
export const sharedMarks = [
  '    uses: embedded',
  '      path: ./login-job.yml',
  '      vars:',
  '      token: res.outputs.token',
]

export const sharedJob = `name: Log in

defaults:
  http:
    url: https://api.example.com
    headers:
      content-type: application/json

steps:
- name: Post credentials
  id: login
  uses: http
  with:
    post: /login
    body:
      username: "{{vars.username}}"
      password: "{{vars.password}}"
  test: res.code == 200
  outputs:
    token: res.body.access_token`

/*
 * The pair below is the literal output of `probe check agent-workflow.yml`
 * on the workflow beside it, with the mistakes a coding agent tends to make
 * left in on purpose. Keep them in step: change one and run check again.
 */

export const agentWorkflow = `name: Profile

jobs:
- name: Profile
  defaults:
    http:
      url: https://api.example.com
  steps:
  - name: Log in
    id: login
    uses: http
    with:
      post: /login
      bdoy:
        username: ada
    test: res.code == 200
    outputs:
      token: res.body.token

  - name: Read profile
    uses: http
    with:
      get: /me
      headers:
        authorization: "Bearer {{outputs.login.tokn}}"
    tset: res.code == 200`

export const agentCheck: ReportLine[] = [
  { text: 'agent-workflow.yml:14: error: job 0 "Profile", step 0 "Log in": with: unknown key "bdoy" for the http action; did you mean "body"?', tone: 'error' },
  { text: 'agent-workflow.yml:20: warning: job 0 "Profile", step 1 "Read profile": nothing checks this step: it has no test', tone: 'warning' },
  { text: 'agent-workflow.yml:25: error: job 0 "Profile", step 1 "Read profile": with: outputs.login.tokn is not published: step "login" publishes token', tone: 'error' },
  { text: 'agent-workflow.yml:26: error: job 0 "Profile", step 1 "Read profile": unknown key "tset"; did you mean "test"?', tone: 'error' },
  { text: '' },
  { text: '3 errors, 1 warning', tone: 'total' },
]

/*
 * The pair below is the literal output of `probe first-note.yml` against the
 * notes application of probe-agent's e2e server, whose new-note page fails,
 * with ANTHROPIC_API_KEY and PASSWORD set. Only the url was swapped for a
 * presentable one, and the lines Probe prints as a step starts and ends were
 * left out, as in heroReport.
 */

export const personaWorkflow = `name: First note

actions:
  agent: github.com/mozership/probe-agent@4d0d6ccee6165624283b22fa0541b02d17e50ac5 # v0.2.0

vars:
  password: "{{PASSWORD}}"

jobs:
- name: A first-time visitor
  steps:
  - name: Writes a first note
    uses: agent
    with:
      url: https://staging.example.com/
      persona: |
        Carol, who has never used a notes app and is
        not technical. She signs up with {secret:EMAIL}
        and the password {secret:PASSWORD}.
      goal: |
        Create an account, and write a first note
        that says "Buy milk".
      secrets:
        EMAIL: "carol+{{random_str(8)}}@example.com"
        PASSWORD: "{{vars.password}}"
      evidence_dir: out
    test: res.verdict != "blocked"`

export const personaReport: ReportLine[] = [
  { text: 'First note', tone: 'title' },
  { text: '' },
  { text: '⏺ A first-time visitor (Completed in 48.49s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Writes a first note', tone: 'pass' },
  { text: '         ❌ fail: The account was created: the welcome page greeted Carol and said the account was ready. No note was ever written, though. The only way to write one is the "Write your first note" link to /notes/new, and that page returned a 500 "Internal Server Error" all three times it was loaded. Nothing in the record shows a "Buy milk" note being saved.' },
  { text: '           evidence: "Welcome to Acme Notes, Carol!"' },
  { text: '           evidence: "Your account {secret:EMAIL} is ready."' },
  { text: '           evidence: "link \\"Write your first note\\" href=\\"/notes/new\\""' },
  { text: '           evidence: "Internal Server Error"' },
  { text: '           2 findings' },
  { text: '           🔴 high: The new-note page (/notes/new) fails with a server error (500)', tone: 'error' },
  { text: '           🟠 medium: After sign-up, nothing shows Carol is signed in and there is no other way to reach notes', tone: 'warning' },
  { text: '           24 steps, stopped: done, 47s' },
  { text: '           report: out/probe-agent-3220800333/report.md' },
  { text: '' },
  { text: 'Total workflow time: 48.49s ', tone: 'total', tail: { text: '✓ All jobs succeeded', tone: 'pass' } },
]

export const installCommand = 'brew install mozership/tap/probe'

export const sourceCommands = 'go install github.com/mozership/probe/cmd/probe@latest'
