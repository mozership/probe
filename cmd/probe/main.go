package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/fatih/color"
	"github.com/mozership/probe"
	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/actions"
	"github.com/mozership/probe/actions/grpc"
	"github.com/mozership/probe/envfile"
	"github.com/mozership/probe/oas"
	"github.com/mozership/probe/report"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	c := newCmd()
	if c != nil {
		os.Exit(c.start(os.Args))
	}
}

type Cmd struct {
	WorkflowPath   string
	SubCommand     string
	SubCommandArgs []string
	Init           bool
	Lint           bool
	Help           bool
	Version        bool
	Verbose        bool
	Timing         bool
	DagMermaid     bool
	FmtCheck       bool
	Output         string
	Report         string
	Baseline       string
	ReadOnly       bool
	AllowHosts     string
	AllowActions   string
	// Vars are the --var flags given, each as name=value, in their order.
	Vars []string
	// EnvFiles are the files the --env-file flags name, in their order.
	EnvFiles []string
	// guardFlags are the guard's flags given, apart from their values, so
	// that a flag given empty or false wins over its environment variable.
	guardFlags map[string]bool
	validFlags []string
	ver        string
	rev        string
	inReader   io.Reader
	outWriter  io.Writer
	errWriter  io.Writer
	mocking    bool
}

func newCmd() *Cmd {
	info, ok := debug.ReadBuildInfo()
	ver, rev := resolveVersion(version, commit, info, ok)
	return &Cmd{
		validFlags: []string{"help", "h", "version", "timing", "verbose", "v", "mermaid", "check", "output", "report", "baseline", "read-only", "allow-host", "allow-action", "var", "env-file"},
		ver:        ver,
		rev:        rev,
		inReader:   os.Stdin,
		outWriter:  os.Stdout,
		errWriter:  os.Stderr,
		mocking:    false,
	}
}

func newBufferCmd() *Cmd {
	c := newCmd()
	c.outWriter = new(bytes.Buffer)
	c.errWriter = new(bytes.Buffer)
	c.mocking = true
	return c
}

// parseArgs parses command line arguments manually to allow options after arguments
func (c *Cmd) parseArgs(args []string) error {
	var nonFlagArgs []string
	skipNext := false

	for i := range args {
		if skipNext {
			skipNext = false
			continue
		}

		arg := args[i]

		// A - alone is the standard input, for fmt.
		if strings.HasPrefix(arg, "-") && arg != "-" {
			// Handle flags
			flagName := strings.TrimLeft(arg, "-")

			// Handle flags with "=" (e.g., --flag=value)
			flagValue := ""
			hasValue := false
			if idx := strings.Index(flagName, "="); idx != -1 {
				flagValue = flagName[idx+1:]
				hasValue = true
				flagName = flagName[:idx]
			}

			if !c.isValidFlag(flagName) {
				return fmt.Errorf("unknown flag: %s", arg)
			}

			// Set the appropriate flag
			switch flagName {
			case "help", "h":
				c.Help = true
			case "version":
				c.Version = true
			case "timing":
				c.Timing = true
			case "verbose", "v":
				c.Verbose = true
			case "mermaid":
				c.DagMermaid = true
			case "check":
				c.FmtCheck = true
			case "output":
				// Accept both --output=stream and --output stream
				if !hasValue {
					if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
						return fmt.Errorf("flag needs an argument: %s", arg)
					}
					flagValue = args[i+1]
					skipNext = true
				}
				if _, err := probe.ParseOutputMode(flagValue); err != nil {
					return err
				}
				c.Output = flagValue
			case "read-only":
				// --read-only alone turns it on; --read-only=false turns it
				// off over PROBE_READ_ONLY.
				c.ReadOnly = true
				if hasValue {
					v, err := parseBool(flagValue)
					if err != nil {
						return fmt.Errorf("%s: %w", arg, err)
					}
					c.ReadOnly = v
				}
				c.markGuardFlag(flagName)
			case "allow-host", "allow-action":
				if !hasValue {
					if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
						return fmt.Errorf("flag needs an argument: %s", arg)
					}
					flagValue = args[i+1]
					skipNext = true
				}
				if flagName == "allow-host" {
					c.AllowHosts = flagValue
				} else {
					c.AllowActions = flagValue
				}
				c.markGuardFlag(flagName)
			case "var", "env-file":
				// Both can be given more than once.
				if !hasValue {
					if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
						return fmt.Errorf("flag needs an argument: %s", arg)
					}
					flagValue = args[i+1]
					skipNext = true
				}
				if flagName == "env-file" {
					files := splitList(flagValue)
					if len(files) == 0 {
						return fmt.Errorf("flag needs an argument: %s", arg)
					}
					c.EnvFiles = append(c.EnvFiles, files...)
					break
				}
				if _, _, err := parseVar(flagValue); err != nil {
					return err
				}
				c.Vars = append(c.Vars, flagValue)
			case "report":
				// Accept both --report=junit and --report junit
				if !hasValue {
					if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
						return fmt.Errorf("flag needs an argument: %s", arg)
					}
					flagValue = args[i+1]
					skipNext = true
				}
				if _, err := report.ParseTargets(flagValue); err != nil {
					return err
				}
				c.Report = flagValue
			case "baseline":
				if !hasValue {
					if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
						return fmt.Errorf("flag needs an argument: %s", arg)
					}
					flagValue = args[i+1]
					skipNext = true
				}
				c.Baseline = flagValue
			}
		} else {
			// Non-flag arguments
			nonFlagArgs = append(nonFlagArgs, arg)
		}
	}

	// Check if first non-flag argument is a subcommand
	if len(nonFlagArgs) > 0 && isSubCommand(nonFlagArgs[0]) {
		c.SubCommand = nonFlagArgs[0]
		c.SubCommandArgs = nonFlagArgs[1:]
	} else if len(nonFlagArgs) > 0 {
		c.WorkflowPath = nonFlagArgs[0]
	}

	return nil
}

