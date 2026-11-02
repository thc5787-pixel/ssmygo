package controller

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/geosite"
	"github.com/thc5787/ssmygo/internal/rules"
	"google.golang.org/protobuf/encoding/protowire"
)

// dlcWith encodes a GeoSiteList: group → root-domain rules.
func dlcWith(groups map[string][]string) []byte {
	var list []byte
	for _, g := range []string{"mygroup", "geolocation-!cn", "cn", "private", "other"} {
		ds, ok := groups[g]
		if !ok {
			continue
		}
		var s []byte
		s = protowire.AppendTag(s, 1, protowire.BytesType)
		s = protowire.AppendString(s, strings.ToUpper(g))
		for _, v := range ds {
			var d []byte
			d = protowire.AppendTag(d, 1, protowire.VarintType)
			d = protowire.AppendVarint(d, 2)
			d = protowire.AppendTag(d, 2, protowire.BytesType)
			d = protowire.AppendString(d, v)
			s = protowire.AppendTag(s, 2, protowire.BytesType)
			s = protowire.AppendBytes(s, d)
		}
		list = protowire.AppendTag(list, 1, protowire.BytesType)
		list = protowire.AppendBytes(list, s)
	}
	return list
}

// geoServer serves *body at /secret/dlc.dat and counts requests.
type geoServer struct {
	mu   sync.Mutex
	body []byte
	hits atomic.Int32
	url  string
}

func newGeoServer(t *testing.T, body []byte) *geoServer {
	g := &geoServer{body: body}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.hits.Add(1)
		g.mu.Lock()
		b := g.body
		g.mu.Unlock()
		w.Write(b)
	}))
	t.Cleanup(s.Close)
	g.url = s.URL + "/secret/dlc.dat"
	return g
}

func (g *geoServer) set(b []byte) {
	g.mu.Lock()
	g.body = b
	g.mu.Unlock()
}

// rulesEnv: rules mode, and a server that refuses every connection, so a
// connection to "localhost" works only if the rules send it direct.
func rulesEnv(t *testing.T, mod func(*config.Config)) (*env, *Controller) {
	e := newEnv(t)
	dead := freePort(t)
	e.writeConfig(func(c *config.Config) {
		c.Configs = []config.Server{{Server: "127.0.0.1", ServerPort: dead, Method: "aes-256-gcm", Password: "pw", Timeout: 5}}
		c.Global = false
		c.Enabled = true
		c.GeositeDirectGroups = []string{"cn"}
		c.GeositeProxiedGroups = []string{"geolocation-!cn"}
		c.GeositeUpdatedAt = time.Now()
		if mod != nil {
			mod(c)
		}
	})
	return e, e.start()
}

// viaLocalhost connects to the echo target as "localhost" through the
// local port; nil means the connection went direct.
func viaLocalhost(port int, target uint16) error {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte{5, 1, 0})
	b := make([]byte, 10)
	if _, err := io.ReadFull(c, b[:2]); err != nil {
		return err
	}
	host := "localhost"
	req := append([]byte{5, 1, 0, 3, byte(len(host))}, host...)
	req = binary.BigEndian.AppendUint16(req, target)
	c.Write(req)
	if _, err := io.ReadFull(c, b); err != nil || b[1] != 0 {
		return fmt.Errorf("socks reply %v %v", b, err)
	}
	c.Write([]byte("ping"))
	got := make([]byte, 4)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "ping" {
		return fmt.Errorf("echo %q %v", got, err)
	}
	return nil
}

