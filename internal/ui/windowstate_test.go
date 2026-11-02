package ui

import (
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/thc5787/ssmygo/internal/config"
)

// fakeStates stands in for the controller; save blocks while gate is held.
type fakeStates struct {
	mu    sync.Mutex
	gate  sync.Mutex
	saved []map[string]config.WindowState
	have  map[string]config.WindowState
}

func (f *fakeStates) WindowStates() map[string]config.WindowState { return f.have }

func (f *fakeStates) SaveWindowStates(m map[string]config.WindowState) error {
	f.gate.Lock()
	defer f.gate.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, m)
	return nil
}

func TestWindowStateStoreLoads(t *testing.T) {
	f := &fakeStates{have: map[string]config.WindowState{"logViewer": {X: 1, Y: 2, Width: 800, Height: 600, Maximized: true}}}
	got, err := NewWindowStateStore(f).LoadWindowStates()
	if err != nil {
		t.Fatal(err)
	}
	if want := (mygo.WindowState{X: 1, Y: 2, Width: 800, Height: 600, Maximized: true}); got["logViewer"] != want {
		t.Fatalf("loaded %+v", got)
	}
}

func TestWindowStateStoreSavesInBackground(t *testing.T) {
	f := &fakeStates{}
	s := NewWindowStateStore(f)
	f.gate.Lock() // the controller is busy (a save holding its lock)
	done := make(chan error, 1)
	go func() {
		done <- s.SaveWindowStates(map[string]mygo.WindowState{"editServers": {Width: 600, Height: 640}})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("SaveWindowStates blocked the main thread on a busy controller")
	}
	// A later save while the first waits: the newest states win.
	s.SaveWindowStates(map[string]mygo.WindowState{"editServers": {Width: 700, Height: 700}})
	f.gate.Unlock()
	s.Wait(time.Second)
	f.mu.Lock()
	defer f.mu.Unlock()
	if n := len(f.saved); n == 0 || f.saved[n-1]["editServers"].Width != 700 {
		t.Fatalf("saved %+v", f.saved)
	}
}

func TestWindowStateStoreWaitTimesOut(t *testing.T) {
	f := &fakeStates{}
	s := NewWindowStateStore(f)
	f.gate.Lock()
	defer f.gate.Unlock()
	s.SaveWindowStates(map[string]mygo.WindowState{"logViewer": {Width: 640}})
	start := time.Now()
	s.Wait(50 * time.Millisecond)
	if time.Since(start) > time.Second {
		t.Fatal("Wait ignored its timeout")
	}
}
