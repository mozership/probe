// Package yamlfmt formats the YAML of a workflow, as gofmt formats Go: there
// is one way a workflow is laid out, and nothing to configure.
//
// It changes only the whitespace and the comments of a file, never what the
// file says: scalars are left as they are written, quotes and all.
//
//   - Indentation is two spaces, and a sequence is not indented under the key
//     it belongs to.
//   - A blank line separates the keys at the top of a file when either holds a
//     mapping or a sequence, and the jobs from one another. There is no other
//     blank line, apart from those a scalar holds.
//   - A comment on its own line is indented as the line it comes before, and
//     one at the end of a line is a single space from it. Each has a space
//     after its #.
//   - A key is a single space from its value, and a - from its entry.
//   - No line ends in whitespace, and the file ends in a single newline.
package yamlfmt

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// indentWidth is the number of spaces a level is indented by.
const indentWidth = 2

// jobsKey is the key at the top of a workflow whose entries are separated by
// a blank line.
const jobsKey = "jobs"

type lineKind int

const (
	// kindContent is a line that a key, an entry of a sequence or a scalar
	// starts on.
	kindContent lineKind = iota
	// kindBlank is a line with nothing on it, outside a scalar.
	kindBlank
	// kindComment is a line with only a comment on it.
	kindComment
	// kindCont is a line that goes on with a scalar or a flow collection
	// started on a line before it.
	kindCont
	// kindBody is a line of a block scalar.
	kindBody
)

type tok struct {
	typ   token.Type
	col   int // in runes, from 0
	value string
}

type line struct {
	text   string
	kind   lineKind
	indent int // the spaces it starts with
	toks   []tok

	// Of a content line.
	dashes  []int // the columns of the -s it starts with
	first   int   // the column of what follows them, or -1
	comment int   // the column of the comment it ends in, or -1
	marker  bool  // a document marker or a directive
	// header is set when the line ends in the header of a block scalar; the
	// body is the lines from bodyStart to bodyEnd, and keeps its indentation
	// relative to the parent of the scalar when bodyParent is set.
	header     bool
	bodyStart  int
	bodyEnd    int
	bodyParent bool
	explicit   bool // the header gives the indentation of the body
	headerCol  int
	headerLen  int

	// Of a comment line: the content line it is indented as, or -1 for the
	// block that the lines before it are in.
	alignTo int

	newIndent int
	blank     bool // a blank line comes before it
}

// Format returns src formatted. It returns an error when src is not YAML,
// and never returns YAML that means anything other than src does.
func Format(src []byte) ([]byte, error) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	want, err := canonical(text)
	if err != nil {
		return nil, err
	}

	lines := scan(text)
	indent(lines)
	separate(lines)
	out := render(lines)

	got, err := canonical(out)
	if err != nil || got != want {
		return nil, errors.New("formatting would change what the YAML means; it is left as it is")
	}
	return []byte(out), nil
}

