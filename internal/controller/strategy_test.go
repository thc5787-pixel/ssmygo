package controller

import (
	"errors"
	"sync"
	"testing"

	"github.com/thc5787/ssmygo/internal/config"
)

// twoServers writes a config with two working servers (both the env's
// upstream, told apart by remarks).
func (e *env) twoServers(mod func(*config.Config)) {
	e.writeConfig(func(c *config.Config) {
		a, b := e.server(), e.server()
		a.Remarks, b.Remarks = "a", "b"
		c.Configs = []config.Server{a, b}
		if mod != nil {
			mod(c)
		}
	})
}

func TestSetStrategy(t *testing.T) {
	e := newEnv(t)
	e.twoServers(nil)
	c := e.start()
	if err := c.SetMode(Global); err != nil {
		t.Fatal(err)
	}
	if err := c.SetStrategy(StrategyBalancing); err != nil {
		t.Fatal(err)
	}
	if s := c.State(); s.Strategy != StrategyBalancing || s.StrategyExcluded != 0 {
		t.Fatalf("state %+v", s)
	}
	if e.diskConfig().Strategy != config.StrategyBalancing {
		t.Fatal("strategy not saved")
	}
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
	if err := c.SetStrategy(StrategyHA); err != nil || e.diskConfig().Strategy != config.StrategyHA || c.State().Strategy != StrategyHA {
		t.Fatalf("ha: %v", err)
	}
	if err := c.SetStrategy("round-robin"); err == nil {
		t.Fatal("unknown strategy accepted")
	}
	if err := c.SetStrategy(StrategyNone); err != nil || c.State().Strategy != StrategyNone || e.diskConfig().Strategy != "" {
		t.Fatalf("none: %v", err)
	}
}

func TestSelectServerClearsStrategy(t *testing.T) {
	e := newEnv(t)
	e.twoServers(func(c *config.Config) { c.Strategy = config.StrategyHA })
	c := e.start()
	if c.State().Strategy != StrategyHA {
		t.Fatalf("loaded strategy %q", c.State().Strategy)
	}
	if err := c.SelectServer(1); err != nil {
		t.Fatal(err)
	}
	if s := c.State(); s.Strategy != StrategyNone || s.Current != 1 || e.diskConfig().Strategy != "" {
		t.Fatalf("state %+v", s)
	}
}

func TestSaveServersClearsStrategy(t *testing.T) {
	e := newEnv(t)
	e.twoServers(func(c *config.Config) { c.Strategy = config.StrategyBalancing })
	c := e.start()
	cfg := c.Config()
	if err := c.SaveServers(cfg.Configs, 0, cfg.LocalPort, cfg.PortableMode); err != nil {
		t.Fatal(err)
	}
	if c.State().Strategy != StrategyNone {
		t.Fatal("Edit Servers OK must clear the strategy")
	}
}

// E-D3: plugin servers are left out and counted; a strategy without members
// is refused and the current server keeps working.
func TestStrategyExcludesPluginServers(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(func(c *config.Config) {
		p := e.server()
		p.Plugin = "no-such-plugin"
		c.Configs = append(c.Configs, p)
		// The plugin server is current: under a strategy its plugin is not needed.
		c.Index, c.Strategy, c.Enabled = 1, config.StrategyBalancing, true
	})
	c := e.start()
	if s := c.State(); s.StrategyExcluded != 1 || s.Strategy != StrategyBalancing || s.Mode != Global {
		t.Fatalf("state %+v", s)
	}
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
	if err := c.SelectServer(1); err == nil {
		t.Fatal("missing plugin must still fail when the server is selected")
	}
}

