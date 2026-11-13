package probe

import (
	"maps"
	"slices"
	"sync"

	"github.com/mozership/probe/internal/notes"
	"github.com/mozership/probe/mask"
)

// noteBook keeps the notes the actions of a run left for people, by the
// job and the step they come from, for the steps after them to read as
// notes. A step's notes are kept once its result is final: when it is
// over, or for a step of a repeated job, when every run of the job is.
type noteBook struct {
	// order is the ids of the jobs in the order of the workflow, which the
	// notes follow whatever order the jobs end in.
	order []string
	// masker returns the masker the notes are shown with, which can
	// change and learn as the run goes.
	masker func() *mask.Masker

	mu      sync.Mutex
	byJob   map[string][]noteEntry
	version int
	// cache is the notes as snapshot last made them, of version and of
	// the masker at maskGen, kept until either changes.
	cache     []map[string]any
	cacheOf   int
	cacheMask *mask.Masker
	maskGen   uint64
}

// noteEntry is a note with what names where it comes from.
type noteEntry struct {
	jobID  string
	job    string
	step   string
	index  int
	status string
	// run is the run of a repeated job the note is of, from 1; 0 for a
	// job that is not repeated.
	run  int
	note notes.Note
}

// newNoteBook returns a book of the notes of the jobs order names, which
// shows them masked with what masker returns.
func newNoteBook(order []string, masker func() *mask.Masker) *noteBook {
	return &noteBook{order: order, masker: masker, byJob: map[string][]noteEntry{}, cacheOf: -1}
}

// record keeps the notes of sr, the final result of a step of the job of
// jobID, called job. A skipped step has none.
func (b *noteBook) record(jobID, job string, sr StepResult) {
	if b == nil || len(sr.notes) == 0 {
		return
	}
	status := reportStatus(sr)
	run := 0
	if sr.RepeatCounter != nil {
		run = sr.RepeatCounter.notesRun
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for _, n := range sr.notes {
		b.byJob[jobID] = append(b.byJob[jobID], noteEntry{
			jobID: jobID, job: job, step: sr.Name, index: sr.Index,
			status: status, run: run, note: n,
		})
	}
	b.version++
}

// snapshot returns the notes kept so far, as the notes of a step's context
// hold them: a list with an entry for each, in the order of the jobs, the
// steps of a job and the notes of a step, with the secrets in their title
// and body hidden. It is never nil, so that a step reads an empty list
// rather than nil when there are none.
//
// Each call returns entries of its own, which a step can change without
// changing those another step reads.
func (b *noteBook) snapshot() []any {
	if b == nil {
		return []any{}
	}
	m := b.masker()
	gen := m.Generation()

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cacheOf != b.version || b.cacheMask != m || b.maskGen != gen {
		b.cache = b.render(m)
		b.cacheOf, b.cacheMask, b.maskGen = b.version, m, gen
	}

	out := make([]any, len(b.cache))
	for i, e := range b.cache {
		out[i] = maps.Clone(e)
	}
	return out
}

// render makes the entries of the notes kept, masked with m. Callers must
// hold mu.
func (b *noteBook) render(m *mask.Masker) []map[string]any {
	var out []map[string]any
	seen := make(map[string]bool, len(b.order))
	add := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, e := range b.byJob[id] {
			entry := map[string]any{
				"job":    m.String(e.job),
				"job_id": e.jobID,
				"step":   m.String(e.step),
				"index":  e.index,
				"status": e.status,
				"level":  string(e.note.Level),
				"title":  m.String(e.note.Title),
				"body":   m.String(e.note.Body),
			}
			if e.run > 0 {
				entry["run"] = e.run
			}
			out = append(out, entry)
		}
	}
	for _, id := range b.order {
		add(id)
	}
	// A job missing from the order, which a run does not have, is put
	// after the others rather than left out, in the order of its id so
	// that every snapshot has the same order.
	for _, id := range slices.Sorted(maps.Keys(b.byJob)) {
		add(id)
	}
	return out
}
