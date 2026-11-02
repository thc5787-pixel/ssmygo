// Package subscribe fetches SIP008 online configs (subscriptions).
package subscribe

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/thc5787/ssmygo/internal/config"
)

const (
	timeout  = 15 * time.Second
	maxBytes = 1 << 20
)

// ErrFetch wraps every fetch failure. Its text names the host only: a
// subscription URL's path and query often hold a secret token.
var ErrFetch = errors.New("subscription update failed")

// Fetch downloads and parses the subscription at rawURL. With proxyPort
// > 0 the request goes through ssmygo's local port (127.0.0.1:proxyPort,
// as an HTTP proxy); otherwise it goes direct. The system proxy is never
// used.
func Fetch(ctx context.Context, rawURL string, proxyPort int) (servers []config.Server, skipped int, err error) {
	fail := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s: %s", ErrFetch, Host(rawURL), fmt.Sprintf(format, a...))
	}
	tr := &http.Transport{Proxy: nil, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 10 * time.Second}
	if proxyPort > 0 {
		// Always a CONNECT tunnel, http:// too: the proxy then sees only
		// host:port, never the path and its token (the core logs request
		// URIs of failed plain-HTTP proxy requests).
		tr.DialContext = connectDialer("127.0.0.1:" + strconv.Itoa(proxyPort))
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, fail("invalid URL")
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // drop the URL from the message
		}
		return nil, 0, fail("%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fail("HTTP %d", resp.StatusCode)
	}
	// Go decompresses gzip itself, so the limit counts decompressed bytes.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, 0, fail("%v", err)
	}
	if len(body) > maxBytes {
		return nil, 0, fail("response larger than 1 MB")
	}
	servers, skipped, err = config.ParseSIP008(body, rawURL)
	if err != nil {
		return nil, skipped, fail("%v", err)
	}
	return servers, skipped, nil
}

// Host is the URL's host name, for logs and messages.
func Host(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "(invalid URL)"
	}
	return u.Hostname()
}

// connectDialer returns a DialContext that opens every connection as an
// HTTP CONNECT tunnel through proxy.
func connectDialer(proxy string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", proxy)
		if err != nil {
			return nil, err
		}
		if dl, ok := ctx.Deadline(); ok {
			c.SetDeadline(dl)
		}
		req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: http.Header{}}
		if err := req.Write(c); err != nil {
			c.Close()
			return nil, err
		}
		resp, err := http.ReadResponse(bufio.NewReader(c), req)
		if err != nil {
			c.Close()
			return nil, err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			c.Close()
			return nil, fmt.Errorf("proxy refused the connection: %s", resp.Status)
		}
		c.SetDeadline(time.Time{})
		return c, nil
	}
}
