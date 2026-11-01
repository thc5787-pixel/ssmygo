//go:build windows

package plugin

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobHandle is a Job Object that kills its processes when ssmygo exits
// (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE): the handle closes with the process.
type jobHandle struct{ h windows.Handle }

func newJob() (jobHandle, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return jobHandle{}, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return jobHandle{}, err
	}
	return jobHandle{h}, nil
}

func (j jobHandle) assign(pid int) error {
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ph)
	return windows.AssignProcessToJobObject(j.h, ph)
}

func (j jobHandle) close() {
	if j.h != 0 {
		windows.CloseHandle(j.h)
	}
}

// command builds the plugin command: the raw command line is the quoted
// program path followed by args exactly as written (original behaviour),
// with no console window.
func command(path, args string) *exec.Cmd {
	cmd := exec.Command(path)
	line := syscall.EscapeArg(path)
	if args != "" {
		line += " " + args
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd
}

// terminate kills every process in the job (the plugin and its children).
func (j jobHandle) terminate() {
	if j.h != 0 {
		windows.TerminateJobObject(j.h, 1)
	}
}
