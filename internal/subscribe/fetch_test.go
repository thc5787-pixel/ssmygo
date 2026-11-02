package subscribe

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/tslog"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core"
	"github.com/thc5787/ssmygo/internal/traffic"
)

const doc = `{"version":1,"servers":[{"server":"jp.example.com","server_port":8388,"password":"pw","method":"aes-256-gcm","remarks":"JP"}]}`

func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL
}

func TestFetchOK(t *testing.T) {
	u := serve(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, doc) })
	s, skipped, err := Fetch(context.Background(), u+"/sub", 0)
	if err != nil || skipped != 0 || len(s) != 1 || s[0].Group != u+"/sub" {
		t.Fatalf("%+v %d %v", s, skipped, err)
	}
}

func TestFetch404(t *testing.T) {
	u := serve(t, func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	if _, _, err := Fetch(context.Background(), u, 0); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("err %v", err)
	}
}

func TestFetchTimeout(t *testing.T) {
	u := serve(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := Fetch(ctx, u, 0); err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
}

// Review Focus 1: the 1 MB limit counts decompressed bytes.
func TestFetchTooLarge(t *testing.T) {
	big := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.Write(bytes.Repeat([]byte(" "), 2<<20)) })
	if _, _, err := Fetch(context.Background(), big, 0); err == nil || !strings.Contains(err.Error(), "1 MB") {
		t.Fatalf("plain: %v", err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(bytes.Repeat([]byte(" "), 10<<20))
	zw.Close()
	bomb := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(gz.Bytes())
	})
	if _, _, err := Fetch(context.Background(), bomb, 0); err == nil || !strings.Contains(err.Error(), "1 MB") {
		t.Fatalf("gzip: %v", err)
	}
}

func TestFetchNotJSON(t *testing.T) {
	u := serve(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "<html>please log in</html>") })
	if _, _, err := Fetch(context.Background(), u, 0); err == nil {
		t.Fatal("HTML accepted")
	}
}

// C-D1: with a proxy port the request goes through ssmygo's local port.
func TestFetchThroughProxy(t *testing.T) {
	u := serve(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, doc) })
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	var m traffic.Meter
	logger, _ := tslog.Config{Level: 8}.NewLogger(io.Discard)
	r := core.NewRunner(logger, &m)
	defer r.Stop()
	cfg := config.Default()
	cfg.LocalPort = port
	sc, _ := core.Build(cfg, nil, 0) // no server: direct through the local port
	if err := r.Apply(sc); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Fetch(context.Background(), u, port); err != nil {
		t.Fatal(err)
	}
	if _, down := m.Totals(); down == 0 {
		t.Fatal("request did not go through the local port")
	}
}

// Review Focus 4: a subscription token never shows in errors.
func TestFetchErrorHidesToken(t *testing.T) {
	_, _, err := Fetch(context.Background(), "http://127.0.0.1:1/sub?token=SECRET", 0)
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "/sub") {
		t.Fatalf("err %v", err)
	}
	if !errors.Is(err, ErrFetch) {
		t.Fatalf("not ErrFetch: %v", err)
	}
	if h := Host("https://user:pw@sub.example.com:8443/x?t=1"); h != "sub.example.com" {
		t.Fatalf("Host = %q", h)
	}
}

// Final review: an http:// subscription must reach the local port as a
// CONNECT tunnel, so the proxy (and its log) never sees the path or token.
func TestFetchHTTPThroughProxyUsesConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	first := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		first <- line
	}()
	Fetch(context.Background(), "http://sub.example.com/s?token=SECRET", ln.Addr().(*net.TCPAddr).Port)
	line := <-first
	if !strings.HasPrefix(line, "CONNECT sub.example.com:80 ") || strings.Contains(line, "SECRET") {
		t.Fatalf("proxy saw %q", line)
	}
}
