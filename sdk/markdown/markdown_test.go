package markdown

import "testing"

func TestEscape(t *testing.T) {
	for _, tt := range []struct {
		style Style
		want  string
	}{
		{AsEntity, "token &lt;secret:TOKEN&gt; here"},
		{AsCode, "token `<secret:TOKEN>` here"},
	} {
		if got := Escape("token <secret:TOKEN> here", tt.style); got != tt.want {
			t.Errorf("Escape(%v) = %q, want %q", tt.style, got, tt.want)
		}
	}
}
