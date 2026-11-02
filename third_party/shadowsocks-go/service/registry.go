package service

// ssmygo patch A: protocol registry (see ../../PATCHES.md).
// Lets code outside this module add client and server protocols to the
// built-in switch statements without editing them per protocol.

import (
	"fmt"
	"sync"

	"github.com/database64128/shadowsocks-go/conn"
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/zerocopy"
)

// ClientFactory creates a stream client for a registered client protocol.
// inner is the stream client to dial through (direct TCP, or the client
// named by ClientConfig.DialerClient — ssmygo patch C).
type ClientFactory func(c *ClientConfig, inner netio.StreamClient) (netio.StreamClient, error)

// ServerFactory creates a stream server for a registered server protocol.
type ServerFactory func(sc *ServerConfig) (netio.StreamServer, error)

var (
	registryMu      sync.RWMutex
	clientFactories = map[string]ClientFactory{}
	serverFactories = map[string]ServerFactory{}
)

// RegisterClientProtocol registers a TCP-only client protocol. It panics if
// name is already registered.
func RegisterClientProtocol(name string, f ClientFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := clientFactories[name]; dup {
		panic(fmt.Sprintf("service: client protocol %q registered twice", name))
	}
	clientFactories[name] = f
}

// RegisterServerProtocol registers a TCP-only server protocol. It panics if
// name is already registered.
func RegisterServerProtocol(name string, f ServerFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := serverFactories[name]; dup {
		panic(fmt.Sprintf("service: server protocol %q registered twice", name))
	}
	serverFactories[name] = f
}

func lookupClientFactory(name string) (ClientFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := clientFactories[name]
	return f, ok
}

func lookupServerFactory(name string) (ServerFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := serverFactories[name]
	return f, ok
}

// ssmygo patch A2: UDP clients for registered protocols.

// UDPEnv is what a registered UDP client gets from AddClient.
type UDPEnv struct {
	Addr         conn.Addr // the client's UDP server address
	Preference   netio.AddressFamilyPreference
	Resolver     conn.Resolver // nil = the system resolver, as for built-in clients
	MTU          int
	SocketConfig conn.UDPSocketConfig
}

// UDPClientFactory creates the UDP client of a registered protocol.
type UDPClientFactory func(c *ClientConfig, env UDPEnv) (zerocopy.UDPClient, error)

var udpClientFactories = map[string]UDPClientFactory{}

// RegisterUDPClientProtocol adds UDP support to a registered client
// protocol. It panics if name already has a UDP factory.
func RegisterUDPClientProtocol(name string, f UDPClientFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := udpClientFactories[name]; dup {
		panic(fmt.Sprintf("service: UDP client protocol %q registered twice", name))
	}
	udpClientFactories[name] = f
}

func lookupUDPClientFactory(name string) (UDPClientFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := udpClientFactories[name]
	return f, ok
}
