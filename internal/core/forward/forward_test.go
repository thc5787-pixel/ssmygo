package forward

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/tslog"
)

// Review Focus 4: special characters (HTTP Basic forbids ":" in the user, RFC 7617).
const user, pass = "b@c d 名", "密码 p:@ss"

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func run(t *testing.T, js string) error {
	t.Helper()
	var cfg service.Config
	if err := json.Unmarshal([]byte(js), &cfg); err != nil {
		t.Fatal(err)
	}
	l, _ := tslog.Config{Level: 8}.NewLogger(io.Discard)
	m, err := cfg.NewManager(l)
	if err != nil {
		return err
	}
	if err := m.Start(context.Background()); err != nil {
		m.Close()
		return err
	}
	t.Cleanup(func() { m.Stop(); m.Close() })
	return nil
}

func echo(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
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

func relay(t *testing.T, dst string) (string, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
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
	return ln.Addr().String(), &n
}

func socksEcho(addr string, target netip.AddrPort) error {
	c, err := net.DialTimeout("tcp", addr, time.Second)
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
	c.Write([]byte("fwd"))
	got := make([]byte, 3)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "fwd" {
		return fmt.Errorf("echo %q %v", got, err)
	}
	return nil
}

// chain: local socks5 → x(none, dialerClient forward) → forward(protocol) →
// relay(count) → hop server → ssnone server → echo.
func chain(t *testing.T, protocol, hopServer, clientAuth string, timeout string) (string, *atomic.Int32, error) {
	t.Helper()
	ss := freeAddr(t)
	if err := run(t, `{"servers":[{"name":"ss","protocol":"none","tcpListeners":[{"network":"tcp","address":"`+ss+`"}]}],
		"clients":[{"name":"direct","protocol":"direct","enableTCP":true}]}`); err != nil {
		t.Fatal(err)
	}
	hop := freeAddr(t)
	if err := run(t, fmt.Sprintf(`{"servers":[%s],"clients":[{"name":"direct","protocol":"direct","enableTCP":true}]}`,
		fmt.Sprintf(hopServer, hop))); err != nil {
		t.Fatal(err)
	}
	r, n := relay(t, hop)
	local := freeAddr(t)
	err := run(t, fmt.Sprintf(`{"servers":[{"name":"local","protocol":"socks5","tcpListeners":[{"network":"tcp","address":%q}]}],
		"clients":[{"name":"forward","protocol":%q,"endpoint":%q,"enableTCP":true,"dialTimeout":%q%s},
		           {"name":"x","protocol":"none","endpoint":%q,"enableTCP":true,"dialerClient":"forward"}],
		"router":{"defaultTCPClientName":"x"}}`, local, protocol, r, timeout, clientAuth, ss))
	return local, n, err
}

const (
	socksHop = `{"name":"hop","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"%s"}]}`
	httpHop  = `{"name":"hop","protocol":"http","tcpListeners":[{"network":"tcp","address":"%s"}]}`
)

func TestSOCKS5Hop(t *testing.T) {
	local, n, err := chain(t, ProtocolSOCKS5, socksHop, "", "3s")
	if err != nil {
		t.Fatal(err)
	}
	if err := socksEcho(local, echo(t)); err != nil {
		t.Fatal(err)
	}
	if n.Load() < 1 {
		t.Fatal("not through the forward proxy")
	}
}

func TestHTTPHop(t *testing.T) {
	local, n, err := chain(t, ProtocolHTTP, httpHop, "", "3s")
	if err != nil {
		t.Fatal(err)
	}
	if err := socksEcho(local, echo(t)); err != nil {
		t.Fatal(err)
	}
	if n.Load() < 1 {
		t.Fatal("not through the forward proxy")
	}
}

func TestAuth(t *testing.T) {
	users := fmt.Sprintf(`"users":[{"username":%q,"password":%q}]`, user, pass)
	socksAuthHop := `{"name":"hop","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"%s"}],"socks5":{"enableUserPassAuth":true,` + users + `}}`
	httpAuthHop := `{"name":"hop","protocol":"http","tcpListeners":[{"network":"tcp","address":"%s"}],"http":{"enableBasicAuth":true,` + users + `}}`
	for _, tc := range []struct {
		name, protocol, hop, good, bad string
	}{
		{"socks5", ProtocolSOCKS5, socksAuthHop,
			fmt.Sprintf(`,"socks5":{"enableUserPassAuth":true,"username":%q,"password":%q}`, user, pass),
			fmt.Sprintf(`,"socks5":{"enableUserPassAuth":true,"username":%q,"password":"wrong"}`, user)},
		{"http", ProtocolHTTP, httpAuthHop,
			fmt.Sprintf(`,"http":{"useBasicAuth":true,"username":%q,"password":%q}`, user, pass),
			fmt.Sprintf(`,"http":{"useBasicAuth":true,"username":%q,"password":"wrong"}`, user)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local, _, err := chain(t, tc.protocol, tc.hop, tc.good, "3s")
			if err != nil {
				t.Fatal(err)
			}
			if err := socksEcho(local, echo(t)); err != nil {
				t.Fatalf("good credentials: %v", err)
			}
			local, _, err = chain(t, tc.protocol, tc.hop, tc.bad, "3s")
			if err != nil {
				t.Fatal(err)
			}
			if err := socksEcho(local, echo(t)); err == nil {
				t.Fatal("wrong password accepted")
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	// a forward proxy that accepts and never answers
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	local := freeAddr(t)
	if err := run(t, fmt.Sprintf(`{"servers":[{"name":"local","protocol":"socks5","tcpListeners":[{"network":"tcp","address":%q}]}],
		"clients":[{"name":"forward","protocol":%q,"endpoint":%q,"enableTCP":true,"dialTimeout":"1s"},
		           {"name":"x","protocol":"none","endpoint":"127.0.0.1:9","enableTCP":true,"dialerClient":"forward"}],
		"router":{"defaultTCPClientName":"x"}}`, local, ProtocolSOCKS5, ln.Addr().String())); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err := socksEcho(local, echo(t))
	if err == nil {
		t.Fatal("dial through a silent proxy succeeded")
	}
	if d := time.Since(start); d > 2500*time.Millisecond {
		t.Fatalf("gave up after %v, want ~1s", d)
	}
}

// The handshake deadline must not outlive the handshake: a connection idle
// for longer than the timeout keeps working.
func TestDeadlineClearedAfterHandshake(t *testing.T) {
	for _, tc := range []struct{ name, protocol, hop string }{{"socks5", ProtocolSOCKS5, socksHop}, {"http", ProtocolHTTP, httpHop}} {
		t.Run(tc.name, func(t *testing.T) {
			local, _, err := chain(t, tc.protocol, tc.hop, "", "1s")
			if err != nil {
				t.Fatal(err)
			}
			target := echo(t)
			c, err := net.DialTimeout("tcp", local, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(6 * time.Second))
			c.Write([]byte{5, 1, 0})
			b := make([]byte, 10)
			io.ReadFull(c, b[:2])
			req := append([]byte{5, 1, 0, 1}, target.Addr().AsSlice()...)
			req = binary.BigEndian.AppendUint16(req, target.Port())
			c.Write(req)
			if _, err := io.ReadFull(c, b); err != nil || b[1] != 0 {
				t.Fatalf("connect %v %v", b, err)
			}
			time.Sleep(1500 * time.Millisecond) // longer than dialTimeout
			c.Write([]byte("late"))
			got := make([]byte, 4)
			if _, err := io.ReadFull(c, got); err != nil || string(got) != "late" {
				t.Fatalf("idle connection died: %q %v", got, err)
			}
		})
	}
}
