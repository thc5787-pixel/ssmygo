package ui

import (
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/thc5787/ssmygo/internal/config"
)

// windowStates is the part of the controller that keeps window states.
type windowStates interface {
	WindowStates() map[string]config.WindowState
	SaveWindowStates(map[string]config.WindowState) error
}

// WindowStateStore keeps MyGo's window states (WindowOptions.StateKey) in
// gui-config.json, through the controller, for App.SetWindowStateStore.
type WindowStateStore struct {
	ctrl windowStates

	mu     sync.Mutex // guards latest
	latest map[string]config.WindowState
	saveMu sync.Mutex     // one save at a time
	wg     sync.WaitGroup // saves under way
}

// NewWindowStateStore keeps window states through ctrl.
func NewWindowStateStore(ctrl windowStates) *WindowStateStore {
	return &WindowStateStore{ctrl: ctrl}
}

// LoadWindowStates returns the saved states.
func (s *WindowStateStore) LoadWindowStates() (map[string]mygo.WindowState, error) {
	in := s.ctrl.WindowStates()
	out := make(map[string]mygo.WindowState, len(in))
	for k, v := range in {
		out[k] = mygo.WindowState(v)
	}
	return out, nil
}

// SaveWindowStates saves states in the background: MyGo calls it on the
// main thread, and the controller's lock may be held for seconds (a save
// waiting for a plugin to start). The newest states win.
func (s *WindowStateStore) SaveWindowStates(states map[string]mygo.WindowState) error {
	m := make(map[string]config.WindowState, len(states))
	for k, v := range states {
		m[k] = config.WindowState(v)
	}
	s.mu.Lock()
	s.latest = m
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.saveMu.Lock()
		defer s.saveMu.Unlock()
		s.mu.Lock()
		m := s.latest
		s.latest = nil
		s.mu.Unlock()
		if m != nil { // nil: an earlier goroutine already saved the newest
			s.ctrl.SaveWindowStates(m) // a failure only loses the window's place
		}
	}()
	return nil
}

// Wait waits up to timeout for the saves under way, so the states MyGo
// saves when the app quits reach the file before the process exits.
func (s *WindowStateStore) Wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
