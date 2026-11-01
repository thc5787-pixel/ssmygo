package ui

import (
	"errors"
	"testing"
	"time"

	"github.com/thc5787/ssmygo/internal/controller"
)

func TestRunActionTrustFlow(t *testing.T) {
	for _, accept := range []bool{true, false} {
		old := confirm
		confirm = func(title, detail, ok string) bool { return accept }
		calls, trusted := 0, ""
		act := func() error {
			calls++
			if calls == 1 {
				return &controller.PluginNotTrustedError{Path: `C:\x\p.exe`, SHA256: "0123456789abcdef0123"}
			}
			return nil
		}
		var notified []string
		var final error = errors.New("unset")
		<-runActionWith(func(p, s string) error { trusted = p + "|" + s; return nil },
			func(m string) { notified = append(notified, m) }, act, func(err error) { final = err })
		confirm = old
		if accept && (calls != 2 || trusted != `C:\x\p.exe|0123456789abcdef0123` || final != nil) {
			t.Fatalf("accept: calls %d trusted %q final %v", calls, trusted, final)
		}
		if !accept && (calls != 1 || trusted != "" || final == nil) {
			t.Fatalf("cancel: calls %d trusted %q final %v", calls, trusted, final)
		}
		if len(notified) != 0 {
			t.Fatalf("notified %v (done handles errors)", notified)
		}
	}
}

// Without a done func, errors become notifications (tray actions).
func TestRunActionNotifies(t *testing.T) {
	var got []string
	<-runActionWith(nil, func(m string) { got = append(got, m) }, func() error { return controller.ErrNoServer }, nil)
	if len(got) != 1 || got[0] != "Add a server first." {
		t.Fatalf("%q", got)
	}
}

// Review Focus 3: a slow action (plugin start) never blocks the caller.
func TestRunActionOffMainThread(t *testing.T) {
	start := time.Now()
	done := runActionWith(nil, func(string) {}, func() error { time.Sleep(300 * time.Millisecond); return nil }, nil)
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("runAction blocked for %v", d)
	}
	<-done
}
