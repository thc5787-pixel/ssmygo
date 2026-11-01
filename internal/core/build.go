// Package core translates gui-config.json into a shadowsocks-go
// service.Config and runs it.
package core

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

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
	serverName  = "local"
	proxyName   = "proxy"
	directName  = "direct"
	forwardName = "forward"
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
func Build(cfg *config.Config, rs *rules.Result, pluginPort int) (*service.Config, error) {
	host := "127.0.0.1"
	if cfg.ShareOverLan {
		host = "0.0.0.0"
	}
	sc := &service.Config{
		Servers: []service.ServerConfig{{
			Name:     serverName,
			Protocol: mixed.Protocol,
			TCPListeners: []service.RelayTCPListenerConfig{{
				Network: "tcp",
				Address: host + ":" + strconv.Itoa(cfg.LocalPort),
			}},
		}},
		Clients: []service.ClientConfig{{Name: directName, Protocol: "direct", EnableTCP: true}},
		Router:  router.Config{DefaultTCPClientName: directName},
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
	case pluginPort > 0:
		srv.Server, srv.ServerPort = "127.0.0.1", pluginPort
		fallthrough
	default:
		if proxy, err = proxyClient(srv); err != nil {
			return sc, fmt.Errorf("%w: %v", ErrNoServer, err)
		}
	}
	if !pluginDown && pluginPort == 0 && cfg.Proxy.UseProxy {
		fc, err := forwardClient(cfg.Proxy)
		if err != nil {
			return sc, err
		}
		sc.Clients = append(sc.Clients, fc) // must precede the client dialing through it
		proxy.DialerClient = forwardName
	}
	sc.Clients = append(sc.Clients, proxy)

	if cfg.Global || rs == nil || !cfg.GeositePreferDirect {
		sc.Router.DefaultTCPClientName = proxyName
	}
	if cfg.Global || rs == nil {
		return sc, notRunning(pluginDown)
	}
	sc.DomainSets = []domainset.Config{
		{Name: "direct", Type: "gob", Path: rs.Direct},
		{Name: "proxied", Type: "gob", Path: rs.Proxied},
	}
	// TCP only in P0: a route without Network would also need UDP clients.
	sc.Router.Routes = []router.RouteConfig{
		// Only literal IPs: resolving every domain to test it would cost a DNS lookup per request.
		{Name: "private", Network: "tcp", Client: directName, ToPrefixes: privatePrefixes, DisableNameResolutionForIPRules: true},
		{Name: "direct", Network: "tcp", Client: directName, ToDomainSets: []string{"direct"}},
		{Name: "proxied", Network: "tcp", Client: proxyName, ToDomainSets: []string{"proxied"}},
	}
	return sc, notRunning(pluginDown)
}

func proxyClient(s config.Server) (service.ClientConfig, error) {
	ep, err := conn.AddrFromHostPort(s.Server, uint16(s.ServerPort))
	if err != nil {
		return service.ClientConfig{}, err
	}
	c := service.ClientConfig{Name: proxyName, Protocol: s.Method, Endpoint: ep, EnableTCP: true}
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