// scan splits text into lines and tells what each is, from the tokens of
// the text.
func scan(text string) []*line {
	texts := strings.Split(text, "\n")
	if n := len(texts); texts[n-1] == "" {
		texts = texts[:n-1]
	}
	lines := make([]*line, len(texts))
	for i, t := range texts {
		lines[i] = &line{
			text:    t,
			indent:  len(t) - len(strings.TrimLeft(t, " ")),
			first:   -1,
			comment: -1,
			alignTo: -1,
		}
	}
	// at returns the line a token is on, by its number from 1.
	at := func(n int) *line {
		if n < 1 || n > len(lines) {
			return nil
		}
		return lines[n-1]
	}
	mark := func(from, to int, kind lineKind) {
		for n := from; n <= to; n++ {
			if l := at(n); l != nil {
				l.kind = kind
			}
		}
	}

	tokens := lexer.Tokenize(text)
	flow, flowStart := 0, 0
	body := map[int]bool{} // the tokens that are the body of a block scalar
	for i, tk := range tokens {
		n := tk.Position.Line
		l := at(n)
		if l == nil {
			continue
		}
		l.toks = append(l.toks, tok{typ: tk.Type, col: tk.Position.Column - 1, value: tk.Value})

		switch tk.Type {
		case token.SequenceStartType, token.MappingStartType:
			if flow == 0 {
				flowStart = n
			}
			flow++
		case token.SequenceEndType, token.MappingEndType:
			if flow > 0 {
				flow--
				if flow == 0 {
					mark(flowStart+1, n, kindCont)
				}
			}
		case token.LiteralType, token.FoldedType:
			// The body is the token after the header, past the comment
			// the header may end in, and it reaches the line before the
			// token that follows it.
			j := i + 1
			for j < len(tokens) && tokens[j].Type == token.CommentType && tokens[j].Position.Line == n {
				j++
			}
			if j >= len(tokens) || tokens[j].Type != token.StringType {
				break
			}
			body[j] = true
			end := len(lines)
			if j+1 < len(tokens) {
				end = tokens[j+1].Position.Line - 1
			}
			// The blank lines a body ends in are part of it only when
			// the header keeps them.
			if !strings.Contains(tk.Value, "+") {
				for end > n && strings.TrimSpace(lines[end-1].text) == "" {
					end--
				}
			}
			l.header = true
			l.bodyStart, l.bodyEnd = n+1, end
			l.explicit = strings.ContainsAny(tk.Value, "123456789")
			l.headerCol, l.headerLen = tk.Position.Column-1, len(tk.Value)
			// A body the text ends in without a newline does not end in
			// one, and says so once the text does.
			if end == len(lines) && !strings.HasSuffix(text, "\n") && strings.TrimSpace(lines[end-1].text) != "" {
				l.strip()
			}
			mark(n+1, end, kindBody)
		default:
			if body[i] || tk.Type == token.CommentType {
				break
			}
			// A scalar written over several lines.
			if more := strings.Count(strings.TrimSpace(tk.Origin), "\n"); more > 0 {
				mark(n+1, n+more, kindCont)
			}
		}
	}

	for _, l := range lines {
		if l.kind != kindContent {
			continue
		}
		switch {
		case strings.TrimSpace(l.text) == "":
			l.kind = kindBlank
		case len(l.toks) == 0:
			// Nothing starts on it, so it is left as it is.
			l.kind = kindCont
		case l.toks[0].typ == token.CommentType:
			l.kind = kindComment
		default:
			l.read()
		}
	}
	return lines
}

// read tells the parts of a content line from its tokens.
func (l *line) read() {
	toks := l.toks
	if last := toks[len(toks)-1]; last.typ == token.CommentType {
		l.comment = last.col
		toks = toks[:len(toks)-1]
	}
	switch toks[0].typ {
	case token.DocumentHeaderType, token.DocumentEndType, token.DirectiveType:
		l.marker = true
		l.first = toks[0].col
		return
	}
	for len(toks) > 0 && toks[0].typ == token.SequenceEntryType {
		l.dashes = append(l.dashes, toks[0].col)
		toks = toks[1:]
	}
	if len(toks) == 0 {
		return
	}
	l.first = toks[0].col
	// A block scalar with nothing before it on the line is indented from
	// what the line is in, rather than from a key of the line.
	if t := toks[0].typ; t == token.LiteralType || t == token.FoldedType {
		l.bodyParent = true
	}
}

// strip rewrites the header of the block scalar the line ends in, so that
// the scalar does not end in a newline.
func (l *line) strip() {
	at := byteAt(l.text, l.headerCol)
	if at+l.headerLen > len(l.text) {
		return
	}
	header := strings.NewReplacer("+", "", "-", "").Replace(l.text[at:at+l.headerLen]) + "-"
	l.text = l.text[:at] + header + l.text[at+l.headerLen:]
	// The comment the header may end in moves with it.
	for i := range l.toks {
		if l.toks[i].col > l.headerCol {
			l.toks[i].col += len(header) - l.headerLen
		}
	}
	l.headerLen = len(header)
}