func eventually(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("not within %v: %s", d, what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func fastPoll(t *testing.T) {
	old := userRulePoll
	userRulePoll = 50 * time.Millisecond
	t.Cleanup(func() { userRulePoll = old })
}

func (e *env) direct(c *Controller) bool {
	return viaLocalhost(c.State().Port, e.target.Port()) == nil
}

func TestUpdateGeositeApplies(t *testing.T) {
	ports := make(chan int, 4)
	old := geositeClient
	geositeClient = func(port int, d time.Duration) *http.Client { ports <- port; return old(port, d) }
	t.Cleanup(func() { geositeClient = old })

	body := dlcWith(map[string][]string{"mygroup": {"localhost"}, "geolocation-!cn": {"example.org"}})
	g := newGeoServer(t, body)
	e, c := rulesEnv(t, func(c *config.Config) {
		c.GeositeDirectGroups = []string{"mygroup"} // not in the embedded dlc
		c.GeositeURL = g.url
		c.GeositeUpdatedAt = time.Time{}.Add(time.Hour) // stale, but the startup check is off in tests
	})
	if e.direct(c) {
		t.Fatal("localhost direct before the update")
	}
	changed, err := c.UpdateGeosite(context.Background())
	if err != nil || !changed {
		t.Fatalf("changed %v err %v", changed, err)
	}
	if p := <-ports; p != c.State().Port {
		t.Fatalf("fetched through port %d, want the local port %d", p, c.State().Port)
	}
	if !e.direct(c) {
		t.Fatal("new geosite not applied: localhost not direct")
	}
	if b, _ := os.ReadFile(filepath.Join(e.portable, "dlc.dat")); !bytes.Equal(b, body) {
		t.Fatal("dlc.dat not saved")
	}
	if at := e.diskConfig().GeositeUpdatedAt; time.Since(at) > time.Minute {
		t.Fatalf("update time %v", at)
	}
	if _, err := os.Stat(filepath.Join(e.portable, "dlc.dat.tmp")); err == nil {
		t.Fatal("temporary file left")
	}
	// The next start uses the saved dlc.dat.
	c.Shutdown()
	c2 := e.start()
	if !e.direct(c2) {
		t.Fatal("saved dlc.dat not used on start")
	}
}

func TestStartIgnoresBrokenSavedDLC(t *testing.T) {
	e := newEnv(t)
	os.WriteFile(filepath.Join(e.portable, "dlc.dat"), []byte("<html>broken</html>"), 0o644)
	e.writeConfig(func(c *config.Config) { c.Global = false })
	c := e.start()
	if c.State().Err != nil {
		t.Fatalf("err %v", c.State().Err)
	}
	if c.rules == nil {
		t.Fatal("no rules: embedded dlc not used")
	}
}

func TestUpdateGeositeSameContent(t *testing.T) {
	body := dlcWith(map[string][]string{"cn": {"localhost"}, "geolocation-!cn": {"example.org"}})
	g := newGeoServer(t, body)
	e, c := rulesEnv(t, func(c *config.Config) { c.GeositeURL = g.url })
	if changed, err := c.UpdateGeosite(context.Background()); err != nil || !changed {
		t.Fatalf("first: %v %v", changed, err)
	}
	gob := filepath.Join(e.portable, "cache", "domainsets", "direct.gob")
	st1, _ := os.Stat(gob)
	first := e.diskConfig().GeositeUpdatedAt
	clock := first.Add(time.Hour)
	oldNow := now
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = oldNow })
	time.Sleep(20 * time.Millisecond)
	changed, err := c.UpdateGeosite(context.Background())
	if err != nil || changed {
		t.Fatalf("second: changed %v err %v", changed, err)
	}
	if st2, _ := os.Stat(gob); !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("rules rebuilt for identical content")
	}
	if at := e.diskConfig().GeositeUpdatedAt; !at.Equal(clock) {
		t.Fatalf("update time %v, want %v", at, clock)
	}
	if n := c.TakeNotice(); n != nil {
		t.Fatalf("notice %v", n)
	}
}

// Review Focus 1: a broken download keeps the current rules and files.
func TestUpdateGeositeBadDownloadKeepsRules(t *testing.T) {
	body := dlcWith(map[string][]string{"cn": {"localhost"}, "geolocation-!cn": {"example.org"}})
	g := newGeoServer(t, body)
	e, c := rulesEnv(t, func(c *config.Config) { c.GeositeURL = g.url })
	if _, err := c.UpdateGeosite(context.Background()); err != nil {
		t.Fatal(err)
	}
	at := e.diskConfig().GeositeUpdatedAt
	for name, bad := range map[string][]byte{
		"html":          []byte("<!DOCTYPE html><html>rate limited</html>"),
		"missing group": dlcWith(map[string][]string{"cn": {"x.com"}}),
		"truncated":     body[:len(body)-3],
	} {
		g.set(bad)
		changed, err := c.UpdateGeosite(context.Background())
		var ge *GeositeUpdateError
		if changed || !errors.As(err, &ge) {
			t.Fatalf("%s: changed %v err %v", name, changed, err)
		}
		if !e.direct(c) {
			t.Fatalf("%s: rules lost", name)
		}
		if b, _ := os.ReadFile(filepath.Join(e.portable, "dlc.dat")); !bytes.Equal(b, body) {
			t.Fatalf("%s: dlc.dat changed", name)
		}
		if got := e.diskConfig().GeositeUpdatedAt; !got.Equal(at) {
			t.Fatalf("%s: update time moved", name)
		}
	}
	var mg *geosite.MissingGroupsError
	g.set(dlcWith(map[string][]string{"cn": {"x.com"}}))
	if _, err := c.UpdateGeosite(context.Background()); !errors.As(err, &mg) || !reflect.DeepEqual(mg.Groups, []string{"geolocation-!cn"}) {
		t.Fatalf("err %v", err)
	}
}

