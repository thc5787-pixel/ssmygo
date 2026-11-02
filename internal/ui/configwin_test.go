package ui

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
)

type saved struct {
	servers  []config.Server
	index    int
	port     int
	portable bool
	calls    int
}

func sample() *config.Config {
	c := config.Default()
	c.Configs = []config.Server{
		{Server: "1.2.3.4", ServerPort: 8388, Method: "chacha20-ietf-poly1305", Password: "pw", Remarks: "Tokyo", Timeout: 5},
		{Server: "5.6.7.8", ServerPort: 443, Method: "2022-blake3-aes-256-gcm", Password: "", Timeout: 5},
	}
	return c
}

func newWin(cfg *config.Config, err error) (*ConfigWindow, *saved, *mui.Tester) {
	s := &saved{}
	w := NewConfigWindow(func(servers []config.Server, index, port int, portable bool) error {
		s.calls++
		s.servers, s.index, s.port, s.portable = servers, index, port, portable
		return err
	}, cfg)
	return w, s, mui.NewTester(w.View, 582, 620)
}

func TestFieldsPresent(t *testing.T) {
	_, _, tt := newWin(sample(), nil)
	for _, s := range []string{"Server IP", "Server Port", "Password", "Show Password", "Encryption", "Plugin Program",
		"Plugin Options", "Need Plugin Argument", "Plugin Arguments", "Remarks", "Timeout(Sec)", "Group",
		"Add", "Delete", "Duplicate", "Move Up", "Move Down", "Proxy Port", "Portable Mode", "OK", "Cancel", "Apply", "Tokyo"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q", s)
		}
	}
}

func TestAddDeleteMove(t *testing.T) {
	w, _, tt := newWin(sample(), nil)
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	if len(w.servers) != 3 || w.sel != 2 || w.servers[2].Method != config.DefaultMethod || w.servers[2].Timeout != "5" {
		t.Fatalf("after Add: %d sel %d %+v", len(w.servers), w.sel, w.servers[2])
	}
	tt.Click("Move Up")
	if w.sel != 1 || w.servers[1].Method != config.DefaultMethod {
		t.Fatalf("after Move Up: sel %d", w.sel)
	}
	tt.Click("Delete")
	if len(w.servers) != 2 {
		t.Fatalf("after Delete: %d", len(w.servers))
	}
	tt.Key(mui.Alt, mui.KeyA)
	if len(w.servers) != 3 {
		t.Fatal("Alt+A did not add")
	}
}

func TestValidationBlocksOK(t *testing.T) {
	w, s, tt := newWin(sample(), nil)
	w.sel = 1                                  // SS2022 server with empty password
	w.servers[1].Password = "still-not-base64" // edited, so it must block (deferred #3)
	tt.Frame()
	if !tt.HasText("Base64 key of 32 bytes.") {
		t.Fatalf("error not shown; texts %q", tt.Texts())
	}
	tt.Click("OK")
	tt.Click("Apply")
	if s.calls != 0 {
		t.Fatal("save called with an invalid server")
	}
}

func TestOKCallsSave(t *testing.T) {
	cfg := sample()
	cfg.Configs = cfg.Configs[:1]
	w, s, tt := newWin(cfg, nil)
	closed := false
	w.OnClose(func() { closed = true })
	w.servers[0].Remarks = "Osaka"
	tt.Frame()
	if err := tt.Click("OK"); err != nil {
		t.Fatal(err)
	}
	want := []config.Server{{Server: "1.2.3.4", ServerPort: 8388, Method: "chacha20-ietf-poly1305", Password: "pw", Remarks: "Osaka", Timeout: 5}}
	if s.calls != 1 || !reflect.DeepEqual(s.servers, want) || s.index != 0 || s.port != 1080 || !s.portable {
		t.Fatalf("saved %+v", s)
	}
	if !closed {
		t.Fatal("OnClose not fired")
	}
}

func TestApplyKeepsOpen(t *testing.T) {
	cfg := sample()
	cfg.Configs = cfg.Configs[:1]
	w, s, tt := newWin(cfg, nil)
	closed := false
	w.OnClose(func() { closed = true })
	tt.Click("Apply")
	if s.calls != 1 || closed {
		t.Fatalf("calls %d closed %v", s.calls, closed)
	}
}

func TestSaveErrorShown(t *testing.T) {
	cfg := sample()
	cfg.Configs = cfg.Configs[:1]
	w, _, tt := newWin(cfg, errors.New("port 1080 in use"))
	closed := false
	w.OnClose(func() { closed = true })
	tt.Click("OK")
	tt.Frame()
	if !tt.HasText("port 1080 in use") || closed {
		t.Fatalf("closed %v texts %q", closed, tt.Texts())
	}
}

func TestPluginArgsKeptForUnknownFields(t *testing.T) {
	cfg := sample()
	cfg.Configs = cfg.Configs[:1]
	cfg.Configs[0].Plugin, cfg.Configs[0].PluginArgs = "obfs-local", "-v"
	w, s, tt := newWin(cfg, nil)
	if !w.servers[0].NeedArgs {
		t.Fatal("NeedArgs not derived from plugin_args")
	}
	tt.Click("OK")
	if s.servers[0].PluginArgs != "-v" {
		t.Fatalf("plugin args lost: %+v", s.servers[0])
	}
}

