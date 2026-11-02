// Package qrcode makes QR codes for sharing servers and reads them from
// screenshots (pure Go: go-qrcode to encode, gozxing to decode).
package qrcode

import (
	"image"

	"github.com/makiuchi-d/gozxing"
	multiqr "github.com/makiuchi-d/gozxing/multi/qrcode"
	goqr "github.com/skip2/go-qrcode"
)

// Encode returns text as a PNG QR code, black on white with the standard
// quiet zone, medium error correction. px is the image width; 0 or less
// uses 4 pixels per module.
func Encode(text string, px int) ([]byte, error) {
	q, err := goqr.New(text, goqr.Medium)
	if err != nil {
		return nil, err
	}
	if px <= 0 {
		px = -4
	}
	return q.PNG(px)
}

// Decode returns the text of every QR code it finds in img, in no
// particular order. Small codes get a second try on a 2× enlargement.
func Decode(img image.Image) []string {
	if got := decode(img); len(got) > 0 {
		return got
	}
	if b := img.Bounds(); b.Dx() <= 4096 && b.Dy() <= 4096 {
		return decode(upscale2(img))
	}
	return nil
}

func decode(img image.Image) []string {
	bmp, err := gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(gozxing.NewLuminanceSourceFromImage(img)))
	if err != nil {
		return nil
	}
	hints := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}
	results, err := multiqr.NewQRCodeMultiReader().DecodeMultiple(bmp, hints)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range results {
		if t := r.GetText(); !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// upscale2 doubles img with nearest-neighbour sampling.
func upscale2(img image.Image) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*2, b.Dy()*2))
	for y := 0; y < b.Dy()*2; y++ {
		for x := 0; x < b.Dx()*2; x++ {
			dst.Set(x, y, img.At(b.Min.X+x/2, b.Min.Y+y/2))
		}
	}
	return dst
}
