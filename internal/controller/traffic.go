package controller

import (
	"path/filepath"
	"time"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/traffic"
)

// trafficInterval is the sampling period (tests shorten it).
var trafficInterval = time.Second

// historyLen is 5 minutes of 1 s samples (B-D5).
const historyLen = 300

// runTraffic samples the meter until stop closes, telling the traffic
// subscribers each time, outside any lock.
func (c *Controller) runTraffic(stop <-chan struct{}) {
	t := time.NewTicker(trafficInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		snap := c.sampler.Tick()
		c.subMu.Lock()
		fns := make([]func(traffic.Snapshot), 0, len(c.trafficSubs))
		for _, fn := range c.trafficSubs {
			fns = append(fns, fn)
		}
		c.subMu.Unlock()
		for _, fn := range fns {
			fn(snap)
		}
	}
}

// SubscribeTraffic calls fn with a snapshot after every sample, on the
// sampling goroutine. Separate from Subscribe so the tray state is not
// rebuilt every second.
func (c *Controller) SubscribeTraffic(fn func(traffic.Snapshot)) (off func()) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	id := c.nextID
	c.nextID++
	c.trafficSubs[id] = fn
	return func() {
		c.subMu.Lock()
		defer c.subMu.Unlock()
		delete(c.trafficSubs, id)
	}
}

// Traffic returns the latest snapshot.
func (c *Controller) Traffic() traffic.Snapshot { return c.sampler.Snapshot() }

// LogPath is the file the app's logger writes: the data root at startup
// (the logger does not move with portable mode).
func (c *Controller) LogPath() string { return filepath.Join(c.d.Paths.Logs(), "ssmygo.log") }

// LogViewer returns the log window's saved settings.
func (c *Controller) LogViewer() config.LogViewer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.LogViewer
}

// SaveLogViewer stores the log window's settings and saves gui-config.json.
func (c *Controller) SaveLogViewer(v config.LogViewer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg.SetLogViewer(v)
	if err := c.save(); err != nil {
		return &SaveError{Err: err}
	}
	return nil
}

// SetShowPluginOutput sets the Help ▸ Show Plugin Output checkbox.
func (c *Controller) SetShowPluginOutput(on bool) error {
	c.mu.Lock()
	defer c.unlock()
	c.cfg.ShowPluginOutput = on
	c.dirty = true
	if err := c.save(); err != nil {
		return &SaveError{Err: err}
	}
	return nil
}

// ConfigWindowBounds is the Edit Servers window's last size and position.
func (c *Controller) ConfigWindowBounds() config.WindowBounds {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.ConfigWindow
}

// SaveConfigWindowBounds stores the Edit Servers window's size and position
// and saves gui-config.json.
func (c *Controller) SaveConfigWindowBounds(b config.WindowBounds) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg.ConfigWindow = b
	if err := c.save(); err != nil {
		return &SaveError{Err: err}
	}
	return nil
}

// SetLanguage saves the UI language ("" follows the system, "en",
// "zh-Hans"); the UI switches its strings itself.
func (c *Controller) SetLanguage(tag string) error {
	c.mu.Lock()
	defer c.unlock()
	c.cfg.Language = tag
	c.dirty = true
	if err := c.save(); err != nil {
		return &SaveError{Err: err}
	}
	return nil
}
