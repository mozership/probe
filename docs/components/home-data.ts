/**
 * Every terminal block below is the literal output of the command shown with
 * it, captured from a real `probe` run of the workflow in `heroWorkflow`:
 * a stub API that sends the confirmation mail, SQLite, and a GreenMail IMAP
 * server. Only the connection details in the workflow were swapped for
 * presentable ones — the API host, the database path, the IMAP host and the
 * password, which is an env var here as it would be anywhere. Keep it that way.
 */

export type ReportLine = {
  text: string
  tone?: 'title' | 'job' | 'pass' | 'idle' | 'echo' | 'total' | 'error' | 'warning'
}

export const heroWorkflow = `name: Sign-up Flow

vars:
  api: https://api.example.com
  mail_user: ada@example.com
  mail_pass: "{{MAIL_PASS}}"

jobs:
- name: Create account
  id: signup
  defaults:
    http:
      url: "{{vars.api}}"
  steps:
  - name: Post the form
    id: create
    uses: http
    with:
      post: /signup
    test: res.code == 201 && res.body.status == "pending"
    outputs:
      user_id: res.body.id

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
      query: SELECT status FROM users WHERE id = ?
      params: ["{{outputs.create.user_id}}"]
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
    uses: imap
    with:
      commands:
      - name: select
        mailbox: INBOX
      - name: fetch
        sequence: "1"
        dataitem: ALL
    test: |
      res.code == 0 &&
      res.data.fetch.messages[0].subject == "Confirm your address" &&
      res.data.fetch.messages[0].to == vars.mail_user

- name: Activate
  needs: [db, mailbox]
  defaults:
    http:
      url: "{{vars.api}}"
  steps:
  - name: Follow the link
    uses: http
    with:
      get: "/users/{{outputs.create.user_id}}"
    test: res.code == 200 && res.body.status == "active"
`

export const heroReport: ReportLine[] = [
  { text: 'Sign-up Flow', tone: 'title' },
  { text: '' },
  { text: '⏺ Create account (Completed in 0.11s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Post the form', tone: 'pass' },
  { text: '' },
  { text: '⏺ Database (Completed in 0.02s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Row is pending', tone: 'pass' },
  { text: '' },
  { text: '⏺ Mailbox (Completed in 0.03s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Confirmation arrived', tone: 'pass' },
  { text: '' },
  { text: '⏺ Activate (Completed in 0.01s)', tone: 'job' },
  { text: '  ⎿ 0. ✓  Follow the link', tone: 'pass' },
  { text: '' },
  { text: 'Total workflow time: 0.15s ✓ All jobs succeeded', tone: 'total' },
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
  { text: 'Total workflow time: 48.49s ✓ All jobs succeeded', tone: 'total' },
]

export const installCommand = 'brew install mozership/tap/probe'

export const sourceCommands = 'go install github.com/mozership/probe/cmd/probe@latest'
