package ui

import (
	"bytes"
	"embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/traffic"
)

//go:generate go run ../../tools/genicons -svg ../../assets/shadowsocks.svg -app ../../resources/icon.png -tray icons

//go:embed icons/tray-*.png
var trayIcons embed.FS

// Badge is the tray icon's traffic overlay, like the original's
// icon_in / icon_out / icon_both.
type Badge int

const (
	BadgeNone Badge = iota
	BadgeIn         // data received in the last second
	BadgeOut        // data sent in the last second
	BadgeBoth
)

// BadgeFor picks the badge for one second's traffic.
func BadgeFor(rate traffic.Sample) Badge {
	b := BadgeNone
	if rate.Down > 0 {
		b |= BadgeIn
	}
	if rate.Up > 0 {
		b |= BadgeOut
	}
	return b
}

type iconKey struct {
	mode  controller.Mode
	dark  bool
	badge Badge
}

var (
	iconMu    sync.Mutex
	iconCache = map[iconKey][]byte{}
)

// TrayIcon returns the 32×32 PNG for mode on a light or dark taskbar, with
// the traffic badge drawn over it. Results are cached.
func TrayIcon(mode controller.Mode, darkTaskbar bool, b Badge) []byte {
	k := iconKey{mode, darkTaskbar, b}
	iconMu.Lock()
	defer iconMu.Unlock()
	if p, ok := iconCache[k]; ok {
		return p
	}
	p := baseIcon(mode, darkTaskbar)
	if b != BadgeNone {
		p = withBadge(p, b, darkTaskbar)
	}
	iconCache[k] = p
	return p
}

func baseIcon(mode controller.Mode, darkTaskbar bool) []byte {
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

var (
	inColor  = color.NRGBA{0x2E, 0xA0, 0x43, 0xFF} // green, down arrow
	outColor = color.NRGBA{0xE0, 0x8A, 0x1E, 0xFF} // orange, up arrow
)

// withBadge draws a down arrow bottom-left (in) and/or an up arrow
// bottom-right (out), each 12×12 with a 1 px outline in the taskbar's
// shade so it reads on any icon colour.
func withBadge(base []byte, b Badge, darkTaskbar bool) []byte {
	src, err := png.Decode(bytes.NewReader(base))
	if err != nil {
		panic(err) // embedded at build time
	}
	img := image.NewNRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
	edge := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	if darkTaskbar {
		edge = color.NRGBA{0x20, 0x20, 0x20, 0xFF}
	}
	if b&BadgeIn != 0 {
		arrow(img, 0, 20, true, inColor, edge)
	}
	if b&BadgeOut != 0 {
		arrow(img, 20, 20, false, outColor, edge)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		panic(err)
	}
	return out.Bytes()
}

// arrow fills a 12×12 arrow at (x0, y0): a triangle head and a shaft,
// pointing down when down is set. The outline is the shape grown by 1 px.
func arrow(img *image.NRGBA, x0, y0 int, down bool, fill, edge color.NRGBA) {
	inside := func(x, y int) bool { // in 12×12 local coordinates, pointing up
		if x < 0 || x > 11 || y < 0 || y > 11 {
			return false
		}
		if y < 6 { // head: width grows from the tip at the top
			return x >= 5-y && x <= 6+y
		}
		return x >= 4 && x <= 7 // shaft
	}
	shape := func(x, y int) bool {
		if down {
			y = 11 - y
		}
		return inside(x, y)
	}
	for y := -1; y <= 12; y++ {
		for x := -1; x <= 12; x++ {
			px, py := x0+x, y0+y
			if !(image.Point{px, py}.In(img.Bounds())) {
				continue
			}
			switch {
			case shape(x, y):
				img.SetNRGBA(px, py, fill)
			case shape(x-1, y) || shape(x+1, y) || shape(x, y-1) || shape(x, y+1):
				img.SetNRGBA(px, py, edge)
			}
		}
	}
}
