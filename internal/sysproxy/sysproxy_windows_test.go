//go:build windows

package sysproxy

import (
	"runtime"
	"testing"
	"unsafe"
)

// C ABI sizes of INTERNET_PER_CONN_OPTION(_LIST)W and RASENTRYNAMEW.
func TestStructLayout(t *testing.T) {
	opt, list, ras := uintptr(16), uintptr(32), uintptr(1048)
	if runtime.GOARCH == "386" {
		opt, list, ras = 12, 20, 1048
	}
	if got := unsafe.Sizeof(perConnOption{}); got != opt {
		t.Errorf("perConnOption %d, want %d", got, opt)
	}
	if got := unsafe.Sizeof(perConnOptionList{}); got != list {
		t.Errorf("perConnOptionList %d, want %d", got, list)
	}
	if got := unsafe.Sizeof(rasEntryName{}); got != ras {
		t.Errorf("rasEntryName %d, want %d", got, ras)
	}
}

// Read-only: querying the live setting must work and report a sane flag.
func TestGetLive(t *testing.T) {
	s, err := Windows().Get()
	if err != nil {
		t.Fatal(err)
	}
	if s.Flags&FlagDirect == 0 {
		t.Errorf("flags %#x lack PROXY_TYPE_DIRECT (always set by Windows)", s.Flags)
	}
	t.Logf("live: %+v", s)
}
