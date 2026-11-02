package controller

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/geosite"
	"github.com/thc5787/ssmygo/internal/rules"
	"github.com/thc5787/ssmygo/internal/subscribe"
)

// Seams for tests.
var (
	geositeClient    = subscribe.Client
	now              = time.Now
	userRulePoll     = 2 * time.Second // D-D4
	geositeAutoCheck = true            // D-D1: the startup check
)

// geositeMaxAge: older than this, the startup check updates geosite (D-D1).
const geositeMaxAge = 7 * 24 * time.Hour

// GeositeUpdatedNotice: the startup check adopted a new geosite.
type GeositeUpdatedNotice struct{}

func (*GeositeUpdatedNotice) Error() string { return "geosite updated" }

// GeositeUpdateError: a geosite update failed; the current rules stay.
// Background is set for the startup check. Err wraps the geosite errors
// (geosite.ErrChecksum, *geosite.MissingGroupsError, …).
type GeositeUpdateError struct {
	Err        error
	Background bool
}

func (e *GeositeUpdateError) Error() string { return "geosite update failed: " + e.Err.Error() }
func (e *GeositeUpdateError) Unwrap() error { return e.Err }

// UserRulesSkippedError: user-rule.txt lines that were not understood
// (1-based) were skipped.
type UserRulesSkippedError struct{ Lines []int }

func (e *UserRulesSkippedError) Error() string {
	return fmt.Sprintf("user-rule.txt: %d unsupported rules skipped (lines %v)", len(e.Lines), e.Lines)
}

// UserRulesReadError: user-rule.txt could not be read; the previous rules stay.
type UserRulesReadError struct{ Err error }

func (e *UserRulesReadError) Error() string { return "read user-rule.txt: " + e.Err.Error() }
func (e *UserRulesReadError) Unwrap() error { return e.Err }

// userRuleTemplate is written by EnsureUserRuleFile: comments only.
const userRuleTemplate = `! ssmygo user rules: one rule per line; they take priority over geosite.
! Lines starting with ! are comments.
!
! ||example.com^              example.com and its subdomains through the proxy
! @@||example.com^            example.com and its subdomains direct
! |https://example.com/path   the host of the URL through the proxy (@@ for direct)
! example.com                 same as ||example.com^
!
! Regular expressions (/.../), paths, wildcards (*) and $options are not
! supported: such lines are skipped. Changes apply within a few seconds.
`

// UserRulePath is user-rule.txt in the current data directory.
func (c *Controller) UserRulePath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paths.UserRule()
}

// EnsureUserRuleFile creates user-rule.txt with a commented template when
// it does not exist, and returns its path.
func (c *Controller) EnsureUserRuleFile() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.paths.UserRule()
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return path, nil
	}
	if err != nil {
		return path, err
	}
	if _, err := f.WriteString(userRuleTemplate); err != nil {
		f.Close()
		return path, err
	}
	return path, f.Close()
}

