// Package actionstate keeps the state each action leaves between its runs,
// for the runs of a job and for the calls of an action caller alike.
package actionstate

import (
	"fmt"
	"sync"
)

// Sendable turns a state into the form it takes when it is sent to an action
// in a process of its own, as sdk.Sendable does. It is given to New for
// this package not to import the sdk package, which imports this one.
type Sendable func(map[string]any) (map[string]any, error)

// Store holds the state each action leaves, keyed by the name of the action.
// A nil Store keeps nothing. Its methods may be called concurrently.
type Store struct {
	mu       sync.Mutex
	states   map[string]map[string]any
	sendable Sendable
}

// New returns an empty Store that keeps each state as sendable turns it.
func New(sendable Sendable) *Store {
	return &Store{states: make(map[string]map[string]any), sendable: sendable}
}

// Get returns a copy of the state the action left, or nil when it left none.
// A runner in this process may change what it is given, and the state kept
// must not change with it, should the step then fail or time out.
func (s *Store) Get(name string) map[string]any {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// What is kept was made sendable when it was set, so this cannot fail.
	state, _ := s.sendable(s.states[name])
	return state
}

// Set records the state the action leaves, in the form it takes when it is
// sent, which is a copy of it: a runner in this process may still hold the
// state it returned. A nil state keeps the one there was. A state that cannot
// be sent is an error, as it is for an action in a process of its own.
func (s *Store) Set(name string, state map[string]any) error {
	if s == nil || state == nil {
		return nil
	}
	sendable, err := s.sendable(state)
	if err != nil {
		return fmt.Errorf("cannot keep the action's state: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[name] = sendable
	return nil
}
