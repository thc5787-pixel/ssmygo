//go:build !windows

package plugin

import (
	"os/exec"
	"strings"
)

type jobHandle struct{}

func newJob() (jobHandle, error)   { return jobHandle{}, nil }
func (jobHandle) assign(int) error { return nil }
func (jobHandle) close()           {}
func (jobHandle) terminate()       {}

func command(path, args string) *exec.Cmd { return exec.Command(path, strings.Fields(args)...) }
