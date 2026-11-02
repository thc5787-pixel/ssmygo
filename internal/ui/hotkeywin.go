package ui

import (
	"log/slog"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/hotkey"
	"github.com/thc5787/ssmygo/internal/i18n"
)

// hkStatus is a row's registration status dot.
type hkStatus int

const (
	hkNone hkStatus = iota // not registered by this window: transparent
	hkOK                   // registered: green
	hkBad                  // taken or not parsable: yellow
)

// hotkeyLabels are the rows' original WPF labels, in HotkeyNames order.
var hotkeyLabels = []string{"resx.ToggleSystemProxy", "resx.ToggleProxyMode", "resx.AllowClientsFromLAN",
	"resx.OpenLogsWindow", "resx.SwitchToPreviousServer", "resx.SwitchToNextServer"}

// HotkeyWindow is the Hotkeys window (original HotkeysView, 320×260).
type HotkeyWindow struct {
	vals [6]string // original format, as "Ctrl+Shift+Q"
	// kept: a configured value ssmygo cannot record or register (e.g.
	// "Ctrl+Snapshot"), shown empty but written back unless the user
	// changes that row.
	kept    [6]string
	startup bool
	status  [6]hkStatus
	tips    [6]string
	focus   int  // FocusBind value: row+1; 0 = none
	active  bool // hotkeys are registered (startup or Register All)
	save    func(config.HotkeyConfig) error
	reg     func(config.HotkeyConfig) map[string]error
	saveErr string
	onClose func()
}

// NewHotkeyWindow edits initial. active tells whether ssmygo's hotkeys are
// registered now; reg registers a configuration (see registerAll).
func NewHotkeyWindow(initial config.HotkeyConfig, active bool, save func(config.HotkeyConfig) error,
	reg func(config.HotkeyConfig) map[string]error) *HotkeyWindow {
	w := &HotkeyWindow{startup: initial.RegHotkeysAtStartup, active: active, save: save, reg: reg}
	vals := hotkeyValues(initial)
	for i, n := range HotkeyNames {
		if _, ok := hotkey.ToAccelerator(vals[n]); ok {
			w.vals[i] = vals[n]
		} else {
			w.kept[i] = vals[n] // unparsable values count as empty
		}
	}
	return w
}

// OnClose is called after Save or Cancel; the owner closes the window.
func (w *HotkeyWindow) OnClose(fn func()) { w.onClose = fn }

func (w *HotkeyWindow) close() {
	if w.onClose != nil {
		w.onClose()
	}
}

// config is what the window holds; withKept adds back configured values it
// cannot show (for saving — they cannot be registered).
func (w *HotkeyWindow) config(withKept bool) config.HotkeyConfig {
	v := w.vals
	for i := range v {
		if withKept && v[i] == "" {
			v[i] = w.kept[i]
		}
	}
	return config.HotkeyConfig{SwitchSystemProxy: v[0], SwitchSystemProxyMode: v[1], SwitchAllowLan: v[2],
		ShowLogs: v[3], ServerMoveUp: v[4], ServerMoveDown: v[5], RegHotkeysAtStartup: w.startup}
}

// registerShown registers cfg and shows each row's result; it reports
// whether every hotkey registered.
func (w *HotkeyWindow) registerShown(cfg config.HotkeyConfig) bool {
	res := w.reg(cfg)
	w.active = true
	ok := true
	for i, n := range HotkeyNames {
		err, has := res[n]
		switch {
		case !has:
			w.status[i], w.tips[i] = hkNone, ""
		case err == nil:
			w.status[i], w.tips[i] = hkOK, i18n.T("ssmygo.hotkeyOK")
		case err == ErrHotkeyParse:
			w.status[i], w.tips[i] = hkBad, i18n.T("ssmygo.hotkeyParse")
			ok = false
		default:
			w.status[i], w.tips[i] = hkBad, i18n.T("ssmygo.hotkeyTaken")
			ok = false
		}
	}
	return ok
}

// wpfKeys are the WPF Key names of the keys a hotkey can use.
var wpfKeys = map[mui.Key]string{
	mui.KeyMinus: "OemMinus", mui.KeyEqual: "OemPlus", mui.KeyComma: "OemComma", mui.KeyPeriod: "OemPeriod",
	mui.KeySlash: "OemQuestion", mui.KeySemicolon: "OemSemicolon", mui.KeyQuote: "OemQuotes",
	mui.KeyBracketLeft: "OemOpenBrackets", mui.KeyBracketRight: "OemCloseBrackets", mui.KeyBackslash: "OemPipe",
	mui.KeyBackquote: "OemTilde", mui.KeyEnter: "Enter", mui.KeyTab: "Tab", mui.KeySpace: "Space",
	mui.KeyDelete: "Delete", mui.KeyInsert: "Insert", mui.KeyHome: "Home", mui.KeyEnd: "End",
	mui.KeyPageUp: "PageUp", mui.KeyPageDown: "PageDown", mui.KeyLeft: "Left", mui.KeyRight: "Right",
	mui.KeyUp: "Up", mui.KeyDown: "Down", mui.KeyEscape: "Escape",
}

