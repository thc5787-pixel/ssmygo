// Package socks5test is a minimal SOCKS5 UDP client for tests: UDP
// ASSOCIATE over TCP, then datagrams with the RFC 1928 header.
package socks5test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

// Session is one UDP association.
type Session struct {
	ctrl  net.Conn
	udp   *net.UDPConn
	relay *net.UDPAddr
}

// ErrRefused is returned when the server rejects UDP ASSOCIATE; Reply holds the code.
type ErrRefused struct{ Reply byte }

func (e ErrRefused) Error() string {
	return fmt.Sprintf("socks5: UDP ASSOCIATE refused (reply %d)", e.Reply)
}

// Associate opens a UDP association through the SOCKS5 server at tcpAddr.
func Associate(tcpAddr string) (*Session, error) {
	c, err := net.DialTimeout("tcp", tcpAddr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(3 * time.Second))
	c.Write([]byte{5, 1, 0})
	b := make([]byte, 4+16+2)
	if _, err := io.ReadFull(c, b[:2]); err != nil {
		c.Close()
		return nil, err
	}
	c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}) // UDP ASSOCIATE 0.0.0.0:0
	if _, err := io.ReadFull(c, b[:4]); err != nil {
		c.Close()
		return nil, err
	}
	if b[1] != 0 {
		c.Close()
		return nil, ErrRefused{b[1]}
	}
	var ip netip.Addr
	switch b[3] {
	case 1:
		io.ReadFull(c, b[:4+2])
		ip = netip.AddrFrom4([4]byte(b[:4]))
		b = b[4:]
	case 4:
		io.ReadFull(c, b[:16+2])
		ip = netip.AddrFrom16([16]byte(b[:16]))
		b = b[16:]
	default:
		c.Close()
		return nil, errors.New("socks5: unexpected bound address type")
	}
	port := binary.BigEndian.Uint16(b[:2])
	if ip.IsUnspecified() {
		ip = netip.MustParseAddr("127.0.0.1")
	}
	c.SetDeadline(time.Time{})
	u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		c.Close()
		return nil, err
	}
	return &Session{ctrl: c, udp: u, relay: net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, port))}, nil
}

// Send sends payload to target ("host:port"; host may be a domain).
func (s *Session) Send(target string, payload []byte) error {
	host, ps, err := net.SplitHostPort(target)
	if err != nil {
		return err
	}
	p, _ := strconv.Atoi(ps)
	pkt := []byte{0, 0, 0}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Is4() {
			pkt = append(pkt, 1)
		} else {
			pkt = append(pkt, 4)
		}
		pkt = append(pkt, ip.AsSlice()...)
	} else {
		pkt = append(pkt, 3, byte(len(host)))
		pkt = append(pkt, host...)
	}
	pkt = binary.BigEndian.AppendUint16(pkt, uint16(p))
	pkt = append(pkt, payload...)
	_, err = s.udp.WriteToUDP(pkt, s.relay)
	return err
}

// Recv returns the next payload, or an error after timeout.
func (s *Session) Recv(timeout time.Duration) ([]byte, error) {
	s.udp.SetReadDeadline(time.Now().Add(timeout))
	b := make([]byte, 65535)
	n, _, err := s.udp.ReadFromUDP(b)
	if err != nil {
		return nil, err
	}
	b = b[:n]
	if len(b) < 4 {
		return nil, errors.New("socks5: short datagram")
	}
	switch b[3] {
	case 1:
		return b[4+4+2:], nil
	case 4:
		return b[4+16+2:], nil
	case 3:
		return b[4+1+int(b[4])+2:], nil
	}
	return nil, errors.New("socks5: bad address type")
}

// Close ends the association.
func (s *Session) Close() { s.udp.Close(); s.ctrl.Close() }

// UDPEcho starts a UDP echo server on 127.0.0.1 and returns its address and a stop func.
func UDPEcho() (netip.AddrPort, func(), error) { return UDPEchoAt("127.0.0.1") }

// UDPEchoAt starts a UDP echo server on ip.
func UDPEchoAt(ip string) (netip.AddrPort, func(), error) {
	c, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(netip.AddrPortFrom(netip.MustParseAddr(ip), 0)))
	if err != nil {
		return netip.AddrPort{}, nil, err
	}
	go func() {
		b := make([]byte, 65535)
		for {
			n, from, err := c.ReadFromUDP(b)
			if err != nil {
				return
			}
			c.WriteToUDP(b[:n], from)
		}
	}()
	return c.LocalAddr().(*net.UDPAddr).AddrPort(), func() { c.Close() }, nil
}

// FreePort returns a port number free for both TCP and UDP on 127.0.0.1.
func FreePort(t testing.TB) int {
	t.Helper()
	for range 20 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := ln.Addr().(*net.TCPAddr).Port
		u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p})
		ln.Close()
		if err == nil {
			u.Close()
			return p
		}
	}
	t.Fatal("no port free for both TCP and UDP")
	return 0
}
