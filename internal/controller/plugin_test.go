package controller

import (
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core/plugin"
)

var fakeBuilt string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ctl-fakeplugin")
	if err != nil {
		panic(err)
	}
	fakeBuilt = filepath.Join(dir, "fakeplugin.exe")
	if out, err := exec.Command("go", "build", "-o", fakeBuilt, "../core/plugin/testdata/fakeplugin").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// pluginEnv is newEnv with its own exe dir holding fakeplugin.exe.
func pluginEnv(t *testing.T) (*env, string) {
	e := newEnv(t)
	exeDir := exeTempDir(t)
	b, _ := os.ReadFile(fakeBuilt)
	path := filepath.Join(exeDir, "fakeplugin.exe")
	os.WriteFile(path, b, 0o755)
	e.exeDir = exeDir
	return e, path
}

func (e *env) pluginServer(opts string) config.Server {
	s := e.server()
	// The configured address is dead: traffic can only arrive through the plugin.
	s.Server, s.ServerPort = "127.0.0.1", 9
	s.Plugin, s.PluginOpts, s.Remarks = "fakeplugin", strings.TrimSuffix("remote="+e.upstream.String()+";"+opts, ";"), "plugged"
	return s
}

func trustNow(t *testing.T, c *Controller, path string) {
	t.Helper()
	sha, err := plugin.Hash(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.TrustPlugin(path, sha); err != nil {
		t.Fatal(err)
	}
}

func TestPluginServerEndToEnd(t *testing.T) {
	e, path := pluginEnv(t)
	e.writeConfig(func(c *config.Config) { c.Configs = append(c.Configs, e.pluginServer("")) })
	c := e.start()
	trustNow(t, c, path)
	if err := c.SelectServer(1); err != nil {
		t.Fatal(err)
	}
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
}

func TestUntrustedPluginAsks(t *testing.T) {
	e, path := pluginEnv(t)
	e.writeConfig(func(c *config.Config) { c.Configs = append(c.Configs, e.pluginServer("")) })
	c := e.start()
	err := c.SelectServer(1)
	var nt *PluginNotTrustedError
	if !errors.As(err, &nt) || !filepathEqual(nt.Path, path) || len(nt.SHA256) != 64 {
		t.Fatalf("err = %#v", err)
	}
	if c.State().Current != 0 {
		t.Fatal("switched to an untrusted plugin server")
	}
	if err := c.TrustPlugin(nt.Path, nt.SHA256); err != nil {
		t.Fatal(err)
	}
	if err := c.SelectServer(1); err != nil {
		t.Fatal(err)
	}
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
	if d := e.diskConfig(); len(d.TrustedPlugins) != 1 {
		t.Fatalf("trusted on disk: %+v", d.TrustedPlugins)
	}
}

// Review Focus 5: a replaced plugin file is not run again without asking.
func TestReplacedPluginAsksAgain(t *testing.T) {
	e, path := pluginEnv(t)
	e.writeConfig(func(c *config.Config) { c.Configs = append(c.Configs, e.pluginServer("")) })
	c := e.start()
	trustNow(t, c, path)
	if err := c.SelectServer(1); err != nil {
		t.Fatal(err)
	}
	pid := c.plug.PID()
	// Windows locks a running exe against writes but allows renaming it.
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path + ".old")
	os.WriteFile(path, append(b, 0), 0o755) // different bytes
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var nt *PluginNotTrustedError
		if errors.As(c.State().Err, &nt) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("State().Err = %v, want PluginNotTrustedError", c.State().Err)
}

func TestPluginNotFoundRollsBack(t *testing.T) {
	e, _ := pluginEnv(t)
	missing := e.server()
	missing.Plugin = "missing-plugin"
	e.writeConfig(func(c *config.Config) { c.Configs = append(c.Configs, missing) })
	c := e.start()
	var nf *plugin.NotFoundError
	if err := c.SelectServer(1); !errors.As(err, &nf) {
		t.Fatalf("err = %v", err)
	}
	if c.State().Current != 0 {
		t.Fatal("switched")
	}
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
}

