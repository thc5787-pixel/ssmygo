// Package core translates gui-config.json into a shadowsocks-go
// service.Config and runs it.
package core

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/database64128/shadowsocks-go/clientgroup"
	"github.com/database64128/shadowsocks-go/conn"
	"github.com/database64128/shadowsocks-go/domainset"
	"github.com/database64128/shadowsocks-go/jsoncfg"
	"github.com/database64128/shadowsocks-go/router"
	"github.com/database64128/shadowsocks-go/service"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core/mixed"
	"github.com/thc5787/ssmygo/internal/rules"

	"github.com/thc5787/ssmygo/internal/core/forward"
	_ "github.com/thc5787/ssmygo/internal/core/legacyaead" // registers legacy AEAD clients
)

// ErrNoServer means there is no valid current server: Build still returns a
// usable config (local port + direct only).
var ErrNoServer = errors.New("core: no server configured")

// ErrPluginNotRunning: the current server needs its plugin, which is not
// running; Build returns a usable config in which that server's traffic
// fails closed (see failClosedClient).
var ErrPluginNotRunning = errors.New("core: plugin not running")

const (
	serverName    = "local"
	udpServerName = "local-udp"
	udpMTU        = 1500
	proxyName     = "proxy"
	directName    = "direct"
	forwardName   = "forward"
)

// privatePrefixes always go direct in rules mode.
var privatePrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}

// Build translates cfg. rs nil means rules are unavailable: behave as global.
// pluginPort > 0 means the current server runs through its SIP003 plugin
// listening on 127.0.0.1:pluginPort; the forward proxy then does not apply
// (A1-D3). Otherwise an enabled forward proxy carries the server connection.
//
// UDP (A2) is on unless the forward proxy is: mixed+udp answers UDP
// ASSOCIATE with the TCP local address, so a UDP-only socks5 server listens
// on the same address, and every client carries UDP too.
func Build(cfg *config.Config, rs *rules.Result, pluginPort int) (*service.Config, error) {
	host := "127.0.0.1"
	if cfg.ShareOverLan {
		host = "0.0.0.0"
	}
	addr := host + ":" + strconv.Itoa(cfg.LocalPort)
	udp := !cfg.Proxy.UseProxy
	sc := &service.Config{
		Servers: []service.ServerConfig{{
			Name:         serverName,
			Protocol:     mixed.Protocol,
			TCPListeners: []service.RelayTCPListenerConfig{{Network: "tcp", Address: addr}},
		}},
		Clients: []service.ClientConfig{withUDP(service.ClientConfig{Name: directName, Protocol: "direct", EnableTCP: true}, udp)},
		Router:  router.Config{DefaultTCPClientName: directName},
	}
	if udp {
		sc.Servers[0].Protocol = mixed.ProtocolUDP
		sc.Servers = append(sc.Servers, service.ServerConfig{
			Name:         udpServerName,
			Protocol:     "socks5",
			MTU:          udpMTU,
			UDPListeners: []service.RelayUDPListenerConfig{{Network: "udp", Address: addr}},
		})
	}
	defer func() {
		if udp {
			sc.Router.DefaultUDPClientName = sc.Router.DefaultTCPClientName
		}
	}()

	if cfg.Strategy.Known() {
		if members, _ := StrategyMembers(cfg); len(members) > 0 {
			if err := strategyClients(sc, cfg, members, udp); err != nil {
				return sc, err
			}
			return sc, route(sc, cfg, rs, false)
		}
	}

	srv, ok := cfg.Current()
	if !ok || len(srv.Validate()) > 0 {
		return sc, ErrNoServer
	}
	// A plugin server whose plugin is not running fails closed: its traffic
	// must neither bypass the plugin nor silently go direct.
	pluginDown := srv.Plugin != "" && pluginPort == 0
	var proxy service.ClientConfig
	var err error
	switch {
	case pluginDown:
		proxy = failClosedClient()
	default:
		if proxy, err = proxyClient(srv, pluginPort, udp); err != nil {
			return sc, fmt.Errorf("%w: %v", ErrNoServer, err)
		}
	}
	proxy = withUDP(proxy, udp)
	if !pluginDown && pluginPort == 0 && cfg.Proxy.UseProxy {
		fc, err := forwardClient(cfg.Proxy)
		if err != nil {
			return sc, err
		}
		sc.Clients = append(sc.Clients, fc) // must precede the client dialing through it
		proxy.DialerClient = forwardName
	}
	sc.Clients = append(sc.Clients, proxy)
	return sc, route(sc, cfg, rs, pluginDown)
}

// route sets where traffic goes: everything to the proxy (global mode, or no
// rules), or the rules' routes.
func route(sc *service.Config, cfg *config.Config, rs *rules.Result, pluginDown bool) error {
	udp := !cfg.Proxy.UseProxy
	if cfg.Global || rs == nil || !cfg.GeositePreferDirect {
		sc.Router.DefaultTCPClientName = proxyName
	}
	if cfg.Global || rs == nil {
		return notRunning(pluginDown)
	}
	sc.DomainSets = []domainset.Config{
		{Name: "direct", Type: "gob", Path: rs.Direct},
		{Name: "proxied", Type: "gob", Path: rs.Proxied},
	}
	// With UDP the routes leave Network empty: they apply to both (A2-D4).
	network := "tcp"
	if udp {
		network = ""
	}
	sc.Router.Routes = []router.RouteConfig{
		// Only literal IPs: resolving every domain to test it would cost a DNS lookup per request.
		{Name: "private", Network: network, Client: directName, ToPrefixes: privatePrefixes, DisableNameResolutionForIPRules: true},
		{Name: "direct", Network: network, Client: directName, ToDomainSets: []string{"direct"}},
		{Name: "proxied", Network: network, Client: proxyName, ToDomainSets: []string{"proxied"}},
	}
	return notRunning(pluginDown)
}

