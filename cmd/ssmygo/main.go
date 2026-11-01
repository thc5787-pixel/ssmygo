// Command ssmygo is a Windows tray Shadowsocks client (MyGo native UI,
// shadowsocks-go core). See docs/superpowers/specs/2026-11-01-ssmygo-design.md.
package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/thc5787/ssmygo/assets"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
	"github.com/thc5787/ssmygo/internal/logx"
	"github.com/thc5787/ssmygo/internal/sysproxy"
	"github.com/thc5787/ssmygo/internal/ui"
)

// version matches mygo.json; release builds may override it with -ldflags -X.
var version = "0.1.0"

func main() {
	mygo.App.SetName("ssmygo")
	mygo.App.SetVersion(version)
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running instance opens Edit Servers (OnSecondInstance)
	}
	i18n.SetLocale(userLocale())

	var ctrl *controller.Controller
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mygo.App.OnWindowAllClosed(func() {}) // live in the tray
	shutdown := func() {
		if ctrl != nil {
			ctrl.Shutdown() // idempotent
		}
	}
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { shutdown() })
	// Windows logoff/shutdown (WM_ENDSESSION) ends the app through OnQuit
	// only, skipping OnBeforeQuit; the user's proxy must still be restored.
	mygo.App.OnQuit(shutdown)
	mygo.App.OnSecondInstance(func([]string, string) {
		if ctrl != nil {
			ui.OpenConfigWindow(ctrl)
		}
	})

	mygo.App.WhenReady(func() {
		exe, _ := os.Executable()
		portable := filepath.Dir(exe)
		roaming := filepath.Join(os.Getenv("APPDATA"), "ssmygo")
		paths := config.Paths{Root: config.ResolveRoot(portable, os.Getenv("APPDATA"))}

		if l, _, err := logx.New(paths.Logs(), 5<<20, 2); err == nil {
			logger = l
		}
		c, err := controller.New(controller.Deps{
			Paths: paths, PortableRoot: portable, RoamingRoot: roaming,
			Sys: sysproxy.Windows(), Logger: logger, DLC: assets.DLC(),
		})
		if err != nil {
			logger.Error("load config", "err", err)
			ui.Notify(logger, err.Error())
			mygo.App.Quit()
			return
		}
		ctrl = c
		if err := ui.RunTray(ctrl, version, logger); err != nil {
			logger.Error("tray", "err", err)
			mygo.App.Quit()
			return
		}
		if err := ctrl.Start(); err != nil {
			ui.Notify(logger, err.Error()) // tray tooltip shows it too; user can fix the port
		} else if s := ctrl.State(); s.LastError != "" {
			ui.Notify(logger, s.LastError)
		}
		if s := ctrl.State(); (s.FirstRun || !s.HasServer()) && !mygo.App.WasOpenedAtLogin() {
			ui.OpenConfigWindow(ctrl)
		}
	})

	if err := mygo.App.Run(); err != nil {
		logger.Error("app", "err", err)
		os.Exit(1)
	}
}
