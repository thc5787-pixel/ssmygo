package plugin

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var fakePath string // built once in TestMain

func TestMain(m *testing.M) {
	if os.Getenv("PLUGIN_HELPER_PARENT") != "" {
		helperParent()
		return
	}
	dir, err := os.MkdirTemp("", "fakeplugin")
	if err != nil {
		panic(err)
	}
	fakePath = filepath.Join(dir, "fakeplugin.exe")
	if out, err := exec.Command("go", "build", "-o", fakePath, "./testdata/fakeplugin").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func echo(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	return netip.MustParseAddrPort(ln.Addr().String())
}

func spec(remote netip.AddrPort, opts string) Spec {
	return Spec{Path: fakePath, Opts: opts, RemoteHost: remote.Addr().String(), RemotePort: int(remote.Port()), Dir: filepath.Dir(fakePath)}
}

func roundTrip(port int) error {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	c.Write([]byte("ping"))
	b := make([]byte, 4)
	if _, err := io.ReadFull(c, b); err != nil || string(b) != "ping" {
		return errors.New("no echo: " + string(b))
	}
	return nil
}

func TestResolveOrder(t *testing.T) {
	exe := t.TempDir()
	os.WriteFile(filepath.Join(exe, "a.exe"), nil, 0o755)
	os.MkdirAll(filepath.Join(exe, "plugins"), 0o755)
	os.WriteFile(filepath.Join(exe, "plugins", "b.exe"), nil, 0o755)
	os.MkdirAll(filepath.Join(exe, "sub"), 0o755)
	os.WriteFile(filepath.Join(exe, "sub", "c.exe"), nil, 0o755)
	for in, want := range map[string]string{
		"a":                           filepath.Join(exe, "a.exe"),
		"b":                           filepath.Join(exe, "plugins", "b.exe"),
		"b.exe":                       filepath.Join(exe, "plugins", "b.exe"),
		filepath.Join("sub", "c"):     filepath.Join(exe, "sub", "c.exe"),
		filepath.Join(exe, "a.exe"):   filepath.Join(exe, "a.exe"),
		" " + filepath.Join(exe, "a"): filepath.Join(exe, "a.exe"),
	} {
		got, err := Resolve(in, exe)
		if err != nil || !strings.EqualFold(got, want) {
			t.Errorf("Resolve(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	_, err := Resolve("nope-no-such-plugin", exe)
	var nf *NotFoundError
	if !errors.As(err, &nf) || len(nf.Tried) < 3 || nf.Name != "nope-no-such-plugin" {
		t.Fatalf("err = %#v", err)
	}
}

func TestHash(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x")
	os.WriteFile(p, []byte("abc"), 0o644)
	h, err := Hash(p)
	if err != nil || h != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("%s %v", h, err)
	}
}

func TestStartRelays(t *testing.T) {
	p, err := Start(spec(echo(t), ""), quiet(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	if err := roundTrip(p.LocalPort()); err != nil {
		t.Fatal(err)
	}
}

// Review Focus 1: odd path, %VAR% expansion, quoted arguments.
func TestArgsExpanded(t *testing.T) {
	dir := filepath.Join(exeTempDir(t), "Program Files ü")
	os.MkdirAll(dir, 0o755)
	odd := filepath.Join(dir, "fake plugin.exe")
	b, _ := os.ReadFile(fakePath)
	os.WriteFile(odd, b, 0o755)
	out := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("FAKE_ARGS_OUT", out)
	s := spec(echo(t), "")
	s.Path = odd
	s.Args = `--port %SS_LOCAL_PORT% --name "a b" --keep %NOT_A_VAR%`
	p, err := Start(s, quiet(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	got, _ := os.ReadFile(out)
	want := "--port|" + strconv.Itoa(p.LocalPort()) + "|--name|a b|--keep|%NOT_A_VAR%"
	if string(got) != want {
		t.Fatalf("args %q, want %q", got, want)
	}
}

// Review Focus 2: a plugin that exits at once is reported at once, with its output.
func TestDiesImmediately(t *testing.T) {
	start := time.Now()
	_, err := Start(spec(echo(t), "die=1"), quiet(), nil, nil, nil)
	var se *StartError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("reported after %v", d)
	}
	if !strings.Contains(strings.Join(se.Tail, "\n"), "fake: bad options") || len(se.Tail) > 5 {
		t.Fatalf("tail %q", se.Tail)
	}
}

func TestNeverListens(t *testing.T) {
	start := time.Now()
	_, err := Start(spec(echo(t), "slow=1"), quiet(), nil, nil, nil)
	var se *StartError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d < 4500*time.Millisecond || d > 6500*time.Millisecond {
		t.Fatalf("gave up after %v, want ~5s", d)
	}
}

func TestStopKills(t *testing.T) {
	p, err := Start(spec(echo(t), ""), quiet(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Running() {
		t.Fatal("not Running() after Start")
	}
	pid := p.PID()
	p.Stop()
	p.Stop() // idempotent
	if alive(pid) {
		t.Fatal("plugin still running after Stop")
	}
	if p.Running() {
		t.Fatal("Running() after Stop")
	}
}

func TestRestartAfterCrash(t *testing.T) {
	ports := make(chan int, 4)
	p, err := Start(spec(echo(t), "crash-on=CRASH"), quiet(), nil, func(port int) { ports <- port }, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	first := p.LocalPort()
	send(first, "CRASH") // makes this process exit
	select {
	case port := <-ports:
		if port == first {
			t.Fatal("restart reused the old port")
		}
		if err := roundTrip(port); err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("no restart")
	}
}

func TestCrashLoop(t *testing.T) {
	var mu sync.Mutex
	var failed error
	starts := 1
	done := make(chan struct{})
	p, err := Start(spec(echo(t), "exit-after-ms=100"), quiet(), nil,
		func(int) { mu.Lock(); starts++; mu.Unlock() },
		func(err error) { mu.Lock(); failed = err; mu.Unlock(); close(done) })
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("no crash-loop report")
	}
	time.Sleep(2500 * time.Millisecond) // a 4th start would happen by now
	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(failed, ErrCrashLoop) || starts != 3 {
		t.Fatalf("failed=%v starts=%d (want ErrCrashLoop after 3 starts)", failed, starts)
	}
	if p.Running() {
		t.Fatal("Running() after giving up")
	}
}

// Review Focus 5: a restart re-checks the file.
func TestVerifyOnRestart(t *testing.T) {
	calls := 0
	rejected := errors.New("not trusted any more")
	done := make(chan error, 1)
	p, err := Start(spec(echo(t), "crash-on=CRASH"), quiet(),
		func(string) error {
			calls++
			if calls > 1 {
				return rejected
			}
			return nil
		},
		func(int) { t.Error("restarted an untrusted file") },
		func(err error) { done <- err })
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	send(p.LocalPort(), "CRASH")
	select {
	case err := <-done:
		if !errors.Is(err, rejected) {
			t.Fatalf("onFail(%v)", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("no onFail")
	}
}

func TestVerifyBeforeFirstStart(t *testing.T) {
	no := errors.New("no")
	if _, err := Start(spec(echo(t), ""), quiet(), func(string) error { return no }, nil, nil); !errors.Is(err, no) {
		t.Fatalf("err = %v", err)
	}
}

// send writes msg on a fresh connection (used to trigger crash-on).
func send(port int, msg string) {
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second); err == nil {
		c.Write([]byte(msg))
		time.Sleep(50 * time.Millisecond)
		c.Close()
	}
}

// Review #2: a plugin with a child process (a .cmd wrapper, a helper) must
// not make Stop hang, and the child must not survive.
func TestStopWithChildProcess(t *testing.T) {
	p, err := Start(spec(echo(t), "spawn-child=1"), quiet(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { p.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop hung on a plugin with a child process")
	}
}

// Review #8: after a stable run the restart backoff starts over at 1 s.
func TestBackoffResetsAfterStableRun(t *testing.T) {
	old := stableAfter
	stableAfter = 300 * time.Millisecond
	defer func() { stableAfter = old }()
	restarted := make(chan int, 4)
	p, err := Start(spec(echo(t), "crash-on=CRASH"), quiet(), nil, func(port int) { restarted <- port }, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	time.Sleep(400 * time.Millisecond)
	send(p.LocalPort(), "CRASH") // first crash: 1 s
	port := <-restarted
	time.Sleep(400 * time.Millisecond) // stable again
	start := time.Now()
	send(port, "CRASH") // second crash: should be 1 s again, not 2 s
	<-restarted
	if d := time.Since(start); d > 1700*time.Millisecond {
		t.Fatalf("second restart took %v; backoff did not reset", d)
	}
}

// Review #9: a plugin path from an ss:// link must not make Windows contact
// another host (UNC → SMB → NTLM authentication) before any dialog.
func TestResolveRefusesNetworkPaths(t *testing.T) {
	for _, p := range []string{"\\\\attacker\\share\\x.exe", "//attacker/share/x.exe", "\\\\?\\UNC\\attacker\\share\\x.exe", "\\\\.\\UNC\\attacker\\share\\x.exe"} {
		_, err := Resolve(p, t.TempDir())
		if !errors.Is(err, ErrNetworkPath) {
			t.Errorf("Resolve(%q) = %v, want ErrNetworkPath", p, err)
		}
	}
}
