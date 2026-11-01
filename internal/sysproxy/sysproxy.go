// Package sysproxy reads and writes the Windows (WinINet) system proxy,
// replacing the original client's sysproxy.exe. It does not depend on MyGo.
package sysproxy

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

// PROXY_TYPE_* flags of INTERNET_PER_CONN_FLAGS.
const (
	FlagDirect       uint32 = 0x1
	FlagProxy        uint32 = 0x2
	FlagAutoProxyURL uint32 = 0x4
	FlagAutoDetect   uint32 = 0x8
)

// Settings is one connection's proxy configuration.
type Settings struct {
	Flags  uint32
	Server string
	Bypass string
	PacURL string
}

// Proxy gets and sets the system proxy.
type Proxy interface {
	Get() (Settings, error)
	Set(Settings) error
}

// ErrUnsupported is returned off Windows.
var ErrUnsupported = errors.New("sysproxy: unsupported platform")

// PointsTo reports whether s sends traffic to a local proxy on port.
func PointsTo(s Settings, port int) bool {
	if s.Flags&FlagProxy == 0 {
		return false
	}
	p := strconv.Itoa(port)
	for _, part := range strings.Split(s.Server, ";") {
		if _, v, ok := strings.Cut(part, "="); ok {
			part = v
		}
		if part == "127.0.0.1:"+p || part == "localhost:"+p {
			return true
		}
	}
	return false
}

// PartialError means the LAN setting was applied but some dial-up/VPN
// entries failed. Callers treat it as a warning, not a failed change.
type PartialError struct {
	Failed map[string]error // entry name → error
}

func (e *PartialError) Error() string {
	names := make([]string, 0, len(e.Failed))
	for n := range e.Failed {
		names = append(names, n)
	}
	sort.Strings(names)
	return "sysproxy: could not update " + strings.Join(names, ", ")
}
