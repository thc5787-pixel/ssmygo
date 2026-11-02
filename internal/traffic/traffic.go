// Package traffic counts bytes relayed through the local port and keeps a
// per-second history for the tray badge and the log window's chart.
package traffic

import (
	"sync"
	"sync/atomic"
)

// Meter holds running totals. Up is what apps send through ssmygo, down
// what they receive. It satisfies shadowsocks-go's service.TrafficMeter.
type Meter struct{ up, down atomic.Uint64 }

// Add counts up and down payload bytes.
func (m *Meter) Add(up, down uint64) {
	if up != 0 {
		m.up.Add(up)
	}
	if down != 0 {
		m.down.Add(down)
	}
}

// Totals returns the bytes counted so far.
func (m *Meter) Totals() (up, down uint64) { return m.up.Load(), m.down.Load() }

// Sample is the bytes moved in one interval (bytes per second at 1 s).
type Sample struct{ Up, Down uint64 }

// Snapshot is the sampler's state at one moment.
type Snapshot struct {
	Rate               Sample
	TotalUp, TotalDown uint64
	History            []Sample // oldest first
}

// Sampler turns a Meter's totals into per-interval samples.
type Sampler struct {
	m    *Meter
	keep int

	mu               sync.Mutex
	lastUp, lastDown uint64
	hist             []Sample
}

// NewSampler keeps the newest keep samples of m.
func NewSampler(m *Meter, keep int) *Sampler { return &Sampler{m: m, keep: keep} }

// Tick records one sample: the bytes counted since the previous Tick.
func (s *Sampler) Tick() Snapshot {
	up, down := s.m.Totals()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hist = append(s.hist, Sample{up - s.lastUp, down - s.lastDown})
	if len(s.hist) > s.keep {
		s.hist = append(s.hist[:0], s.hist[len(s.hist)-s.keep:]...)
	}
	s.lastUp, s.lastDown = up, down
	return s.snapshotLocked()
}

// Snapshot returns a copy of the current state; safe from any goroutine.
func (s *Sampler) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Sampler) snapshotLocked() Snapshot {
	snap := Snapshot{TotalUp: s.lastUp, TotalDown: s.lastDown, History: append([]Sample(nil), s.hist...)}
	if n := len(s.hist); n > 0 {
		snap.Rate = s.hist[n-1]
	}
	return snap
}
