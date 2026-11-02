package mixed

import (
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/service"
)

func init() {
	service.RegisterServerProtocol(Protocol, func(sc *service.ServerConfig) (netio.StreamServer, error) {
		return New(false, sc.TrafficMeter)
	})
	service.RegisterServerProtocol(ProtocolUDP, func(sc *service.ServerConfig) (netio.StreamServer, error) {
		return New(true, sc.TrafficMeter)
	})
}
