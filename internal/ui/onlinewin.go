package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
)

// subSource is what the Online Config window needs from the controller.
type subSource interface {
	Subscriptions() []string
	SubscriptionResults() map[string]controller.SubResult
	AddSubscription(string) error
	RemoveSubscription(string) error
	UpdateSubscription(context.Context, string) (controller.SubResult, error)
	UpdateAllSubscriptions(context.Context) []string
	GroupSize(string) int
}

// onlineView is the Online Config window (original OnlineConfigView).
type onlineView struct {
	src      subSource
	urls     []string
	results  map[string]controller.SubResult
	sel      int
	input    string
	inputErr string
	busy     bool
	confirm  bool
	opErr    string // the last action's error
	// run executes work off the main thread (with the plugin trust dialog)
	// and then done on it; nil runs both at once (tests).
	run  func(work func() error, done func(error))
	copy func(string)
}

func newOnlineView(src subSource) *onlineView {
	v := &onlineView{src: src, sel: -1}
	v.refresh()
	return v
}

func (v *onlineView) refresh() {
	v.urls, v.results = v.src.Subscriptions(), v.src.SubscriptionResults()
	if v.sel >= len(v.urls) {
		v.sel = len(v.urls) - 1
	}
}

// do runs work in the background with the buttons disabled, and shows
// its error.
func (v *onlineView) do(work func() error) {
	if v.busy {
		return
	}
	v.busy, v.opErr = true, ""
	run := v.run
	if run == nil {
		run = func(w func() error, d func(error)) { d(w()) }
	}
	run(work, func(err error) {
		v.busy = false
		if err != nil {
			v.opErr = Message(err)
		}
		v.refresh()
	})
}

func (v *onlineView) update(u string) func() error {
	return func() error { _, err := v.src.UpdateSubscription(context.Background(), u); return err }
}

func (v *onlineView) updateAll() error {
	n := len(v.src.Subscriptions())
	if failed := v.src.UpdateAllSubscriptions(context.Background()); len(failed) > 0 {
		return &controller.SubscriptionsFailedError{Failed: len(failed), Total: n}
	}
	return nil
}

func (v *onlineView) selected() (string, bool) {
	if v.sel < 0 || v.sel >= len(v.urls) {
		return "", false
	}
	return v.urls[v.sel], true
}

func (v *onlineView) add() {
	u := v.input
	if err := v.src.AddSubscription(u); err != nil {
		switch {
		case errors.Is(err, controller.ErrBadSubscriptionURL):
			v.inputErr = i18n.T("ssmygo.badSubURL")
		case errors.Is(err, controller.ErrDuplicateSubscription):
			v.inputErr = i18n.T("ssmygo.dupSub")
		default:
			v.inputErr = Message(err)
		}
		return
	}
	v.input, v.inputErr = "", ""
	v.refresh()
	v.sel = len(v.urls) - 1
	v.do(v.update(u))
}

func (v *onlineView) confirmRemove() {
	v.confirm = false
	if u, ok := v.selected(); ok {
		v.do(func() error { return v.src.RemoveSubscription(u) })
	}
}

