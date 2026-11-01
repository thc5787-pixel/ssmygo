package controller

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thc5787/ssmygo/assets"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core/legacyaead/aeadtest"
	"github.com/thc5787/ssmygo/internal/sysproxy"
)

// userProxy is a realistic prior setting (corporate PAC + bypass).
var userProxy = sysproxy.Settings{Flags: sysproxy.FlagDirect | sysproxy.FlagAutoProxyURL, PacURL: "http://corp/pac", Bypass: "*.corp"}

type env struct {
	t        *testing.T
	portable string
	roaming  string
	sys      *sysproxy.Fake
	upstream netip.AddrPort
	target   netip.AddrPort
}

func newEnv(t *testing.T) *env {
	t.Helper()
	appData := t.TempDir()
	return &env{
		t:        t,
		portable: t.TempDir(),
		roaming:  filepath.Join(appData, "ssmygo"),
		sys:      &sysproxy.Fake{Cur: userProxy},
		upstream: aeadtest.Start(t, "aes-256-gcm", "pw"),
		target:   echoServer(t),
	}
}

func (e *env) deps() Deps {
	return Deps{
		Paths:        config.Paths{Root: e.portable},
		PortableRoot: e.portable,
		RoamingRoot:  e.roaming,
		Sys:          e.sys,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		DLC:          assets.DLC(),
	}
}

func (e *env) server() config.Server {
	return config.Server{Server: e.upstream.Addr().String(), ServerPort: int(e.upstream.Port()),
		Method: "aes-256-gcm", Password: "pw", Timeout: 5}
}

