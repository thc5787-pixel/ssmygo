package controller

import (
	"errors"
	"fmt"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core"
)

// Strategies the tray offers (E-D1); State.Strategy is one of them.
const (
	StrategyNone      = ""
	StrategyBalancing = "balancing"
	StrategyHA        = "ha"
)

var strategyIDs = map[string]config.StrategyID{
	StrategyNone:      "",
	StrategyBalancing: config.StrategyBalancing,
	StrategyHA:        config.StrategyHA,
}

// ErrNoStrategyMembers: no valid server without a plugin, so a strategy has
// nothing to choose from and the current server is used.
var ErrNoStrategyMembers = errors.New("no server can join the strategy (plugin servers are excluded)")

// strategyActive reports whether cfg runs a strategy: a known one with at
// least one member. Otherwise the current server is used.
func strategyActive(cfg *config.Config) bool {
	if !cfg.Strategy.Known() {
		return false
	}
	m, _ := core.StrategyMembers(cfg)
	return len(m) > 0
}

// strategyName is State.Strategy for cfg.
func strategyName(cfg *config.Config) string {
	if !strategyActive(cfg) {
		return StrategyNone
	}
	if cfg.Strategy == config.StrategyHA {
		return StrategyHA
	}
	return StrategyBalancing
}

// SetStrategy turns a strategy on (StrategyBalancing, StrategyHA) or off
// (StrategyNone). A strategy without members is refused.
func (c *Controller) SetStrategy(name string) error {
	c.mu.Lock()
	defer c.unlock()
	id, ok := strategyIDs[name]
	if !ok {
		return c.fail(fmt.Errorf("unknown strategy %q", name))
	}
	next := c.cfg.Clone()
	next.Strategy = id
	if id != "" && !strategyActive(next) {
		return c.fail(ErrNoStrategyMembers)
	}
	return c.commit(next)
}

// ToggleSystemProxy switches between Disabled and the mode last enabled
// (cfg.Global, kept while disabled; Rules on a fresh configuration).
func (c *Controller) ToggleSystemProxy() error {
	c.mu.Lock()
	defer c.unlock()
	switch {
	case c.cfg.Enabled:
		return c.setModeLocked(Disabled)
	case c.cfg.Global:
		return c.setModeLocked(Global)
	default:
		return c.setModeLocked(Rules)
	}
}

// ToggleProxyMode switches between Rules ("PAC") and Global; from Disabled
// it enables Rules.
func (c *Controller) ToggleProxyMode() error {
	c.mu.Lock()
	defer c.unlock()
	if c.cfg.Enabled && !c.cfg.Global {
		return c.setModeLocked(Global)
	}
	return c.setModeLocked(Rules)
}

// ToggleShareLAN flips Allow Clients from LAN.
func (c *Controller) ToggleShareLAN() error {
	c.mu.Lock()
	defer c.unlock()
	next := c.cfg.Clone()
	next.ShareOverLan = !next.ShareOverLan
	return c.commit(next)
}

// SelectRelative selects the server delta places away, wrapping around the
// list, and clears the strategy (original ServerMoveUp/Down).
func (c *Controller) SelectRelative(delta int) error {
	c.mu.Lock()
	defer c.unlock()
	n := len(c.cfg.Configs)
	if n == 0 {
		return c.fail(ErrNoServer)
	}
	return c.selectServerLocked(((c.cfg.Index+delta)%n + n) % n)
}

// Hotkeys returns the hotkey settings.
func (c *Controller) Hotkeys() config.HotkeyConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.Hotkey
}

// SaveHotkeys writes the Hotkeys window's settings; nothing else changes,
// so the core is not rebuilt.
func (c *Controller) SaveHotkeys(h config.HotkeyConfig) error {
	c.mu.Lock()
	defer c.unlock()
	next := c.cfg.Clone()
	next.SetHotkey(h)
	c.cfg = next
	c.dirty = true
	if err := c.save(); err != nil {
		return c.fail(&SaveError{Err: err})
	}
	return nil
}
