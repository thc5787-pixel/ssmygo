package ui

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
	"github.com/thc5787/ssmygo/internal/traffic"
)

// MenuActions are what the tray menu items do.
type MenuActions struct {
	SetMode                            func(controller.Mode)
	Select                             func(int)
	EditServers, ImportClipboard, Quit func()
	Forward                            func()
	ToggleBoot, ToggleLAN              func(bool)
	BootOn                             bool
	ShowLogs                           func()
	TogglePluginOutput                 func(bool)
	ShareServers, ScanQR, OnlineConfig func()
	UpdateGeosite, EditUserRule        func() // the PAC submenu (P1-D)
	SetStrategy                        func(string)
	EditHotkeys                        func()
}

// BuildMenu builds the P0 tray menu (spec §4.6) for state s.
func BuildMenu(s controller.State, a MenuActions) *mygo.Menu {
	T := i18n.T
	mode := func(id string, m controller.Mode, label string) *mygo.MenuItem {
		return &mygo.MenuItem{ID: id, Label: label, Type: mygo.MenuItemRadio, Checked: s.Mode == m,
			Disabled: m != controller.Disabled && !s.HasServer(),
			Click:    func(*mygo.MenuItem, *mygo.Window) { a.SetMode(m) }}
	}
	// The original's order: the strategies, a separator, the servers.
	strategy := func(name, label string) *mygo.MenuItem {
		if s.StrategyExcluded > 0 {
			label = fmt.Sprintf(T("ssmygo.strategyExcluded"), label)
		}
		return &mygo.MenuItem{ID: "strategy." + name, Label: label, Type: mygo.MenuItemRadio, Checked: s.Strategy == name,
			Disabled: !s.HasServer(), Click: func(*mygo.MenuItem, *mygo.Window) { a.SetStrategy(name) }}
	}
	servers := []*mygo.MenuItem{
		strategy(controller.StrategyBalancing, T("Load Balance")),
		strategy(controller.StrategyHA, T("High Availability")),
		mygo.Separator(),
	}
	for i, title := range s.Servers {
		servers = append(servers, &mygo.MenuItem{ID: "server." + strconv.Itoa(i), Label: title, Type: mygo.MenuItemRadio,
			Checked: i == s.Current && s.Strategy == controller.StrategyNone, Click: func(*mygo.MenuItem, *mygo.Window) { a.Select(i) }})
	}
	if len(servers) > 0 {
		servers = append(servers, mygo.Separator())
	}
	servers = append(servers,
		&mygo.MenuItem{ID: "edit", Label: T("Edit Servers..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.EditServers() }},
		&mygo.MenuItem{ID: "share", Label: T("Share Server Config..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.ShareServers() }},
		&mygo.MenuItem{ID: "scan", Label: T("Scan QRCode from Screen..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.ScanQR() }},
		&mygo.MenuItem{ID: "import", Label: T("Import URL from Clipboard..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.ImportClipboard() }},
	)
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: T("System Proxy"), Submenu: []*mygo.MenuItem{
			mode("mode.disabled", controller.Disabled, T("Disable")),
			mode("mode.rules", controller.Rules, T("PAC")),
			mode("mode.global", controller.Global, T("Global")),
		}},
		{Label: T("Servers"), Submenu: servers},
		// "PAC " in the original, which has no translation for it.
		{Label: "PAC", Submenu: []*mygo.MenuItem{
			{ID: "pac.update", Label: T("Update Local PAC from Geosite"), Click: func(*mygo.MenuItem, *mygo.Window) { a.UpdateGeosite() }},
			{ID: "pac.userRule", Label: T("Edit User Rule for Geosite..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.EditUserRule() }},
		}},
		{ID: "forward", Label: T("Forward Proxy..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.Forward() }},
		{ID: "online", Label: T("Online Config..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.OnlineConfig() }},
		mygo.Separator(),
		{ID: "boot", Label: T("Start on Boot"), Type: mygo.MenuItemCheckbox, Checked: a.BootOn,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) { a.ToggleBoot(it.IsChecked()) }},
		{ID: "lan", Label: T("Allow other Devices to connect"), Type: mygo.MenuItemCheckbox, Checked: s.ShareLAN,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) { a.ToggleLAN(it.IsChecked()) }},
		{ID: "hotkeys", Label: T("Edit Hotkeys..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.EditHotkeys() }},
		{Label: T("Help"), Submenu: []*mygo.MenuItem{
			{ID: "help.logs", Label: T("Show Logs..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.ShowLogs() }},
			{ID: "help.pluginOutput", Label: T("Show Plugin Output"), Type: mygo.MenuItemCheckbox, Checked: s.ShowPluginOutput,
				Click: func(it *mygo.MenuItem, _ *mygo.Window) { a.TogglePluginOutput(it.IsChecked()) }},
		}},
		mygo.Separator(),
		{ID: "quit", Label: T("Quit"), Click: func(*mygo.MenuItem, *mygo.Window) { a.Quit() }},
	})
}

// Tooltip is the tray hover text: version, server, mode, the last second's
// rates while traffic flows, and any error.
func Tooltip(s controller.State, version string, rate traffic.Sample) string {
	T := i18n.T
	lines := []string{"ssmygo " + version}
	switch {
	case s.Strategy == controller.StrategyBalancing:
		lines = append(lines, T("Load Balance"))
	case s.Strategy == controller.StrategyHA:
		lines = append(lines, T("High Availability"))
	case s.Current >= 0 && s.Current < len(s.Servers):
		lines = append(lines, s.Servers[s.Current])
	}
	switch s.Mode {
	case controller.Disabled:
		lines = append(lines, T("System Proxy Disabled"))
	case controller.Rules:
		lines = append(lines, T("System Proxy On: ")+T("PAC"))
	case controller.Global:
		lines = append(lines, T("System Proxy On: ")+T("Global"))
	}
	if s.ForwardIgnored {
		lines = append(lines, T("ssmygo.forwardIgnored"))
	}
	if s.UDPDisabled {
		lines = append(lines, T("ssmygo.udpOff"))
	}
	if s.LastError != "" {
		msg := s.LastError
		if s.Err != nil {
			msg = Message(s.Err)
		}
		lines = append(lines, "⚠ "+msg)
	}
	// The rates line goes last and gives way first, so it never pushes a
	// warning out of the 127-rune limit.
	if rate != (traffic.Sample{}) {
		withRate := append(lines, "↑ "+traffic.FormatRate(rate.Up)+" ↓ "+traffic.FormatRate(rate.Down))
		if tip := strings.Join(withRate, "\n"); len([]rune(tip)) <= 127 {
			return tip
		}
	}
	tip := strings.Join(lines, "\n")
	if r := []rune(tip); len(r) > 127 { // Windows notification-area limit
		tip = string(r[:126]) + "…"
	}
	return tip
}

// Notify shows a system notification; failures are only logged.
func Notify(logger *slog.Logger, body string) {
	n := mygo.NewNotification(mygo.NotificationOptions{Title: "ssmygo", Body: body})
	if err := n.Show(); err != nil {
		logger.Warn("notification", "err", err)
	}
}

// RunTray creates the tray icon and keeps it in step with the controller.
// Call it on the main thread after the app is ready.
func RunTray(ctrl *controller.Controller, version string, logger *slog.Logger) error {
	s := ctrl.State()
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "ssmygo", Icon: TrayIcon(s.Mode, false, BadgeNone), ToolTip: Tooltip(s, version, traffic.Sample{})})
	if err != nil {
		return err
	}
	report := func(err error) {
		if err != nil {
			Notify(logger, Message(err))
		}
	}
	var menu *mygo.Menu
	actions := MenuActions{
		// Off the main thread: a plugin may take up to 5 s to start (Review Focus 3).
		SetMode:     func(m controller.Mode) { runAction(ctrl, logger, func() error { return ctrl.SetMode(m) }) },
		Select:      func(i int) { runAction(ctrl, logger, func() error { return ctrl.SelectServer(i) }) },
		SetStrategy: func(name string) { runAction(ctrl, logger, func() error { return ctrl.SetStrategy(name) }) },
		EditHotkeys: func() { openHotkeysWindow(ctrl) },
		Forward:     func() { OpenForwardWindow(ctrl) },
		EditServers: func() { OpenConfigWindow(ctrl) },
		ImportClipboard: func() {
			text := mygo.Clipboard.ReadText() // main thread
			go func() {                       // a commit may wait for the current plugin (Review #4)
				added, errs := ctrl.ImportURLs(text)
				for _, e := range errs {
					logger.Warn("import", "err", e)
				}
				mygo.RunOnMain(func() {
					if len(added) == 0 && len(errs) == 0 {
						Notify(logger, i18n.T("ssmygo.noLinks"))
						return
					}
					if len(added) > 0 {
						appendToOpenConfigWindow(added) // deferred #7
					}
					Notify(logger, fmt.Sprintf(i18n.T("ssmygo.imported"), len(added), len(errs)))
				})
			}()
		},
		Quit:      mygo.App.Quit,
		ToggleLAN: func(on bool) { runAction(ctrl, logger, func() error { return ctrl.SetShareLAN(on) }) },
		ToggleBoot: func(on bool) {
			report(mygo.App.SetOpenAtLogin(on))
		},
		ShowLogs:     func() { openLogWindow(ctrl) },
		ShareServers: func() { OpenShareWindow(ctrl) },
		OnlineConfig: func() { OpenOnlineConfigWindow(ctrl) },
		ScanQR: func() {
			notify := func(m string) { Notify(logger, m) }
			open := func(added []config.Server) {
				mygo.RunOnMain(func() {
					appendToOpenConfigWindow(added) // an open window keeps them on OK
					OpenConfigWindow(ctrl)
				})
			}
			go scanScreen(realScanDeps(ctrl, notify, open)) // capture and the dialog block
		},
		TogglePluginOutput: func(on bool) {
			runAction(ctrl, logger, func() error { return ctrl.SetShowPluginOutput(on) })
		},
		UpdateGeosite: func() {
			updateGeositeAction(ctrl.UpdateGeosite, func(m string) { Notify(logger, m) })
		},
		EditUserRule: func() {
			editUserRuleAction(ctrl.EnsureUserRuleFile, mygo.Shell.OpenPath, func(m string) { Notify(logger, m) })
		},
	}
	u := &trayUpdater{
		setIcon: func(p []byte) { tray.SetIcon(p, false) },
		setTip:  tray.SetToolTip,
		version: version,
		badge:   -1, // force the first apply
	}
	var rate traffic.Sample // last second's traffic; main thread only
	// refresh uses only s: subscribers run while the controller holds its lock.
	refresh := func(s controller.State) {
		actions.BootOn = mygo.App.OpenAtLogin()
		menu = BuildMenu(s, actions)
		u.force()
		u.apply(s, rate, tray.Dark())
	}
	refresh(s)
	startHotkeys(ctrl, logger)
	last := s
	// Traffic arrives every second off the main thread; the icon and the
	// tooltip change only when what they show changes (Review Focus 4).
	ctrl.SubscribeTraffic(func(snap traffic.Snapshot) {
		mygo.RunOnMain(func() {
			rate = snap.Rate
			u.apply(last, rate, tray.Dark())
		})
	})
	ctrl.Subscribe(func(s controller.State) {
		mygo.RunOnMain(func() {
			refresh(s)
			msg, retrust := pluginAlert(last, s)
			last = s
			for n := ctrl.TakeNotice(); n != nil; n = ctrl.TakeNotice() {
				Notify(logger, Message(n))
			}
			mergeIntoOpenConfigWindow(ctrl.Config())
			switch {
			case retrust: // the plugin file changed: ask again, then reconnect
				runAction(ctrl, logger, func() error { return ctrl.SelectServer(s.Current) })
			case msg != "":
				Notify(logger, msg)
			}
		})
	})
	tray.OnAppearanceChange(func() { refresh(ctrl.State()) })
	tray.OnClick(func() { OpenConfigWindow(ctrl) })
	tray.OnRightClick(func() {
		tray.SetMenu(menu)
		tray.PopUpMenu()
		tray.SetMenu(nil)
	})
	return nil
}

// pluginAlert decides what to tell the user when plugin supervision gives up
// (Review #3): a message to show, or retrust=true to offer the trust dialog
// for a plugin file that was replaced.
func pluginAlert(prev, cur controller.State) (msg string, retrust bool) {
	if cur.PluginFailure == nil || cur.PluginFailure == prev.PluginFailure {
		return "", false
	}
	var nt *controller.PluginNotTrustedError
	if errors.As(cur.PluginFailure, &nt) {
		return "", true
	}
	return Message(cur.PluginFailure), false
}

// trayUpdater pushes icon and tooltip changes to the tray, skipping
// repeats so a steady stream of traffic does not redraw every second.
type trayUpdater struct {
	setIcon func([]byte)
	setTip  func(string)
	version string

	mode  controller.Mode
	dark  bool
	badge Badge
	tip   string
}

// force makes the next apply update both, e.g. after a mode change.
func (u *trayUpdater) force() { u.badge, u.tip = -1, "" }

func (u *trayUpdater) apply(s controller.State, rate traffic.Sample, dark bool) {
	if b := BadgeFor(rate); b != u.badge || s.Mode != u.mode || dark != u.dark {
		u.badge, u.mode, u.dark = b, s.Mode, dark
		u.setIcon(TrayIcon(s.Mode, dark, b))
	}
	if tip := Tooltip(s, u.version, rate); tip != u.tip {
		u.tip = tip
		u.setTip(tip)
	}
}

// openLogWindow opens the log viewer (set by logwin.go).
var openLogWindow = func(*controller.Controller) {}

// openHotkeysWindow opens the Hotkeys window (set by hotkeywin.go).
var openHotkeysWindow = func(*controller.Controller) {}
