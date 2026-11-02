package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/core/plugin"
	"github.com/thc5787/ssmygo/internal/geosite"
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
		geoFail   *controller.GeositeUpdateError
		geoGroups *geosite.MissingGroupsError
		skipped   *controller.UserRulesSkippedError
		userRead  *controller.UserRulesReadError
		geoDone   *controller.GeositeUpdatedNotice
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
	case errors.Is(err, controller.ErrBadSubscriptionURL):
		return i18n.T("ssmygo.badSubURL")
	case errors.As(err, &geoDone):
		return i18n.T("PAC updated")
	case errors.As(err, &geoGroups):
		return fmt.Sprintf(i18n.T("ssmygo.geositeMissing"), strings.Join(geoGroups.Groups, ", "))
	case errors.Is(err, geosite.ErrChecksum):
		return i18n.T("ssmygo.geositeChecksum")
	case errors.Is(err, geosite.ErrTooLarge):
		return i18n.T("ssmygo.geositeTooLarge")
	case errors.Is(err, geosite.ErrNotDLC):
		return i18n.T("ssmygo.geositeNotDLC")
	case errors.As(err, &geoFail):
		return fmt.Sprintf(i18n.T("ssmygo.geositeFailed"), geoFail.Err)
	case errors.As(err, &skipped):
		lines := make([]string, len(skipped.Lines))
		for i, l := range skipped.Lines {
			lines[i] = strconv.Itoa(l)
		}
		return fmt.Sprintf(i18n.T("ssmygo.userRuleSkipped"), len(skipped.Lines), strings.Join(lines, ", "))
	case errors.As(err, &userRead):
		return fmt.Sprintf(i18n.T("ssmygo.userRuleRead"), userRead.Err)
	}
	return err.Error()
}
