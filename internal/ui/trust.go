package ui

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
)

// confirm asks the user before a plugin program first runs (A1-D2). It
// blocks, so it is only called off the main thread (runActionWith).
var confirm = func(title, detail, ok string) bool {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:          mygo.MessageWarning,
		Title:         "ssmygo",
		Message:       title,
		Detail:        detail,
		Buttons:       []string{ok, i18n.T("Cancel")},
		DefaultButton: 0,
		CancelButton:  1,
	})
	return err == nil && res.Button == 0
}

// runActionWith runs act on a goroutine, so a slow plugin start never
// freezes the tray (Review Focus 3). An untrusted plugin triggers the
// confirmation dialog; on "Run" it is trusted and act runs once more. The
// final error goes to done if given, else becomes a notification. The
// returned channel closes when everything finished.
func runActionWith(trust func(path, sha string) error, notify func(string), act func() error, done func(error)) <-chan struct{} {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		err := act()
		var nt *controller.PluginNotTrustedError
		if errors.As(err, &nt) && trust != nil {
			detail := fmt.Sprintf(i18n.T("ssmygo.trustDetail"), filepath.Base(nt.Path), nt.Path, nt.SHA256[:min(16, len(nt.SHA256))])
			if confirm(i18n.T("ssmygo.trustTitle"), detail, i18n.T("ssmygo.trustRun")) {
				if err = trust(nt.Path, nt.SHA256); err == nil {
					err = act()
				}
			}
		}
		if done != nil {
			done(err)
		} else if err != nil {
			notify(Message(err))
		}
	}()
	return finished
}

// runAction is runActionWith for tray actions on the real controller.
func runAction(ctrl *controller.Controller, logger *slog.Logger, act func() error) {
	runActionWith(ctrl.TrustPlugin, func(m string) { Notify(logger, m) }, act, nil)
}
