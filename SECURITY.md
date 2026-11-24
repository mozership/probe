# Security Policy

## Supported Versions

Only the latest release of Probe gets security fixes. A fix ships in a new
release; earlier releases are not patched.

The same applies to the external actions under
[mozership](https://github.com/mozership) (`probe-agent`, `probe-slack` and
the others): only the latest release of each is fixed.

## Reporting a Vulnerability

Please do not open a public issue for a vulnerability. Report it privately
through GitHub's
[private vulnerability reporting](https://github.com/mozership/probe/security/advisories/new),
on the repository the vulnerability is in.

Include what you can of:

- the version of Probe (`probe --version`) and of the action, if one is
  involved
- a workflow file, or the smallest steps, that shows the problem
- what an attacker gains, and what they need to control to gain it

You should hear back within a week. Once a fix is released, the advisory is
published with credit to you, unless you ask otherwise.

## Scope

A workflow file is code: it runs commands with the `shell` and `ssh`
actions, starts external actions, and reaches whatever hosts it names, with
the rights of the user running Probe. Running a workflow you do not trust is
like running a script you do not trust, and what such a workflow can do is
not a vulnerability.

These are in scope, for example:

- a value Probe should hide, such as one listed under `secrets` or the
  password of a URL, appearing in its output, a report or the notes of a run,
  beyond the limits the
  [reference](https://probe.linyo.ws/reference/yaml-configuration#secrets)
  tells
- an external action at a `uses` reference other than the one named, such
  as a different commit or repository, being downloaded or run
- Probe reading or writing a file, or reaching a host, that the workflow did
  not ask for
- a crash or hang that input from a host under test, rather than the
  workflow itself, can cause