// key returns the key a content line starts with, when it starts with one.
func (l *line) key() string {
	if l.kind != kindContent || l.marker || len(l.dashes) > 0 || l.first < 0 {
		return ""
	}
	return l.toks[0].value
}

// level is an open mapping or sequence: where it was indented to, and where
// it is indented to now.
type level struct {
	old, new int
	seq      bool
}

// indent gives each line the indentation it is to have.
func indent(lines []*line) {
	var stack []level
	top := func() *level { return &stack[len(stack)-1] }
	// delta is how far the lines that go on with the content line last read
	// are moved.
	delta := 0

	// next is the content line that follows each line.
	next := make([]int, len(lines))
	for i, n := len(lines)-1, -1; i >= 0; i-- {
		next[i] = n
		if lines[i].kind == kindContent {
			n = i
		}
	}

	for i, l := range lines {
		switch l.kind {
		case kindComment:
			if n := next[i]; n >= 0 && l.indent <= lines[n].indent {
				// It is a comment on what follows it.
				l.alignTo = n
				break
			}
			// It ends the block before it, at the level it is written at.
			for _, lv := range stack {
				if lv.old <= l.indent {
					l.newIndent = lv.new
				}
			}
		case kindCont:
			l.newIndent = max(l.indent+delta, 0)
		case kindContent:
			if l.marker {
				stack = nil
				delta = -l.indent
				break
			}
			seq := len(l.dashes) > 0
			o := l.indent
			for len(stack) > 0 && top().old > o {
				stack = stack[:len(stack)-1]
			}
			// A key at the indentation of a sequence ends the sequence,
			// which was the value of the key before it.
			if !seq && len(stack) > 0 && top().old == o && top().seq {
				stack = stack[:len(stack)-1]
			}
			// parentDelta is how far what holds the last part of the line
			// is moved, which a block scalar counts its indentation from
			// when the header gives it.
			n, parentDelta := 0, 0
			if len(stack) > 0 {
				parentDelta = top().new - top().old
			}
			switch {
			case len(stack) == 0:
				stack = append(stack, level{old: o, new: n, seq: seq})
			case top().old == o && top().seq == seq:
				n = top().new
				if len(stack) > 1 {
					parentDelta = stack[len(stack)-2].new - stack[len(stack)-2].old
				}
			case seq && !top().seq:
				// A sequence is not indented under its key.
				n = top().new
				stack = append(stack, level{old: o, new: n, seq: true})
			default:
				n = top().new + indentWidth
				stack = append(stack, level{old: o, new: n, seq: seq})
			}
			l.newIndent = n
			delta = n - o
			// parent is the column of what holds the last part of the
			// line, and n that of the part.
			parent := n - indentWidth

			// What follows a - on its line is a level of its own.
			cols := l.dashes
			if seq {
				cols = cols[1:]
				if l.first >= 0 {
					cols = append(append([]int{}, cols...), l.first)
				}
			} else {
				cols = nil
			}
			for j, col := range cols {
				parentDelta = delta
				parent = n
				n += indentWidth
				stack = append(stack, level{old: col, new: n, seq: j < len(l.dashes)-1})
				delta = n - col
			}

			if l.header {
				if l.bodyParent {
					n, delta = parent, parentDelta
				}
				indentBody(lines[l.bodyStart-1:l.bodyEnd], max(n, 0)+indentWidth, delta, l.explicit)
			}
		}
	}

	for i, l := range lines {
		if l.kind != kindComment {
			continue
		}
		if l.alignTo >= 0 {
			l.newIndent = lines[l.alignTo].newIndent
			continue
		}
		// One that ends a block where what follows starts reads as a
		// comment on what follows, and is taken as one.
		if n := next[i]; n >= 0 && l.newIndent == lines[n].newIndent {
			l.alignTo = n
		}
	}
}

