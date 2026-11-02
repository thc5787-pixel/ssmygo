package mixed

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/tslog"
	"github.com/thc5787/ssmygo/internal/core/socks5test"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// startMixed runs a Manager with a "mixed" server and a direct client.
func startMixed(t *testing.T) string { return startMixedMeter(t, nil) }

func startMixedMeter(t *testing.T, meter service.TrafficMeter) string {
	t.Helper()
	addr := freeAddr(t)
	js := fmt.Sprintf(`{"servers":[{"name":"local","protocol":"mixed","tcpListeners":[{"network":"tcp","address":%q}]}],
	"clients":[{"name":"direct","protocol":"direct","enableTCP":true}]}`, addr)
	var cfg service.Config
	if err := json.Unmarshal([]byte(js), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Servers[0].TrafficMeter = meter
	logger, _ := tslog.Config{Level: 8}.NewLogger(io.Discard)
	m, err := cfg.NewManager(logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Stop(); m.Close() })
	return addr
}

// echoAfterEOF answers with everything it read once the client half-closes.
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

func TestSocks5Connect(t *testing.T) {
	proxy := startMixed(t)
	target := echoAfterEOF(t)
	c, err := net.Dial("tcp", proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte{5, 1, 0})
	buf := make([]byte, 10)
	if _, err := io.ReadFull(c, buf[:2]); err != nil || buf[1] != 0 {
		t.Fatalf("greeting %v %v", buf[:2], err)
	}
	req := append([]byte{5, 1, 0, 1}, target.Addr().AsSlice()...)
	req = binary.BigEndian.AppendUint16(req, target.Port())
	c.Write(req)
	if _, err := io.ReadFull(c, buf); err != nil || buf[1] != 0 {
		t.Fatalf("reply %v %v", buf, err)
	}
	c.Write([]byte("socks"))
	c.(*net.TCPConn).CloseWrite()
	got, _ := io.ReadAll(c)
	if string(got) != "socks" {
		t.Fatalf("got %q", got)
	}
}

func TestHTTPConnect(t *testing.T) {
	proxy := startMixed(t)
	target := echoAfterEOF(t)
	c, err := net.Dial("tcp", proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("CONNECT: %v %v", resp, err)
	}
	c.Write([]byte("tunnel"))
	c.(*net.TCPConn).CloseWrite()
	got, _ := io.ReadAll(br)
	if string(got) != "tunnel" {
		t.Fatalf("got %q", got)
	}
}

func TestHTTPPlainGet(t *testing.T) {
	proxy := startMixed(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello via proxy")
	}))
	defer ts.Close()
	pu, _ := url.Parse("http://" + proxy)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}, Timeout: 5 * time.Second}
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "hello via proxy" {
		t.Fatalf("body %q", b)
	}
}

func TestFirstByteTimeout(t *testing.T) {
	old := DefaultFirstByteTimeout
	DefaultFirstByteTimeout = 100 * time.Millisecond
	defer func() { DefaultFirstByteTimeout = old }()
	proxy := startMixed(t)
	c, err := net.Dial("tcp", proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(time.Second))
	_, err = c.Read(make([]byte, 1))
	if err == nil || strings.Contains(err.Error(), "timeout") {
		t.Fatalf("server did not close idle conn within 1s: %v", err)
	}
}

// Review Focus 5: HTTP/1.0-style request then half-close; response arrives.
func TestHalfCloseHTTP(t *testing.T) {
	proxy := startMixed(t)
	target := echoAfterEOF(t)
	c, err := net.Dial("tcp", proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "CONNECT %s HTTP/1.0\r\n\r\n", target)
	br := bufio.NewReader(c)
	if resp, err := http.ReadResponse(br, nil); err != nil || resp.StatusCode != 200 {
		t.Fatalf("CONNECT: %v", err)
	}
	c.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
	c.(*net.TCPConn).CloseWrite()
	got, _ := io.ReadAll(br)
	if string(got) != "GET / HTTP/1.0\r\n\r\n" {
		t.Fatalf("got %q", got)
	}
}

// associateReply sends UDP ASSOCIATE over a fresh connection to addr and
// returns the reply code and bound address; the connection stays open.
func associateReply(t *testing.T, addr string) (byte, netip.AddrPort) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(3 * time.Second))
	c.Write([]byte{5, 1, 0})
	b := make([]byte, 10)
	if _, err := io.ReadFull(c, b[:2]); err != nil {
		t.Fatal(err)
	}
	c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	if _, err := io.ReadFull(c, b[:10]); err != nil {
		t.Fatal(err)
	}
	if b[3] != 1 {
		t.Fatalf("bound address type %d", b[3])
	}
	return b[1], netip.AddrPortFrom(netip.AddrFrom4([4]byte(b[4:8])), binary.BigEndian.Uint16(b[8:10]))
}

