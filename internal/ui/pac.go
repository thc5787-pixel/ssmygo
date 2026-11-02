package ui

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/thc5787/ssmygo/internal/i18n"
)

// updateGeositeAction runs PAC ▸ Update Local PAC from Geosite off the main
// thread (a download takes up to a minute) and reports the outcome with
// the original's messages. The returned channel closes when it finished.
func updateGeositeAction(update func(context.Context) (bool, error), notify func(string)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		changed, err := update(context.Background())
		switch {
		case err != nil:
			notify(Message(err))
		case changed:
			notify(i18n.T("PAC updated"))
		default:
			notify(i18n.T("No updates found. Please report to Geosite if you have problems with it."))
		}
	}()
	return done
}

// editUserRuleAction runs PAC ▸ Edit User Rule for Geosite...: it creates
// user-rule.txt from the template when missing and opens it in the
// default editor. Changes apply by themselves (the controller polls it).
func editUserRuleAction(ensure func() (string, error), open func(string) error, notify func(string)) {
	path, err := ensure()
	if err == nil {
		err = open(path)
	}
	if err != nil {
		notify(fmt.Sprintf(i18n.T("ssmygo.cannotOpen"), filepath.Base(path), err))
	}
}
