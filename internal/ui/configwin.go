package ui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
	"github.com/thc5787/ssmygo/internal/subscribe"
)

// SaveFunc applies the edited servers (controller.SaveServers).
type SaveFunc func(servers []config.Server, index, port int, portable bool) error

// formFields are what the form edits, numbers as text; comparable, so a
// server can be told apart from its state when the window opened.
type formFields struct {
	Server, Port, Password, Method, Plugin, PluginOpts, PluginArgs, Remarks, Timeout, Group string
	NeedArgs                                                                                bool
}

// editServer is one server in the window.
type editServer struct {
	formFields
	base  formFields    // as loaded: unchanged servers are passed back untouched
	isNew bool          // added in this window
	orig  config.Server // keeps unknown JSON fields
}

func fromServer(s config.Server) editServer {
	f := formFields{
		Server: s.Server, Port: strconv.Itoa(s.ServerPort), Password: s.Password, Method: s.Method,
		Plugin: s.Plugin, PluginOpts: s.PluginOpts, PluginArgs: s.PluginArgs, Remarks: s.Remarks,
		Timeout: strconv.Itoa(s.Timeout), Group: s.Group, NeedArgs: s.PluginArgs != "",
	}
	return editServer{formFields: f, base: f, orig: s}
}

func newEditServer() editServer {
	e := fromServer(config.Server{ServerPort: 8388, Method: config.DefaultMethod, Timeout: config.DefaultTimeout})
	e.isNew = true
	return e
}

// dirty reports whether the user added or changed this server.
func (e editServer) dirty() bool { return e.isNew || e.formFields != e.base }

// result is what OK saves: the loaded server itself when untouched, so an
// invalid server from an old config survives unchanged (deferred #3).
func (e editServer) result() config.Server {
	if !e.dirty() {
		return e.orig
	}
	s, _ := e.server()
	return s
}

// server converts the form back, with every validation error.
func (e editServer) server() (config.Server, []config.FieldError) {
	s := e.orig
	s.Server, s.Password, s.Method = strings.TrimSpace(e.Server), e.Password, e.Method
	s.Plugin, s.PluginOpts, s.Remarks, s.Group = strings.TrimSpace(e.Plugin), e.PluginOpts, e.Remarks, e.Group
	s.PluginArgs = ""
	if e.NeedArgs && s.Plugin != "" {
		s.PluginArgs = e.PluginArgs
	}
	var errs []config.FieldError
	port, err := strconv.Atoi(strings.TrimSpace(e.Port))
	if err != nil {
		errs = append(errs, config.FieldError{Field: "server_port", Msg: "1–65535"})
	}
	s.ServerPort = port
	timeout := 0
	if t := strings.TrimSpace(e.Timeout); t != "" {
		if timeout, err = strconv.Atoi(t); err != nil {
			errs = append(errs, config.FieldError{Field: "timeout", Msg: "0–20 seconds."})
		}
	}
	s.Timeout = timeout
	for _, fe := range s.Validate() {
		if !hasField(errs, fe.Field) {
			errs = append(errs, fe)
		}
	}
	return s, errs
}

func hasField(errs []config.FieldError, f string) bool {
	for _, e := range errs {
		if e.Field == f {
			return true
		}
	}
	return false
}

func errMsg(errs []config.FieldError, f string) string {
	for _, e := range errs {
		if e.Field == f {
			return e.Msg
		}
	}
	return ""
}

// ConfigWindow is the Edit Servers window (original ConfigForm layout).
type ConfigWindow struct {
	save     SaveFunc
	servers  []editServer
	sel      int
	list     mui.ListState
	showPass bool
	port     string
	portable bool
	saveErr  string
	portBusy bool // the last save failed because port was taken
	saving   bool // a save is in flight (run is asynchronous in the real window)
	// run executes the save; nil = synchronously. The real window runs it off
	// the main thread with the plugin trust dialog (runActionWith).
	run      func(work func() error, done func(error))
	busyPort string // the port text at that moment; editing it clears portBusy
	onClose  func()
	sources  map[string]bool // subscription URLs: their servers are read-only
	// attempted: OK or Apply was pressed with errors; until then a new
	// server's untouched fields show none (as Windows forms do).
	attempted bool
}

