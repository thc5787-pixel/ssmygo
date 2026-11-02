package ui

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
)

// ForwardWindow is the Forward Proxy window (original ForwardProxyView, 280×400; 320×520 here for labels above fields).
type ForwardWindow struct {
	save    func(config.ForwardProxy) error
	typ     string // "none", "socks5", "http"
	addr    string
	port    string
	timeout string
	user    string
	pwd     string
	saveErr string
	onClose func()
	saving  bool // a save is in flight
	// run executes the save; nil = synchronously. The real window runs it off
	// the main thread (a commit may wait for the current plugin).
	run func(work func() error, done func(error))
}

// NewForwardWindow edits a copy of initial.
func NewForwardWindow(save func(config.ForwardProxy) error, initial config.ForwardProxy) *ForwardWindow {
	w := &ForwardWindow{save: save, typ: "none", addr: initial.ProxyServer, timeout: strconv.Itoa(initial.ProxyTimeout),
		user: initial.AuthUser, pwd: initial.AuthPwd}
	if initial.ProxyPort != 0 {
		w.port = strconv.Itoa(initial.ProxyPort)
	}
	if initial.UseProxy {
		w.typ = map[int]string{config.ProxySOCKS5: "socks5", config.ProxyHTTP: "http"}[initial.ProxyType]
	}
	return w
}

// OnClose is called after Save or Cancel; the owner closes the window.
func (w *ForwardWindow) OnClose(fn func()) { w.onClose = fn }

func (w *ForwardWindow) close() {
	if w.onClose != nil {
		w.onClose()
	}
}

// proxy builds the settings and their problems; port and timeout errors
// come first, then config.ForwardProxy.Validate.
func (w *ForwardWindow) proxy() (config.ForwardProxy, []config.FieldError) {
	p := config.ForwardProxy{UseProxy: w.typ != "none", ProxyServer: strings.TrimSpace(w.addr),
		AuthUser: w.user, AuthPwd: w.pwd, ProxyType: config.ProxySOCKS5}
	// As the original: using credentials follows from filling them in.
	p.UseAuth = strings.TrimSpace(w.user) != "" || w.pwd != ""
	if w.typ == "http" {
		p.ProxyType = config.ProxyHTTP
	}
	var errs []config.FieldError
	var err error
	if p.ProxyPort, err = strconv.Atoi(strings.TrimSpace(w.port)); err != nil && p.UseProxy {
		errs = append(errs, config.FieldError{Field: "proxyPort", Msg: "1–65535"})
	}
	if p.ProxyTimeout, err = strconv.Atoi(strings.TrimSpace(w.timeout)); err != nil {
		p.ProxyTimeout = config.DefaultProxyTimeout
		if p.UseProxy {
			errs = append(errs, config.FieldError{Field: "proxyTimeout", Msg: "1–10 seconds."})
		}
	}
	if p.UseProxy && (strings.TrimSpace(w.user) == "") != (w.pwd == "") {
		errs = append(errs, config.FieldError{Field: "authPwd", Msg: "Enter both user name and password."})
	}
	for _, fe := range p.Validate() {
		if !hasField(errs, fe.Field) {
			errs = append(errs, fe)
		}
	}
	return p, errs
}

// View lays out the original ForwardProxyView.
func (w *ForwardWindow) View(c *mui.Context) {
	T := i18n.T
	Compact(c)
	t := c.Theme()
	if c.Shortcut(0, mui.KeyEscape) {
		w.close()
		return
	}
	p, errs := w.proxy()
	off := w.typ == "none"
	mui.Column(c).Fill().Padding(t.Space(3)).Gap(t.Space(2)).Children(func() {
		mui.Scroll(c).Grow(1).Children(func() {
			mui.Text(c, T("resx.Type")).Bold()
			mui.RadioGroup(c, func() {
				mui.Radio(c, &w.typ, "none", T("resx.NoProxy"))
				mui.Radio(c, &w.typ, "socks5", T("SOCKS5"))
				mui.Radio(c, &w.typ, "http", T("HTTP"))
			}).Label(T("resx.Type"))
			fields(c, func() {
				mui.Fieldset(c, T("resx.Details"), func() {
					mui.Field(c, T("resx.Address"), func() { mui.TextInput(c, &w.addr).Disabled(off) }).Error(errMsg(errs, "proxyServer"))
					mui.Field(c, T("resx.Port"), func() { mui.TextInput(c, &w.port).Disabled(off) }).Error(errMsg(errs, "proxyPort"))
					mui.Field(c, T("resx.Timeout"), func() { mui.TextInput(c, &w.timeout).Disabled(off) }).Error(errMsg(errs, "proxyTimeout"))
				})
				mui.Fieldset(c, T("resx.CredentialsOptional"), func() {
					mui.Field(c, T("resx.Username"), func() { mui.TextInput(c, &w.user).Disabled(off) }).Error(errMsg(errs, "authUser"))
					mui.Field(c, T("Password"), func() { mui.TextInput(c, &w.pwd).Password().Disabled(off) }).Error(errMsg(errs, "authPwd"))
				})
			})
		})
		if w.saveErr != "" {
			mui.Text(c, w.saveErr).TextColor(t.Danger)
		}
		mui.Row(c).Gap(t.Space(2)).Justify(mui.End).Children(func() {
			if mui.PrimaryButton(c, T("Save")).MinWidth(t.Rem(5)).Disabled(w.saving).Clicked() || c.Shortcut(0, mui.KeyEnter) {
				w.apply(p, errs)
			}
			if mui.Button(c, T("Cancel")).MinWidth(t.Rem(5)).Clicked() {
				w.close()
			}
		})
	})
}

// apply saves when valid (field errors are already on screen) and closes
// the window on success.
func (w *ForwardWindow) apply(p config.ForwardProxy, errs []config.FieldError) {
	if len(errs) > 0 || w.saving {
		return
	}
	run := w.run
	if run == nil {
		run = func(work func() error, done func(error)) { done(work()) }
	}
	w.saving = true
	run(func() error { return w.save(p) }, func(err error) {
		w.saving = false
		if err != nil {
			w.saveErr = Message(err)
			return
		}
		w.saveErr = ""
		w.close()
	})
}

var forwardWin *mygo.Window

// OpenForwardWindow shows the Forward Proxy window (lazy, destroyed on close).
func OpenForwardWindow(ctrl *controller.Controller) {
	if forwardWin != nil && !forwardWin.IsDestroyed() {
		forwardWin.Show()
		forwardWin.Focus()
		return
	}
	w := NewForwardWindow(ctrl.SetForwardProxy, ctrl.Config().Proxy)
	win := mygo.NewWindow(mygo.WindowOptions{
		Title: i18n.T("resx.ForwardProxy"), Width: 320, Height: 520, MinWidth: 300, MinHeight: 460,
		Content: mui.View(w.View),
	})
	w.run = func(work func() error, done func(error)) {
		runActionWith(ctrl.TrustPlugin, nil, work, func(err error) { win.Update(func() { done(err) }) })
	}
	w.OnClose(win.Close)
	win.OnClosed(func() { forwardWin = nil })
	forwardWin = win
}
