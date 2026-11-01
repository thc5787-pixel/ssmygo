package legacyaead

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	maxPayload = 0x3FFF // per-chunk payload limit of the protocol
	lenSize    = 2
)

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
	return nil, errors.New("legacyaead: unsupported method " + method)
}

// increment adds one to a little-endian nonce.
func increment(nonce []byte) {
	for i := range nonce {
		nonce[i]++
		if nonce[i] != 0 {
			return
		}
	}
}

// sealer encrypts chunks of one direction of a session.
type sealer struct {
	aead  cipher.AEAD
	nonce []byte
}

// appendChunks seals p as [len][payload] chunks and appends them to dst.
func (s *sealer) appendChunks(dst, p []byte) []byte {
	for len(p) > 0 {
		n := min(len(p), maxPayload)
		var l [lenSize]byte
		l[0], l[1] = byte(n>>8), byte(n)
		dst = s.aead.Seal(dst, s.nonce, l[:], nil)
		increment(s.nonce)
		dst = s.aead.Seal(dst, s.nonce, p[:n], nil)
		increment(s.nonce)
		p = p[n:]
	}
	return dst
}

// opener decrypts chunks of one direction of a session.
type opener struct {
	aead  cipher.AEAD
	nonce []byte
	buf   []byte
}

var errAuth = errors.New("legacyaead: authentication failed")

// open decrypts a sealed block of exactly sealedLen bytes from b.
func (o *opener) open(b []byte) ([]byte, error) {
	out, err := o.aead.Open(o.buf[:0], o.nonce, b, nil)
	if err != nil {
		return nil, errAuth
	}
	increment(o.nonce)
	return out, nil
}
