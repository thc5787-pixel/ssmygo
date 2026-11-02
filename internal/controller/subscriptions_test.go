package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thc5787/ssmygo/internal/config"
)

// subDoc is a SIP008 document with servers named by remarks.
func subDoc(remarks ...string) string {
	var parts []string
	for i, r := range remarks {
		parts = append(parts, fmt.Sprintf(`{"server":"192.0.2.%d","server_port":8388,"password":"pw-%s","method":"aes-256-gcm","remarks":%q}`, i+1, r, r))
	}
	return `{"version":1,"servers":[` + strings.Join(parts, ",") + `]}`
}

// subServer serves body() at a fresh URL.
func subServer(t *testing.T, body func() (int, string)) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		code, b := body()
		w.WriteHeader(code)
		io.WriteString(w, b)
	}))
	t.Cleanup(s.Close)
	return s.URL + "/sub"
}

func remarks(servers []config.Server, group string) []string {
	var out []string
	for _, s := range servers {
		if s.Group == group {
			out = append(out, s.Remarks)
		}
	}
	return out
}

func current(c *Controller) config.Server {
	cfg := c.Config()
	return cfg.Configs[cfg.Index]
}

// directFetch makes fetches go direct: the tests' subscription servers are
// unreachable addresses, and a selected one would carry the fetch (C-D1).
func directFetch(t *testing.T) {
	old := fetchSubscription
	fetchSubscription = func(ctx context.Context, u string, _ int) ([]config.Server, int, error) { return old(ctx, u, 0) }
	t.Cleanup(func() { fetchSubscription = old })
}

func withSub(url string, rem ...string) func(*config.Config) {
	return func(c *config.Config) {
		c.OnlineConfigSource = append(c.OnlineConfigSource, url)
		for i, r := range rem {
			c.Configs = append(c.Configs, config.Server{Server: fmt.Sprintf("192.0.2.%d", i+1), ServerPort: 8388,
				Password: "pw-" + r, Method: "aes-256-gcm", Remarks: r, Group: url, Timeout: 5})
		}
	}
}

func TestUpdateReplacesOnlyGroup(t *testing.T) {
	e := newEnv(t)
	a := subServer(t, func() (int, string) { return 200, subDoc("a1", "a2", "a3") })
	b := "https://b.example/sub"
	e.writeConfig(func(c *config.Config) { withSub(a, "old-a")(c); withSub(b, "b1")(c) })
	c := e.start()
	res, err := c.UpdateSubscription(context.Background(), a)
	if err != nil || res.Servers != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	cfg := c.Config()
	if got := strings.Join(remarks(cfg.Configs, a), ","); got != "a1,a2,a3" {
		t.Fatalf("group a: %s", got)
	}
	if got := strings.Join(remarks(cfg.Configs, b), ","); got != "b1" || cfg.Configs[0].Group != "" {
		t.Fatalf("group b %s, first %+v", got, cfg.Configs[0])
	}
}

