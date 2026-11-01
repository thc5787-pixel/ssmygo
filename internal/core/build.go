// Package core translates gui-config.json into a shadowsocks-go
// service.Config and runs it.
package core

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"strconv"

	"github.com/database64128/shadowsocks-go/conn"
	"github.com/database64128/shadowsocks-go/domainset"
	"github.com/database64128/shadowsocks-go/router"
	"github.com/database64128/shadowsocks-go/service"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/core/mixed"
	"github.com/thc5787/ssmygo/internal/rules"

	_ "github.com/thc5787/ssmygo/internal/core/legacyaead" // registers legacy AEAD clients
)

// ErrNoServer means there is no valid current server: Build still returns a
// usable config (local port + direct only).
var ErrNoServer = errors.New("core: no server configured")

const (
	serverName = "local"
	proxyName  = "proxy"
	directName = "direct"
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
func Build(cfg *config.Config, rs *rules.Result) (*service.Config, error) {
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
	proxy, err := proxyClient(srv)
	if err != nil {
		return sc, fmt.Errorf("%w: %v", ErrNoServer, err)
	}
	sc.Clients = append(sc.Clients, proxy)

	if cfg.Global || rs == nil || !cfg.GeositePreferDirect {
		sc.Router.DefaultTCPClientName = proxyName
	}
	if cfg.Global || rs == nil {
		return sc, nil
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
	return sc, nil
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
