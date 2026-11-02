package geosite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/database64128/shadowsocks-go/tslog"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core"
	"github.com/thc5787/ssmygo/internal/subscribe"
	"github.com/thc5787/ssmygo/internal/traffic"
	"google.golang.org/protobuf/encoding/protowire"
)

// dlc encodes a GeoSiteList with one root-domain rule per group.
func dlc(groups ...string) []byte {
	var list []byte
	for _, g := range groups {
		var d []byte
		d = protowire.AppendTag(d, 1, protowire.VarintType)
		d = protowire.AppendVarint(d, 2)
		d = protowire.AppendTag(d, 2, protowire.BytesType)
		d = protowire.AppendString(d, g+".example")
		var s []byte
		s = protowire.AppendTag(s, 1, protowire.BytesType)
		s = protowire.AppendString(s, strings.ToUpper(g))
		s = protowire.AppendTag(s, 2, protowire.BytesType)
		s = protowire.AppendBytes(s, d)
		list = protowire.AppendTag(list, 1, protowire.BytesType)
		list = protowire.AppendBytes(list, s)
	}
	return list
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// server serves body at /dlc.dat and sumText at /dlc.dat.sha256sum.
func server(t *testing.T, body []byte, sumText string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/secret/dlc.dat", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	mux.HandleFunc("/secret/dlc.dat.sha256sum", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sumText))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

var groups = []string{"cn", "geolocation-!cn@cn", "private"}

func TestFetchOK(t *testing.T) {
	body := dlc("cn", "geolocation-!cn", "private")
	s := server(t, body, strings.ToUpper(sum(body))+"  dlc.dat\n")
	got, err := Fetch(context.Background(), s.URL+"/secret/dlc.dat", s.URL+"/secret/dlc.dat.sha256sum", s.Client(), groups)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("data differs")
	}
}

func TestFetchShaMismatch(t *testing.T) {
	body := dlc("cn", "geolocation-!cn", "private")
	s := server(t, body, sum([]byte("other"))+"  dlc.dat\n")
	_, err := Fetch(context.Background(), s.URL+"/secret/dlc.dat", s.URL+"/secret/dlc.dat.sha256sum", s.Client(), groups)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v, want ErrChecksum", err)
	}
}

func TestFetchCustomWithoutSumNotVerified(t *testing.T) {
	body := dlc("cn", "geolocation-!cn", "private")
	s := server(t, body, "garbage")
	_, err := Fetch(context.Background(), s.URL+"/secret/dlc.dat", "", s.Client(), groups)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsAlwaysVerify(t *testing.T) {
	u, su := Sources("", "")
	if u != DefaultURL || su != DefaultSumURL {
		t.Fatalf("defaults: %q %q", u, su)
	}
	if DefaultSumURL != DefaultURL+".sha256sum" {
		t.Fatalf("sum URL %q", DefaultSumURL)
	}
	// The original ignores geositeSha256sumUrl unless geositeUrl is set.
	if u, su := Sources("", "https://x/y"); u != DefaultURL || su != DefaultSumURL {
		t.Fatalf("got %q %q", u, su)
	}
	if u, su := Sources("https://a/dlc.dat", ""); u != "https://a/dlc.dat" || su != "" {
		t.Fatalf("custom: %q %q", u, su)
	}
}

func TestFetchTooLarge(t *testing.T) {
	big := make([]byte, MaxBytes+1)
	s := server(t, big, "")
	_, err := Fetch(context.Background(), s.URL+"/secret/dlc.dat", "", s.Client(), groups)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestFetchNotDLC(t *testing.T) {
	s := server(t, []byte("<!DOCTYPE html><html><body>rate limited</body></html>"), "")
	_, err := Fetch(context.Background(), s.URL+"/secret/dlc.dat", "", s.Client(), groups)
	if !errors.Is(err, ErrNotDLC) {
		t.Fatalf("err = %v, want ErrNotDLC", err)
	}
	// An empty body parses as an empty list: not a dlc either.
	s = server(t, nil, "")
	if _, err := Fetch(context.Background(), s.URL+"/secret/dlc.dat", "", s.Client(), nil); !errors.Is(err, ErrNotDLC) {
		t.Fatalf("empty: err = %v, want ErrNotDLC", err)
	}
}

func TestFetchMissingGroup(t *testing.T) {
	s := server(t, dlc("cn", "private"), "")
	_, err := Fetch(context.Background(), s.URL+"/secret/dlc.dat", "", s.Client(), groups)
	var mg *MissingGroupsError
	if !errors.As(err, &mg) {
		t.Fatalf("err = %v, want MissingGroupsError", err)
	}
	if len(mg.Groups) != 1 || mg.Groups[0] != "geolocation-!cn@cn" || !strings.Contains(err.Error(), "geolocation-!cn@cn") {
		t.Fatalf("groups %v, text %q", mg.Groups, err)
	}
}

func TestFetchHTTPErrorHostOnly(t *testing.T) {
	s := httptest.NewServer(http.NotFoundHandler())
	defer s.Close()
	_, err := Fetch(context.Background(), s.URL+"/secret/dlc.dat?token=abc", "", s.Client(), groups)
	if err == nil || !errors.Is(err, ErrFetch) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "token") || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error text %q", err)
	}
	// Connection failure: the url.Error's URL must not leak either.
	_, err = Fetch(context.Background(), "http://127.0.0.1:1/secret/dlc.dat", "", s.Client(), groups)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error text %q", err)
	}
	// Sum URL failure too.
	body := dlc("cn")
	s2 := server(t, body, "")
	_, err = Fetch(context.Background(), s2.URL+"/secret/dlc.dat", s2.URL+"/secret/missing.sha256sum", s2.Client(), []string{"cn"})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("sum error text %q", err)
	}
}

// D-D2: through ssmygo's local port, with subscribe's CONNECT client.
func TestFetchThroughLocalPort(t *testing.T) {
	body := dlc("cn", "geolocation-!cn", "private")
	s := server(t, body, sum(body))
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
	if _, err := Fetch(context.Background(), s.URL+"/secret/dlc.dat", s.URL+"/secret/dlc.dat.sha256sum", subscribe.Client(port, Timeout), groups); err != nil {
		t.Fatal(err)
	}
	if _, down := m.Totals(); down < uint64(len(body)) {
		t.Fatalf("request did not go through the local port (down %d)", down)
	}
}
