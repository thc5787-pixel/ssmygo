// Package ui is ssmygo's MyGo interface: the tray and the Edit Servers
// window. Only this package and cmd/ import MyGo.
package ui

import mui "github.com/egoist/mygo/ui"

// Compact applies MyGo's compact density (Spacing 3) to the window, the
// closest to WinForms rows that keeps check boxes usable (spike-findings).
func Compact(c *mui.Context) {
	t := *c.Theme()
	t.Spacing = 3
	c.SetTheme(&t)
}
