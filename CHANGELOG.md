# Changelog

Release notes for each version are generated from the commits by GoReleaser.
This file records what those notes cannot carry well: changes that break code
importing probe as a library.

## Unreleased

### Breaking Changes

This release breaks the Go API in a minor version, for code that imports
probe as a library: the packages only probe itself imported, the root
package, `actionrpc` on the side that runs an action, and the packages of
the built-in actions. Workflow files are not affected, except as told under
Behaviour Changes below and where `Load` now refuses a workflow before any
job runs, which the root package's part tells. An external action served with `actionrpc.Serve`,
which is how one is written, is not affected unless it imports `mapping`.

Much of what goes was used only by Probe's own tests, or by nothing; it is
listed so that code which did use it knows what to use instead.

#### Packages moved under `internal/`

These packages were imported only by probe itself, and are now under
`internal/`, so code outside probe can no longer import them. No alias is
left behind, and what they gained or lost on the way is not listed here.

| Before | After |
|---|---|
| `github.com/mozership/probe/expr` | not for use outside probe |
| `github.com/mozership/probe/jsonutil` | not for use outside probe; the expression functions `match_json` and `diff_json` |
| `github.com/mozership/probe/mapping` | `actionrpc`, as the table below shows |
| `github.com/mozership/probe/binary` | not for use outside probe |
| `github.com/mozership/probe/envfile` | not for use outside probe; `--env-file` of the `probe` command |
| `github.com/mozership/probe/oas` | not for use outside probe |
| `github.com/mozership/probe/procgroup` | not for use outside probe |
| `github.com/mozership/probe/safefile` | not for use outside probe |
| `github.com/mozership/probe/truncate` | not for use outside probe |
| `github.com/mozership/probe/pb` | `actionrpc.Serve`, as below |
| `actions/grpc.NewCoverage` | not for use outside probe; `probe coverage` |

The gRPC service between Probe and an action keeps its name, `pb.Actions`,
on the wire, so an action built with an earlier probe still runs under this
one. Code that implemented `pb.ActionsServer` or `pb.ActionsClient` itself,
as v1.27.0 told of, can no longer name them, and serves an action with
`actionrpc.Serve`. `actionrpc.Server.Run` and `Server.Cleanup` take the
moved `pb` types, so only `Serve` calls them.

#### If your action imports `mapping`

An external action that read its parameters with `mapping` does so with
`actionrpc`, the package it is written against. The functions run the same
code, so an action reads its parameters as the built-in actions read
theirs.

| Before | After |
|---|---|
| `mapping.MapToStructByTags(with, &req)` | `actionrpc.DecodeParams(with, &req)` |
| `mapping.StructToMapByTags(v)` | `actionrpc.EncodeResult(v)` |
| `mapping.FieldTags(v)` | `actionrpc.ParamNames(v)` |
| `mapping.ParseTimeout(v)` | `actionrpc.ParseTimeout(v)` |

They read more strictly than `mapping` did in v1.29.0. A number field
refuses a fraction or a value outside its range, where it was cut down, and
a `time.Duration` field refuses a number. A bool field takes 0 and 1. A
`map[string]string` field takes a number or a bool as its text and nil as
an empty string, and refuses a list or a map. `ParseTimeout` rounds a
fraction of a second to the nearest nanosecond, where it was truncated.

#### Running the action of a step

A run used to find the runner of a step's action on the step, where only a
test could set it, and had two interfaces for it. There is now one, with one
method, and the runner belongs to the run.

| Before | After |
|---|---|
| `ActionRunner.RunActions(name string, with map[string]any, opts RunOptions) (map[string]any, error)` | `ActionRunner.Run(name string, call actionrpc.Call, opts RunOptions) (result, newState map[string]any, err error)` |
| `StepActionRunner`, with `RunStep` | removed; `ActionRunner` is the one interface |
| `PluginActionRunner.RunActions`, `PluginActionRunner.RunStep` | `PluginActionRunner.Run` |
| `MockActionRunner`, `NewMockActionRunner` | removed; a test writes its own `ActionRunner` |
| A runner set on each `Step`, by tests alone | `Config.ActionRunner` for a workflow, `WithActionRunner(r)` for `Job.RunStandalone`; nil runs each action in a process of its own, as before |

`Run` is called concurrently and is to return once `opts.Context()` is done.
A runner of its own takes part in the cleanups at the end of a run only by
passing the call on to `PluginActionRunner` with `opts` as it was given.

#### The steps that pass without a test

In v1.29.0 every step without a test counted as untested. Since then a
`hello` step passes without one, as the `probe` command runs and checks
workflows; the engine does not know `hello` by name, but is told which
actions' steps pass without a test, and the `probe` command tells it. A
program that runs workflows through the root package and leaves it unset
counts such a step as untested, as v1.29.0 did.

