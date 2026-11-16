package probe

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/mozership/probe/internal/textwidth"
)

// OutputMode selects how a workflow run delivers its report.
type OutputMode int

const (
	// OutputModeAuto picks spinner on an interactive terminal and stream otherwise.
	OutputModeAuto OutputMode = iota
	// OutputModeSpinner shows a spinner while running and prints the whole
	// report once every job has finished.
	OutputModeSpinner
	// OutputModeStream prints each job block as soon as it is final and logs
	// step progress to stderr while jobs are still running.
	OutputModeStream
)

// ParseOutputMode converts a user supplied name into an OutputMode.
func ParseOutputMode(s string) (OutputMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return OutputModeAuto, nil
	case "spinner":
		return OutputModeSpinner, nil
	case "stream":
		return OutputModeStream, nil
	default:
		return OutputModeAuto, fmt.Errorf("unknown output mode: %s (expected auto, spinner or stream)", s)
	}
}

func (m OutputMode) String() string {
	switch m {
	case OutputModeSpinner:
		return "spinner"
	case OutputModeStream:
		return "stream"
	default:
		return "auto"
	}
}

// resolve turns auto into a concrete mode.
func (m OutputMode) resolve() OutputMode {
	if m != OutputModeAuto {
		return m
	}
	// A CI runner never renders the spinner, so the log stays silent until the
	// whole workflow finishes. Stream the report there instead.
	if os.Getenv("CI") != "" || !isatty.IsTerminal(os.Stdout.Fd()) {
		return OutputModeStream
	}
	return OutputModeSpinner
}

// Reporter decides when the parts of a workflow report reach the user, and
// on which of stdout and stderr; a reportRenderer renders them.
// Jobs run concurrently, so implementations are called from several
// goroutines and must serialize their own writes.
type Reporter interface {
	Start(name, description string)
	StepStart(jobID, stepName string)
	StepDone(jobID string, sr StepResult)
	JobDone(jobID string, jr *JobResult)
	Finish(rs *Result)
}

// jobLabeler is a Reporter that labels what it prints by job, and is told
// the names of the jobs once the vars they are evaluated with are known.
type jobLabeler interface {
	labelJobs(name func(id string) string)
}

// newReporter builds the reporter for the given mode.
func newReporter(mode OutputMode, p *Printer, render *reportRenderer) Reporter {
	if mode.resolve() == OutputModeStream {
		return newStreamReporter(p, render)
	}
	return newBufferedReporter(p, render)
}

// BufferedReporter keeps the original behaviour: a spinner during the run and
// the complete report at the end.
type BufferedReporter struct {
	printer     *Printer
	render      *reportRenderer
	name        string
	description string
}

// newBufferedReporter creates a reporter that prints the report render
// renders at the end.
func newBufferedReporter(p *Printer, render *reportRenderer) *BufferedReporter {
	return &BufferedReporter{printer: p, render: render}
}

func (r *BufferedReporter) Start(name, description string) {
	r.name = name
	r.description = description
	r.printer.StartSpinner()
}

func (r *BufferedReporter) StepStart(jobID, stepName string) {
	r.printer.AddSpinnerSuffix(stepName)
}

func (r *BufferedReporter) StepDone(jobID string, sr StepResult) {}

func (r *BufferedReporter) JobDone(jobID string, jr *JobResult) {}

func (r *BufferedReporter) Finish(rs *Result) {
	r.printer.StopSpinner()
	r.printer.Fprint(r.printer.outWriter, r.render.headerText(r.name, r.description)+r.render.reportText(rs))
}

// StreamReporter prints the report incrementally.
//
// A job block is only printed once the job is final, which keeps blocks from
// interleaving even though jobs run concurrently. Blocks are held back until
// every earlier job in workflow order has been printed, so the finished report
// is identical to the buffered one. Step level progress goes to stderr, which
// is what keeps a CI log alive while the first job is still running.
type StreamReporter struct {
	printer *Printer
	render  *reportRenderer
	order   []string
	index   map[string]int

	mu         sync.Mutex
	labels     map[string]string
	labelWidth int
	cursor     int
	ready      map[string]*JobResult
	// totals are those of the jobs printed so far, for the footer.
	totals reportTotals
}