func TestAssociateAllowed(t *testing.T) {
	addr := fmt.Sprintf("127.0.0.1:%d", socks5test.FreePort(t))
	js := fmt.Sprintf(`{"servers":[
		{"name":"local","protocol":"mixed+udp","tcpListeners":[{"network":"tcp","address":%[1]q}]},
		{"name":"local-udp","protocol":"socks5","mtu":1500,"udpListeners":[{"network":"udp","address":%[1]q}]}],
	"clients":[{"name":"direct","protocol":"direct","enableTCP":true,"enableUDP":true,"mtu":1500}]}`, addr)
	var cfg service.Config
	if err := json.Unmarshal([]byte(js), &cfg); err != nil {
		t.Fatal(err)
	}
	logger, _ := tslog.Config{Level: 8}.NewLogger(io.Discard)
	m, err := cfg.NewManager(logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Stop(); m.Close() })

	rep, bound := associateReply(t, addr)
	if rep != 0 || bound.String() != addr {
		t.Fatalf("reply %d bound %s, want 0 %s", rep, bound, addr)
	}
	// The datagrams then flow through the UDP-only socks5 server.
	target, stop, err := socks5test.UDPEcho()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	s, err := socks5test.Associate(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Send(target.String(), []byte("hi"))
	if got, err := s.Recv(3 * time.Second); err != nil || string(got) != "hi" {
		t.Fatalf("echo = %q, %v", got, err)
	}
}

// A2-D2: plain "mixed" (forward proxy in use) refuses UDP.
func TestAssociateRefused(t *testing.T) {
	if rep, _ := associateReply(t, startMixed(t)); rep != 7 {
		t.Fatalf("reply %d, want 7 (command not supported)", rep)
	}
}

type testMeter struct{ up, down atomic.Uint64 }

func (m *testMeter) Add(up, down uint64) { m.up.Add(up); m.down.Add(down) }

// waitMeter waits briefly: a reply is counted just after it is written.
func waitMeter(m *testMeter, up, down uint64) (uint64, uint64) {
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if m.up.Load() >= up && m.down.Load() >= down {
			break
		}
	}
	return m.up.Load(), m.down.Load()
}

// socksConnect opens a SOCKS5 CONNECT to target through proxy.
func socksConnect(t *testing.T, proxy string, target netip.AddrPort) *net.TCPConn {
	t.Helper()
	c, err := net.Dial("tcp", proxy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte{5, 1, 0})
	buf := make([]byte, 10)
	io.ReadFull(c, buf[:2])
	req := append([]byte{5, 1, 0, 1}, target.Addr().AsSlice()...)
	req = binary.BigEndian.AppendUint16(req, target.Port())
	c.Write(req)
	if _, err := io.ReadFull(c, buf); err != nil || buf[1] != 0 {
		t.Fatalf("reply %v %v", buf, err)
	}
	return c.(*net.TCPConn)
}

// Review Focus 3: SOCKS5 payload counted once, handshake not counted.
func TestCountsSocks5(t *testing.T) {
	m := &testMeter{}
	c := socksConnect(t, startMixedMeter(t, m), echoAfterEOF(t))
	c.Write(make([]byte, 1000))
	c.CloseWrite()
	if got, _ := io.ReadAll(c); len(got) != 1000 {
		t.Fatalf("echo %d", len(got))
	}
	if up, down := waitMeter(m, 1000, 1000); up != 1000 || down != 1000 {
		t.Fatalf("meter %d/%d, want 1000/1000", up, down)
	}
}

func TestCountsHalfClose(t *testing.T) {
	m := &testMeter{}
	c := socksConnect(t, startMixedMeter(t, m), echoAfterEOF(t))
	c.Write(make([]byte, 500))
	c.CloseWrite() // the echo only answers after this
	io.ReadAll(c)
	if up, down := waitMeter(m, 500, 500); up != 500 || down != 500 {
		t.Fatalf("meter %d/%d, want 500/500", up, down)
	}
}

func TestCountsHTTPConnect(t *testing.T) {
	m := &testMeter{}
	target := echoAfterEOF(t)
	c, err := net.Dial("tcp", startMixedMeter(t, m))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("connect %v %v", resp, err)
	}
	c.Write(make([]byte, 1000))
	c.(*net.TCPConn).CloseWrite()
	if got, _ := io.ReadAll(br); len(got) != 1000 {
		t.Fatalf("echo %d", len(got))
	}
	if up, down := waitMeter(m, 1000, 1000); up != 1000 || down != 1000 {
		t.Fatalf("meter %d/%d, want 1000/1000", up, down)
	}
}

func TestCountsHTTPGet(t *testing.T) {
	m := &testMeter{}
	body := strings.Repeat("b", 10240)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) }))
	defer srv.Close()
	pu, _ := url.Parse("http://" + startMixedMeter(t, m))
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}, Timeout: 5 * time.Second}
	resp, err := cl.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	cl.CloseIdleConnections()
	if len(got) != 10240 {
		t.Fatalf("body %d", len(got))
	}
	up, down := waitMeter(m, 1, 10240)
	if up == 0 || down < 10240 || down > 10240+1024 {
		t.Fatalf("meter %d/%d, want up > 0, down in [10240, 11264]", up, down)
	}
}

func TestNilMeter(t *testing.T) {
	c := socksConnect(t, startMixed(t), echoAfterEOF(t))
	c.Write([]byte("x"))
	c.CloseWrite()
	if got, _ := io.ReadAll(c); string(got) != "x" {
		t.Fatalf("got %q", got)
	}
}
