// Package legacyaead implements the Shadowsocks AEAD client (the original
// shadowsocks-windows ciphers) as a shadowsocks-go stream client protocol.
package legacyaead

import (
	"crypto/md5"
	"crypto/sha1"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// Methods lists the legacy AEAD methods this package registers.
var Methods = []string{"aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305"}

// KeySize returns the key (and salt) size of method in bytes.
func KeySize(method string) (int, error) {
	switch method {
	case "aes-128-gcm":
		return 16, nil
	case "aes-192-gcm":
		return 24, nil
	case "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305":
		return 32, nil
	}
	return 0, fmt.Errorf("legacyaead: unsupported method %q", method)
}

// DeriveKey is OpenSSL's EVP_BytesToKey with MD5 and no salt, as every
// Shadowsocks implementation uses to turn a password into a master key.
func DeriveKey(password string, size int) []byte {
	key := make([]byte, 0, size+md5.Size)
	var prev []byte
	for len(key) < size {
		h := md5.New()
		h.Write(prev)
		h.Write([]byte(password))
		prev = h.Sum(nil)
		key = append(key, prev...)
	}
	return key[:size]
}

// SubKey derives the per-session key: HKDF-SHA1(key, salt, "ss-subkey").
func SubKey(key, salt []byte) []byte {
	sub := make([]byte, len(key))
	if _, err := io.ReadFull(hkdf.New(sha1.New, key, salt, []byte("ss-subkey")), sub); err != nil {
		panic(err) // cannot happen for len(key) ≤ 255*20
	}
	return sub
}
