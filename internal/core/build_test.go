package core

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/database64128/shadowsocks-go/service"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/rules"
)

var update = flag.Bool("update", false, "rewrite golden files")

func sampleConfig() *config.Config {
	c := config.Default()
	c.Configs = []config.Server{{Server: "ss.example.com", ServerPort: 8388, Method: "chacha20-ietf-poly1305", Password: "pw", Timeout: 5}}
	c.Enabled = true
	return c
}

var sampleRules = &rules.Result{Direct: `C:\data\cache\domainsets\direct.gob`, Proxied: `C:\data\cache\domainsets\proxied.gob`}

func TestBuildGolden(t *testing.T) {
	cases := map[string]func() (*config.Config, *rules.Result, int){
		"global": func() (*config.Config, *rules.Result, int) {
			c := sampleConfig()
			c.Global = true
			return c, sampleRules, 0
		},
		"rules": func() (*config.Config, *rules.Result, int) { return sampleConfig(), sampleRules, 0 },
		"rules-preferdirect": func() (*config.Config, *rules.Result, int) {
			c := sampleConfig()
			c.GeositePreferDirect = true
			return c, sampleRules, 0
		},
		"lan": func() (*config.Config, *rules.Result, int) {
			c := sampleConfig()
			c.ShareOverLan, c.LocalPort = true, 2080
			return c, sampleRules, 0
		},
		"plugin": func() (*config.Config, *rules.Result, int) {
			c := sampleConfig()
			c.Configs[0].Plugin = "v2ray-plugin"
			return c, sampleRules, 41234
		},
		"forward-socks5": func() (*config.Config, *rules.Result, int) {
			c := sampleConfig()
			c.Proxy = fwd(config.ProxySOCKS5)
			return c, sampleRules, 0
		},
		"forward-http": func() (*config.Config, *rules.Result, int) {
			c := sampleConfig()
			c.Proxy = fwd(config.ProxyHTTP)
			return c, sampleRules, 0
		},
		"plugin-and-forward": func() (*config.Config, *rules.Result, int) {
			c := sampleConfig()
			c.Configs[0].Plugin = "v2ray-plugin"
			c.Proxy = fwd(config.ProxySOCKS5)
			return c, sampleRules, 41234
		},
		"plugin-down": func() (*config.Config, *rules.Result, int) {
			c := sampleConfig()
			c.Configs[0].Plugin = "v2ray-plugin"
			return c, sampleRules, 0
		},
		"strategy-balancing": func() (*config.Config, *rules.Result, int) {
			return strategyConfig(config.StrategyBalancing), sampleRules, 0
		},
		"strategy-ha": func() (*config.Config, *rules.Result, int) {
			c := strategyConfig(config.StrategyHA)
			c.Global = true
			return c, sampleRules, 0
		},
		"strategy-forward": func() (*config.Config, *rules.Result, int) {
			c := strategyConfig(config.StrategyBalancing)
			c.Proxy = fwd(config.ProxyHTTP)
			return c, sampleRules, 0
		},
		"ss2022": func() (*config.Config, *rules.Result, int) {
			c := sampleConfig()
			c.Configs[0].Method, c.Configs[0].Password = "2022-blake3-aes-128-gcm", "AAECAwQFBgcICQoLDA0ODw=="
			c.Global = true
			return c, nil, 0
		},
	}
	for name, mk := range cases {
		cfg, rs, port := mk()
		sc, err := Build(cfg, rs, port)
		if err != nil && !errors.Is(err, ErrPluginNotRunning) {
			t.Fatalf("%s: %v", name, err)
		}
		got, _ := json.MarshalIndent(sc, "", "  ")
		path := filepath.Join("testdata", "golden", name+".json")
		if *update {
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, append(got, '\n'), 0o644)
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(got)+"\n" {
			t.Errorf("%s: got\n%s", name, got)
		}
	}
}

func TestBuildNoServer(t *testing.T) {
	c := config.Default()
	sc, err := Build(c, sampleRules, 0)
	if !errors.Is(err, ErrNoServer) {
		t.Fatalf("err = %v", err)
	}
	if sc == nil || sc.Servers[0].Name != "local" || len(sc.Clients) != 1 || sc.Clients[0].Name != "direct" ||
		sc.Router.DefaultTCPClientName != "direct" {
		t.Fatalf("config %+v", sc)
	}
}

func TestBuildInvalidServerIsNoServer(t *testing.T) {
	c := sampleConfig()
	c.Configs[0].ServerPort = 0
	if _, err := Build(c, nil, 0); !errors.Is(err, ErrNoServer) {
		t.Fatalf("err = %v", err)
	}
}

func fwd(typ int) config.ForwardProxy {
	return config.ForwardProxy{UseProxy: true, ProxyType: typ, ProxyServer: "proxy.corp", ProxyPort: 3128,
		ProxyTimeout: 5, UseAuth: true, AuthUser: "u", AuthPwd: "p"}
}

