package ui

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
)

type hkHarness struct {
	w      *HotkeyWindow
	tt     *mui.Tester
	saved  []config.HotkeyConfig
	regs   []config.HotkeyConfig
	result map[string]error
	closed bool
}

func newHk(t *testing.T, initial config.HotkeyConfig, active bool) *hkHarness {
	t.Helper()
	h := &hkHarness{result: map[string]error{}}
	h.w = NewHotkeyWindow(initial, active,
		func(c config.HotkeyConfig) error { h.saved = append(h.saved, c); return nil },
		func(c config.HotkeyConfig) map[string]error {
			h.regs = append(h.regs, c)
			res := map[string]error{}
			for n, v := range hotkeyValues(c) {
				if v != "" {
					res[n] = h.result[n]
				}
			}
			return res
		})
	h.w.OnClose(func() { h.closed = true })
	h.tt = mui.NewTester(h.w.View, 320, 260)
	return h
}

func TestHotkeyWindowLabels(t *testing.T) {
	h := newHk(t, config.HotkeyConfig{SwitchSystemProxy: "Ctrl+Shift+Q"}, false)
	for _, s := range []string{"Toggle system proxy", "Toggle proxy mode", "Allow clients from LAN", "Open logs window",
		"Switch to previous server", "Switch to next server", "Register hotkeys at startup", "Register all", "Save", "Cancel", "Ctrl+Shift+Q"} {
		if !h.tt.HasText(s) {
			t.Errorf("missing %q in %q", s, h.tt.Texts())
		}
	}
}

func TestHotkeyWindowRecords(t *testing.T) {
	h := newHk(t, config.HotkeyConfig{SwitchSystemProxy: "Ctrl+Shift+Q"}, false)
	if err := h.tt.Click("Ctrl+Shift+Q"); err != nil {
		t.Fatal(err)
	}
	h.tt.Key(mui.Ctrl|mui.Alt, mui.Key1)
	h.tt.Frame()
	if h.w.vals[0] != "Ctrl+Alt+D1" || !h.tt.HasText("Ctrl+Alt+D1") {
		t.Fatalf("recorded %q", h.w.vals[0])
	}
	// The second row, by focus.
	h.w.focus = 2
	h.tt.Frame()
	h.tt.Key(mui.Ctrl|mui.Shift, mui.KeyQ)
	h.tt.Key(mui.Shift, mui.KeyBackquote)
	h.tt.Frame()
	if h.w.vals[1] != "Shift+OemTilde" {
		t.Fatalf("recorded %q", h.w.vals[1])
	}
	h.tt.Key(0, mui.KeyQ) // no modifier: ignored
	h.tt.Key(mui.Ctrl, mui.KeyUnknown)
	if h.w.vals[1] != "Shift+OemTilde" {
		t.Fatalf("a key without modifier or a lone modifier changed it: %q", h.w.vals[1])
	}
	h.tt.Key(0, mui.KeyBackspace)
	h.tt.Frame()
	if h.w.vals[1] != "" {
		t.Fatalf("Backspace left %q", h.w.vals[1])
	}
}

func TestHotkeyWindowRegisterAll(t *testing.T) {
	h := newHk(t, config.HotkeyConfig{SwitchSystemProxy: "Ctrl+Shift+Q", SwitchAllowLan: "Ctrl+Alt+L", ShowLogs: "Ctrl+Alt+D1",
		ServerMoveUp: "bogus"}, false)
	if h.w.vals[4] != "" {
		t.Fatalf("an unparsable hotkey must count as empty: %q", h.w.vals[4])
	}
	h.result["SwitchAllowLan"] = errors.New("taken")
	h.result["ShowLogs"] = ErrHotkeyParse
	h.tt.Click("Register all")
	h.tt.Frame()
	if len(h.regs) != 1 || len(h.saved) != 0 || h.closed {
		t.Fatalf("regs %d saved %d closed %v", len(h.regs), len(h.saved), h.closed)
	}
	want := []hkStatus{hkOK, hkNone, hkBad, hkBad, hkNone, hkNone}
	for i, s := range want {
		if h.w.status[i] != s {
			t.Errorf("row %d status %v want %v", i, h.w.status[i], s)
		}
	}
	if h.w.tips[2] != "Could not register: the combination may be in use by another program." || h.w.tips[3] != "Not a valid hotkey." {
		t.Fatalf("tips %q", h.w.tips)
	}
	// Recording a new combination resets the row's status.
	h.w.focus = 3
	h.tt.Frame()
	h.tt.Key(mui.Ctrl, mui.KeyF5)
	if h.w.status[2] != hkNone || h.w.vals[2] != "Ctrl+F5" {
		t.Fatalf("row 2 %v %q", h.w.status[2], h.w.vals[2])
	}
}

func TestHotkeyWindowSave(t *testing.T) {
	h := newHk(t, config.HotkeyConfig{SwitchSystemProxy: "Ctrl+Shift+Q"}, false)
	h.tt.Click("Register hotkeys at startup")
	h.tt.Frame()
	h.tt.Click("Save")
	if len(h.saved) != 1 || h.saved[0].SwitchSystemProxy != "Ctrl+Shift+Q" || !h.saved[0].RegHotkeysAtStartup {
		t.Fatalf("saved %+v", h.saved)
	}
	if len(h.regs) != 1 || !h.closed {
		t.Fatalf("startup on: re-registered %d, closed %v", len(h.regs), h.closed)
	}
}

func TestHotkeyWindowSaveWithoutRegistering(t *testing.T) {
	h := newHk(t, config.HotkeyConfig{}, false)
	h.tt.Click("Save")
	if len(h.saved) != 1 || len(h.regs) != 0 || !h.closed {
		t.Fatalf("saved %d regs %d closed %v", len(h.saved), len(h.regs), h.closed)
	}
}

// Hotkeys already registered (startup or Register All): Save re-registers
// the new settings; a failure keeps the window open showing it.
func TestHotkeyWindowSaveReregisters(t *testing.T) {
	h := newHk(t, config.HotkeyConfig{SwitchAllowLan: "Ctrl+Alt+L"}, true)
	h.result["SwitchAllowLan"] = errors.New("taken")
	h.tt.Click("Save")
	h.tt.Frame()
	if len(h.saved) != 1 || len(h.regs) != 1 || h.closed || h.w.status[2] != hkBad {
		t.Fatalf("saved %d regs %d closed %v status %v", len(h.saved), len(h.regs), h.closed, h.w.status)
	}
}

func TestHotkeyWindowCancel(t *testing.T) {
	h := newHk(t, config.HotkeyConfig{}, false)
	h.tt.Click("Cancel")
	if len(h.saved) != 0 || len(h.regs) != 0 || !h.closed {
		t.Fatalf("saved %d regs %d closed %v", len(h.saved), len(h.regs), h.closed)
	}
}

func TestHotkeySnapshots(t *testing.T) {
	dir := os.Getenv("SNAP_DIR")
	if dir == "" {
		t.Skip("SNAP_DIR unset")
	}
	for _, dark := range []bool{false, true} {
		h := newHk(t, config.HotkeyConfig{SwitchSystemProxy: "Ctrl+Shift+Q", SwitchAllowLan: "Ctrl+Alt+L", ShowLogs: "Ctrl+Alt+D1"}, false)
		h.result["SwitchAllowLan"] = errors.New("taken")
		h.tt.SetDark(dark)
		h.tt.Click("Register all")
		h.tt.Frame()
		name := map[bool]string{false: "hotkeys-light.png", true: "hotkeys-dark.png"}[dark]
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, h.tt.Image())
		f.Close()
	}
}