func TestStrategyWithoutMembers(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(func(c *config.Config) { c.Configs[0].Plugin = "no-such-plugin"; c.Enabled = false })
	c, err := New(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	t.Cleanup(c.Shutdown)
	if err := c.SetStrategy(StrategyHA); !errors.Is(err, ErrNoStrategyMembers) {
		t.Fatalf("err = %v", err)
	}
	if c.State().Strategy != StrategyNone || e.diskConfig().Strategy != "" {
		t.Fatal("strategy without members was kept")
	}
}

// A saved strategy whose members are gone falls back to the current server
// and tells the user once.
func TestLoadedStrategyWithoutMembersNotifies(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(func(c *config.Config) { c.Configs[0].Plugin = "no-such-plugin"; c.Strategy = config.StrategyBalancing })
	c, err := New(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	t.Cleanup(c.Shutdown)
	if c.State().Strategy != StrategyNone {
		t.Fatal("inactive strategy shown as on")
	}
	if n := c.TakeNotice(); !errors.Is(n, ErrNoStrategyMembers) {
		t.Fatalf("notice %v", n)
	}
}

func TestToggleSystemProxyRemembersMode(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(func(c *config.Config) { c.Global = false })
	c := e.start()
	if err := c.ToggleSystemProxy(); err != nil || c.State().Mode != Rules {
		t.Fatalf("first enable: %v %v", err, c.State().Mode)
	}
	if err := c.SetMode(Global); err != nil {
		t.Fatal(err)
	}
	if err := c.ToggleSystemProxy(); err != nil || c.State().Mode != Disabled || e.sys.Cur != userProxy {
		t.Fatalf("disable: %v %v", err, c.State().Mode)
	}
	if err := c.ToggleSystemProxy(); err != nil || c.State().Mode != Global {
		t.Fatalf("re-enable: %v %v", err, c.State().Mode)
	}
}

func TestToggleProxyMode(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	for _, want := range []Mode{Rules, Global, Rules} {
		if err := c.ToggleProxyMode(); err != nil || c.State().Mode != want {
			t.Fatalf("want %v: %v %v", want, err, c.State().Mode)
		}
	}
}

func TestToggleShareLAN(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	if err := c.ToggleShareLAN(); err != nil || !c.State().ShareLAN {
		t.Fatal(err)
	}
	if err := c.ToggleShareLAN(); err != nil || c.State().ShareLAN {
		t.Fatal(err)
	}
}

func TestSelectRelativeWraps(t *testing.T) {
	e := newEnv(t)
	e.twoServers(func(c *config.Config) { c.Strategy = config.StrategyBalancing })
	c := e.start()
	if err := c.SelectRelative(-1); err != nil || c.State().Current != 1 || c.State().Strategy != StrategyNone {
		t.Fatalf("up from 0: %v %+v", err, c.State())
	}
	if err := c.SelectRelative(1); err != nil || c.State().Current != 0 {
		t.Fatalf("down from 1: %v", err)
	}
	if err := c.SelectRelative(1); err != nil || c.State().Current != 1 {
		t.Fatal(err)
	}
}

func TestSelectRelativeWithoutServers(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(func(c *config.Config) { c.Configs = nil }) // a free port, not the real 1080
	c := e.start()
	if err := c.SelectRelative(1); !errors.Is(err, ErrNoServer) {
		t.Fatalf("err = %v", err)
	}
}

// Hotkeys pressed rapidly, from several goroutines: every toggle applies.
func TestRapidToggles(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { c.ToggleShareLAN() })
	}
	wg.Wait()
	if c.State().ShareLAN {
		t.Fatal("an even number of toggles left LAN on")
	}
}

func TestSaveHotkeys(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	h := config.HotkeyConfig{SwitchSystemProxy: "Ctrl+Shift+Q", RegHotkeysAtStartup: true}
	if err := c.SaveHotkeys(h); err != nil {
		t.Fatal(err)
	}
	if got := c.Hotkeys(); got.SwitchSystemProxy != "Ctrl+Shift+Q" || !got.RegHotkeysAtStartup {
		t.Fatalf("hotkeys %+v", got)
	}
	if d := e.diskConfig().Hotkey; d.SwitchSystemProxy != "Ctrl+Shift+Q" || !d.RegHotkeysAtStartup {
		t.Fatalf("disk %+v", d)
	}
}

// Final review #2: under a strategy the core carries traffic even when the
// remembered server uses a (stopped) plugin, so subscriptions go through it.
func TestFetchPortUnderStrategy(t *testing.T) {
	e, _ := pluginEnv(t)
	e.writeConfig(func(c *config.Config) {
		c.Configs = append([]config.Server{e.pluginServer("")}, c.Configs...) // remembered: plugin server
		c.Index = 0
		c.Strategy = config.StrategyBalancing
	})
	c := e.start()
	if p := c.fetchPort(); p != c.State().Port {
		t.Fatalf("fetch port %d, want the local port %d", p, c.State().Port)
	}
}
