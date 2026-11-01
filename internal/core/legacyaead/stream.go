package legacyaead

import (
	"context"
	"crypto/rand"
	"io"
	"sync"

	"github.com/database64128/shadowsocks-go/conn"
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/socks5"
)

// StreamClientConfig configures a legacy AEAD stream client.
type StreamClientConfig struct {
	Name        string
	Method      string
	Key         []byte // master key, DeriveKey(password, KeySize(method))
	InnerClient netio.StreamClient
	Addr        conn.Addr // the Shadowsocks server
}

// NewStreamClient validates c and returns the client.
func (c *StreamClientConfig) NewStreamClient() (netio.StreamClient, error) {
	n, err := KeySize(c.Method)
	if err != nil {
		return nil, err
	}
	if len(c.Key) != n {
		return nil, io.ErrShortBuffer
	}
	return &streamClient{cfg: *c}, nil
}

type streamClient struct{ cfg StreamClientConfig }

func (c *streamClient) NewStreamDialer() (netio.StreamDialer, netio.StreamDialerInfo) {
	return c, netio.StreamDialerInfo{Name: c.cfg.Name, NativeInitialPayload: true}
}

// DialStream sends salt and the first chunk (target address + payload)
// together with the connection request.
func (c *streamClient) DialStream(ctx context.Context, addr conn.Addr, payload []byte) (netio.Conn, error) {
	a := &aeadConn{method: c.cfg.Method, key: c.cfg.Key}
	first := socks5.AppendAddrFromConnAddr(nil, addr)
	first = append(first, payload...)
	sealed, err := a.seal(nil, first)
	if err != nil {
		return nil, err
	}
	raw, err := c.cfg.InnerClient.DialStream(ctx, c.cfg.Addr, sealed)
	if err != nil {
		return nil, err
	}
	a.Conn = raw
	return a, nil
}

// NewConn wraps raw in the legacy AEAD framing in both directions: writes
// send this side's salt first, reads expect the peer's salt first. Servers
// use it directly; clients use StreamClientConfig.
func NewConn(raw netio.Conn, method string, key []byte) netio.Conn {
	return &aeadConn{Conn: raw, method: method, key: key}
}

type aeadConn struct {
	netio.Conn
	method string
	key    []byte

	wmu sync.Mutex
	enc *sealer

	rmu  sync.Mutex
	dec  *opener
	rbuf []byte // decrypted bytes not yet returned
	rerr error
}

// seal appends this side's salt (first time only) and the chunks of p.
func (a *aeadConn) seal(dst, p []byte) ([]byte, error) {
	if a.enc == nil {
		salt := make([]byte, len(a.key))
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		aead, err := newAEAD(a.method, SubKey(a.key, salt))
		if err != nil {
			return nil, err
		}
		a.enc = &sealer{aead: aead, nonce: make([]byte, aead.NonceSize())}
		dst = append(dst, salt...)
	}
	return a.enc.appendChunks(dst, p), nil
}

func (a *aeadConn) Write(p []byte) (int, error) {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	if len(p) == 0 && a.enc != nil {
		return 0, nil
	}
	out, err := a.seal(nil, p)
	if err != nil {
		return 0, err
	}
	if _, err := a.Conn.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (a *aeadConn) Read(p []byte) (int, error) {
	a.rmu.Lock()
	defer a.rmu.Unlock()
	for len(a.rbuf) == 0 {
		if a.rerr != nil {
			return 0, a.rerr
		}
		a.rbuf, a.rerr = a.readChunk()
	}
	n := copy(p, a.rbuf)
	a.rbuf = a.rbuf[n:]
	return n, nil
}

// readChunk reads the peer's salt (first time) and one chunk. A clean EOF
// at a chunk boundary is io.EOF; anything else is an error.
func (a *aeadConn) readChunk() ([]byte, error) {
	if a.dec == nil {
		salt := make([]byte, len(a.key))
		if _, err := io.ReadFull(a.Conn, salt); err != nil {
			return nil, err
		}
		aead, err := newAEAD(a.method, SubKey(a.key, salt))
		if err != nil {
			return nil, err
		}
		a.dec = &opener{aead: aead, nonce: make([]byte, aead.NonceSize()),
			buf: make([]byte, 0, maxPayload+aead.Overhead())}
	}
	tag := a.dec.aead.Overhead()
	hdr := make([]byte, lenSize+tag)
	if _, err := io.ReadFull(a.Conn, hdr); err != nil {
		return nil, err
	}
	l, err := a.dec.open(hdr)
	if err != nil {
		return nil, err
	}
	n := int(l[0])<<8 | int(l[1])
	if n == 0 || n > maxPayload {
		return nil, errAuth
	}
	body := make([]byte, n+tag)
	if _, err := io.ReadFull(a.Conn, body); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	out, err := a.dec.open(body)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), out...), nil
}
