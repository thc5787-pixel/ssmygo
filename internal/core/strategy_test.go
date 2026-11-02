package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/thc5787/ssmygo/internal/config"
)

// strategyConfig has three servers: two plain ones and a plugin server.
func strategyConfig(id config.StrategyID) *config.Config {
	c := sampleConfig()
	c.Configs = append(c.Configs,
		config.Server{Server: "ss2.example.com", ServerPort: 8389, Method: "aes-256-gcm", Password: "pw2"},
		config.Server{Server: "ss3.example.com", ServerPort: 443, Method: "aes-128-gcm", Password: "pw3", Plugin: "v2ray-plugin"},
	)
	c.Strategy = id
	return c
}

func TestStrategyMembers(t *testing.T) {
	c := strategyConfig(config.StrategyBalancing)
	c.Configs = append(c.Configs, config.Server{Server: "", ServerPort: 1}) // invalid
	m, ex := StrategyMembers(c)
	if !slices.Equal(m, []int{0, 1}) || ex != 2 {
		t.Fatalf("members %v excluded %d", m, ex)
	}
}

func TestBuildStrategyBalancing(t *testing.T) {
	sc, err := Build(strategyConfig(config.StrategyBalancing), sampleRules, 41234)
	if err != nil {
		t.Fatal(err)
	}
	if clientByName(sc, "proxy").Name != "" {
		t.Fatal("a client named proxy collides with the group")
	}
	for _, n := range []string{"proxy-0", "proxy-1"} {
		if c := clientByName(sc, n); c.Name == "" || !c.EnableUDP {
			t.Fatalf("member %s: %+v", n, c)
		}
	}
	if clientByName(sc, "proxy-2").Name != "" {
		t.Fatal("plugin server must be excluded")
	}
	if len(sc.ClientGroups) != 1 {
		t.Fatalf("groups %+v", sc.ClientGroups)
	}
	g := sc.ClientGroups[0]
	if g.Name != "proxy" || g.TCP.Policy != "random" || g.UDP.Policy != "random" ||
		!slices.Equal(g.TCP.Clients, []string{"proxy-0", "proxy-1"}) || !slices.Equal(g.UDP.Clients, g.TCP.Clients) {
		t.Fatalf("group %+v", g)
	}
	if sc.Router.DefaultTCPClientName != "proxy" || sc.Router.DefaultUDPClientName != "proxy" {
		t.Fatalf("router %+v", sc.Router)
	}
}

// The current server comes first: high availability starts on it until the
// first probe.
func TestBuildStrategyHACurrentFirst(t *testing.T) {
	c := strategyConfig(config.StrategyHA)
	c.Index = 1
	sc, err := Build(c, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	g := sc.ClientGroups[0]
	if g.TCP.Policy != "min-max-latency" || g.UDP.Policy != "availability" || !slices.Equal(g.TCP.Clients, []string{"proxy-1", "proxy-0"}) {
		t.Fatalf("group %+v", g)
	}
}

// With the forward proxy every member dials through it and UDP is off.
func TestBuildStrategyForward(t *testing.T) {
	c := strategyConfig(config.StrategyHA)
	c.Proxy = fwd(config.ProxySOCKS5)
	sc, err := Build(c, sampleRules, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"proxy-0", "proxy-1"} {
		if cl := clientByName(sc, n); cl.DialerClient != "forward" || cl.EnableUDP {
			t.Fatalf("member %s: %+v", n, cl)
		}
	}
	if g := sc.ClientGroups[0]; len(g.UDP.Clients) != 0 || len(g.TCP.Clients) != 2 {
		t.Fatalf("group %+v", g)
	}
}

// No member (only plugin servers): the single-server config of index.
func TestBuildStrategyNoMembersFallsBack(t *testing.T) {
	c := sampleConfig()
	c.Configs[0].Plugin = "v2ray-plugin"
	c.Strategy = config.StrategyBalancing
	sc, err := Build(c, nil, 41234)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.ClientGroups) != 0 || clientByName(sc, "proxy").Endpoint.String() != "127.0.0.1:41234" &&
		clientByName(sc, "proxy").TCPAddress.String() != "127.0.0.1:41234" {
		t.Fatalf("config %+v", sc)
	}
}

func TestBuildUnknownStrategyIsNone(t *testing.T) {
	sc, err := Build(strategyConfig("com.shadowsocks.strategy.scbs"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.ClientGroups) != 0 || clientByName(sc, "proxy").Name == "" {
		t.Fatalf("config %+v", sc)
	}
}

// Final review: a member whose address cannot be dialed (passes Validate,
// fails as a client address) is left out instead of sending all traffic
// direct.
func TestStrategyMemberWithBadAddressExcluded(t *testing.T) {
	cfg := strategyConfig(config.StrategyBalancing)
	bad := cfg.Configs[0]
	bad.Server = strings.Repeat("a", 300) + ".example"
	cfg.Configs = append(cfg.Configs, bad)
	members, excluded := StrategyMembers(cfg)
	if slices.Contains(members, len(cfg.Configs)-1) || excluded < 1 {
		t.Fatalf("members %v excluded %d", members, excluded)
	}
	sc, err := Build(cfg, nil, 0)
	if err != nil || len(sc.ClientGroups) != 1 {
		t.Fatalf("err %v groups %d", err, len(sc.ClientGroups))
	}
}
