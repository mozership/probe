package report

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTargets_GitHubSummary(t *testing.T) {
	got, err := ParseTargets("github-summary,markdown")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Format != GitHubSummary || got[0].Path != "" {
		t.Errorf("github-summary target = %+v, want an empty path resolved at write time", got[0])
	}

	got, err = ParseTargets("github-summary=out/summary.md")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Path != "out/summary.md" {
		t.Errorf("path = %q, want the one given", got[0].Path)
	}
}

func TestReport_WriteGitHubSummary_Appends(t *testing.T) {
	r := newTestReport()
	path := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(path, []byte("## Earlier step\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := range 2 {
		if err := r.Write(Target{Format: GitHubSummary, Path: path}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.HasPrefix(out, "## Earlier step\n\n# Demo\n") {
		t.Errorf("the page should follow earlier content after a blank line, got:\n%.80s", out)
	}
	if n := strings.Count(out, "# Demo\n"); n != 2 {
		t.Errorf("found the page %d times, want 2 (appended, not overwritten)", n)
	}
	if !strings.Contains(out, "<details><summary>Response</summary>") {
		t.Error("a summary within the limit should keep requests and responses")
	}
}

func TestReport_WriteGitHubSummary_FromEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "step_summary")
	t.Setenv("GITHUB_STEP_SUMMARY", path)

	if err := newTestReport().Write(Target{Format: GitHubSummary}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# Demo\n") {
		t.Errorf("a new summary should start with the page, got:\n%.80s", data)
	}
}

func TestReport_WriteGitHubSummary_NoEnv(t *testing.T) {
	t.Setenv("GITHUB_STEP_SUMMARY", "")

	err := newTestReport().Write(Target{Format: GitHubSummary})
	if !errors.Is(err, ErrNoStepSummary) {
		t.Errorf("error = %v, want ErrNoStepSummary", err)
	}
}

func TestReport_WriteGitHubSummary_Unwritable(t *testing.T) {
	err := newTestReport().Write(Target{Format: GitHubSummary, Path: t.TempDir()})
	if err == nil || errors.Is(err, ErrNoStepSummary) {
		t.Errorf("error = %v, want a write failure", err)
	}
}

func TestReport_MarkdownWithoutPayloads(t *testing.T) {
	r := newTestReport()
	full := r.markdown(true)
	compact := r.markdown(false)

	if !strings.Contains(full, "<details>") {
		t.Fatal("the full page should carry the request and response")
	}
	if strings.Contains(compact, "<details>") {
		t.Error("the compact page should leave out the request and response")
	}
	if !strings.Contains(compact, "### API / 1. Create\n\nassertion: test evaluated to false\n\n```\nres.code == 201\n```\n") {
		t.Errorf("the compact page should keep the failure and its test, got:\n%s", compact)
	}

	var buf bytes.Buffer
	if err := r.WriteMarkdown(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != full {
		t.Error("WriteMarkdown should write the full page")
	}
}

func TestFitStepSummary(t *testing.T) {
	r := newTestReport()
	full := r.markdown(true)
	compact := r.markdown(false)

	t.Run("fits as is", func(t *testing.T) {
		if got := fitStepSummary(r, len(full)); got != full {
			t.Error("a page within the budget should be written unchanged")
		}
	})

	t.Run("drops payloads", func(t *testing.T) {
		got := fitStepSummary(r, len(full)-1)
		if got != compact+payloadsOmittedNote {
			t.Errorf("got:\n%s", got)
		}
	})

	t.Run("cuts at a section", func(t *testing.T) {
		budget := len(compact) + len(payloadsOmittedNote) - 1
		got := fitStepSummary(r, budget)
		if len(got) > budget {
			t.Errorf("page is %d bytes, over the budget of %d", len(got), budget)
		}
		if !strings.HasSuffix(got, truncatedNote) {
			t.Errorf("a cut page should say so, got:\n%s", got)
		}
		body := strings.TrimSuffix(got, truncatedNote)
		if strings.Count(body, "```")%2 != 0 {
			t.Errorf("the cut left a code fence open:\n%s", body)
		}
		if !strings.HasPrefix(compact, body) {
			t.Error("a cut page should be a prefix of the compact page")
		}
	})

	t.Run("no room", func(t *testing.T) {
		if got := fitStepSummary(r, 10); got != "" {
			t.Errorf("with no room the page should be empty, got %q", got)
		}
	})
}

// TestFitStepSummary_HeadingInsideFence covers a test and a message that
// contain lines looking like a failure heading. The page must only be cut at
// the boundaries the renderer recorded, never at one of those lines.
func TestFitStepSummary_HeadingInsideFence(t *testing.T) {
	failed := func(i int) Step {
		return Step{
			Index:  i,
			Name:   fmt.Sprintf("step %d", i),
			Status: Failed,
			Test:   "res.code == 200 &&\n### not a heading\nres.body != \"\"",
			Failure: &Failure{
				Kind:    FailureTestError,
				Message: "cannot evaluate\n### also not a heading",
			},
		}
	}
	r := &Report{
		Name:   "Fenced",
		Status: Failed,
		Jobs: []Job{{
			Name:   "job",
			Status: Failed,
			Steps:  []Step{failed(0), failed(1), failed(2)},
		}},
	}

	page, cuts := r.markdownWithCuts(false)
	for budget := len(truncatedNote) + 1; budget < len(page)+len(payloadsOmittedNote); budget++ {
		got := fitStepSummary(r, budget)
		if got == "" {
			continue
		}
		if len(got) > budget {
			t.Fatalf("budget %d: page is %d bytes", budget, len(got))
		}
		body := strings.TrimSuffix(got, truncatedNote)
		if body == got {
			continue // written whole, with only the payloads note
		}
		if strings.Count(body, "```")%2 != 0 {
			t.Fatalf("budget %d: the cut left a code fence open:\n%s", budget, body)
		}
		onCut := false
		for _, c := range cuts {
			if c == len(body) {
				onCut = true
			}
		}
		if !onCut {
			t.Fatalf("budget %d: cut at %d, which is not a recorded boundary %v", budget, len(body), cuts)
		}
	}
}

func TestReport_WriteGitHubSummary_CreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out", "new", "summary.md")
	if err := newTestReport().Write(Target{Format: GitHubSummary, Path: path}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Errorf("summary not written: %v", err)
	}
}

func TestReport_WriteGitHubSummary_Full(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxStepSummaryBytes), 0o644); err != nil {
		t.Fatal(err)
	}

	err := newTestReport().Write(Target{Format: GitHubSummary, Path: path})
	if !errors.Is(err, ErrStepSummaryFull) {
		t.Errorf("error = %v, want ErrStepSummaryFull", err)
	}
	if info, _ := os.Stat(path); info.Size() != maxStepSummaryBytes {
		t.Errorf("size = %d, want it left at %d; not even the separator may be added", info.Size(), maxStepSummaryBytes)
	}
}

