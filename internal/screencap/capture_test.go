package screencap

import (
	"os"
	"testing"
)

func TestCaptureAll(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("CI runners have no visible desktop to capture")
	}
	imgs, err := CaptureAll()
	if err != nil {
		t.Skipf("no interactive desktop: %v", err)
	}
	if len(imgs) == 0 {
		t.Fatal("no displays captured")
	}
	b := imgs[0].Bounds()
	if b.Dx() < 640 || b.Dy() < 480 {
		t.Fatalf("size %v", b)
	}
	first := imgs[0].At(b.Min.X, b.Min.Y)
	r0, g0, b0, _ := first.RGBA()
	for y := b.Min.Y; y < b.Max.Y; y += 7 {
		for x := b.Min.X; x < b.Max.X; x += 7 {
			if r, g, bl, _ := imgs[0].At(x, y).RGBA(); r != r0 || g != g0 || bl != b0 {
				return
			}
		}
	}
	t.Fatal("capture is one solid colour")
}
