# IMAP Action

The `imap` action connects to IMAP servers to perform email operations such as reading messages, searching, and mailbox management.

## Basic Syntax

An IMAP step opens a connection and then runs the commands listed in `commands`.

```yaml
vars:
  imap_username: "{{IMAP_USERNAME}}"
  imap_password: "{{IMAP_PASSWORD}}"

steps:
  - name: "Check Email"
    uses: imap
    with:
      host: "imap.example.com"
      port: 993
      username: "{{vars.imap_username}}"
      password: "{{vars.imap_password}}"
      tls: true
      commands:
      - name: "select"
        mailbox: "INBOX"
      - name: "search"
        criteria:
          not_flags: ["seen"]
    test: res.code == 0
```

## Parameters

The parameters describe the server to reach, the credentials to present, how the connection is secured, and the list of IMAP commands to run once it is open.

### `host` (required)

**Type:** String  
**Description:** IMAP server hostname or IP address  
**Supports:** Template expressions

```yaml
with:
  host: "imap.gmail.com"
  host: "imap.example.com" 
  host: "{{vars.imap_server}}"
```

### `port` (optional)

**Type:** Integer  
**Default:** `993`  
**Description:** IMAP server port

```yaml
with:
  port: 993   # IMAPS (SSL/TLS)
  port: 143   # IMAP without TLS (tls: false)
```

### `username` (required)

**Type:** String  
**Description:** IMAP authentication username  
**Supports:** Template expressions

```yaml
vars:
  email_user: "{{EMAIL_USER}}"

with:
  username: "{{vars.email_user}}"
  username: "user@example.com"
```

### `password` (required)

**Type:** String  
**Description:** IMAP authentication password  
**Supports:** Template expressions

```yaml
vars:
  email_password: "{{EMAIL_PASSWORD}}"
  app_password: "{{EMAIL_APP_PASSWORD}}"

with:
  password: "{{vars.email_password}}"
  password: "{{vars.app_password}}"
```

### `tls` (optional)

**Type:** Boolean  
**Default:** `true`  
**Description:** Whether to use TLS/SSL encryption

```yaml
with:
  host: "imap.example.com"
  port: 993
  tls: true     # Use TLS (recommended)

with:
  host: "imap.example.com" 
  port: 143
  tls: false    # Plain connection (not recommended)
```

STARTTLS is not supported: with `tls: false` the whole session, the password included, is sent unencrypted.

### `insecure_skip_tls` (optional)

**Type:** Boolean  
**Default:** `false`  
**Description:** Accept the server's certificate without verifying it

With `tls: true` the certificate is verified against the system's trusted authorities, so a server with a self-signed certificate, such as a local or staging mail server, fails with `x509: certificate signed by unknown authority`. `insecure_skip_tls: true` connects anyway. The connection is still encrypted, but it is no longer protected against an impostor, so use it only for servers you control.

```yaml
with:
  host: "mail.staging.internal"
  port: 993
  tls: true
  insecure_skip_tls: true
```

### `timeout` (optional)

**Type:** Duration string or number of seconds  
**Default:** `30s`  
**Description:** Limit for the whole session: connecting, the TLS handshake, logging in, every command and logging out

```yaml
with:
  timeout: "60s"   # or 60
```

A server that does not answer in time is cut off, and the step fails with an action error, which exits the run with status `3`, such as `IMAP session timed out after 30s while running commands`. That is unlike a command the server refuses, which sets `res.code` to `1`. A value that is not a duration, not a number, or not above zero is rejected.

### `commands` (optional)

**Type:** Array of command objects  
**Description:** IMAP commands to execute sequentially. Without them, the step connects, logs in and logs out, which checks the login alone

The commands run in order in one session. The first command that fails stops the rest: `res.code` becomes `1`, `res.error` names the command, and `res.data` keeps what the commands before it returned.

Each command object takes these fields; which of them a command reads is described with the command below.