func TestReport_WriteGitHubSummary_StaysUnderLimit(t *testing.T) {
	r := newTestReport()
	compact := r.markdown(false)

	// Leave less room than the compact page needs, so the page is cut.
	existing := maxStepSummaryBytes - len(compact) - 1
	path := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := r.Write(Target{Format: GitHubSummary, Path: path}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Size() > maxStepSummaryBytes {
		t.Errorf("summary is %d bytes, over the %d limit", info.Size(), maxStepSummaryBytes)
	}
	if info.Size() == int64(existing) {
		t.Error("a shortened page should still have been written")
	}
}

func TestReport_WriteGitHubSummary_RefusesSymlinkedPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(target, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "summary.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := newTestReport().Write(Target{Format: GitHubSummary, Path: link})
	if err == nil || !strings.Contains(err.Error(), "path escapes") {
		t.Errorf("error = %v, want the link out of its directory refused", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "keep me" {
		t.Errorf("the file the link pointed at was changed to %.40q", data)
	}

	// The path GitHub Actions hands over is trusted, link or not.
	t.Setenv("GITHUB_STEP_SUMMARY", link)
	if err := newTestReport().Write(Target{Format: GitHubSummary}); err != nil {
		t.Errorf("GITHUB_STEP_SUMMARY should be used as given: %v", err)
	}
}
