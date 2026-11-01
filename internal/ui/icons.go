package ui

import (
	"embed"

	"github.com/thc5787/ssmygo/internal/controller"
)

//go:generate go run ../../tools/genicons -svg ../../assets/shadowsocks.svg -app ../../resources/icon.png -tray icons

//go:embed icons/tray-*.png
var trayIcons embed.FS

// TrayIcon returns the pre-rendered 32×32 PNG for mode on a light or dark taskbar.
func TrayIcon(mode controller.Mode, darkTaskbar bool) []byte {
	name := map[controller.Mode]string{controller.Disabled: "disabled", controller.Rules: "rules", controller.Global: "global"}[mode]
	shade := "light"
	if darkTaskbar {
		shade = "dark"
	}
	b, err := trayIcons.ReadFile("icons/tray-" + name + "-" + shade + ".png")
	if err != nil {
		panic(err) // embedded at build time
	}
	return b
}