// errsFor is what row e shows of errs: everything for loaded servers and
// after a failed OK, else only the fields the user changed.
func (w *ConfigWindow) errsFor(e editServer, errs []config.FieldError) []config.FieldError {
	if !e.isNew || w.attempted {
		return errs
	}
	changed := map[string]bool{
		"server": e.Server != e.base.Server, "server_port": e.Port != e.base.Port, "password": e.Password != e.base.Password,
		"method": e.Method != e.base.Method, "timeout": e.Timeout != e.base.Timeout,
	}
	var out []config.FieldError
	for _, fe := range errs {
		if changed[fe.Field] {
			out = append(out, fe)
		}
	}
	return out
}

// isSub: the server came from a subscription (C-D4) and is read-only here;
// the next update would overwrite any edit. A hand-typed group that is not
// a subscription (the original allowed it) stays the user's.
func (w *ConfigWindow) isSub(e editServer) bool { return !e.isNew && w.sources[e.orig.Group] }

func sourcesOf(cfg *config.Config) map[string]bool {
	m := map[string]bool{}
	for _, u := range cfg.OnlineConfigSource {
		m[u] = true
	}
	return m
}

// AppendServers adds servers imported while the window is open, so OK does
// not drop them (deferred #7). They are already saved, so not "new".
func (w *ConfigWindow) AppendServers(servers []config.Server) {
	for _, s := range servers {
		w.insertOwn(fromServer(s)) // above subscription servers
	}
}

// NewConfigWindow edits a copy of initial.
func NewConfigWindow(save SaveFunc, initial *config.Config) *ConfigWindow {
	w := &ConfigWindow{save: save, sel: initial.Index, port: strconv.Itoa(initial.LocalPort), portable: initial.PortableMode,
		sources: sourcesOf(initial)}
	for _, s := range initial.Configs {
		w.servers = append(w.servers, fromServer(s))
	}
	if w.sel < 0 || w.sel >= len(w.servers) {
		w.sel = 0
	}
	return w
}

// OnClose is called after OK, Cancel or Esc; the owner closes the window.
func (w *ConfigWindow) OnClose(fn func()) { w.onClose = fn }

func (w *ConfigWindow) close() {
	if w.onClose != nil {
		w.onClose()
	}
}

func (w *ConfigWindow) portErr() string {
	p, err := strconv.Atoi(strings.TrimSpace(w.port))
	if err != nil || p < 1 || p > 65535 {
		return "1–65535"
	}
	return ""
}

// valid reports whether the port and every added or changed server
// validate; untouched invalid servers do not block saving (deferred #3).
func (w *ConfigWindow) valid() bool {
	if w.portErr() != "" {
		return false
	}
	for _, e := range w.servers {
		if !e.dirty() {
			continue
		}
		if _, errs := e.server(); len(errs) > 0 {
			return false
		}
	}
	return true
}

// apply saves through run (synchronous when nil); OK closes the window on
// success. While a save is in flight, further OK/Apply clicks are ignored.
func (w *ConfigWindow) apply(closeAfter bool) {
	if w.saving {
		return
	}
	if !w.valid() {
		w.attempted = true // show every error now
		return
	}
	servers := make([]config.Server, len(w.servers))
	for i, e := range w.servers {
		servers[i] = e.result()
	}
	port, _ := strconv.Atoi(strings.TrimSpace(w.port))
	portText, sel, portable := w.port, w.sel, w.portable // as of OK, not of later clicks (Review #5)
	run := w.run
	if run == nil {
		run = func(work func() error, done func(error)) { done(work()) }
	}
	w.saving = true
	run(func() error { return w.save(servers, sel, port, portable) }, func(err error) {
		w.saving = false
		if err != nil {
			w.saveErr = Message(err)
			var busy *controller.PortInUseError
			if errors.As(err, &busy) {
				w.portBusy, w.busyPort = true, portText
			}
			return
		}
		w.saveErr = ""
		if closeAfter {
			w.close()
		}
	})
}

