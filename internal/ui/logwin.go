package ui

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
	"github.com/thc5787/ssmygo/internal/logtail"
	"github.com/thc5787/ssmygo/internal/traffic"
)

const (
	logMaxLines  = 2000
	logPollEvery = 500 * time.Millisecond
	chartPoints  = 300 // 5 minutes of 1 s samples (B-D5)
)

// logSource is what the log window needs from the controller.
type logSource interface {
	LogViewer() config.LogViewer
	SaveLogViewer(config.LogViewer) error
	Traffic() traffic.Snapshot
	LogPath() string
	State() controller.State
}

// logView is the Log Viewer (original LogForm): the log text above a
// traffic chart, with a toolbar and View menu toggles.
type logView struct {
	src  logSource
	v    config.LogViewer // working settings, saved on close
	tail *logtail.Tail
	buf  *logtail.Buffer

	lines       []string // shown: filtered and cut
	unavailable string   // why the file cannot be read, or ""
	showPlugin  bool
	list        mui.ListState
	pct         float32 // log pane's share, percent
	snap        traffic.Snapshot
	selected    int // chart point under the pointer, or -1

	fontOpen   bool
	fontFamily string
	fontSize   float64

	onTopMost func(bool) // the real window applies it
}

func newLogView(src logSource) *logView {
	l := &logView{src: src, v: src.LogViewer(), tail: logtail.Open(src.LogPath()), buf: logtail.NewBuffer(logMaxLines),
		showPlugin: src.State().ShowPluginOutput, snap: src.Traffic(), selected: -1}
	l.list.FollowEnd = true
	// logx renames ssmygo.log to ssmygo.1.log when it rotates.
	l.tail.Rotated = strings.TrimSuffix(src.LogPath(), ".log") + ".1.log"
	l.pct = l.v.Splitter * 100
	return l
}

// poll reads what the log gained since the last poll.
func (l *logView) poll() {
	lines, reset, err := l.tail.Poll()
	if err != nil {
		l.unavailable = fmt.Sprintf(i18n.T("ssmygo.logUnavailable"), err)
		return
	}
	l.unavailable = ""
	if reset {
		l.buf.Clear()
	}
	if len(lines) > 0 || reset {
		l.buf.Append(lines...)
		l.render()
	}
}

func (l *logView) render() {
	l.lines = l.lines[:0]
	for _, s := range l.buf.Lines(!l.showPlugin) {
		l.lines = append(l.lines, logtail.CutLine(s))
	}
}

func (l *logView) setShowPlugin(on bool) {
	if on != l.showPlugin {
		l.showPlugin = on
		l.render()
	}
}

func (l *logView) setTraffic(s traffic.Snapshot) { l.snap = s }

// clear empties the view; the file is untouched (as the original).
func (l *logView) clear() {
	l.buf.Clear()
	l.render()
}

func (l *logView) openFont() {
	l.fontFamily, l.fontSize, l.fontOpen = l.v.FontFamily, float64(l.v.FontSize), true
}

func (l *logView) setTopMost(on bool) {
	l.v.TopMost = on
	if l.onTopMost != nil {
		l.onTopMost(on)
	}
}

// close saves the settings; the owner closes the window.
func (l *logView) close() {
	l.v.Splitter = l.pct / 100
	l.src.SaveLogViewer(l.v)
}

var mnemonic = regexp.MustCompile(`\(&.\)|&`)

// plain drops the original's mnemonics ("File", "文件(&F)").
func plain(s string) string { return strings.TrimSpace(mnemonic.ReplaceAllString(s, "")) }

var (
	upColor   = mui.RGB(0xE0, 0x8A, 0x1E) // as the tray's out arrow
	downColor = mui.RGB(0x2E, 0xA0, 0x43) // as the tray's in arrow
)

// View lays out the window.
func (l *logView) View(c *mui.Context) {
	T := func(k string) string { return plain(i18n.T(k)) }
	applyTheme(c)
	t := c.Theme()
	mui.Column(c).Fill().Children(func() {
		if l.v.ToolbarShown {
			mui.Row(c).Gap(t.Space(2)).Padding(t.Space(2)).Children(func() {
				if mui.Button(c, T("Clear Logs")).Clicked() {
					l.clear()
				}
				if mui.Button(c, T("Change Font")).Clicked() {
					l.openFont()
				}
				mui.Checkbox(c, &l.v.WrapText, T("Wrap Text"))
				top := l.v.TopMost
				if mui.Checkbox(c, &top, T("Top Most")).Changed() {
					l.setTopMost(top)
				}
			})
		}
		mui.SplitVerticalPercent(c, &l.pct, func() { l.logPane(c) }, func() { l.trafficPane(c) }).Grow(1)
	})
	mui.Modal(c, &l.fontOpen, func() { l.fontDialog(c) })
}