func TestWeeklyGeositeCheck(t *testing.T) {
	oldAuto, oldNow := geositeAutoCheck, now
	geositeAutoCheck = true
	clock := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return clock }
	t.Cleanup(func() { geositeAutoCheck, now = oldAuto, oldNow })

	body := dlcWith(map[string][]string{"cn": {"localhost"}, "geolocation-!cn": {"example.org"}})
	for _, tc := range []struct {
		age   time.Duration
		check bool
	}{{6 * 24 * time.Hour, false}, {8 * 24 * time.Hour, true}} {
		g := newGeoServer(t, body)
		e, c := rulesEnv(t, func(c *config.Config) {
			c.GeositeURL = g.url
			c.GeositeUpdatedAt = clock.Add(-tc.age)
		})
		if !tc.check {
			time.Sleep(500 * time.Millisecond)
			if g.hits.Load() != 0 {
				t.Fatalf("age %v: checked", tc.age)
			}
			continue
		}
		eventually(t, 3*time.Second, "weekly update applied", func() bool { return e.direct(c) })
		var n error
		eventually(t, time.Second, "notice", func() bool { n = c.TakeNotice(); return n != nil })
		if _, ok := n.(*GeositeUpdatedNotice); !ok {
			t.Fatalf("notice %T %v", n, n)
		}
		if at := e.diskConfig().GeositeUpdatedAt; !at.Equal(clock) {
			t.Fatalf("update time %v", at)
		}
	}
}

func TestWeeklyGeositeCheckFailureNotifiesOnce(t *testing.T) {
	oldAuto := geositeAutoCheck
	geositeAutoCheck = true
	t.Cleanup(func() { geositeAutoCheck = oldAuto })
	g := newGeoServer(t, []byte("not a dlc"))
	_, c := rulesEnv(t, func(c *config.Config) {
		c.GeositeURL = g.url
		c.GeositeUpdatedAt = time.Time{}
	})
	var n error
	eventually(t, 3*time.Second, "failure notice", func() bool { n = c.TakeNotice(); return n != nil })
	var ge *GeositeUpdateError
	if !errors.As(n, &ge) || !ge.Background {
		t.Fatalf("notice %v", n)
	}
	if c.TakeNotice() != nil {
		t.Fatal("second notice")
	}
}

// D-D4: a user-rule.txt change applies within 3 s; user rules beat geosite
// in both directions (Review Focus 3).
func TestUserRuleReload(t *testing.T) {
	fastPoll(t)
	e, c := rulesEnv(t, nil)
	if e.direct(c) {
		t.Fatal("localhost direct without rules")
	}
	path := c.UserRulePath()
	if path != filepath.Join(e.portable, "user-rule.txt") {
		t.Fatalf("path %q", path)
	}
	os.WriteFile(path, []byte("@@||localhost^\n"), 0o644)
	eventually(t, 3*time.Second, "user direct rule", func() bool { return e.direct(c) })

	// Saved by rename (editors writing a temporary file).
	tmp := path + ".swp"
	os.WriteFile(tmp, []byte("! nothing\n"), 0o644)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, "rename-save picked up", func() bool { return !e.direct(c) })

	// Deleted: no user rules.
	os.WriteFile(path, []byte("@@||localhost^\n"), 0o644)
	eventually(t, 3*time.Second, "rewrite", func() bool { return e.direct(c) })
	os.Remove(path)
	eventually(t, 3*time.Second, "delete", func() bool { return !e.direct(c) })
}

