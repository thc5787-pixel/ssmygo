// Package controller is ssmygo's state centre: it owns gui-config.json and
// drives the core, the system proxy and the rules. It does not use MyGo;
// subscribers are called on the caller's goroutine after its lock is released.
package controller

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
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
	lastErr error // shown in the tray; typed so the UI can translate it
	dirty   bool  // state changed: subscribers are told after the lock is released
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
		c.lastErr = fmt.Errorf("gui-config.json was corrupt and was backed up to %s", filepath.Base(bak))
		cfg, err = config.Default(), nil
	}
	if err != nil {
		return nil, err
	}
	c.cfg = cfg
	return c, nil
}

// Errors the UI translates (deferred #4).
var (
	// ErrNoServer: enabling the system proxy without a usable server.
	ErrNoServer = errors.New("no server configured")
	// ErrPluginUnsupported: SIP003 plugins arrive in P1.
	ErrPluginUnsupported = errors.New("plugins are not supported in this version")
)

// PortInUseError: the local proxy port could not be listened on.
type PortInUseError struct {
	Port int
	Err  error
}

func (e *PortInUseError) Error() string { return fmt.Sprintf("port %d is in use: %v", e.Port, e.Err) }
func (e *PortInUseError) Unwrap() error { return e.Err }

// SaveError: gui-config.json could not be written.
type SaveError struct{ Err error }

func (e *SaveError) Error() string { return "save gui-config.json: " + e.Err.Error() }
func (e *SaveError) Unwrap() error { return e.Err }

// unlock releases the lock and then tells subscribers about a change, so
// they may call back into the controller (deferred #8④).
func (c *Controller) unlock() {
	pending := c.dirty
	c.dirty = false
	var s State
	if pending {
		s = c.stateLocked()
	}
	c.mu.Unlock()
	if pending {
		c.publish(s)
	}
}

// Start builds the rules, starts the core and applies the system proxy.
func (c *Controller) Start() error {
	c.mu.Lock()
	defer c.unlock()
	c.started = true
	c.recoverCrash()

	rs, err := rules.Build(c.d.DLC, c.cfg.GeositeDirectGroups, c.cfg.GeositeProxiedGroups, c.paths.DomainSets())
	if err != nil {
		c.d.Logger.Error("rules unavailable, rules mode routes everything through the server", "err", err)
		c.lastErr = fmt.Errorf("rules unavailable: %w", err)
	} else {
		c.rules = &rs
		if len(rs.Missing) > 0 {
			c.d.Logger.Warn("unknown geosite groups skipped", "groups", rs.Missing)
			c.lastErr = fmt.Errorf("unknown geosite groups skipped: %v", rs.Missing)
		}
	}
	// Return the cold rules build's garbage to the OS right away (tray-only
	// working-set target is 40 MB); cheap, and a no-op on warm starts.
	debug.FreeOSMemory()

	coreErr := c.applyCore(c.cfg)
	if c.cfg.Enabled && (coreErr != nil || unusable(c.cfg) != nil) {
		// Never leave the system proxy on a port we cannot serve, nor point it
		// at a core that would send everything direct.
		reason := coreErr
		if reason == nil {
			reason = unusable(c.cfg)
		}
		next := c.cfg.Clone()
		next.Enabled = false
		if next.Sysproxy.Recorded {
			if err := c.setSys(recorded(next.Sysproxy)); err != nil {
				c.d.Logger.Error("restore system proxy", "err", err)
			} else {
				next.Sysproxy = config.SysproxyRecord{}
			}
		}
		c.cfg = next
		c.save()
		c.lastErr = reason
		c.dirty = true
		return reason
	}
	if coreErr != nil {
		c.lastErr = coreErr
		c.dirty = true
		return coreErr
	}
	if c.cfg.Enabled {
		next := c.cfg.Clone()
		if err := c.applySysproxy(c.cfg, next); err != nil {
			next.Enabled = false
			c.cfg = next
			c.save()
			c.lastErr = err
			c.dirty = true
			return err
		}
		c.cfg = next
		c.save()
	}
	c.dirty = true
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
		if err := c.setSys(recorded(rec)); err != nil {
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

// setSys applies s; a failure on some VPN/dial-up entries only is logged as
// a warning — the LAN setting, which browsers use, was applied (deferred #5).
func (c *Controller) setSys(s sysproxy.Settings) error {
	err := c.d.Sys.Set(s)
	var partial *sysproxy.PartialError
	if errors.As(err, &partial) {
		c.d.Logger.Warn("system proxy not applied to some connections", "err", err)
		return nil
	}
	return err
}

// applyCore rebuilds the core for cfg. A listen failure becomes a
// *PortInUseError.
func (c *Controller) applyCore(cfg *config.Config) error {
	sc, err := core.Build(cfg, c.rules, 0)
	if err != nil && !errors.Is(err, core.ErrNoServer) {
		return err
	}
	err = c.runner.Apply(sc)
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "listen" {
		return &PortInUseError{Port: cfg.LocalPort, Err: err}
	}
	return err
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
		if err := c.setSys(sysproxy.LocalSettings(next.LocalPort, next.Sysproxy.Bypass)); err != nil {
			return fmt.Errorf("set system proxy: %w", err)
		}
	case prev.Enabled && next.Sysproxy.Recorded:
		if err := c.setSys(recorded(next.Sysproxy)); err != nil {
			return fmt.Errorf("restore system proxy: %w", err)
		}
		next.Sysproxy = config.SysproxyRecord{}
	}
	return nil
}

// commit applies next (core, then system proxy), saves it and marks the
// state changed. On failure everything returns to the previous configuration.
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
		c.lastErr = nil
		c.dirty = true
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
	c.lastErr = nil
	c.dirty = true
	if err := c.save(); err != nil {
		c.lastErr = &SaveError{Err: err}
		return c.lastErr
	}
	return nil
}