func (l *logView) logPane(c *mui.Context) {
	t := c.Theme()
	mui.Column(c).Fill().Children(func() {
		if l.unavailable != "" {
			mui.Text(c, l.unavailable).TextColor(t.Danger).Padding(t.Space(1), t.Space(2))
		}
		mui.List(c, &l.list, len(l.lines), func(i int) {
			e := mui.Text(c, l.lines[i]).Font(l.v.FontFamily).FontSize(l.v.FontSize*4/3).Padding(0, t.Space(2)).Selectable()
			if !l.v.WrapText {
				e.NoWrap()
			}
		}).Grow(1)
	})
}

func (l *logView) trafficPane(c *mui.Context) {
	t := c.Theme()
	s := l.snap
	mui.Column(c).Fill().Padding(t.Space(2)).Gap(t.Space(1)).Children(func() {
		mui.Row(c).Gap(t.Space(3)).Children(func() {
			mui.Text(c, "↑ "+traffic.FormatRate(s.Rate.Up)).TextColor(upColor).Bold()
			mui.Text(c, "↓ "+traffic.FormatRate(s.Rate.Down)).TextColor(downColor).Bold()
			mui.Text(c, fmt.Sprintf("%s ↑ %s ↓ %s", i18n.T("ssmygo.total"), traffic.FormatBytes(s.TotalUp), traffic.FormatBytes(s.TotalDown))).
				TextColor(t.TextMuted)
		})
		l.chart(c).Grow(1)
	})
}

// chart plots the up and down rates of the last 5 minutes, newest at the
// right edge, with the y axis in B/s, KB/s or MB/s.
func (l *logView) chart(c *mui.Context) mui.Element {
	t := c.Theme()
	h := l.snap.History
	peak := 1.0
	for _, v := range h {
		peak = math.Max(peak, float64(max(v.Up, v.Down)))
	}
	// Scale in the unit the labels use, so the ticks are round in it.
	unit, name := 1.0, "B/s"
	for _, u := range []string{"KB/s", "MB/s", "GB/s"} {
		if peak/unit < 1024 {
			break
		}
		unit, name = unit*1024, u
	}
	y := chart.NewLinear(0, peak/unit).Nice(4)
	y.Format = func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + " " + name }
	xs := make([]float32, len(h))
	ups, downs := make([]float32, len(h)), make([]float32, len(h))
	for i, v := range h {
		xs[i] = float32(i+chartPoints-len(h)) / float32(chartPoints-1)
		ups[i], downs[i] = y.At(float64(v.Up)/unit), y.At(float64(v.Down)/unit)
	}
	return chart.Plot(c, chart.Axes{Left: y.Ticks(4)}, func(area mui.Element) {
		area.Draw(func(p *mui.Painter, r mui.Rect) {
			for _, tk := range y.Ticks(4) {
				p.Line(r.X, chart.Y(r, tk.At), r.X+r.W, chart.Y(r, tk.At), 1, t.Border)
			}
			if len(h) > 1 {
				p.StrokePath(chart.Line(r, xs, downs, chart.CurveLinear), 1.5, downColor)
				p.StrokePath(chart.Line(r, xs, ups, chart.CurveLinear), 1.5, upColor)
			}
		})
		chart.Select(c, area, &l.selected, xs, func(i int) string {
			return fmt.Sprintf("%ds: ↑ %s ↓ %s", i-len(h)+1, traffic.FormatRate(h[i].Up), traffic.FormatRate(h[i].Down))
		})
		if i := l.selected; i >= 0 && i < len(h) {
			chart.RuleX(c, xs[i], 1).Background(t.TextMuted)
			chart.Anchor(c, xs[i], 1, mui.Center, mui.Start, func() {
				mui.Text(c, fmt.Sprintf("↑ %s ↓ %s", traffic.FormatRate(h[i].Up), traffic.FormatRate(h[i].Down))).
					NoWrap().Padding(2, 6).Radius(4).Background(t.Surface)
			})
		}
	}).Gap(t.Space(2)).MinHeight(60)
}