// StrategyMembers returns the indexes of the servers a strategy uses: the
// valid ones without a plugin (E-D3: one plugin runs at a time), and how
// many servers it leaves out.
func StrategyMembers(cfg *config.Config) (members []int, excluded int) {
	for i, s := range cfg.Configs {
		if s.Plugin == "" && len(s.Validate()) == 0 {
			members = append(members, i)
		}
	}
	return members, len(cfg.Configs) - len(members)
}

// strategyClients adds a client per member, proxy-<index>, each through the
// forward proxy when it is on, and the group "proxy" joining them. The
// current server goes first: the probing policies start on the first client
// until their first probe.
func strategyClients(sc *service.Config, cfg *config.Config, members []int, udp bool) error {
	if i := slices.Index(members, cfg.Index); i > 0 {
		members = append([]int{cfg.Index}, slices.Delete(slices.Clone(members), i, i+1)...)
	}
	if cfg.Proxy.UseProxy {
		fc, err := forwardClient(cfg.Proxy)
		if err != nil {
			return err
		}
		sc.Clients = append(sc.Clients, fc)
	}
	names := make([]string, 0, len(members))
	for _, i := range members {
		c, err := proxyClient(cfg.Configs[i], 0, udp)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNoServer, err)
		}
		c.Name = proxyName + "-" + strconv.Itoa(i)
		if cfg.Proxy.UseProxy {
			c.DialerClient = forwardName
		}
		sc.Clients = append(sc.Clients, withUDP(c, udp))
		names = append(names, c.Name)
	}
	// Probes keep shadowsocks-go's defaults: an HTTP 204 from
	// clients3.google.com and a DNS query to one.one.one.one, every 30 s.
	g := clientgroup.Config{Name: proxyName}
	g.TCP.Policy, g.TCP.Clients = clientgroup.PolicyRandom, names
	if cfg.Strategy == config.StrategyHA {
		g.TCP.Policy = clientgroup.PolicyMinMaxLatency
	}
	if udp {
		g.UDP.Policy, g.UDP.Clients = clientgroup.PolicyRandom, names
		if cfg.Strategy == config.StrategyHA {
			g.UDP.Policy = clientgroup.PolicyAvailability
		}
	}
	sc.ClientGroups = []clientgroup.Config{g}
	return nil
}

// proxyClient is the client for server s. With a running plugin (pluginPort
// > 0) TCP goes to the plugin; UDP, if on, goes to the real server (A2-D1).
func proxyClient(s config.Server, pluginPort int, udp bool) (service.ClientConfig, error) {
	ep, err := conn.AddrFromHostPort(s.Server, uint16(s.ServerPort))
	if err != nil {
		return service.ClientConfig{}, err
	}
	c := service.ClientConfig{Name: proxyName, Protocol: s.Method, Endpoint: ep, EnableTCP: true}
	if pluginPort > 0 {
		c.Endpoint = conn.AddrFromIPAndPort(netip.MustParseAddr("127.0.0.1"), uint16(pluginPort))
		if udp {
			c.Endpoint, c.TCPAddress, c.UDPAddress = conn.Addr{}, c.Endpoint, ep
		}
	}
	switch {
	case s.Method == "plain":
		c.Protocol = "none"
	case config.PSKSize(s.Method) > 0:
		if c.PSK, err = base64.StdEncoding.DecodeString(s.Password); err != nil {
			return service.ClientConfig{}, err
		}
	case s.Method != "none":
		c.Password = s.Password
	}
	return c, nil
}

// forwardClient is the forward proxy hop (original "Forward Proxy").
func forwardClient(p config.ForwardProxy) (service.ClientConfig, error) {
	if errs := p.Validate(); len(errs) > 0 {
		return service.ClientConfig{}, fmt.Errorf("forward proxy: %w", errs[0])
	}
	ep, err := conn.AddrFromHostPort(strings.TrimSpace(p.ProxyServer), uint16(p.ProxyPort))
	if err != nil {
		return service.ClientConfig{}, fmt.Errorf("forward proxy: %w", err)
	}
	c := service.ClientConfig{
		Name:        forwardName,
		Protocol:    forward.ProtocolSOCKS5,
		Endpoint:    ep,
		EnableTCP:   true,
		DialTimeout: jsoncfg.Duration(time.Duration(p.ProxyTimeout) * time.Second),
	}
	if p.ProxyType == config.ProxyHTTP {
		c.Protocol = forward.ProtocolHTTP
		c.HTTP.UseBasicAuth = p.UseAuth
		if p.UseAuth {
			c.HTTP.Username, c.HTTP.Password = p.AuthUser, p.AuthPwd
		}
	} else if p.UseAuth {
		c.Socks5.EnableUserPassAuth = true
		c.Socks5.Username, c.Socks5.Password = p.AuthUser, p.AuthPwd
	}
	return c, nil
}

func notRunning(pluginDown bool) error {
	if pluginDown {
		return ErrPluginNotRunning
	}
	return nil
}

// failClosedClient stands in for a server that cannot be reached right now:
// a direct client whose denylist covers every address, so every connection
// meant for the server fails instead of going out unproxied.
func failClosedClient() service.ClientConfig {
	return service.ClientConfig{Name: proxyName, Protocol: "direct", EnableTCP: true,
		IPDenylistPrefixes: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}}
}

// withUDP turns on UDP for c when udp is set.
func withUDP(c service.ClientConfig, udp bool) service.ClientConfig {
	if udp {
		c.EnableUDP, c.MTU = true, udpMTU
	}
	return c
}