| To get what `probe` does | Set |
|---|---|
| for a run | `actions.Configure(&cfg)`, which sets `Config.Actions` and `Config.NeedsNoCheck` |
| for `Check` | `actions.CheckOptions()`, which fills every field but `Manifest` |
| for `Job.RunStandalone` | `WithNeedsNoCheck(actions.NeedsNoCheck())` |

#### The root package `probe`

These are removed, with what to use where there is something:

| Removed | Use instead |
|---|---|
| `Job.Start`, `Job.RunIndependently` | `Job.RunStandalone`, or run the workflow |
| `JobScheduler`, `NewJobScheduler`, `JobStatus`, `JobPending`, `JobRunning`, `JobCompleted`, `JobFailed`, and the `JobScheduler` of a `JobContext` | nothing; the scheduler is internal |
| `Outputs.Get`, `Outputs.GetFlat`, `Outputs.GetAllWithFlat` | `Outputs.GetAll()`, by the step's id or the output's name |
| `Outputs.GetConflicts` | — |
| `JobResult.Success` | `JobResult.Succeeded()`, read from `JobResult.Status` |
| `Workflow.SetExitStatus` | — ; a failing job marks its run, and `Probe.ExitStatus` reads it |
| `LoadEmbeddedJob(path)` | `ReadJobFile(path, names)`, which reads a job file as the embedded action runs it |
| `Step.Do`, `Step.DoTest`, `Step.SetCtx`, `Step.Expr`, `Step.Idx` | — ; what a run finds out is no longer kept on the `Step` |
| `Config.Log`, `LogLevel` and its constants | `Config.Warn` for what a load warns of |
| `ErrorTypeValidation`, `ErrorTypeNetwork`, `ErrorTypeDependency`, `ErrorTypeAction`, `NewActionError` | — ; no error of these types was made |
| `ProbeError.Context`, `ProbeError.WithContext` | — ; never printed |
| `NewStreamReporter`, `NewBufferedReporter` | — ; they take a renderer that is internal |
| `Printer.Buffer`, `BufferIDs`, `SetBufferIDs`, `SetBaseline`, `PrintHeader`, `PrintReport`, `GenerateReport`, `GenerateReportOnlySteps` | `Config.Baseline`; the report is rendered by the run |
| `Printer.SetReporter`, `Printer.Reporter`, `Printer.StepStart` | `Result.SetReporter`; the printer knows no reporter |
| `StepResult.TestOutput`, why a step failed, laid out and colored | `StepResult.Failure` |
| `RepeatTestResult.Output` | `RepeatTestResult.Failure` |

These change:

| Before | After |
|---|---|
| `NewExecutor(w *Workflow, job *Job)` | `NewExecutor(job *Job)` |
| `Executor.Execute` | `Executor.Run` |
| `NewPrinter(verbose bool, bufferIDs []string)` | `NewPrinter(verbose bool)` |
| `JobResult.Status string` (`"Running"`, `"Completed"`, `"Failed"`, `"skipped"`) | `JobResult.Status StatusType`: `StatusSuccess`, `StatusError` or `StatusSkipped` |
| `JobContext` held the run's config, printer, result, scheduler and repeat counters as fields of its own | It holds the run's own vars, outputs and state; what the jobs share and what the runs of a repeated job share are in unexported fields, so a `JobContext` literal that set them no longer compiles |
| `Workflow.Env()` returned, after a `Start` given `Config.Env`, that run's env merged in | It returns the workflow's own env; `Config.Env` is the run's alone |
| `StepResult.EchoOutput`, indented for the terminal | `StepResult.Echo`, as the echo came to, and `StepResult.HasEcho` |
| `StepRepeatCounter.EchoOutputs` | `StepRepeatCounter.Echoes`, as each echo came to |
| `StepResult.Name` of a skipped step ended with ` (SKIPPED)` | It is the step's name |
| `New` set `PROBE_TTY=1` in the environment of the process when its standard output was a terminal | The `probe` command sets it. A program that wants the jobs of embedded actions colored on a terminal sets `PROBE_TTY=1` or `FORCE_COLOR=1` itself |

A workflow built in code, without `Load`, now gives a job without an ID the
ID `job_N`, as a loaded workflow has, where it used to take the job's name,
so a `needs` that names a job by its name no longer finds it. `Load` no
longer writes `step_N` into a step without an `id`: such a step publishes
its outputs by their names alone (`Outputs.SetFlat`). `Load`, and `Start`
for a workflow built in code, refuse a workflow whose expressions,
templates, step IDs or written `wait` cannot run, before any job runs.

