package plugin

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Spec describes one plugin process.
type Spec struct {
	Path       string // resolved absolute path
	Opts       string // plugin_opts → SS_PLUGIN_OPTIONS
	Args       string // plugin_args: command line tail, %VAR% expanded
	RemoteHost string
	RemotePort int
	Dir        string // working directory (exe dir); "" = Path's directory
}

// StartError means the plugin did not start listening; Tail holds its last
// output lines (at most 5).
type StartError struct {
	Name string
	Tail []string
	Err  error
}

func (e *StartError) Error() string {
	return fmt.Sprintf("plugin %s did not start: %v: %s", e.Name, e.Err, strings.Join(e.Tail, " | "))
}
func (e *StartError) Unwrap() error { return e.Err }

// ErrCrashLoop: the plugin exited unexpectedly 3 times within a minute.
var ErrCrashLoop = errors.New("plugin: crashed 3 times within a minute")

const (
	readyTimeout = 5 * time.Second
	probeEvery   = 50 * time.Millisecond
	tailLines    = 20
	shownTail    = 5
	crashWindow  = time.Minute
	crashLimit   = 3
)

// stableAfter: a process that ran this long resets the restart backoff.
var stableAfter = crashWindow

var backoffs = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}

// Process is a supervised plugin.
type Process struct {
	spec      Spec
	logger    *slog.Logger
	verify    func(string) error
	onRestart func(int)
	onFail    func(error)
	job       jobHandle

	mu       sync.Mutex
	cmd      *exec.Cmd
	exited   chan struct{}
	port     int
	stopped  bool
	gaveUp   bool      // supervision ended (crash loop or verify failure)
	upSince  time.Time // when the current process became ready
	crashes  []time.Time
	restarts int
	tail     []string
}

// Start verifies and launches the plugin and waits until it listens.
// verify (nil = always ok) runs before every start and restart. After an
// unexpected exit the plugin is restarted with backoff; onRestart gets the
// new port, onFail gets ErrCrashLoop or a verify error when giving up.
func Start(s Spec, logger *slog.Logger, verify func(string) error, onRestart func(int), onFail func(error)) (*Process, error) {
	if verify != nil {
		if err := verify(s.Path); err != nil {
			return nil, err
		}
	}
	if s.Dir == "" {
		s.Dir = filepath.Dir(s.Path)
	}
	job, err := newJob()
	if err != nil {
		return nil, err
	}
	p := &Process{spec: s, logger: logger, verify: verify, onRestart: onRestart, onFail: onFail, job: job}
	if err := p.launch(); err != nil {
		job.close()
		return nil, err
	}
	return p, nil
}

func (p *Process) name() string { return filepath.Base(p.spec.Path) }

// LocalPort is where the plugin currently listens.
func (p *Process) LocalPort() int { p.mu.Lock(); defer p.mu.Unlock(); return p.port }

// PID of the current plugin process.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Spec returns what the process was started with.
func (p *Process) Spec() Spec { return p.spec }

// launch starts one process and waits for readiness.
func (p *Process) launch() error {
	port, err := freePort()
	if err != nil {
		return err
	}
	env := append(os.Environ(),
		"SS_REMOTE_HOST="+p.spec.RemoteHost,
		"SS_REMOTE_PORT="+strconv.Itoa(p.spec.RemotePort),
		"SS_LOCAL_HOST=127.0.0.1",
		"SS_LOCAL_PORT="+strconv.Itoa(port),
		"SS_PLUGIN_OPTIONS="+p.spec.Opts,
	)
	cmd := command(p.spec.Path, expand(p.spec.Args, env))
	cmd.Env, cmd.Dir = env, p.spec.Dir
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return &StartError{Name: p.name(), Err: err}
	}
	if err := p.job.assign(cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return &StartError{Name: p.name(), Err: err}
	}
	var pumps sync.WaitGroup
	pumps.Add(2)
	go p.pump(stdout, &pumps)
	go p.pump(stderr, &pumps)
	exited := make(chan struct{})
	var waitErr error
	// exited depends on the process only: a child that inherited stdout/stderr
	// would keep the pumps open forever (Wait closes our read ends itself).
	pumpsDone := make(chan struct{})
	go func() { pumps.Wait(); close(pumpsDone) }()
	go func() { waitErr = cmd.Wait(); close(exited) }()

	deadline := time.Now().Add(readyTimeout)
	for {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), probeEvery); err == nil {
			c.Close()
			break
		}
		select {
		case <-exited:
			select { // let the pumps catch the last lines it printed
			case <-pumpsDone:
			case <-time.After(500 * time.Millisecond):
			}
			p.job.terminate() // and any children it left
			return &StartError{Name: p.name(), Tail: p.lastLines(), Err: fmt.Errorf("exited: %v", waitErr)}
		case <-time.After(probeEvery):
		}
		if time.Now().After(deadline) {
			p.job.terminate()
			cmd.Process.Kill()
			<-exited
			return &StartError{Name: p.name(), Tail: p.lastLines(), Err: errors.New("not listening after 5s")}
		}
	}
	p.mu.Lock()
	p.cmd, p.exited, p.port, p.upSince = cmd, exited, port, time.Now()
	p.mu.Unlock()
	go p.supervise(cmd, exited)
	return nil
}