func (l *logView) fontDialog(c *mui.Context) {
	t := c.Theme()
	families := fontFamilies()
	mui.Text(c, plain(i18n.T("Change Font"))).Bold()
	mui.Form(c, func() {
		mui.Field(c, plain(i18n.T("Change Font")), func() { mui.Select(c, &l.fontFamily, families) })
		mui.Field(c, i18n.T("ssmygo.fontSize"), func() { mui.NumberInput(c, &l.fontSize, 6, 36, 0.5) })
	})
	mui.Row(c).Gap(t.Space(2)).Justify(mui.End).Children(func() {
		if defaultButton(c, i18n.T("OK")).Clicked() {
			if l.fontFamily != "" {
				l.v.FontFamily, l.v.FontSize = l.fontFamily, float32(l.fontSize)
			}
			l.fontOpen = false
		}
		if mui.Button(c, i18n.T("Cancel")).Clicked() {
			l.fontOpen = false
		}
	})
}

var (
	familiesOnce sync.Once
	families     []string
)

// fontFamilies lists the system's fonts, monospaced first (cached).
func fontFamilies() []string {
	familiesOnce.Do(func() {
		var mono, other []string
		for _, f := range mui.FontFamilies() {
			if f.Monospaced {
				mono = append(mono, f.Name)
			} else {
				other = append(other, f.Name)
			}
		}
		families = append(mono, other...)
	})
	return families
}

// logMenuActions are what only the real window can do.
type logMenuActions struct {
	Exit, OpenLocation func()
	TopMost            func(bool)
}

// logMenu is the window's menu bar: File and View, as the original.
func logMenu(l *logView, a logMenuActions) *mygo.Menu {
	T := func(k string) string { return plain(i18n.T(k)) }
	check := func(id, label string, on bool, set func(bool)) *mygo.MenuItem {
		return &mygo.MenuItem{ID: id, Label: label, Type: mygo.MenuItemCheckbox, Checked: on,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) { set(it.IsChecked()) }}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: T("File"), Submenu: []*mygo.MenuItem{
			{ID: "file.open", Label: T("Open Location"), Click: func(*mygo.MenuItem, *mygo.Window) { a.OpenLocation() }},
			{ID: "file.exit", Label: T("Exit"), Click: func(*mygo.MenuItem, *mygo.Window) { a.Exit() }},
		}},
		{Label: T("View"), Submenu: []*mygo.MenuItem{
			{ID: "view.clear", Label: T("Clear Logs"), Click: func(*mygo.MenuItem, *mygo.Window) { l.clear() }},
			{ID: "view.font", Label: T("Change Font"), Click: func(*mygo.MenuItem, *mygo.Window) { l.openFont() }},
			check("view.wrap", T("Wrap Text"), l.v.WrapText, func(on bool) { l.v.WrapText = on }),
			check("view.top", T("Top Most"), l.v.TopMost, func(on bool) { l.setTopMost(on); a.TopMost(on) }),
			mygo.Separator(),
			check("view.toolbar", T("Show Toolbar"), l.v.ToolbarShown, func(on bool) { l.v.ToolbarShown = on }),
		}},
	})
}

var logWin *mygo.Window

func init() { openLogWindow = OpenLogWindow }

// OpenLogWindow shows the Log Viewer; a second call brings it to the front.
func OpenLogWindow(ctrl *controller.Controller) {
	if logWin != nil && !logWin.IsDestroyed() {
		logWin.Show()
		logWin.Focus()
		return
	}
	l := newLogView(ctrl)
	l.poll()
	v := l.v
	win := mygo.NewWindow(mygo.WindowOptions{Title: plain(i18n.T("Log Viewer")), Width: 640, Height: 420, MinWidth: 400, MinHeight: 240,
		StateKey:    "logViewer",                           // MyGo keeps its place, size and maximized state
		AlwaysOnTop: v.TopMost, Content: mui.View(l.View)}) // the only window that can maximize
	l.onTopMost = win.SetAlwaysOnTop
	refreshMenu := func() {
		win.SetMenu(logMenu(l, logMenuActions{
			Exit:         win.Close,
			OpenLocation: func() { mygo.Shell.ShowItemInFolder(l.src.LogPath()) },
			TopMost:      func(bool) {},
		}))
	}
	refreshMenu()

	stop := make(chan struct{})
	go func() {
		tk := time.NewTicker(logPollEvery)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				win.Update(l.poll)
			}
		}
	}()
	offTraffic := ctrl.SubscribeTraffic(func(s traffic.Snapshot) { win.Update(func() { l.setTraffic(s) }) })
	offState := ctrl.Subscribe(func(s controller.State) {
		win.Update(func() { l.setShowPlugin(s.ShowPluginOutput); refreshMenu() })
	})
	win.OnClose(func(*mygo.CloseEvent) { l.close() })
	win.OnClosed(func() {
		close(stop)
		offTraffic()
		offState()
		logWin = nil
	})
	logWin = win
}
