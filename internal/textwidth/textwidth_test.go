package textwidth

import "testing"

func TestWidth(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"Tests", 5},
		{"結合テスト", 10},
		{"ﾃｽﾄ", 3},
		{"é", 1},
		{"e\u0301", 1},
		{"Ｔｅｓｔ", 8},
		{"a\u200bb", 2},
		{"╭─○…╮", 5},
	}
	for _, tt := range tests {
		if got := Width(tt.in); got != tt.want {
			t.Errorf("Width(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"Tests", 5, "Tests"},
		{"Testing", 5, "Test…"},
		{"Testing", 1, "…"},
		{"Testing", 0, ""},
		{"Testing", -1, ""},
		{"", 0, ""},
		{"結合テスト", 10, "結合テスト"},
		{"結合テスト", 6, "結合…"},
		{"結合テスト", 5, "結合…"},
		{"結合テスト", 2, "…"},
	}
	for _, tt := range tests {
		got := Truncate(tt.in, tt.max)
		if got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
		if w := Width(got); w > max(tt.max, 0) {
			t.Errorf("Truncate(%q, %d) is %d columns wide", tt.in, tt.max, w)
		}
	}
}

func TestOneLine(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Tests", "Tests"},
		{" Check\n  [api]\t ", "Check [api]"},
		{"a\r\nb\x00c", "a b c"},
		{"Check\u202e ipa\u200b", "Check ipa"},
		{"結合\u3000テスト", "結合 テスト"},
		{"\n\t", ""},
	}
	for _, tt := range tests {
		if got := OneLine(tt.in); got != tt.want {
			t.Errorf("OneLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
