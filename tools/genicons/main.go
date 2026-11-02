// Command genicons renders assets/shadowsocks.svg into the app icon and the
// tray icons with MyGo's own SVG renderer (headless). Run via
// `go generate ./internal/ui`; the outputs are committed.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"

	mui "github.com/egoist/mygo/ui"
)

// strokeWidth makes the 48-unit outline legible at 16 px (spike preview).
const strokeWidth = "2.5"

type tray struct {
	mode        string
	light, dark string
}

// Tray colours: the original SelectColorMask intent, opaque.
var trays = []tray{
	{"global", "#197DBF", "#197DBF"},
	{"rules", "#404040", "#FFFFFF"},
	{"disabled", "#A0A0A0", "#969696"},
}

func main() {
	svgPath := flag.String("svg", "assets/shadowsocks.svg", "source SVG")
	app := flag.String("app", "assets/icon.png", "app icon output (1024×1024)")
	trayDir := flag.String("tray", "internal/ui/icons", "tray icon output directory (32×32)")
	flag.Parse()

	raw, err := os.ReadFile(*svgPath)
	if err != nil {
		log.Fatal(err)
	}
	src := string(raw)
	const anchor = "stroke-linejoin:round;"
	if !strings.Contains(src, anchor) {
		log.Fatalf("%s: style anchor %q not found", *svgPath, anchor)
	}
	svg := mui.MustParseSVG([]byte(strings.Replace(src, anchor, anchor+"stroke-width:"+strokeWidth+";", 1)))

	write(*app, render(svg, 1024, mui.Hex("#197DBF")))
	for _, tr := range trays {
		write(filepath.Join(*trayDir, fmt.Sprintf("tray-%s-light.png", tr.mode)), render(svg, 32, mui.Hex(tr.light)))
		write(filepath.Join(*trayDir, fmt.Sprintf("tray-%s-dark.png", tr.mode)), render(svg, 32, mui.Hex(tr.dark)))
	}
}

// render draws svg filling size×size in col with a transparent background.
// The headless Tester always paints an opaque theme background, so the icon
// is drawn on black and on white: coverage = 1 - (white-black)/255, and the
// pixel is col with that alpha (exact colour, anti-aliased edges).
func render(svg *mui.SVG, size int, col mui.Color) *image.NRGBA {
	onBlack := renderOn(svg, size, col, mui.RGB(0, 0, 0))
	onWhite := renderOn(svg, size, col, mui.RGB(255, 255, 255))
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			b, w := onBlack.RGBAAt(x, y), onWhite.RGBAAt(x, y)
			diff := (int(w.R) - int(b.R) + int(w.G) - int(b.G) + int(w.B) - int(b.B)) / 3
			a := 255 - max(0, min(255, diff))
			if a > 0 {
				out.SetNRGBA(x, y, color.NRGBA{R: col.R, G: col.G, B: col.B, A: uint8(a)})
			}
		}
	}
	return out
}

func renderOn(svg *mui.SVG, size int, col, bg mui.Color) *image.RGBA {
	view := func(c *mui.Context) {
		t := *c.Theme()
		t.Background = bg
		c.SetTheme(&t)
		mui.Column(c).Fill().Background(bg).Draw(func(p *mui.Painter, r mui.Rect) {
			p.Icon(svg, r, col)
		})
	}
	tt := mui.NewTester(view, size, size)
	tt.Frame()
	return tt.Image()
}

func write(path string, img image.Image) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}
