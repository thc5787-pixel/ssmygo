package legacyaead

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"testing"

	"golang.org/x/crypto/hkdf"
)

func TestDeriveKey(t *testing.T) {
	k := DeriveKey("foobar", 32)
	if hex.EncodeToString(k[:16]) != "3858f62230ac3c915f300c664312c63f" {
		t.Fatalf("k[:16] = %x", k[:16])
	}
	second := md5.Sum(append(append([]byte{}, k[:16]...), "foobar"...))
	if !bytes.Equal(k[16:], second[:]) {
		t.Fatalf("k[16:] = %x, want %x", k[16:], second)
	}
	if !bytes.Equal(DeriveKey("foobar", 16), k[:16]) {
		t.Fatal("16-byte key is not a prefix")
	}
	if len(DeriveKey("foobar", 24)) != 24 {
		t.Fatal("24-byte key length")
	}
}

func TestSubKey(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	salt := bytes.Repeat([]byte{2}, 32)
	want := make([]byte, 32)
	io.ReadFull(hkdf.New(sha1.New, key, salt, []byte("ss-subkey")), want)
	if got := SubKey(key, salt); !bytes.Equal(got, want) {
		t.Fatalf("SubKey = %x, want %x", got, want)
	}
}

func TestKeySize(t *testing.T) {
	want := map[string]int{"aes-128-gcm": 16, "aes-192-gcm": 24, "aes-256-gcm": 32,
		"chacha20-ietf-poly1305": 32, "xchacha20-ietf-poly1305": 32}
	for m, n := range want {
		if got, err := KeySize(m); err != nil || got != n {
			t.Errorf("%s: %d %v", m, got, err)
		}
	}
	if _, err := KeySize("rc4-md5"); err == nil {
		t.Error("rc4-md5 accepted")
	}
}
