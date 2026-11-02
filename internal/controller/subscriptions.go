package controller

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"time"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/subscribe"
)

var (
	ErrBadSubscriptionURL    = errors.New("subscription URL must start with http:// or https://")
	ErrDuplicateSubscription = errors.New("subscription already added")
	errSubscriptionRemoved   = errors.New("subscription was removed during the update")
)

// SubResult is the outcome of a subscription's last update in this run.
type SubResult struct {
	Servers, Skipped int
	Err              error
	At               time.Time
}

// SelectionReplacedError: an update or removal took away the selected
// server; New was selected instead.
type SelectionReplacedError struct{ Old, New string }

func (e *SelectionReplacedError) Error() string {
	return fmt.Sprintf("server %q is gone; %q selected", e.Old, e.New)
}

// SubscriptionsFailedError: the startup update of all subscriptions had
// failures.
type SubscriptionsFailedError struct{ Failed, Total int }

func (e *SubscriptionsFailedError) Error() string {
	return fmt.Sprintf("%d of %d subscriptions failed to update", e.Failed, e.Total)
}

// fetchSubscription is subscribe.Fetch (tests may replace it).
var fetchSubscription = subscribe.Fetch

// Subscriptions returns the subscription URLs.
func (c *Controller) Subscriptions() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.cfg.OnlineConfigSource)
}

// SubscriptionResults returns the last update result of each subscription
// updated in this run.
func (c *Controller) SubscriptionResults() map[string]SubResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]SubResult, len(c.subResults))
	for k, v := range c.subResults {
		out[k] = v
	}
	return out
}

// TakeNotice returns, once, a notice the UI should show (see
// SelectionReplacedError, SubscriptionsFailedError).
func (c *Controller) TakeNotice() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.notice
	c.notice = nil
	return n
}

// AddSubscription adds a subscription URL and saves; it does not fetch.
func (c *Controller) AddSubscription(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrBadSubscriptionURL
	}
	c.mu.Lock()
	defer c.unlock()
	if slices.Contains(c.cfg.OnlineConfigSource, rawURL) {
		return ErrDuplicateSubscription
	}
	c.cfg.OnlineConfigSource = append(slices.Clone(c.cfg.OnlineConfigSource), rawURL)
	c.dirty = true
	if err := c.save(); err != nil {
		return &SaveError{Err: err}
	}
	return nil
}

// RemoveSubscription removes the URL and its servers.
func (c *Controller) RemoveSubscription(rawURL string) error {
	c.mu.Lock()
	defer c.unlock()
	next := c.cfg.Clone()
	next.OnlineConfigSource = slices.DeleteFunc(slices.Clone(next.OnlineConfigSource), func(s string) bool { return s == rawURL })
	notice := c.replaceGroup(next, rawURL, nil)
	if err := c.commit(next); err != nil {
		return err
	}
	delete(c.subResults, rawURL)
	if notice != nil {
		c.notice = notice
	}
	return nil
}

// UpdateSubscription fetches one subscription and replaces its servers.
// The fetch runs without the controller lock; on failure the servers stay.
func (c *Controller) UpdateSubscription(ctx context.Context, rawURL string) (SubResult, error) {
	c.updMu.Lock() // one update at a time: no interleaved replaces
	defer c.updMu.Unlock()
	servers, skipped, err := fetchSubscription(ctx, rawURL, c.fetchPort())
	res := SubResult{Servers: len(servers), Skipped: skipped, Err: err, At: time.Now()}
	c.mu.Lock()
	defer c.unlock()
	if !slices.Contains(c.cfg.OnlineConfigSource, rawURL) {
		return res, errSubscriptionRemoved // Review Focus 5
	}
	c.subResults[rawURL] = res
	c.dirty = true
	if err != nil {
		c.d.Logger.Warn("subscription update failed", "host", subscribe.Host(rawURL), "err", err)
		return res, err
	}
	next := c.cfg.Clone()
	notice := c.replaceGroup(next, rawURL, servers)
	if err := c.commit(next); err != nil {
		res.Err = err
		c.subResults[rawURL] = res
		return res, err
	}
	if notice != nil { // only once the change stands
		c.notice = notice
	}
	c.d.Logger.Info("subscription updated", "host", subscribe.Host(rawURL), "servers", len(servers), "skipped", skipped)
	return res, nil
}