// mbtn is a button with an Alt+key mnemonic.
func mbtn(c *mui.Context, label string, k mui.Key, disabled bool, minW float32) bool {
	clicked := mui.Button(c, label).MinWidth(minW).Disabled(disabled).Clicked()
	return !disabled && (clicked || c.Shortcut(mui.Alt, k))
}

// View builds the window in the Windows 10 (Settings app) style: the
// server list in a left pane, the selected server's fields with labels
// above them, then the list buttons, the port, and OK / Cancel / Apply.
func (w *ConfigWindow) View(c *mui.Context) {
	T := i18n.T
	Compact(c)
	t := c.Theme()
	w.list.Selected = &w.sel
	w.list.Label = func(i int) string { return w.title(i) }
	valid := w.sel >= 0 && w.sel < len(w.servers)
	ro := valid && w.isSub(w.servers[w.sel])
	// Own servers stay above subscription servers: no moving across.
	canUp := valid && !ro && w.sel > 0
	canDown := valid && !ro && w.sel < len(w.servers)-1 && !w.isSub(w.servers[w.sel+1])

	if c.Shortcut(0, mui.KeyEscape) {
		w.close()
		return
	}

	mui.Column(c).Fill().Background(t.Background).Children(func() {
		mui.Row(c).Grow(1).AlignItems(mui.Stretch).Children(func() {
			mui.Column(c).Width(240).Background(navBackground(t)).Children(func() {
				mui.List(c, &w.list, len(w.servers), func(i int) {
					txt := mui.Text(c, w.title(i)).SingleLine().Padding(t.Space(2), t.Space(3))
					if _, errs := w.servers[i].server(); len(w.errsFor(w.servers[i], errs)) > 0 {
						txt.TextColor(t.Danger)
					}
				}).Grow(1).Padding(t.Space(1), 0).Label(T("Servers"))
				// List buttons: two even rows under the list.
				mui.Column(c).Gap(t.Space(1)).Padding(t.Space(2)).Children(func() {
					mui.Row(c).Gap(t.Space(1)).Children(func() {
						if mbtn(c, T("Add"), mui.KeyA, false, 0) {
							w.sel = w.insertOwn(newEditServer()) // above subscription servers
						}
						if mbtn(c, T("Delete"), mui.KeyD, !valid || ro, 0) {
							w.servers = append(w.servers[:w.sel], w.servers[w.sel+1:]...)
							w.sel = max(0, min(w.sel, len(w.servers)-1))
						}
						if mbtn(c, T("Duplicate"), mui.KeyC, !valid || ro, 0) {
							d := w.servers[w.sel]
							w.sel = w.insertOwn(d)
						}
					})
					mui.Row(c).Gap(t.Space(1)).Children(func() {
						if mbtn(c, T("Move Up"), mui.KeyU, !canUp, 0) {
							w.servers[w.sel-1], w.servers[w.sel] = w.servers[w.sel], w.servers[w.sel-1]
							w.sel--
						}
						if mbtn(c, T("Move Down"), mui.KeyO, !canDown, 0) {
							w.servers[w.sel+1], w.servers[w.sel] = w.servers[w.sel], w.servers[w.sel+1]
							w.sel++
						}
					})
				})
			})

			// Scroll keeps the fields inside the page above the buttons.
			mui.Scroll(c).Grow(1).Padding(t.Space(4), t.Space(6)).Children(func() {
				if !valid {
					return
				}
				e := &w.servers[w.sel]
				_, all := e.server()
				errs := w.errsFor(*e, all)
				mui.Text(c, T("Server")).FontSize(t.Rem(1.45)).FontWeight(300).Padding(0, 0, t.Space(3), 0)
				if ro {
					mui.Text(c, fmt.Sprintf(T("ssmygo.fromSub"), subscribe.Host(e.orig.Group))).TextColor(t.TextMuted).
						Padding(0, 0, t.Space(3), 0)
				}
				field := func(label, err string, build func()) {
					mui.Field(c, label, build).Error(err).Padding(0, 0, t.Space(3), 0)
				}
				mui.Themed(c, fieldTheme(t)).Background(mui.Transparent).MaxWidth(t.Rem(30)).Children(func() {
					field(T("Server IP"), errMsg(errs, "server"), func() { mui.TextInput(c, &e.Server).Disabled(ro) })
					field(T("Server Port"), errMsg(errs, "server_port"), func() { mui.TextInput(c, &e.Port).Disabled(ro) })
					field(T("Password"), errMsg(errs, "password"), func() {
						in := mui.TextInput(c, &e.Password).Disabled(ro)
						if !w.showPass {
							in.Password()
						}
					})
					mui.Checkbox(c, &w.showPass, T("Show Password")).Padding(0, 0, t.Space(3), 0)
					field(T("Encryption"), errMsg(errs, "method"), func() { mui.Select(c, &e.Method, config.Methods).Disabled(ro) })
					field(T("Plugin Program"), "", func() { mui.TextInput(c, &e.Plugin).Disabled(ro) })
					field(T("Plugin Options"), "", func() { mui.TextInput(c, &e.PluginOpts).Disabled(ro || e.Plugin == "") })
					mui.Checkbox(c, &e.NeedArgs, T("Need Plugin Argument")).Disabled(ro || e.Plugin == "").Padding(0, 0, t.Space(3), 0)
					field(T("Plugin Arguments"), "", func() {
						mui.TextInput(c, &e.PluginArgs).Disabled(ro || e.Plugin == "" || !e.NeedArgs)
					})
					field(T("Remarks"), "", func() { mui.TextInput(c, &e.Remarks).Disabled(ro) })
					field(T("Timeout(Sec)"), errMsg(errs, "timeout"), func() { mui.TextInput(c, &e.Timeout).Disabled(ro) })
					field(T("Group"), "", func() { mui.TextInput(c, &e.Group).ReadOnly(true) })
				})
			})
		})

		// The window's own settings and its buttons, on a strip below.
		mui.Column(c).Padding(t.Space(3), t.Space(4)).Gap(t.Space(2)).Background(navBackground(t)).Children(func() {
			if w.saveErr != "" {
				mui.Text(c, w.saveErr).TextColor(t.Danger)
			}
			mui.Row(c).Gap(t.Space(3)).AlignItems(mui.Center).Children(func() {
				mui.Text(c, T("Proxy Port"))
				var port mui.Element
				mui.Themed(c, fieldTheme(t)).Background(mui.Transparent).Children(func() {
					port = mui.TextInput(c, &w.port).Label(T("Proxy Port")).Width(t.Rem(6))
				})
				if w.portBusy && w.port != w.busyPort {
					w.portBusy = false // the user changed the port
				}
				if msg := w.portErr(); msg != "" {
					port.Border(2, t.Danger).Tooltip(msg)
				} else if w.portBusy {
					port.Border(2, t.Danger).Tooltip(w.saveErr)
				}
				mui.Checkbox(c, &w.portable, T("Portable Mode"))
			})
			mui.Row(c).Gap(t.Space(2)).Justify(mui.End).Children(func() {
				btnW := t.Rem(6)
				ok := mui.PrimaryButton(c, T("OK")).MinWidth(btnW).Disabled(w.saving)
				if ok.Clicked() || (!w.saving && c.Shortcut(0, mui.KeyEnter)) {
					w.apply(true)
				}
				if mui.Button(c, T("Cancel")).MinWidth(btnW).Clicked() {
					w.close()
				}
				if mui.Button(c, T("Apply")).MinWidth(btnW).Disabled(w.saving).Clicked() {
					w.apply(false)
				}
			})
		})
	})
}

