package qrcode

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand"
	"slices"
	"testing"
)

var links = []string{
	"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@ss.example.com:8388#Tokyo",
	"ss://MjAyMi1ibGFrZTMtYWVzLTI1Ni1nY206QUFFQ0F3UUZCZ2NJQ1FvTERBME9EeEFSRWhNVUZSWVhHQmthR3h3ZEhoOD0@[2001:db8::1]:443#SS2022%20key",
	"ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwdw@h.example:80/?plugin=v2ray-plugin%3Bmode%3Dwebsocket%3Bhost%3Dcdn.example#ws",
}

func decodePNG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestRoundTrip(t *testing.T) {
	for _, l := range links {
		b, err := Encode(l, 300)
		if err != nil {
			t.Fatal(err)
		}
		if got := Decode(decodePNG(t, b)); !slices.Equal(got, []string{l}) {
			t.Errorf("%q → %q", l, got)
		}
	}
}

// screenshot draws each code, scaled by factor, into a noisy 1920×1080 image.
func screenshot(t *testing.T, factor float64, texts ...string) image.Image {
	t.Helper()
	dst := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	r := rand.New(rand.NewSource(1))
	for i := range dst.Pix {
		dst.Pix[i] = uint8(180 + r.Intn(60))
	}
	for i, text := range texts {
		b, err := Encode(text, 0)
		if err != nil {
			t.Fatal(err)
		}
		src := decodePNG(t, b)
		w := int(float64(src.Bounds().Dx()) * factor)
		x0, y0 := 200+i*800, 200
		for y := 0; y < w; y++ {
			for x := 0; x < w; x++ {
				c := src.At(int(float64(x)/factor), int(float64(y)/factor))
				dst.Set(x0+x, y0+y, c)
			}
		}
	}
	_ = draw.Src
	_ = color.Black
	return dst
}

func TestDecodeInScreenshot(t *testing.T) {
	for _, f := range []float64{1, 1.5, 2} {
		if got := Decode(screenshot(t, f, links[0])); !slices.Equal(got, []string{links[0]}) {
			t.Errorf("scale %v: %q", f, got)
		}
	}
}

func TestDecodeMultiple(t *testing.T) {
	got := Decode(screenshot(t, 1, links[0], links[2]))
	slices.Sort(got)
	want := []string{links[0], links[2]}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
}

func TestDecodeNone(t *testing.T) {
	if got := Decode(image.NewRGBA(image.Rect(0, 0, 800, 600))); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}
