package core

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/thc5787/ssmygo/internal/core/socks5test"
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
	"github.com/thc5787/ssmygo/internal/traffic"
)

func quietLogger() *tslog.Logger {
	l, _ := tslog.Config{Level: 8}.NewLogger(io.Discard)
	return l
}

func freePort(t *testing.T) int { return socks5test.FreePort(t) }

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
	js := fmt.Sprintf(`{"servers":[{"name":"up","protocol":%[1]q,"mtu":1500,"tcpListeners":[{"network":"tcp","address":%[2]q}],
		"udpListeners":[{"network":"udp","address":%[2]q}]%[3]s}],
		"clients":[{"name":"direct","protocol":"direct","enableTCP":true,"enableUDP":true,"mtu":1500}]}`, protocol, addr, pskJSON)
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

	r := NewRunner(quietLogger(), nil)
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
	r := NewRunner(quietLogger(), nil)
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
	r := NewRunner(quietLogger(), nil)
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
	r := NewRunner(quietLogger(), nil)
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
	r := NewRunner(quietLogger(), nil)
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
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	if err := r.Apply(sc); err != nil {
		t.Fatal(err)
	}
	if err := socksEcho(cfg.LocalPort, echoServer(t), "leak?"); err == nil {
		t.Fatal("traffic went direct while the plugin was not running")
	}
}

func udpEcho(t *testing.T) netip.AddrPort {
	t.Helper()
	a, stop, err := socks5test.UDPEcho()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return a
}

// udpExchange sends msg to target over a UDP association and waits for it back.
func udpExchange(s *socks5test.Session, target netip.AddrPort, msg string, wait time.Duration) error {
	if err := s.Send(target.String(), []byte(msg)); err != nil {
		return err
	}
	got, err := s.Recv(wait)
	if err != nil {
		return err
	}
	if string(got) != msg {
		return fmt.Errorf("echo %q, want %q", got, msg)
	}
	return nil
}

func applyGlobal(t *testing.T, r *Runner, s config.Server, pluginPort int) int {
	t.Helper()
	cfg := config.Default()
	cfg.LocalPort = freePort(t)
	cfg.Global = true
	cfg.Configs = []config.Server{s}
	sc, err := Build(cfg, nil, pluginPort)
	if err != nil && !errors.Is(err, ErrPluginNotRunning) {
		t.Fatal(err)
	}
	if err := r.Apply(sc); err != nil {
		t.Fatal(err)
	}
	return cfg.LocalPort
}

