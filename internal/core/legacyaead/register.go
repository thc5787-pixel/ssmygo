package legacyaead

import (
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/zerocopy"
)

func init() {
	for _, m := range Methods {
		service.RegisterClientProtocol(m, newFromService)
		service.RegisterUDPClientProtocol(m, newUDPFromService)
	}
}

func newFromService(c *service.ClientConfig, inner netio.StreamClient) (netio.StreamClient, error) {
	size, err := KeySize(c.Protocol)
	if err != nil {
		return nil, err
	}
	addr := c.Endpoint
	if !addr.IsValid() {
		addr = c.TCPAddress
	}
	cfg := StreamClientConfig{
		Name:        c.Name,
		Method:      c.Protocol,
		Key:         DeriveKey(c.Password, size),
		InnerClient: inner,
		Addr:        addr,
	}
	return cfg.NewStreamClient()
}

func newUDPFromService(c *service.ClientConfig, env service.UDPEnv) (zerocopy.UDPClient, error) {
	size, err := KeySize(c.Protocol)
	if err != nil {
		return nil, err
	}
	return NewUDPClient(c.Name, c.Protocol, DeriveKey(c.Password, size), env)
}
