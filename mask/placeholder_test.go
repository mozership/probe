package mask

import "testing"

func TestPlaceholderMatchesWhatAMaskerWrites(t *testing.T) {
	m := New([]string{"API_TOKEN", "API TOKEN"}, map[string]string{"API_TOKEN": "tok-123456", "API TOKEN": "spaced-secret"})
	m.Learn(map[string]any{"headers": map[string]any{"Authorization": "Bearer abcdefgh"}})
	got := m.Value("tok-123456 spaced-secret Bearer abcdefgh").(string)
	want := []string{"<secret:API_TOKEN>", "<secret:API TOKEN>", "<redacted>"}
	found := Placeholder.FindAllString(got, -1)
	if len(found) != len(want) {
		t.Fatalf("Placeholder in %q = %q, want %q", got, found, want)
	}
	for i := range want {
		if found[i] != want[i] {
			t.Errorf("Placeholder in %q = %q, want %q", got, found, want)
		}
	}
}