// writeConfig puts a gui-config.json with one working server into the portable root.
func (e *env) writeConfig(mod func(*config.Config)) {
	c := config.Default()
	c.Configs = []config.Server{e.server()}
	c.LocalPort = freePort(e.t)
	c.FirstRun = false
	if mod != nil {
		mod(c)
	}
	if err := c.Save(filepath.Join(e.portable, "gui-config.json")); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) start() *Controller {
	e.t.Helper()
	c, err := New(e.deps())
	if err != nil {
		e.t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(c.Shutdown)
	return c
}

func (e *env) diskConfig() *config.Config {
	c, err := config.Load(filepath.Join(e.portable, "gui-config.json"))
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func echoServer(t *testing.T) netip.AddrPort {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	return netip.MustParseAddrPort(ln.Addr().String())
}

func socksEcho(port int, target netip.AddrPort) error {
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
	req := append([]byte{5, 1, 0, 1}, target.Addr().AsSlice()...)
	req = binary.BigEndian.AppendUint16(req, target.Port())
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

func TestStartFreshCreatesDefaultConfig(t *testing.T) {
	e := newEnv(t)
	c := e.newOnly()
	if !c.State().FirstRun || c.State().Mode != Disabled {
		t.Fatalf("state %+v", c.State())
	}
	if len(e.sys.Calls) != 0 {
		t.Fatalf("sysproxy touched: %+v", e.sys.Calls)
	}
	if err := c.SaveServers([]config.Server{e.server()}, 0, freePort(t), true); err != nil {
		t.Fatal(err)
	}
	if e.diskConfig().FirstRun {
		t.Fatal("FirstRun still true on disk")
	}
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
}

func TestSetModeRulesAndGlobal(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	port := c.State().Port
	if err := c.SetMode(Rules); err != nil {
		t.Fatal(err)
	}
	if e.sys.Cur != sysproxy.LocalSettings(port, userProxy.Bypass) || c.State().Mode != Rules {
		t.Fatalf("rules: sys %+v state %+v", e.sys.Cur, c.State())
	}
	if err := c.SetMode(Global); err != nil {
		t.Fatal(err)
	}
	if e.sys.Cur != sysproxy.LocalSettings(port, userProxy.Bypass) || !e.diskConfig().Global {
		t.Fatalf("global: sys %+v", e.sys.Cur)
	}
	if err := socksEcho(port, e.target); err != nil {
		t.Fatal(err)
	}
	if err := c.SetMode(Disabled); err != nil {
		t.Fatal(err)
	}
	if e.sys.Cur != userProxy || e.diskConfig().Enabled {
		t.Fatalf("disabled: sys %+v", e.sys.Cur)
	}
}

// Review Focus 4.
func TestRestoresUserProxyExactly(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c, err := New(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	c.SetMode(Rules)
	c.SetMode(Disabled)
	if e.sys.Cur != userProxy {
		t.Fatalf("after disable %+v", e.sys.Cur)
	}
	c.SetMode(Rules)
	c.Shutdown()
	if e.sys.Cur != userProxy {
		t.Fatalf("after shutdown %+v", e.sys.Cur)
	}
	d := e.diskConfig()
	if !d.Enabled || d.Sysproxy.Recorded {
		t.Fatalf("disk after shutdown: enabled=%v recorded=%v (want enabled kept, record cleared)", d.Enabled, d.Sysproxy.Recorded)
	}
}

// Review Focus 4: crashed after recording, while the system still points at us.
func TestCrashRecovery(t *testing.T) {
	e := newEnv(t)
	port := freePort(t)
	e.writeConfig(func(c *config.Config) {
		c.LocalPort = port
		c.Enabled = false
		c.Sysproxy = config.SysproxyRecord{Recorded: true, Flags: userProxy.Flags, PacURL: userProxy.PacURL, Bypass: userProxy.Bypass}
	})
	e.sys.Cur = sysproxy.LocalSettings(port, userProxy.Bypass)
	e.start()
	if e.sys.Cur != userProxy {
		t.Fatalf("not restored: %+v", e.sys.Cur)
	}
	if e.diskConfig().Sysproxy.Recorded {
		t.Fatal("record not cleared on disk")
	}
}

// Enabled at crash: restart keeps the original record and re-points the proxy.
func TestRestartWhileEnabledKeepsRecord(t *testing.T) {
	e := newEnv(t)
	port := freePort(t)
	e.writeConfig(func(c *config.Config) {
		c.LocalPort = port
		c.Enabled = true
		c.Sysproxy = config.SysproxyRecord{Recorded: true, Flags: userProxy.Flags, PacURL: userProxy.PacURL, Bypass: userProxy.Bypass}
	})
	e.sys.Cur = sysproxy.LocalSettings(port, userProxy.Bypass) // left over from the crash
	c := e.start()
	if c.State().Mode != Rules {
		t.Fatalf("mode %v", c.State().Mode)
	}
	c.SetMode(Disabled)
	if e.sys.Cur != userProxy {
		t.Fatalf("restored to %+v, want the pre-crash user setting", e.sys.Cur)
	}
}

// Review Focus 3.
func TestSaveServersBusyPortRollsBack(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	oldPort := c.State().Port
	before, _ := os.ReadFile(filepath.Join(e.portable, "gui-config.json"))
	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer busy.Close()
	err := c.SaveServers([]config.Server{e.server()}, 0, busy.Addr().(*net.TCPAddr).Port, true)
	if err == nil {
		t.Fatal("SaveServers on busy port succeeded")
	}
	if err := socksEcho(oldPort, e.target); err != nil {
		t.Fatalf("old port broken after rollback: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(e.portable, "gui-config.json"))
	if string(before) != string(after) {
		t.Fatal("config on disk changed")
	}
	if c.State().LastError == "" || c.State().Port != oldPort {
		t.Fatalf("state %+v", c.State())
	}
}

func TestSaveServersRejectsInvalid(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	bad := e.server()
	bad.ServerPort = 0
	var fe config.FieldError
	if err := c.SaveServers([]config.Server{bad}, 0, c.State().Port, true); !errors.As(err, &fe) || fe.Field != "server_port" {
		t.Fatalf("err = %v", err)
	}
}

func TestSysproxySetFailureRollsBack(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	e.sys.SetErr = errors.New("access denied")
	if err := c.SetMode(Rules); err == nil {
		t.Fatal("no error")
	}
	if c.State().Mode != Disabled || e.diskConfig().Enabled {
		t.Fatalf("mode %v disk enabled %v", c.State().Mode, e.diskConfig().Enabled)
	}
}

func TestEnableWithoutServerFails(t *testing.T) {
	e := newEnv(t)
	c := e.newOnly()
	if err := c.SetMode(Global); err == nil || len(e.sys.Calls) != 0 {
		t.Fatalf("err %v calls %v", err, e.sys.Calls)
	}
}

func TestImportURLs(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	text := "ss://YWVzLTI1Ni1nY206cGFzc0AxMjcuMC4wLjE6ODA4OA#a\nss://!!!\nss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@1.2.3.4:8388#b"
	added, errs := c.ImportURLs(text) // deferred #7: returns the servers added
	if len(added) != 2 || len(errs) != 1 || added[1].Remarks != "b" {
		t.Fatalf("added %+v errs %v", added, errs)
	}
	d := e.diskConfig()
	if len(d.Configs) != 3 || d.Index != 0 || d.Configs[2].Remarks != "b" {
		t.Fatalf("disk %+v", d.Configs)
	}
	if got := strings.Join(c.State().Servers, ","); got != e.server().Title()+",a,b" {
		t.Fatalf("servers %s", got)
	}
}

func TestSubscribeNotified(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	var got []State
	off := c.Subscribe(func(s State) { got = append(got, s) })
	c.SetMode(Global)
	c.SetShareLAN(true)
	c.SelectServer(0)
	off()
	c.SetMode(Disabled)
	if len(got) != 3 || got[0].Mode != Global || !got[1].ShareLAN {
		t.Fatalf("notifications %+v", got)
	}
}

func TestDLCCorruptFallsBackToGlobal(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	d := e.deps()
	d.DLC = []byte{0x0a, 0xff}
	c, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()
	if !strings.Contains(c.State().LastError, "rules") {
		t.Fatalf("LastError %q", c.State().LastError)
	}
	if err := c.SetMode(Rules); err != nil {
		t.Fatal(err)
	}
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptConfigBackedUp(t *testing.T) {
	e := newEnv(t)
	os.WriteFile(filepath.Join(e.portable, "gui-config.json"), []byte(`{"configs":[`), 0o644)
	c := e.newOnly()
	if !strings.Contains(c.State().LastError, "bad-") {
		t.Fatalf("LastError %q", c.State().LastError)
	}
	m, _ := filepath.Glob(filepath.Join(e.portable, "gui-config.json.bad-*"))
	if len(m) != 1 {
		t.Fatalf("backups %v", m)
	}
}

// Switching portable mode must move the config so the next launch finds it.
func TestPortableSwitchMovesConfig(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	if err := c.SaveServers([]config.Server{e.server()}, 0, c.State().Port, false); err != nil {
		t.Fatal(err)
	}
	if got := config.ResolveRoot(e.portable, filepath.Dir(e.roaming)); got != e.roaming {
		t.Fatalf("next launch resolves to %s", got)
	}
	moved, err := config.Load(filepath.Join(e.roaming, "gui-config.json"))
	if err != nil || len(moved.Configs) != 1 || moved.PortableMode {
		t.Fatalf("roaming config %+v %v", moved, err)
	}
}

// Review I5: the core must not log every connection's destination.
func TestCoreDoesNotLogDestinations(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	var buf strings.Builder
	d := e.deps()
	d.Logger = slog.New(slog.NewTextHandler(&lockedWriter{w: &buf}, nil))
	c, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if s := buf.String(); strings.Contains(s, e.target.String()) {
		t.Fatalf("destination logged:\n%s", s)
	}
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// Review I2: enabled on disk but the current server is unusable → never
// point the system proxy at a direct-only core.
func TestStartEnabledWithInvalidServer(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(func(c *config.Config) {
		c.Enabled = true
		c.Configs[0].Method = "aes-256-cfb"
	})
	c, err := New(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	defer c.Shutdown()
	if len(e.sys.Calls) != 0 || e.sys.Cur != userProxy {
		t.Fatalf("system proxy changed: %+v", e.sys.Calls)
	}
	if c.State().Mode != Disabled || c.State().LastError == "" {
		t.Fatalf("state %+v", c.State())
	}
}

func TestSelectInvalidServerWhileEnabled(t *testing.T) {
	e := newEnv(t)
	bad := e.server()
	bad.Method = "aes-256-cfb"
	e.writeConfig(func(c *config.Config) { c.Configs = append(c.Configs, bad) })
	c := e.start()
	if err := c.SetMode(Rules); err != nil {
		t.Fatal(err)
	}
	if err := c.SelectServer(1); err == nil {
		t.Fatal("selecting an unusable server while enabled succeeded")
	}
	if c.State().Current != 0 {
		t.Fatalf("current %d", c.State().Current)
	}
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
}

// Review I3: crash left the proxy on our port, then another program took
// the port: we cannot serve, so the user's proxy must come back.
func TestStartBindFailureRestoresProxy(t *testing.T) {
	e := newEnv(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	e.writeConfig(func(c *config.Config) {
		c.LocalPort = port
		c.Enabled = true
		c.Sysproxy = config.SysproxyRecord{Recorded: true, Flags: userProxy.Flags, PacURL: userProxy.PacURL, Bypass: userProxy.Bypass}
	})
	e.sys.Cur = sysproxy.LocalSettings(port, userProxy.Bypass)
	c, err := New(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err == nil {
		t.Fatal("Start on a busy port succeeded")
	}
	defer c.Shutdown()
	if e.sys.Cur != userProxy {
		t.Fatalf("proxy left on dead port: %+v", e.sys.Cur)
	}
	if c.State().Mode != Disabled {
		t.Fatalf("mode %v", c.State().Mode)
	}
	if err := c.SetMode(Disabled); err != nil && e.sys.Cur != userProxy {
		t.Fatalf("disable: %v, proxy %+v", err, e.sys.Cur)
	}
}

// Review M6: if the system proxy already points at our port (left by the
// original client), recording it would make Disable restore a dead proxy.
func TestRecordingOwnProxyRecordsDirect(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	port := c.State().Port
	e.sys.Cur = sysproxy.Settings{Flags: sysproxy.FlagDirect | sysproxy.FlagProxy, Server: fmt.Sprintf("127.0.0.1:%d", port), Bypass: "*.corp"}
	c.SetMode(Rules)
	c.SetMode(Disabled)
	if e.sys.Cur.Flags&sysproxy.FlagProxy != 0 || e.sys.Cur.Bypass != "*.corp" {
		t.Fatalf("restored to %+v, want direct with the user's bypass", e.sys.Cur)
	}
}

// newOnly constructs without Start: Start on a fresh config binds the
// default port 1080, which a running ssmygo may hold (review M10).
func (e *env) newOnly() *Controller {
	e.t.Helper()
	c, err := New(e.deps())
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(c.runner.Stop)
	return c
}

// Deferred #2: the old portable file keeps no servers or passwords, the
// switch back removes the roaming copy, and both resolve correctly.
func TestPortableSwitchLeavesOnlyMarker(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	if err := c.SaveServers([]config.Server{e.server()}, 0, c.State().Port, false); err != nil {
		t.Fatal(err)
	}
	old, _ := os.ReadFile(filepath.Join(e.portable, "gui-config.json"))
	if strings.Contains(string(old), `"configs"`) || strings.Contains(string(old), "pw") {
		t.Fatalf("exe-dir file still holds servers: %s", old)
	}
	if err := c.SaveServers([]config.Server{e.server()}, 0, c.State().Port, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.roaming, "gui-config.json")); !os.IsNotExist(err) {
		t.Fatal("roaming copy left behind after switching back")
	}
	if got := config.ResolveRoot(e.portable, filepath.Dir(e.roaming)); got != e.portable {
		t.Fatalf("resolves to %s", got)
	}
	back, _ := config.Load(filepath.Join(e.portable, "gui-config.json"))
	if len(back.Configs) != 1 || !back.PortableMode {
		t.Fatalf("portable config %+v", back)
	}
}

// Deferred #2: if the new location cannot be written, nothing moves.
func TestPortableSwitchSaveFailureKeepsOld(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	os.MkdirAll(filepath.Dir(e.roaming), 0o755)
	os.WriteFile(e.roaming, []byte("a file where the directory should be"), 0o644)
	c := e.start()
	before, _ := os.ReadFile(filepath.Join(e.portable, "gui-config.json"))
	if err := c.SaveServers([]config.Server{e.server()}, 0, c.State().Port, false); err == nil {
		t.Fatal("switch succeeded although the roaming dir is unwritable")
	}
	after, _ := os.ReadFile(filepath.Join(e.portable, "gui-config.json"))
	if string(before) != string(after) || !c.Config().PortableMode {
		t.Fatalf("old config changed or mode flipped")
	}
}

// Deferred #3: an invalid server that was already there and is unchanged
// does not block other edits; a new invalid one does.
func TestSaveServersKeepsUnchangedInvalid(t *testing.T) {
	e := newEnv(t)
	legacy := e.server()
	legacy.Method, legacy.Remarks = "aes-256-cfb", "old"
	e.writeConfig(func(c *config.Config) { c.Configs = append(c.Configs, legacy) })
	c := e.start()
	servers := c.Config().Configs
	if err := c.SaveServers(servers, 0, freePort(t), true); err != nil {
		t.Fatalf("port change blocked by an untouched invalid server: %v", err)
	}
	fresh := legacy
	fresh.Remarks = "edited"
	if err := c.SaveServers(append(servers, fresh), 0, c.State().Port, true); err == nil {
		t.Fatal("a new invalid server was accepted")
	}
}

// Plugins are P1: such a server cannot carry traffic yet.
func TestPluginServerNotUsable(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(func(c *config.Config) { c.Configs[0].Plugin = "v2ray-plugin" })
	c := e.start()
	if err := c.SetMode(Rules); !errors.Is(err, ErrPluginUnsupported) || len(e.sys.Calls) != 0 {
		t.Fatalf("err %v calls %v", err, e.sys.Calls)
	}
}

// Deferred #4: errors are typed so the UI can translate them.
func TestPortInUseErrorTyped(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	err := c.SaveServers([]config.Server{e.server()}, 0, port, true)
	var pe *PortInUseError
	if !errors.As(err, &pe) || pe.Port != port {
		t.Fatalf("err %T %v", err, err)
	}
	if !errors.As(c.State().Err, &pe) {
		t.Fatalf("State().Err = %v", c.State().Err)
	}
}

// Deferred #5: a VPN entry failing is a warning; the change stands and
// Disable still restores the user's proxy.
func TestPartialSysproxyIsWarning(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	e.sys.SetErr = &sysproxy.PartialError{Failed: map[string]error{"VPN": errors.New("denied")}}
	if err := c.SetMode(Rules); err != nil {
		t.Fatalf("partial failure rejected the change: %v", err)
	}
	if c.State().Mode != Rules {
		t.Fatalf("mode %v", c.State().Mode)
	}
	e.sys.SetErr = nil
	c.SetMode(Disabled)
	if e.sys.Cur != userProxy {
		t.Fatalf("restore %+v", e.sys.Cur)
	}
}

// Deferred #8④: subscribers run after the lock is released, so they may
// call back into the controller.
func TestSubscriberMayCallController(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nil)
	c := e.start()
	c.Subscribe(func(State) { _ = c.State() })
	done := make(chan error, 1)
	go func() { done <- c.SetShareLAN(true) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deadlock: subscriber called State() under the controller lock")
	}
}
