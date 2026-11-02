package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
	"github.com/thc5787/ssmygo/internal/traffic"
)

type fakeLogSource struct {
	path   string
	v      config.LogViewer
	saved  []config.LogViewer
	snap   traffic.Snapshot
	plugin bool
}

func (f *fakeLogSource) LogViewer() config.LogViewer { return f.v }
func (f *fakeLogSource) SaveLogViewer(v config.LogViewer) error {
	f.saved = append(f.saved, v)
	return nil
}
func (f *fakeLogSource) Traffic() traffic.Snapshot { return f.snap }
func (f *fakeLogSource) LogPath() string           { return f.path }
func (f *fakeLogSource) State() controller.State {
	return controller.State{ShowPluginOutput: f.plugin}
}

const pluginLine = `time=2026-11-02T10:00:00.000+08:00 level=INFO msg="plugin output" plugin=v2ray-plugin line="hello from plugin"`

func newLogTest(t *testing.T, content string) (*logView, *fakeLogSource, *mui.Tester) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ssmygo.log")
	if content != "" {
		os.WriteFile(p, []byte(content), 0o644)
	}
	src := &fakeLogSource{path: p, v: config.DefaultLogViewer()}
	l := newLogView(src)
	l.poll()
	tt := mui.NewTester(l.View, 640, 420)
	tt.Frame()
	return l, src, tt
}

