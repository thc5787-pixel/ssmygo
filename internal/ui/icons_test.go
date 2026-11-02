package ui

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/thc5787/ssmygo/internal/controller"
)

// dominant returns the most frequent opaque colour of a PNG.
func dominant(t *testing.T, b []byte) (img image.Image, r, g, bl uint8) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	count := map[[3]uint8]int{}
	bd := img.Bounds()
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		for x := bd.Min.X; x < bd.Max.X; x++ {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			if ca>>8 == 0xFF {
				count[[3]uint8{uint8(cr >> 8), uint8(cg >> 8), uint8(cb >> 8)}]++
			}
		}
	}
	best, n := [3]uint8{}, 0
	for k, v := range count {
		if v > n {
			best, n = k, v
		}
	}
	if n == 0 {
		t.Fatal("no opaque pixels")
	}
	return img, best[0], best[1], best[2]
}

func near(a, b uint8) bool { return a-b <= 3 || b-a <= 3 }

func TestTrayIconColors(t *testing.T) {
	want := map[controller.Mode][2][3]uint8{
		controller.Global:   {{0x19, 0x7D, 0xBF}, {0x19, 0x7D, 0xBF}},
		controller.Rules:    {{0x40, 0x40, 0x40}, {0xFF, 0xFF, 0xFF}},
		controller.Disabled: {{0xA0, 0xA0, 0xA0}, {0x96, 0x96, 0x96}},
	}
	for mode, cols := range want {
		for i, dark := range []bool{false, true} {
			img, r, g, b := dominant(t, TrayIcon(mode, dark, BadgeNone))
			if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
				t.Errorf("%v dark=%v: size %v", mode, dark, img.Bounds())
			}
			c := cols[i]
			if !near(r, c[0]) || !near(g, c[1]) || !near(b, c[2]) {
				t.Errorf("%v dark=%v: colour %02x%02x%02x want %02x%02x%02x", mode, dark, r, g, b, c[0], c[1], c[2])
			}
		}
	}
}

func TestAppIcon(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "resources", "icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	img, _, _, _ := dominant(t, b)
	if img.Bounds().Dx() != 1024 || img.Bounds().Dy() != 1024 {
		t.Fatalf("size %v", img.Bounds())
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatal("corner not transparent")
	}
}

func TestGenerateUpToDate(t *testing.T) {
	out := t.TempDir()
	cmd := exec.Command("go", "run", "../../tools/genicons", "-svg", "../../assets/shadowsocks.svg", "-app", filepath.Join(out, "icon.png"), "-tray", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	pairs := map[string]string{filepath.Join(out, "icon.png"): filepath.Join("..", "..", "resources", "icon.png")}
	ents, _ := filepath.Glob(filepath.Join("icons", "tray-*.png"))
	if len(ents) != 6 {
		t.Fatalf("committed tray icons: %v", ents)
	}
	for _, e := range ents {
		pairs[filepath.Join(out, filepath.Base(e))] = e
	}
	for gen, committed := range pairs {
		a, _ := os.ReadFile(gen)
		b, _ := os.ReadFile(committed)
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs from a fresh generation; run go generate ./internal/ui", committed)
		}
	}
}

func TestTrayIconBadgesDiffer(t *testing.T) {
	seen := map[string]Badge{}
	for _, b := range []Badge{BadgeNone, BadgeIn, BadgeOut, BadgeBoth} {
		png1 := TrayIcon(controller.Global, false, b)
		img, err := png.Decode(bytes.NewReader(png1))
		if err != nil || img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
			t.Fatalf("badge %v: %v %v", b, err, img.Bounds())
		}
		if prev, dup := seen[string(png1)]; dup {
			t.Fatalf("badge %v renders like %v", b, prev)
		}
		seen[string(png1)] = b
		if again := TrayIcon(controller.Global, false, b); &again[0] != &png1[0] {
			t.Fatalf("badge %v not cached", b)
		}
	}
}