func TestUserProxiedBeatsGeositeDirect(t *testing.T) {
	fastPoll(t)
	e, c := rulesEnv(t, func(c *config.Config) {
		c.GeositeDirectGroups = []string{"private"} // localhost
		c.GeositePreferDirect = true
	})
	if !e.direct(c) {
		t.Fatal("localhost not direct by geosite")
	}
	os.WriteFile(c.UserRulePath(), []byte("||localhost^\n"), 0o644)
	eventually(t, 3*time.Second, "user proxy rule", func() bool { return !e.direct(c) })
}

func TestUserRuleUnreadableKeepsRules(t *testing.T) {
	fastPoll(t)
	e, c := rulesEnv(t, nil)
	path := c.UserRulePath()
	os.WriteFile(path, []byte("@@||localhost^\n"), 0o644)
	eventually(t, 3*time.Second, "user rule", func() bool { return e.direct(c) })
	c.TakeNotice()
	os.Remove(path)
	os.Mkdir(path, 0o755) // stat works, reading fails
	var n error
	eventually(t, 3*time.Second, "read error notice", func() bool { n = c.TakeNotice(); return n != nil })
	var re *UserRulesReadError
	if !errors.As(n, &re) {
		t.Fatalf("notice %v", n)
	}
	if !e.direct(c) {
		t.Fatal("previous rules lost")
	}
	time.Sleep(300 * time.Millisecond)
	if n := c.TakeNotice(); n != nil {
		t.Fatalf("notified again: %v", n)
	}
}

func TestUserRuleSkippedNotice(t *testing.T) {
	fastPoll(t)
	e := newEnv(t)
	os.WriteFile(filepath.Join(e.portable, "user-rule.txt"), []byte("! c\n/re/\n||a.com^$third-party\n"), 0o644)
	e.writeConfig(func(c *config.Config) { c.Global = false })
	c := e.start()
	n := c.TakeNotice()
	var sk *UserRulesSkippedError
	if !errors.As(n, &sk) || !reflect.DeepEqual(sk.Lines, []int{2, 3}) {
		t.Fatalf("notice %v", n)
	}
	// Same text again (touched): not notified again.
	future := time.Now().Add(time.Minute)
	os.Chtimes(c.UserRulePath(), future, future)
	time.Sleep(300 * time.Millisecond)
	if n := c.TakeNotice(); n != nil {
		t.Fatalf("notified again: %v", n)
	}
}

// Review Focus 4: in global mode rules are rebuilt but neither the core
// nor the system proxy is touched.
func TestGlobalModeRebuildLeavesSysproxy(t *testing.T) {
	fastPoll(t)
	body := dlcWith(map[string][]string{"private": {"localhost"}, "cn": {"example.cn"}, "geolocation-!cn": {"example.org"}})
	g := newGeoServer(t, body)
	e := newEnv(t)
	e.writeConfig(func(c *config.Config) {
		c.Enabled = true
		c.GeositeURL = g.url
		c.GeositeUpdatedAt = time.Now()
	})
	c := e.start()
	calls := len(e.sys.Calls)
	os.WriteFile(c.UserRulePath(), []byte("@@||example.net^\n"), 0o644)
	eventually(t, 3*time.Second, "user rules rebuilt", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.rules != nil && c.rules.UserDirect != ""
	})
	if _, err := c.UpdateGeosite(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(e.sys.Calls) != calls {
		t.Fatalf("sysproxy touched: %+v", e.sys.Calls[calls:])
	}
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatalf("global mode broken: %v", err)
	}
}

func TestEnsureUserRuleFile(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	path, err := c.EnsureUserRuleFile()
	if err != nil || path != c.UserRulePath() {
		t.Fatalf("%q %v", path, err)
	}
	b, _ := os.ReadFile(path)
	d, p, skipped := rules.ParseUserRules(string(b))
	if len(b) == 0 || len(d)+len(p)+len(skipped) != 0 {
		t.Fatalf("template has rules: %q", b)
	}
	os.WriteFile(path, []byte("||mine.com^\n"), 0o644)
	if _, err := c.EnsureUserRuleFile(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "||mine.com^\n" {
		t.Fatalf("existing file overwritten: %q", b)
	}
}