| Field | Used by |
|-------|---------|
| `name` | Every command (required): the command to run, case-insensitive |
| `mailbox` | `select`, `examine`, `copy`, `uid copy`, `create`, `delete`, `subscribe`, `unsubscribe` |
| `oldmailbox`, `newmailbox` | `rename` |
| `reference`, `pattern` | `list` |
| `criteria` | `search`, `uid search` |
| `sequence` | `fetch`, `uid fetch`, `store`, `uid store`, `copy`, `uid copy` |
| `dataitem` | `fetch`, `uid fetch`, `store`, `uid store` |
| `value` | `store`, `uid store` |

```yaml
with:
  commands:
  - name: "select"
    mailbox: "INBOX"
  - name: "search"
    criteria:
      since: "today"
  - name: "fetch"
    sequence: "1:5"
    dataitem: "ALL"
```

## IMAP Commands

Each entry in `commands` names one IMAP operation and carries the fields that operation needs. The commands below are the ones the action understands.

### `select` - Select Mailbox

Select a mailbox for read-write operations.

```yaml
- name: "select"
  mailbox: "INBOX"      # Required: mailbox name
- name: "select" 
  mailbox: "Sent"
- name: "select"
  mailbox: "INBOX/Work"
```

### `examine` - Read-only Mailbox Access

Select a mailbox for read-only operations.

```yaml
- name: "examine"
  mailbox: "INBOX"      # Required: mailbox name
```

### `search` and `uid search` - Search Messages

Search the selected mailbox. `search` returns sequence numbers and `uid search` returns UIDs; both put the result in `res.data.search`, so a later search replaces an earlier one there. The result is also what a following `fetch`, `store` or `copy` without `sequence` acts on, as described under each of them.

```yaml
- name: "search"
  criteria:
    since: "today"           # Arrived today or later
    not_flags: ["seen"]      # Not read yet
    headers:                 # Header fields containing the text
      from: "sender@example.com"
      subject: "urgent"
    bodies: ["important"]    # Body containing the text
    texts: ["meeting"]       # Headers or body containing the text
```

Every criterion given has to match. The criteria are:

| Criterion | Type | Matches messages |
|-----------|------|------------------|
| `seq_nums` | Array of strings | With these sequence numbers, such as `"2:3"`, `"5"` or `"10:*"` |
| `uids` | Array of strings | With these UIDs, in the same form |
| `since` | Date | That arrived on or after the date |
| `before` | Date | That arrived before the date |
| `sent_since` | Date | Whose `Date` header is on or after the date |
| `sent_before` | Date | Whose `Date` header is before the date |
| `headers` | Object | Whose header field, the key, contains the value |
| `bodies` | Array of strings | Whose body contains every string |
| `texts` | Array of strings | Whose headers or body contain every string |
| `flags` | Array of strings | That have every flag |
| `not_flags` | Array of strings | That have none of the flags |

A flag is given without its backslash, as in `seen`, `answered`, `flagged`, `deleted`, `draft` or `recent`, or with it, as in `'\Seen'`. Any other name is a keyword, such as `$Important`, and is passed as it is. There is no `unseen` flag: unread messages are `not_flags: ["seen"]`.

A date is one of:

- `today` or `yesterday`, from midnight in the local time zone of the machine running probe
- `N hours ago` or `N minutes ago`, such as `2 hours ago`
- `2006-01-02`, `2006/01/02`, `02/01/2006` (day first) or `02-Jan-2006`
- RFC 3339, such as `2006-01-02T15:04:05+09:00`, or RFC 822, such as `02 Jan 06 15:04 JST`

IMAP compares dates without the time, so only the day of a date counts: `2 hours ago` matches everything from the start of that day.

With no message matching, `count` is `0` and `all` is empty.

### `list` - List Mailboxes

List available mailboxes.

```yaml
- name: "list"
  reference: ""         # Optional: reference name
  pattern: "*"          # Optional: mailbox pattern (default: "*")
- name: "list"
  reference: "INBOX"
  pattern: "INBOX/*"
```

