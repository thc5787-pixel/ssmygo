package controller

// Mode is the system proxy mode shown in the tray.
type Mode int

const (
	Disabled Mode = iota // system proxy off; the local port keeps working
	Rules                // the tray's "PAC": core routes by geosite rules
	Global               // everything through the server
)

func (m Mode) String() string {
	return [...]string{"Disabled", "Rules", "Global"}[m]
}

// State is what the UI shows.
type State struct {
	Mode      Mode
	Servers   []string // menu titles
	Current   int
	ShareLAN  bool
	LastError string // Err.Error(), for display
	Err       error  // typed: see PortInUseError, ErrNoServer, SaveError
	Port      int
	FirstRun  bool
	// ForwardIgnored: a forward proxy is set but the current server uses a
	// plugin, which connects by itself (A1-D3).
	ForwardIgnored bool
}

// HasServer reports whether there is a server to select.
func (s State) HasServer() bool { return len(s.Servers) > 0 }