`Interval`, behind a step's `timeout` and the `interval` and
`initial_delay` of a repeat and a retry, keeps the fraction of a number of
seconds (`1.5` was cut to 1s), and refuses a negative value and one too
long for a `time.Duration`, with errors that read `invalid duration X: ...`.

#### `actionrpc` on the side that runs an action

`actionrpc` had six functions to start an action and call it once. There
are now two, named for what they start, and both take a `ctx`: once it is
done, the action is sent SIGTERM and killed `TermGrace` later. Nothing an
action calls changes.

| Before | After |
|---|---|
| `actionrpc.RunStep(name, call, log)` | `actionrpc.RunBuiltin(ctx, name, call, log)` |
| `actionrpc.RunExecutableStep(path, sum, call, log)` | `actionrpc.RunExecutable(ctx, path, sum, call, log)` |
| `actionrpc.RunExecutable(path, sum, with, log) (map[string]any, error)` | the same name, with the signature above, which returns the state the action leaves too |
| `actionrpc.Run(name, with, log)` | `RunBuiltin`, with `Call{With: with}` |
| `(*Client).RunStep(call)` | `(*Client).RunStep(ctx, call)` |
| `(*Client).Run(with)` | `(*Client).RunStep(ctx, Call{With: with})`; a `Client` is no longer an `Action` |

`TermGrace`, how long an action given up on has to exit, and `ExitWait`,
how long a call takes at most to return once its `ctx` is done, are new.
An error of a call is now the action's own message, or an `*ExitError` or
`*StartError` when the action exited or did not start.

#### `actions` and the packages of the built-in actions

Each built-in action's package declares itself by one function, `Spec()`,
which the registry in `actions` reads. The functions each package exported
for the registry are gone, as is the one that served the action:

| Before | After |
|---|---|
| `Serve()` of `db`, `dns`, `grpc`, `hello`, `http`, `imap`, `shell`, `smtp`, `ssh` | `actions.Lookup(name)` |
| `embedded.Serve()` | `embedded.ServeWithActions(names, opts...)`, as `actions.Lookup("embedded")` serves it |
| `Keeps()` of `db`, `dns`, `embedded`, `grpc`, `hello` and `http`, and `Params()` of each package but `hello` | `actions.Keeps()[name]`, `actions.AllParams()[name]`, or the fields of `<package>.Spec()` |
| `actions.Keeping()` | `actions.Keeps()` |
| `actions.Params(name)` | `actions.AllParams()[name]` |
| `embedded.ServeWithActions(builtin []string)` | `embedded.ServeWithActions(builtin []string, opts ...Option)`; a call compiles as it did |

`Spec()` returns an `actionspec.Spec`, a type under `internal/`: its fields
can be read, but the type cannot be named outside probe.

#### `actions/smtp`

| Before | After |
|---|---|
| `smtp.Send(data, opts...)` | `smtp.Deliver(data, opts...)` |
| `Mail.Send()` | `Mail.SendSession()` |
| `Bulk.Send()`, `Bulk.Deliver()`, `Bulk.MakeData()`, `NewBulk` | `Bulk.DeliverWithResult()`, with `Bulk` filled from a `Req` |
| `Dial(addr)` | `Dial(addr, timeout)`; zero for no limit |
| `Res.FilePath` | removed; it was always empty |
| `Mail.Auth`, `Client.Auth`, `Client.Verify`, `Client.Reset`, `Client.Noop`, `Client.TLSConnectionState` | removed; the action has no auth parameters |
| `MockServer`, `MockServerSession`, `TLS`, `OptimisticUID` | removed; the mock server is internal to probe's tests |

`Req.Timeout`, `Bulk.Timeout`, `Mail.Timeout`, `Mail.LastSent`,
`DeliveryResult.LastSent` and `SentMessage` are new.

#### `actions/imap`

The mailbox commands were methods of `Req` that panicked unless `Do` had
connected first. They are now methods of the session `Do` opens:
`Req.Select`, `Examine`, `Search`, `UIDSearch`, `List`, `Fetch`,
`UIDFetch`, `Store`, `Copy`, `Create`, `Delete`, `Rename`, `Subscribe`,
`Unsubscribe`, `Noop` and `ExecCommands` are removed. `Request`, `NewReq`,
`Req.Do` and the result types are unchanged.

