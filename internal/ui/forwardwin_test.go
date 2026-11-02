package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
)

func newFwd(initial config.ForwardProxy) (*ForwardWindow, *[]config.ForwardProxy, *mui.Tester) {
	var saved []config.ForwardProxy
	w := NewForwardWindow(func(p config.ForwardProxy) error { saved = append(saved, p); return nil }, initial)
	return w, &saved, mui.NewTester(w.View, 320, 520)
}

func TestForwardWindowFields(t *testing.T) {
	_, _, tt := newFwd(config.ForwardProxy{ProxyTimeout: 3})
	for _, s := range []string{"Type", "No proxy", "SOCKS5", "HTTP", "Details", "Address", "Port", "Timeout (sec)",
		"Credentials (optional)", "Username", "Password", "Save", "Cancel"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q", s)
		}
	}
}

func TestForwardWindowDisabledStates(t *testing.T) {
	w, _, tt := newFwd(config.ForwardProxy{ProxyTimeout: 3}) // No proxy
	tt.Click("Address")
	tt.Type("x")
	if w.addr != "" {
		t.Fatal("address editable with No proxy")
	}
	tt.Click("SOCKS5")
	tt.Frame()
	tt.Click("Address")
	tt.Type("h")
	if w.addr != "h" {
		t.Fatalf("address not editable with SOCKS5: %q", w.addr)
	}
	tt.Click("Username")
	tt.Type("u")
	if w.user != "u" {
		t.Fatal("username not editable with SOCKS5")
	}
}

func TestForwardWindowValidation(t *testing.T) {
	w, saved, tt := newFwd(config.ForwardProxy{ProxyTimeout: 3})
	w.typ = "socks5"
	tt.Frame()
	tt.Click("Save")
	tt.Frame()
	if len(*saved) != 0 || !tt.HasText("Required.") {
		t.Fatalf("saved %v; texts %q", *saved, tt.Texts())
	}
	w.typ, w.addr, w.port, w.timeout, w.user, w.pwd = "http", "proxy.corp", "3128", "5", "u", "p"
	closed := false
	w.OnClose(func() { closed = true })
	tt.Frame()
	tt.Click("Save")
	want := config.ForwardProxy{UseProxy: true, ProxyType: config.ProxyHTTP, ProxyServer: "proxy.corp", ProxyPort: 3128,
		ProxyTimeout: 5, UseAuth: true, AuthUser: "u", AuthPwd: "p"}
	if len(*saved) != 1 || (*saved)[0].Validate() != nil || !sameProxy((*saved)[0], want) || !closed {
		t.Fatalf("saved %+v closed %v", *saved, closed)
	}
}

func TestForwardWindowNoProxySavesOff(t *testing.T) {
	_, saved, tt := newFwd(config.ForwardProxy{UseProxy: true, ProxyType: config.ProxySOCKS5, ProxyServer: "h", ProxyPort: 1, ProxyTimeout: 3})
	tt.Click("No proxy")
	tt.Frame()
	tt.Click("Save")
	if len(*saved) != 1 || (*saved)[0].UseProxy || (*saved)[0].ProxyServer != "h" {
		t.Fatalf("saved %+v (off, keeping the address)", *saved)
	}
}

func sameProxy(a, b config.ForwardProxy) bool {
	return a.UseProxy == b.UseProxy && a.ProxyType == b.ProxyType && a.ProxyServer == b.ProxyServer &&
		a.ProxyPort == b.ProxyPort && a.ProxyTimeout == b.ProxyTimeout && a.UseAuth == b.UseAuth &&
		a.AuthUser == b.AuthUser && a.AuthPwd == b.AuthPwd
}

func TestForwardSnapshots(t *testing.T) {
	dir := os.Getenv("SNAP_DIR")
	if dir == "" {
		t.Skip("SNAP_DIR unset")
	}
	for _, dark := range []bool{false, true} {
		_, _, tt := newFwd(config.ForwardProxy{UseProxy: true, ProxyType: config.ProxySOCKS5, ProxyServer: "127.0.0.1", ProxyPort: 7890, ProxyTimeout: 3})
		tt.SetDark(dark)
		tt.Frame()
		name := map[bool]string{false: "forward-light.png", true: "forward-dark.png"}[dark]
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, tt.Image())
		f.Close()
	}
}

// As the original: credentials are optional but must be given together, and
// using them follows from filling them in (no separate checkbox).
func TestForwardWindowCredentialsTogether(t *testing.T) {
	w, saved, tt := newFwd(config.ForwardProxy{ProxyTimeout: 3})
	w.typ, w.addr, w.port, w.user = "socks5", "h", "1080", "u"
	tt.Frame()
	tt.Click("Save")
	tt.Frame()
	if len(*saved) != 0 || !tt.HasText("Enter both user name and password.") {
		t.Fatalf("saved %v texts %q", *saved, tt.Texts())
	}
	w.user = ""
	tt.Frame()
	tt.Click("Save")
	if len(*saved) != 1 || (*saved)[0].UseAuth {
		t.Fatalf("saved %+v", *saved)
	}
}

// Review #4: the real window saves off the main thread.
func TestForwardSaveGoesThroughRun(t *testing.T) {
	w, saved, tt := newFwd(config.ForwardProxy{ProxyTimeout: 3})
	var pending func()
	w.run = func(work func() error, done func(error)) { pending = func() { done(work()) } }
	closed := false
	w.OnClose(func() { closed = true })
	tt.Click("Save")
	if len(*saved) != 0 || pending == nil {
		t.Fatal("saved on the caller")
	}
	tt.Frame()
	tt.Click("Save") // in flight: ignored
	pending()
	if len(*saved) != 1 || !closed {
		t.Fatalf("saved %d closed %v", len(*saved), closed)
	}
}