func (w *ConfigWindow) title(i int) string {
	e := w.servers[i]
	if e.Remarks != "" {
		return e.Remarks
	}
	if strings.TrimSpace(e.Server) == "" {
		return i18n.T("ssmygo.newServer")
	}
	return e.Server + ":" + e.Port
}

var (
	configWin  *mygo.Window
	configView *ConfigWindow
)

// OpenConfigWindow shows the Edit Servers window, creating it on first use
// and destroying it on close (tray-only memory stays low).
func OpenConfigWindow(ctrl *controller.Controller) {
	if configWin != nil && !configWin.IsDestroyed() {
		configWin.Show()
		configWin.Focus()
		return
	}
	w := NewConfigWindow(ctrl.SaveServers, ctrl.Config())
	win := mygo.NewWindow(mygo.WindowOptions{
		Title: i18n.T("Edit Servers"), Width: 582, Height: 620, MinWidth: 582, MinHeight: 620,
		Content: mui.View(w.View),
	})
	// Save off the main thread (a plugin may take seconds to start) and ask
	// before running an untrusted plugin; results return on the main thread.
	w.run = func(work func() error, done func(error)) {
		runActionWith(ctrl.TrustPlugin, nil, work, func(err error) { win.Update(func() { done(err) }) })
	}
	w.OnClose(win.Close)
	win.OnClosed(func() { configWin, configView = nil, nil })
	configWin, configView = win, w
}

