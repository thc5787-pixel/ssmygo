// Package aeadtest runs legacy AEAD Shadowsocks servers for tests: an
// in-process one, and optionally a shadowsocks-rust ssserver for interop.
package aeadtest

import (
	"io"
	"net"
	"net/netip"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/database64128/shadowsocks-go/socks5"
	"github.com/thc5787/ssmygo/internal/core/legacyaead"
)

// Start runs an in-process server on 127.0.0.1 until the test ends.
func Start(t testing.TB, method, password string) netip.AddrPort {
	t.Helper()
	size, err := legacyaead.KeySize(method)
	if err != nil {
		t.Fatal(err)
	}
	key := legacyaead.DeriveKey(password, size)
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
			go serve(c.(*net.TCPConn), method, key)
		}
	}()
	return netip.MustParseAddrPort(ln.Addr().String())
}

func serve(raw *net.TCPConn, method string, key []byte) {
	defer raw.Close()
	c := legacyaead.NewConn(raw, method, key)
	raw.SetReadDeadline(time.Now().Add(10 * time.Second))
	addr, err := socks5.ConnAddrFromReader(c, nil)
	if err != nil {
		return
	}
	raw.SetReadDeadline(time.Time{})
	target, err := net.DialTimeout("tcp", addr.String(), 5*time.Second)
	if err != nil {
		return
	}
	defer target.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(target, c)
		target.(*net.TCPConn).CloseWrite()
	}()
	io.Copy(c, target)
	c.CloseWrite()
	wg.Wait()
}

// StartRust runs shadowsocks-rust's ssserver binary at bin until the test ends.
func StartRust(t testing.TB, bin, method, password string) netip.AddrPort {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddrPort(ln.Addr().String())
	ln.Close()
	cmd := exec.Command(bin, "-s", addr.String(), "-m", method, "-k", password)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("tcp", addr.String()); err == nil {
			c.Close()
			return addr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("ssserver did not listen on %s", addr)
	return netip.AddrPort{}
}
