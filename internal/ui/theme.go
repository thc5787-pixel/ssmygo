// Package ui is ssmygo's MyGo interface: the tray and the windows. Only
// this package and cmd/ import MyGo.
package ui

import mui "github.com/egoist/mygo/ui"

// classicTheme is the classic Windows 10 desktop look, as in Control Panel
// dialogs and the original shadowsocks-windows (WinForms): a #F0F0F0
// window, gray #E1E1E1 buttons with a #ADADAD border that turn light blue
// on hover, white fields with a #7A7A7A border, square corners, Segoe UI
// 9 pt (12 px), the system blue #0078D7 for selection and focus. Windows 10
// has no dark classic look; the dark variant keeps the same shapes.
func classicTheme(dark bool) *mui.Theme {
	if dark {
		return &mui.Theme{
			Dark:           true,
			Background:     mui.Hex("#202020"),
			Surface:        mui.Hex("#333333"),
			SurfaceHover:   mui.Hex("#1c3a52"),
			SurfacePressed: mui.Hex("#244b69"),
			Border:         mui.Hex("#5a5a5a"),
			Text:           mui.Hex("#ffffff"),
			TextMuted:      mui.Hex("#8c8c8c"),
			Accent:         mui.Hex("#0078d7"),
			AccentHover:    mui.Hex("#1a86d9"),
			AccentPressed:  mui.Hex("#005a9e"),
			AccentText:     mui.Hex("#ffffff"),
			Danger:         mui.Hex("#f1707b"),
			Warning:        mui.Hex("#fce100"),
			Success:        mui.Hex("#6ccb5f"),
			Selection:      mui.RGBA(0, 120, 215, 0.5),
			Focus:          mui.Hex("#0078d7"),
			Scrollbar:      mui.RGBA(255, 255, 255, 0.35),
			ScrollbarWidth: 8,
			Radius:         0,
			Spacing:        3,
			FontSize:       12,
			Font:           "Segoe UI",
		}
	}
	return &mui.Theme{
		Background:     mui.Hex("#f0f0f0"),
		Surface:        mui.Hex("#e1e1e1"),
		SurfaceHover:   mui.Hex("#e5f1fb"),
		SurfacePressed: mui.Hex("#cce4f7"),
		Border:         mui.Hex("#adadad"),
		Text:           mui.Hex("#000000"),
		TextMuted:      mui.Hex("#6d6d6d"),
		Accent:         mui.Hex("#0078d7"),
		AccentHover:    mui.Hex("#006cc1"),
		AccentPressed:  mui.Hex("#005a9e"),
		AccentText:     mui.Hex("#ffffff"),
		Danger:         mui.Hex("#e81123"),
		Warning:        mui.Hex("#9d5d00"),
		Success:        mui.Hex("#107c10"),
		Selection:      mui.RGBA(0, 120, 215, 0.35),
		Focus:          mui.Hex("#0078d7"),
		Scrollbar:      mui.RGBA(0, 0, 0, 0.35),
		ScrollbarWidth: 8,
		Radius:         0,
		Spacing:        3,
		FontSize:       12,
		Font:           "Segoe UI",
	}
}

// fieldColors are the classic text field's fill and border.
func fieldColors(t *mui.Theme) (fill, border mui.Color) {
	if t.Dark {
		return mui.Hex("#191919"), mui.Hex("#7a7a7a")
	}
	return mui.Hex("#ffffff"), mui.Hex("#7a7a7a")
}

// fieldTheme is t for text fields, drop-downs and list boxes: white with a
// darker border, while buttons keep the gray surface.
func fieldTheme(t *mui.Theme) *mui.Theme {
	f := *t
	fill, border := fieldColors(t)
	f.Surface, f.SurfaceHover, f.SurfacePressed, f.Border = fill, fill, fill, border
	return &f
}

// fields builds fn with the classic field look (see fieldTheme).
func fields(c *mui.Context, fn func()) mui.Element {
	return mui.Themed(c, fieldTheme(c.Theme())).Background(mui.Transparent).Children(fn)
}

// defaultButton is the window's default button (OK, Save): a normal gray
// button with the blue border Windows draws around the default button.
func defaultButton(c *mui.Context, label string) mui.Element {
	return mui.Button(c, label).Border(2, c.Theme().Accent)
}

// applyTheme applies the classic Windows 10 theme to the window (light or
// dark as the system is).
func applyTheme(c *mui.Context) {
	c.SetTheme(classicTheme(c.Theme().Dark))
}
