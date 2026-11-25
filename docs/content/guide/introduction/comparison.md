# Compared with similar tools

Probe is a workflow runner, and testing and monitoring are two things you can do with it. Several similar tools exist with different goals, and which one fits depends on what you are trying to do.

## At a glance

The table places each tool by what it is, how it is written, what it can reach, and what it takes to run.

| | Kind | Written as | Operates on | Runs as |
|---|---|---|---|---|
| **Probe** | Workflow runner | YAML | HTTP, SMTP, IMAP, DNS, SSH, shell; DB, gRPC, a browser, WebSocket, Redis and S3 as external actions | A single Go binary |
| **k6** | Load testing | JavaScript | HTTP, gRPC, WebSocket | A single Go binary |
| **Postman / Newman** | API testing | A GUI and its collections | HTTP | A GUI app and Node.js |
| **Hurl** | HTTP testing | Its own plain-text format | HTTP | A single Rust binary |
| **Venom** | Test runner | YAML | HTTP, SMTP, IMAP, SSH, SQL, gRPC and more | A single Go binary |
| **runn** | Scenario runner | YAML runbooks | HTTP, gRPC, DB, browser, SSH, commands | A single Go binary, or called from Go tests |
| **Blackbox exporter** | Probing for monitoring | A config file | HTTP, TCP, DNS, ICMP | A long-running process |
| **GitHub Actions** | CI | YAML | Whatever runs on the runner | GitHub's runners |
| **CWL / WDL** | Describing computational pipelines | YAML or JSON (CWL), its own DSL (WDL) | Commands in containers, and files | Engines such as cwltool, Cromwell, miniwdl |

## Where each one differs

The table gives the shape of each tool. This section takes them one at a time and says where Probe overlaps, and where another tool is the better answer.

### k6

[k6](https://k6.io/) puts load on a system and measure what happens: concurrency, the distribution of response times. Probe has no way to generate load; it checks that one pass through a flow comes out right. Measure performance with k6.

### Postman / Newman

[Postman](https://www.postman.com/) and its CLI, [Newman](https://github.com/postmanlabs/newman), are HTTP-centred, and running them needs Node.js. Collections are a format meant to be edited in the GUI, which makes them awkward to read as a diff. Probe mixes other protocols into the same file, and the file is YAML you can review.

### Hurl

[Hurl](https://hurl.dev/) is narrowed to HTTP, and within that range nothing is shorter to write. Probe covers more than HTTP, and pays for it in the job and step structure you have to spell out. For HTTP alone, Hurl is the lighter tool.

### Venom

[Venom](https://github.com/ovh/venom) is close too: YAML, several protocols, a single binary.

Two things differ. Probe declares dependencies between jobs with `needs` and can print that graph with `probe dag`. And `repeat` runs the same file on an interval and reports a success rate, which is what lets a file written as a test serve as a monitor.

### runn

[runn](https://github.com/k1LoW/runn) is another of the closest. Scenarios in YAML, with HTTP, gRPC, databases, a browser, SSH and commands available in the same file. It evaluates expressions with expr, as Probe does, so what you can write in `test` reads much the same.

Three things differ. runn walks a runbook from top to bottom, where Probe schedules whole jobs in parallel and declares the order with `needs`. runn can be called from Go test code as a test helper, where Probe is a CLI. And Probe's actions are separate processes behind go-plugin, so one can be added without touching the binary.

### Blackbox exporter

[Blackbox exporter](https://github.com/prometheus/blackbox_exporter) repeats single probes for monitoring, built to be scraped by Prometheus. It does not carry a multi-step flow — signing in, taking a token, passing it to the next request. Probe writes that flow, but has nothing that collects and stores metrics.

### GitHub Actions

The notation of [GitHub Actions](https://docs.github.com/actions) is close: `jobs`, `steps`, `needs`, `uses` and `with` line up with Probe almost one for one. But it runs on GitHub's runners, so you cannot run it from your own terminal or see the dependency graph. Probe behaves the same way in CI and on your machine.


### CWL / WDL

The same word, a different subject. The [Common Workflow Language](https://www.commonwl.org/) and the [Workflow Description Language](https://openwdl.org/) are specifications for describing computational pipelines, used in bioinformatics and other research fields.

A step runs a command inside a container and takes files in and out. The order is not declared the way `needs` declares it: it falls out of connecting one step's output to another's input. What runs them is not the specification but an engine — cwltool, Cromwell, miniwdl — usually against an HPC scheduler or a cloud batch service.

Probe touches a running system and checks what it answers; it is not a pipeline for transforming data. Feeding a lot of files through a series of stages is not what it is for.

## A caveat

This comparison reflects how these tools were built as of September 2026. They all keep moving, so check each project's current documentation before deciding.

## See Also

- **[What is Probe?](/guide/introduction/what-is-probe)** - What it does and what stands out
- **[Understanding Probe](/guide/introduction/understanding-probe)** - The execution model and the design
