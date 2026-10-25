# Mail Latency Action

The mail-latency action reads messages from a Maildir, computes the delivery latency of each one from its `Received` headers, and writes the result as a CSV file.

It is an [external action](/guide/concepts/actions#external-actions), published in [mozership/probe-mail-latency](https://github.com/mozership/probe-mail-latency); it was built into Probe up to v1.21.0. Probe downloads it the first time a workflow uses it, and runs the executable whose SHA-256 the `action.yml` at the pinned commit names. It needs Probe v1.21.0 or later. The notes of each [release](https://github.com/mozership/probe-mail-latency/releases) start with the `uses` line to copy.

The action connects to no host, so its `action.yml` declares `guard: [allow-host]`, and Probe runs it under `--allow-host` without `--allow-action`. It writes its CSV into `output_dir`, so it declares no `read-only`: under `--read-only`, a step that uses it is refused unless `--allow-action` names it. See [Guard](/guide/concepts/guard).

## Basic Syntax

The action reads the messages in a directory and writes the measurements as a CSV file.

```yaml
- name: Measure delivery latency
  uses: github.com/mozership/probe-mail-latency@2340503ddb7081e1a7e4cc7e4391ac6e5937574d # v0.1.0
  with:
    mail_dir: "/var/mail/probe/new"
    output_dir: "./reports"
  test: res.status == 0
```

## Parameters

Both directories are required: one to read from, one to write to.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `mail_dir` | String | Yes | Directory holding the messages to measure |
| `output_dir` | String | Yes | Directory the CSV file is written to |

`mail_dir` is read recursively, subdirectories included. A file is measured when it starts with a `Return-Path:` or `Delivered-To:` header, which is how a delivered message usually begins. Other files are left alone. Only the headers are read, up to the first empty line, and a header may be folded over several lines or not. The action fails, and writes no CSV, when a measured message has no `Date` header or no `Received` header.

Each message becomes one row of the CSV, with these columns:

- `Sent Time Offset (sec)` and `Received Time Offset (sec)`: when the message was sent and received, counted from the earliest `Date` in the directory
- `Sent Time`: the `Date` header
- `Last Received Time` and `First Received Time`: the topmost and the bottom `Received` header
- `End-to-End Latency (sec)`: from `Date` to the topmost `Received`
- `Relay Latency (sec)`: from the bottom `Received` to the topmost one
- `Return Path` and `File Path`: the envelope sender and the file name

Times are written in the local time zone of the machine running probe, as `2006-01-02 15:04:05`.

## Response Object

The result points at the file that was written.

| Field | Type | Description |
|-------|------|-------------|
| `res.output_file` | String | Path of the written CSV file, named `mail-latency.<timestamp>.csv`. A run in the same second as an earlier one writes `mail-latency.<timestamp>-2.csv` and so on instead of overwriting it |
| `res.status` | Integer | `0` on success |
| `rt.duration` | String | Time spent measuring, such as `"1.2ms"` |
| `rt.sec` | Float | Time spent measuring in seconds |
| `status` | Integer | `0` on success |

## Examples

Measuring after waiting for a message sent over SMTP to arrive:

```yaml
name: Mail latency
vars:
  smtp_addr: localhost:25
  run_id: "{{random_str(8)}}"
  maildir: /var/mail/probe
jobs:
- name: Mail delivery latency
  steps:
    - name: Send a probe mail
      uses: smtp
      with:
        addr: "{{vars.smtp_addr}}"
        from: "probe@example.com"
        to: "probe@example.com"
        subject: "probe {{vars.run_id}}"
        session: 1
        message: 1
        length: 500
      test: res.code == 0 && res.sent > 0

    - name: Measure
      id: latency
      uses: github.com/mozership/probe-mail-latency@2340503ddb7081e1a7e4cc7e4391ac6e5937574d # v0.1.0
      wait: 30s
      with:
        mail_dir: "{{vars.maildir}}/new"
        output_dir: "./reports"
      test: res.status == 0
      outputs:
        report: res.output_file

    - name: Report
      uses: hello
      echo: "CSV: {{outputs.latency.report}}"
```

## See Also

- **[SMTP](/reference/actions/smtp)** - Sending mail
- **[IMAP](/reference/actions/imap)** - Working with mailboxes
- **[YAML Configuration](/reference/yaml-configuration)** - Step properties