// indentBody moves the lines of a block scalar so that it starts at column
// to, or by delta when its header gives its indentation, which is counted
// from what holds the scalar. A line of only spaces is emptied when the
// spaces are indentation rather than content.
func indentBody(body []*line, to, delta int, explicit bool) {
	least := -1
	for _, l := range body {
		if strings.TrimSpace(l.text) != "" && (least < 0 || l.indent < least) {
			least = l.indent
		}
	}
	if !explicit && least >= 0 {
		delta = to - least
	}
	for _, l := range body {
		l.newIndent = max(l.indent+delta, 0)
		if strings.Trim(l.text, " ") == "" && !explicit && len(l.text) <= least {
			l.newIndent = 0
			l.text = ""
		}
	}
}

// separate marks the content lines that a blank line comes before: the keys
// at the top of a document, when either of two that meet holds a mapping or
// a sequence, and the jobs but the first.
func separate(lines []*line) {
	type entry struct {
		at       int
		children bool
	}
	var entries []entry
	flush := func() {
		for i := 1; i < len(entries); i++ {
			if entries[i-1].children || entries[i].children {
				lines[entries[i].at].blank = true
			}
		}
		entries = nil
	}

	jobs, first := false, false
	for i, l := range lines {
		if l.kind != kindContent {
			continue
		}
		switch {
		case l.marker:
			flush()
			jobs = false
		case l.newIndent == 0 && len(l.dashes) == 0:
			entries = append(entries, entry{at: i})
			jobs, first = l.key() == jobsKey, true
		default:
			if len(entries) > 0 {
				entries[len(entries)-1].children = true
			}
			if jobs && l.newIndent == 0 && len(l.dashes) > 0 {
				l.blank = !first
				first = false
			}
		}
	}
	flush()
}

