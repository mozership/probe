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
	"bytes"
	"slices"
	"strings"
	"unicode"

	"github.com/mozership/probe/mask"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// gfm reads Markdown as CommonMark with the tables of GitHub Flavored
// Markdown, which GitHub and the markdown blocks of Slack both render. A
// table row is cut into its cells before their text is read, so a code
// span does not go on from one cell to the next as it would in a paragraph.
var gfm = goldmark.New(goldmark.WithExtensions(extension.Table)).Parser()

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
// CommonMark parser finds it, with its blocks, quotes, lists and headings,
// and the tables of GitHub Flavored Markdown.
//
// A backslash right before a placeholder is escaped, so that it does not
// escape what is written in its place. With AsEntity, what the placeholder
// names is written as text, so that a & or a * in the name of a secret is
// not read as an entity or as emphasis. With AsCode, a placeholder is put
// in a span of a number of backticks no run of s has, so that no backtick
// left unmatched before it can close the span, and it is kept apart by a
// space from a backtick or another placeholder right next to it, whose
// backticks would make one run with those of its span. In a table row, a |
// of the name is escaped too, so that it does not end the cell.
func Escape(s string, style Style) string {
	found := mask.Placeholder.FindAllStringIndex(s, -1)
	if found == nil {
		return s
	}
	// The text is parsed as it is written out, where a | of a name ends no
	// cell of a table: each is read as a letter, which keeps every offset.
	src := []byte(s)
	for _, m := range found {
		for i := m[0]; i < m[1]; i++ {
			if src[i] == '|' {
				src[i] = 'x'
			}
		}
	}
	code, rows := parse(src)
	delim := strings.Repeat("`", unusedRun(s))

	var b strings.Builder
	last := 0
	prevEnd := -1
	// next is the first code span that does not end before the placeholder;
	// the placeholders and the spans are both in order, so it only moves on.
	next := 0
	// row is the first table row that does not end before the
	// placeholder, which moves on as next does.
	row := 0
	for _, m := range found {
		for next < len(code) && code[next].end < m[1] {
			next++
		}
		for row < len(rows) && rows[row].end < m[1] {
			row++
		}
		inTable := row < len(rows) && rows[row].start <= m[0]
		label := s[m[0]:m[1]]
		if next < len(code) && code[next].start <= m[0] {
			// A placeholder in code shows as it is. In a table, which has
			// no code block, the code is a span, and a | of the name would
			// end the cell, and the span with it.
			if inTable && strings.Contains(label, "|") {
				b.WriteString(s[last:m[0]] + escapePipes(label, false))
				last = m[1]
			}
			continue
		}
		before := s[last:m[0]]
		if trailingBackslashes(s[:m[0]])%2 == 1 {
			before += `\`
		}
		if style == AsEntity {
			// literal escapes a | as it does all punctuation.
			b.WriteString(before + "&lt;" + literal(label[1:len(label)-1]) + "&gt;")
			last = m[1]
			continue
		}
		if (m[0] > 0 && s[m[0]-1] == '`') || m[0] == prevEnd {
			before += " "
		}
		if inTable {
			label = escapePipes(label, true)
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

// parse returns the spans of src that are code, the lines of code blocks,
// fenced or indented, and the text of code spans, and the spans of the
// rows of its tables, each in order.
func parse(src []byte) (code, rows []span) {
	doc := gfm.Parse(text.NewReader(src))
	add := func(lines *text.Segments) {
		for i := range lines.Len() {
			seg := lines.At(i)
			code = append(code, span{seg.Start, seg.Stop})
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
		case *east.TableHeader, *east.TableRow:
			// A row is from the start of its first cell.
			row := span{-1, -1}
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				lines := c.Lines()
				for i := range lines.Len() {
					seg := lines.At(i)
					if row.start < 0 || seg.Start < row.start {
						row.start = seg.Start
					}
					row.end = max(row.end, seg.Stop)
				}
			}
			if row.start >= 0 {
				// A row is one line, and goes on to its end, past the
				// cells of more than the columns of the header, which are
				// dropped.
				if i := bytes.IndexByte(src[row.end:], '\n'); i >= 0 {
					row.end += i
				} else {
					row.end = len(src)
				}
				rows = append(rows, row)
			}
		case *ast.CodeSpan:
			// The span is from the start of its first text to the end of
			// its last. In a table, its text is cut at a \| to leave the
			// backslash out, and the span goes on over it.
			cs := span{-1, -1}
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					if cs.start < 0 {
						cs.start = t.Segment.Start
					}
					cs.end = t.Segment.Stop
				}
			}
			if cs.start >= 0 {
				code = append(code, cs)
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	byStart := func(a, b span) int { return a.start - b.start }
	slices.SortFunc(code, byStart)
	slices.SortFunc(rows, byStart)
	return code, rows
}

// escapePipes escapes the | of s for a table, which reads a | right after
// a backslash as a | of the cell, and leaves that backslash out, in a code
// span too, however many backslashes come before it. In a code span the
// action writes, every | is escaped, so that the name shows as written,
// a backslash before a | of it among it. In one that s is in already, a |
// right after a backslash is a | of the cell as it is, and only the others
// are escaped, so that what the span shows does not change.
func escapePipes(s string, every bool) string {
	var b strings.Builder
	for i := range len(s) {
		if s[i] == '|' && (every || i == 0 || s[i-1] != '\\') {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
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
// and an underscore between two letters or digits no emphasis, so the names
// of secrets as they are mostly written, such as secret:API_TOKEN, read as
// written.
func literal(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == ':' || r >= 0x80:
			b.WriteRune(r)
		case r == '_' && i > 0 && i < len(name)-1 && alnum(name[i-1]) && alnum(name[i+1]):
			// An underscore between two letters or digits opens and
			// closes no emphasis.
			b.WriteRune(r)
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			b.WriteString(`\` + string(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func alnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}