// At the minimum size every button stays inside the window; the fields
// scroll inside the page above the bottom strip, errors included.
func TestFitsAt582x620(t *testing.T) {
	check := func(tt *mui.Tester) {
		t.Helper()
		ok, okFound := tt.Find("OK")
		add, addFound := tt.Find("Add")
		head, headFound := tt.Find("Server")
		if !okFound || !addFound || !headFound {
			t.Fatalf("missing OK %v Add %v header %v", okFound, addFound, headFound)
		}
		if ok.Y+ok.H > 620 || add.Y+add.H > 620 || head.Y > ok.Y {
			t.Fatalf("OK %+v Add %+v header %+v", ok, add, head)
		}
	}
	_, _, tt := newWin(sample(), nil)
	check(tt)
	w, _, tt2 := newWin(sample(), nil)
	w.sel = 1
	w.servers[1].Server, w.servers[1].Port = "", "0"
	tt2.Frame()
	check(tt2)
}

func TestSnapshots(t *testing.T) {
	dir := os.Getenv("SNAP_DIR")
	if dir == "" {
		t.Skip("SNAP_DIR unset")
	}
	for _, dark := range []bool{false, true} {
		_, _, tt := newWin(sample(), nil)
		tt.SetDark(dark)
		tt.Frame()
		name := "configwin-light.png"
		if dark {
			name = "configwin-dark.png"
		}
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, tt.Image())
		f.Close()
	}
}

// Deferred #3: an invalid server the user did not touch stays red but does
// not block saving other changes; it is passed back unchanged.
func TestUnchangedInvalidDoesNotBlockOK(t *testing.T) {
	w, s, tt := newWin(sample(), nil) // server 1: SS2022 with an empty key, untouched
	w.port = "2080"
	tt.Frame()
	if err := tt.Click("OK"); err != nil {
		t.Fatal(err)
	}
	if s.calls != 1 || s.port != 2080 {
		t.Fatalf("save not called: %+v", s)
	}
	if !reflect.DeepEqual(s.servers[1], sample().Configs[1]) {
		t.Fatalf("untouched server altered: %+v", s.servers[1])
	}
}

// Deferred #4: a busy port is explained in the user's language and the
// port box is marked until the port is edited.
func TestPortInUseShownTranslated(t *testing.T) {
	cfg := sample()
	cfg.Configs = cfg.Configs[:1]
	w, _, tt := newWin(cfg, &controller.PortInUseError{Port: 1080, Err: errors.New("bind")})
	tt.Click("OK")
	tt.Frame()
	if !tt.HasText("Port 1080 is in use by another program.") || !w.portBusy {
		t.Fatalf("portBusy %v texts %q", w.portBusy, tt.Texts())
	}
	w.port = "1081"
	tt.Frame()
	if w.portBusy {
		t.Fatal("port mark not cleared after editing the port")
	}
}

// Deferred #7: servers imported from the tray while the window is open are
// merged into it and saved with OK.
func TestAppendServers(t *testing.T) {
	cfg := sample()
	cfg.Configs = cfg.Configs[:1]
	w, s, tt := newWin(cfg, nil)
	w.AppendServers([]config.Server{{Server: "9.9.9.9", ServerPort: 443, Method: "aes-256-gcm", Password: "p", Remarks: "imported", Timeout: 5}})
	tt.Frame()
	if !tt.HasText("imported") {
		t.Fatal("imported server not listed")
	}
	tt.Click("OK")
	if len(s.servers) != 2 || s.servers[1].Remarks != "imported" {
		t.Fatalf("saved %+v", s.servers)
	}
}

// A1: plugin servers are supported, so no warning and no red row.
func TestPluginServerNotWarned(t *testing.T) {
	cfg := sample()
	cfg.Configs = cfg.Configs[:1]
	cfg.Configs[0].Plugin = "v2ray-plugin"
	_, s, tt := newWin(cfg, nil)
	tt.Frame()
	if tt.HasText("Plugins are not supported in this version.") {
		t.Fatal("stale P0 plugin warning")
	}
	tt.Click("OK")
	if s.calls != 1 {
		t.Fatal("plugin server not savable")
	}
}

// Review Focus 3: the real window saves through run (off the main thread,
// with the plugin trust dialog); while saving, OK/Apply do nothing more.
func TestSaveGoesThroughRun(t *testing.T) {
	cfg := sample()
	cfg.Configs = cfg.Configs[:1]
	w, s, tt := newWin(cfg, nil)
	var pending func()
	w.run = func(work func() error, done func(error)) { pending = func() { done(work()) } }
	closed := false
	w.OnClose(func() { closed = true })
	tt.Click("OK")
	if s.calls != 0 || pending == nil {
		t.Fatalf("save ran on the caller: calls %d", s.calls)
	}
	tt.Frame()
	tt.Click("OK") // still saving: ignored
	tt.Click("Apply")
	pending()
	tt.Frame()
	if s.calls != 1 || !closed {
		t.Fatalf("calls %d closed %v", s.calls, closed)
	}
}

