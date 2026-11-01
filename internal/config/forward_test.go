package config

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyLoaded(t *testing.T) {
	c, err := Load(filepath.Join("testdata", "original-4.4.1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Proxy.ProxyTimeout != 3 || c.Proxy.UseProxy || c.Proxy.ProxyType != ProxySOCKS5 {
		t.Fatalf("proxy %+v", c.Proxy)
	}
}

func TestProxyDefault(t *testing.T) {
	p := Default().Proxy
	if p.ProxyTimeout != DefaultProxyTimeout || p.ProxyType != ProxySOCKS5 || p.UseProxy {
		t.Fatalf("default %+v", p)
	}
}

func TestProxyKeepsUnknownKeys(t *testing.T) {
	var p ForwardProxy
	if err := p.UnmarshalJSON([]byte(`{"useProxy":true,"proxyPort":8080,"futureKey":7}`)); err != nil {
		t.Fatal(err)
	}
	b, _ := p.MarshalJSON()
	if !strings.Contains(string(b), `"futureKey":7`) || !p.UseProxy || p.ProxyPort != 8080 {
		t.Fatalf("%s", b)
	}
}

func TestProxyValidate(t *testing.T) {
	ok := ForwardProxy{UseProxy: true, ProxyType: ProxySOCKS5, ProxyServer: "127.0.0.1", ProxyPort: 1080, ProxyTimeout: 3}
	long := strings.Repeat("u", 256)
	cases := []struct {
		name  string
		mod   func(*ForwardProxy)
		field string
	}{
		{"off ignores everything", func(p *ForwardProxy) { *p = ForwardProxy{} }, ""},
		{"valid", func(p *ForwardProxy) {}, ""},
		{"empty server", func(p *ForwardProxy) { p.ProxyServer = " " }, "proxyServer"},
		{"port 0", func(p *ForwardProxy) { p.ProxyPort = 0 }, "proxyPort"},
		{"port 65536", func(p *ForwardProxy) { p.ProxyPort = 65536 }, "proxyPort"},
		{"timeout 0", func(p *ForwardProxy) { p.ProxyTimeout = 0 }, "proxyTimeout"},
		{"timeout 11", func(p *ForwardProxy) { p.ProxyTimeout = 11 }, "proxyTimeout"},
		{"timeout 10", func(p *ForwardProxy) { p.ProxyTimeout = 10 }, ""},
		{"bad type", func(p *ForwardProxy) { p.ProxyType = 2 }, "proxyType"},
		{"auth no user", func(p *ForwardProxy) { p.UseAuth = true }, "authUser"},
		{"auth special chars socks5", func(p *ForwardProxy) { p.UseAuth, p.AuthUser, p.AuthPwd = true, "a:b@c d", "密码 p@ss" }, ""},
		{"auth special chars http", func(p *ForwardProxy) {
			p.ProxyType, p.UseAuth, p.AuthUser, p.AuthPwd = ProxyHTTP, true, "a:b@c d", "密码 p@ss"
		}, ""},
		{"socks5 user 256 bytes", func(p *ForwardProxy) { p.UseAuth, p.AuthUser = true, long }, "authUser"},
		{"socks5 pwd 256 bytes", func(p *ForwardProxy) { p.UseAuth, p.AuthUser, p.AuthPwd = true, "u", long }, "authPwd"},
		{"http user 256 bytes", func(p *ForwardProxy) { p.ProxyType, p.UseAuth, p.AuthUser = ProxyHTTP, true, long }, ""},
	}
	for _, tc := range cases {
		p := ok
		tc.mod(&p)
		errs := p.Validate()
		if tc.field == "" && len(errs) != 0 {
			t.Errorf("%s: unexpected %v", tc.name, errs)
		}
		if tc.field != "" && !hasField(errs, tc.field) {
			t.Errorf("%s: want %s, got %v", tc.name, tc.field, errs)
		}
	}
}

func TestProxyLogRedacts(t *testing.T) {
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "proxy", ForwardProxy{UseAuth: true, AuthUser: "u", AuthPwd: "hunter2"})
	if strings.Contains(buf.String(), "hunter2") {
		t.Fatal(buf.String())
	}
}

func TestTrusted(t *testing.T) {
	c := Default()
	c.TrustedPlugins = []TrustedPlugin{{Path: `C:\p\a.exe`, SHA256: "ab"}}
	if !c.Trusted(`C:\p\a.exe`, "ab") || c.Trusted(`C:\p\a.exe`, "cd") || c.Trusted(`C:\p\b.exe`, "ab") {
		t.Fatal("Trusted")
	}
	if !c.Trusted(`c:\P\A.EXE`, "AB") {
		t.Fatal("Windows paths and hex are case-insensitive")
	}
	d := c.Clone()
	d.TrustedPlugins[0].SHA256 = "zz"
	if c.TrustedPlugins[0].SHA256 != "ab" {
		t.Fatal("Clone shares TrustedPlugins")
	}
}