var subCommands = []string{"gen", "dag", "check", "fmt", "coverage", "guide", "skill", "manifest"}

func isSubCommand(name string) bool {
	return slices.Contains(subCommands, name)
}

func (c *Cmd) isValidFlag(flagName string) bool {
	return slices.Contains(c.validFlags, flagName)
}

func (c *Cmd) isValid(flag string) bool {
	if idx := strings.Index(flag, "="); idx != -1 {
		flag = flag[:idx]
	}

	return slices.Contains(c.validFlags, strings.TrimLeft(flag, "-"))
}

func (c *Cmd) usage() {
	logo := `
 __  __  __  __  __
|  ||  ||  ||  || _|
|  ||  /| |||  /|  |
| | |  \| |||  \| _|
|_| |_\_|__||__||__|
`

	desc := `
Probe - A YAML-based workflow automation tool.
https://github.com/mozership/probe (ver: %s, rev: %s)
`

	head := `
Usage: probe [options] <workflow-file>
       probe gen <openapi-file>
       probe dag [--mermaid] <workflow-file>
       probe check <workflow-file>
       probe fmt [--check] <workflow-file>...
       probe coverage <openapi-file|proto-file> <report-file>
       probe guide [topic]
       probe skill [install [dir]]
       probe manifest init <owner>/<repo>
       probe manifest <tag> <checksums-file> [action-file]

Arguments:
  workflow-file    Path to YAML workflow file(s). Multiple files can be
                   specified with comma-separated paths (e.g., "base.yml,override.yml")
                   to merge configurations.

Subcommands:
  gen <file>       Generate probe workflow YAML from OpenAPI specification
  dag <file>       Show job dependency graph as ASCII art (default)
                   Use --mermaid to output in Mermaid format
  check <file>     Find what is wrong or weak in a workflow without running it
  fmt <file>...    Format workflow files in place, and print those it changed
                   A directory is every YAML file under it, and - is the
                   standard input, formatted to the standard output
                   Use --check to change nothing, and exit with 1 when a
                   file is not formatted
  coverage <openapi-file|proto-file> <report-file>
                   Show which operations of an OpenAPI document, or methods of
                   a .proto file, the steps of a run checked, from its
                   --report json file
  guide [topic]    Print a page of the documentation as Markdown
                   Without a topic, list the topics
  skill            Print the skill that teaches coding agents to use Probe
                   install [dir] writes it to dir (.claude/skills/probe)
  manifest init <owner>/<repo>
                   Print an action.yml to start a new external action from,
                   published in that GitHub repository
  manifest <tag> <checksums-file> [action-file]
                   Print the action.yml of a release of an external action:
                   action-file (action.yml) with the tag in runs.url and the
                   digests of checksums-file in runs.checksums

Options:`

	blue := color.New(color.FgBlue)
	grey := color.New(color.FgHiBlack)

	_, _ = blue.Fprintln(c.errWriter, strings.TrimLeft(logo, "\n"))
	_, _ = grey.Fprintf(c.errWriter, strings.TrimLeft(desc, "\n"), c.ver, c.rev)
	_, _ = fmt.Fprintln(c.errWriter, head)
	c.printOptions()
}

