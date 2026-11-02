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
	FontSize     float32 `json:"-"`        // points
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
	return LogViewer{FontFamily: defaultFontFamily, FontSize: defaultFontSize, Splitter: 0.7}
}

// logViewerFields is the JSON shape. ssmygo's own keys are pointers so a
// section the user never changed round-trips byte for byte.
type logViewerFields struct {
	TopMost      bool     `json:"topMost"`
	WrapText     bool     `json:"wrapText"`
	ToolbarShown bool     `json:"toolbarShown"`
	Font         string   `json:"Font,omitempty"`
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
	// The window's size and position moved to Config.WindowStates (MyGo's
	// window state): drop the keys earlier versions wrote here.
	for _, k := range []string{"width", "height", "left", "top", "maximized"} {
		delete(extra, k)
	}
	*v = LogViewer{TopMost: f.TopMost, WrapText: f.WrapText, ToolbarShown: f.ToolbarShown,
		Splitter: deref(f.Splitter, d.Splitter), font: f.Font, extra: extra}
	v.FontFamily, v.FontSize = parseFont(f.Font)
	v.readFamily, v.readSize = v.FontFamily, v.FontSize
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
	if v.Splitter != DefaultLogViewer().Splitter {
		f.Splitter = &v.Splitter
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
