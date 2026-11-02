package legacyaead

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"

	"github.com/database64128/shadowsocks-go/conn"
	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/service"
	"github.com/database64128/shadowsocks-go/socks5"
	"github.com/database64128/shadowsocks-go/zerocopy"
)

const tagSize = 16

// UDPClient is a legacy AEAD Shadowsocks UDP client. Each packet is
// salt | Seal(SubKey(key, salt), nonce 0, SOCKS address + payload).
type UDPClient struct {
	method   string
	key      []byte
	addr     conn.Addr
	pref     netio.AddressFamilyPreference
	resolver conn.Resolver
	info     zerocopy.UDPClientSessionInfo
}

// NewUDPClient creates a UDP client for method with the master key.
func NewUDPClient(name, method string, key []byte, env service.UDPEnv) (zerocopy.UDPClient, error) {
	size, err := KeySize(method)
	if err != nil {
		return nil, err
	}
	if len(key) != size {
		return nil, fmt.Errorf("legacyaead: key is %d bytes, want %d", len(key), size)
	}
	return &UDPClient{
		method:   method,
		key:      key,
		addr:     env.Addr,
		pref:     env.Preference,
		resolver: env.Resolver,
		info: zerocopy.UDPClientSessionInfo{
			Name:           name,
			PackerHeadroom: packerHeadroom(size),
			MTU:            env.MTU,
			SocketConfig:   env.SocketConfig,
		},
	}, nil
}

func packerHeadroom(size int) zerocopy.Headroom {
	return zerocopy.Headroom{Front: size + socks5.MaxAddrLen, Rear: tagSize}
}

// Info implements [zerocopy.UDPClient.Info].
func (c *UDPClient) Info() zerocopy.UDPClientInfo {
	return zerocopy.UDPClientInfo{Name: c.info.Name, PackerHeadroom: c.info.PackerHeadroom}
}

// NewSession implements [zerocopy.UDPClient.NewSession].
func (c *UDPClient) NewSession(ctx context.Context) (zerocopy.UDPClientSessionInfo, zerocopy.UDPClientSession, error) {
	server, err := netio.ResolveIPPort(ctx, c.addr, c.pref, c.resolver)
	if err != nil {
		return c.info, zerocopy.UDPClientSession{}, fmt.Errorf("failed to resolve endpoint address: %w", err)
	}
	max := zerocopy.MaxPacketSizeForAddr(c.info.MTU, server.Addr())
	pu := &packUnpacker{method: c.method, key: c.key, server: server, maxPacketSize: max}
	return c.info, zerocopy.UDPClientSession{
		MaxPacketSize: max,
		Packer:        pu,
		Unpacker:      pu,
		Close:         zerocopy.NoopClose,
	}, nil
}

type packUnpacker struct {
	method        string
	key           []byte
	server        netip.AddrPort
	maxPacketSize int
}

func (p *packUnpacker) ClientPackerInfo() zerocopy.ClientPackerInfo {
	return zerocopy.ClientPackerInfo{Headroom: packerHeadroom(len(p.key))}
}

// PackInPlace keeps domain targets as domains: the server resolves them.
func (p *packUnpacker) PackInPlace(ctx context.Context, b []byte, targetAddr conn.Addr, payloadStart, payloadLen int) (destAddrPort netip.AddrPort, packetStart, packetLen int, err error) {
	size := len(p.key)
	addrLen := socks5.LengthOfAddrFromConnAddr(targetAddr)
	plainStart := payloadStart - addrLen
	packetStart = plainStart - size
	packetLen = size + addrLen + payloadLen + tagSize
	destAddrPort = p.server
	if packetLen > p.maxPacketSize {
		err = zerocopy.ErrPayloadTooBig
		return
	}
	socks5.PutAddrFromConnAddr(b[plainStart:], targetAddr)
	salt := b[packetStart:plainStart]
	rand.Read(salt)
	aead, err := newAEAD(p.method, SubKey(p.key, salt))
	if err != nil {
		return
	}
	aead.Seal(b[plainStart:plainStart], make([]byte, aead.NonceSize()), b[plainStart:payloadStart+payloadLen], nil)
	return
}

func (p *packUnpacker) ClientUnpackerInfo() zerocopy.ClientUnpackerInfo {
	return zerocopy.ClientUnpackerInfo{Headroom: zerocopy.Headroom{Front: len(p.key) + socks5.IPv6AddrLen, Rear: tagSize}}
}

var errShortPacket = errors.New("legacyaead: packet too short")

// UnpackInPlace drops packets not from the server or failing authentication.
func (p *packUnpacker) UnpackInPlace(b []byte, packetSourceAddrPort netip.AddrPort, packetStart, packetLen int) (payloadSourceAddrPort netip.AddrPort, payloadStart, payloadLen int, err error) {
	if !conn.AddrPortMappedEqual(packetSourceAddrPort, p.server) {
		err = fmt.Errorf("dropped packet from non-server source %s", packetSourceAddrPort)
		return
	}
	size := len(p.key)
	if packetLen < size+tagSize {
		err = errShortPacket
		return
	}
	aead, err := newAEAD(p.method, SubKey(p.key, b[packetStart:packetStart+size]))
	if err != nil {
		return
	}
	cipherStart := packetStart + size
	plain, err := aead.Open(b[cipherStart:cipherStart], make([]byte, aead.NonceSize()), b[cipherStart:packetStart+packetLen], nil)
	if err != nil {
		err = errAuth
		return
	}
	var addrLen int
	payloadSourceAddrPort, addrLen, err = socks5.AddrPortFromBytes(plain)
	if err != nil {
		return
	}
	payloadStart = cipherStart + addrLen
	payloadLen = len(plain) - addrLen
	return
}
