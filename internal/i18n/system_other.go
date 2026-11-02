//go:build !windows

package i18n

import "os"

// SystemLocale is $LANG outside Windows.
func SystemLocale() string { return os.Getenv("LANG") }
