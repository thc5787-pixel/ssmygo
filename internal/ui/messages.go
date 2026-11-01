package ui

import (
	"errors"
	"fmt"

	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
)

// Message is the text shown to the user for err, translated for the errors
// users can act on; other errors keep their own text (details are logged).
func Message(err error) string {
	if err == nil {
		return ""
	}
	var port *controller.PortInUseError
	var save *controller.SaveError
	switch {
	case errors.As(err, &port):
		return fmt.Sprintf(i18n.T("ssmygo.portInUse"), port.Port)
	case errors.Is(err, controller.ErrNoServer):
		return i18n.T("ssmygo.noServer")
	case errors.Is(err, controller.ErrPluginUnsupported):
		return i18n.T("ssmygo.pluginUnsupported")
	case errors.As(err, &save):
		return fmt.Sprintf(i18n.T("ssmygo.cannotSave"), save.Err)
	}
	return err.Error()
}
