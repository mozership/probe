package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary stands in for probe when fakeEnv is set: it writes as its
// json report what it was given, its arguments and its vars, then waits
// fakeSleepEnv and exits with fakeExitEnv.
const (
	fakeEnv      = "PROBE_SERVE_TEST_FAKE"
	fakeSleepEnv = "PROBE_SERVE_TEST_SLEEP"
	fakeExitEnv  = "PROBE_SERVE_TEST_EXIT"
)

// fakeReport is what the fake probe writes as its report.
type fakeReport struct {
	Args []string          `json:"args"`
	Vars map[string]string `json:"vars"`
	Env  []string          `json:"env"`
}

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) == "1" {
		os.Exit(fakeProbe())
	}
	os.Exit(m.Run())
}

func fakeProbe() int {
	var report string
	for i, a := range os.Args {
		if a == "--report" && i+1 < len(os.Args) {
			report = strings.TrimPrefix(os.Args[i+1], "json=")
		}
	}
	stdin, _ := io.ReadAll(os.Stdin)
	var vars map[string]any
	_ = json.Unmarshal(stdin, &vars)
	text := make(map[string]string, len(vars))
	for k, v := range vars {
		text[k] = fmt.Sprint(v)
	}
	data, _ := json.Marshal(fakeReport{Args: os.Args[1:], Vars: text, Env: os.Environ()})
	_ = os.WriteFile(report, data, 0o644)
	fmt.Println("fake probe ran")
	if d, err := time.ParseDuration(os.Getenv(fakeSleepEnv)); err == nil {
		time.Sleep(d)
	}
	code, _ := strconv.Atoi(os.Getenv(fakeExitEnv))
	return code
}

// fakeProbeExe returns the probe the runs of a test are processes of, with
// sleep and exit as what each does.
func fakeProbeExe(t *testing.T, sleep time.Duration, exit int) string {
	t.Helper()
	t.Setenv(fakeEnv, "1")
	t.Setenv(fakeSleepEnv, sleep.String())
	t.Setenv(fakeExitEnv, strconv.Itoa(exit))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func readFakeReport(t *testing.T, r Run) fakeReport {
	t.Helper()
	data, err := os.ReadFile(r.ReportPath())
	if err != nil {
		t.Fatal(err)
	}
	var fr fakeReport
	if err := json.Unmarshal(data, &fr); err != nil {
		t.Fatal(err)
	}
	return fr
}
