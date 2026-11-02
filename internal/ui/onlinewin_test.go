package ui

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
)

type fakeSubs struct {
	urls    []string
	results map[string]controller.SubResult
	updated []string
	removed []string
	servers map[string]int
}

func (f *fakeSubs) Subscriptions() []string { return slices.Clone(f.urls) }
func (f *fakeSubs) SubscriptionResults() map[string]controller.SubResult {
	return f.results
}
func (f *fakeSubs) AddSubscription(u string) error {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return controller.ErrBadSubscriptionURL
	}
	if slices.Contains(f.urls, u) {
		return controller.ErrDuplicateSubscription
	}
	f.urls = append(f.urls, u)
	return nil
}
func (f *fakeSubs) RemoveSubscription(u string) error {
	f.removed = append(f.removed, u)
	f.urls = slices.DeleteFunc(f.urls, func(s string) bool { return s == u })
	return nil
}
func (f *fakeSubs) UpdateSubscription(_ context.Context, u string) (controller.SubResult, error) {
	f.updated = append(f.updated, u)
	r := controller.SubResult{Servers: 12}
	f.results[u] = r
	return r, nil
}
func (f *fakeSubs) UpdateAllSubscriptions(ctx context.Context) []string {
	for _, u := range f.urls {
		f.UpdateSubscription(ctx, u)
	}
	return nil
}
func (f *fakeSubs) GroupSize(u string) int { return f.servers[u] }

func newOnline(urls ...string) (*onlineView, *fakeSubs, *mui.Tester) {
	f := &fakeSubs{urls: urls, results: map[string]controller.SubResult{}, servers: map[string]int{}}
	v := newOnlineView(f)
	return v, f, mui.NewTester(v.View, 480, 510)
}

func TestOnlineAddValidation(t *testing.T) {
	v, f, tt := newOnline("https://a.example/s")
	v.input = "ftp://x.example/s"
	tt.Frame()
	tt.Click("Add")
	tt.Frame()
	if len(f.urls) != 1 || !tt.HasText("URL must start with http:// or https://") {
		t.Fatalf("urls %v texts %q", f.urls, tt.Texts())
	}
	v.input = "https://a.example/s"
	tt.Frame()
	tt.Click("Add")
	tt.Frame()
	if !tt.HasText("Already added.") {
		t.Fatalf("texts %q", tt.Texts())
	}
	v.input = "https://b.example/s"
	tt.Frame()
	tt.Click("Add")
	tt.Frame()
	if len(f.urls) != 2 || !slices.Contains(f.updated, "https://b.example/s") || v.input != "" {
		t.Fatalf("urls %v updated %v input %q", f.urls, f.updated, v.input)
	}
}

func TestOnlineBusyDisablesButtons(t *testing.T) {
	v, f, tt := newOnline("https://a.example/s")
	var pending func()
	v.run = func(work func() error, done func(error)) { pending = func() { done(work()) } }
	tt.Frame()
	tt.Click("Update all")
	tt.Frame()
	if !v.busy {
		t.Fatal("not busy during update")
	}
	tt.Click("Update all") // disabled: must not start a second run
	if len(f.updated) != 0 {
		t.Fatal("work ran before its turn")
	}
	pending()
	tt.Frame()
	if v.busy || len(f.updated) != 1 {
		t.Fatalf("busy %v updated %v", v.busy, f.updated)
	}
}

func TestOnlineRemoveConfirm(t *testing.T) {
	v, f, tt := newOnline("https://a.example/s")
	f.servers["https://a.example/s"] = 12
	v.sel = 0
	tt.Frame()
	tt.Click("Remove")
	tt.Frame()
	if !tt.HasText("Remove this subscription and its 12 servers?") {
		t.Fatalf("texts %q", tt.Texts())
	}
	tt.Click("Cancel")
	tt.Frame()
	if len(f.removed) != 0 {
		t.Fatal("removed on Cancel")
	}
	tt.Click("Remove")
	tt.Frame()
	v.confirmRemove()
	tt.Frame()
	if len(f.removed) != 1 || len(v.urls) != 0 {
		t.Fatalf("removed %v urls %v", f.removed, v.urls)
	}
}

func TestOnlineRowStatus(t *testing.T) {
	v, f, tt := newOnline("https://a.example/s", "https://b.example/s")
	f.results["https://a.example/s"] = controller.SubResult{Servers: 12}
	f.results["https://b.example/s"] = controller.SubResult{Err: errors.New("HTTP 404")}
	v.refresh()
	tt.Frame()
	if !tt.HasText("✓ 12") || !tt.HasText("⚠") {
		t.Fatalf("texts %q", tt.Texts())
	}
}

func TestOnlineSnapshots(t *testing.T) {
	dir := os.Getenv("SNAP_DIR")
	if dir == "" {
		t.Skip("SNAP_DIR unset")
	}
	for _, dark := range []bool{false, true} {
		v, f, tt := newOnline("https://sub.example.com/api/v1/client/subscribe?token=abc", "https://x.example/b")
		f.results[v.urls[0]] = controller.SubResult{Servers: 12}
		f.results[v.urls[1]] = controller.SubResult{Err: errors.New("HTTP 404")}
		v.refresh()
		v.sel = 0
		tt.SetDark(dark)
		tt.Frame()
		name := map[bool]string{false: "onlinewin-light.png", true: "onlinewin-dark.png"}[dark]
		fh, _ := os.Create(filepath.Join(dir, name))
		png.Encode(fh, tt.Image())
		fh.Close()
	}
}

var _ = config.Server{}

// Final review #3: a failed action is shown, not swallowed.
func TestOnlineShowsActionError(t *testing.T) {
	v, _, tt := newOnline("https://a.example/s")
	v.sel = 0
	v.run = func(work func() error, done func(error)) { work(); done(errors.New("plugin not trusted")) }
	tt.Frame()
	tt.Click("Update")
	tt.Frame()
	if !tt.HasText("plugin not trusted") {
		t.Fatalf("texts %q", tt.Texts())
	}
}
