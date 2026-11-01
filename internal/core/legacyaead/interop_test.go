package legacyaead_test

import (
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"

	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/socks"
)

// startGoSS2 runs an independent implementation (go-shadowsocks2) as the
// server, so client bugs mirrored in our own test server cannot hide.
func startGoSS2(t *testing.T, name, password string) netip.AddrPort {
	t.Helper()
	ciph, err := core.PickCipher(name, nil, password)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer raw.Close()
				c := ciph.StreamConn(raw)
				tgt, err := socks.ReadAddr(c)
				if err != nil {
					return
				}
				target, err := net.Dial("tcp", tgt.String())
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
				raw.(*net.TCPConn).CloseWrite()
				wg.Wait()
			}()
		}
	}()
	return netip.MustParseAddrPort(ln.Addr().String())
}

func TestInteropGoShadowsocks2(t *testing.T) {
	target := echoAfterEOF(t)
	for method, name := range map[string]string{
		"aes-128-gcm":            "AEAD_AES_128_GCM",
		"aes-256-gcm":            "AEAD_AES_256_GCM",
		"chacha20-ietf-poly1305": "AEAD_CHACHA20_POLY1305",
	} {
		t.Run(method, func(t *testing.T) {
			srv := startGoSS2(t, name, "interop-pw")
			c := dialer(t, method, "interop-pw", srv)
			p := make([]byte, 100_000)
			for i := range p {
				p[i] = byte(i)
			}
			if got := roundTrip(t, c, target, p); string(got) != string(p) {
				t.Fatalf("got %d bytes, want %d", len(got), len(p))
			}
		})
	}
}