`Config.ActionRunner`, `Config.NeedsNoCheck`, `Config.Jobs`, `Config.Trace`,
`Config.Warn`, `CheckOptions.Contracts`, `ReqFields`, `ResFields`,
`ReqTypes`, `ResTypes`, `WithFields`, `Required` and `NeedsNoCheck`,
`RunOptions.Context` and `RunOptions.Trace`, `WithActionRunner`,
`WithNeedsNoCheck`, `ReadJobFile`, `Outputs.SetFlat`, `JobResult.Succeeded`
and `Printer.PrintGivenRequest` are new in the root package.
`actionrpc.Call.Contract`, `Call.Processes`, `Contract`, `ContractCheck`,
`ContractCheckOf`, `ContractMatch`, `Violation`, `Process`, `Processes`, `ExitError`, `StartError`, `DecodeParams`,
`EncodeResult`, `ParamNames` and `ParseTimeout` are new, as are
`actionref.Manifest.Res`; `actions.Configure`, `CheckOptions`,
`NeedsNoCheck`, `Contracts`, `WithFields`, `AllRequired`, `AllReqFields`,
`AllResFields`, `AllReqTypes` and `AllResTypes`; `embedded.WithNeedsNoCheck`;
`mask.MinLength` and `Masker.TooShort`; and `report.ReadBaseline`,
`report.Selection`, `Report.Selection` and `Report.Partial`.

### Behaviour Changes

