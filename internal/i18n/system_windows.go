package i18n

import "golang.org/x/sys/windows"

// SystemLocale is the user's first preferred Windows UI language, e.g.
// "zh-CN".
func SystemLocale() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return "en"
	}
	return langs[0]
}
