package main

import "golang.org/x/sys/windows"

// userLocale is the user's first preferred UI language, e.g. "zh-CN".
func userLocale() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return "en"
	}
	return langs[0]
}
