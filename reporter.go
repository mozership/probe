package probe

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
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

// Reporter decides when the parts of a workflow report reach the user.
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
	r.printer.Fprint(r.printer.outWriter, r.render.generateHeader(r.name, r.description)+r.render.GenerateReport(rs))
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

	mu            sync.Mutex
	labels        map[string]string
	labelWidth    int
	cursor        int
	ready         map[string]*JobResult
	succeeded     int
	earliestStart time.Time
	latestEnd     time.Time
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
		w = max(w, displayWidth(l))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.labels, r.labelWidth = labels, w
}

func (r *StreamReporter) Start(name, description string) {
	r.printer.Fprint(r.printer.outWriter, r.render.generateHeader(name, description))
}

func (r *StreamReporter) StepStart(jobID, stepName string) {
	r.progress(jobID, colorDim().Sprint("▸"), stepName, "")
}

func (r *StreamReporter) StepDone(jobID string, sr StepResult) {
	var icon string
	switch sr.Status {
	case StatusSuccess:
		icon = colorSuccess().Sprint(strings.TrimSpace(IconSuccess))
	case StatusError:
		icon = colorError().Sprint(strings.TrimSpace(IconError))
	case StatusWarning:
		icon = colorNoTest().Sprint(strings.TrimSpace(IconTriangle))
	case StatusSkipped:
		icon = colorInfo().Sprint(strings.TrimSpace(IconSkip))
	default:
		icon = colorWarning().Sprint("?")
	}

	suffix := ""
	if sr.RepeatCounter != nil {
		suffix = colorDim().Sprintf(" (%d/%d success)",
			sr.RepeatCounter.SuccessCount, sr.RepeatCounter.RepeatTotal)
	} else if sr.RT != "" {
		suffix = colorDim().Sprintf(" (%s)", sr.RT)
	}

	r.progress(jobID, icon, sr.Name, suffix)
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

	var totalTime float64
	if !r.earliestStart.IsZero() && !r.latestEnd.IsZero() {
		totalTime = r.latestEnd.Sub(r.earliestStart).Seconds()
	}

	var output strings.Builder
	r.render.generateFooter(totalTime, r.succeeded, len(rs.Jobs), &output)
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
	block, succeeded, start, end := r.render.generateJobReport(jr)
	if succeeded {
		r.succeeded++
	}
	if r.earliestStart.IsZero() || start.Before(r.earliestStart) {
		r.earliestStart = start
	}
	if r.latestEnd.IsZero() || end.After(r.latestEnd) {
		r.latestEnd = end
	}
	r.printer.Fprint(r.printer.outWriter, block)
}

// progress writes one live line to stderr. The report on stdout only grows a
// whole job at a time, so these lines are what show which step is running now.
// Jobs run at once, so a line is labelled by its job, and the labels are
// padded to one width, so that the steps line up in a log.
func (r *StreamReporter) progress(jobID, icon, text, suffix string) {
	ts := colorDim().Sprint(time.Now().Format("15:04:05.000"))

	r.mu.Lock()
	defer r.mu.Unlock()
	label, ok := r.labels[jobID]
	if !ok {
		label = jobID
	}
	job := colorDim().Sprintf("[%s]", label)
	if pad := r.labelWidth - displayWidth(label); pad > 0 {
		job += strings.Repeat(" ", pad)
	}
	r.printer.Fprintf(r.printer.errWriter, "%s %s %s %s%s\n", ts, job, icon, text, suffix)
}