func TestUpdateKeepsSelection(t *testing.T) {
	directFetch(t)
	e := newEnv(t)
	a := subServer(t, func() (int, string) { return 200, subDoc("new", "keep") })
	e.writeConfig(func(c *config.Config) {
		withSub(a, "x", "keep")(c)
		c.Index = 2 // "keep" (own server is 0)
	})
	c := e.start()
	// The new document lists "keep" at another address: match by address etc.
	// fails, so make the document keep the same server fields.
	if _, err := c.UpdateSubscription(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	// "keep" moved from 192.0.2.2 to 192.0.2.2 (second entry both times): same server.
	if s := current(c); s.Remarks != "keep" {
		t.Fatalf("selected %q", s.Remarks)
	}
}

func TestUpdateSelectionFallback(t *testing.T) {
	directFetch(t)
	e := newEnv(t)
	a := subServer(t, func() (int, string) { return 200, subDoc("fresh") })
	e.writeConfig(func(c *config.Config) {
		withSub(a, "gone-1", "gone-2")(c)
		c.Index = 2 // "gone-2"
	})
	c := e.start()
	if _, err := c.UpdateSubscription(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if s := current(c); s.Remarks != "fresh" {
		t.Fatalf("selected %q, want the first server of the same subscription", s.Remarks)
	}
	var sr *SelectionReplacedError
	if n := c.TakeNotice(); !errors.As(n, &sr) || sr.Old != "gone-2 (192.0.2.2:8388)" {
		t.Fatalf("notice %v", n)
	}
	// Removing the subscription of the selected server falls back to the first server.
	if err := c.RemoveSubscription(a); err != nil {
		t.Fatal(err)
	}
	if cfg := c.Config(); cfg.Index != 0 || len(remarks(cfg.Configs, a)) != 0 || len(cfg.OnlineConfigSource) != 0 {
		t.Fatalf("after remove: index %d, %v", cfg.Index, cfg.OnlineConfigSource)
	}
}

func TestUpdateFailureKeepsServers(t *testing.T) {
	e := newEnv(t)
	a := subServer(t, func() (int, string) { return 404, "nope" })
	e.writeConfig(withSub(a, "a1", "a2"))
	c := e.start()
	res, err := c.UpdateSubscription(context.Background(), a)
	if err == nil || res.Err == nil {
		t.Fatal("404 succeeded")
	}
	if got := remarks(c.Config().Configs, a); len(got) != 2 {
		t.Fatalf("servers lost: %v", got)
	}
	if r := c.SubscriptionResults()[a]; r.Err == nil {
		t.Fatal("result not recorded")
	}
}

func TestSubscriptionsSortedAfterOwn(t *testing.T) {
	e := newEnv(t)
	a := subServer(t, func() (int, string) { return 200, subDoc("a1") })
	e.writeConfig(func(c *config.Config) {
		own := c.Configs[0]
		withSub(a, "old")(c)
		own.Remarks = "own-2"
		c.Configs = append(c.Configs, own) // own server after the subscription's
	})
	c := e.start()
	c.UpdateSubscription(context.Background(), a)
	cfg := c.Config()
	if cfg.Configs[0].Group != "" || cfg.Configs[1].Group != "" || cfg.Configs[2].Group != a {
		t.Fatalf("order %+v", cfg.Configs)
	}
}

// Review Focus 5: a subscription removed during its update stays removed.
func TestRemoveDuringUpdate(t *testing.T) {
	e := newEnv(t)
	release := make(chan struct{})
	a := subServer(t, func() (int, string) { <-release; return 200, subDoc("a1") })
	e.writeConfig(withSub(a, "old"))
	c := e.start()
	done := make(chan error, 1)
	go func() { _, err := c.UpdateSubscription(context.Background(), a); done <- err }()
	time.Sleep(100 * time.Millisecond)
	if err := c.RemoveSubscription(a); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	cfg := c.Config()
	if len(remarks(cfg.Configs, a)) != 0 || len(cfg.OnlineConfigSource) != 0 {
		t.Fatalf("removed subscription came back: %v %v", remarks(cfg.Configs, a), cfg.OnlineConfigSource)
	}
}

func TestConcurrentUpdatesNoDuplicates(t *testing.T) {
	e := newEnv(t)
	a := subServer(t, func() (int, string) { time.Sleep(50 * time.Millisecond); return 200, subDoc("a1", "a2") })
	e.writeConfig(withSub(a))
	c := e.start()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { c.UpdateSubscription(context.Background(), a) })
	}
	wg.Wait()
	if got := remarks(c.Config().Configs, a); len(got) != 2 {
		t.Fatalf("servers %v", got)
	}
}

// C-D3: subscriptions update after startup without delaying it.
func TestStartupUpdateDoesNotBlockStart(t *testing.T) {
	e := newEnv(t)
	a := subServer(t, func() (int, string) { time.Sleep(time.Second); return 200, subDoc("late") })
	e.writeConfig(withSub(a))
	start := time.Now()
	c := e.start()
	if d := time.Since(start); d > 800*time.Millisecond {
		t.Fatalf("Start took %v", d)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if len(remarks(c.Config().Configs, a)) == 1 {
			return
		}
	}
	t.Fatal("startup update did not happen")
}

