package ui

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
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

func TestFitsAt582x620(t *testing.T) {
	_, _, tt := newWin(sample(), nil)
	group, ok1 := tt.Find("Group")
	add, ok2 := tt.Find("Add")
	if !ok1 || !ok2 || group.Y+group.H > add.Y {
		t.Fatalf("Group %+v overlaps Add %+v", group, add)
	}
	// three visible errors must stay inside the group box (scroll), not over the buttons
	w, _, tt2 := newWin(sample(), nil)
	w.sel = 1
	w.servers[1].Server, w.servers[1].Port = "", "0"
	tt2.Frame()
	add2, _ := tt2.Find("Add")
	box, ok := tt2.Find("Server")
	if !ok || box.Y > add2.Y {
		t.Fatalf("layout broken with errors: box %+v add %+v", box, add2)
	}
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
