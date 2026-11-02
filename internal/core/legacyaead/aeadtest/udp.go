package aeadtest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/thc5787/ssmygo/internal/core/legacyaead"
	"golang.org/x/crypto/chacha20poly1305"
)

// UDPServer is an in-process legacy AEAD UDP server for tests.
type UDPServer struct {
	Addr     netip.AddrPort
	packets  atomic.Int64
	lastATYP atomic.Int32
	tamper   atomic.Int64
}

// Packets is how many client packets the server decrypted.
func (s *UDPServer) Packets() int64 { return s.packets.Load() }

// LastATYP is the SOCKS address type of the last target (1, 3 or 4).
func (s *UDPServer) LastATYP() byte { return byte(s.lastATYP.Load()) }

// TamperNext corrupts the tag of the next n replies.
func (s *UDPServer) TamperNext(n int64) { s.tamper.Store(n) }

// StartUDP runs a UDP server on 127.0.0.1 until the test ends. It decrypts
// each packet, sends the payload to the target, and encrypts the replies.
// Domain targets are resolved here, on the server (IPv4 only).
func StartUDP(t testing.TB, method, password string) *UDPServer {
	t.Helper()
	size, err := legacyaead.KeySize(method)
	if err != nil {
		t.Fatal(err)
	}
	key := legacyaead.DeriveKey(password, size)
	ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	s := &UDPServer{Addr: ln.LocalAddr().(*net.UDPAddr).AddrPort()}
	var (
		mu   sync.Mutex
		nats = map[netip.AddrPort]*net.UDPConn{}
	)
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range nats {
			c.Close()
		}
		mu.Unlock()
	})
	go func() {
		b := make([]byte, 65535)
		for {
			n, from, err := ln.ReadFromUDPAddrPort(b)
			if err != nil {
				return
			}
			if n < size+16 {
				continue
			}
			aead, err := newAEAD(method, legacyaead.SubKey(key, b[:size]))
			if err != nil {
				continue
			}
			plain, err := aead.Open(nil, make([]byte, aead.NonceSize()), b[size:n], nil)
			if err != nil {
				continue
			}
			atyp, target, rest, ok := parseAddr(plain)
			if !ok {
				continue
			}
			s.packets.Add(1)
			s.lastATYP.Store(int32(atyp))
			ua, err := net.ResolveUDPAddr("udp4", target)
			if err != nil {
				if ua, err = net.ResolveUDPAddr("udp", target); err != nil {
					continue
				}
			}
			mu.Lock()
			out := nats[from]
			if out == nil {
				out, err = net.ListenUDP("udp", nil)
				if err != nil {
					mu.Unlock()
					continue
				}
				nats[from] = out
				go s.replies(ln, out, from, method, key, size)
			}
			mu.Unlock()
			out.WriteToUDP(rest, ua)
		}
	}()
	return s
}

func (s *UDPServer) replies(ln, out *net.UDPConn, client netip.AddrPort, method string, key []byte, size int) {
	b := make([]byte, 65535)
	for {
		n, src, err := out.ReadFromUDPAddrPort(b)
		if err != nil {
			return
		}
		src = netip.AddrPortFrom(src.Addr().Unmap(), src.Port())
		salt := make([]byte, size)
		rand.Read(salt)
		aead, err := newAEAD(method, legacyaead.SubKey(key, salt))
		if err != nil {
			return
		}
		plain := appendAddr(nil, src)
		plain = append(plain, b[:n]...)
		pkt := aead.Seal(salt, make([]byte, aead.NonceSize()), plain, nil)
		if s.tamper.Add(-1) >= 0 {
			pkt[len(pkt)-1] ^= 0xFF
		}
		ln.WriteToUDPAddrPort(pkt, client)
	}
}

func appendAddr(b []byte, ap netip.AddrPort) []byte {
	if ap.Addr().Is4() {
		b = append(b, 1)
	} else {
		b = append(b, 4)
	}
	b = append(b, ap.Addr().AsSlice()...)
	return binary.BigEndian.AppendUint16(b, ap.Port())
}

func parseAddr(b []byte) (atyp byte, target string, rest []byte, ok bool) {
	if len(b) < 1 {
		return
	}
	atyp = b[0]
	var host string
	switch atyp {
	case 1:
		if len(b) < 1+4+2 {
			return
		}
		host, b = netip.AddrFrom4([4]byte(b[1:5])).String(), b[5:]
	case 4:
		if len(b) < 1+16+2 {
			return
		}
		host, b = netip.AddrFrom16([16]byte(b[1:17])).String(), b[17:]
	case 3:
		if len(b) < 2 || len(b) < 2+int(b[1])+2 {
			return
		}
		host, b = string(b[2:2+int(b[1])]), b[2+int(b[1]):]
	default:
		return
	}
	port := binary.BigEndian.Uint16(b[:2])
	return atyp, net.JoinHostPort(host, strconv.Itoa(int(port))), b[2:], true
}

// newAEAD is the test server's own copy, independent of the client code.
func newAEAD(method string, subkey []byte) (cipher.AEAD, error) {
	switch method {
	case "aes-128-gcm", "aes-192-gcm", "aes-256-gcm":
		block, err := aes.NewCipher(subkey)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	case "chacha20-ietf-poly1305":
		return chacha20poly1305.New(subkey)
	case "xchacha20-ietf-poly1305":
		return chacha20poly1305.NewX(subkey)
	}
	return nil, errors.New("aeadtest: unsupported method " + method)
}
