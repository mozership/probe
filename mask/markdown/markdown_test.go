package markdown

import (
	"strings"
	"testing"

	"github.com/mozership/probe/mask"
)

func TestEscape(t *testing.T) {
	for _, tc := range []struct{ in, entity, code string }{
		{"the password <secret:SHOP_PASSWORD> fails", "the password &lt;secret:SHOP_PASSWORD&gt; fails", "the password `<secret:SHOP_PASSWORD>` fails"},
		{"Authorization: <redacted>", "Authorization: &lt;redacted&gt;", "Authorization: `<redacted>`"},
		{"no placeholder", "no placeholder", "no placeholder"},
		// In a code span or a code block, a placeholder shows as it is.
		{"sent `<secret:TOKEN>` and <redacted>", "sent `<secret:TOKEN>` and &lt;redacted&gt;", "sent `<secret:TOKEN>` and ``<redacted>``"},
		{"``a ` <redacted>`` <redacted>", "``a ` <redacted>`` &lt;redacted&gt;", "``a ` <redacted>`` ```<redacted>```"},
		{"```\ntoken=<secret:TOKEN>\n```\n<secret:TOKEN>", "```\ntoken=<secret:TOKEN>\n```\n&lt;secret:TOKEN&gt;", "```\ntoken=<secret:TOKEN>\n```\n`<secret:TOKEN>`"},
		{"~~~\n```\n<redacted>\n~~~", "~~~\n```\n<redacted>\n~~~", "~~~\n```\n<redacted>\n~~~"},
		{"````\n```\n<redacted>\n````\n<redacted>", "````\n```\n<redacted>\n````\n&lt;redacted&gt;", "````\n```\n<redacted>\n````\n`<redacted>`"},
		// A code span goes on over the lines of a paragraph, not past it.
		{"a `span\nwith <redacted>` and <redacted>", "a `span\nwith <redacted>` and &lt;redacted&gt;", "a `span\nwith <redacted>` and ``<redacted>``"},
		{"an open `\n\n<redacted> x", "an open `\n\n&lt;redacted&gt; x", "an open `\n\n``<redacted>`` x"},
		// Nor past a heading, a list item or a quote.
		{"# Heading `\n<redacted>`", "# Heading `\n&lt;redacted&gt;`", "# Heading `\n``<redacted>`` `"},
		{"- one `\n- <redacted> `", "- one `\n- &lt;redacted&gt; `", "- one `\n- ``<redacted>`` `"},
		// A code block in a quote, or indented, is code.
		{"> ~~~\n> <redacted>\n> ~~~\n\n<redacted>", "> ~~~\n> <redacted>\n> ~~~\n\n&lt;redacted&gt;", "> ~~~\n> <redacted>\n> ~~~\n\n`<redacted>`"},
		{"text\n\n    token=<redacted>\n\n<redacted>", "text\n\n    token=<redacted>\n\n&lt;redacted&gt;", "text\n\n    token=<redacted>\n\n`<redacted>`"},
		// A run with no closer of its length is text, and opens no span.
		{"an open ` <redacted>", "an open ` &lt;redacted&gt;", "an open ` ``<redacted>``"},
		{"` <redacted>`` x", "` &lt;redacted&gt;`` x", "` ```<redacted>``` `` x"},
		// An escaped backtick opens no span.
		{"\\` <redacted> x", "\\` &lt;redacted&gt; x", "\\` ``<redacted>`` x"},
		// A backslash right before a placeholder is escaped, and shown.
		{"C:\\<secret:DIR>", "C:\\\\&lt;secret:DIR&gt;", "C:\\\\`<secret:DIR>`"},
		{"C:\\\\<secret:DIR>", "C:\\\\&lt;secret:DIR&gt;", "C:\\\\`<secret:DIR>`"},
		// A placeholder is kept apart from a backtick, or another
		// placeholder, right next to it.
		{"`code`<redacted>`more`", "`code`&lt;redacted&gt;`more`", "`code` ``<redacted>`` `more`"},
		{"a`<redacted>", "a`&lt;redacted&gt;", "a` ``<redacted>``"},
		{"<redacted><redacted>", "&lt;redacted&gt;&lt;redacted&gt;", "`<redacted>` `<redacted>`"},
		// The span is of a length no run of the text has: 1 and 2 are taken
		// here, so it is of 3.
		{"`a` ``b`` <redacted>", "`a` ``b`` &lt;redacted&gt;", "`a` ``b`` ```<redacted>```"},
		// What a placeholder names is text, not an entity or emphasis.
		{"<secret:API&copy;> <secret:*B*>", "&lt;secret:API&amp;copy\\;&gt; &lt;secret:\\*B\\*&gt;", "`<secret:API&copy;>` `<secret:*B*>`"},
		// A secret named with a space is a placeholder too.
		{"<secret:API TOKEN>", "&lt;secret:API TOKEN&gt;", "`<secret:API TOKEN>`"},
		// What is not a placeholder is left alone.
		{"<b>bold</b> and <https://example.com|a link>", "<b>bold</b> and <https://example.com|a link>", "<b>bold</b> and <https://example.com|a link>"},
	} {
		if got := Escape(tc.in, AsEntity); got != tc.entity {
			t.Errorf("Escape(%q, AsEntity) = %q, want %q", tc.in, got, tc.entity)
		}
		if got := Escape(tc.in, AsCode); got != tc.code {
			t.Errorf("Escape(%q, AsCode) = %q, want %q", tc.in, got, tc.code)
		}
	}
}

// TestEscapeKeepsSpansShort checks that the span a placeholder is put in
// stays short however long the runs of backticks of the text are, so that
// a text of many placeholders does not grow by the length of its longest
// run for each.
func TestEscapeKeepsSpansShort(t *testing.T) {
	s := "~~~\n" + strings.Repeat("`", 100000) + "\n~~~\n\n" + strings.Repeat("<redacted> ", 10000)
	got := Escape(s, AsCode)
	if len(got) > len(s)+10000*3 {
		t.Errorf("Escape() of %d bytes gave %d, want each placeholder to grow by a short span", len(s), len(got))
	}
	if !strings.Contains(got, " `<redacted>` ") {
		t.Errorf("Escape() does not put the placeholders in spans of one backtick")
	}
}

// TestEscapeWhatAMaskerWrites checks Escape against the placeholders a
// Masker writes, a secret named with a space among them.
func TestEscapeWhatAMaskerWrites(t *testing.T) {
	m := mask.New([]string{"API_TOKEN", "API TOKEN"}, map[string]string{"API_TOKEN": "tok-123456", "API TOKEN": "spaced-secret"})
	m.Learn(map[string]any{"headers": map[string]any{"Authorization": "Bearer abcdefgh"}})
	masked := m.Value("tok-123456, spaced-secret and Bearer abcdefgh").(string)
	want := "&lt;secret:API_TOKEN&gt;, &lt;secret:API TOKEN&gt; and &lt;redacted&gt;"
	if got := Escape(masked, AsEntity); got != want {
		t.Errorf("Escape(%q) = %q, want %q", masked, got, want)
	}
}
