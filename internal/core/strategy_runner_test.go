package core

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/conn"
	"github.com/database64128/shadowsocks-go/jsoncfg"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core/legacyaead/aeadtest"
)

// countedServer is an aeadtest server behind a relay counting connections.
func countedServer(t *testing.T, n *atomic.Int32) config.Server {
	t.Helper()
	up := aeadtest.Start(t, "aes-256-gcm", "pw")
	host, port, _ := net.SplitHostPort(countingRelay(t, up.String(), n))
	p, _ := strconv.Atoi(port)
	return config.Server{Server: host, ServerPort: p, Method: "aes-256-gcm", Password: "pw"}
}

func TestRunnerBalancingUsesEveryServer(t *testing.T) {
	var a, b atomic.Int32
	cfg := config.Default()
	cfg.LocalPort = freePort(t)
	cfg.Global = true
	cfg.Strategy = config.StrategyBalancing
	cfg.Configs = []config.Server{countedServer(t, &a), countedServer(t, &b)}
	sc, err := Build(cfg, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	if err := r.Apply(sc); err != nil {
		t.Fatal(err)
	}
	target := echoServer(t)
	for i := range 20 {
		if err := socksEcho(cfg.LocalPort, target, "lb "+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	if a.Load() == 0 || b.Load() == 0 || a.Load()+b.Load() != 20 {
		t.Fatalf("connections: %d and %d", a.Load(), b.Load())
	}
}

// generate204 answers the high availability probe.
func generate204(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return netip.MustParseAddrPort(ln.Addr().String())
}

func TestRunnerHAPicksTheWorkingServer(t *testing.T) {
	var n atomic.Int32
	dead := freePort(t)
	cfg := config.Default()
	cfg.LocalPort = freePort(t)
	cfg.Global = true
	cfg.Strategy = config.StrategyHA
	cfg.Configs = []config.Server{
		{Server: "127.0.0.1", ServerPort: dead, Method: "aes-256-gcm", Password: "pw"}, // current, down
		countedServer(t, &n),
	}
	sc, err := Build(cfg, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The defaults probe clients3.google.com every 30 s; the test probes a
	// local endpoint quickly.
	g := &sc.ClientGroups[0]
	g.TCP.Probe.Address = conn.AddrFromIPPort(generate204(t))
	g.TCP.Probe.Interval = jsoncfg.Duration(100 * time.Millisecond)
	g.TCP.Probe.Timeout = jsoncfg.Duration(time.Second)
	g.UDP.Probe.Address = conn.AddrFromIPPort(netip.MustParseAddrPort("127.0.0.1:9"))
	g.UDP.Probe.Interval = jsoncfg.Duration(100 * time.Millisecond)
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	if err := r.Apply(sc); err != nil {
		t.Fatal(err)
	}
	target := echoServer(t)
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := socksEcho(cfg.LocalPort, target, "ha")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never switched to the working server: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if n.Load() == 0 {
		t.Fatal("the working server saw no connection")
	}
}

// Final review #1: every Apply restarts probing; the first probe must not
// wait a whole interval, or HA sticks to a dead server after each change.
func TestRunnerHAProbesAtOnce(t *testing.T) {
	var n atomic.Int32
	dead := freePort(t)
	cfg := config.Default()
	cfg.LocalPort = freePort(t)
	cfg.Global = true
	cfg.Strategy = config.StrategyHA
	cfg.Configs = []config.Server{
		{Server: "127.0.0.1", ServerPort: dead, Method: "aes-256-gcm", Password: "pw"}, // current, down
		countedServer(t, &n),
	}
	sc, err := Build(cfg, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	g := &sc.ClientGroups[0]
	g.TCP.Probe.Address = conn.AddrFromIPPort(generate204(t))
	g.TCP.Probe.Interval = jsoncfg.Duration(time.Hour) // only the first probe can help
	g.TCP.Probe.Timeout = jsoncfg.Duration(time.Second)
	g.UDP.Probe.Address = conn.AddrFromIPPort(netip.MustParseAddrPort("127.0.0.1:9"))
	g.UDP.Probe.Interval = jsoncfg.Duration(time.Hour)
	r := NewRunner(quietLogger(), nil)
	defer r.Stop()
	if err := r.Apply(sc); err != nil {
		t.Fatal(err)
	}
	target := echoServer(t)
	deadline := time.Now().Add(5 * time.Second)
	for socksEcho(cfg.LocalPort, target, "ha-now") != nil {
		if time.Now().After(deadline) {
			t.Fatal("still on the dead server: the first probe waited for the interval")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
