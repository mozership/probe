// Package markdown writes the placeholders a mask.Masker puts in place of
// hidden values so that Markdown shows them, for an action that posts text
// with them, such as the notes of a run, where <secret:NAME> and
// <redacted> would be taken for something else: an HTML tag on GitHub,
// which leaves it out, or a link or a mention on Slack.
//
// It is a package of its own, apart from mask, which every action links
// through actionrpc, so that only an action that writes Markdown carries
// the Markdown parser it reads the text with.
package markdown

import (
	"slices"
	"strings"
	"unicode"

	"github.com/mozership/probe/mask"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Style is how Escape writes a placeholder.
type Style int

const (
	// AsEntity writes a placeholder with HTML entities, as
	// &lt;secret:NAME&gt;, for Markdown that takes HTML, such as that of
	// GitHub.
	AsEntity Style = iota
	// AsCode puts a placeholder in a code span, for Markdown that reads
	// <...> as something else, such as that of Slack.
	AsCode
)

// span is the bytes from start to end of a text.
type span struct{ start, end int }

// Escape returns s, a Markdown text such as the body of a note, with each
// placeholder outside code written as style says, so that a reader sees
// where a value was hidden. A placeholder in a code block or a code span is
// shown as it is, so it is left alone there. The code of s is found as a
// CommonMark parser finds it, with its blocks, quotes, lists and headings.
//
// A backslash right before a placeholder is escaped, so that it does not
// escape what is written in its place. With AsEntity, what the placeholder
// names is written as text, so that a & or a * in the name of a secret is
// not read as an entity or as emphasis. With AsCode, a placeholder is put
// in a span of a number of backticks no run of s has, so that no backtick
// left unmatched before it can close the span, and it is kept apart by a
// space from a backtick or another placeholder right next to it, whose
// backticks would make one run with those of its span.
func Escape(s string, style Style) string {
	found := mask.Placeholder.FindAllStringIndex(s, -1)
	if found == nil {
		return s
	}
	code := codeSpans([]byte(s))
	delim := strings.Repeat("`", unusedRun(s))

	var b strings.Builder
	last := 0
	prevEnd := -1
	for _, m := range found {
		if inside(code, m[0], m[1]) {
			continue
		}
		before := s[last:m[0]]
		if trailingBackslashes(s[:m[0]])%2 == 1 {
			before += `\`
		}
		label := s[m[0]:m[1]]
		if style == AsEntity {
			b.WriteString(before + "&lt;" + literal(label[1:len(label)-1]) + "&gt;")
			last = m[1]
			continue
		}
		if (m[0] > 0 && s[m[0]-1] == '`') || m[0] == prevEnd {
			before += " "
		}
		b.WriteString(before + delim + label + delim)
		if m[1] < len(s) && s[m[1]] == '`' {
			b.WriteString(" ")
		}
		last, prevEnd = m[1], m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// codeSpans returns the spans of src that are code: the lines of code
// blocks, fenced or indented, and the text of code spans.
func codeSpans(src []byte) []span {
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))
	var out []span
	add := func(lines *text.Segments) {
		for i := range lines.Len() {
			seg := lines.At(i)
			out = append(out, span{seg.Start, seg.Stop})
		}
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.FencedCodeBlock:
			add(n.Lines())
			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock:
			add(n.Lines())
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan:
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					out = append(out, span{t.Segment.Start, t.Segment.Stop})
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	slices.SortFunc(out, func(a, b span) int { return a.start - b.start })
	return out
}

// inside reports whether start to end is within one of spans.
func inside(spans []span, start, end int) bool {
	for _, c := range spans {
		if c.start <= start && end <= c.end {
			return true
		}
	}
	return false
}

func trailingBackslashes(s string) int {
	n := 0
	for n < len(s) && s[len(s)-1-n] == '\\' {
		n++
	}
	return n
}

// unusedRun is the shortest length of a run of backticks that s does not
// have. A code span closes only with a run of its own length, so a span of
// it is closed by nothing of s. s cannot have runs of every length up to n
// without being longer than n*(n+1)/2, so the length stays short whatever s
// holds.
func unusedRun(s string) int {
	used := map[int]bool{}
	n := 0
	for i := range len(s) + 1 {
		if i < len(s) && s[i] == '`' {
			n++
			continue
		}
		if n > 0 {
			used[n] = true
		}
		n = 0
	}
	for l := 1; ; l++ {
		if !used[l] {
			return l
		}
	}
}

// literal writes name, what a placeholder names, so that Markdown shows it
// as it is: a & as an entity, and ASCII punctuation, which Markdown may read
// as a part of its syntax, escaped with a backslash. A colon is no syntax,
// and an underscore inside a word no emphasis, so the names of secrets as
// they are mostly written, such as secret:API_TOKEN, read as written.
func literal(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == ':' || r == '_' || r >= 0x80:
			b.WriteRune(r)
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			b.WriteString(`\` + string(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