func associate(t *testing.T, port int) *socks5test.Session {
	t.Helper()
	s, err := socks5test.Associate(fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestRunnerUDPEndToEnd(t *testing.T) {
	target := udpEcho(t)
	psk := make([]byte, 16)
	rand.Read(psk)
	cases := []struct {
		method, password string
		addr             netip.AddrPort
	}{
		{"aes-256-gcm", "pw", aeadtest.StartUDP(t, "aes-256-gcm", "pw").Addr},
		{"2022-blake3-aes-128-gcm", base64.StdEncoding.EncodeToString(psk), ssgoServer(t, "2022-blake3-aes-128-gcm", psk)},
		{"none", "", ssgoServer(t, "none", nil)},
	}
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	for _, c := range cases {
		t.Run(c.method, func(t *testing.T) {
			port := applyGlobal(t, r, config.Server{Server: c.addr.Addr().String(), ServerPort: int(c.addr.Port()), Method: c.method, Password: c.password}, 0)
			s := associate(t, port)
			if err := udpExchange(s, target, "udp "+c.method, 3*time.Second); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Review Focus 2: parallel associations each get only their own replies.
func TestRunnerUDPConcurrentSessions(t *testing.T) {
	target := udpEcho(t)
	srv := aeadtest.StartUDP(t, "aes-128-gcm", "pw")
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	port := applyGlobal(t, r, config.Server{Server: "127.0.0.1", ServerPort: int(srv.Addr.Port()), Method: "aes-128-gcm", Password: "pw"}, 0)
	errs := make(chan error, 20)
	for i := range 20 {
		go func() {
			s, err := socks5test.Associate(fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				errs <- err
				return
			}
			defer s.Close()
			for j := range 5 {
				if err := udpExchange(s, target, fmt.Sprintf("session %d packet %d", i, j), 3*time.Second); err != nil {
					errs <- fmt.Errorf("session %d: %w", i, err)
					return
				}
			}
			errs <- nil
		}()
	}
	for range 20 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

// Review Focus 5: TCP goes to the plugin, UDP to the real server; with the
// plugin down UDP fails closed.
func TestRunnerUDPPluginServer(t *testing.T) {
	target := udpEcho(t)
	srv := aeadtest.StartUDP(t, "aes-256-gcm", "pw")
	plugin, err := net.Listen("tcp", "127.0.0.1:0") // a plugin that never relays UDP
	if err != nil {
		t.Fatal(err)
	}
	defer plugin.Close()
	s := config.Server{Server: "127.0.0.1", ServerPort: int(srv.Addr.Port()), Method: "aes-256-gcm", Password: "pw", Plugin: "fake-plugin"}
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	port := applyGlobal(t, r, s, plugin.Addr().(*net.TCPAddr).Port)
	if err := udpExchange(associate(t, port), target, "via server", 3*time.Second); err != nil {
		t.Fatal(err)
	}
	port = applyGlobal(t, r, s, 0)
	if err := udpExchange(associate(t, port), target, "leak?", time.Second); err == nil {
		t.Fatal("UDP went out while the plugin was not running")
	}
}

// A2-D2: forward proxy on → UDP ASSOCIATE refused.
func TestRunnerUDPOffWithForward(t *testing.T) {
	cfg := config.Default()
	cfg.LocalPort = freePort(t)
	cfg.Global = true
	cfg.Configs = []config.Server{{Server: "127.0.0.1", ServerPort: 9, Method: "aes-256-gcm", Password: "pw"}}
	cfg.Proxy = config.ForwardProxy{UseProxy: true, ProxyType: config.ProxySOCKS5, ProxyServer: "127.0.0.1", ProxyPort: 9, ProxyTimeout: 3}
	sc, err := Build(cfg, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	if err := r.Apply(sc); err != nil {
		t.Fatal(err)
	}
	_, err = socks5test.Associate(fmt.Sprintf("127.0.0.1:%d", cfg.LocalPort))
	var refused socks5test.ErrRefused
	if !errors.As(err, &refused) || refused.Reply != 7 {
		t.Fatalf("err = %v, want refused 7", err)
	}
}

// Review Focus 4: only the UDP port is taken → a listen error, nothing runs.
func TestRunnerUDPPortBusy(t *testing.T) {
	u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	cfg := config.Default()
	cfg.LocalPort = u.LocalAddr().(*net.UDPAddr).Port
	sc, _ := Build(cfg, nil, 0)
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	err = r.Apply(sc)
	var op *net.OpError
	if !errors.As(err, &op) || op.Op != "listen" {
		t.Fatalf("err = %v, want a listen *net.OpError", err)
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.LocalPort), 300*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("TCP listener left running after the UDP listen failed")
	}
}

// Final review #1: rules apply to every UDP packet, not just a session's
// first. One association: private (direct), proxied domain (server), private.
func TestRunnerUDPRoutesEveryPacket(t *testing.T) {
	rs, err := rules.Build(assets.DLC(), []string{"private", "cn"}, []string{"geolocation-!cn"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := aeadtest.StartUDP(t, "aes-256-gcm", "pw")
	cfg := config.Default()
	cfg.LocalPort = freePort(t)
	cfg.Enabled = true
	cfg.Configs = []config.Server{{Server: "127.0.0.1", ServerPort: int(srv.Addr.Port()), Method: "aes-256-gcm", Password: "pw"}}
	sc, err := Build(cfg, &rs, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	if err := r.Apply(sc); err != nil {
		t.Fatal(err)
	}
	target := udpEcho(t)
	s := associate(t, cfg.LocalPort)
	if err := udpExchange(s, target, "lan first", 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if n := srv.Packets(); n != 0 {
		t.Fatalf("private target reached the server (%d packets)", n)
	}
	// A proxied domain on the same socket must reach the server, not go direct.
	if err := s.Send("nx-ssmygo-test.google.com:9", []byte("proxied")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for srv.Packets() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.Packets() != 1 || srv.LastATYP() != 3 {
		t.Fatalf("proxied packet: server packets %d ATYP %d, want 1 and 3", srv.Packets(), srv.LastATYP())
	}
	if err := udpExchange(s, target, "lan again", 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if srv.Packets() != 1 {
		t.Fatalf("private target reached the server after a proxied packet")
	}
}

// Patch A4 end to end: legacy AEAD UDP is counted by payload, not by the
// encrypted size.
func TestRunnerCountsUDPPayload(t *testing.T) {
	target := udpEcho(t)
	srv := aeadtest.StartUDP(t, "aes-256-gcm", "pw")
	var m traffic.Meter
	r := NewRunner(quietLogger(), &m)
	defer r.Stop()
	port := applyGlobal(t, r, config.Server{Server: "127.0.0.1", ServerPort: int(srv.Addr.Port()), Method: "aes-256-gcm", Password: "pw"}, 0)
	if err := udpExchange(associate(t, port), target, strings.Repeat("x", 1200), 3*time.Second); err != nil {
		t.Fatal(err)
	}
	// The reply is counted just after it is written: allow it to land.
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if _, down := m.Totals(); down >= 1200 {
			break
		}
	}
	if up, down := m.Totals(); up != 1200 || down != 1200 {
		t.Fatalf("meter %d/%d, want 1200/1200", up, down)
	}
}

func TestRunnerNilMeter(t *testing.T) {
	target := udpEcho(t)
	srv := aeadtest.StartUDP(t, "aes-256-gcm", "pw")
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	port := applyGlobal(t, r, config.Server{Server: "127.0.0.1", ServerPort: int(srv.Addr.Port()), Method: "aes-256-gcm", Password: "pw"}, 0)
	if err := udpExchange(associate(t, port), target, "no meter", 3*time.Second); err != nil {
		t.Fatal(err)
	}
}