// A plugin server talks to its plugin; the forward proxy cannot apply (A1-D3).
func TestBuildPluginIgnoresForward(t *testing.T) {
	c := sampleConfig()
	c.Configs[0].Plugin = "v2ray-plugin"
	c.Proxy = fwd(config.ProxySOCKS5)
	sc, err := Build(c, nil, 41234)
	if err != nil {
		t.Fatal(err)
	}
	for _, cl := range sc.Clients {
		if cl.Name == "forward" || cl.DialerClient != "" {
			t.Fatalf("forward used with a plugin: %+v", cl)
		}
		if cl.Name == "proxy" && cl.Endpoint.String() != "127.0.0.1:41234" {
			t.Fatalf("proxy endpoint %s", cl.Endpoint)
		}
	}
}

func TestBuildInvalidForwardIsError(t *testing.T) {
	c := sampleConfig()
	c.Proxy = config.ForwardProxy{UseProxy: true, ProxyType: config.ProxySOCKS5, ProxyTimeout: 3} // no server
	if _, err := Build(c, nil, 0); err == nil {
		t.Fatal("invalid forward proxy accepted")
	}
}

// A plugin server whose plugin is not running must not be dialed directly:
// that skips the plugin the user configured.
func TestBuildPluginNotRunningIsNoProxy(t *testing.T) {
	c := sampleConfig()
	c.Configs[0].Plugin = "v2ray-plugin"
	sc, err := Build(c, nil, 0)
	if !errors.Is(err, ErrPluginNotRunning) {
		t.Fatalf("err = %v", err)
	}
	for _, cl := range sc.Clients {
		if cl.Name == "proxy" && (cl.Protocol != "direct" || len(cl.IPDenylistPrefixes) != 2) {
			t.Fatalf("plugin server without its plugin must fail closed: %+v", cl)
		}
	}
	if sc.Router.DefaultTCPClientName != "proxy" {
		t.Fatal("proxied traffic must go to the fail-closed client, not direct")
	}
}

func clientByName(sc *service.Config, name string) service.ClientConfig {
	for _, c := range sc.Clients {
		if c.Name == name {
			return c
		}
	}
	return service.ClientConfig{}
}

// Spec §3.4: UDP on — mixed+udp on TCP, a UDP-only socks5 on the same
// address, UDP clients at MTU 1500, routes for both networks.
func TestBuildUDP(t *testing.T) {
	sc, err := Build(sampleConfig(), sampleRules, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Servers) != 2 || sc.Servers[0].Protocol != "mixed+udp" {
		t.Fatalf("servers %+v", sc.Servers)
	}
	u := sc.Servers[1]
	if u.Name != "local-udp" || u.Protocol != "socks5" || len(u.TCPListeners) != 0 || len(u.UDPListeners) != 1 ||
		u.UDPListeners[0].Address != sc.Servers[0].TCPListeners[0].Address || u.MTU != 1500 {
		t.Fatalf("udp server %+v", u)
	}
	for _, n := range []string{"direct", "proxy"} {
		if c := clientByName(sc, n); !c.EnableUDP || c.MTU != 1500 {
			t.Fatalf("%s client %+v", n, c)
		}
	}
	if sc.Router.DefaultUDPClientName != sc.Router.DefaultTCPClientName {
		t.Fatalf("udp default %q, tcp default %q", sc.Router.DefaultUDPClientName, sc.Router.DefaultTCPClientName)
	}
	for _, r := range sc.Router.Routes {
		if r.Network != "" {
			t.Fatalf("route %s network %q, want both", r.Name, r.Network)
		}
	}
}

// A2-D1: TCP goes to the plugin, UDP straight to the server.
func TestBuildUDPPlugin(t *testing.T) {
	c := sampleConfig()
	c.Configs[0].Plugin = "v2ray-plugin"
	sc, err := Build(c, nil, 41234)
	if err != nil {
		t.Fatal(err)
	}
	p := clientByName(sc, "proxy")
	if p.Endpoint.IsValid() || p.TCPAddress.String() != "127.0.0.1:41234" || p.UDPAddress.String() != "ss.example.com:8388" {
		t.Fatalf("proxy %+v", p)
	}
}

// A2-D5: the fail-closed client denies UDP too.
func TestBuildUDPPluginDown(t *testing.T) {
	c := sampleConfig()
	c.Configs[0].Plugin = "v2ray-plugin"
	sc, _ := Build(c, nil, 0)
	if p := clientByName(sc, "proxy"); !p.EnableUDP || p.MTU != 1500 || len(p.IPDenylistPrefixes) != 2 {
		t.Fatalf("proxy %+v", p)
	}
	if sc.Router.DefaultUDPClientName != "proxy" {
		t.Fatalf("udp default %q", sc.Router.DefaultUDPClientName)
	}
}

// A2-D2: forward proxy on → TCP-only config, as in A1.
func TestBuildUDPOffWithForward(t *testing.T) {
	c := sampleConfig()
	c.Proxy = fwd(config.ProxySOCKS5)
	sc, err := Build(c, sampleRules, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Servers) != 1 || sc.Servers[0].Protocol != "mixed" || sc.Router.DefaultUDPClientName != "" {
		t.Fatalf("config %+v", sc)
	}
	for _, cl := range sc.Clients {
		if cl.EnableUDP {
			t.Fatalf("client %s has UDP", cl.Name)
		}
	}
	for _, r := range sc.Router.Routes {
		if r.Network != "tcp" {
			t.Fatalf("route %s network %q", r.Name, r.Network)
		}
	}
}