// render writes the lines out as they are to be.
func render(lines []*line) string {
	var out []string
	// attached is where the comments on the content line to come start in
	// out, so that its blank line comes before them; -1 when there are none.
	attached := -1
	for i, l := range lines {
		switch l.kind {
		case kindBlank:
			continue
		case kindComment:
			if l.alignTo >= 0 && attached < 0 {
				attached = len(out)
			}
			if l.alignTo < 0 {
				attached = -1
			}
			out = append(out, pad(l.newIndent)+comment(strings.TrimSpace(l.text), i == 0))
			continue
		case kindCont:
			out = append(out, strings.TrimRight(pad(l.newIndent)+strings.TrimLeft(l.text, " "), " \t"))
		case kindBody:
			// What a line of a block scalar ends in is part of the scalar.
			out = append(out, pad(l.newIndent)+strings.TrimLeft(l.text, " "))
		case kindContent:
			if l.blank && len(out) > 0 {
				at := len(out)
				if attached >= 0 {
					at = attached
				}
				out = append(out[:at], append([]string{""}, out[at:]...)...)
			}
			out = append(out, l.render())
		}
		attached = -1
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// render writes a content line: its -s, what follows them with a single
// space after each key, and its comment.
func (l *line) render() string {
	rest, cmt := "", ""
	end := len(l.text)
	if l.comment >= 0 {
		end = commentAt(l.text, byteAt(l.text, l.comment))
		cmt = comment(strings.TrimSpace(l.text[end:]), false)
	}
	if l.first >= 0 {
		start := byteAt(l.text, l.first)
		rest = l.text[start:end]
		// From the last to the first, so that the columns before one hold.
		for i := len(l.toks) - 1; i >= 0; i-- {
			if t := l.toks[i]; t.typ == token.MappingValueType && t.col >= l.first {
				rest = tighten(rest, byteAt(l.text, t.col)-start)
			}
		}
		rest = strings.TrimRight(rest, " \t")
	}

	parts := make([]string, 0, len(l.dashes)+2)
	for range l.dashes {
		parts = append(parts, "-")
	}
	if rest != "" {
		parts = append(parts, rest)
	}
	if cmt != "" {
		parts = append(parts, cmt)
	}
	return pad(l.newIndent) + strings.Join(parts, " ")
}

// tighten leaves a single space after the : at i of s, when more than one
// separates it from what follows.
func tighten(s string, i int) string {
	if i < 0 || i >= len(s) || s[i] != ':' {
		return s
	}
	after := strings.TrimLeft(s[i+1:], " ")
	if after == "" || len(s)-i-1-len(after) < 2 {
		return s
	}
	return s[:i+1] + " " + after
}

// comment returns a comment with a space after its #. The #! a file may
// start with is left as it is.
func comment(s string, first bool) string {
	s = strings.TrimRight(s, " \t")
	if first && strings.HasPrefix(s, "#!") {
		return s
	}
	text := strings.TrimLeft(s, "#")
	if text == "" || text[0] == ' ' || text[0] == '\t' {
		return s
	}
	return s[:len(s)-len(text)] + " " + text
}

// commentAt returns where the comment of a line starts, given where its
// token says it does: the column of one that follows the header of a block
// scalar is past its #.
func commentAt(s string, at int) int {
	if at > 0 && at <= len(s) && (at == len(s) || s[at] != '#') && s[at-1] == '#' {
		return at - 1
	}
	return at
}

func pad(n int) string {
	return strings.Repeat(" ", n)
}

// byteAt returns the offset in s of its column col, which is counted in
// runes.
func byteAt(s string, col int) int {
	off := 0
	for ; col > 0 && off < len(s); col-- {
		_, size := utf8.DecodeRuneInString(s[off:])
		off += size
	}
	return off
}

// canonical returns what text means, apart from how it is laid out and from
// its comments: two texts that mean the same have the same one.
func canonical(text string) (string, error) {
	// A key may be written more than once, as a run reads a workflow.
	file, err := parser.ParseBytes([]byte(text), 0, parser.AllowDuplicateMapKey())
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	for _, doc := range file.Docs {
		writeNode(&b, doc.Body)
		b.WriteString("\n---\n")
	}
	return b.String(), nil
}

func writeNode(b *bytes.Buffer, node ast.Node) {
	// A typed nil is a node that is not there.
	if v := reflect.ValueOf(node); node == nil || v.Kind() == reflect.Pointer && v.IsNil() {
		b.WriteString("~")
		return
	}
	switch n := node.(type) {
	case *ast.MappingNode:
		b.WriteString("{")
		for _, v := range n.Values {
			writeNode(b, v)
		}
		b.WriteString("}")
	case *ast.MappingValueNode:
		writeNode(b, n.Key)
		b.WriteString(":")
		writeNode(b, n.Value)
		b.WriteString(",")
	case *ast.MappingKeyNode:
		b.WriteString("?")
		writeNode(b, n.Value)
	case *ast.SequenceNode:
		b.WriteString("[")
		for _, v := range n.Values {
			writeNode(b, v)
			b.WriteString(",")
		}
		b.WriteString("]")
	case *ast.AnchorNode:
		b.WriteString("&")
		writeNode(b, n.Name)
		b.WriteString(" ")
		writeNode(b, n.Value)
	case *ast.AliasNode:
		b.WriteString("*")
		writeNode(b, n.Value)
	case *ast.TagNode:
		fmt.Fprintf(b, "%q ", n.Start.Value)
		writeNode(b, n.Value)
	case *ast.LiteralNode:
		// The header is in what the scalar reads as.
		fmt.Fprintf(b, "%s(%q)", n.Start.Type, n.Value.Value)
	case *ast.DirectiveNode:
		fmt.Fprintf(b, "%%%q", n.String())
	default:
		tk := node.GetToken()
		fmt.Fprintf(b, "%s(%q)", tk.Type, tk.Value)
	}
}