func init() {
	for i := range 26 {
		wpfKeys[mui.KeyA+mui.Key(i)] = string(rune('A' + i))
	}
	for i := range 10 {
		wpfKeys[mui.Key0+mui.Key(i)] = "D" + string(rune('0'+i))
	}
	for i, k := range []mui.Key{mui.KeyF1, mui.KeyF2, mui.KeyF3, mui.KeyF4, mui.KeyF5, mui.KeyF6, mui.KeyF7, mui.KeyF8,
		mui.KeyF9, mui.KeyF10, mui.KeyF11, mui.KeyF12} {
		wpfKeys[k] = "F" + itoa(i+1)
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// record takes a key pressed in row i: Backspace clears it, a key with at
// least one modifier sets it; anything else is left to the window.
func (w *HotkeyWindow) record(i int, ev mui.InputEvent) bool {
	if ev.Kind != mui.InputKeyDown {
		return false
	}
	if ev.Key == mui.KeyBackspace {
		w.set(i, "")
		return true
	}
	var mods []string
	for _, m := range []struct {
		bit  mui.Modifiers
		name string
	}{{mui.Ctrl, "Ctrl"}, {mui.Alt, "Alt"}, {mui.Shift, "Shift"}, {mui.Super, "Windows"}} {
		if ev.Mods&m.bit != 0 {
			mods = append(mods, m.name)
		}
	}
	key := ev.Key
	if key == mui.KeyUnknown {
		key = ev.PhysicalKey // a key of a non-Latin layout, by its place
	}
	name, ok := wpfKeys[key]
	if !ok || len(mods) == 0 {
		return len(mods) > 0 // a lone modifier: nothing to record yet
	}
	if s := hotkey.FromKeys(mods, name); s != "" {
		w.set(i, s)
		return true
	}
	return false
}

func (w *HotkeyWindow) set(i int, v string) {
	w.kept[i] = "" // the user decided this row
	if w.vals[i] != v {
		w.vals[i], w.status[i], w.tips[i] = v, hkNone, ""
	}
}

// View lays out the original HotkeysView.
func (w *HotkeyWindow) View(c *mui.Context) {
	Compact(c)
	t := c.Theme()
	if c.Shortcut(0, mui.KeyEscape) {
		w.close()
		return
	}
	fill, border := fieldColors(t)
	mui.Column(c).Fill().Padding(t.Space(3)).Gap(t.Space(1)).Children(func() {
		for i, key := range hotkeyLabels {
			mui.Row(c).Gap(t.Space(2)).AlignItems(mui.Center).Children(func() {
				mui.Text(c, wpf(key)).SingleLine().Width(t.Rem(11))
				field := mui.Row(c.Key(key)).Grow(1).Height(t.Rem(1.6)).Padding(0, t.Space(2)).AlignItems(mui.Center).
					Border(1, border).Radius(t.Radius).Background(fill).Focusable().
					FocusBind(&w.focus, i+1).Label(wpf(key)).
					HandleInput(func(ev mui.InputEvent) bool { return w.record(i, ev) })
				if field.Focused() {
					field.Border(2, t.Accent)
				}
				field.Children(func() { mui.Text(c, w.vals[i]).SingleLine() })
				dot := mui.Box(c).Size(t.Rem(0.6), t.Rem(0.6)).Radius(t.Rem(0.3))
				switch w.status[i] {
				case hkOK:
					dot.Background(t.Success)
				case hkBad:
					dot.Background(t.Warning)
				}
				if w.tips[i] != "" {
					dot.Tooltip(w.tips[i])
				}
			})
		}
		mui.Checkbox(c, &w.startup, wpf("resx.RegisterHotkeysAtStartup"))
		if w.saveErr != "" {
			mui.Text(c, w.saveErr).TextColor(t.Danger)
		}
		mui.Box(c).Grow(1)
		mui.Row(c).Gap(t.Space(2)).Children(func() {
			if mui.Button(c, wpf("resx.registerAllButton_Content")).Clicked() {
				w.registerShown(w.config(false))
			}
			mui.Box(c).Grow(1)
			if defaultButton(c, wpf("resx.saveButton_Content")).MinWidth(t.Rem(4)).Clicked() {
				w.apply()
			}
			if mui.Button(c, wpf("resx.cancelButton_Content")).MinWidth(t.Rem(4)).Clicked() {
				w.close() // hotkeys already registered stay registered, as in the original
			}
		})
	})
}

// apply saves; with startup registration on, or hotkeys registered already,
// it registers the new settings too, and stays open if any failed.
func (w *HotkeyWindow) apply() {
	if err := w.save(w.config(true)); err != nil {
		w.saveErr = Message(err)
		return
	}
	w.saveErr = ""
	if (w.startup || w.active) && !w.registerShown(w.config(false)) {
		return
	}
	w.close()
}

var hotkeyWin *mygo.Window

// OpenHotkeysWindow shows the Hotkeys window (lazy, destroyed on close).
func OpenHotkeysWindow(ctrl *controller.Controller) {
	if hotkeyWin != nil && !hotkeyWin.IsDestroyed() {
		hotkeyWin.Show()
		hotkeyWin.Focus()
		return
	}
	if hotkeys.acts == nil {
		hotkeys.acts = hotkeyActions(ctrl, slog.New(slog.DiscardHandler), newActionQueue(16))
	}
	reg := func(h config.HotkeyConfig) map[string]error {
		hotkeys.registered = true
		return registerAll(h, hotkeys.acts, realRegister, realUnregisterAll)
	}
	w := NewHotkeyWindow(ctrl.Hotkeys(), hotkeys.registered, ctrl.SaveHotkeys, reg)
	win := mygo.NewWindow(mygo.WindowOptions{
		Title: wpf("resx.Hotkeys"), Width: 320, Height: 260, MinWidth: 320, MinHeight: 260,
		Content: mui.View(w.View),
	})
	w.OnClose(win.Close)
	win.OnClosed(func() { hotkeyWin = nil })
	hotkeyWin = win
}

func init() { openHotkeysWindow = OpenHotkeysWindow }
