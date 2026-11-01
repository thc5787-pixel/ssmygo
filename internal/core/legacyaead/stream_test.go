package legacyaead_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/conn"
	"github.com/database64128/shadowsocks-go/conntest"
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/tslog"
	"github.com/thc5787/ssmygo/internal/core/legacyaead"
	"github.com/thc5787/ssmygo/internal/core/legacyaead/aeadtest"
)

// echoAfterEOF reads everything until the client half-closes, then echoes it
// and closes: it only answers once the request side is finished.
func echoAfterEOF(t *testing.T) netip.AddrPort {
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
			go func() {
				defer c.Close()
				b, _ := io.ReadAll(c)
				c.Write(b)
			}()
		}
	}()
	return netip.MustParseAddrPort(ln.Addr().String())
}

func dialer(t *testing.T, method, password string, server netip.AddrPort) netio.StreamClient {
	t.Helper()
	inner, err := (&netio.TCPClientConfig{Name: "inner", Dialer: conntest.DefaultTCPDialer()}).NewTCPClient()
	if err != nil {
		t.Fatal(err)
	}
	cfg := legacyaead.StreamClientConfig{
		Name:        "t",
		Method:      method,
		Key:         legacyaead.DeriveKey(password, mustKeySize(t, method)),
		InnerClient: inner,
		Addr:        conn.AddrFromIPAndPort(server.Addr(), server.Port()),
	}
	c, err := cfg.NewStreamClient()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustKeySize(t *testing.T, m string) int {
	n, err := legacyaead.KeySize(m)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// roundTrip sends payload, half-closes, and returns what came back.
func roundTrip(t *testing.T, c netio.StreamClient, target netip.AddrPort, payload []byte) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cn, err := c.DialStream(ctx, conn.AddrFromIPAndPort(target.Addr(), target.Port()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cn.Close()
	cn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := cn.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := cn.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(cn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return got
}

func TestRoundTrip(t *testing.T) {
	target := echoAfterEOF(t)
	for _, m := range legacyaead.Methods {
		t.Run(m, func(t *testing.T) {
			srv := aeadtest.Start(t, m, "pw-"+m)
			c := dialer(t, m, "pw-"+m, srv)
			for _, n := range []int{0, 1, 0x3FFF, 0x4000, 1 << 20} {
				p := make([]byte, n)
				rand.Read(p)
				if got := roundTrip(t, c, target, p); !bytes.Equal(got, p) {
					t.Fatalf("size %d: got %d bytes", n, len(got))
				}
			}
		})
	}
}

// Review Focus 5: request, half-close, then the answer must still arrive.
func TestHalfClose(t *testing.T) {
	target := echoAfterEOF(t)
	srv := aeadtest.Start(t, "aes-256-gcm", "pw")
	c := dialer(t, "aes-256-gcm", "pw", srv)
	if got := roundTrip(t, c, target, []byte("GET / HTTP/1.0\r\n\r\n")); string(got) != "GET / HTTP/1.0\r\n\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestWrongPasswordFails(t *testing.T) {
	target := echoAfterEOF(t)
	srv := aeadtest.Start(t, "chacha20-ietf-poly1305", "right")
	c := dialer(t, "chacha20-ietf-poly1305", "wrong", srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cn, err := c.DialStream(ctx, conn.AddrFromIPAndPort(target.Addr(), target.Port()), []byte("x"))
	if err != nil {
		return // refused early is fine
	}
	defer cn.Close()
	cn.SetDeadline(time.Now().Add(5 * time.Second))
	cn.CloseWrite()
	if b, err := io.ReadAll(cn); err == nil && len(b) > 0 {
		t.Fatalf("wrong password produced data %q", b)
	}
}

func TestTamperedTagFails(t *testing.T) {
	a, b := tcpPair(t)
	w := legacyaead.NewConn(a, "aes-128-gcm", legacyaead.DeriveKey("k", 16))
	if _, err := w.Write([]byte("hello, world")); err != nil {
		t.Fatal(err)
	}
	a.CloseWrite()
	raw, _ := io.ReadAll(b)
	raw[len(raw)-1] ^= 0xFF // flip a tag byte

	c, d := tcpPair(t)
	c.Write(raw)
	c.CloseWrite()
	r := legacyaead.NewConn(d, "aes-128-gcm", legacyaead.DeriveKey("k", 16))
	got, err := io.ReadAll(r)
	if err == nil {
		t.Fatalf("tampered stream read without error: %q", got)
	}
	if len(got) != 0 {
		t.Fatalf("tampered stream yielded data %q", got)
	}
}

func tcpPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ch := make(chan net.Conn, 1)
	go func() { c, _ := ln.Accept(); ch <- c }()
	a, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b := <-ch
	t.Cleanup(func() { a.Close(); b.Close() })
	return a.(*net.TCPConn), b.(*net.TCPConn)
}

func TestRegistered(t *testing.T) {
	for _, m := range legacyaead.Methods {
		js := `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"127.0.0.1:0"}]}],
		"clients":[{"name":"x","protocol":"` + m + `","endpoint":"127.0.0.1:8388","enableTCP":true,"password":"x"}]}`
		var cfg service.Config
		if err := json.Unmarshal([]byte(js), &cfg); err != nil {
			t.Fatal(err)
		}
		logger, _ := tslog.Config{Level: 8}.NewLogger(io.Discard)
		mgr, err := cfg.NewManager(logger)
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		mgr.Close()
	}
}

// TestInteropRust runs against a real shadowsocks-rust ssserver when the
// SSSERVER env var points to its binary.
func TestInteropRust(t *testing.T) {
	bin := os.Getenv("SSSERVER")
	if bin == "" {
		t.Skip("SSSERVER not set")
	}
	target := echoAfterEOF(t)
	for _, m := range legacyaead.Methods {
		t.Run(m, func(t *testing.T) {
			addr := aeadtest.StartRust(t, bin, m, "interop")
			c := dialer(t, m, "interop", addr)
			if got := roundTrip(t, c, target, []byte("ping")); string(got) != "ping" {
				t.Fatalf("got %q", got)
			}
		})
	}
}
