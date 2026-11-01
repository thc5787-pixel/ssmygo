//go:build !windows

package main

import "os"

func userLocale() string { return os.Getenv("LANG") }
