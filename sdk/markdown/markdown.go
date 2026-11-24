// Package markdown writes the placeholders Probe puts in place of hidden
// values so that Markdown shows them, for an action that posts text with
// them, such as the notes of a run, where <secret:NAME> and <redacted> would
// be taken for something else: an HTML tag on GitHub, which leaves it out,
// or a link or a mention on Slack.
//
// It is a package of its own, apart from the sdk package, so that only an
// action that writes Markdown carries the Markdown parser it reads the text
// with.
package markdown

import "github.com/mozership/probe/internal/mask/markdown"

// Style is how Escape writes a placeholder.
type Style = markdown.Style

const (
	// AsEntity writes a placeholder with HTML entities, as
	// &lt;secret:NAME&gt;, for Markdown that takes HTML, such as that of
	// GitHub.
	AsEntity = markdown.AsEntity
	// AsCode puts a placeholder in a code span, for Markdown that reads
	// <...> as something else, such as that of Slack.
	AsCode = markdown.AsCode
)

// Escape returns s, Markdown with placeholders in it, with each placeholder
// written in style, so that Markdown shows it as it is and reads the rest of
// s as before.
func Escape(s string, style Style) string {
	return markdown.Escape(s, style)
}
