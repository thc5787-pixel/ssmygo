package service

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/tslog"
)

type testMeter struct{ up, down atomic.Uint64 }

func (m *testMeter) Add(up, down uint64) { m.up.Add(up); m.down.Add(down) }

// ssmygo patch A4: the UDP NAT relay reports payload bytes, not SOCKS
// headers, in both directions.
func TestUDPNATRelayCountsPayload(t *testing.T) {
	echo, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		b := make([]byte, 2048)
		for {
			n, from, err := echo.ReadFromUDP(b)
			if err != nil {
				return
			}
			echo.WriteToUDP(b[:n], from)
		}
	}()
	probe, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	addr := probe.LocalAddr().String()
	probe.Close()

	js := fmt.Sprintf(`{"servers":[{"name":"s","protocol":"socks5","mtu":1500,"udpListeners":[{"network":"udp","address":%q}]}],
		"clients":[{"name":"direct","protocol":"direct","enableUDP":true,"mtu":1500}]}`, addr)
	var cfg Config
	if err := json.Unmarshal([]byte(js), &cfg); err != nil {
		t.Fatal(err)
	}
	m := &testMeter{}
	cfg.Servers[0].TrafficMeter = m
	logger, _ := tslog.Config{Level: 8}.NewLogger(io.Discard)
	mgr, err := cfg.NewManager(logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { mgr.Stop(); mgr.Close() }()

	c, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ea := echo.LocalAddr().(*net.UDPAddr)
	pkt := append([]byte{0, 0, 0, 1}, ea.IP.To4()...)
	pkt = binary.BigEndian.AppendUint16(pkt, uint16(ea.Port))
	pkt = append(pkt, make([]byte, 100)...)
	c.Write(pkt)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	b := make([]byte, 2048)
	if _, err := c.Read(b); err != nil {
		t.Fatal(err)
	}
	// The reply is counted just after it is written: allow it to land.
	for deadline := time.Now().Add(time.Second); m.down.Load() == 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if m.up.Load() != 100 || m.down.Load() != 100 {
		t.Fatalf("meter up %d down %d, want 100 100", m.up.Load(), m.down.Load())
	}
}
