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

// oldBounds is how 1.0.0 kept a window's place: ssmygoConfigWindow for
// Edit Servers, and width/height/left/top(/maximized) in logViewer.
type oldBounds struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Left      int  `json:"left"`
	Top       int  `json:"top"`
	Maximized bool `json:"maximized"`
}

// state is b as a WindowState, as 1.0.0 restored it: a missing size or
// one below minW×minH is the default, defW×defH; false without a
// position (the window opened centred) or at Windows' parking spot of
// minimized windows (-32000).
func (b oldBounds) state(defW, defH, minW, minH int) (WindowState, bool) {
	if b.Width < minW || b.Height < minH {
		b.Width, b.Height = defW, defH
	}
	if (b.Left == 0 && b.Top == 0) || b.Left < -10000 || b.Top < -10000 {
		return WindowState{}, false
	}
	return WindowState{X: b.Left, Y: b.Top, Width: b.Width, Height: b.Height, Maximized: b.Maximized}, true
}

// migrateWindow puts 1.0.0's place of window key into WindowStates,
// unless MyGo already saved one.
func (c *Config) migrateWindow(key string, b *oldBounds, defW, defH, minW, minH int) {
	if b == nil {
		return
	}
	if _, ok := c.WindowStates[key]; ok {
		return
	}
	if s, ok := b.state(defW, defH, minW, minH); ok {
		if c.WindowStates == nil {
			c.WindowStates = map[string]WindowState{}
		}
		c.WindowStates[key] = s
	}
}
