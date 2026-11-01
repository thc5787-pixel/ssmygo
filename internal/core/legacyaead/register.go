package legacyaead

import (
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/service"
)

func init() {
	for _, m := range Methods {
		service.RegisterClientProtocol(m, newFromService)
	}
}

func newFromService(c *service.ClientConfig, inner *netio.TCPClient) (netio.StreamClient, error) {
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