// supervise restarts the plugin after an unexpected exit.
func (p *Process) supervise(cmd *exec.Cmd, exited chan struct{}) {
	<-exited
	p.mu.Lock()
	if p.stopped || p.cmd != cmd {
		p.mu.Unlock()
		return
	}
	now := time.Now()
	recent := p.crashes[:0]
	for _, t := range p.crashes {
		if now.Sub(t) < crashWindow {
			recent = append(recent, t)
		}
	}
	p.crashes = append(recent, now)
	if len(p.crashes) >= crashLimit {
		p.mu.Unlock()
		p.logger.Error("plugin keeps crashing; not restarting", "plugin", p.name())
		p.fail(ErrCrashLoop)
		return
	}
	if now.Sub(p.upSince) > stableAfter {
		p.restarts = 0 // it ran fine for a while: start the backoff over
	}
	wait := backoffs[min(p.restarts, len(backoffs)-1)]
	p.restarts++
	p.mu.Unlock()
	p.logger.Warn("plugin exited; restarting", "plugin", p.name(), "in", wait)
	time.Sleep(wait)

	p.mu.Lock()
	stopped := p.stopped
	p.mu.Unlock()
	if stopped {
		return
	}
	if p.verify != nil {
		if err := p.verify(p.spec.Path); err != nil {
			p.fail(err)
			return
		}
	}
	if err := p.launch(); err != nil {
		p.logger.Error("plugin restart failed", "plugin", p.name(), "err", err)
		// count it as another crash and try again
		go p.supervise(cmd, closedChan())
		p.mu.Lock()
		p.cmd = cmd
		p.mu.Unlock()
		return
	}
	if p.onRestart != nil {
		p.onRestart(p.LocalPort())
	}
}

func closedChan() chan struct{} { c := make(chan struct{}); close(c); return c }

func (p *Process) fail(err error) {
	p.mu.Lock()
	p.gaveUp = true
	p.mu.Unlock()
	if p.onFail != nil {
		p.onFail(err)
	}
}

// Stop ends the plugin; it is not treated as a crash. Idempotent.
func (p *Process) Stop() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	cmd, exited := p.cmd, p.exited
	p.mu.Unlock()
	p.job.terminate() // the whole tree: a child holding our pipes must go too
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		if exited != nil {
			<-exited
		}
	}
	p.job.close()
}

func (p *Process) pump(r io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		p.logger.Info("plugin output", "plugin", p.name(), "line", line)
		p.mu.Lock()
		p.tail = append(p.tail, line)
		if len(p.tail) > tailLines {
			p.tail = p.tail[len(p.tail)-tailLines:]
		}
		p.mu.Unlock()
	}
}

func (p *Process) lastLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := min(len(p.tail), shownTail)
	return append([]string(nil), p.tail[len(p.tail)-n:]...)
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

var percentVar = regexp.MustCompile(`%([^%\s]+)%`)

// expand replaces %NAME% with values from env (case-insensitive, as on
// Windows); unknown names are left as they are.
func expand(s string, env []string) string {
	vals := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			vals[strings.ToUpper(k)] = v
		}
	}
	return percentVar.ReplaceAllStringFunc(s, func(m string) string {
		if v, ok := vals[strings.ToUpper(m[1:len(m)-1])]; ok {
			return v
		}
		return m
	})
}

// Running reports whether the plugin is supervised: false after Stop or
// after supervision gave up (crash loop, file no longer trusted).
func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.stopped && !p.gaveUp
}