### `fetch` and `uid fetch` - Fetch Message Data

Retrieve messages by sequence number, or by UID with `uid fetch`. Without `sequence`, they fetch the messages the latest search found, provided it was the matching kind: `search` for `fetch`, `uid search` for `uid fetch`. `fetch` needs `dataitem`; `uid fetch` fetches `ALL` without it. Both put the messages in `res.data.fetch`.

```yaml
- name: "fetch"
  sequence: "1:5"       # Sequence range; the last search result if omitted
  dataitem: "ALL"       # Data items to fetch
- name: "fetch"
  sequence: "*"         # Latest message
  dataitem: "ENVELOPE FLAGS"
- name: "uid search"
  criteria:
    not_flags: ["seen"]
- name: "uid fetch"     # The UIDs just found, with ALL
```

`dataitem` is one of the macros `ALL`, `FAST` and `FULL` alone, or a space-separated list of `ENVELOPE`, `FLAGS`, `INTERNALDATE`, `RFC822`, `RFC822.HEADER`, `RFC822.SIZE`, `RFC822.TEXT`, `UID`, `BODYSTRUCTURE` and `BODY[...]` or `BODY.PEEK[...]` sections. Case does not matter, and the list may be in parentheses, as in `(FLAGS UID)`. A section may end with a partial range, as in `BODY.PEEK[TEXT]<0.512>` for the first 512 bytes. An item not in this list fails the command. `ENVELOPE` fills `from`, `to` and `subject`; `RFC822.SIZE` fills `size`. `RFC822` is `BODY[]`, `RFC822.HEADER` is `BODY.PEEK[HEADER]` and `RFC822.TEXT` is `BODY[TEXT]`, as in IMAP. A section fills these fields of the message:

| Section | Fills |
|---------|-------|
| `BODY[]` | `body` with the whole message, headers included |
| `BODY[TEXT]` | `body` with the body only |
| `BODY[HEADER]` | `headers` with every header field |
| `BODY[HEADER.FIELDS (SUBJECT FROM)]` | `headers` with the listed fields |

`BODY[...]` marks the messages read; `BODY.PEEK[...]` leaves their flags alone. Header names in `headers` are lower case, as in `headers.subject`. A body containing `<html` is also put in `html_body`.

### `create`, `delete`, `rename`, `subscribe` and `unsubscribe` - Manage Mailboxes

Create a mailbox, delete one, rename one, or add one to or remove it from the subscribed mailboxes. `rename` takes the current name in `oldmailbox` and the new one in `newmailbox`; the others take `mailbox`. A missing name fails the command.

```yaml
- name: "create"
  mailbox: "Work"
- name: "rename"
  oldmailbox: "Work"
  newmailbox: "Projects"
- name: "subscribe"
  mailbox: "Projects"
- name: "unsubscribe"
  mailbox: "Projects"
- name: "delete"
  mailbox: "Projects"
```

### `noop` - Do Nothing

Ask the server for nothing. It checks that the session is still alive, and lets the server report changes to the selected mailbox.

```yaml
- name: "noop"
```

### `store` and `uid store` - Change Flags

Set, add or remove flags on messages. `dataitem` is `FLAGS` to replace the flags, `+FLAGS` to add, or `-FLAGS` to remove, optionally followed by `.SILENT` so the server does not send the new flags back. `value` lists the flags, with or without parentheses; an empty value or `()` with `FLAGS` clears every flag, and a lone parenthesis is refused. `uid store` takes UIDs in `sequence`. Without `sequence`, both act on the messages the latest search found, as `fetch` does, provided it was the matching kind: `search` for `store`, `uid search` for `uid store`. Each search replaces the previous one of either kind, so an older search is never used.

In `sequence`, a lone `*` is the last message, and every number has to be from `1` to `4294967295`; `0` or a larger value is refused rather than read as `*`. A `select` or `examine` forgets the last search result, since numbers belong to one mailbox, so a command without `sequence` after switching mailboxes fails instead of acting on the wrong messages.