// UpdateAllSubscriptions updates every subscription and returns the URLs
// that failed.
func (c *Controller) UpdateAllSubscriptions(ctx context.Context) (failed []string) {
	for _, u := range c.Subscriptions() {
		if _, err := c.UpdateSubscription(ctx, u); err != nil && !errors.Is(err, errSubscriptionRemoved) {
			failed = append(failed, u)
		}
	}
	return failed
}

// fetchPort is the local port to fetch through (C-D1): the core's port
// when the current server can carry traffic, else 0 (direct).
func (c *Controller) fetchPort() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	srv, ok := c.cfg.Current()
	if !ok || unusable(c.cfg) != nil || c.pluginFailure != nil || (srv.Plugin != "" && c.plug == nil) {
		return 0
	}
	return c.cfg.LocalPort
}

// replaceGroup replaces next's servers of group with servers, keeps them
// after the user's own, and keeps the selected server selected (matched by
// address, port, method, password and plugin). When it is gone the first
// server of its subscription, else the first server, is selected and the
// returned notice says so; the caller shows it once the commit succeeds.
func (c *Controller) replaceGroup(next *config.Config, group string, servers []config.Server) error {
	var sel config.Server
	hadSel := false
	if s, ok := next.Current(); ok {
		sel, hadSel = s, true
	}
	kept := slices.DeleteFunc(slices.Clone(next.Configs), func(s config.Server) bool { return s.Group == group })
	next.Configs = config.SortByGroup(append(kept, servers...))
	if !hadSel {
		next.Index = 0
		return nil
	}
	for i, s := range next.Configs {
		if sameServer(s, sel) {
			next.Index = i
			return nil
		}
	}
	next.Index = 0
	for i, s := range next.Configs {
		if sel.Group != "" && s.Group == sel.Group {
			next.Index = i
			break
		}
	}
	if len(next.Configs) > 0 {
		return &SelectionReplacedError{Old: sel.Title(), New: next.Configs[next.Index].Title()}
	}
	return nil
}

func sameServer(a, b config.Server) bool {
	return a.Server == b.Server && a.ServerPort == b.ServerPort && a.Method == b.Method &&
		a.Password == b.Password && a.Plugin == b.Plugin && a.Group == b.Group
}

// updateAllInBackground runs the startup update (C-D3).
func (c *Controller) updateAllInBackground(ctx context.Context, urls []string) {
	if len(urls) == 0 {
		return
	}
	go func() {
		if failed := c.UpdateAllSubscriptions(ctx); len(failed) > 0 {
			c.mu.Lock()
			c.notice = &SubscriptionsFailedError{Failed: len(failed), Total: len(urls)}
			c.dirty = true
			c.unlock()
		}
	}()
}

// GroupSize is how many servers came from subscription rawURL.
func (c *Controller) GroupSize(rawURL string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, s := range c.cfg.Configs {
		if s.Group == rawURL {
			n++
		}
	}
	return n
}

// isSubscription reports whether group names one of cfg's subscriptions.
func isSubscription(cfg *config.Config, group string) bool {
	return group != "" && slices.Contains(cfg.OnlineConfigSource, group)
}

// reconcileSaved takes the Edit Servers list and returns what to save: the
// user's own servers from the window (no group, or a hand-typed group that
// still exists), the subscription servers as the controller has them now
// (the window may hold stale or removed ones), own servers first. index is
// mapped to the same server.
func reconcileSaved(cur *config.Config, servers []config.Server, index int) ([]config.Server, int) {
	var sel config.Server
	hasSel := index >= 0 && index < len(servers)
	if hasSel {
		sel = servers[index]
	}
	groupExists := func(g string) bool {
		return slices.ContainsFunc(cur.Configs, func(s config.Server) bool { return s.Group == g })
	}
	var out []config.Server
	for _, s := range servers {
		if s.Group == "" || (!isSubscription(cur, s.Group) && groupExists(s.Group)) {
			out = append(out, s)
		}
	}
	for _, s := range cur.Configs {
		if isSubscription(cur, s.Group) {
			out = append(out, s)
		}
	}
	out = config.SortByGroup(out)
	if hasSel {
		for i, s := range out {
			if sameServer(s, sel) || (s.Group == "" && reflect.DeepEqual(s, sel)) {
				return out, i
			}
		}
	}
	return out, 0
}