// readUserRules returns user-rule.txt's text; a missing file has no rules.
func readUserRules(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// ruleInputs is what a rules build needs from the configuration.
type ruleInputs struct {
	direct, proxied []string
	outDir          string
}

func (c *Controller) ruleInputsLocked() ruleInputs {
	return ruleInputs{
		direct:  slices.Clone(c.cfg.GeositeDirectGroups),
		proxied: slices.Clone(c.cfg.GeositeProxiedGroups),
		outDir:  c.paths.DomainSets(),
	}
}

func (in ruleInputs) build(dlc []byte, user string) (rules.Result, error) {
	return rules.Build(dlc, in.direct, in.proxied, user, in.outDir)
}

func (c *Controller) dlcPath() string { return filepath.Join(c.paths.Root, "dlc.dat") }

// startRulesLocked builds the rules at Start. The geosite is <root>/dlc.dat
// from an earlier update when it builds with every configured group, else
// the built-in one.
func (c *Controller) startRulesLocked() {
	in := c.ruleInputsLocked()
	user, uerr := readUserRules(c.paths.UserRule())
	if uerr != nil {
		c.d.Logger.Warn("user-rule.txt unreadable, ignored", "err", uerr)
		c.addNotice(&UserRulesReadError{Err: uerr})
		c.userRuleFailed = true
	}
	c.userText = user
	var rs rules.Result
	var err error
	built := false
	if saved, rerr := os.ReadFile(c.dlcPath()); rerr == nil {
		rs, err = in.build(saved, user)
		if err == nil && len(rs.Missing) == 0 {
			c.dlc, built = saved, true
		} else {
			c.d.Logger.Warn("saved dlc.dat unusable, using the built-in geosite", "err", err, "missing", rs.Missing)
		}
	}
	if !built {
		c.dlc = c.d.DLC
		rs, err = in.build(c.dlc, user)
	}
	if err != nil {
		c.d.Logger.Error("rules unavailable, rules mode routes everything through the server", "err", err)
		c.lastErr = fmt.Errorf("rules unavailable: %w", err)
		return
	}
	c.adoptRulesLocked(&rs)
	if len(rs.Missing) > 0 {
		c.d.Logger.Warn("unknown geosite groups skipped", "groups", rs.Missing)
		c.lastErr = fmt.Errorf("unknown geosite groups skipped: %v", rs.Missing)
	}
}

// adoptRulesLocked makes rs current and reports newly skipped user rules.
func (c *Controller) adoptRulesLocked(rs *rules.Result) {
	c.rules = rs
	if len(rs.UserSkipped) > 0 && !slices.Equal(rs.UserSkipped, c.userSkipped) {
		c.d.Logger.Warn("user-rule.txt: unsupported rules skipped", "lines", rs.UserSkipped)
		c.addNotice(&UserRulesSkippedError{Lines: rs.UserSkipped})
	}
	c.userSkipped = rs.UserSkipped
}

// reapplyLocked restarts the core with the current rules; in global mode
// the core does not use them, so nothing happens. The system proxy is not
// touched either way.
func (c *Controller) reapplyLocked() {
	if c.cfg.Global {
		return
	}
	if err := c.applyCore(c.cfg, c.pluginPort()); err != nil {
		c.d.Logger.Error("apply new rules", "err", err)
		c.lastErr = err
	}
	c.dirty = true
}

// UpdateGeosite downloads geosite (D-D2: through the local port when the
// current server can carry traffic, else direct) and adopts it: saves
// <root>/dlc.dat, rebuilds the rules and re-applies the core. changed is
// false when the download equals the current geosite. On failure
// (*GeositeUpdateError) the current rules and files stay as they are.
func (c *Controller) UpdateGeosite(ctx context.Context) (changed bool, err error) {
	return c.updateGeosite(ctx, false)
}

func (c *Controller) updateGeosite(ctx context.Context, background bool) (bool, error) {
	fail := func(err error) (bool, error) { return false, &GeositeUpdateError{Err: err, Background: background} }
	c.rulesMu.Lock() // one rules build at a time: they share the gob files
	defer c.rulesMu.Unlock()
	c.mu.Lock()
	rawURL, sumURL := c.cfg.GeositeURL, c.cfg.GeositeSha256sumURL
	in := c.ruleInputsLocked()
	cur, user, dlcPath := c.dlc, c.userText, c.dlcPath()
	c.mu.Unlock()

	src, _ := geosite.Sources(rawURL, sumURL)
	client := geositeClient(c.fetchPort(), geosite.Timeout)
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, geosite.Timeout)
	defer cancel()
	data, err := geosite.Fetch(ctx, rawURL, sumURL, client, append(slices.Clone(in.direct), in.proxied...))
	if err != nil {
		c.d.Logger.Warn("geosite update failed", "host", geosite.Host(src), "err", err)
		return fail(err)
	}
	if geosite.Sum(data) == geosite.Sum(cur) {
		c.d.Logger.Info("geosite is up to date", "host", geosite.Host(src))
		c.mu.Lock()
		defer c.unlock()
		c.touchGeositeLocked()
		return false, nil
	}
	// Written aside first: nothing is replaced until everything worked.
	tmp := dlcPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(dlcPath), 0o755); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		os.Remove(tmp)
		return fail(err)
	}
	rs, err := in.build(data, user)
	if err == nil {
		err = os.Rename(tmp, dlcPath)
	}
	if err != nil {
		os.Remove(tmp)
		// The build may have replaced some sets: put the current ones back.
		if _, rerr := in.build(cur, user); rerr != nil {
			c.d.Logger.Error("restore rules", "err", rerr)
		}
		return fail(err)
	}
	c.mu.Lock()
	defer c.unlock()
	c.dlc = data
	c.adoptRulesLocked(&rs)
	c.touchGeositeLocked()
	if c.started {
		c.reapplyLocked()
	}
	c.dirty = true
	c.d.Logger.Info("geosite updated", "host", geosite.Host(src), "bytes", len(data))
	return true, nil
}

