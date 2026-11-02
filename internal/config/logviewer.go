package config

import (
	"encoding/json"
	"strconv"
	"strings"
)

// LogViewer is the "logViewer" section. topMost, wrapText, toolbarShown and
// Font are the original client's; BackgroundColor and TextColor are kept in
// extra and written back unused (colors follow the theme). Size, position
// and splitter are ssmygo's (the original kept them outside the JSON).
type LogViewer struct {
	TopMost      bool    `json:"topMost"`
	WrapText     bool    `json:"wrapText"`
	ToolbarShown bool    `json:"toolbarShown"`
	FontFamily   string  `json:"-"`
	FontSize     float32 `json:"-"` // points
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	Left         int     `json:"left"`
	Top          int     `json:"top"`
	Maximized    bool    `json:"maximized"`
	Splitter     float32 `json:"splitter"` // log text's share of the height, 0–1

	font       string // Font as read; written back while the font is unchanged
	readFamily string
	readSize   float32
	extra      map[string]json.RawMessage
}

const (
	defaultFontFamily = "Consolas"
	defaultFontSize   = 9
)

// DefaultLogViewer is used when the section is missing.
func DefaultLogViewer() LogViewer {
	return LogViewer{FontFamily: defaultFontFamily, FontSize: defaultFontSize, Width: 640, Height: 420, Splitter: 0.7}
}

// logViewerFields is the JSON shape. ssmygo's own keys are pointers so a
// section the user never changed round-trips byte for byte.
type logViewerFields struct {
	TopMost      bool     `json:"topMost"`
	WrapText     bool     `json:"wrapText"`
	ToolbarShown bool     `json:"toolbarShown"`
	Font         string   `json:"Font,omitempty"`
	Width        *int     `json:"width,omitempty"`
	Height       *int     `json:"height,omitempty"`
	Left         *int     `json:"left,omitempty"`
	Top          *int     `json:"top,omitempty"`
	Maximized    *bool    `json:"maximized,omitempty"`
	Splitter     *float32 `json:"splitter,omitempty"`
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func (v *LogViewer) UnmarshalJSON(b []byte) error {
	d := DefaultLogViewer()
	var f logViewerFields
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	extra, err := unknownFields(b, &f)
	if err != nil {
		return err
	}
	*v = LogViewer{TopMost: f.TopMost, WrapText: f.WrapText, ToolbarShown: f.ToolbarShown,
		Width: deref(f.Width, d.Width), Height: deref(f.Height, d.Height), Left: deref(f.Left, 0), Top: deref(f.Top, 0),
		Maximized: deref(f.Maximized, false), Splitter: deref(f.Splitter, d.Splitter),
		font: f.Font, extra: extra}
	v.FontFamily, v.FontSize = parseFont(f.Font)
	v.readFamily, v.readSize = v.FontFamily, v.FontSize
	if v.Width < 400 || v.Height < 240 {
		v.Width, v.Height = d.Width, d.Height
	}
	// Windows parks minimized windows at -32000; such a position (or any far
	// off-screen one) reopens the window centred instead.
	if v.Left < -10000 || v.Top < -10000 {
		v.Left, v.Top = 0, 0
	}
	if v.Splitter <= 0 || v.Splitter >= 1 {
		v.Splitter = d.Splitter
	}
	return nil
}

func (v LogViewer) MarshalJSON() ([]byte, error) {
	font := v.font
	if font == "" || v.FontFamily != v.readFamily || v.FontSize != v.readSize {
		font = v.FontFamily + ", " + strconv.FormatFloat(float64(v.FontSize), 'f', -1, 32) + "pt"
	}
	f := logViewerFields{TopMost: v.TopMost, WrapText: v.WrapText, ToolbarShown: v.ToolbarShown, Font: font}
	d := DefaultLogViewer()
	if v.Width != d.Width || v.Height != d.Height || v.Left != 0 || v.Top != 0 || v.Maximized || v.Splitter != d.Splitter {
		f.Width, f.Height, f.Left, f.Top, f.Maximized, f.Splitter = &v.Width, &v.Height, &v.Left, &v.Top, &v.Maximized, &v.Splitter
	}
	return marshalWithExtra(f, v.extra)
}

// parseFont reads the original's "<family>, <size>pt"; anything else gives
// the defaults.
func parseFont(s string) (string, float32) {
	i := strings.LastIndex(s, ",")
	if i <= 0 {
		return defaultFontFamily, defaultFontSize
	}
	family := strings.TrimSpace(s[:i])
	size, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s[i+1:]), "pt"), 32)
	if family == "" || err != nil || size < 1 || size > 72 {
		return defaultFontFamily, defaultFontSize
	}
	return family, float32(size)
}

// SetLogViewer stores v, keeping the original-only fields read from disk.
func (c *Config) SetLogViewer(v LogViewer) {
	cur := c.LogViewer
	v.font, v.readFamily, v.readSize, v.extra = cur.font, cur.readFamily, cur.readSize, cur.extra
	c.LogViewer = v
}
