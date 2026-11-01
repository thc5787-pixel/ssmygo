// Command fakeplugin is a SIP003 plugin for tests: it relays
// SS_LOCAL_HOST:SS_LOCAL_PORT to SS_REMOTE_HOST:SS_REMOTE_PORT.
// SS_PLUGIN_OPTIONS (";"-separated) controls failures:
//
//	die=1            print "fake: bad options" to stderr, exit 3
//	slow=1           never listen
//	crash-on=WORD    exit 4 when a client sends data starting with WORD
//	exit-after-ms=N  exit 5 N ms after listening
//	remote=HOST:PORT relay there instead of SS_REMOTE_HOST:PORT
//	spawn-child=1    start a sleeping child that inherits stdout/stderr
//
// FAKE_ARGS_OUT, if set, receives os.Args[1:] joined with "|".
package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	if os.Getenv("FAKE_SLEEPER") != "" { // child of spawn-child=1: holds the inherited stdout
		time.Sleep(time.Hour)
	}
	opts := map[string]string{}
	for _, kv := range strings.Split(os.Getenv("SS_PLUGIN_OPTIONS"), ";") {
		k, v, _ := strings.Cut(kv, "=")
		opts[k] = v
	}
	if out := os.Getenv("FAKE_ARGS_OUT"); out != "" {
		os.WriteFile(out, []byte(strings.Join(os.Args[1:], "|")), 0o644)
	}
	fmt.Println("fake: starting")
	if opts["spawn-child"] == "1" { // like a .cmd wrapper or a helper process
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "FAKE_SLEEPER=1")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		child.Start()
	}
	if opts["die"] == "1" {
		fmt.Fprintln(os.Stderr, "fake: bad options")
		os.Exit(3)
	}
	if opts["slow"] == "1" {
		time.Sleep(time.Hour) // not select{}: with no goroutines Go aborts at once
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(os.Getenv("SS_LOCAL_HOST"), os.Getenv("SS_LOCAL_PORT")))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake:", err)
		os.Exit(2)
	}
	if ms, err := strconv.Atoi(opts["exit-after-ms"]); err == nil {
		go func() { time.Sleep(time.Duration(ms) * time.Millisecond); os.Exit(5) }()
	}
	crashWord := opts["crash-on"]
	remote := net.JoinHostPort(os.Getenv("SS_REMOTE_HOST"), os.Getenv("SS_REMOTE_PORT"))
	if r := opts["remote"]; r != "" {
		remote = r // tests: the configured server is unreachable, only this path works
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			os.Exit(1)
		}
		go func(c net.Conn) {
			defer c.Close()
			first := make([]byte, 64)
			n, err := c.Read(first) // readiness probes send nothing and close
			if err != nil {
				return
			}
			if crashWord != "" && strings.HasPrefix(string(first[:n]), crashWord) {
				os.Exit(4)
			}
			r, err := net.Dial("tcp", remote)
			if err != nil {
				return
			}
			defer r.Close()
			r.Write(first[:n])
			go io.Copy(r, c)
			io.Copy(c, r)
		}(c)
	}
}
