// Package ui is ssmygo's MyGo interface: the tray and the windows. Only
// this package and cmd/ import MyGo.
package ui

import mui "github.com/egoist/mygo/ui"

// win10Theme is the look of Windows 10's own apps (the Settings app):
// Segoe UI 14 px, square corners, the system blue accent, gray buttons on a
// white page, 2 px field borders. dark follows the system's dark mode.
func win10Theme(dark bool) *mui.Theme {
	if dark {
		return &mui.Theme{
			Dark:           true,
			Background:     mui.Hex("#1f1f1f"),
			Surface:        mui.Hex("#333333"),
			SurfaceHover:   mui.Hex("#454545"),
			SurfacePressed: mui.Hex("#595959"),
			Border:         mui.Hex("#767676"),
			Text:           mui.Hex("#ffffff"),
			TextMuted:      mui.Hex("#a6a6a6"),
			Accent:         mui.Hex("#0078d7"),
			AccentHover:    mui.Hex("#1a86d9"),
			AccentPressed:  mui.Hex("#005a9e"),
			AccentText:     mui.Hex("#ffffff"),
			Danger:         mui.Hex("#f1707b"),
			Warning:        mui.Hex("#fce100"),
			Success:        mui.Hex("#6ccb5f"),
			Selection:      mui.RGBA(0, 120, 215, 0.45),
			Focus:          mui.Hex("#ffffff"),
			Scrollbar:      mui.RGBA(255, 255, 255, 0.4),
			ScrollbarWidth: 6,
			Radius:         0,
			Spacing:        4,
			FontSize:       14,
			Font:           "Segoe UI",
		}
	}
	return &mui.Theme{
		Background:     mui.Hex("#ffffff"),
		Surface:        mui.Hex("#cccccc"),
		SurfaceHover:   mui.Hex("#b8b8b8"),
		SurfacePressed: mui.Hex("#999999"),
		Border:         mui.Hex("#999999"),
		Text:           mui.Hex("#000000"),
		TextMuted:      mui.Hex("#666666"),
		Accent:         mui.Hex("#0078d7"),
		AccentHover:    mui.Hex("#006cc1"),
		AccentPressed:  mui.Hex("#005a9e"),
		AccentText:     mui.Hex("#ffffff"),
		Danger:         mui.Hex("#e81123"),
		Warning:        mui.Hex("#9d5d00"),
		Success:        mui.Hex("#107c10"),
		Selection:      mui.RGBA(0, 120, 215, 0.4),
		Focus:          mui.Hex("#000000"),
		Scrollbar:      mui.RGBA(0, 0, 0, 0.4),
		ScrollbarWidth: 6,
		Radius:         0,
		Spacing:        4,
		FontSize:       14,
		Font:           "Segoe UI",
	}
}

// fieldTheme is t for text fields and drop-downs: on Windows 10 they are
// page-colored with a border, while buttons keep the gray surface.
func fieldTheme(t *mui.Theme) *mui.Theme {
	f := *t
	f.Surface, f.SurfaceHover, f.SurfacePressed = t.Background, t.Background, t.Background
	return &f
}

// navBackground is the Settings app's left pane color.
func navBackground(t *mui.Theme) mui.Color {
	if t.Dark {
		return mui.Hex("#2b2b2b")
	}
	return mui.Hex("#f2f2f2")
}

// Compact applies the Windows 10 theme to the window (light or dark as the
// system is). The name predates the theme: it was MyGo's compact density.
func Compact(c *mui.Context) {
	c.SetTheme(win10Theme(c.Theme().Dark))
}

// fields builds fn with the Windows 10 field look: page-colored text fields
// and drop-downs with a border (buttons outside keep the gray surface).
func fields(c *mui.Context, fn func()) mui.Element {
	return mui.Themed(c, fieldTheme(c.Theme())).Background(mui.Transparent).Children(fn)
}
