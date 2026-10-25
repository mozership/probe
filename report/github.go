package report

import (
	"errors"
	"fmt"
	"os"

	"github.com/mozership/probe/internal/safefile"
)

// maxStepSummaryBytes is the most GitHub accepts in one step's job summary.
// A summary over it is rejected as a whole, so Probe keeps under it.
const maxStepSummaryBytes = 1024 * 1024

// ErrNoStepSummary is returned for a github-summary report when no path was
// given and GITHUB_STEP_SUMMARY is not set, as on a machine outside GitHub
// Actions. It is not a failure: the same command line can run anywhere.
var ErrNoStepSummary = errors.New("GITHUB_STEP_SUMMARY is not set; skipping the github-summary report")

// ErrStepSummaryFull is returned when earlier writes have left no room in the
// job summary for even a shortened page. Writing anyway would push it past
// GitHub's limit and lose what the earlier steps wrote, so nothing is written.
var ErrStepSummaryFull = errors.New("the job summary has no room left under GitHub's 1 MiB limit; skipping the github-summary report")

// stepSummaryWriter appends the Markdown page to the job summary file. It
// appends because earlier steps, and other tools in the same step, write to
// the same file. When the page would push the file past GitHub's limit, it
// leaves out the requests and responses, and if that is still too large it
// cuts the page short and says so.
type stepSummaryWriter struct{}

func (stepSummaryWriter) write(r *Report, path string) error {
	// An explicit path can sit in a checked-out project, so it is kept inside
	// the current directory the way report files are. The path GitHub Actions
	// provides in GITHUB_STEP_SUMMARY is trusted and used as given.
	confine := path != ""
	if !confine {
		path = os.Getenv("GITHUB_STEP_SUMMARY")
	}
	if path == "" {
		return ErrNoStepSummary
	}

	f, existing, err := safefile.OpenForAppend(path, confine)
	if err != nil {
		return fmt.Errorf("failed to open the job summary %s: %w", path, err)
	}

	// A blank line separates this page from whatever the file already holds,
	// and it counts against the limit too.
	sep := ""
	if existing > 0 {
		sep = "\n"
	}
	page := fitStepSummary(r, maxStepSummaryBytes-int(existing)-len(sep))
	if page == "" {
		_ = f.Close()
		return ErrStepSummaryFull
	}

	_, err = f.WriteString(sep + page)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("failed to write the job summary %s: %w", path, err)
	}
	return nil
}

const (
	payloadsOmittedNote = "\n_Requests and responses are left out to keep the job summary within GitHub's size limit. Write a json or markdown report to keep them._\n"
	truncatedNote       = "\n_The job summary was cut short to stay within GitHub's size limit. Write a json or markdown report for the full result._\n"
)

// fitStepSummary renders the page so that it is at most budget bytes, or
// returns an empty string when not even a shortened page fits.
func fitStepSummary(r *Report, budget int) string {
	if page := r.markdown(true); len(page) <= budget {
		return page
	}
	page, cuts := r.markdownWithCuts(false)
	if len(page)+len(payloadsOmittedNote) <= budget {
		return page + payloadsOmittedNote
	}

	// Keep everything before the last cut point that leaves room for the
	// note. Cut points fall between blocks, so no code fence is left open.
	limit := budget - len(truncatedNote)
	end := -1
	for _, c := range cuts {
		if c <= limit && c <= len(page) {
			end = c
		}
	}
	if end <= 0 {
		return ""
	}
	return page[:end] + truncatedNote
}
