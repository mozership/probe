package mask

import (
	"regexp"
	"strings"
)

// Placeholder matches what a Masker writes in place of a value it hides:
// <secret:NAME> for a secret of the workflow, and <redacted> for a
// credential it learned.
var Placeholder = regexp.MustCompile(`<(?:redacted|secret:[^<>\s]+)>`)

// MarkdownEscape is how EscapeMarkdown writes a placeholder.
type MarkdownEscape int

const (
	// EscapeAsEntity writes a placeholder with HTML entities, as
	// &lt;secret:NAME&gt;, for Markdown that takes HTML, such as that of
	// GitHub, which would take a placeholder for a tag and leave it out.
	EscapeAsEntity MarkdownEscape = iota
	// EscapeAsCode puts a placeholder in a code span, for Markdown that
	// reads <...> as something else, such as that of Slack, which takes it
	// for a link or a mention.
	EscapeAsCode
)

// fence opens or closes a fenced code block: three or more backticks or
// tildes, indented by at most three spaces, and what follows them.
var fence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")

// EscapeMarkdown returns s, a Markdown text such as the body of a note,
// with each placeholder outside code written as how says, so that a reader
// sees where a value was hidden. In a fenced code block or a code span a
// placeholder is shown as it is, so it is left alone there.
//
// A code span can go on over the lines of a paragraph, and opens and closes
// with runs of backticks of the same length, not escaped by a backslash; a
// run with no such closer is text. A backslash right before a placeholder
// is escaped, so that it does not escape what is written in its place. With
// EscapeAsCode, a placeholder is put in a span of more backticks than any
// run of its paragraph, so that no run left open before it can close the
// span, and it is kept apart by a space from backticks right next to it,
// which would make one run with those of its span.
func EscapeMarkdown(s string, how MarkdownEscape) string {
	lines := strings.Split(s, "\n")
	// open is the fence of the code block the line is in, or empty.
	var open string
	// para is where the paragraph the line is in starts, or -1.
	para := -1
	flush := func(end int) {
		if para >= 0 {
			text := escapeParagraph(strings.Join(lines[para:end], "\n"), how)
			copy(lines[para:end], strings.Split(text, "\n"))
			para = -1
		}
	}
	for i, line := range lines {
		if m := fence.FindStringSubmatch(line); m != nil {
			switch {
			case open == "" && (m[1][0] != '`' || !strings.Contains(m[2], "`")):
				flush(i)
				open = m[1]
				continue
			case open != "" && m[1][0] == open[0] && len(m[1]) >= len(open) && strings.TrimSpace(m[2]) == "":
				open = ""
				continue
			}
		}
		if open != "" {
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush(i)
			continue
		}
		if para < 0 {
			para = i
		}
	}
	flush(len(lines))
	return strings.Join(lines, "\n")
}

// escapeParagraph escapes the placeholders of a paragraph outside its code
// spans.
func escapeParagraph(p string, how MarkdownEscape) string {
	delim := strings.Repeat("`", longestRun(p)+1)
	var b strings.Builder
	text := 0
	// escape escapes the text from text to end, which a code span may close
	// right before it and open right after it.
	escape := func(end int) string {
		return escapeText(p[text:end], how, delim, text > 0 && p[text-1] == '`', end < len(p) && p[end] == '`')
	}
	for i := 0; i < len(p); {
		if p[i] == '\\' {
			// A backslash makes the character after it text, so an escaped
			// backtick opens no code span.
			i += 2
			continue
		}
		if p[i] != '`' {
			i++
			continue
		}
		n := ticks(p, i)
		end := closer(p, i+n, n)
		if end < 0 {
			i += n
			continue
		}
		b.WriteString(escape(i))
		b.WriteString(p[i : end+n])
		i, text = end+n, end+n
	}
	b.WriteString(escape(len(p)))
	return b.String()
}

// escapeText writes each placeholder of s, text outside code, as how says.
func escapeText(s string, how MarkdownEscape, delim string, tickBefore, tickAfter bool) string {
	var b strings.Builder
	last := 0
	for _, m := range Placeholder.FindAllStringIndex(s, -1) {
		before := s[last:m[0]]
		if trailingBackslashes(before)%2 == 1 {
			before += `\`
		}
		label := s[m[0]:m[1]]
		if how == EscapeAsEntity {
			b.WriteString(before + "&lt;" + label[1:len(label)-1] + "&gt;")
			last = m[1]
			continue
		}
		if (m[0] == 0 && tickBefore) || (m[0] > 0 && s[m[0]-1] == '`') {
			before += " "
		}
		b.WriteString(before + delim + label + delim)
		if (m[1] == len(s) && tickAfter) || (m[1] < len(s) && s[m[1]] == '`') {
			b.WriteString(" ")
		}
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

func trailingBackslashes(s string) int {
	n := 0
	for n < len(s) && s[len(s)-1-n] == '\\' {
		n++
	}
	return n
}

// longestRun is the length of the longest run of backticks in s.
func longestRun(s string) int {
	longest := 0
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		n := ticks(s, i)
		longest = max(longest, n)
		i += n
	}
	return longest
}

// ticks is the length of the run of backticks at i.
func ticks(s string, i int) int {
	n := 0
	for i+n < len(s) && s[i+n] == '`' {
		n++
	}
	return n
}

// closer is where the first run of exactly n backticks from i starts, or -1.
func closer(s string, i, n int) int {
	for i < len(s) {
		if s[i] != '`' {
			i++
			continue
		}
		run := ticks(s, i)
		if run == n {
			return i
		}
		i += run
	}
	return -1
}
