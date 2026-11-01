package ui

import (
	"errors"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
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
}

// AppendServers adds servers imported while the window is open, so OK does
// not drop them (deferred #7). They are already saved, so not "new".
func (w *ConfigWindow) AppendServers(servers []config.Server) {
	for _, s := range servers {
		w.servers = append(w.servers, fromServer(s))
	}
}

// NewConfigWindow edits a copy of initial.
func NewConfigWindow(save SaveFunc, initial *config.Config) *ConfigWindow {
	w := &ConfigWindow{save: save, sel: initial.Index, port: strconv.Itoa(initial.LocalPort), portable: initial.PortableMode}
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
	if !w.valid() || w.saving {
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

// mbtn is a button with an Alt+key mnemonic placed in the bottom grid.
func mbtn(c *mui.Context, label string, k mui.Key, disabled bool, minW float32, row, col int) bool {
	clicked := mui.Button(c, label).MinWidth(minW).Disabled(disabled).RowStart(row).ColumnStart(col).Clicked()
	return !disabled && (clicked || c.Shortcut(mui.Alt, k))
}

// View builds the window: list | "Server" group, then the 6-column button grid.
func (w *ConfigWindow) View(c *mui.Context) {
	T := i18n.T
	Compact(c)
	t := c.Theme()
	gap := t.Space(2)
	w.list.Selected = &w.sel
	w.list.Label = func(i int) string { return w.title(i) }
	valid := w.sel >= 0 && w.sel < len(w.servers)
	canSave := w.valid() && !w.saving

	if c.Shortcut(0, mui.KeyEscape) {
		w.close()
		return
	}

	mui.Column(c).Fill().Padding(t.Space(3)).Gap(gap).Children(func() {
		mui.Row(c).Grow(1).Gap(gap).AlignItems(mui.Stretch).Children(func() {
			mui.List(c, &w.list, len(w.servers), func(i int) {
				txt := mui.Text(c, w.title(i)).SingleLine().Padding(t.Space(1), t.Space(2))
				if _, errs := w.servers[i].server(); len(errs) > 0 {
					txt.TextColor(t.Danger)
				}
			}).Width(180).Padding(t.Space(1)).Radius(t.Radius).Border(1, t.Border).
				Background(t.Background).Label(T("Servers"))

			// Scroll keeps extra error lines inside the box instead of over the buttons.
			mui.Scroll(c).Grow(1).Padding(t.Space(2), t.Space(3)).Radius(t.Radius).
				Border(1, t.Border).Children(func() {
				if !valid {
					return
				}
				e := &w.servers[w.sel]
				_, errs := e.server()
				mui.Form(c, func() {
					mui.Fieldset(c, T("Server"), func() {
						mui.Field(c, T("Server IP"), func() { mui.TextInput(c, &e.Server) }).Error(errMsg(errs, "server"))
						mui.Field(c, T("Server Port"), func() { mui.TextInput(c, &e.Port) }).Error(errMsg(errs, "server_port"))
						mui.Field(c, T("Password"), func() {
							if in := mui.TextInput(c, &e.Password); !w.showPass {
								in.Password()
							}
						}).Error(errMsg(errs, "password"))
						mui.Field(c, "", func() { mui.Checkbox(c, &w.showPass, T("Show Password")) })
						mui.Field(c, T("Encryption"), func() { mui.Select(c, &e.Method, config.Methods) }).Error(errMsg(errs, "method"))
						mui.Field(c, T("Plugin Program"), func() { mui.TextInput(c, &e.Plugin) })
						mui.Field(c, T("Plugin Options"), func() { mui.TextInput(c, &e.PluginOpts).Disabled(e.Plugin == "") })
						mui.Field(c, "", func() {
							mui.Checkbox(c, &e.NeedArgs, T("Need Plugin Argument")).Disabled(e.Plugin == "")
						})
						mui.Field(c, T("Plugin Arguments"), func() {
							mui.TextInput(c, &e.PluginArgs).Disabled(e.Plugin == "" || !e.NeedArgs)
						})
						mui.Field(c, T("Remarks"), func() { mui.TextInput(c, &e.Remarks) })
						mui.Field(c, T("Timeout(Sec)"), func() { mui.TextInput(c, &e.Timeout) }).Error(errMsg(errs, "timeout"))
						mui.Field(c, T("Group"), func() { mui.TextInput(c, &e.Group) })
					})
				})
			})
		})

		if w.saveErr != "" {
			mui.Text(c, w.saveErr).TextColor(t.Danger)
		}

		btnW := t.Rem(7)
		mui.Grid(c).ColumnTracks(mui.FitContent(), mui.FitContent(), mui.FitContent(), mui.Fr(1), mui.FitContent(), mui.FitContent()).
			Gap(gap).AlignItems(mui.Center).Children(func() {
			if mbtn(c, T("Add"), mui.KeyA, false, btnW, 1, 1) {
				w.servers = append(w.servers, newEditServer())
				w.sel = len(w.servers) - 1
			}
			if mbtn(c, T("Delete"), mui.KeyD, !valid, btnW, 1, 2) {
				w.servers = append(w.servers[:w.sel], w.servers[w.sel+1:]...)
				w.sel = max(0, min(w.sel, len(w.servers)-1))
			}
			mui.Text(c, T("Proxy Port")).RowStart(1).ColumnStart(3).JustifySelf(mui.End)
			// No Field here (it would add a label row); mark it like Field.Error does.
			port := mui.TextInput(c, &w.port).Label(T("Proxy Port")).RowStart(1).ColumnStart(4).ColumnSpan(-1)
			if w.portBusy && w.port != w.busyPort {
				w.portBusy = false // the user changed the port
			}
			if msg := w.portErr(); msg != "" {
				port.Border(1, t.Danger).Tooltip(msg)
			} else if w.portBusy {
				port.Border(1, t.Danger).Tooltip(w.saveErr)
			}

			if mbtn(c, T("Duplicate"), mui.KeyC, !valid, btnW, 2, 1) {
				d := w.servers[w.sel]
				w.servers = append(w.servers, d)
				w.sel = len(w.servers) - 1
			}
			mui.Checkbox(c, &w.portable, T("Portable Mode")).RowStart(2).ColumnStart(4).ColumnSpan(-1)

			if mbtn(c, T("Move Up"), mui.KeyU, !valid || w.sel <= 0, btnW, 3, 1) {
				w.servers[w.sel-1], w.servers[w.sel] = w.servers[w.sel], w.servers[w.sel-1]
				w.sel--
			}
			if mbtn(c, T("Move Down"), mui.KeyO, !valid || w.sel >= len(w.servers)-1, btnW, 3, 2) {
				w.servers[w.sel+1], w.servers[w.sel] = w.servers[w.sel], w.servers[w.sel+1]
				w.sel++
			}
			ok := mui.PrimaryButton(c, T("OK")).MinWidth(btnW).Disabled(!canSave).RowStart(3).ColumnStart(4).JustifySelf(mui.End)
			if ok.Clicked() || (canSave && c.Shortcut(0, mui.KeyEnter)) {
				w.apply(true)
			}
			if mui.Button(c, T("Cancel")).MinWidth(btnW).RowStart(3).ColumnStart(5).Clicked() {
				w.close()
			}
			if mui.Button(c, T("Apply")).MinWidth(btnW).Disabled(!canSave).RowStart(3).ColumnStart(6).Clicked() {
				w.apply(false)
			}
		})
	})
}

func (w *ConfigWindow) title(i int) string {
	e := w.servers[i]
	if e.Remarks != "" {
		return e.Remarks
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