func TestLogViewShowsTail(t *testing.T) {
	l, src, tt := newLogTest(t, "first line\nsecond line\n")
	if !tt.HasText("first line") || !tt.HasText("second line") {
		t.Fatalf("texts %q", tt.Texts())
	}
	f, _ := os.OpenFile(src.path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("third line\n")
	f.Close()
	l.poll()
	tt.Frame()
	if !tt.HasText("third line") {
		t.Fatal("appended line not shown")
	}
}

func TestLogViewUnavailable(t *testing.T) {
	_, _, tt := newLogTest(t, "")
	found := false
	for _, s := range tt.Texts() {
		if strings.HasPrefix(s, "Log file not available") {
			found = true
		}
	}
	if !found {
		t.Fatalf("texts %q", tt.Texts())
	}
}

func TestLogViewPluginFilter(t *testing.T) {
	l, src, tt := newLogTest(t, "plain\n"+pluginLine+"\n")
	if tt.HasText(pluginLine) {
		t.Fatal("plugin output shown while ShowPluginOutput is off")
	}
	src.plugin = true
	l.setShowPlugin(true)
	tt.Frame()
	if !tt.HasText(pluginLine) {
		t.Fatal("plugin output hidden while ShowPluginOutput is on")
	}
}

func TestLogViewClear(t *testing.T) {
	l, _, tt := newLogTest(t, "old line\n")
	l.clear()
	tt.Frame()
	if tt.HasText("old line") {
		t.Fatal("clear left the line")
	}
}

func TestLogViewToolbarToggle(t *testing.T) {
	l, _, tt := newLogTest(t, "x\n")
	if tt.HasText("Clear Logs") {
		t.Fatal("toolbar shown by default (original default is hidden)")
	}
	l.v.ToolbarShown = true
	tt.Frame()
	for _, s := range []string{"Clear Logs", "Change Font", "Wrap Text", "Top Most"} {
		if !tt.HasText(s) {
			t.Errorf("toolbar lacks %q", s)
		}
	}
	tt.Click("Wrap Text")
	if !l.v.WrapText {
		t.Fatal("Wrap Text checkbox did nothing")
	}
}

func TestLogViewRates(t *testing.T) {
	l, _, tt := newLogTest(t, "x\n")
	l.setTraffic(traffic.Snapshot{Rate: traffic.Sample{Up: 12595, Down: 348160}, TotalUp: 5 << 20, TotalDown: 88 << 20,
		History: []traffic.Sample{{Up: 1, Down: 2}, {Up: 12595, Down: 348160}}})
	tt.Frame()
	for _, s := range []string{"↑ 12.3 KB/s", "↓ 340.0 KB/s", "5.0 MB", "88.0 MB"} {
		found := false
		for _, txt := range tt.Texts() {
			if strings.Contains(txt, s) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %q in %q", s, tt.Texts())
		}
	}
}

func TestLogViewSettingsOnClose(t *testing.T) {
	l, src, _ := newLogTest(t, "x\n")
	l.v.WrapText, l.pct = true, 50 // the divider moved to the middle
	l.close()
	if len(src.saved) != 1 || !src.saved[0].WrapText || src.saved[0].Splitter != 0.5 {
		t.Fatalf("saved %+v", src.saved)
	}
}

// Review Focus 1: a 1 MB line with invalid UTF-8 is shown cut, no panic.
func TestLogViewHugeLine(t *testing.T) {
	_, _, tt := newLogTest(t, "\xff"+strings.Repeat("z", 1<<20)+"\n")
	for _, s := range tt.Texts() {
		if len(s) > 4096+len("…")+len("�") {
			t.Fatalf("line of %d bytes shown", len(s))
		}
	}
}

func TestLogMenu(t *testing.T) {
	l, _, _ := newLogTest(t, "x\n")
	exited, opened := false, false
	m := logMenu(l, logMenuActions{Exit: func() { exited = true }, OpenLocation: func() { opened = true }, TopMost: func(bool) {}})
	for _, id := range []string{"file.open", "file.exit", "view.clear", "view.font", "view.wrap", "view.top", "view.toolbar"} {
		if m.ItemByID(id) == nil {
			t.Fatalf("menu lacks %s", id)
		}
	}
	m.ItemByID("file.exit").Click(m.ItemByID("file.exit"), nil)
	m.ItemByID("file.open").Click(m.ItemByID("file.open"), nil)
	it := m.ItemByID("view.toolbar")
	it.Checked = true
	it.Click(it, nil)
	if !exited || !opened || !l.v.ToolbarShown {
		t.Fatalf("exit %v open %v toolbar %v", exited, opened, l.v.ToolbarShown)
	}
}

func TestLogViewSnapshots(t *testing.T) {
	dir := os.Getenv("SNAP_DIR")
	if dir == "" {
		t.Skip("SNAP_DIR unset")
	}
	hist := make([]traffic.Sample, 120)
	for i := range hist {
		hist[i] = traffic.Sample{Up: uint64(i * 300), Down: uint64((i % 40) * 20000)}
	}
	for _, dark := range []bool{false, true} {
		l, _, tt := newLogTest(t, "time=2026-11-02T10:00:00.000+08:00 level=INFO msg=Ready services=2\ntime=2026-11-02T10:00:01.000+08:00 level=WARN msg=\"plugin exited; restarting\"\n")
		l.v.ToolbarShown = true
		l.setTraffic(traffic.Snapshot{Rate: hist[len(hist)-1], TotalUp: 3 << 20, TotalDown: 70 << 20, History: hist})
		tt.SetDark(dark)
		tt.Frame()
		name := map[bool]string{false: "logwin-light.png", true: "logwin-dark.png"}[dark]
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, tt.Image())
		f.Close()
	}
}

// The Log Viewer's menus and toolbar are translated (their keys are stored
// without the original's "&" access keys).
func TestLogMenuTranslated(t *testing.T) {
	i18n.SetLocale("zh-CN")
	defer i18n.SetLocale("en")
	l, _, tt := newLogTest(t, "x\n")
	m := logMenu(l, logMenuActions{Exit: func() {}, OpenLocation: func() {}, TopMost: func(bool) {}})
	for id, key := range map[string]string{"file.open": "Open Location", "file.exit": "Exit", "view.clear": "Clear Logs",
		"view.font": "Change Font", "view.wrap": "Wrap Text", "view.top": "Top Most", "view.toolbar": "Show Toolbar"} {
		if got, want := m.ItemByID(id).Label, i18n.T(key); got != want || want == key {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}
	l.v.ToolbarShown = true
	tt.Frame()
	if !tt.HasText(i18n.T("Clear Logs")) {
		t.Fatalf("toolbar not translated: %q", tt.Texts())
	}
}
