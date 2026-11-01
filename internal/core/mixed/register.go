package mixed

import (
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/service"
)

func init() {
	service.RegisterServerProtocol(Protocol, func(*service.ServerConfig) (netio.StreamServer, error) {
		return New()
	})
}
