package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/traffic"
)

func TestServersMenuStrategies(t *testing.T) {
	var chose string
	a := MenuActions{SetStrategy: func(s string) { chose = s }}
	s := controller.State{Servers: []string{"Tokyo", "Osaka"}, Current: 1, Strategy: controller.StrategyHA}
	m := BuildMenu(s, a)
	var servers []string
	for _, it := range m.Items()[1].Submenu {
		servers = append(servers, it.ID)
	}
	if !slices.Equal(servers[:4], []string{"strategy.balancing", "strategy.ha", "", "server.0"}) {
		t.Fatalf("servers submenu %q", servers)
	}
	lb, ha := m.ItemByID("strategy.balancing"), m.ItemByID("strategy.ha")
	if lb.Label != "Load Balance" || ha.Label != "High Availability" || lb.IsChecked() || !ha.IsChecked() {
		t.Fatalf("strategy items %q %v / %q %v", lb.Label, lb.IsChecked(), ha.Label, ha.IsChecked())
	}
	if m.ItemByID("server.0").IsChecked() || m.ItemByID("server.1").IsChecked() {
		t.Fatal("no server is checked while a strategy is on")
	}
	lb.Click(lb, nil)
	if chose != controller.StrategyBalancing {
		t.Fatalf("SetStrategy got %q", chose)
	}
	s.StrategyExcluded = 2
	m = BuildMenu(s, a)
	if l := m.ItemByID("strategy.ha").Label; l != "High Availability (plugin servers excluded)" {
		t.Fatalf("label %q", l)
	}
	s.Strategy = controller.StrategyNone
	m = BuildMenu(s, a)
	if !m.ItemByID("server.1").IsChecked() || m.ItemByID("strategy.ha").IsChecked() {
		t.Fatal("without a strategy the current server is checked")
	}
}

func TestServersMenuStrategiesNeedServers(t *testing.T) {
	m := BuildMenu(controller.State{}, MenuActions{})
	if m.ItemByID("strategy.balancing").IsEnabled() || m.ItemByID("strategy.ha").IsEnabled() {
		t.Fatal("strategies enabled without servers")
	}
}

func TestTooltipShowsStrategy(t *testing.T) {
	s := controller.State{Mode: controller.Global, Servers: []string{"Tokyo"}, Strategy: controller.StrategyBalancing}
	tip := Tooltip(s, "0.1.0", traffic.Sample{})
	if !strings.Contains(tip, "Load Balance") || strings.Contains(tip, "Tokyo") {
		t.Fatalf("tooltip %q", tip)
	}
}

func TestMenuEditHotkeysAfterLAN(t *testing.T) {
	opened := false
	m := BuildMenu(controller.State{}, MenuActions{EditHotkeys: func() { opened = true }})
	items := m.Items()
	for i, it := range items {
		if it.ID == "lan" {
			if next := items[i+1]; next.ID != "hotkeys" || next.Label != "Edit Hotkeys..." {
				t.Fatalf("after lan: %q %q", next.ID, next.Label)
			}
		}
	}
	it := m.ItemByID("hotkeys")
	it.Click(it, nil)
	if !opened {
		t.Fatal("EditHotkeys not called")
	}
}

// fakeRegistrar records registrations and refuses the accelerators in taken.
type fakeRegistrar struct {
	taken      map[string]bool
	registered map[string]func()
	unregAll   int
}

func newFakeRegistrar(taken ...string) *fakeRegistrar {
	f := &fakeRegistrar{taken: map[string]bool{}, registered: map[string]func(){}}
	for _, t := range taken {
		f.taken[t] = true
	}
	return f
}

func (f *fakeRegistrar) register(acc string, fn func()) error {
	if f.taken[acc] || f.registered[acc] != nil {
		return errors.New("taken")
	}
	f.registered[acc] = fn
	return nil
}

func (f *fakeRegistrar) unregisterAll() { f.unregAll++; clear(f.registered) }

func TestRegisterAll(t *testing.T) {
	var ran []string
	acts := HotkeyActions{}
	for _, n := range HotkeyNames {
		acts[n] = func() { ran = append(ran, n) }
	}
	h := config.HotkeyConfig{SwitchSystemProxy: "Ctrl+Shift+Q", SwitchSystemProxyMode: "Ctrl+Alt+D1",
		SwitchAllowLan: "Ctrl+Alt+L", ShowLogs: "garbage", ServerMoveDown: "Ctrl+Shift+Q"}
	f := newFakeRegistrar("Ctrl+Alt+L")
	res := registerAll(h, acts, f.register, f.unregisterAll)
	if f.unregAll != 1 {
		t.Fatal("existing hotkeys not unregistered first")
	}
	if res["SwitchSystemProxy"] != nil || res["SwitchSystemProxyMode"] != nil {
		t.Fatalf("results %v", res)
	}
	if !errors.Is(res["ShowLogs"], ErrHotkeyParse) {
		t.Fatalf("garbage: %v", res["ShowLogs"])
	}
	if res["SwitchAllowLan"] == nil || errors.Is(res["SwitchAllowLan"], ErrHotkeyParse) {
		t.Fatalf("taken key: %v", res["SwitchAllowLan"])
	}
	if res["ServerMoveDown"] == nil {
		t.Fatal("a duplicate of another hotkey must be reported")
	}
	if _, ok := res["ServerMoveUp"]; ok {
		t.Fatal("empty hotkeys have no result")
	}
	f.registered["Ctrl+Alt+1"]()
	f.registered["Ctrl+Shift+Q"]()
	if !slices.Equal(ran, []string{"SwitchSystemProxyMode", "SwitchSystemProxy"}) {
		t.Fatalf("ran %v", ran)
	}
}

func TestRegisterAtStartup(t *testing.T) {
	h := config.HotkeyConfig{SwitchSystemProxy: "Ctrl+Shift+Q"}
	f := newFakeRegistrar()
	registerAtStartup(h, HotkeyActions{}, f.register, f.unregisterAll, func(string) {})
	if len(f.registered) != 0 || f.unregAll != 0 {
		t.Fatal("registered without RegHotkeysAtStartup")
	}
	h.RegHotkeysAtStartup = true
	h.ShowLogs = "Ctrl+Alt+L"
	f.taken["Ctrl+Alt+L"] = true
	var warned string
	registerAtStartup(h, HotkeyActions{}, f.register, f.unregisterAll, func(m string) { warned = m })
	if f.registered["Ctrl+Shift+Q"] == nil {
		t.Fatal("not registered at startup")
	}
	if warned == "" {
		t.Fatal("a failed hotkey at startup is not reported")
	}
}

// The hotkey queue runs actions one after another, off the caller.
func TestHotkeyQueue(t *testing.T) {
	q := newActionQueue(4)
	done := make(chan int, 10)
	for i := range 3 {
		q.push(func() { done <- i })
	}
	for want := range 3 {
		if got := <-done; got != want {
			t.Fatalf("order: got %d want %d", got, want)
		}
	}
}