// View lays out the original OnlineConfigView: the list with buttons on
// the right, the URL field below.
func (v *onlineView) View(c *mui.Context) {
	T := wpf
	Compact(c)
	t := c.Theme()
	cur, hasSel := v.selected()
	mui.Column(c).Fill().Padding(t.Space(3)).Gap(t.Space(2)).Children(func() {
		mui.Row(c).Grow(1).Gap(t.Space(2)).AlignItems(mui.Stretch).Children(func() {
			mui.Column(c).Grow(1).Border(1, t.Border).Radius(t.Radius).Children(func() {
				mui.List(c, nil, len(v.urls), func(i int) { v.row(c, i) }).Grow(1)
			})
			mui.Column(c).Gap(t.Space(2)).Children(func() {
				if mui.Button(c, T("resx.updateButton_Content")).Disabled(v.busy || !hasSel).Clicked() {
					v.do(v.update(cur))
				}
				if mui.Button(c, T("resx.updateAllButton_Content")).Disabled(v.busy || len(v.urls) == 0).Clicked() {
					v.do(v.updateAll)
				}
				if mui.Button(c, T("resx.copyLinkButton_Content")).Disabled(!hasSel).Clicked() && v.copy != nil {
					v.copy(cur)
				}
				mui.Box(c).Grow(1)
				if mui.Button(c, T("resx.removeButton_Content")).Disabled(v.busy || !hasSel).Clicked() {
					v.confirm = true
				}
			})
		})
		mui.Row(c).Gap(t.Space(2)).AlignItems(mui.Center).Children(func() {
			mui.Text(c, "URL")
			var in mui.Element
			fields(c, func() { in = mui.TextInput(c, &v.input) }).Grow(1)
			if v.inputErr != "" {
				in.Border(1, t.Danger).Tooltip(v.inputErr)
			}
			if mui.Button(c, T("resx.addButton_Content")).Disabled(v.busy).Clicked() || in.Submitted() {
				v.add()
			}
		})
		if v.inputErr != "" {
			mui.Text(c, v.inputErr).TextColor(t.Danger)
		}
		if v.opErr != "" {
			mui.Text(c, v.opErr).TextColor(t.Danger)
		}
	})
	mui.Modal(c, &v.confirm, func() {
		mui.Text(c, fmt.Sprintf(T("ssmygo.removeSub"), v.src.GroupSize(cur)))
		mui.Row(c).Gap(t.Space(2)).Justify(mui.End).Children(func() {
			if mui.Button(c, T("Cancel")).Clicked() {
				v.confirm = false
			}
			if defaultButton(c, T("resx.removeButton_Content")).Clicked() {
				v.confirmRemove()
			}
		})
	})
}

// wpf is i18n.T without the WPF access-key underscore ("_Add" → "Add").
func wpf(key string) string { return strings.Replace(i18n.T(key), "_", "", 1) }

func (v *onlineView) row(c *mui.Context, i int) {
	t := c.Theme()
	u := v.urls[i]
	r := mui.Row(c).Gap(t.Space(2)).Padding(t.Space(1), t.Space(2)).AlignItems(mui.Center).Children(func() {
		mui.Text(c, u).SingleLine().MinWidth(0).Grow(1).Shrink(1)
		if res, ok := v.results[u]; ok {
			if res.Err != nil {
				mui.Text(c, "⚠").TextColor(t.Danger).Tooltip(res.Err.Error())
			} else {
				mui.Text(c, fmt.Sprintf("✓ %d", res.Servers)).TextColor(t.TextMuted)
			}
		}
	})
	if i == v.sel {
		r.Background(t.Accent.Alpha(0.15))
	}
	if r.Clicked() {
		v.sel = i
	}
}

var onlineWin *mygo.Window

// OpenOnlineConfigWindow shows the Online Config window (one at a time).
func OpenOnlineConfigWindow(ctrl *controller.Controller) {
	if onlineWin != nil && !onlineWin.IsDestroyed() {
		onlineWin.Show()
		onlineWin.Focus()
		return
	}
	v := newOnlineView(ctrl)
	win := mygo.NewWindow(mygo.WindowOptions{Title: wpf("resx.OnlineConfigDelivery"), Width: 480, Height: 510,
		MinWidth: 400, MinHeight: 300, Content: mui.View(v.View)})
	v.run = func(work func() error, done func(error)) {
		runActionWith(ctrl.TrustPlugin, nil, work, func(err error) { win.Update(func() { done(err) }) })
	}
	v.copy = func(s string) { mygo.Clipboard.WriteText(s) }
	off := ctrl.Subscribe(func(controller.State) { win.Update(v.refresh) })
	win.OnClosed(func() { off(); onlineWin = nil })
	onlineWin = win
}
