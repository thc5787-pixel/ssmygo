package ui

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
)

// MenuActions are what the tray menu items do.
type MenuActions struct {
	SetMode                            func(controller.Mode)
	Select                             func(int)
	EditServers, ImportClipboard, Quit func()
	ToggleBoot, ToggleLAN              func(bool)
	BootOn                             bool
}

// BuildMenu builds the P0 tray menu (spec §4.6) for state s.
func BuildMenu(s controller.State, a MenuActions) *mygo.Menu {
	T := i18n.T
	mode := func(id string, m controller.Mode, label string) *mygo.MenuItem {
		return &mygo.MenuItem{ID: id, Label: label, Type: mygo.MenuItemRadio, Checked: s.Mode == m,
			Disabled: m != controller.Disabled && !s.HasServer(),
			Click:    func(*mygo.MenuItem, *mygo.Window) { a.SetMode(m) }}
	}
	var servers []*mygo.MenuItem
	for i, title := range s.Servers {
		servers = append(servers, &mygo.MenuItem{ID: "server." + strconv.Itoa(i), Label: title, Type: mygo.MenuItemRadio,
			Checked: i == s.Current, Click: func(*mygo.MenuItem, *mygo.Window) { a.Select(i) }})
	}
	if len(servers) > 0 {
		servers = append(servers, mygo.Separator())
	}
	servers = append(servers,
		&mygo.MenuItem{ID: "edit", Label: T("Edit Servers..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.EditServers() }},
		&mygo.MenuItem{ID: "import", Label: T("Import URL from Clipboard..."), Click: func(*mygo.MenuItem, *mygo.Window) { a.ImportClipboard() }},
	)
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: T("System Proxy"), Submenu: []*mygo.MenuItem{
			mode("mode.disabled", controller.Disabled, T("Disable")),
			mode("mode.rules", controller.Rules, T("PAC")),
			mode("mode.global", controller.Global, T("Global")),
		}},
		{Label: T("Servers"), Submenu: servers},
		mygo.Separator(),
		{ID: "boot", Label: T("Start on Boot"), Type: mygo.MenuItemCheckbox, Checked: a.BootOn,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) { a.ToggleBoot(it.IsChecked()) }},
		{ID: "lan", Label: T("Allow other Devices to connect"), Type: mygo.MenuItemCheckbox, Checked: s.ShareLAN,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) { a.ToggleLAN(it.IsChecked()) }},
		mygo.Separator(),
		{ID: "quit", Label: T("Quit"), Click: func(*mygo.MenuItem, *mygo.Window) { a.Quit() }},
	})
}

// Tooltip is the tray hover text: version, server, mode and any error.
func Tooltip(s controller.State, version string) string {
	T := i18n.T
	lines := []string{"ssmygo " + version}
	if s.Current >= 0 && s.Current < len(s.Servers) {
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
	if s.LastError != "" {
		lines = append(lines, "⚠ "+s.LastError)
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
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "ssmygo", Icon: TrayIcon(s.Mode, false), ToolTip: Tooltip(s, version)})
	if err != nil {
		return err
	}
	report := func(err error) {
		if err != nil {
			Notify(logger, err.Error())
		}
	}
	var menu *mygo.Menu
	actions := MenuActions{
		SetMode:     func(m controller.Mode) { report(ctrl.SetMode(m)) },
		Select:      func(i int) { report(ctrl.SelectServer(i)) },
		EditServers: func() { OpenConfigWindow(ctrl) },
		ImportClipboard: func() {
			added, errs := ctrl.ImportURLs(mygo.Clipboard.ReadText())
			for _, e := range errs {
				logger.Warn("import", "err", e)
			}
			if added == 0 && len(errs) == 0 {
				Notify(logger, i18n.T("ssmygo.noLinks"))
				return
			}
			Notify(logger, fmt.Sprintf(i18n.T("ssmygo.imported"), added, len(errs)))
		},
		Quit:      mygo.App.Quit,
		ToggleLAN: func(on bool) { report(ctrl.SetShareLAN(on)) },
		ToggleBoot: func(on bool) {
			report(mygo.App.SetOpenAtLogin(on))
		},
	}
	// refresh uses only s: subscribers run while the controller holds its lock.
	refresh := func(s controller.State) {
		actions.BootOn = mygo.App.OpenAtLogin()
		menu = BuildMenu(s, actions)
		tray.SetIcon(TrayIcon(s.Mode, tray.Dark()), false)
		tray.SetToolTip(Tooltip(s, version))
	}
	refresh(s)
	ctrl.Subscribe(func(s controller.State) { mygo.RunOnMain(func() { refresh(s) }) })
	tray.OnAppearanceChange(func() { refresh(ctrl.State()) })
	tray.OnClick(func() { OpenConfigWindow(ctrl) })
	tray.OnRightClick(func() {
		tray.SetMenu(menu)
		tray.PopUpMenu()
		tray.SetMenu(nil)
	})
	return nil
}
