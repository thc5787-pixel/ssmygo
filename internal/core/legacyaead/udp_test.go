package legacyaead_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/tslog"
	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/socks"
	"github.com/thc5787/ssmygo/internal/core/legacyaead/aeadtest"
	"github.com/thc5787/ssmygo/internal/core/socks5test"
)

// freePort returns a port number free for both TCP and UDP on 127.0.0.1.
func freePort(t *testing.T) int {
	t.Helper()
	for range 20 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := ln.Addr().(*net.TCPAddr).Port
		u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p})
		ln.Close()
		if err == nil {
			u.Close()
			return p
		}
	}
	t.Fatal("no free port")
	return 0
}

// udpRelay runs a shadowsocks-go Manager: a local socks5 server (TCP+UDP on
// one port) and a client of method pointing at server. It returns the local
// TCP address for UDP ASSOCIATE.
func udpRelay(t *testing.T, method, password string, server netip.AddrPort) string {
	t.Helper()
	local := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	js := fmt.Sprintf(`{"servers":[{"name":"s","protocol":"socks5","mtu":1500,
		"tcpListeners":[{"network":"tcp","address":%[1]q}],
		"udpListeners":[{"network":"udp","address":%[1]q}]}],
		"clients":[{"name":"x","protocol":%[2]q,"endpoint":%[3]q,"enableTCP":true,"enableUDP":true,"mtu":1500,"password":%[4]q}]}`,
		local, method, server.String(), password)
	var cfg service.Config
	if err := json.Unmarshal([]byte(js), &cfg); err != nil {
		t.Fatal(err)
	}
	logger, _ := tslog.Config{Level: 8}.NewLogger(io.Discard)
	mgr, err := cfg.NewManager(logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.Stop(); mgr.Close() })
	return local
}

