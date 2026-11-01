// Package controller is ssmygo's state centre: it owns gui-config.json and
// drives the core, the system proxy and the rules. It does not use MyGo;
// subscribers are called synchronously on the caller's goroutine.
package controller

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/database64128/shadowsocks-go/tslog"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core"
	"github.com/thc5787/ssmygo/internal/rules"
	"github.com/thc5787/ssmygo/internal/sysproxy"
)

// Deps are the controller's collaborators.
type Deps struct {
	Paths        config.Paths // current data root
	PortableRoot string       // exe directory
	RoamingRoot  string       // %AppData%\ssmygo
	Sys          sysproxy.Proxy
	Logger       *slog.Logger
	DLC          []byte // geosite dlc.dat
}

// Controller serialises every change to the configuration.
type Controller struct {
	mu      sync.Mutex
	d       Deps
	paths   config.Paths
	cfg     *config.Config
	runner  *core.Runner
	rules   *rules.Result
	lastErr string
	started bool

	subMu  sync.Mutex
	subs   map[int]func(State)
	nextID int
}

// New loads the configuration. A corrupt file is backed up and replaced by
// defaults; the backup is reported through State().LastError.
func New(d Deps) (*Controller, error) {
	c := &Controller{
		d:     d,
		paths: d.Paths,
		// Warn: at Info the core logs every connection's destination, which
		// would write a browsing history next to a portable exe.
		runner: core.NewRunner(tslog.Config{Level: slog.LevelWarn}.NewLoggerWithHandler(d.Logger.Handler())),
		subs:   map[int]func(State){},
	}
	cfg, err := config.Load(d.Paths.Config())
	if errors.Is(err, config.ErrCorrupt) {
		bak, berr := config.BackupCorrupt(d.Paths.Config(), time.Now())
		if berr != nil {
			return nil, errors.Join(err, berr)
		}
		d.Logger.Error("config corrupt, using defaults", "backup", bak, "err", err)
		c.lastErr = "gui-config.json was corrupt and was backed up to " + filepath.Base(bak)
		cfg, err = config.Default(), nil
	}
	if err != nil {
		return nil, err
	}
	c.cfg = cfg
	return c, nil
}

// Start builds the rules, starts the core and applies the system proxy.
func (c *Controller) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.started = true
	c.recoverCrash()

	rs, err := rules.Build(c.d.DLC, c.cfg.GeositeDirectGroups, c.cfg.GeositeProxiedGroups, c.paths.DomainSets())
	if err != nil {
		c.d.Logger.Error("rules unavailable, rules mode routes everything through the server", "err", err)
		c.lastErr = "rules unavailable: " + err.Error()
	} else {
		c.rules = &rs
	}
	// Return the cold rules build's garbage to the OS right away (tray-only
	// working-set target is 40 MB); cheap, and a no-op on warm starts.
	debug.FreeOSMemory()

	coreErr := c.applyCore(c.cfg)
	if c.cfg.Enabled && (coreErr != nil || !usable(c.cfg)) {
		// Never leave the system proxy on a port we cannot serve, nor point it
		// at a core that would send everything direct.
		reason := coreErr
		if reason == nil {
			reason = ErrNoServer
		}
		next := c.cfg.Clone()
		next.Enabled = false
		if next.Sysproxy.Recorded {
			if err := c.d.Sys.Set(recorded(next.Sysproxy)); err != nil {
				c.d.Logger.Error("restore system proxy", "err", err)
			} else {
				next.Sysproxy = config.SysproxyRecord{}
			}
		}
		c.cfg = next
		c.save()
		c.lastErr = reason.Error()
		c.notifyLocked()
		return reason
	}
	if coreErr != nil {
		c.lastErr = coreErr.Error()
		c.notifyLocked()
		return coreErr
	}
	if c.cfg.Enabled {
		next := c.cfg.Clone()
		if err := c.applySysproxy(c.cfg, next); err != nil {
			next.Enabled = false
			c.cfg = next
			c.save()
			c.lastErr = err.Error()
			c.notifyLocked()
			return err
		}
		c.cfg = next
		c.save()
	}
	c.notifyLocked()
	return nil
}

// recoverCrash restores the user's proxy left pointing at us by a run that
// ended without disabling it.
func (c *Controller) recoverCrash() {
	rec := c.cfg.Sysproxy
	if c.cfg.Enabled || !rec.Recorded {
		return
	}
	if cur, err := c.d.Sys.Get(); err == nil && sysproxy.PointsTo(cur, c.cfg.LocalPort) {
		if err := c.d.Sys.Set(recorded(rec)); err != nil {
			c.d.Logger.Error("restore system proxy after crash", "err", err)
			return
		}
	}
	c.cfg.Sysproxy = config.SysproxyRecord{}
	c.save()
}

