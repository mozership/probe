package suggest

import "testing"

func TestClosest(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		want    string
	}{
		{"tset", []string{"name", "test"}, "test"},
		{"api_base_url", []string{"API_BASE_URL", "api_url"}, "API_BASE_URL"},
		{"steps", []string{"outputs", "status", "vars"}, ""},
		{"x", []string{"res", "rt"}, ""},
		{"us", []string{"res", "rt"}, ""},
		{"rs", []string{"res", "rt"}, "res"},
		{"var", []string{"outputs", "vars"}, "vars"},
	}
	for _, tt := range tests {
		if got := Closest(tt.name, tt.allowed); got != tt.want {
			t.Errorf("Closest(%q, %q) = %q, want %q", tt.name, tt.allowed, got, tt.want)
		}
	}
}

func TestDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"test", "test", 0},
		{"tset", "test", 2},
		{"need", "needs", 1},
		{"", "abc", 3},
	}
	for _, tt := range tests {
		if got := distance(tt.a, tt.b); got != tt.want {
			t.Errorf("distance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
