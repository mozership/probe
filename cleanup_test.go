package probe

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mozership/probe/actionrpc"
)

// recorder stands in for the actions cleanups calls and the warnings it
// gives.
type recorder struct {
	mu    sync.Mutex
	calls []string
	warns []string
	fail  map[string]error
}

func (r *recorder) cleanups() *cleanups {
	return &cleanups{
		runID: "run1",
		clean: func(uses string) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.calls = append(r.calls, uses)
			return r.fail[uses]
		},
		warn: func(format string, args ...any) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.warns = append(r.warns, fmt.Sprintf(format, args...))
		},
	}
}

func (r *recorder) called() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func TestCleanupsRun(t *testing.T) {
	tests := []struct {
		name string
		use  func(c *cleanups)
		want []string
	}{
		{
			name: "nothing ran",
			use:  func(c *cleanups) {},
		},
		{
			name: "an action is called once however many steps ran it",
			use: func(c *cleanups) {
				for range 10 {
					c.begin("/a")()
				}
			},
			want: []string{"/a"},
		},
		{
			name: "the last action first run is called first",
			use: func(c *cleanups) {
				c.begin("/a")()
				c.begin("/b")()
				c.begin("/a")()
				c.begin("/c")()
			},
			want: []string{"/c", "/b", "/a"},
		},
		{
			name: "an action a step ran in its own process is called as well",
			use: func(c *cleanups) {
				c.begin("/a")()
				c.owe("/b", "/a", "/c")
			},
			want: []string{"/c", "/b", "/a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &recorder{}
			c := r.cleanups()
			tt.use(c)
			c.run()
			if got := r.called(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("called %v, want %v", got, tt.want)
			}
			// A second run, as after a signal, calls nothing again.
			c.run()
			if got := r.called(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("called %v after a second run, want %v", got, tt.want)
			}
			if len(r.warns) != 0 {
				t.Errorf("warned %v", r.warns)
			}
		})
	}
}

func TestCleanupsNil(t *testing.T) {
	var c *cleanups
	c.begin("/a")()
	c.owe("/a")
	c.passOn(&actionrpc.Cleanups{})
	c.run()
}

func TestCleanupsRunWarnsOfAFailure(t *testing.T) {
	r := &recorder{fail: map[string]error{"/b": errors.New("no such server")}}
	c := r.cleanups()
	c.begin("/a")()
	c.begin("/b")()
	c.run()

	// The action after the one that failed is called all the same.
	if got, want := r.called(), []string{"/b", "/a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("called %v, want %v", got, want)
	}
	if len(r.warns) != 1 {
		t.Fatalf("warned %v, want one warning", r.warns)
	}
	for _, want := range []string{"/b", "run1", "no such server"} {
		if !strings.Contains(r.warns[0], want) {
			t.Errorf("warning %q does not tell %q", r.warns[0], want)
		}
	}
}

// A step that timed out goes on in the background, and may leave something
// behind after that, so run waits a moment for it.
func TestCleanupsRunWaitsForARunningStep(t *testing.T) {
	r := &recorder{}
	c := r.cleanups()
	end := c.begin("/a")
	ended := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		close(ended)
		end()
	}()
	c.run()

	select {
	case <-ended:
	default:
		t.Error("run did not wait for the step")
	}
	if got, want := r.called(), []string{"/a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("called %v, want %v", got, want)
	}
}

// What comes to be owed once run is over is called at once, since nothing
// else would.
func TestCleanupsAfterRun(t *testing.T) {
	r := &recorder{}
	c := r.cleanups()
	c.run()

	end := c.begin("/a")
	if got := r.called(); len(got) != 0 {
		t.Errorf("called %v while the step runs", got)
	}
	end()
	c.owe("/b")
	if got, want := r.called(), []string{"/a", "/b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("called %v, want %v", got, want)
	}
}

func TestCleanupsPassOn(t *testing.T) {
	r := &recorder{}
	c := r.cleanups()
	c.begin("/a")()
	c.owe("/b")

	to := &actionrpc.Cleanups{}
	c.passOn(to)
	if got, want := to.List(), []string{"/a", "/b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("passed on %v, want %v", got, want)
	}

	// The run they were passed on to calls them, and this one none: what a
	// step left behind is still in use there.
	c.run()
	if got := r.called(); len(got) != 0 {
		t.Errorf("called %v after passing on", got)
	}

	// Nor does it call what comes to be owed after that, as by a step that
	// timed out: that is passed on as well.
	c.begin("/c")()
	c.owe("/d", "/a")
	if got := r.called(); len(got) != 0 {
		t.Errorf("called %v after passing on", got)
	}
	if got, want := to.List(), []string{"/a", "/b", "/c", "/d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("passed on %v, want %v", got, want)
	}
}

func TestCleanupsPassOnToNone(t *testing.T) {
	r := &recorder{}
	c := r.cleanups()
	c.begin("/a")()
	c.passOn(nil)
	c.run()
	if got, want := r.called(), []string{"/a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("called %v, want %v", got, want)
	}
}

// A signal can end a job before it is over. Its cleanups have run by then,
// so nothing is left to pass on.
func TestCleanupsPassOnAfterRun(t *testing.T) {
	r := &recorder{}
	c := r.cleanups()
	c.begin("/a")()
	c.run()

	to := &actionrpc.Cleanups{}
	c.passOn(to)
	if got := to.List(); len(got) != 0 {
		t.Errorf("passed on %v after run", got)
	}
	if got, want := r.called(), []string{"/a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("called %v, want %v", got, want)
	}
}

// The log records of an action's cleanup are named after the action as the
// step's uses writes it, as those of the step are, and one no step of the
// run wrote, as one an embedded job ran, by its name in full.
func TestCleanupsNameOf(t *testing.T) {
	c := (&recorder{}).cleanups()
	c.writtenAs("/work/actions/greet", "./actions/greet")
	if got := c.nameOf("/work/actions/greet"); got != "./actions/greet" {
		t.Errorf("nameOf = %q, want ./actions/greet", got)
	}
	if got := c.nameOf("/work/other"); got != "/work/other" {
		t.Errorf("nameOf = %q, want /work/other", got)
	}
	var none *cleanups
	none.writtenAs("/a", "./a")
}
