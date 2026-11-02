package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrategyAndHotkeyRoundTrip(t *testing.T) {
	src := `{"configs":[],"strategy":"com.shadowsocks.strategy.ha","hotkey":{` +
		`"SwitchSystemProxy":"Ctrl+Shift+Q","SwitchSystemProxyMode":"Ctrl+Alt+D1","SwitchAllowLan":"",` +
		`"ShowLogs":"Windows+F1","ServerMoveUp":"Shift+OemTilde","ServerMoveDown":"","RegHotkeysAtStartup":true,"Future":5}}`
	dir := t.TempDir()
	p := filepath.Join(dir, "gui-config.json")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Strategy != StrategyHA {
		t.Fatalf("strategy %q", c.Strategy)
	}
	h := c.Hotkey
	if h.SwitchSystemProxy != "Ctrl+Shift+Q" || h.SwitchSystemProxyMode != "Ctrl+Alt+D1" ||
		h.ShowLogs != "Windows+F1" || h.ServerMoveUp != "Shift+OemTilde" || !h.RegHotkeysAtStartup {
		t.Fatalf("hotkey %+v", h)
	}
	d := c.Clone()
	d.SetHotkey(HotkeyConfig{SwitchAllowLan: "Ctrl+L"})
	if c.Hotkey.SwitchAllowLan != "" {
		t.Fatal("Clone shares hotkey")
	}
	out := filepath.Join(dir, "out.json")
	if err := d.Save(out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	s := string(b)
	for _, want := range []string{`"strategy": "com.shadowsocks.strategy.ha"`, `"SwitchAllowLan": "Ctrl+L"`, `"Future": 5`, `"RegHotkeysAtStartup": false`} {
		if !strings.Contains(s, want) {
			t.Errorf("saved config lacks %s:\n%s", want, s)
		}
	}
}

func TestStrategyEmptySavesNull(t *testing.T) {
	c := Default()
	if c.Strategy != "" || c.Hotkey.SwitchSystemProxy != "" || c.Hotkey.RegHotkeysAtStartup {
		t.Fatalf("defaults %q %+v", c.Strategy, c.Hotkey)
	}
	p := filepath.Join(t.TempDir(), "g.json")
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), `"strategy": null`) || !strings.Contains(string(b), `"ServerMoveDown": ""`) {
		t.Fatalf("saved %s", b)
	}
}

func TestKnownStrategy(t *testing.T) {
	for id, want := range map[StrategyID]bool{StrategyBalancing: true, StrategyHA: true, "": false, "com.shadowsocks.strategy.scbs": false} {
		if got := id.Known(); got != want {
			t.Errorf("%q: %v", id, got)
		}
	}
}