```yaml
- name: "select"
  mailbox: "INBOX"
- name: "store"
  sequence: "1:3"
  dataitem: "+FLAGS"
  value: '\Seen \Flagged'
```

`res.data.store.count` is the number of messages the server reported with their new flags, which is `0` with `.SILENT`. Only flags can be stored; another data item fails the command.

### `copy` and `uid copy` - Copy Messages

Copy messages into another mailbox, which has to exist. `uid copy` takes UIDs in `sequence`, and without `sequence` both act on the last search result.

```yaml
- name: "select"
  mailbox: "INBOX"
- name: "copy"
  sequence: "1:2"
  mailbox: "Archive"
```

`res.data.copy.count` is the number of messages copied when the server reports it, which it does when it supports UIDPLUS; otherwise it is `0`.

A command that fails, such as a `copy` into a mailbox that does not exist, sets `res.code` to `1` and puts the reason in `res.error`, as every IMAP command does.

## Response Object

The IMAP action provides a `res` object with the following structure:

| Property | Type | Description |
|----------|------|-------------|
| `code` | Integer | `0` when every command succeeded, `1` when a command failed, `2` when every command succeeded but logging out failed |
| `data` | Object | Command results organized by command type |
| `error` | String | Error message if operation failed |

The top-level `status` is the same as `res.code`, and `rt.duration` (such as `"12ms"`) and `rt.sec` give how long the whole session took, from connecting to logging out. A connection, login or timeout failure is not a `res.code`: it fails the step as an action error.

`res.data` has one entry per command, named after it. A command that did not run leaves its entry at zero values, so `success` is `false` only then: a command that fails stops the run with `res.code` `1` instead. `uid search`, `uid fetch`, `uid store` and `uid copy` share the entry of the command without `uid`.

| Entry | Fields |
|-------|--------|
| `select`, `examine` | `exists` (messages in the mailbox), `recent`, `first_unseen` (sequence number of the first unread message, `0` if the server does not say), `uid_next`, `flags`, `permanent_flags` |
| `search` | `all` (the matching numbers as a set, such as `2:3`), `min`, `max`, `count` |
| `list` | `mailboxes` (each with `name`, `attributes` and `delimiter`), `count` |
| `fetch` | `messages`, `count` |
| `store`, `copy` | `success`, `count` |
| `create`, `delete`, `subscribe`, `unsubscribe` | `success`, `mailbox` |
| `rename` | `success`, `old_mailbox`, `new_mailbox` |
| `noop` | `success` |

Each entry of `res.data.fetch.messages` has `uid`, `flags`, `date`, `from`, `to`, `subject`, `size`, `body`, `html_body` and `headers`, filled as the data items asked for. `date` is the envelope's `Date` in RFC 3339, such as `2025-10-08T07:11:55Z`, and is empty when the message has none. `from` and `to` are the first address only. Index the list to reach a message, as in `res.data.fetch.messages[0].from`.

## IMAP Examples

Providers differ in port, TLS and credentials. The example below is a working configuration for Gmail.

### Gmail Configuration

Gmail requires an app password rather than the account password, and TLS on port 993.

```yaml
vars:
  gmail_username: "{{GMAIL_USERNAME}}"
  gmail_app_password: "{{GMAIL_APP_PASSWORD}}"

steps:
  - name: "Check Gmail Inbox"
    uses: imap
    with:
      host: "imap.gmail.com"
      port: 993
      username: "{{vars.gmail_username}}"
      password: "{{vars.gmail_app_password}}"  # Use App Password
      tls: true
      commands:
      - name: "select"
        mailbox: "INBOX"
      - name: "search"
        criteria:
          not_flags: ["seen"]
          since: "today"
      - name: "fetch"
        sequence: "*"
        dataitem: "ENVELOPE FLAGS"
    test: res.code == 0
    outputs:
      unread_count: res.data.search.count
      latest_sender: res.data.fetch.messages[0].from
```
