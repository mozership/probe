# SMTP Action

The `smtp` action delivers mail to an SMTP server. It is built for measuring and exercising delivery rather than for sending hand-written notifications: the message body is generated, and its size is set with `length`.

## Basic Syntax

An SMTP step names the server, the envelope addresses, and what to send.

```yaml
steps:
  - name: Send a probe mail
    uses: smtp
    with:
      addr: "localhost:2525"
      from: "sender@example.com"
      to: "recipient@example.com"
      subject: "Delivery probe"
      session: 1
      message: 1
      length: 500
    test: res.code == 0 && res.sent > 0
```

## Parameters

The fields below describe the delivery. All of them accept template expressions.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `addr` | String | Yes | - | SMTP server as `host:port` |
| `from` | String | Yes | - | Envelope sender |
| `to` | String | Yes | - | Envelope recipient. Several recipients are separated by commas, such as `a@example.com, b@example.com` |
| `subject` | String | No | `""` | Subject line |
| `myhostname` | String | No | The machine's host name, or `localhost` if it is unknown | Hostname sent in the `EHLO` / `HELO` command. Many servers refuse a client that greets with `localhost`, so set a name the server accepts |
| `session` | Integer | No | `1` | Number of SMTP sessions to open at the same time, from 1 to 1000 |
| `message` | Integer | No | `1` | Messages to send in total, divided among the sessions. At least 1 |
| `length` | Integer | No | `0` | Number of `*` characters appended to the generated body, up to 104857600 (100 MiB) |
| `starttls` | String | No | `auto` | `auto` switches to TLS with `STARTTLS` when the server offers it, `required` fails a session whose server does not, and `off` never uses it |
| `insecure_skip_tls` | Boolean | No | `false` | Accept any certificate after `STARTTLS`, such as a self-signed one |
| `timeout` | Duration | No | `1m` | Time limit for connecting, and for each wait on the server to read or write. The reply to the end of a message may take twice as long |

After `STARTTLS` the server's certificate is verified against the host in `addr`, so a self-signed or mismatched one fails the session unless `insecure_skip_tls` is set. A connection that starts in TLS, as on port 465, is not supported. There are no parameters for authentication, CC/BCC, a custom body or HTML. To include a report in the run output, use the step's `echo`.

`timeout` accepts a duration string such as `10s` or `1m30s`, or a plain number of seconds. A value that is not a duration, not a number, not above zero, or longer than `24h` is rejected. It bounds each wait rather than the whole session, so a session of many messages may take longer than it as long as the server keeps answering. The reply to the end of a message may take twice `timeout`, 2 minutes by default, as a server may check the message before it answers; a message whose reply runs out of time is counted as failed, although the server may have taken it. A session whose server does not answer in time fails, and when no message was delivered the step ends with an error, such as `mail delivery failed: tcp dial error: read tcp ...: i/o timeout (the server did not answer within 1m0s)`.

What bounds the whole session is the step's own `timeout`, 5m by default, which stops the action however the waits go and leaves no `res` to test. The default `timeout` is well below it, so that a server that stops answering fails its session, and the step still reports the counts of all sessions, before that happens. RFC 5321 (section 4.5.3.2) asks a client to wait 5 minutes for most replies and 10 for the reply to the end of a message, so for a slow server, such as one that greylists or scans each message, raise both: `with.timeout` for each wait, and the step's `timeout` above the time the whole session may take.

```yaml
- name: Deliver to a slow server
  uses: smtp
  timeout: 30m
  with:
    addr: "mx.example.com:25"
    from: "sender@example.com"
    to: "recipient@example.com"
    timeout: 5m
```

The generated message has `From`, `To`, `Date` and `Subject` headers and a body of `This is a test mail.` followed by `length` `*` characters, broken into lines of 80. Each message is sent with two more headers before these, `X-Send-Timestamp-Ns`, the time it was sent in Unix nanoseconds, and a `Message-ID` of its own, whose part before `@` is also added to the end of the subject as ` - <id>`. The message is made once when the step starts sending, so every message of a step has the same `Date`, while `X-Send-Timestamp-Ns` is the time each one was sent.

## Response Object

After the step, `res` reports how much was delivered.

| Field | Type | Description |
|-------|------|-------------|
| `res.code` | Integer | `0` when no session failed and at least one message was delivered |
| `res.sent` | Integer | Messages the server accepted, including those a session delivered before it failed |
| `res.failed` | Integer | Sessions that failed |
| `res.total` | Integer | Messages attempted, which is `message` |
| `res.error` | String | Error message, when delivery failed |
| `res.maildata` | String | The last message the server accepted, as it was sent with its `Message-ID` and send time. With several sessions, it is the last message of the session that finished last. Empty when none was accepted |
| `rt.duration` | String | Time the delivery took, such as `"3.4ms"` |
| `rt.sec` | Float | Time the delivery took in seconds |
| `status` | Integer | Same as `res.code` |

A server that refuses the mail, for example with `550` to `RCPT TO`, has answered, so the step goes on to its test with `res.code` set to `1` and the reply in `res.error`. When no session can reach the server at all, the step ends with an error.

## SMTP Examples

The examples below send generated messages and then read the counts back from the step outputs.

### Several Sessions and Messages

`message` is the total, divided among the sessions: here one session delivers 2 messages and the other 1.

```yaml
steps:
  - name: Deliver 3 messages over 2 sessions
    id: bulk
    uses: smtp
    with:
      addr: "{{vars.smtp_addr}}"
      from: "{{vars.from_addr}}"
      to: "{{vars.to_addr}}"
      subject: "Bulk delivery test"
      myhostname: probe-client.local
      session: 2
      message: 3
      length: 750
    test: res.code == 0 && res.sent == 3
    outputs:
      sent: res.sent
      elapsed: rt.duration
```

### Reporting the Result

The counts captured as outputs can be printed by a later step.

```yaml
  - name: Delivery summary
    uses: hello
    echo: |
      Sent: {{outputs.bulk.sent}}
      Took: {{outputs.bulk.elapsed}}
```
