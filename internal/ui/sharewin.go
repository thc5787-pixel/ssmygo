package ui

import (
	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/qrcode"
)

// shareView is the Share Server Config window (original ServerSharingView):
// a QR code and the server list side by side, the ss:// link below.
type shareView struct {
	servers []config.Server
	sel     int
	link    string
	qr      []byte // PNG of link
	bmp     *mui.Bitmap
	copy    func(string)
}

func newShareView(cfg *config.Config) *shareView {
	v := &shareView{servers: cfg.Configs, sel: -1}
	if len(v.servers) > 0 {
		v.choose(min(max(cfg.Index, 0), len(v.servers)-1))
	}
	return v
}

// choose selects server i and renders its link as a QR code (once per
// selection, not per frame).
func (v *shareView) choose(i int) {
	if i == v.sel {
		return
	}
	v.sel = i
	v.link = config.FormatURL(v.servers[i])
	v.qr, _ = qrcode.Encode(v.link, 512) // black on white with a quiet zone, in both themes
	v.bmp, _ = mui.DecodeBitmap(v.qr)
}

func (v *shareView) View(c *mui.Context) {
	Compact(c)
	t := c.Theme()
	mui.Column(c).Fill().Padding(t.Space(3)).Gap(t.Space(2)).Children(func() {
		mui.Row(c).Grow(1).Gap(t.Space(3)).AlignItems(mui.Stretch).Children(func() {
			mui.Box(c).Grow(1).Basis(0).Children(func() {
				if v.bmp != nil {
					mui.Image(c, v.bmp).Fill().Fit(mui.Contain)
				}
			})
			mui.Column(c).Grow(1).Basis(0).Border(1, t.Border).Radius(t.Radius).Children(func() {
				mui.List(c, nil, len(v.servers), func(i int) {
					r := mui.Row(c).Padding(t.Space(1), t.Space(2)).Children(func() {
						mui.Text(c, v.servers[i].Title()).SingleLine().MinWidth(0)
					})
					if i == v.sel {
						r.Background(t.Accent.Alpha(0.15))
					}
					if r.Clicked() {
						v.choose(i)
					}
				}).Grow(1)
			})
		})
		mui.Row(c).Gap(t.Space(2)).Children(func() {
			mui.TextInput(c, &v.link).ReadOnly(true).Grow(1)
			if mui.Button(c, wpf("resx.Copy")).Disabled(v.sel < 0).Clicked() && v.copy != nil {
				v.copy(v.link)
			}
		})
	})
}

var shareWin *mygo.Window

// OpenShareWindow shows the Share Server Config window (one at a time).
func OpenShareWindow(ctrl *controller.Controller) {
	if shareWin != nil && !shareWin.IsDestroyed() {
		shareWin.Show()
		shareWin.Focus()
		return
	}
	v := newShareView(ctrl.Config())
	v.copy = func(s string) { mygo.Clipboard.WriteText(s) }
	win := mygo.NewWindow(mygo.WindowOptions{Title: wpf("resx.ServerSharing"), Width: 660, Height: 400,
		MinWidth: 660, MinHeight: 400, Content: mui.View(v.View)})
	win.OnClosed(func() { shareWin = nil })
	shareWin = win
}