func (c *Cmd) printOptions() {
	options := []struct {
		short, long, description string
	}{
		{"-h", "--help", "Show command usage"},
		{"", "--version", "Show version information"},
		{"", "--timing", "Show timing (start time, response time)"},
		{"-v", "--verbose", "Show verbose log"},
		{"", "--output", "Report output: auto, spinner or stream (env: PROBE_OUTPUT)"},
		{"", "--report", "Write reports: json, junit, markdown, github-summary as format[=path],... (env: PROBE_REPORT)"},
		{"", "--baseline", "Compare the response times to those of a --report json file (env: PROBE_BASELINE)"},
		{"", "--read-only", "Refuse what writes, such as an HTTP POST or an UPDATE (env: PROBE_READ_ONLY)"},
		{"", "--allow-host", "Refuse connecting to hosts but these, as host[:port] or *.domain,... (env: PROBE_ALLOW_HOSTS)"},
		{"", "--allow-action", "Run these actions under --read-only or --allow-host although they do not keep to them (env: PROBE_ALLOW_ACTIONS)"},
		{"", "--var", "Give a var of the workflow as name=value, in place of what it writes; can be repeated"},
		{"", "--env-file", "Read variables for the vars from files of NAME=value lines, as file,...; can be repeated"},
	}

	for _, opt := range options {
		if opt.short != "" {
			_, _ = fmt.Fprintf(c.errWriter, "  %s, %-14s %s\n", opt.short, opt.long, opt.description)
		} else {
			_, _ = fmt.Fprintf(c.errWriter, "      %-14s %s\n", opt.long, opt.description)
		}
	}
}

func (c *Cmd) start(args []string) int {
	if len(args) >= 3 && args[1] == actionrpc.BuiltinCmd {
		c.runBuiltinActions(args[2])
		return 0
	}

	// Parse arguments manually to allow options after arguments
	if err := c.parseArgs(args[1:]); err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\ntry --help to know more\n", err)
		return probe.ExitConfigError
	}

	switch {
	case c.Help:
		c.usage()
		return 1

	case c.Version:
		c.printVersion()
		return 0

	case c.FmtCheck && c.SubCommand != "fmt":
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] --check can only be used with the fmt subcommand\n")
		_, _ = fmt.Fprint(c.errWriter, fmtUsage)
		return probe.ExitConfigError

	case c.SubCommand != "":
		return c.runSubCommand()

	case c.DagMermaid:
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] --mermaid can only be used with the dag subcommand\n")
		_, _ = fmt.Fprintf(c.errWriter, "Usage: probe dag [--mermaid] <workflow-file>\n")
		return probe.ExitConfigError

	case c.WorkflowPath == "":
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] workflow is required\n")
		return probe.ExitConfigError

	default:
		if !c.mocking {
			return c.runProbe()
		}
		return 0
	}
}

