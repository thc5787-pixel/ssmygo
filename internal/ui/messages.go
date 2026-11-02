package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/core/plugin"
	"github.com/thc5787/ssmygo/internal/i18n"
)

// Message is the text shown to the user for err, translated for the errors
// users can act on; other errors keep their own text (details are logged).
func Message(err error) string {
	if err == nil {
		return ""
	}
	var (
		port      *controller.PortInUseError
		save      *controller.SaveError
		untrusted *controller.PluginNotTrustedError
		notFound  *plugin.NotFoundError
		start     *plugin.StartError
		replaced  *controller.SelectionReplacedError
		subsFail  *controller.SubscriptionsFailedError
	)
	switch {
	case errors.As(err, &port):
		return fmt.Sprintf(i18n.T("ssmygo.portInUse"), port.Port)
	case errors.Is(err, controller.ErrNoServer):
		return i18n.T("ssmygo.noServer")
	case errors.As(err, &save):
		return fmt.Sprintf(i18n.T("ssmygo.cannotSave"), save.Err)
	case errors.As(err, &untrusted):
		return fmt.Sprintf(i18n.T("ssmygo.pluginNotTrusted"), filepath.Base(untrusted.Path))
	case errors.As(err, &notFound):
		return fmt.Sprintf(i18n.T("ssmygo.pluginNotFound"), notFound.Name, strings.Join(notFound.Tried, "; "))
	case errors.As(err, &start):
		detail := strings.Join(start.Tail, " | ")
		if detail == "" {
			detail = start.Err.Error()
		}
		return fmt.Sprintf(i18n.T("ssmygo.pluginStart"), start.Name, detail)
	case errors.Is(err, plugin.ErrCrashLoop):
		return i18n.T("ssmygo.pluginCrashLoop")
	case errors.As(err, &replaced):
		return fmt.Sprintf(i18n.T("ssmygo.selReplaced"), replaced.Old, replaced.New)
	case errors.As(err, &subsFail):
		return fmt.Sprintf(i18n.T("ssmygo.subsFailed"), subsFail.Failed, subsFail.Total)
	case errors.Is(err, controller.ErrNoStrategyMembers):
		return i18n.T("ssmygo.noStrategyMembers")
	case errors.Is(err, controller.ErrBadSubscriptionURL):
		return i18n.T("ssmygo.badSubURL")
	}
	return err.Error()
}
