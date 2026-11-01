package controller

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core/plugin"
)

// PluginNotTrustedError: the plugin program at Path (with this SHA-256) has
// not been confirmed by the user (A1-D2). TrustPlugin, then retry.
type PluginNotTrustedError struct{ Path, SHA256 string }

func (e *PluginNotTrustedError) Error() string {
	return fmt.Sprintf("plugin %s is not trusted yet (sha256 %.16s…)", e.Path, e.SHA256)
}

// procRef lets plugin callbacks recognise the process they belong to; it
// is filled in right after plugin.Start returns.
type procRef struct{ p *plugin.Process }

// preparePlugin makes sure next's current server has its plugin running.
// It returns a newly started process (nil when none was started: no plugin,
// or the running one is reused) and the port to dial.
func (c *Controller) preparePlugin(next *config.Config) (*plugin.Process, int, error) {
	s, ok := next.Current()
	if !ok || s.Plugin == "" {
		return nil, 0, nil
	}
	path, err := plugin.Resolve(s.Plugin, c.d.ExeDir)
	if err != nil {
		return nil, 0, err
	}
	sha, err := plugin.Hash(path)
	if err != nil {
		return nil, 0, err
	}
	if !next.Trusted(path, sha) {
		return nil, 0, &PluginNotTrustedError{Path: path, SHA256: sha}
	}
	spec := plugin.Spec{Path: path, Opts: s.PluginOpts, Args: s.PluginArgs,
		RemoteHost: s.Server, RemotePort: s.ServerPort, Dir: c.d.ExeDir}
	if c.plug != nil && c.plug.Running() && c.plug.Spec() == spec {
		return nil, c.plug.LocalPort(), nil
	}
	ref := &procRef{}
	p, err := plugin.Start(spec, c.d.Logger, verifyAgainst(next.TrustedPlugins),
		func(port int) { c.pluginRestarted(ref, port) },
		func(err error) { c.pluginFailed(ref, err) })
	if err != nil {
		return nil, 0, err
	}
	ref.p = p
	return p, p.LocalPort(), nil
}

// verifyAgainst checks the file before every (re)start against the trust
// list as it was when the process was started (Review Focus 5). It takes no
// lock: it runs both under the controller lock and from plugin goroutines.
func verifyAgainst(list []config.TrustedPlugin) func(string) error {
	snapshot := &config.Config{TrustedPlugins: slices.Clone(list)}
	return func(path string) error {
		sha, err := plugin.Hash(path)
		if err != nil {
			return err
		}
		if !snapshot.Trusted(path, sha) {
			return &PluginNotTrustedError{Path: path, SHA256: sha}
		}
		return nil
	}
}

// adoptPlugin records the plugin state after next was applied.
func (c *Controller) adoptPlugin(next *config.Config, started *plugin.Process) {
	if started != nil {
		c.pluginFailure = nil
		if c.plug != nil {
			c.plug.Stop()
		}
		c.plug = started
		return
	}
	if s, ok := next.Current(); !ok || s.Plugin == "" {
		c.stopPlugin()
	}
}

func (c *Controller) stopPlugin() {
	c.pluginFailure = nil
	if c.plug != nil {
		c.plug.Stop()
		c.plug = nil
	}
}

// pluginPort is the port of the running plugin, 0 if none.
func (c *Controller) pluginPort() int {
	if c.plug != nil && c.plug.Running() {
		return c.plug.LocalPort()
	}
	return 0
}

// pluginRestarted rebuilds the core on the restarted plugin's new port.
func (c *Controller) pluginRestarted(ref *procRef, port int) {
	c.mu.Lock()
	defer c.unlock()
	if ref.p == nil || c.plug != ref.p {
		return
	}
	if err := c.applyCore(c.cfg, port); err != nil {
		c.lastErr = err
	}
	c.dirty = true
}

// pluginFailed reports a plugin that will not be restarted.
func (c *Controller) pluginFailed(ref *procRef, err error) {
	c.mu.Lock()
	defer c.unlock()
	if ref.p == nil || c.plug != ref.p {
		return
	}
	c.d.Logger.Error("plugin stopped", "err", err)
	c.lastErr = err
	c.pluginFailure = err
	c.dirty = true
}

// TrustPlugin records the user's confirmation that the plugin program at
// path with this SHA-256 may run.
func (c *Controller) TrustPlugin(path, sha256 string) error {
	c.mu.Lock()
	defer c.unlock()
	if c.cfg.Trusted(path, sha256) {
		return nil
	}
	c.cfg.TrustedPlugins = append(c.cfg.TrustedPlugins,
		config.TrustedPlugin{Path: filepath.Clean(path), SHA256: strings.ToLower(sha256)})
	c.dirty = true
	if err := c.save(); err != nil {
		return &SaveError{Err: err}
	}
	return nil
}

// SetForwardProxy applies the Forward Proxy window. Invalid settings are
// returned as their config.FieldError and nothing changes.
func (c *Controller) SetForwardProxy(p config.ForwardProxy) error {
	c.mu.Lock()
	defer c.unlock()
	if errs := p.Validate(); len(errs) > 0 {
		return errs[0]
	}
	next := c.cfg.Clone()
	next.SetProxy(p)
	return c.commit(next)
}

// pluginNeeded reports whether going from prev to next requires next's
// plugin to run: a different (or edited) current server, or turning the
// system proxy on.
func pluginNeeded(prev, next *config.Config) bool {
	ns, ok := next.Current()
	if !ok || ns.Plugin == "" {
		return false
	}
	ps, pok := prev.Current()
	if !pok || !reflect.DeepEqual(ps, ns) {
		return true
	}
	return next.Enabled && !prev.Enabled
}