- A job starts as soon as the jobs it needs are done, not when every job started with them is (#412)
- `probe a.yml b.yml` reads the files as one workflow, and `--job` runs the jobs named and what they need (#433)
- `probe check` reports errors it did not, such as names, fields of `res` and `req`, required keys and the kinds of values an expression reads, so a workflow it passed in v1.29.0 may now fail it (#437, #447, #450)
- With the `probe` command, a `hello` step without a test passes, and `probe check` no longer warns of it (#448)
- A secret shorter than 4 characters is masked only where it is a whole value, and the password in a URL is always hidden (#443, #444)
- Starting the same workflow twice in a program gives each start a run of its own: no job failed, no outputs, every job, and its own env (#459)
- `timeout` of the shell, ssh, grpc, db and dns actions is read as smtp and imap read it: a duration or a number of seconds, fractions kept, above zero. ssh and grpc now take `30`; shell refuses `0`, which stopped the command at once (#483)
- The http action's `timeout` refuses a value too long for a duration, or below a nanosecond, where it used to wrap or mean no limit; `0` still means no limit (#483)
- `interval: 1.5` and `initial_delay: 1.5` are 1.5s, not 1s, and `wait: 1.5` is taken; a negative `interval`, `initial_delay`, step `timeout` or `wait` is an error, as is one too long for a duration (#483)
- A number given for an integer parameter of any built-in action must be whole and fit it, as `session: 2.5` used to be cut to 2; a bool parameter takes 0 and 1 (#475)
- The smtp action has a `timeout`, `1m` by default and at most `24h`, for each wait on the server; `session` is 1 to 1000, `message` 1 or more, `length` at most 100 MiB (#475)
- smtp's `res.maildata` is the last message the server accepted, exactly as sent; `res.filepath` is gone; the messages of a step share one `Date`; `addr is required` and the like replace `Req.Addr is required` (#475)
- The imap action's errors read `invalid imap parameters: ...` and `invalid search criteria: ...`, and drop the `failed to Do(): ` prefix (#473)
- An imap step logs its request and response at `-vv`, with the password hidden (#453)
- A response of a `+json` or `+xml` type, such as `application/problem+json`, is text in the http action's `res.body`, decoded when JSON, not a file in `res.filepath` (#455)
- A db step with `DESC t` returns its rows; a statement is read by its first word, so `SELECT*FROM t` counts as `SELECT` under `--read-only` (#454)
- grpc's `insecure: true` still sends the client certificate, and `cert_file` without `key_file`, or the reverse, fails the step (#452)
- An invalid `/pattern/` in `match_json` or `diff_json` fails with an error that names its key, not a panic; `diff_json` reports the first difference in sorted key order (#458)
- `probe dag` measures names by terminal columns, so a box holding wide characters lines up (#482)
- Jobs started together, skipped jobs and the jobs of a `jobs ... cannot run` error are in the order of the workflow, not map order or sorted (#476)
- `StepResult.Elapsed` counts the time the step's test took (#480)
- A line of an action's log over 64 KiB is written, where it and the rest of the write were dropped (#474)
- A number or a bool in the grpc action's `metadata`, or the http or imap action's `headers`, is sent as its text; nil is an empty value, where http sent `<nil>`; a list or a map is an error naming the key (#485)
- A log or a template output cut short ends at the start of a character, not inside a multi-byte one (#485)
- The `error_message` of a JSON body that does not parse reads `invalid JSON: ...`, not `mustMarshalJSON error: ...` (#485)
- What is shown under a failed step, and the message of a failure in the report files, are masked as the report is written, so a value a later step uses as a credential is hidden there too (#484, #487)
- An empty item under `steps`, such as a lone `-`, is reported as `the step is empty` by `Load` and `probe check`, where both panicked (#472)
- With `-v`, the lines of each block of verbose output come out together, not interleaved with another job's (#463)

## v1.27.0 (2026-10-13)

### Breaking Changes

This release breaks the Go API in a minor version, for code that implements
the gRPC service of an action itself. Workflow files are not affected, and
neither is an action served with `actionrpc.Serve`, which is how an external
action is written.

#### `pb.ActionsServer` and `pb.ActionsClient`

The service an action serves has a second method, `Cleanup`, which Probe
calls once a run is over on an action whose `action.yml` sets `cleanup`.
`pb.ActionsServer` and `pb.ActionsClient` gain it, so a type that implements
either without `actionrpc` no longer does.

| Before | After |
|---|---|
| A server with `Run` alone implements `pb.ActionsServer` | It also needs `Cleanup(context.Context, *pb.CleanupRequest) (*pb.CleanupResponse, error)`, or embeds `pb.UnimplementedActionsServer`, which answers it as not implemented |
| A stand-in with `Run` alone implements `pb.ActionsClient` | It also needs `Cleanup(ctx context.Context, in *pb.CleanupRequest, opts ...grpc.CallOption) (*pb.CleanupResponse, error)` |

An action built against an earlier probe still runs under this one: its steps
are run as before, and it is not called once the run is over, as its
`action.yml` cannot ask for that.

`actionrpc.Cleaner`, `actionrpc.CleanupCall`, `actionrpc.Cleanups`,
`Call.Cleanups`, `Client.Cleanup`, `Server.Cleanup` and
`actionrpc.CleanupExecutable` are new, as are `actionref.Manifest.Cleanup`,
`actionref.Full`, `procgroup.Tracker.AfterStop`, and `WithCleanups` of the
root package and of `actions/embedded`.

## v1.26.0 (2026-10-12)

### Breaking Changes

This release breaks the Go API in a minor version. Workflow files are not
affected: an `iteration` written as a list is read as it was.

#### The module path

The module is now `github.com/mozership/probe`, after the organization the
repository moved to. Code that imports probe, as an external action does for
`actionrpc`, replaces the path in its imports and in its `go.mod`:

| Before | After |
|---|---|
| `import "github.com/linyows/probe/actionrpc"` | `import "github.com/mozership/probe/actionrpc"` |
| `go install github.com/linyows/probe/cmd/probe@latest` | `go install github.com/mozership/probe/cmd/probe@latest` |

Versions up to v1.25.0 stay under `github.com/linyows/probe`, which gets no
later one: `go get -u` on the old path does not find this release.

#### `Step.Iteration`

`Step.Iteration` was the list of entries a step runs once for each of. A
workflow can now give a template in its place, which comes to the list when
the step is reached, so the field holds either.

| Before | After |
|---|---|
| `Step.Iteration []map[string]any` | `Step.Iteration Iteration`, with the list in `Iteration.Entries` and the template in `Iteration.Expr` |
| `Step{Iteration: []map[string]any{...}}` | `Step{Iteration: Iteration{Entries: []map[string]any{...}}}` |
| `len(step.Iteration)`, `range step.Iteration` | `len(step.Iteration.Entries)`, `range step.Iteration.Entries`; a step whose `Iteration.Expr` is set has its entries only when it runs |

`Iteration` reads and writes YAML as the field did: a list of maps, or a
string that is one template.

`Workflow.Defaults`, `Job.Always`, `Job.FailFast`, `Config.Vars` and
`Config.Env` are new,
`mask.Masker` gains `Value`, and the `envfile` package reads files of
environment variables.

## v1.22.0 (2026-10-09)

### Breaking Changes

This release breaks, in a minor version, a workflow that uses the built-in
`browser` or `mail-latency` action, and code that imports their packages.

The built-in `browser` action is removed. It is now the external action
`github.com/mozership/probe-browser`, which a step names by repository and
commit, as `uses: github.com/mozership/probe-browser@<commit SHA>`, in place
of `uses: browser`. Its `with` and its result are the same.

The built-in `mail-latency` action is removed in the same way. It is now
`github.com/mozership/probe-mail-latency`, named as
`uses: github.com/mozership/probe-mail-latency@<commit SHA>` in place of
`uses: mail-latency`, with the same `with` and result. Its `action.yml`
declares `guard: [allow-host]`, so it now runs under `--allow-host` without
`--allow-action`.

Their packages moved with them, with no alias left:

| Before | After |
|---|---|
| `github.com/linyows/probe/actions/browser` | `github.com/mozership/probe-browser/browser` |
| `github.com/linyows/probe/actions/mail-latency` (package `maillatency`) | `github.com/mozership/probe-mail-latency/maillatency` |

`actions.Names`, `actions.Params` and `actions.AllParams` no longer list
`browser` or `mail-latency`, and probe no longer depends on chromedp.

## v1.21.0 (2026-10-09)

### Breaking Changes

This release breaks the Go API in a minor version, as v1.20.0 did.
Workflow files are not affected.

The actions that run under a guard used to be a list of names in Probe.
Each action now declares the kinds of guard it keeps to: a built-in one in
its package, an external one in `guard` in its `action.yml`. A step runs
under a guard only when its action declares every kind the run is under,
`read-only` for `--read-only` and `allow-host` for `--allow-host`, or when
`--allow-action` names it. The built-in actions keep to the guard as they
did.

| Before | After |
|---|---|
| `actionrpc.Guard.Keeping []string`, the actions that keep to the guard | `actionrpc.Guard.Keeps map[string][]string`, the kinds of guard each action keeps to, by its `uses` |
| `Guard.Runs(uses)`: the action is in `Keeping` | `Guard.Runs(uses)`: the action keeps to every kind in `Guard.Kinds()`; `Guard.Missing(uses)` returns those it does not |
| `pb.Guard.keeping`, field 4 | `pb.Guard.keeps`, field 5, a map of `GuardKinds`; field 4 is reserved |
| Building a `Guard` with `Keeping: actions.Keeping()` | `Keeps: actions.Keeps()` |

A `Guard` that sets neither leaves every action refused under it, as one
without `Keeping` did. `actions.Keeping()` still returns the names of the
built-in actions that keep to every kind, and each of `db`, `embedded`,
`grpc`, `hello` and `http` has a `Keeps()` that returns the kinds it
declares. `Guard.WithKeeps` and the constants `actionrpc.KindReadOnly` and
`actionrpc.KindAllowHost` are new.

An external action is told the guard as before; an executable built with
an earlier `actionrpc` reads the guard it is sent, and leaves the new field
alone.

`actionref.Manifest` gains `Guard` and `Params`, and `Resolver.Manifest`
and `actionref.ReadManifest` read an `action.yml` without fetching the
executable. Probe reads it for every external action before the first job,
so a step refused under a guard still has its `action.yml` read, but not
its executable fetched. `probe.CheckOptions` gains `Manifest`, which
`probe check` sets to check the `with` of an external action against the
`params` of its `action.yml`; left nil, nothing is read, as before.

## v1.20.0 (2026-10-08)

### Breaking Changes

This release breaks the Go API in a minor version, as v1.18.0 did, and
raises the Go version a module importing probe needs. Workflow files are
not affected.

Probe needs Go 1.27, up from 1.26.6, because chromedp v0.19.1, which the
browser action is built on, needs it. A module that imports probe has to be
built with Go 1.27 or later.

chromedp turned its actions into the generic type `chromedp.Action[T]`, and
an action that returns no value is a `chromedp.Action[chromedp.Void]`. The
runner of the browser action takes those, so an implementation of
`browser.BrowserRunner`, and code that reads the actions `browser.MockRunner`
recorded, has to be updated.

| Before | After |
|---|---|
| `BrowserRunner.Run(ctx context.Context, actions ...chromedp.Action) error` | `BrowserRunner.Run(ctx context.Context, actions ...chromedp.Action[chromedp.Void]) error` |
| `MockRunner.RunFunc`, `SetRunFunc`: `func(context.Context, ...chromedp.Action) error` | `func(context.Context, ...chromedp.Action[chromedp.Void]) error` |
| `MockRunner.CallHistory`, `GetAllCalls`: `[][]chromedp.Action` | `[][]chromedp.Action[chromedp.Void]` |
| `MockRunner.GetLastCall`: `[]chromedp.Action` | `[]chromedp.Action[chromedp.Void]` |

## v1.19.0 (2026-10-06)

### Breaking Changes

This release breaks the Go API in two narrow ways, both in what was added
since v1.18.0. Code that only calls these functions is unaffected; code that
uses them as values or compares them has to be updated.

`(*Job).RunStandalone` takes options after its other parameters, as
`...StandaloneOption`, so that a job run by the embedded action can be told
the run of the step that embeds it with `WithRunID`. A call compiles as it
did, but the method as a value has another type, so assigning it to a
variable of the old function type, or an interface declaring the old
signature, no longer compiles.

| Before | After |
|---|---|
| `func(map[string]any, *Printer, string, string) JobRun` | `func(map[string]any, *Printer, string, string, ...StandaloneOption) JobRun` |

`expr.Expr` has a field of function type, `BeforeTemplate`, which `template()`
calls with the text it is about to expand, so `Expr` is no longer
comparable: comparing two with `==`, or using one as a map key, no longer
compiles.

### Behaviour Changes

Every request of the http action has a cookie jar, so a cookie a server
sets on a redirect is sent on to where the redirect leads, as a browser
sends it; it used to be dropped. A `cookie` header written in `headers` is
still sent. The cookies set are in `res.cookies`, and are kept across steps
only with the new `keep_cookies`.

A key of a map in `with`, `vars` or a step's `vars` that holds `{{ }}` is
now evaluated as a template, as a value is; it used to be sent as written.
Two keys that come to the same key are an error.

More is hidden in the output: the values of the http action's `cookies` and
`res.cookies`, and a secret or credential as a form body percent-encodes it.
With `-v`, the http action logs the method, URL, status and headers of each
request and response, with credentials hidden, where it used to log only a
warning that they could not be encoded.

## v1.18.0 (2026-10-05)

### Breaking Changes

This release breaks the Go API in a minor version, as v1.16.0 did: code
that imports probe as a library has to be updated. Workflow files are
affected too, by the first behaviour change below: a template that cannot
be evaluated now fails its step, where its value used to be sent on with
the error written into it.

The packages that implement the built-in actions moved under `actions/`,
each into the package of the action it serves, so that the repository root
keeps the engine and its helpers. No aliases are left behind, so code that
imports them has to be updated as follows. The command line, workflow files
and external actions, which only use `actionrpc`, are unaffected.

| Before | After |
|---|---|
| `github.com/linyows/probe/http` | `github.com/linyows/probe/actions/http` |
| `github.com/linyows/probe/grpc` | `github.com/linyows/probe/actions/grpc` |
| `github.com/linyows/probe/db` | `github.com/linyows/probe/actions/db` |
| `github.com/linyows/probe/browser` | `github.com/linyows/probe/actions/browser` |
| `github.com/linyows/probe/shell` | `github.com/linyows/probe/actions/shell` |
| `github.com/linyows/probe/ssh` | `github.com/linyows/probe/actions/ssh` |
| `github.com/linyows/probe/imap` | `github.com/linyows/probe/actions/imap` |
| `github.com/linyows/probe/embedded` | `github.com/linyows/probe/actions/embedded` |
| `github.com/linyows/probe/grpc/testserver/pb` | `github.com/linyows/probe/actions/grpc/testserver/pb` |

The `mail` package served two actions, so it was split between them:

| Before | After |
|---|---|
| `mail.Send`, `mail.Req`, `mail.Res`, `mail.Result`, `mail.NewReq`, `mail.Option`, `mail.Callback`, `mail.WithBefore`, `mail.WithAfter`, `mail.Mail`, `mail.Bulk`, `mail.NewBulk`, `mail.DeliveryResult`, `mail.Client`, `mail.Dial`, `mail.NewClient`, `mail.StartTLSOff`, `mail.StartTLSAuto`, `mail.StartTLSRequired`, `mail.TLS`, `mail.OptimisticUID`, `mail.MockServer`, `mail.MockServerSession` | the same names in `github.com/linyows/probe/actions/smtp` |
| `mail.GetLatencies`, `mail.Latency`, `mail.Latencies`, `mail.ReadFirstBytes`, `mail.IsMailText`, `mail.HasFlexedPrefix`, `mail.CreateHistogramByBucket`, `mail.CreateHistogramByDuration` | the same names in `github.com/linyows/probe/actions/mail-latency` (package `maillatency`) |

`expr.EvalTemplateMap` now returns an error as well as the map, and
`expr.EvalTemplate` returns an error for a template that cannot be
evaluated. Both used to write the error into the value instead, as
`[CompileError: ...]`, `[RuntimeError: ...]` or `[EvaluationError]`, and
return no error. The error is a `*expr.TemplateError` naming the template;
`EvalTemplateMap` joins one `*expr.FieldError` for each value that failed,
naming its path, such as `headers.authorization`, and returns the map with
nil in place of each such value.

| Before | After |
|---|---|
| `m := ev.EvalTemplateMap(in, env)` | `m, err := ev.EvalTemplateMap(in, env)` |

### Behaviour Changes

A template that cannot be evaluated now fails what needs it instead of
being sent on with the error written into it. In a step's `with`, `vars`
or `name`, the step fails with the new failure kind `template` and the
action does not run; in a workflow's `vars`, the run stops before the first
job with exit status 2. A name that is not defined still reads as nil and is
not an error. `echo` shows the error, indented as its other lines are,
instead of the Go representation of the error value.

The result of a step in a job with `repeat` now carries why its first
failing iteration failed, so a JSON report keeps the kind, such as `action`
or `template`, and JUnit reports an `<error>` for it instead of an assertion
`<failure>`.

`mapping.AssignStruct` now enforces `validate:"required"` on an `int` field:
a missing key is reported as `params '<name>' is required`, as it already was
for a string, and as `mapping.MapToStructByTags` does. It used to accept a
missing required int and leave the field at zero. A value of `0` that is
given still passes. Code that relied on omitting such a field has to give it.

## v1.16.0 (2026-10-04)

### Breaking Changes

This release breaks the Go API in a minor version: code that imports probe
as a library has to be updated, while the command line and workflow files
are unaffected.

Helpers that do not depend on the workflow engine moved out of the root
`probe` package into packages of their own. No aliases are left behind, so
code that imports them has to be updated as follows.

| Before | After |
|---|---|
| `probe.Actions` | `actionrpc.Action` |
| `probe.ActionsPlugin` | `actionrpc.Plugin` |
| `probe.ActionsClient` | `actionrpc.Client` |
| `probe.ActionsServer` | `actionrpc.Server` |
| `probe.Handshake` | `actionrpc.Handshake` |
| `probe.PluginMap` | `actionrpc.PluginMap` |
| `probe.BuiltinCmd` | `actionrpc.BuiltinCmd` |
| `probe.LogActionParams` | `actionrpc.LogParams` |
| `probe.LogActionOutcome` | `actionrpc.LogOutcome` |
| `Workflow.RenderDagAscii()` | `dag.ASCII{}.Render(w.Graph())` |
| `Workflow.RenderDagMermaid()` | `dag.Mermaid{}.Render(w.Graph())` |
| `probe.Expr` | `expr.Expr` |
| `probe.Report` | `report.Report` |
| `probe.ReportSummary` | `report.Summary` |
| `probe.ReportCount` | `report.Count` |
| `probe.JobReport` | `report.Job` |
| `probe.StepReport` | `report.Step` |
| `probe.RetryReport` | `report.Retry` |
| `probe.RepeatReport` | `report.Repeat` |
| `probe.FailureReport` | `report.Failure` |
| `probe.ReportPassed`, `ReportFailed`, `ReportSkipped`, `ReportUntested` | `report.Passed`, `Failed`, `Skipped`, `Untested` |
| `probe.ReportFormat` | `report.Format` |
| `probe.ReportJSON`, `ReportJUnit`, `ReportMarkdown`, `ReportGitHubSummary` | `report.JSON`, `JUnit`, `Markdown`, `GitHubSummary` |
| `probe.ReportTarget` | `report.Target` |
| `probe.ParseReportTargets` | `report.ParseTargets` |
| `probe.ErrNoStepSummary` | `report.ErrNoStepSummary` |
| `probe.ErrStepSummaryFull` | `report.ErrStepSummaryFull` |
| `probe.MatchJSON` | `jsonutil.Match` |
| `probe.DiffJSON` | `jsonutil.Diff` |
| `probe.ParseJSON` | `jsonutil.Parse` |
| `probe.Masker` | `mask.Masker` |
| `probe.NewMasker` | `mask.New` |
| `probe.TruncateString` | `truncate.String` |
| `probe.TruncateMapStringString` | `truncate.MapString` |
| `probe.TruncateMapStringAny` | `truncate.Map` |
| `probe.GetTruncationMessage` | `truncate.Message` |
| `probe.MaxLogStringLength` | `truncate.MaxLogLength` |
| `probe.MaxStringLength` | `truncate.MaxLength` |

`Printer.SetMasker`, `Printer.Masker`, `Report.Mask` and `RunOptions.Masker`
now take or return a `*mask.Masker`, and `Step.Expr` is a `*expr.Expr`.
`BuildReport` stays in the root package and returns a `*report.Report`, and
`Config.Reports` is a `[]report.Target`.

`probe.ActionsArgs` and `probe.ActionsParams` are removed; nothing used them.
An action is now served with `actionrpc.Serve`, which builds the logger and
the plugin that each action used to build for itself.

`probe.DagRendererBase`, `probe.DagAsciiRenderer`, `probe.DagAsciiJobNode`,
`probe.DagMermaidRenderer` and their constructors are removed. The renderers
are now `dag.ASCII` and `dag.Mermaid`, which draw the `dag.Graph` that
`Workflow.Graph` returns.

The new packages are imported from `github.com/linyows/probe/actionrpc`,
`github.com/linyows/probe/expr`, `github.com/linyows/probe/jsonutil`,
`github.com/linyows/probe/mask`, `github.com/linyows/probe/report` and
`github.com/linyows/probe/truncate`, and the renderers from the existing
`github.com/linyows/probe/dag`. Two more new packages, `safefile` and
`procgroup`, hold what used to be unexported in the root package.
