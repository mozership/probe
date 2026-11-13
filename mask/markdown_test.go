package mask

import "testing"

func TestEscapeMarkdown(t *testing.T) {
	for _, tc := range []struct{ in, entity, code string }{
		{"the password <secret:SHOP_PASSWORD> fails", "the password &lt;secret:SHOP_PASSWORD&gt; fails", "the password `<secret:SHOP_PASSWORD>` fails"},
		{"Authorization: <redacted>", "Authorization: &lt;redacted&gt;", "Authorization: `<redacted>`"},
		// In a code span or a fenced code block, a placeholder shows as it is.
		{"sent `<secret:TOKEN>` and <redacted>", "sent `<secret:TOKEN>` and &lt;redacted&gt;", "sent `<secret:TOKEN>` and ``<redacted>``"},
		{"``a ` <redacted>`` <redacted>", "``a ` <redacted>`` &lt;redacted&gt;", "``a ` <redacted>`` ```<redacted>```"},
		{"```\ntoken=<secret:TOKEN>\n```\n<secret:TOKEN>", "```\ntoken=<secret:TOKEN>\n```\n&lt;secret:TOKEN&gt;", "```\ntoken=<secret:TOKEN>\n```\n`<secret:TOKEN>`"},
		{"~~~\n```\n<redacted>\n~~~", "~~~\n```\n<redacted>\n~~~", "~~~\n```\n<redacted>\n~~~"},
		{"````\n```\n<redacted>\n````\n<redacted>", "````\n```\n<redacted>\n````\n&lt;redacted&gt;", "````\n```\n<redacted>\n````\n`<redacted>`"},
		{"```\n<redacted>\n``` not a closer\n<redacted>\n```\n<redacted>", "```\n<redacted>\n``` not a closer\n<redacted>\n```\n&lt;redacted&gt;", "```\n<redacted>\n``` not a closer\n<redacted>\n```\n`<redacted>`"},
		{"``` a`b\n<redacted>", "``` a`b\n&lt;redacted&gt;", "``` a`b\n````<redacted>````"},
		// A code span goes on over the lines of a paragraph, not past it.
		{"a `span\nwith <redacted>` and <redacted>", "a `span\nwith <redacted>` and &lt;redacted&gt;", "a `span\nwith <redacted>` and ``<redacted>``"},
		{"an open `\n\n<redacted> x", "an open `\n\n&lt;redacted&gt; x", "an open `\n\n`<redacted>` x"},
		// A run with no closer of its length is text, and opens no span.
		{"an open ` <redacted>", "an open ` &lt;redacted&gt;", "an open ` ``<redacted>``"},
		{"` <redacted>`` x", "` &lt;redacted&gt;`` x", "` ```<redacted>``` `` x"},
		// An escaped backtick opens no span.
		{"\\` <redacted> x", "\\` &lt;redacted&gt; x", "\\` ``<redacted>`` x"},
		// A backslash right before a placeholder is escaped, and shown.
		{"C:\\<secret:DIR>", "C:\\\\&lt;secret:DIR&gt;", "C:\\\\`<secret:DIR>`"},
		{"C:\\\\<secret:DIR>", "C:\\\\&lt;secret:DIR&gt;", "C:\\\\`<secret:DIR>`"},
		// A placeholder right next to a code span is kept apart from it.
		{"`code`<redacted>`more`", "`code`&lt;redacted&gt;`more`", "`code` ``<redacted>`` `more`"},
		{"a`<redacted>", "a`&lt;redacted&gt;", "a` ``<redacted>``"},
		// What is not a placeholder is left alone.
		{"<b>bold</b> and <https://example.com|a link>", "<b>bold</b> and <https://example.com|a link>", "<b>bold</b> and <https://example.com|a link>"},
	} {
		if got := EscapeMarkdown(tc.in, EscapeAsEntity); got != tc.entity {
			t.Errorf("EscapeMarkdown(%q, EscapeAsEntity) = %q, want %q", tc.in, got, tc.entity)
		}
		if got := EscapeMarkdown(tc.in, EscapeAsCode); got != tc.code {
			t.Errorf("EscapeMarkdown(%q, EscapeAsCode) = %q, want %q", tc.in, got, tc.code)
		}
	}
}

// TestPlaceholderMatchesWhatAMaskerWrites checks that Placeholder matches
// the labels a Masker puts in place of a secret and of a credential.
func TestPlaceholderMatchesWhatAMaskerWrites(t *testing.T) {
	m := New([]string{"API_TOKEN"}, map[string]string{"API_TOKEN": "tok-123456"})
	m.Learn(map[string]any{"headers": map[string]any{"Authorization": "Bearer abcdefgh"}})
	got := m.Value("tok-123456 Bearer abcdefgh").(string)
	if found := Placeholder.FindAllString(got, -1); len(found) != 2 || found[0] != "<secret:API_TOKEN>" || found[1] != "<redacted>" {
		t.Errorf("Placeholder in %q = %q, want <secret:API_TOKEN> and <redacted>", got, found)
	}
}
