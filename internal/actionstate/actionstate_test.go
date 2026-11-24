package actionstate

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

func copyState(v map[string]any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	if _, ok := v["unsendable"]; ok {
		return nil, errors.New("cannot be sent")
	}
	return maps.Clone(v), nil
}

func TestStore_KeepsTheStateOfEachAction(t *testing.T) {
	s := New(copyState)
	if err := s.Set("count", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("count")["n"]; got != 1 {
		t.Errorf("Get(count)[n] = %v, want 1", got)
	}
	if got := s.Get("other"); got != nil {
		t.Errorf("Get(other) = %v, want nil", got)
	}
}

func TestStore_GetGivesACopy(t *testing.T) {
	s := New(copyState)
	if err := s.Set("count", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	s.Get("count")["n"] = 2
	if got := s.Get("count")["n"]; got != 1 {
		t.Errorf("Get(count)[n] = %v after changing a copy, want 1", got)
	}
}

func TestStore_SetKeepsACopy(t *testing.T) {
	s := New(copyState)
	state := map[string]any{"n": 1}
	if err := s.Set("count", state); err != nil {
		t.Fatal(err)
	}
	state["n"] = 2
	if got := s.Get("count")["n"]; got != 1 {
		t.Errorf("Get(count)[n] = %v after changing what was set, want 1", got)
	}
}

func TestStore_NilStateKeepsTheOneThereWas(t *testing.T) {
	s := New(copyState)
	if err := s.Set("count", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("count", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("count")["n"]; got != 1 {
		t.Errorf("Get(count)[n] = %v, want 1", got)
	}
}

func TestStore_UnsendableState(t *testing.T) {
	s := New(copyState)
	err := s.Set("count", map[string]any{"unsendable": true})
	if err == nil || !strings.Contains(err.Error(), "cannot keep the action's state") {
		t.Fatalf("err = %v, want one that tells the state cannot be kept", err)
	}
}

func TestStore_NilStoreKeepsNothing(t *testing.T) {
	var s *Store
	if err := s.Set("count", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("count"); got != nil {
		t.Errorf("Get(count) = %v, want nil", got)
	}
}