func TestSwitchAwayStopsPlugin(t *testing.T) {
	e, path := pluginEnv(t)
	e.writeConfig(func(c *config.Config) { c.Configs = append(c.Configs, e.pluginServer("")) })
	c := e.start()
	trustNow(t, c, path)
	c.SelectServer(1)
	pid := c.plug.PID()
	if err := c.SelectServer(0); err != nil {
		t.Fatal(err)
	}
	if processAlive(pid) {
		t.Fatal("plugin still running after switching away")
	}
	if c.plug != nil {
		t.Fatal("controller still holds the plugin")
	}
}

func TestPluginRestartRebuildsCore(t *testing.T) {
	e, path := pluginEnv(t)
	e.writeConfig(func(c *config.Config) { c.Configs = append(c.Configs, e.pluginServer("crash-on=CRASH")) })
	c := e.start()
	trustNow(t, c, path)
	if err := c.SelectServer(1); err != nil {
		t.Fatal(err)
	}
	old := c.plug.LocalPort()
	if conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(old)); err == nil {
		conn.Write([]byte("CRASH"))
		time.Sleep(50 * time.Millisecond)
		conn.Close()
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c.plug.LocalPort() != old && socksEcho(c.State().Port, e.target) == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("traffic did not resume after the plugin restarted")
}

// Start with an untrusted plugin server: never dial the server without its plugin.
func TestStartUntrustedPluginServer(t *testing.T) {
	e, _ := pluginEnv(t)
	e.writeConfig(func(c *config.Config) {
		c.Configs = []config.Server{e.pluginServer("")}
		c.Enabled = true
	})
	c, err := New(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	var nt *PluginNotTrustedError
	if err := c.Start(); !errors.As(err, &nt) {
		t.Fatalf("Start err = %v", err)
	}
	defer c.Shutdown()
	if c.State().Mode != Disabled || len(e.sys.Calls) != 0 {
		t.Fatalf("mode %v sys calls %v", c.State().Mode, e.sys.Calls)
	}
}

func TestSetForwardProxy(t *testing.T) {
	e, path := pluginEnv(t)
	e.writeConfig(func(c *config.Config) { c.Configs = append(c.Configs, e.pluginServer("")) })
	c := e.start()
	hop, count := socks5Hop(t)
	host, ps, _ := net.SplitHostPort(hop)
	port, _ := strconv.Atoi(ps)
	fp := config.ForwardProxy{UseProxy: true, ProxyType: config.ProxySOCKS5, ProxyServer: host, ProxyPort: port, ProxyTimeout: 3}
	if err := c.SetForwardProxy(fp); err != nil {
		t.Fatal(err)
	}
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
	if count.Load() < 1 {
		t.Fatal("not through the forward proxy")
	}
	bad := fp
	bad.ProxyPort = 0
	var fe config.FieldError
	if err := c.SetForwardProxy(bad); !errors.As(err, &fe) || fe.Field != "proxyPort" {
		t.Fatalf("err = %v", err)
	}
	// plugin server: forward proxy not used, and the state says so
	trustNow(t, c, path)
	if err := c.SelectServer(1); err != nil {
		t.Fatal(err)
	}
	before := count.Load()
	if err := socksEcho(c.State().Port, e.target); err != nil {
		t.Fatal(err)
	}
	if !c.State().ForwardIgnored || count.Load() != before {
		t.Fatalf("ForwardIgnored=%v hop count %d→%d", c.State().ForwardIgnored, before, count.Load())
	}
}

// socks5Hop is a counting SOCKS5 proxy (a shadowsocks-go socks5 server
// behind a counting relay).
func socks5Hop(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	addr := "127.0.0.1:" + strconv.Itoa(freePort(t))
	startSSGo(t, `{"servers":[{"name":"hop","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"`+addr+`"}]}],
		"clients":[{"name":"direct","protocol":"direct","enableTCP":true}]}`)
	var n atomic.Int32
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			cn, err := ln.Accept()
			if err != nil {
				return
			}
			n.Add(1)
			go func() {
				defer cn.Close()
				d, err := net.Dial("tcp", addr)
				if err != nil {
					return
				}
				defer d.Close()
				go io.Copy(d, cn)
				io.Copy(cn, d)
			}()
		}
	}()
	return ln.Addr().String(), &n
}

func filepathEqual(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b) || (len(a) == len(b) && equalFold(a, b))
}

func equalFold(a, b string) bool {
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 32
		}
		if 'A' <= y && y <= 'Z' {
			y += 32
		}
		if x != y {
			return false
		}
	}
	return true
}
