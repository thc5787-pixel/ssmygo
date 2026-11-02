package ui

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/qrcode"
)

func shareConfig() *config.Config {
	c := config.Default()
	c.Configs = []config.Server{
		{Server: "jp.example.com", ServerPort: 8388, Method: "aes-256-gcm", Password: "pw1", Remarks: "Tokyo", Timeout: 5},
		{Server: "hk.example.com", ServerPort: 443, Method: "chacha20-ietf-poly1305", Password: "pw2", Remarks: "HK",
			Plugin: "v2ray-plugin", PluginOpts: "mode=websocket", Group: "https://sub.example/s", Timeout: 5},
	}
	c.Index = 1
	return c
}

func newShare() (*shareView, *mui.Tester) {
	v := newShareView(shareConfig())
	return v, mui.NewTester(v.View, 660, 400)
}

func TestShareSelectsCurrent(t *testing.T) {
	v, _ := newShare()
	if v.sel != 1 || v.link != config.FormatURL(shareConfig().Configs[1]) {
		t.Fatalf("sel %d link %q", v.sel, v.link)
	}
}

func TestShareLinkMatchesSelection(t *testing.T) {
	v, tt := newShare()
	tt.Frame()
	tt.Click("Tokyo (jp.example.com:8388)")
	tt.Frame()
	if v.sel != 0 || v.link != config.FormatURL(shareConfig().Configs[0]) {
		t.Fatalf("sel %d link %q", v.sel, v.link)
	}
}

func TestShareQRDecodesToLink(t *testing.T) {
	v, _ := newShare()
	img, err := png.Decode(bytes.NewReader(v.qr))
	if err != nil {
		t.Fatal(err)
	}
	if got := qrcode.Decode(img); !slices.Equal(got, []string{v.link}) {
		t.Fatalf("QR says %q, link %q", got, v.link)
	}
}

func TestShareCopy(t *testing.T) {
	v, tt := newShare()
	var copied string
	v.copy = func(s string) { copied = s }
	tt.Frame()
	tt.Click("Copy")
	if copied != v.link {
		t.Fatalf("copied %q", copied)
	}
}

func TestShareSnapshots(t *testing.T) {
	dir := os.Getenv("SNAP_DIR")
	if dir == "" {
		t.Skip("SNAP_DIR unset")
	}
	for _, dark := range []bool{false, true} {
		_, tt := newShare()
		tt.SetDark(dark)
		tt.Frame()
		name := map[bool]string{false: "sharewin-light.png", true: "sharewin-dark.png"}[dark]
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, tt.Image())
		f.Close()
	}
}
