package mixed

import (
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/service"
)

// countPending counts a request's payload once the handshake is done:
// the initial payload as up, then every byte through the established conn.
// Handshake bytes are not counted (spec: payload bytes only).
type countPending struct {
	netio.PendingConn
	meter   service.TrafficMeter
	payload int
}

func (p *countPending) Proceed() (netio.Conn, error) {
	c, err := p.PendingConn.Proceed()
	if err != nil {
		return c, err
	}
	if p.payload > 0 {
		p.meter.Add(uint64(p.payload), 0)
	}
	return &countConn{Conn: c, meter: p.meter}, nil
}

// countConn counts reads (from the app: up) and writes (to the app: down).
// It exposes only netio.Conn, so every copy goes through Read and Write.
type countConn struct {
	netio.Conn
	meter service.TrafficMeter
}

func (c *countConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.meter.Add(uint64(n), 0)
	}
	return n, err
}

func (c *countConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		c.meter.Add(0, uint64(n))
	}
	return n, err
}
