package service

// ssmygo patch C tests (see ../../PATCHES.md).

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/ssnone"
)

func startManager(t *testing.T, js string) {
	t.Helper()
	m, err := testManager(t, js)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Stop(); m.Close() })
}

func echoTarget(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	return netip.MustParseAddrPort(ln.Addr().String())
}

// countingRelay forwards to dst and counts accepted connections.
func countingRelay(t *testing.T, dst string) (string, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n.Add(1)
			go func() {
				defer c.Close()
				d, err := net.Dial("tcp", dst)
				if err != nil {
					return
				}
				defer d.Close()
				go io.Copy(d, c)
				io.Copy(c, d)
			}()
		}
	}()
	return ln.Addr().String(), &n
}

func socksEcho(addr string, target netip.AddrPort) error {
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte{5, 1, 0})
	b := make([]byte, 10)
	if _, err := io.ReadFull(c, b[:2]); err != nil {
		return err
	}
	req := append([]byte{5, 1, 0, 1}, target.Addr().AsSlice()...)
	req = binary.BigEndian.AppendUint16(req, target.Port())
	c.Write(req)
	if _, err := io.ReadFull(c, b); err != nil || b[1] != 0 {
		return fmt.Errorf("socks reply %v %v", b, err)
	}
	c.Write([]byte("hop"))
	got := make([]byte, 3)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "hop" {
		return fmt.Errorf("echo %q %v", got, err)
	}
	return nil
}

func TestDialerClientChain(t *testing.T) {
	target := echoTarget(t)
	hop := freePort(t)
	startManager(t, `{"servers":[{"name":"hop","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"`+hop+`"}]}],
		"clients":[{"name":"direct","protocol":"direct","enableTCP":true}]}`)
	ss := freePort(t)
	startManager(t, `{"servers":[{"name":"ss","protocol":"none","tcpListeners":[{"network":"tcp","address":"`+ss+`"}]}],
		"clients":[{"name":"direct","protocol":"direct","enableTCP":true}]}`)
	relay, count := countingRelay(t, hop)
	local := freePort(t)
	startManager(t, `{"servers":[{"name":"local","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"`+local+`"}]}],
		"clients":[{"name":"forward","protocol":"socks5","endpoint":"`+relay+`","enableTCP":true},
		           {"name":"x","protocol":"none","endpoint":"`+ss+`","enableTCP":true,"dialerClient":"forward"}],
		"router":{"defaultTCPClientName":"x"}}`)
	if err := socksEcho(local, target); err != nil {
		t.Fatal(err)
	}
	if count.Load() < 1 {
		t.Fatal("connection did not go through the dialer client")
	}
}

func TestDialerClientUnknown(t *testing.T) {
	_, err := testManager(t, `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"127.0.0.1:0"}]}],
		"clients":[{"name":"x","protocol":"none","endpoint":"127.0.0.1:1","enableTCP":true,"dialerClient":"nope"}]}`)
	if err == nil || !strings.Contains(err.Error(), `dialer client "nope" not found`) {
		t.Fatalf("err = %v", err)
	}
}

func TestDialerClientOrder(t *testing.T) {
	_, err := testManager(t, `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"127.0.0.1:0"}]}],
		"clients":[{"name":"x","protocol":"none","endpoint":"127.0.0.1:1","enableTCP":true,"dialerClient":"hop"},
		           {"name":"hop","protocol":"socks5","endpoint":"127.0.0.1:2","enableTCP":true}]}`)
	if err == nil || !strings.Contains(err.Error(), `dialer client "hop" not found`) {
		t.Fatalf("err = %v", err)
	}
}

func TestDialerClientUDP(t *testing.T) {
	_, err := testManager(t, `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"127.0.0.1:0"}]}],
		"clients":[{"name":"hop","protocol":"socks5","endpoint":"127.0.0.1:2","enableTCP":true},
		           {"name":"x","protocol":"none","endpoint":"127.0.0.1:1","enableTCP":true,"enableUDP":true,"mtu":1500,"dialerClient":"hop"}]}`)
	if err == nil || !strings.Contains(err.Error(), "does not carry UDP") {
		t.Fatalf("err = %v", err)
	}
}

func TestDialTimeoutReachesFactory(t *testing.T) {
	var got time.Duration
	RegisterClientProtocol("test-timeout", func(c *ClientConfig, inner netio.StreamClient) (netio.StreamClient, error) {
		got = c.DialTimeout.Value()
		cfg := ssnone.StreamClientConfig{Name: c.Name, InnerClient: inner}
		return cfg.NewStreamClient(), nil
	})
	m, err := testManager(t, `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"127.0.0.1:0"}]}],
		"clients":[{"name":"x","protocol":"test-timeout","endpoint":"127.0.0.1:1","enableTCP":true,"dialTimeout":"3s"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	if got != 3*time.Second {
		t.Fatalf("DialTimeout = %v", got)
	}
}
