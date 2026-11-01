package ui

import (
	"strings"
	"testing"

	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/core/plugin"
)

func TestBuildMenu(t *testing.T) {
	var got controller.Mode = -1
	a := MenuActions{SetMode: func(m controller.Mode) { got = m }, Select: func(int) {}, EditServers: func() {},
		ImportClipboard: func() {}, Quit: func() {}, ToggleBoot: func(bool) {}, ToggleLAN: func(bool) {}, BootOn: true}
	s := controller.State{Mode: controller.Rules, Servers: []string{"Tokyo", "Osaka"}, Current: 1, ShareLAN: true}
	m := BuildMenu(s, a)
	for id, want := range map[string]bool{"mode.disabled": false, "mode.rules": true, "mode.global": false,
		"server.0": false, "server.1": true, "boot": true, "lan": true} {
		it := m.ItemByID(id)
		if it == nil {
			t.Fatalf("no item %q", id)
		}
		if it.IsChecked() != want {
			t.Errorf("%s checked=%v want %v", id, it.IsChecked(), want)
		}
	}
	if m.ItemByID("mode.rules").Label != "PAC" || m.ItemByID("server.1").Label != "Osaka" {
		t.Error("labels")
	}
	m.ItemByID("mode.global").Click(m.ItemByID("mode.global"), nil)
	if got != controller.Global {
		t.Fatalf("SetMode got %v", got)
	}
	var labels []string
	for _, it := range m.Items() {
		labels = append(labels, it.Label)
	}
	if strings.Join(labels, "|") != "System Proxy|Servers|Forward Proxy...||Start on Boot|Allow other Devices to connect||Quit" {
		t.Fatalf("top level %q", labels)
	}
}

func TestBuildMenuNoServers(t *testing.T) {
	m := BuildMenu(controller.State{}, MenuActions{})
	for _, id := range []string{"mode.rules", "mode.global"} {
		if m.ItemByID(id).IsEnabled() {
			t.Errorf("%s enabled without servers", id)
		}
	}
	if !m.ItemByID("edit").IsEnabled() || !m.ItemByID("import").IsEnabled() {
		t.Error("edit/import must stay enabled")
	}
}

func TestTooltip(t *testing.T) {
	s := controller.State{Mode: controller.Global, Servers: []string{"Tokyo"}, LastError: "port 1080 in use"}
	tip := Tooltip(s, "0.1.0")
	for _, want := range []string{"ssmygo 0.1.0", "Tokyo", "Global", "⚠ port 1080 in use"} {
		if !strings.Contains(tip, want) {
			t.Errorf("tooltip %q lacks %q", tip, want)
		}
	}
	if !strings.Contains(Tooltip(controller.State{}, "0.1.0"), "System Proxy Disabled") {
		t.Error("disabled text")
	}
}

func TestBuildMenuForward(t *testing.T) {
	opened := false
	m := BuildMenu(controller.State{}, MenuActions{Forward: func() { opened = true }})
	it := m.ItemByID("forward")
	if it == nil || it.Label != "Forward Proxy..." {
		t.Fatalf("item %+v", it)
	}
	it.Click(it, nil)
	if !opened {
		t.Fatal("Forward action not called")
	}
}

func TestTooltipForwardIgnored(t *testing.T) {
	tip := Tooltip(controller.State{Mode: controller.Global, Servers: []string{"x"}, ForwardIgnored: true}, "0.1.0")
	if !strings.Contains(tip, "Forward proxy: not used with this server's plugin") {
		t.Fatalf("%q", tip)
	}
}

// Review #3: tell the user when supervision gives up on the plugin; for a
// replaced file, offer the trust dialog right away.
func TestPluginAlert(t *testing.T) {
	nt := &controller.PluginNotTrustedError{Path: `C:\p\x.exe`, SHA256: "ab"}
	cases := []struct {
		name      string
		prev, cur error
		msg       string
		retrust   bool
	}{
		{"none", nil, nil, "", false},
		{"crash loop", nil, plugin.ErrCrashLoop, "Plugin keeps crashing and was stopped.", false},
		{"replaced file", nil, nt, "", true},
		{"same failure again", plugin.ErrCrashLoop, plugin.ErrCrashLoop, "", false},
	}
	for _, c := range cases {
		msg, retrust := pluginAlert(controller.State{PluginFailure: c.prev}, controller.State{PluginFailure: c.cur})
		if msg != c.msg || retrust != c.retrust {
			t.Errorf("%s: %q %v", c.name, msg, retrust)
		}
	}
}