func (c *Controller) rollbackCore(prev *config.Config) {
	if err := c.applyCore(prev); err != nil {
		c.d.Logger.Error("rollback failed", "err", err)
	}
}

func (c *Controller) fail(err error) error {
	c.d.Logger.Error("change rejected", "err", err)
	c.lastErr = err
	c.dirty = true
	return err
}

func (c *Controller) save() error {
	if err := os.MkdirAll(c.paths.Root, 0o755); err != nil {
		return err
	}
	return c.cfg.Save(c.paths.Config())
}

// unusable says why cfg's current server cannot carry traffic, or nil.
func unusable(cfg *config.Config) error {
	s, ok := cfg.Current()
	switch {
	case !ok:
		return ErrNoServer
	case s.Plugin != "":
		return ErrPluginUnsupported
	}
	if errs := s.Validate(); len(errs) > 0 {
		return fmt.Errorf("server %q: %w", s.Title(), errs[0])
	}
	return nil
}

// SetMode switches between Disabled, Rules ("PAC") and Global.
func (c *Controller) SetMode(m Mode) error {
	c.mu.Lock()
	defer c.unlock()
	next := c.cfg.Clone()
	next.Enabled = m != Disabled
	if m != Disabled {
		next.Global = m == Global
		if err := unusable(next); err != nil {
			return c.fail(err)
		}
	}
	return c.commit(next)
}

// SelectServer makes server i current. While the system proxy is on, an
// unusable server is refused: the core would silently send traffic direct.
func (c *Controller) SelectServer(i int) error {
	c.mu.Lock()
	defer c.unlock()
	if i < 0 || i >= len(c.cfg.Configs) {
		return c.fail(fmt.Errorf("no server %d", i))
	}
	next := c.cfg.Clone()
	next.Index = i
	if next.Enabled {
		if err := unusable(next); err != nil {
			return c.fail(err)
		}
	}
	return c.commit(next)
}

// SetShareLAN listens on all interfaces when on.
func (c *Controller) SetShareLAN(on bool) error {
	c.mu.Lock()
	defer c.unlock()
	next := c.cfg.Clone()
	next.ShareOverLan = on
	return c.commit(next)
}

// SaveServers applies the Edit Servers window. A new or changed server that
// does not validate is returned as its config.FieldError; invalid servers
// already in the configuration and passed back unchanged are kept as they
// are (deferred #3).
func (c *Controller) SaveServers(servers []config.Server, index, port int, portable bool) error {
	c.mu.Lock()
	defer c.unlock()
	for _, s := range servers {
		if c.unchanged(s) {
			continue
		}
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
	if next.Enabled {
		if err := unusable(next); err != nil {
			return c.fail(err)
		}
	}
	next.LocalPort = port
	next.FirstRun = false
	next.PortableMode = portable
	if portable == c.cfg.PortableMode {
		return c.commit(next)
	}
	return c.commitMoving(next, portable)
}

// unchanged reports whether s is exactly one of the current servers.
func (c *Controller) unchanged(s config.Server) bool {
	for _, o := range c.cfg.Configs {
		if reflect.DeepEqual(o, s) {
			return true
		}
	}
	return false
}

// commitMoving commits next into the other data root (deferred #2): the new
// location is written first; only then is the old file reduced to what
// ResolveRoot needs — a {"portableMode":false} marker in the exe dir, or
// nothing in AppData. If the new location cannot be written, nothing moves.
func (c *Controller) commitMoving(next *config.Config, portable bool) error {
	from := c.paths
	to := config.Paths{Root: c.d.RoamingRoot}
	if portable {
		to = config.Paths{Root: c.d.PortableRoot}
	}
	if to.Root == "" || to.Root == from.Root {
		return c.commit(next)
	}
	c.paths = to
	if err := c.commit(next); err != nil {
		var se *SaveError
		if errors.As(err, &se) {
			// Applied but not written: keep the old root and the old mode.
			c.paths = from
			c.cfg.PortableMode = !portable
			c.save()
		} else {
			c.paths = from
		}
		return err
	}
	old := from.Config()
	if portable {
		os.Remove(old) // roaming copy: ResolveRoot now finds the exe-dir file
	} else if err := os.WriteFile(old, []byte("{\"portableMode\": false}\n"), 0o644); err != nil {
		c.d.Logger.Warn("could not reduce the old portable config to a marker", "err", err)
	}
	return nil
}

// ImportURLs appends the servers of every ss:// link in text and returns
// those added (the Edit Servers window merges them if open, deferred #7).
func (c *Controller) ImportURLs(text string) ([]config.Server, []error) {
	c.mu.Lock()
	defer c.unlock()
	servers, errs := config.ParseURLs(text)
	if len(servers) == 0 {
		return nil, errs
	}
	next := c.cfg.Clone()
	next.Configs = append(next.Configs, servers...)
	next.FirstRun = false
	if err := c.commit(next); err != nil {
		return nil, append(errs, err)
	}
	return servers, errs
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
		Current:  c.cfg.Index,
		ShareLAN: c.cfg.ShareOverLan,
		Err:      c.lastErr,
		Port:     c.cfg.LocalPort,
		FirstRun: c.cfg.FirstRun,
	}
	if c.lastErr != nil {
		s.LastError = c.lastErr.Error()
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

// Subscribe calls fn with the new State after every change, outside the
// controller's lock.
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

func (c *Controller) publish(s State) {
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
		if err := c.setSys(recorded(c.cfg.Sysproxy)); err != nil {
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