func (c *Cmd) runSubCommand() int {
	switch c.SubCommand {
	case "gen":
		return c.runGen()
	case "dag":
		return c.runDag()
	case "check":
		return c.runCheck()
	case "fmt":
		return c.runFmt()
	case "coverage":
		return c.runCoverage()
	case "guide":
		return c.runGuide()
	case "skill":
		return c.runSkill()
	case "manifest":
		return c.runManifest()
	default:
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] unknown subcommand: %s\n", c.SubCommand)
		return probe.ExitConfigError
	}
}

func (c *Cmd) runGen() int {
	if len(c.SubCommandArgs) == 0 {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] OpenAPI spec file is required\n")
		_, _ = fmt.Fprintf(c.errWriter, "Usage: probe gen <openapi-file>\n")
		return probe.ExitConfigError
	}

	output, err := oas.Generate(c.SubCommandArgs[0])
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}

	_, _ = fmt.Fprint(c.outWriter, output)
	return 0
}

func (c *Cmd) runProbe() int {
	p := probe.New(c.WorkflowPath, c.Verbose)
	p.Config.Actions = actions.Names()
	if c.Timing {
		p.Config.Timing = true
	}

	// The flag wins over PROBE_OUTPUT, which in turn wins over auto detection.
	outputMode := c.Output
	if outputMode == "" {
		outputMode = os.Getenv("PROBE_OUTPUT")
	}
	mode, err := probe.ParseOutputMode(outputMode)
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	p.Config.Output = mode

	// Likewise the flag wins over PROBE_REPORT.
	spec := c.Report
	if spec == "" {
		spec = os.Getenv("PROBE_REPORT")
	}
	reports, err := report.ParseTargets(spec)
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	p.Config.Reports = reports

	// And over PROBE_BASELINE. The file is read before the run, so that a
	// run is not made for reports that cannot be compared.
	baseline := c.Baseline
	if baseline == "" {
		baseline = os.Getenv("PROBE_BASELINE")
	}
	if baseline != "" {
		r, err := report.Read(baseline)
		if err == nil && r.StartedAt.IsZero() {
			// Any JSON object parses as a report; one of a run says when
			// the run started.
			err = fmt.Errorf("%s is not a report of a run, written by --report json", baseline)
		}
		if err != nil {
			_, _ = fmt.Fprintf(c.errWriter, "[ERROR] --baseline: %v\n", err)
			return probe.ExitConfigError
		}
		p.Config.Baseline = r
	}

	guard, err := c.guard()
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	p.Config.Guard = guard

	// The files give variables for the vars of the workflow to read, as
	// they read the environment.
	env, err := loadEnvFiles(c.EnvFiles)
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] --env-file: %v\n", err)
		return probe.ExitConfigError
	}
	p.Config.Env = env
	p.Config.Vars = c.vars()

	if err := p.Do(); err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	return p.ExitStatus()
}

func (c *Cmd) runDag() int {
	if len(c.SubCommandArgs) == 0 {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] workflow file is required\n")
		_, _ = fmt.Fprintf(c.errWriter, "Usage: probe dag [--mermaid] <workflow-file>\n")
		return probe.ExitConfigError
	}

	if c.mocking {
		return 0
	}

	p := probe.New(c.SubCommandArgs[0], c.Verbose)
	p.Config.Actions = actions.Names()
	var graph string
	var err error
	if c.DagMermaid {
		graph, err = p.DagMermaid()
	} else {
		graph, err = p.DagAscii()
	}
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	_, _ = fmt.Fprint(c.outWriter, graph)
	return 0
}

// parseVar reads a --var, name=value. The value is all that comes after the
// first =, and may be empty.
func parseVar(s string) (name, value string, err error) {
	name, value, ok := strings.Cut(s, "=")
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return "", "", fmt.Errorf("--var must be name=value, not %q", s)
	}
	return name, value, nil
}

// vars returns the vars the --var flags give, by name. A name given more
// than once has the value given last.
func (c *Cmd) vars() map[string]string {
	if len(c.Vars) == 0 {
		return nil
	}
	vars := make(map[string]string, len(c.Vars))
	for _, v := range c.Vars {
		// Each was checked as it was parsed.
		name, value, _ := parseVar(v)
		vars[name] = value
	}
	return vars
}