// touchGeositeLocked records a successful update check.
func (c *Controller) touchGeositeLocked() {
	next := c.cfg.Clone()
	next.GeositeUpdatedAt = now().UTC().Truncate(time.Second)
	c.cfg = next
	if err := c.save(); err != nil {
		c.d.Logger.Warn("save geosite update time", "err", err)
	}
}

// geositeStale reports whether the startup check should run (D-D1).
func geositeStale(cfg *config.Config) bool {
	return geositeAutoCheck && now().Sub(cfg.GeositeUpdatedAt) > geositeMaxAge
}

// updateGeositeInBackground runs the startup check; its outcome is shown
// once as a notice.
func (c *Controller) updateGeositeInBackground(ctx context.Context) {
	go func() {
		changed, err := c.updateGeosite(ctx, true)
		if ctx.Err() != nil {
			return // shutting down
		}
		c.mu.Lock()
		switch {
		case err != nil:
			c.addNotice(err)
		case changed:
			c.addNotice(&GeositeUpdatedNotice{})
		}
		c.dirty = true
		c.unlock()
	}()
}

// fileStamp is what the user-rule.txt poller compares (D-D4).
type fileStamp struct {
	path   string
	exists bool
	mod    time.Time
	size   int64
}

func stampOf(path string) fileStamp {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{path: path}
	}
	return fileStamp{path: path, exists: true, mod: st.ModTime(), size: st.Size()}
}

// watchUserRules polls user-rule.txt until stop is closed. last is the
// file's stamp taken before Start read it.
func (c *Controller) watchUserRules(stop <-chan struct{}, interval time.Duration, last fileStamp) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		path := c.UserRulePath()
		st := stampOf(path)
		if st == last {
			continue
		}
		// Taken before reading: a write that lands after the read changes
		// the stamp again and is picked up by the next poll.
		last = st
		c.reloadUserRules(path)
	}
}

// reloadUserRules rebuilds the rules from user-rule.txt. A file that
// cannot be read keeps the previous rules (notified once); a missing file
// means no user rules.
func (c *Controller) reloadUserRules(path string) {
	c.rulesMu.Lock()
	defer c.rulesMu.Unlock()
	user, rerr := readUserRules(path)
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return
	}
	if rerr != nil {
		c.d.Logger.Warn("user-rule.txt unreadable, previous rules kept", "err", rerr)
		if !c.userRuleFailed {
			c.userRuleFailed = true
			c.addNotice(&UserRulesReadError{Err: rerr})
			c.dirty = true
		}
		c.unlock()
		return
	}
	c.userRuleFailed = false
	if user == c.userText {
		c.unlock()
		return
	}
	in, dlc := c.ruleInputsLocked(), c.dlc
	c.mu.Unlock()

	rs, err := in.build(dlc, user)

	c.mu.Lock()
	defer c.unlock()
	if !c.started {
		return
	}
	if err != nil {
		c.d.Logger.Error("rebuild rules for user-rule.txt", "err", err)
		c.addNotice(&UserRulesReadError{Err: err})
		c.dirty = true
		return
	}
	c.userText = user
	c.adoptRulesLocked(&rs)
	c.reapplyLocked()
	c.d.Logger.Info("user-rule.txt reloaded", "direct", rs.UserDirect != "", "proxied", rs.UserProxied != "", "skipped", len(rs.UserSkipped))
}
