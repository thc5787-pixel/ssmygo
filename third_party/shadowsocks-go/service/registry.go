package service

// ssmygo patch A: protocol registry (see ../../PATCHES.md).
// Lets code outside this module add client and server protocols to the
// built-in switch statements without editing them per protocol.

import (
	"fmt"
	"sync"

	"github.com/database64128/shadowsocks-go/netio"
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
