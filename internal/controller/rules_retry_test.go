package controller

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Final review #1: a save that was briefly locked (editor, antivirus,
// OneDrive) is applied once the lock goes, without another edit.
func TestUserRuleRetriedAfterLockedRead(t *testing.T) {
	fastPoll(t)
	e, c := rulesEnv(t, nil)
	path := c.UserRulePath()
	p, _ := windows.UTF16PtrFromString(path)
	// Written through an exclusive handle: the poller sees the change but
	// cannot read the file until the handle closes.
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.CREATE_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	f := os.NewFile(uintptr(h), path)
	f.WriteString("@@||localhost^\n")
	f.Sync()
	time.Sleep(400 * time.Millisecond) // several polls fail to read
	f.Close()                          // no further write
	eventually(t, 3*time.Second, "locked save applied after the lock went", func() bool { return e.direct(c) })
}

// Final review #4: switching portable mode carries user-rule.txt and the
// downloaded dlc.dat to the new root, so user rules stay in force.
func TestPortableSwitchCarriesRuleFiles(t *testing.T) {
	fastPoll(t)
	e, c := rulesEnv(t, nil)
	os.WriteFile(c.UserRulePath(), []byte("@@||localhost^\n"), 0o644)
	eventually(t, 3*time.Second, "user rule", func() bool { return e.direct(c) })
	dlc := filepath.Join(e.portable, "dlc.dat")
	os.WriteFile(dlc, []byte("saved geosite"), 0o644)
	if err := c.SaveServers(c.Config().Configs, 0, c.State().Port, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"user-rule.txt", "dlc.dat"} {
		if _, err := os.Stat(filepath.Join(e.roaming, name)); err != nil {
			t.Errorf("%s not carried to the new root: %v", name, err)
		}
	}
	time.Sleep(300 * time.Millisecond) // several polls of the new path
	if !e.direct(c) {
		t.Fatal("user rules lost after the switch")
	}
}
