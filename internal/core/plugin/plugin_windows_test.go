//go:build windows

package plugin

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func alive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

// helperParent runs in a child test process: it starts a plugin, prints its
// PID and waits to be killed.
func helperParent() {
	remote := netip.MustParseAddrPort(os.Getenv("PLUGIN_HELPER_REMOTE"))
	s := Spec{Path: os.Getenv("PLUGIN_HELPER_PATH"), RemoteHost: remote.Addr().String(), RemotePort: int(remote.Port())}
	p, err := Start(s, quiet(), nil, nil, nil)
	if err != nil {
		fmt.Println("ERR", err)
		os.Exit(1)
	}
	fmt.Println("PID", p.PID())
	select {}
}

// The job object ends the plugin when ssmygo dies without calling Stop.
func TestJobKillsOnParentExit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "PLUGIN_HELPER_PARENT=1", "PLUGIN_HELPER_PATH="+fakePath, "PLUGIN_HELPER_REMOTE="+echo(t).String())
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PID")))
	if err != nil {
		cmd.Process.Kill()
		t.Fatalf("helper said %q", line)
	}
	if !alive(pid) {
		t.Fatal("plugin not running")
	}
	cmd.Process.Kill()
	cmd.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if alive(pid) {
		t.Fatal("plugin outlived its parent")
	}
}
