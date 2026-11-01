// Package forward registers the forward-proxy client protocols that carry
// ssmygo's connections to the SS server through a SOCKS5 or HTTP proxy
// (original "Forward Proxy"), with a connection timeout.
package forward

import (
	"context"
	"time"

	"github.com/database64128/shadowsocks-go/conn"
	"github.com/database64128/shadowsocks-go/httpproxy"
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/socks5"
)

// Client protocol names registered with shadowsocks-go.
const (
	ProtocolSOCKS5 = "fwd-socks5"
	ProtocolHTTP   = "fwd-http"
)

func init() {
	service.RegisterClientProtocol(ProtocolSOCKS5, newSOCKS5)
	service.RegisterClientProtocol(ProtocolHTTP, newHTTP)
}

func endpoint(c *service.ClientConfig) conn.Addr {
	if c.Endpoint.IsValid() {
		return c.Endpoint
	}
	return c.TCPAddress
}

func newSOCKS5(c *service.ClientConfig, inner netio.StreamClient) (netio.StreamClient, error) {
	var authMsg []byte
	if c.Socks5.EnableUserPassAuth {
		u := socks5.UserInfo{Username: c.Socks5.Username, Password: c.Socks5.Password}
		if err := u.Validate(); err != nil {
			return nil, err
		}
		authMsg = u.AppendAuthMsg(nil)
	}
	return build(inner, c.DialTimeout.Value(), func(inner netio.StreamClient) (netio.StreamClient, error) {
		cfg := socks5.StreamClientConfig{Name: c.Name, InnerClient: inner, Addr: endpoint(c), AuthMsg: authMsg}
		return cfg.NewStreamClient(), nil
	})
}

func newHTTP(c *service.ClientConfig, inner netio.StreamClient) (netio.StreamClient, error) {
	return build(inner, c.DialTimeout.Value(), func(inner netio.StreamClient) (netio.StreamClient, error) {
		cfg := httpproxy.ClientConfig{
			Name:         c.Name,
			InnerClient:  inner,
			Addr:         endpoint(c),
			Username:     c.HTTP.Username,
			Password:     c.HTTP.Password,
			UseBasicAuth: c.HTTP.UseBasicAuth,
		}
		return cfg.NewProxyClient()
	})
}

// The proxy handshake reads its reply without a deadline, so a context
// alone cannot stop a proxy that accepts and then stays silent. The raw
// connection therefore gets a deadline when it is dialed (deadlineDialer),
// and the outer client clears it once the handshake has succeeded.

// deadlineDialer dials the forward proxy and arms a deadline on the conn.
type deadlineDialer struct {
	inner netio.StreamClient
	d     time.Duration
}

func (dd *deadlineDialer) NewStreamDialer() (netio.StreamDialer, netio.StreamDialerInfo) {
	_, info := dd.inner.NewStreamDialer()
	return dd, info
}

func (dd *deadlineDialer) DialStream(ctx context.Context, addr conn.Addr, payload []byte) (netio.Conn, error) {
	c, err := dd.inner.DialStream(ctx, addr, payload)
	if err != nil {
		return nil, err
	}
	if err := c.SetDeadline(time.Now().Add(dd.d)); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// timeoutClient bounds each DialStream (connect + proxy handshake).
type timeoutClient struct {
	inner netio.StreamClient
	d     time.Duration
}

func (t *timeoutClient) NewStreamDialer() (netio.StreamDialer, netio.StreamDialerInfo) {
	_, info := t.inner.NewStreamDialer()
	return t, info
}

func (t *timeoutClient) DialStream(ctx context.Context, addr conn.Addr, payload []byte) (netio.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, t.d)
	defer cancel()
	c, err := t.inner.DialStream(ctx, addr, payload)
	if err != nil {
		return nil, err
	}
	if err := c.SetDeadline(time.Time{}); err != nil { // handshake done: no deadline for the data
		c.Close()
		return nil, err
	}
	return c, nil
}

// build wraps the proxy client so connect + handshake take at most d.
func build(inner netio.StreamClient, d time.Duration, mk func(inner netio.StreamClient) (netio.StreamClient, error)) (netio.StreamClient, error) {
	if d <= 0 {
		return mk(inner)
	}
	pc, err := mk(&deadlineDialer{inner: inner, d: d})
	if err != nil {
		return nil, err
	}
	return &timeoutClient{inner: pc, d: d}, nil
}