// Review #5: the save uses the selection at the moment OK was pressed, even
// if the user clicks another row while the save (or trust dialog) is pending.
func TestSaveCapturesSelection(t *testing.T) {
	w, s, tt := newWin(sample(), nil)
	w.servers[1].Password = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=" // make server 1 valid
	w.sel = 1
	var pending func()
	w.run = func(work func() error, done func(error)) { pending = func() { done(work()) } }
	tt.Frame()
	tt.Click("OK")
	w.sel, w.portable = 0, false // user clicks around meanwhile
	pending()
	if s.index != 1 || !s.portable {
		t.Fatalf("saved index %d portable %v", s.index, s.portable)
	}
}

func subConfig() *config.Config {
	c := sample()
	c.Configs = append(c.Configs, config.Server{Server: "hk.example.com", ServerPort: 443, Method: "aes-256-gcm",
		Password: "pw", Remarks: "HK-sub", Group: "https://sub.example.com/s?token=x", Timeout: 5})
	c.OnlineConfigSource = []string{"https://sub.example.com/s?token=x"}
	return c
}

// C-D4: subscription servers are read-only in Edit Servers.
func TestConfigWinSubscriptionRowsReadOnly(t *testing.T) {
	w, _, tt := newWin(subConfig(), nil)
	w.sel = len(w.servers) - 1
	tt.Frame()
	if !tt.HasText("From subscription sub.example.com") {
		t.Fatalf("texts %q", tt.Texts())
	}
	before := w.servers[w.sel].Remarks
	tt.Click("Remarks")
	tt.Type("zzz")
	tt.Frame()
	if w.servers[w.sel].Remarks != before {
		t.Fatal("subscription server edited")
	}
	n := len(w.servers)
	tt.Click("Delete")
	tt.Click("Duplicate")
	tt.Frame()
	if len(w.servers) != n {
		t.Fatal("subscription server deleted or duplicated")
	}
}

func TestConfigWinMergeKeepsOwnEdits(t *testing.T) {
	w, _, _ := newWin(subConfig(), nil)
	w.servers[0].Remarks = "edited, unsaved"
	next := subConfig()
	next.Configs = append(next.Configs[:len(next.Configs)-1], config.Server{Server: "sg.example.com", ServerPort: 443,
		Method: "aes-256-gcm", Password: "pw", Remarks: "SG-sub", Group: "https://sub.example.com/s?token=x", Timeout: 5})
	w.MergeSubscriptions(next)
	if w.servers[0].Remarks != "edited, unsaved" {
		t.Fatal("own edit lost")
	}
	last := w.servers[len(w.servers)-1]
	if last.Remarks != "SG-sub" || len(w.servers) != len(next.Configs) {
		t.Fatalf("subscription rows not replaced: %d rows, last %q", len(w.servers), last.Remarks)
	}
}

// Final review #1: an own server selected below a subscription block stays
// selected when a merge moves it up.
func TestConfigWinMergeKeepsOwnSelection(t *testing.T) {
	cfg := subConfig()
	own2 := cfg.Configs[0]
	own2.Remarks = "own2"
	cfg.Configs = append(cfg.Configs, own2) // [own…, sub, own2] as an old import left it
	w, _, _ := newWin(cfg, nil)
	w.sel = len(w.servers) - 1
	w.MergeSubscriptions(subConfig())
	if w.servers[w.sel].Remarks != "own2" {
		t.Fatalf("selection moved to %q", w.servers[w.sel].Remarks)
	}
}

// Final review #4: a hand-typed group (not a subscription) stays editable.
func TestConfigWinHandGroupedEditable(t *testing.T) {
	cfg := sample()
	cfg.Configs[0].Group = "My group"
	w, _, tt := newWin(cfg, nil)
	w.sel = 0
	tt.Frame()
	if tt.HasText("From subscription") {
		t.Fatal("hand-grouped server shown as a subscription server")
	}
	n := len(w.servers)
	tt.Click("Delete")
	tt.Frame()
	if len(w.servers) != n-1 {
		t.Fatal("hand-grouped server cannot be deleted")
	}
}

// Win10 UI: a new server is listed as "New server", not ":".
func TestNewServerTitle(t *testing.T) {
	_, _, tt := newWin(sample(), nil)
	tt.Click("Add")
	tt.Frame()
	if !tt.HasText("New server") || slices.Contains(tt.Texts(), ":") {
		t.Fatalf("texts %q", tt.Texts())
	}
}

// Win10 UI: a new server's errors wait until OK, as Windows forms do.
func TestNewServerErrorsWaitForOK(t *testing.T) {
	_, s, tt := newWin(sample(), nil)
	tt.Click("Add")
	tt.Frame()
	if tt.HasText("Required.") {
		t.Fatal("errors shown before the user did anything")
	}
	tt.Click("OK")
	tt.Frame()
	if !tt.HasText("Required.") || s.calls != 0 {
		t.Fatalf("after OK: texts %q, saves %d", tt.Texts(), s.calls)
	}
}
