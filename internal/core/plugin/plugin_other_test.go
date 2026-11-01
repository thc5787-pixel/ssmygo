//go:build !windows

package plugin

import (
	"os"
	"syscall"
)

func helperParent() { os.Exit(0) }

func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}
