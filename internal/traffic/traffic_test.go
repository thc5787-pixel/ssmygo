package traffic

import (
	"sync"
	"testing"
)

func TestMeterConcurrentAdd(t *testing.T) {
	var m Meter
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			for range 1000 {
				m.Add(1, 2)
			}
		})
	}
	wg.Wait()
	if up, down := m.Totals(); up != 100000 || down != 200000 {
		t.Fatalf("totals %d %d", up, down)
	}
}

func TestSamplerRates(t *testing.T) {
	var m Meter
	s := NewSampler(&m, 300)
	m.Add(10, 20)
	if got := s.Tick(); got.Rate != (Sample{10, 20}) || got.TotalUp != 10 || got.TotalDown != 20 {
		t.Fatalf("first tick %+v", got)
	}
	got := s.Tick()
	if got.Rate != (Sample{}) {
		t.Fatalf("idle tick rate %+v", got.Rate)
	}
	if len(got.History) != 2 || got.History[0] != (Sample{10, 20}) || got.History[1] != (Sample{}) {
		t.Fatalf("history %+v", got.History)
	}
}

func TestSamplerCap(t *testing.T) {
	var m Meter
	s := NewSampler(&m, 300)
	for i := range 305 {
		m.Add(uint64(i+1), 0)
		s.Tick()
	}
	h := s.Snapshot().History
	if len(h) != 300 || h[0].Up != 6 || h[299].Up != 305 {
		t.Fatalf("len %d first %d last %d", len(h), h[0].Up, h[len(h)-1].Up)
	}
}

func TestSnapshotIsCopy(t *testing.T) {
	var m Meter
	s := NewSampler(&m, 10)
	m.Add(5, 5)
	s.Tick()
	snap := s.Snapshot()
	snap.History[0].Up = 999
	if s.Snapshot().History[0].Up != 5 {
		t.Fatal("snapshot shares history with the sampler")
	}
}

func TestFormatRate(t *testing.T) {
	for in, want := range map[uint64]string{0: "0 B/s", 1023: "1023 B/s", 12595: "12.3 KB/s", 1572864: "1.5 MB/s"} {
		if got := FormatRate(in); got != want {
			t.Errorf("FormatRate(%d) = %q, want %q", in, got, want)
		}
	}
	if got := FormatBytes(92274688); got != "88.0 MB" {
		t.Errorf("FormatBytes = %q", got)
	}
}
