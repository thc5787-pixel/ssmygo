package config

import "encoding/json"

// WindowBounds is a window's last size and position (ssmygo field). Zero
// size means the default size; zero position means centred.
type WindowBounds struct {
	Width     int  `json:"width,omitzero"`
	Height    int  `json:"height,omitzero"`
	Left      int  `json:"left,omitzero"`
	Top       int  `json:"top,omitzero"`
	Maximized bool `json:"maximized,omitzero"`
}

type windowBoundsFields WindowBounds

// UnmarshalJSON drops a size below 400×300 and Windows' parking position of
// minimized windows (-32000), which would reopen the window unusable or
// off-screen.
func (b *WindowBounds) UnmarshalJSON(data []byte) error {
	var f windowBoundsFields
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f.Width < 400 || f.Height < 300 {
		f.Width, f.Height = 0, 0
	}
	if f.Left < -10000 || f.Top < -10000 {
		f.Left, f.Top = 0, 0
	}
	*b = WindowBounds(f)
	return nil
}