func recorded(r config.SysproxyRecord) sysproxy.Settings {
	return sysproxy.Settings{Flags: r.Flags, Server: r.Server, Bypass: r.Bypass, PacURL: r.PacURL}
}

func (c *Controller) applyCore(cfg *config.Config) error {
	sc, err := core.Build(cfg, c.rules)
	if err != nil && !errors.Is(err, core.ErrNoServer) {
		return err
	}
	return c.runner.Apply(sc)
}

// applySysproxy moves the system proxy from prev's state to next's,
// recording the user's settings into next before the first change.
func (c *Controller) applySysproxy(prev, next *config.Config) error {
	switch {
	case next.Enabled:
		if !next.Sysproxy.Recorded {
			cur, err := c.d.Sys.Get()
			if err != nil {
				return fmt.Errorf("read system proxy: %w", err)
			}
			next.Sysproxy = config.SysproxyRecord{Recorded: true, Flags: cur.Flags, Server: cur.Server, Bypass: cur.Bypass, PacURL: cur.PacURL}
			if sysproxy.PointsTo(cur, next.LocalPort) || sysproxy.PointsTo(cur, prev.LocalPort) {
				// Already ours (e.g. left by the original client on the same
				// port): restoring it later would leave a dead proxy.
				next.Sysproxy = config.SysproxyRecord{Recorded: true, Flags: sysproxy.FlagDirect, Bypass: cur.Bypass}
			}
		}
		if err := c.d.Sys.Set(sysproxy.LocalSettings(next.LocalPort, next.Sysproxy.Bypass)); err != nil {
			return fmt.Errorf("set system proxy: %w", err)
		}
	case prev.Enabled && next.Sysproxy.Recorded:
		if err := c.d.Sys.Set(recorded(next.Sysproxy)); err != nil {
			return fmt.Errorf("restore system proxy: %w", err)
		}
		next.Sysproxy = config.SysproxyRecord{}
	}
	return nil
}

// commit applies next (core, then system proxy), saves it and notifies. On
// failure everything returns to the previous configuration.
func (c *Controller) commit(next *config.Config) error {
	prev := c.cfg
	if prev.Enabled && !next.Enabled {
		// Disabling: give the user their proxy back first, even if the core
		// then fails (e.g. the port was taken) — never stay on a dead port.
		if err := c.applySysproxy(prev, next); err != nil {
			return c.fail(err)
		}
		coreErr := c.applyCore(next)
		c.cfg = next
		c.save()
		if coreErr != nil {
			return c.fail(coreErr)
		}
		c.lastErr = ""
		c.notifyLocked()
		return nil
	}
	if err := c.applyCore(next); err != nil {
		c.rollbackCore(prev)
		return c.fail(err)
	}
	if err := c.applySysproxy(prev, next); err != nil {
		c.rollbackCore(prev)
		return c.fail(err)
	}
	c.cfg = next
	c.lastErr = ""
	if err := c.save(); err != nil {
		c.lastErr = "save gui-config.json: " + err.Error()
		c.notifyLocked()
		return err
	}
	c.notifyLocked()
	return nil
}

func (c *Controller) rollbackCore(prev *config.Config) {
	if err := c.applyCore(prev); err != nil {
		c.d.Logger.Error("rollback failed", "err", err)
	}
}

func (c *Controller) fail(err error) error {
	c.d.Logger.Error("change rejected", "err", err)
	c.lastErr = err.Error()
	c.notifyLocked()
	return err
}

func (c *Controller) save() error {
	if err := os.MkdirAll(c.paths.Root, 0o755); err != nil {
		return err
	}
	return c.cfg.Save(c.paths.Config())
}

// ErrNoServer is returned when enabling the system proxy without a usable server.
var ErrNoServer = errors.New("no server configured")

// SetMode switches between Disabled, Rules ("PAC") and Global.
func (c *Controller) SetMode(m Mode) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := c.cfg.Clone()
	next.Enabled = m != Disabled
	if m != Disabled {
		next.Global = m == Global
		if !usable(next) {
			return c.fail(ErrNoServer)
		}
	}
	return c.commit(next)
}

// usable reports whether cfg's current server can carry traffic.
func usable(cfg *config.Config) bool {
	s, ok := cfg.Current()
	return ok && len(s.Validate()) == 0
}