// loadEnvFiles reads the variables the files at paths give, by name, for
// the vars of the workflow to read as they read the environment. Among the
// files, the last to give a variable wins.
//
// They are not set in the environment of this process: Probe's own
// settings, such as the guard of the run, and the environment of the
// actions it runs stay what the person running it set, whatever a file
// says.
func loadEnvFiles(paths []string) (map[string]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	env := map[string]string{}
	for _, path := range paths {
		vars, err := envfile.Read(path)
		if err != nil {
			return nil, err
		}
		for _, v := range vars {
			env[v.Name] = v.Value
		}
	}
	return env, nil
}

func (c *Cmd) markGuardFlag(name string) {
	if c.guardFlags == nil {
		c.guardFlags = map[string]bool{}
	}
	c.guardFlags[name] = true
}

// parseBool reads the value of a boolean flag or environment variable:
// true or 1, and false, 0 or empty.
func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	default:
		return false, fmt.Errorf("must be true or false, not %q", s)
	}
}

// guard returns the guard of the run from the flags, each of which wins over
// its environment variable when it is given, even empty or false:
// PROBE_READ_ONLY, PROBE_ALLOW_HOSTS and PROBE_ALLOW_ACTIONS.
func (c *Cmd) guard() (actionrpc.Guard, error) {
	readOnly := c.ReadOnly
	if !c.guardFlags["read-only"] {
		v, err := parseBool(os.Getenv("PROBE_READ_ONLY"))
		if err != nil {
			return actionrpc.Guard{}, fmt.Errorf("PROBE_READ_ONLY %w", err)
		}
		readOnly = v
	}
	hosts := c.AllowHosts
	if !c.guardFlags["allow-host"] {
		hosts = os.Getenv("PROBE_ALLOW_HOSTS")
	}
	allowed := c.AllowActions
	if !c.guardFlags["allow-action"] {
		allowed = os.Getenv("PROBE_ALLOW_ACTIONS")
	}
	return actionrpc.Guard{
		ReadOnly:     readOnly,
		AllowHosts:   splitList(hosts),
		AllowActions: splitList(allowed),
		Keeps:        actions.Keeps(),
	}, nil
}

// splitList splits a comma-separated list, leaving out empty entries.
func splitList(s string) []string {
	var out []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// runCheck prints what is wrong or weak in a workflow, without running it.
// It exits with 2 when it finds an error, and with 0 when it finds only
// warnings or nothing.
func (c *Cmd) runCheck() int {
	if len(c.SubCommandArgs) != 1 {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] workflow file is required\n")
		_, _ = fmt.Fprintf(c.errWriter, "Usage: probe check <workflow-file>\n")
		return probe.ExitConfigError
	}

	findings, err := probe.Check(c.SubCommandArgs[0], probe.CheckOptions{Actions: actions.Names(), Params: actions.AllParams(), Contracts: actions.Contracts(), Manifest: actionref.ReadManifest})
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}

	errs, warnings := 0, 0
	for _, f := range findings {
		_, _ = fmt.Fprintln(c.outWriter, f.String())
		if f.Severity == probe.SeverityError {
			errs++
		} else {
			warnings++
		}
	}
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(c.outWriter, "No problems found")
		return 0
	}
	_, _ = fmt.Fprintf(c.outWriter, "\n%s, %s\n", plural(errs, "error"), plural(warnings, "warning"))
	if errs > 0 {
		return probe.ExitConfigError
	}
	return 0
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// runCoverage prints which operations and responses of an OpenAPI
// document, or methods of a .proto file, the steps of a run checked, as its
// JSON report records them.
func (c *Cmd) runCoverage() int {
	if len(c.SubCommandArgs) != 2 {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] an OpenAPI spec file or a .proto file, and a report file, are required\n")
		_, _ = fmt.Fprintf(c.errWriter, "Usage: probe coverage <openapi-file|proto-file> <report-file>\n")
		return probe.ExitConfigError
	}

	r, err := oas.ReadReport(c.SubCommandArgs[1])
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	// A .proto file is the contract of the grpc action, and any other the
	// OpenAPI document of the http action.
	newCoverage := oas.NewCoverage
	if strings.HasSuffix(c.SubCommandArgs[0], ".proto") {
		newCoverage = grpc.NewCoverage
	}
	cov, err := newCoverage(c.SubCommandArgs[0], r)
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	if err := cov.Write(c.outWriter); err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	return 0
}

