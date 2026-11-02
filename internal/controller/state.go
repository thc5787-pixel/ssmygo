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
	// UDPDisabled: the forward proxy is on, so UDP ASSOCIATE is refused (A2-D2).
	UDPDisabled bool
	// ShowPluginOutput: Help ▸ Show Plugin Output; the log window shows
	// plugin output lines only when set.
	ShowPluginOutput bool
	// PluginFailure: plugin supervision gave up (crash loop, or the file was
	// replaced and is no longer trusted). Not set for refusals of the user's
	// own actions, which the UI handles where they happen.
	PluginFailure error
}

// HasServer reports whether there is a server to select.
func (s State) HasServer() bool { return len(s.Servers) > 0 }
