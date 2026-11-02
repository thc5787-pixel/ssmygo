package ui

import (
	"errors"
	"log/slog"

	"github.com/egoist/mygo"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/hotkey"
	"github.com/thc5787/ssmygo/internal/i18n"
)

// HotkeyNames are the original HotkeyConfig fields, in the Hotkeys window's
// order.
var HotkeyNames = []string{"SwitchSystemProxy", "SwitchSystemProxyMode", "SwitchAllowLan", "ShowLogs", "ServerMoveUp", "ServerMoveDown"}

// HotkeyActions maps a HotkeyNames entry to what its hotkey does.
type HotkeyActions map[string]func()

// ErrHotkeyParse: a hotkey string that is not a combination ssmygo can
// register.
var ErrHotkeyParse = errors.New("hotkey cannot be parsed")

// registrar registers a global shortcut (mygo.GlobalShortcut.Register);
// tests inject a fake.
type registrar func(acc string, fn func()) error

// hotkeyValues returns h's strings by name.
func hotkeyValues(h config.HotkeyConfig) map[string]string {
	return map[string]string{
		"SwitchSystemProxy": h.SwitchSystemProxy, "SwitchSystemProxyMode": h.SwitchSystemProxyMode,
		"SwitchAllowLan": h.SwitchAllowLan, "ShowLogs": h.ShowLogs,
		"ServerMoveUp": h.ServerMoveUp, "ServerMoveDown": h.ServerMoveDown,
	}
}

// registerAll unregisters ssmygo's hotkeys, then registers every hotkey of
// h. The result has an entry per non-empty hotkey: nil when registered,
// ErrHotkeyParse, or the registration error (taken by another program).
// One failure does not stop the others.
func registerAll(h config.HotkeyConfig, acts HotkeyActions, reg registrar, unregAll func()) map[string]error {
	unregAll()
	vals := hotkeyValues(h)
	res := map[string]error{}
	for _, n := range HotkeyNames {
		v := vals[n]
		if v == "" {
			continue
		}
		acc, ok := hotkey.ToAccelerator(v)
		if !ok {
			res[n] = ErrHotkeyParse
			continue
		}
		fn := acts[n]
		if fn == nil {
			fn = func() {}
		}
		res[n] = reg(acc, fn)
	}
	return res
}

// registerAtStartup registers h when RegHotkeysAtStartup is set (E-D2) and
// warns once if any hotkey failed.
func registerAtStartup(h config.HotkeyConfig, acts HotkeyActions, reg registrar, unregAll func(), warn func(string)) map[string]error {
	if !h.RegHotkeysAtStartup {
		return nil
	}
	res := registerAll(h, acts, reg, unregAll)
	for _, err := range res {
		if err != nil {
			warn(i18n.T("Register hotkey failed"))
			break
		}
	}
	return res
}

// actionQueue runs functions one at a time on its own goroutine, so a
// hotkey callback on the main thread never waits for the controller.
type actionQueue struct{ ch chan func() }

func newActionQueue(n int) *actionQueue {
	q := &actionQueue{ch: make(chan func(), n)}
	go func() {
		for fn := range q.ch {
			fn()
		}
	}()
	return q
}

// push queues fn; it reports false (and drops fn) when the queue is full.
func (q *actionQueue) push(fn func()) bool {
	select {
	case q.ch <- fn:
		return true
	default:
		return false
	}
}

// hotkeyActions are the six hotkey actions on the real controller. All but
// Show Logs run in order on q, with runAction's trust dialog and notices.
func hotkeyActions(ctrl *controller.Controller, logger *slog.Logger, q *actionQueue) HotkeyActions {
	queued := func(act func() error) func() {
		return func() {
			if !q.push(func() { <-runActionWith(ctrl.TrustPlugin, func(m string) { Notify(logger, m) }, act, nil) }) {
				logger.Warn("hotkey ignored: too many pending")
			}
		}
	}
	return HotkeyActions{
		"SwitchSystemProxy":     queued(ctrl.ToggleSystemProxy),
		"SwitchSystemProxyMode": queued(ctrl.ToggleProxyMode),
		"SwitchAllowLan":        queued(ctrl.ToggleShareLAN),
		"ShowLogs":              func() { openLogWindow(ctrl) }, // main thread
		"ServerMoveUp":          queued(func() error { return ctrl.SelectRelative(-1) }),
		"ServerMoveDown":        queued(func() error { return ctrl.SelectRelative(1) }),
	}
}

// Hotkeys are the app's registered global shortcuts (main thread only).
var hotkeys struct {
	acts       HotkeyActions
	registered bool // Register All or startup registration happened
}

func realRegister(acc string, fn func()) error { return mygo.GlobalShortcut.Register(acc, fn) }
func realUnregisterAll()                       { mygo.GlobalShortcut.UnregisterAll() }

// startHotkeys registers the configured hotkeys at startup when asked to.
func startHotkeys(ctrl *controller.Controller, logger *slog.Logger) {
	hotkeys.acts = hotkeyActions(ctrl, logger, newActionQueue(16))
	h := ctrl.Hotkeys()
	logUnparsable(h, logger)
	if res := registerAtStartup(h, hotkeys.acts, realRegister, realUnregisterAll, func(m string) { Notify(logger, m) }); res != nil {
		hotkeys.registered = true
		for n, err := range res {
			if err != nil {
				logger.Warn("hotkey not registered", "hotkey", n, "err", err)
			}
		}
	}
}

// logUnparsable logs, once at startup, hotkeys of the configuration that
// cannot be converted; they count as empty.
func logUnparsable(h config.HotkeyConfig, logger *slog.Logger) {
	for n, v := range hotkeyValues(h) {
		if _, ok := hotkey.ToAccelerator(v); v != "" && !ok {
			logger.Warn("hotkey cannot be parsed, ignored", "hotkey", n, "value", v)
		}
	}
}
