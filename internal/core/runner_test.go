package core

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/tslog"
	"github.com/thc5787/ssmygo/assets"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core/legacyaead/aeadtest"
	"github.com/thc5787/ssmygo/internal/rules"
)

func quietLogger() *tslog.Logger {
	l, _ := tslog.Config{Level: 8}.NewLogger(io.Discard)
	return l
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func echoServer(t *testing.T) netip.AddrPort {
	t.Helper()
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

// ssgoServer runs a shadowsocks-go server of protocol (2022 or none).
func ssgoServer(t *testing.T, protocol string, psk []byte) netip.AddrPort {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	pskJSON := ""
	if psk != nil {
		pskJSON = fmt.Sprintf(`,"psk":%q`, base64.StdEncoding.EncodeToString(psk))
	}
	js := fmt.Sprintf(`{"servers":[{"name":"up","protocol":%q,"tcpListeners":[{"network":"tcp","address":%q}]%s}],
		"clients":[{"name":"direct","protocol":"direct","enableTCP":true}]}`, protocol, addr, pskJSON)
	var sc service.Config
	if err := json.Unmarshal([]byte(js), &sc); err != nil {
		t.Fatal(err)
	}
	m, err := sc.NewManager(quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Stop(); m.Close() })
	return netip.MustParseAddrPort(addr)
}

// socksEcho connects through the local SOCKS5 port to target and echoes msg.
func socksEcho(port int, target netip.AddrPort, msg string) error {
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
	if _, err := io.ReadFull(c, b); err != nil {
		return err
	}
	if b[1] != 0 {
		return fmt.Errorf("socks reply %d", b[1])
	}
	c.Write([]byte(msg))
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(c, got); err != nil {
		return err
	}
	if string(got) != msg {
		return fmt.Errorf("echo %q", got)
	}
	return nil
}

func TestRunnerEndToEnd(t *testing.T) {
	target := echoServer(t)
	type upstream struct {
		method, password string
		addr             netip.AddrPort
	}
	var ups []upstream
	for _, m := range []string{"aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305"} {
		ups = append(ups, upstream{m, "pw-" + m, aeadtest.Start(t, m, "pw-"+m)})
	}
	for _, m := range []string{"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"} {
		psk := make([]byte, config.PSKSize(m))
		rand.Read(psk)
		ups = append(ups, upstream{m, base64.StdEncoding.EncodeToString(psk), ssgoServer(t, m, psk)})
	}
	ups = append(ups, upstream{"none", "", ssgoServer(t, "none", nil)})

	r := NewRunner(quietLogger())
	defer r.Stop()
	for _, u := range ups {
		t.Run(u.method, func(t *testing.T) {
			cfg := config.Default()
			cfg.LocalPort = freePort(t)
			cfg.Global = true
			cfg.Configs = []config.Server{{Server: u.addr.Addr().String(), ServerPort: int(u.addr.Port()), Method: u.method, Password: u.password}}
			sc, err := Build(cfg, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Apply(sc); err != nil {
				t.Fatal(err)
			}
			if err := socksEcho(cfg.LocalPort, target, "hello "+u.method); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunnerApplyBusyPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	cfg := config.Default()
	cfg.LocalPort = ln.Addr().(*net.TCPAddr).Port
	sc, _ := Build(cfg, nil, 0)
	r := NewRunner(quietLogger())
	defer r.Stop()
	if err := r.Apply(sc); err == nil || !strings.Contains(err.Error(), "bind") {
		t.Fatalf("err = %v", err)
	}
	cfg.LocalPort = freePort(t)
	sc, _ = Build(cfg, nil, 0)
	if err := r.Apply(sc); err != nil {
		t.Fatalf("apply on free port after failure: %v", err)
	}
}

func TestRunnerReapply(t *testing.T) {
	target := echoServer(t)
	cfg := config.Default()
	cfg.LocalPort = freePort(t)
	r := NewRunner(quietLogger())
	defer r.Stop()
	for i := 0; i < 3; i++ {
		sc, _ := Build(cfg, nil, 0) // ErrNoServer: direct only
		if err := r.Apply(sc); err != nil {
			t.Fatalf("apply #%d: %v", i, err)
		}
	}
	if err := socksEcho(cfg.LocalPort, target, "direct"); err != nil {
		t.Fatal(err)
	}
}

// Rules mode with real domain sets must actually start (not just marshal).
func TestRunnerAppliesRulesMode(t *testing.T) {
	rs, err := rules.Build(assets.DLC(), []string{"private", "cn"}, []string{"geolocation-!cn"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	up := aeadtest.Start(t, "aes-256-gcm", "pw")
	cfg := config.Default()
	cfg.LocalPort = freePort(t)
	cfg.Enabled = true
	cfg.Configs = []config.Server{{Server: up.Addr().String(), ServerPort: int(up.Port()), Method: "aes-256-gcm", Password: "pw"}}
	sc, err := Build(cfg, &rs, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(quietLogger())
	defer r.Stop()
	if err := r.Apply(sc); err != nil {
		t.Fatalf("rules mode does not start: %v", err)
	}
	if err := socksEcho(cfg.LocalPort, echoServer(t), "rules"); err != nil {
		t.Fatal(err)
	}
}

// Connections to the SS server go through the forward proxy.
func TestRunnerThroughForward(t *testing.T) {
	up := aeadtest.Start(t, "aes-256-gcm", "pw")
	hop := ssgoSOCKS5(t)
	var n atomic.Int32
	relay := countingRelay(t, hop, &n)
	host, portStr, _ := net.SplitHostPort(relay)
	port, _ := strconv.Atoi(portStr)
	cfg := config.Default()
	cfg.LocalPort = freePort(t)
	cfg.Global = true
	cfg.Configs = []config.Server{{Server: up.Addr().String(), ServerPort: int(up.Port()), Method: "aes-256-gcm", Password: "pw"}}
	cfg.Proxy = config.ForwardProxy{UseProxy: true, ProxyType: config.ProxySOCKS5, ProxyServer: host, ProxyPort: port, ProxyTimeout: 3}
	sc, err := Build(cfg, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(quietLogger())
	defer r.Stop()
	if err := r.Apply(sc); err != nil {
		t.Fatal(err)
	}
	if err := socksEcho(cfg.LocalPort, echoServer(t), "via forward"); err != nil {
		t.Fatal(err)
	}
	if n.Load() < 1 {
		t.Fatal("did not go through the forward proxy")
	}
}

func ssgoSOCKS5(t *testing.T) string {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	var sc service.Config
	json.Unmarshal([]byte(`{"servers":[{"name":"hop","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"`+addr+`"}]}],
		"clients":[{"name":"direct","protocol":"direct","enableTCP":true}]}`), &sc)
	m, err := sc.NewManager(quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Stop(); m.Close() })
	return addr
}

func countingRelay(t *testing.T, dst string, n *atomic.Int32) string {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n.Add(1)
			go func() {
				defer c.Close()
				d, err := net.Dial("tcp", dst)
				if err != nil {
					return
				}
				defer d.Close()
				go io.Copy(d, c)
				io.Copy(c, d)
			}()
		}
	}()
	return ln.Addr().String()
}

// While a plugin server's plugin is not running, traffic meant for the
// server fails instead of silently going direct.
func TestPluginNotRunningFailsClosed(t *testing.T) {
	cfg := config.Default()
	cfg.LocalPort = freePort(t)
	cfg.Global = true
	cfg.Configs = []config.Server{{Server: "127.0.0.1", ServerPort: 9, Method: "aes-256-gcm", Password: "pw", Plugin: "v2ray-plugin"}}
	sc, err := Build(cfg, nil, 0)
	if !errors.Is(err, ErrPluginNotRunning) {
		t.Fatal(err)
	}
	r := NewRunner(quietLogger())
	defer r.Stop()
	if err := r.Apply(sc); err != nil {
		t.Fatal(err)
	}
	if err := socksEcho(cfg.LocalPort, echoServer(t), "leak?"); err == nil {
		t.Fatal("traffic went direct while the plugin was not running")
	}
}
