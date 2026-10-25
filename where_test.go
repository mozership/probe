package probe

import "testing"

func TestWhere(t *testing.T) {
	jobs := []Job{
		{Name: "Users", Steps: []*Step{{Name: "Create"}, {}}},
		{},
	}
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"a named job", jobWhere(jobs, 0), `job 0 "Users"`},
		{"a job without a name", jobWhere(jobs, 1), `job 1 ""`},
		{"a named step", stepWhere(jobWhere(jobs, 0), 0, jobs[0].Steps[0]), `job 0 "Users", step 0 "Create"`},
		{"a step without a name", stepWhere(jobWhere(jobs, 0), 1, jobs[0].Steps[1]), `job 0 "Users", step 1`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %s, want %s", tt.got, tt.want)
			}
		})
	}
}