func associate(t *testing.T, local string) *socks5test.Session {
	t.Helper()
	s, err := socks5test.Associate(local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func echo(t *testing.T, ip string) netip.AddrPort {
	t.Helper()
	a, stop, err := socks5test.UDPEchoAt(ip)
	if err != nil {
		t.Skipf("no UDP on %s: %v", ip, err)
	}
	t.Cleanup(stop)
	return a
}

// exchange sends p to target and waits for its echo.
func exchange(t *testing.T, s *socks5test.Session, target string, p []byte) {
	t.Helper()
	if err := s.Send(target, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recv(3 * time.Second)
	if err != nil {
		t.Fatalf("%d bytes to %s: %v", len(p), target, err)
	}
	if !bytes.Equal(got, p) {
		t.Fatalf("got %d bytes, want %d", len(got), len(p))
	}
}

func TestUDPRoundTrip(t *testing.T) {
	target := echo(t, "127.0.0.1")
	for _, m := range []string{"aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305"} {
		t.Run(m, func(t *testing.T) {
			srv := aeadtest.StartUDP(t, m, "pw-"+m)
			s := associate(t, udpRelay(t, m, "pw-"+m, srv.Addr))
			for _, n := range []int{1, 0, 1200} {
				p := make([]byte, n)
				rand.Read(p)
				exchange(t, s, target.String(), p)
			}
		})
	}
}

// Review Focus 3: a domain target travels as ATYP=3 and is resolved by the server.
func TestUDPTargets(t *testing.T) {
	v4 := echo(t, "127.0.0.1")
	srv := aeadtest.StartUDP(t, "aes-256-gcm", "pw")
	s := associate(t, udpRelay(t, "aes-256-gcm", "pw", srv.Addr))

	exchange(t, s, v4.String(), []byte("v4"))
	if srv.LastATYP() != 1 {
		t.Fatalf("IPv4 target ATYP = %d", srv.LastATYP())
	}
	exchange(t, s, fmt.Sprintf("localhost:%d", v4.Port()), []byte("domain"))
	if srv.LastATYP() != 3 {
		t.Fatalf("domain target ATYP = %d, want 3 (resolved locally?)", srv.LastATYP())
	}
	v6 := echo(t, "::1")
	exchange(t, s, v6.String(), []byte("v6"))
	if srv.LastATYP() != 4 {
		t.Fatalf("IPv6 target ATYP = %d", srv.LastATYP())
	}
}

// Review Focus 1: a datagram that cannot fit the MTU once encrypted never
// leaves, and the relay keeps working.
func TestUDPOversized(t *testing.T) {
	target := echo(t, "127.0.0.1")
	srv := aeadtest.StartUDP(t, "aes-256-gcm", "pw")
	s := associate(t, udpRelay(t, "aes-256-gcm", "pw", srv.Addr))
	// 32 salt + 7 addr + 1450 + 16 tag = 1505 > 1472 (MTU 1500 over IPv4).
	if err := s.Send(target.String(), make([]byte, 1450)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recv(500 * time.Millisecond); err == nil {
		t.Fatal("oversized datagram got a reply")
	}
	if n := srv.Packets(); n != 0 {
		t.Fatalf("server received %d packets, want 0", n)
	}
	exchange(t, s, target.String(), []byte("after"))
}

func TestUDPTampered(t *testing.T) {
	target := echo(t, "127.0.0.1")
	srv := aeadtest.StartUDP(t, "chacha20-ietf-poly1305", "pw")
	s := associate(t, udpRelay(t, "chacha20-ietf-poly1305", "pw", srv.Addr))
	exchange(t, s, target.String(), []byte("warm-up"))
	srv.TamperNext(1)
	if err := s.Send(target.String(), []byte("bad")); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Recv(500 * time.Millisecond); err == nil {
		t.Fatalf("tampered reply delivered: %q", got)
	}
	exchange(t, s, target.String(), []byte("good"))
}

// startGoSS2UDP runs go-shadowsocks2's UDP server side as an independent peer.
func startGoSS2UDP(t *testing.T, name, password string) netip.AddrPort {
	t.Helper()
	ciph, err := core.PickCipher(name, nil, password)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pc := ciph.PacketConn(raw)
	t.Cleanup(func() { pc.Close() })
	var mu sync.Mutex
	nats := map[string]net.PacketConn{}
	go func() {
		b := make([]byte, 65535)
		for {
			n, from, err := pc.ReadFrom(b)
			if err != nil {
				return
			}
			tgt := socks.SplitAddr(b[:n])
			if tgt == nil {
				continue
			}
			ua, err := net.ResolveUDPAddr("udp", tgt.String())
			if err != nil {
				continue
			}
			mu.Lock()
			out := nats[from.String()]
			if out == nil {
				out, _ = net.ListenPacket("udp", "127.0.0.1:0")
				nats[from.String()] = out
				t.Cleanup(func() { out.Close() })
				go func() {
					rb := make([]byte, 65535)
					for {
						m, src, err := out.ReadFrom(rb)
						if err != nil {
							return
						}
						pc.WriteTo(append(socks.ParseAddr(src.String()), rb[:m]...), from)
					}
				}()
			}
			mu.Unlock()
			out.WriteTo(b[len(tgt):n], ua)
		}
	}()
	return netip.MustParseAddrPort(raw.LocalAddr().String())
}

func TestUDPInteropGoShadowsocks2(t *testing.T) {
	target := echo(t, "127.0.0.1")
	for method, name := range map[string]string{
		"aes-128-gcm":            "AEAD_AES_128_GCM",
		"aes-256-gcm":            "AEAD_AES_256_GCM",
		"chacha20-ietf-poly1305": "AEAD_CHACHA20_POLY1305",
	} {
		t.Run(method, func(t *testing.T) {
			srv := startGoSS2UDP(t, name, "interop-pw")
			s := associate(t, udpRelay(t, method, "interop-pw", srv))
			exchange(t, s, target.String(), []byte("interop"))
			exchange(t, s, target.String(), make([]byte, 1200))
		})
	}
}