func TestAddSubscriptionValidation(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	if err := c.AddSubscription("ftp://x.example/s"); !errors.Is(err, ErrBadSubscriptionURL) {
		t.Fatalf("ftp: %v", err)
	}
	if err := c.AddSubscription("https://ok.example/s"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddSubscription("https://ok.example/s"); !errors.Is(err, ErrDuplicateSubscription) {
		t.Fatalf("dup: %v", err)
	}
	if got := c.Subscriptions(); len(got) != 1 || got[0] != "https://ok.example/s" {
		t.Fatalf("%v", got)
	}
	cfg, _ := config.Load(e.deps().Paths.Config())
	if len(cfg.OnlineConfigSource) != 1 {
		t.Fatal("not saved")
	}
}

// C-D1: through the local port only when the current server can carry traffic.
func TestFetchPortRules(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	if p := c.fetchPort(); p != c.State().Port {
		t.Fatalf("working server: port %d, want %d", p, c.State().Port)
	}
	e2 := newEnv(t)
	e2.writeConfig(func(cfg *config.Config) { cfg.Configs = nil })
	if p := e2.start().fetchPort(); p != 0 {
		t.Fatalf("no server: port %d, want 0 (direct)", p)
	}
}

// Final review: a failed commit leaves the selection and raises no notice.
func TestFailedCommitNoNotice(t *testing.T) {
	directFetch(t)
	e := newEnv(t)
	doc := `{"version":1,"servers":[{"server":"192.0.2.9","server_port":8388,"password":"pw","method":"aes-256-gcm","remarks":"new","plugin":"no-such-plugin-xyz"}]}`
	a := subServer(t, func() (int, string) { return 200, doc })
	e.writeConfig(func(c *config.Config) {
		withSub(a, "old")(c)
		// Added after Start: the startup update would fail too and race
		// this test with its own (true) notice.
		c.OnlineConfigSource = nil
		c.Index = 1
		c.Enabled = true
	})
	c := e.start()
	if err := c.AddSubscription(a); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateSubscription(context.Background(), a); err == nil {
		t.Fatal("update with a missing plugin succeeded")
	}
	if s := current(c); s.Remarks != "old" {
		t.Fatalf("selected %q", s.Remarks)
	}
	if n := c.TakeNotice(); n != nil {
		t.Fatalf("false notice: %v", n)
	}
}

// Final review #1: imported servers go before subscription servers, and the
// selection stays on the same server.
func TestImportKeepsOwnFirst(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(func(c *config.Config) {
		withSub("https://a.example/s", "sub1")(c)
		c.Index = 1 // sub1
	})
	c := e.start()
	if _, errs := c.ImportURLs("ss://YWVzLTI1Ni1nY206cHc@new.example.com:8388#imported"); len(errs) > 0 {
		t.Fatal(errs)
	}
	cfg := c.Config()
	if cfg.Configs[1].Remarks != "imported" || cfg.Configs[2].Remarks != "sub1" {
		t.Fatalf("order %v", []string{cfg.Configs[0].Remarks, cfg.Configs[1].Remarks, cfg.Configs[2].Remarks})
	}
	if s := current(c); s.Remarks != "sub1" {
		t.Fatalf("selected %q", s.Remarks)
	}
}

// Final review #4: Edit Servers cannot write back stale subscription
// servers; hand-grouped servers (not a subscription) stay the user's.
func TestSaveServersUsesCurrentSubscriptions(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(func(c *config.Config) {
		withSub("https://a.example/s", "a-current")(c)
		hand := c.Configs[0]
		hand.Remarks, hand.Group = "hand-grouped", "My group"
		c.Configs = append(c.Configs, hand)
	})
	c := e.start()
	cfg := c.Config()
	own := cfg.Configs[0]
	stale := config.Server{Server: "192.0.2.99", ServerPort: 1, Password: "x", Method: "aes-256-gcm", Remarks: "a-stale", Group: "https://a.example/s", Timeout: 5}
	removed := config.Server{Server: "192.0.2.98", ServerPort: 1, Password: "x", Method: "aes-256-gcm", Remarks: "gone-sub", Group: "https://gone.example/s", Timeout: 5}
	var hand config.Server
	for _, s := range cfg.Configs {
		if s.Remarks == "hand-grouped" {
			hand = s
		}
	}
	if err := c.SaveServers([]config.Server{own, hand, stale, removed}, 0, cfg.LocalPort, true); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range c.Config().Configs {
		got = append(got, s.Remarks)
	}
	if strings.Join(got, ",") != strings.Join([]string{own.Remarks, "hand-grouped", "a-current"}, ",") {
		t.Fatalf("saved %v", got)
	}
}
