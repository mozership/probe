package dag

import (
	"reflect"
	"testing"
)

func TestDetectCycleFn_NoCycle(t *testing.T) {
	getDeps := func(id string) []string {
		switch id {
		case "A":
			return []string{"B", "C"}
		case "B":
			return []string{"D"}
		case "C":
			return []string{"D"}
		default:
			return nil
		}
	}

	cycle := DetectCycleFn([]string{"A", "B", "C", "D"}, getDeps)
	if cycle != nil {
		t.Errorf("expected no cycle, got %v", cycle)
	}
}

func TestDetectCycleFn_WithCycle(t *testing.T) {
	getDeps := func(id string) []string {
		switch id {
		case "A":
			return []string{"B"}
		case "B":
			return []string{"C"}
		case "C":
			return []string{"A"} // Cycle back to A
		default:
			return nil
		}
	}

	cycle := DetectCycleFn([]string{"A", "B", "C"}, getDeps)
	if cycle == nil {
		t.Error("expected cycle, got nil")
	}
}

func TestDetectCycleFn_ReturnsCyclePath(t *testing.T) {
	getDeps := func(id string) []string {
		switch id {
		case "A":
			return []string{"B"}
		case "B":
			return []string{"C"}
		case "C":
			return []string{"A"}
		default:
			return nil
		}
	}

	cycle := DetectCycleFn([]string{"A", "B", "C"}, getDeps)
	want := []string{"A", "B", "C"}
	if !reflect.DeepEqual(cycle, want) {
		t.Errorf("expected cycle %v, got %v", want, cycle)
	}
}

func TestDetectCycleFn_SelfLoop(t *testing.T) {
	getDeps := func(id string) []string {
		if id == "A" {
			return []string{"A"}
		}
		return nil
	}

	cycle := DetectCycleFn([]string{"A"}, getDeps)
	want := []string{"A"}
	if !reflect.DeepEqual(cycle, want) {
		t.Errorf("expected cycle %v, got %v", want, cycle)
	}
}

func TestDetectCycleFn_Empty(t *testing.T) {
	getDeps := func(id string) []string { return nil }

	if cycle := DetectCycleFn([]string{}, getDeps); cycle != nil {
		t.Errorf("expected no cycle, got %v", cycle)
	}
}