// appendToOpenConfigWindow merges servers imported from the tray into the
// open Edit Servers window, if any (deferred #7). Main thread only.
func appendToOpenConfigWindow(servers []config.Server) {
	if configWin == nil || configWin.IsDestroyed() || configView == nil {
		return
	}
	v := configView
	configWin.Update(func() { v.AppendServers(servers) })
}

// mergeIntoOpenConfigWindow refreshes an open Edit Servers window's
// subscription rows after an update (spec §3.4).
func mergeIntoOpenConfigWindow(cfg *config.Config) {
	if configWin == nil || configWin.IsDestroyed() || configView == nil {
		return
	}
	v := configView
	configWin.Update(func() {
		if v.subscriptionsDiffer(cfg) {
			v.MergeSubscriptions(cfg)
		}
	})
}

// insertOwn adds e after the user's own servers (before subscription
// servers) and returns its index.
func (w *ConfigWindow) insertOwn(e editServer) int {
	i := 0
	for i < len(w.servers) && !w.isSub(w.servers[i]) {
		i++
	}
	w.servers = slices.Insert(w.servers, i, e)
	return i
}

// MergeSubscriptions takes cfg's subscription servers after an update,
// keeping the rows (and unsaved edits) of the user's own servers.
func (w *ConfigWindow) MergeSubscriptions(cfg *config.Config) {
	var selOrig config.Server
	valid := w.sel >= 0 && w.sel < len(w.servers)
	selSub := valid && w.isSub(w.servers[w.sel])
	ownBefore := 0 // the selected own row's place among own rows
	if selSub {
		selOrig = w.servers[w.sel].orig
	} else if valid {
		for i := 0; i < w.sel; i++ {
			if !w.isSub(w.servers[i]) {
				ownBefore++
			}
		}
	}
	own := slices.DeleteFunc(slices.Clone(w.servers), func(e editServer) bool { return w.isSub(e) })
	w.sources = sourcesOf(cfg)
	merged := own
	for _, s := range cfg.Configs {
		if s.Group != "" {
			merged = append(merged, fromServer(s))
		}
	}
	w.servers = merged
	if valid && !selSub {
		w.sel = ownBefore // own rows come first, in the same order
	}
	if selSub {
		w.sel = 0
		for i, e := range w.servers {
			if w.isSub(e) && reflect.DeepEqual(e.orig, selOrig) {
				w.sel = i
				break
			}
		}
	}
	w.sel = min(w.sel, len(w.servers)-1)
}

// subscriptionsDiffer reports whether cfg's subscription servers differ
// from the window's.
func (w *ConfigWindow) subscriptionsDiffer(cfg *config.Config) bool {
	var mine, theirs []config.Server
	for _, e := range w.servers {
		if w.isSub(e) {
			mine = append(mine, e.orig)
		}
	}
	for _, s := range cfg.Configs {
		if s.Group != "" {
			theirs = append(theirs, s)
		}
	}
	return !slices.EqualFunc(mine, theirs, func(a, b config.Server) bool { return reflect.DeepEqual(a, b) })
}
