// Package mixed serves SOCKS5 and HTTP proxy requests on one port, like the
// original client's single "Proxy Port". It sniffs the first byte: 0x05 is
// SOCKS5, anything else is HTTP.
package mixed

import (
	"errors"
	"time"

	"github.com/database64128/shadowsocks-go/httpproxy"
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/socks5"
	"github.com/database64128/shadowsocks-go/tslog"
)

// Protocol is the server protocol name registered with shadowsocks-go.
// Its SOCKS5 refuses UDP ASSOCIATE (reply 7), used with a forward proxy.
const Protocol = "mixed"

// ProtocolUDP also answers UDP ASSOCIATE. The reply names the TCP local
// address, so a UDP socks5 server must listen on the same address.
const ProtocolUDP = "mixed+udp"

// DefaultFirstByteTimeout bounds how long a client may stay silent.
var DefaultFirstByteTimeout = 10 * time.Second

// Server implements netio.StreamServer.
type Server struct {
	socks, http      netio.StreamServer
	FirstByteTimeout time.Duration
}

// New returns a mixed server with SOCKS5 (UDP ASSOCIATE only if enableUDP)
// and a plain HTTP proxy.
func New(enableUDP bool) (*Server, error) {
	sc := socks5.StreamServerConfig{EnableTCP: true, EnableUDP: enableUDP}
	s, err := sc.NewStreamServer()
	if err != nil {
		return nil, err
	}
	var hc httpproxy.ServerConfig
	h, err := hc.NewProxyServer()
	if err != nil {
		return nil, err
	}
	return &Server{socks: s, http: h, FirstByteTimeout: DefaultFirstByteTimeout}, nil
}

// StreamServerInfo implements netio.StreamServer. Initial payload is never
// native: both sub-protocols need a handshake before data.
func (s *Server) StreamServerInfo() netio.StreamServerInfo {
	return netio.StreamServerInfo{NativeInitialPayload: false}
}

var errNoData = errors.New("mixed: no request before timeout")

// HandleStream implements netio.StreamServer.
func (s *Server) HandleStream(c netio.Conn, l *tslog.Logger) (netio.ConnRequest, error) {
	if err := c.SetReadDeadline(time.Now().Add(s.FirstByteTimeout)); err != nil {
		return netio.ConnRequest{}, err
	}
	var b [1]byte
	if _, err := c.Read(b[:]); err != nil {
		return netio.ConnRequest{}, errors.Join(errNoData, err)
	}
	if err := c.SetReadDeadline(time.Time{}); err != nil {
		return netio.ConnRequest{}, err
	}
	pc := &prefixConn{Conn: c, prefix: b[0], has: true}
	if b[0] == 5 {
		return s.socks.HandleStream(pc, l)
	}
	return s.http.HandleStream(pc, l)
}

// prefixConn returns one already-read byte before reading from Conn.
type prefixConn struct {
	netio.Conn
	prefix byte
	has    bool
}

func (p *prefixConn) Read(b []byte) (int, error) {
	if p.has && len(b) > 0 {
		b[0] = p.prefix
		p.has = false
		return 1, nil
	}
	return p.Conn.Read(b)
}
