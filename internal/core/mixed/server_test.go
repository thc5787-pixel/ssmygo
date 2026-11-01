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
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/tslog"
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
func startMixed(t *testing.T) string {
	t.Helper()
	addr := freeAddr(t)
	js := fmt.Sprintf(`{"servers":[{"name":"local","protocol":"mixed","tcpListeners":[{"network":"tcp","address":%q}]}],
	"clients":[{"name":"direct","protocol":"direct","enableTCP":true}]}`, addr)
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
