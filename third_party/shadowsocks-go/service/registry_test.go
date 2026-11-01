package service

// ssmygo patch A/B tests (see ../../PATCHES.md).

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/netio"
	"github.com/database64128/shadowsocks-go/socks5"
	"github.com/database64128/shadowsocks-go/ssnone"
	"github.com/database64128/shadowsocks-go/tslog"
)

func testManager(t *testing.T, js string) (*Manager, error) {
	t.Helper()
	var cfg Config
	if err := json.Unmarshal([]byte(js), &cfg); err != nil {
		t.Fatal(err)
	}
	logger, err := tslog.Config{Level: 8}.NewLogger(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.NewManager(logger)
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestRegisterClientProtocol(t *testing.T) {
	RegisterClientProtocol("test-echo", func(c *ClientConfig, inner *netio.TCPClient) (netio.StreamClient, error) {
		cfg := ssnone.StreamClientConfig{Name: c.Name, InnerClient: inner}
		return cfg.NewStreamClient(), nil
	})
	m, err := testManager(t, `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"127.0.0.1:0"}]}],"clients":[{"name":"x","protocol":"test-echo","endpoint":"127.0.0.1:1","enableTCP":true}]}`)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.Close()
}

func TestRegisteredClientRejectsUDP(t *testing.T) {
	RegisterClientProtocol("test-tcponly", func(c *ClientConfig, inner *netio.TCPClient) (netio.StreamClient, error) {
		cfg := ssnone.StreamClientConfig{Name: c.Name, InnerClient: inner}
		return cfg.NewStreamClient(), nil
	})
	_, err := testManager(t, `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"127.0.0.1:0"}]}],"clients":[{"name":"x","protocol":"test-tcponly","endpoint":"127.0.0.1:1","enableTCP":true,"enableUDP":true,"mtu":1500}]}`)
	if err == nil || !strings.Contains(err.Error(), `protocol "test-tcponly": UDP not supported`) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownProtocolStillErrors(t *testing.T) {
	_, err := testManager(t, `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"127.0.0.1:0"}]}],"clients":[{"name":"x","protocol":"nope","endpoint":"127.0.0.1:1","enableTCP":true}]}`)
	if err == nil || !strings.Contains(err.Error(), `unknown protocol: "nope"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientPasswordField(t *testing.T) {
	var got string
	RegisterClientProtocol("test-pass", func(c *ClientConfig, inner *netio.TCPClient) (netio.StreamClient, error) {
		got = c.Password
		cfg := ssnone.StreamClientConfig{Name: c.Name, InnerClient: inner}
		return cfg.NewStreamClient(), nil
	})
	m, err := testManager(t, `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"127.0.0.1:0"}]}],"clients":[{"name":"x","protocol":"test-pass","endpoint":"127.0.0.1:1","enableTCP":true,"password":"s3cret"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	if got != "s3cret" {
		t.Fatalf("password = %q", got)
	}
}

func TestRegisterServerProtocol(t *testing.T) {
	RegisterServerProtocol("test-socks", func(sc *ServerConfig) (netio.StreamServer, error) {
		cfg := socks5.StreamServerConfig{EnableTCP: true}
		return cfg.NewStreamServer()
	})
	m, err := testManager(t, `{"servers":[{"name":"s","protocol":"test-socks","tcpListeners":[{"network":"tcp","address":"`+freePort(t)+`"}]}]}`)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestStartReturnsBindError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	m, err := testManager(t, `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"`+ln.Addr().String()+`"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	done := make(chan error, 1)
	go func() { done <- m.Start(context.Background()) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "bind") {
			t.Fatalf("Start err = %v, want bind error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not return within 1s")
	}
}

func TestStopIdempotent(t *testing.T) {
	m, err := testManager(t, `{"servers":[{"name":"s","protocol":"socks5","tcpListeners":[{"network":"tcp","address":"`+freePort(t)+`"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestDuplicateRegisterPanics(t *testing.T) {
	f := func(c *ClientConfig, inner *netio.TCPClient) (netio.StreamClient, error) { return nil, nil }
	RegisterClientProtocol("test-dup", f)
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate RegisterClientProtocol did not panic")
		}
	}()
	RegisterClientProtocol("test-dup", f)
}
