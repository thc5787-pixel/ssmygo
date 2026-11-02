package config

// WindowState is a window's last state, kept for MyGo's window state
// (WindowOptions.StateKey) in Config.WindowStates: its bounds in the normal
// state and whether it was maximized or full screen. MyGo restores it only
// where it shows on a connected display.
type WindowState struct {
	X          int  `json:"x"`
	Y          int  `json:"y"`
	Width      int  `json:"width"`
	Height     int  `json:"height"`
	Maximized  bool `json:"maximized,omitempty"`
	FullScreen bool `json:"fullScreen,omitempty"`
}