// runGuide prints the topic list, or the page for the topic given.
func (c *Cmd) runGuide() int {
	if len(c.SubCommandArgs) == 0 {
		_, _ = fmt.Fprint(c.outWriter, probe.GuideIndex())
		return 0
	}

	page, err := probe.Guide(c.SubCommandArgs[0])
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	_, _ = fmt.Fprint(c.outWriter, page)
	return 0
}

// runSkill prints the agent skill, or installs it with "install [dir]".
func (c *Cmd) runSkill() int {
	args := c.SubCommandArgs
	switch {
	case len(args) == 0:
		_, _ = fmt.Fprint(c.outWriter, probe.Skill())
		return 0
	case args[0] == "install" && len(args) <= 2:
		dir := ""
		if len(args) == 2 {
			dir = args[1]
		}
		if c.mocking && dir == "" {
			return 0
		}
		path, err := probe.InstallSkill(dir)
		if err != nil {
			_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
			return probe.ExitConfigError
		}
		_, _ = fmt.Fprintf(c.outWriter, "Wrote %s\n", path)
		return 0
	default:
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] unknown skill arguments: %s\n", strings.Join(args, " "))
		_, _ = fmt.Fprintf(c.errWriter, "Usage: probe skill [install [dir]]\n")
		return probe.ExitConfigError
	}
}

// runManifest prints the action.yml of a release of an external action,
// from the one of the release before it and the checksums of its
// executables, or, with "init <owner>/<repo>", one to start a new action
// from.
func (c *Cmd) runManifest() int {
	args := c.SubCommandArgs
	if len(args) > 0 && args[0] == "init" {
		if len(args) != 2 {
			_, _ = fmt.Fprintf(c.errWriter, "[ERROR] a repository is required\n")
			_, _ = fmt.Fprintf(c.errWriter, "Usage: probe manifest init <owner>/<repo>\n")
			return probe.ExitConfigError
		}
		out, err := actionref.Scaffold(args[1])
		if err != nil {
			_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
			return probe.ExitConfigError
		}
		_, _ = c.outWriter.Write(out)
		return 0
	}
	if len(args) < 2 || len(args) > 3 {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] a tag and a checksums file are required\n")
		_, _ = fmt.Fprintf(c.errWriter, "Usage: probe manifest init <owner>/<repo>\n")
		_, _ = fmt.Fprintf(c.errWriter, "       probe manifest <tag> <checksums-file> [action-file]\n")
		return probe.ExitConfigError
	}
	actionFile := actionref.ManifestFile
	if len(args) == 3 {
		actionFile = args[2]
	}

	manifest, err := os.ReadFile(actionFile)
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	checksums, err := os.Open(args[1])
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	defer func() { _ = checksums.Close() }()

	out, err := actionref.Release(manifest, args[0], checksums)
	if err != nil {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] %v\n", err)
		return probe.ExitConfigError
	}
	_, _ = c.outWriter.Write(out)
	return 0
}

func (c *Cmd) printVersion() {
	_, _ = fmt.Fprintf(c.outWriter, "Probe Version %s (commit: %s)\n", c.ver, c.rev)
}

func (c *Cmd) runBuiltinActions(name string) {
	serve, ok := actions.Lookup(name)
	if !ok {
		_, _ = fmt.Fprintf(c.errWriter, "[ERROR] not supported plugin: %s\n", name)
		return
	}

	if !c.mocking {
		serve()
	}
}