// SelectServer makes server i current. While the system proxy is on, an
// unusable server is refused: the core would silently send traffic direct.
func (c *Controller) SelectServer(i int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i < 0 || i >= len(c.cfg.Configs) {
		return c.fail(fmt.Errorf("no server %d", i))
	}
	next := c.cfg.Clone()
	next.Index = i
	if next.Enabled && !usable(next) {
		return c.fail(fmt.Errorf("server %q is not usable: %v", next.Configs[i].Title(), next.Configs[i].Validate()[0]))
	}
	return c.commit(next)
}

// SetShareLAN listens on all interfaces when on.
func (c *Controller) SetShareLAN(on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := c.cfg.Clone()
	next.ShareOverLan = on
	return c.commit(next)
}

// SaveServers applies the Edit Servers window. A server that does not
// validate is returned as its config.FieldError.
func (c *Controller) SaveServers(servers []config.Server, index, port int, portable bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range servers {
		if errs := s.Validate(); len(errs) > 0 {
			return errs[0]
		}
	}
	if port < 1 || port > 65535 {
		return config.FieldError{Field: "localPort", Msg: "1–65535"}
	}
	next := c.cfg.Clone()
	next.Configs = append([]config.Server{}, servers...)
	next.Index = index
	if index < 0 || index >= len(servers) {
		next.Index = 0
	}
	next.LocalPort = port
	next.FirstRun = false
	moving := portable != c.cfg.PortableMode
	next.PortableMode = portable
	if err := c.commit(next); err != nil {
		return err
	}
	if moving {
		return c.movePortable(portable)
	}
	return nil
}

// movePortable switches the data root so the next launch finds the config:
// portable → exe dir (portableMode true there); roaming → AppData, leaving
// a portableMode=false marker in the exe dir when one exists there.
func (c *Controller) movePortable(portable bool) error {
	from := c.paths
	to := config.Paths{Root: c.d.RoamingRoot}
	if portable {
		to = config.Paths{Root: c.d.PortableRoot}
	}
	if to.Root == "" || to.Root == from.Root {
		return nil
	}
	c.paths = to
	if err := c.save(); err != nil {
		c.paths = from
		return c.fail(fmt.Errorf("move config: %w", err))
	}
	// The old file now only tells ResolveRoot where to look (portableMode
	// already holds the new value), so it is kept as written by commit.
	c.notifyLocked()
	return nil
}

// ImportURLs appends the servers of every ss:// link in text.
func (c *Controller) ImportURLs(text string) (int, []error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	servers, errs := config.ParseURLs(text)
	if len(servers) == 0 {
		return 0, errs
	}
	next := c.cfg.Clone()
	next.Configs = append(next.Configs, servers...)
	next.FirstRun = false
	if err := c.commit(next); err != nil {
		return 0, append(errs, err)
	}
	return len(servers), errs
}

// Config returns a copy for editing.
func (c *Controller) Config() *config.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.Clone()
}

// State returns the current state.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stateLocked()
}

func (c *Controller) stateLocked() State {
	s := State{
		Current:   c.cfg.Index,
		ShareLAN:  c.cfg.ShareOverLan,
		LastError: c.lastErr,
		Port:      c.cfg.LocalPort,
		FirstRun:  c.cfg.FirstRun,
	}
	switch {
	case !c.cfg.Enabled:
		s.Mode = Disabled
	case c.cfg.Global:
		s.Mode = Global
	default:
		s.Mode = Rules
	}
	for _, srv := range c.cfg.Configs {
		s.Servers = append(s.Servers, srv.Title())
	}
	return s
}

// Subscribe calls fn with the new State after every change.
func (c *Controller) Subscribe(fn func(State)) (off func()) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	id := c.nextID
	c.nextID++
	c.subs[id] = fn
	return func() {
		c.subMu.Lock()
		defer c.subMu.Unlock()
		delete(c.subs, id)
	}
}

func (c *Controller) notifyLocked() {
	s := c.stateLocked()
	c.subMu.Lock()
	fns := make([]func(State), 0, len(c.subs))
	for _, fn := range c.subs {
		fns = append(fns, fn)
	}
	c.subMu.Unlock()
	for _, fn := range fns {
		fn(s)
	}
}

// Shutdown restores the user's system proxy (keeping Enabled for the next
// launch, as the original client does), saves and stops the core. It is
// safe to call more than once.
func (c *Controller) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started {
		return
	}
	c.started = false
	if c.cfg.Enabled && c.cfg.Sysproxy.Recorded {
		if err := c.d.Sys.Set(recorded(c.cfg.Sysproxy)); err != nil {
			c.d.Logger.Error("restore system proxy on exit", "err", err)
		} else {
			c.cfg.Sysproxy = config.SysproxyRecord{}
		}
	}
	if err := c.save(); err != nil {
		c.d.Logger.Error("save on exit", "err", err)
	}
	c.runner.Stop()
}
