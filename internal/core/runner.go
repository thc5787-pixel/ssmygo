package core

import (
	"context"
	"sync"

	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/tslog"
)

// Runner owns the running shadowsocks-go Manager. Every Apply rebuilds it
// (about 3 ms, spike R4); connections already relaying keep their server.
type Runner struct {
	mu     sync.Mutex
	logger *tslog.Logger
	meter  service.TrafficMeter
	cur    *service.Manager
}

// NewRunner returns a Runner whose servers report traffic to meter (nil =
// no counting). The meter outlives every Manager, so totals carry across
// Apply.
func NewRunner(logger *tslog.Logger, meter service.TrafficMeter) *Runner {
	return &Runner{logger: logger, meter: meter}
}

// Apply stops the current Manager and starts sc. On error nothing is running.
func (r *Runner) Apply(sc *service.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
	if r.meter != nil {
		for i := range sc.Servers {
			sc.Servers[i].TrafficMeter = r.meter
		}
	}
	m, err := sc.NewManager(r.logger)
	if err != nil {
		return err
	}
	if err := m.Start(context.Background()); err != nil {
		m.Close()
		return err
	}
	r.cur = m
	return nil
}

// Stop stops the current Manager, if any.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

func (r *Runner) stopLocked() {
	if r.cur == nil {
		return
	}
	r.cur.Stop()
	r.cur.Close()
	r.cur = nil
}