// newStreamReporter creates a reporter that flushes job blocks, which render
// renders, as they finish.
func newStreamReporter(p *Printer, render *reportRenderer) *StreamReporter {
	index := make(map[string]int, len(render.order))
	for i, id := range render.order {
		index[id] = i
	}

	return &StreamReporter{
		printer: p,
		render:  render,
		order:   render.order,
		index:   index,
		ready:   make(map[string]*JobResult),
	}
}

// labelJobs labels the progress lines of each job by its name, which name
// gives for its id. The names are known once the vars they are evaluated
// with are, which is after Start. Until then a line is labelled by its id.
func (r *StreamReporter) labelJobs(name func(id string) string) {
	// A name is masked before it is cut short: the line is masked again
	// as it is written, but a secret cut short is no longer one the masker
	// finds, and the padding is for the label as it is shown.
	masker := r.printer.Masker()
	labels := jobLabels(r.order, func(id string) string {
		return masker.String(name(id))
	})
	w := 0
	for _, l := range labels {
		w = max(w, textwidth.Width(l))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.labels, r.labelWidth = labels, w
}

func (r *StreamReporter) Start(name, description string) {
	r.printer.Fprint(r.printer.outWriter, r.render.headerText(name, description))
}

func (r *StreamReporter) StepStart(jobID, stepName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.printer.Fprint(r.printer.errWriter, r.render.stepStartText(r.progressLineLocked(jobID), stepName))
}

func (r *StreamReporter) StepDone(jobID string, sr StepResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.printer.Fprint(r.printer.errWriter, r.render.stepDoneText(r.progressLineLocked(jobID), sr))
}

func (r *StreamReporter) JobDone(jobID string, jr *JobResult) {
	r.mu.Lock()
	defer r.mu.Unlock()

	i, known := r.index[jobID]
	if !known {
		// A job missing from the declared order cannot be placed, so print it
		// straight away rather than dropping it from the report.
		r.writeJobLocked(jr)
		return
	}
	if i < r.cursor {
		// Already printed; nothing left to flush for this job.
		return
	}

	r.ready[jobID] = jr
	r.flushLocked()
}

func (r *StreamReporter) Finish(rs *Result) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Pick up any job that finished without a JobDone notification so the
	// report is never short of a block.
	for i := r.cursor; i < len(r.order); i++ {
		id := r.order[i]
		if _, queued := r.ready[id]; queued {
			continue
		}
		if jr, exists := rs.Jobs[id]; exists {
			r.ready[id] = jr
		}
	}
	r.flushLocked()

	var output strings.Builder
	r.render.writeFooter(&output, r.totals, len(rs.Jobs))
	r.printer.Fprint(r.printer.outWriter, output.String())
}

// flushLocked prints every job block that is both ready and next in workflow
// order. Callers must hold r.mu.
func (r *StreamReporter) flushLocked() {
	for r.cursor < len(r.order) {
		id := r.order[r.cursor]
		jr, ok := r.ready[id]
		if !ok {
			break
		}
		delete(r.ready, id)
		r.cursor++
		r.writeJobLocked(jr)
	}
}

// writeJobLocked renders one job block and folds it into the footer totals.
// Callers must hold r.mu.
func (r *StreamReporter) writeJobLocked(jr *JobResult) {
	r.printer.Fprint(r.printer.outWriter, r.render.jobText(jr, &r.totals))
}

// progressLineLocked is where a line of the progress of jobID is, as it is
// written now: the label of the job and the width of the labels. The report
// on stdout only grows a whole job at a time, so these lines, on stderr, are
// what show which step is running now. Callers must hold r.mu.
func (r *StreamReporter) progressLineLocked(jobID string) progressLine {
	label, ok := r.labels[jobID]
	if !ok {
		label = jobID
	}
	return progressLine{at: time.Now(), label: label, width: r.labelWidth}
}
