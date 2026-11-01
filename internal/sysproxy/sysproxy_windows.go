//go:build windows

package sysproxy

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	wininet  = windows.NewLazySystemDLL("wininet.dll")
	rasapi32 = windows.NewLazySystemDLL("rasapi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procQueryOption = wininet.NewProc("InternetQueryOptionW")
	procSetOption   = wininet.NewProc("InternetSetOptionW")
	procRasEnum     = rasapi32.NewProc("RasEnumEntriesW")
	procGlobalFree  = kernel32.NewProc("GlobalFree")
)

const (
	optPerConnection   = 75 // INTERNET_OPTION_PER_CONNECTION_OPTION
	optSettingsChanged = 39 // INTERNET_OPTION_SETTINGS_CHANGED
	optRefresh         = 37 // INTERNET_OPTION_REFRESH

	perConnFlags      = 1 // INTERNET_PER_CONN_FLAGS
	perConnServer     = 2 // INTERNET_PER_CONN_PROXY_SERVER
	perConnBypass     = 3 // INTERNET_PER_CONN_PROXY_BYPASS
	perConnAutoURL    = 4 // INTERNET_PER_CONN_AUTOCONFIG_URL
	errBufferTooSmall = 603
)

// perConnOption is INTERNET_PER_CONN_OPTIONW: a DWORD and an 8-byte union.
// uint64 gets the C alignment on every arch (8 on 64-bit, 4 on 386).
type perConnOption struct {
	Option uint32
	Value  uint64
}

// perConnOptionList is INTERNET_PER_CONN_OPTION_LISTW.
type perConnOptionList struct {
	Size        uint32
	Connection  *uint16
	OptionCount uint32
	OptionError uint32
	Options     *perConnOption
}

// rasEntryName is RASENTRYNAMEW (WINVER >= 0x500).
type rasEntryName struct {
	Size  uint32
	Name  [257]uint16
	Flags uint32
	Path  [261]uint16
}

type winProxy struct{}

// Windows returns the WinINet system proxy.
func Windows() Proxy { return winProxy{} }

// Get reads the LAN (default connection) settings.
func (winProxy) Get() (Settings, error) {
	opts := []perConnOption{{Option: perConnFlags}, {Option: perConnServer}, {Option: perConnBypass}, {Option: perConnAutoURL}}
	list := perConnOptionList{OptionCount: uint32(len(opts)), Options: &opts[0]}
	list.Size = uint32(unsafe.Sizeof(list))
	size := list.Size
	r, _, err := procQueryOption.Call(0, optPerConnection, uintptr(unsafe.Pointer(&list)), uintptr(unsafe.Pointer(&size)))
	runtime.KeepAlive(opts)
	if r == 0 {
		return Settings{}, fmt.Errorf("sysproxy: InternetQueryOption: %w", err)
	}
	s := Settings{Flags: uint32(opts[0].Value)}
	s.Server = takeString(opts[1].Value)
	s.Bypass = takeString(opts[2].Value)
	s.PacURL = takeString(opts[3].Value)
	return s, nil
}

// takeString converts and frees a string WinINet allocated with GlobalAlloc.
// v is the option union; its first word is the pointer (little-endian).
func takeString(v uint64) string {
	if v == 0 {
		return ""
	}
	p := *(**uint16)(unsafe.Pointer(&v))
	s := windows.UTF16PtrToString(p)
	procGlobalFree.Call(uintptr(v))
	return s
}

// Set applies s to the LAN connection and every dial-up/VPN entry, then
// tells running programs to reload.
// A LAN failure is an error; failures on dial-up/VPN entries only are
// reported as *PartialError after the rest was applied.
func (w winProxy) Set(s Settings) error {
	if err := setOne("", s); err != nil {
		return err
	}
	var partial *PartialError
	if names, err := rasEntries(); err == nil {
		for _, name := range names {
			if err := setOne(name, s); err != nil {
				if partial == nil {
					partial = &PartialError{Failed: map[string]error{}}
				}
				partial.Failed[name] = err
			}
		}
	}
	procSetOption.Call(0, optSettingsChanged, 0, 0)
	procSetOption.Call(0, optRefresh, 0, 0)
	if partial != nil {
		return partial
	}
	return nil
}

func setOne(conn string, s Settings) error {
	server, _ := windows.UTF16PtrFromString(s.Server)
	bypass, _ := windows.UTF16PtrFromString(s.Bypass)
	pac, _ := windows.UTF16PtrFromString(s.PacURL)
	opts := []perConnOption{
		{Option: perConnFlags, Value: uint64(s.Flags)},
		{Option: perConnServer, Value: uint64(uintptr(unsafe.Pointer(server)))},
		{Option: perConnBypass, Value: uint64(uintptr(unsafe.Pointer(bypass)))},
		{Option: perConnAutoURL, Value: uint64(uintptr(unsafe.Pointer(pac)))},
	}
	list := perConnOptionList{OptionCount: uint32(len(opts)), Options: &opts[0]}
	if conn != "" {
		list.Connection, _ = windows.UTF16PtrFromString(conn)
	}
	list.Size = uint32(unsafe.Sizeof(list))
	r, _, err := procSetOption.Call(0, optPerConnection, uintptr(unsafe.Pointer(&list)), uintptr(list.Size))
	runtime.KeepAlive(server)
	runtime.KeepAlive(bypass)
	runtime.KeepAlive(pac)
	runtime.KeepAlive(opts)
	if r == 0 {
		name := conn
		if name == "" {
			name = "LAN"
		}
		return fmt.Errorf("sysproxy: InternetSetOption(%s): %w", name, err)
	}
	return nil
}

// rasEntries lists dial-up/VPN phonebook entries; none is not an error.
func rasEntries() ([]string, error) {
	if err := procRasEnum.Find(); err != nil {
		return nil, err
	}
	entries := make([]rasEntryName, 1)
	entries[0].Size = uint32(unsafe.Sizeof(entries[0]))
	cb := uint32(unsafe.Sizeof(entries[0]))
	var n uint32
	r, _, _ := procRasEnum.Call(0, 0, uintptr(unsafe.Pointer(&entries[0])), uintptr(unsafe.Pointer(&cb)), uintptr(unsafe.Pointer(&n)))
	if r == errBufferTooSmall {
		entries = make([]rasEntryName, int(cb)/int(unsafe.Sizeof(entries[0]))+1)
		entries[0].Size = uint32(unsafe.Sizeof(entries[0]))
		r, _, _ = procRasEnum.Call(0, 0, uintptr(unsafe.Pointer(&entries[0])), uintptr(unsafe.Pointer(&cb)), uintptr(unsafe.Pointer(&n)))
	}
	if r != 0 {
		return nil, errors.New("sysproxy: RasEnumEntries failed")
	}
	names := make([]string, n)
	for i := range names {
		names[i] = windows.UTF16ToString(entries[i].Name[:])
	}
	return names, nil
}
