# ssmygo P1-B Implementation Plan: traffic stats, log window, tray badge

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Live up/down traffic counted at the local port (TCP + UDP), shown as a tray badge, a tooltip line and a chart in a new log viewer that follows `ssmygo.log` and can hide plugin output.

**Architecture:** One app-lifetime `traffic.Meter` (two atomic counters) is handed by `core.Runner` to every shadowsocks-go server through a `json:"-"` field (patch A4); `mixed` counts TCP, the UDP NAT relay counts UDP. The controller samples it every second into a 300-point history and publishes snapshots on a separate subscription; the tray and the log window consume them. A MyGo-free `logtail` package follows the log file.

**Tech Stack:** Go 1.27.3, no cgo, MyGo v0.7.4 (`ui`, `ui/chart`), vendored shadowsocks-go (patches A, B, C, A2, A3, A4).

**Spec:** `docs/superpowers/specs/2026-11-02-ssmygo-b-monitoring-design.md`

## Global Constraints

- All P0/A1/A2 constraints hold: no cgo; MyGo imported only by `cmd/` and `internal/ui`; no secrets in logs; commit trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_0197wVrsfVcKv9s9u4vo2grA`.
- Up (↑, outbound) = bytes the app sends through ssmygo; down (↓, inbound) = bytes it receives. Payload bytes only (no encryption or protocol overhead).
- Sampling every 1 s; history 300 samples; totals for this run only (not saved).
- Log view: initial read last 64 KB from the first full line; poll every 0.5 s; keep ≤ 2000 lines; display lines cut at 4 KB + "…"; plugin lines are `msg="plugin output"`.
- Log window default 640×420, minimum 400×240; default font Consolas 9 pt when `logViewer.Font` is missing.
- `logViewer` original fields: `topMost`, `wrapText`, `toolbarShown`, `Font` (string like `"Consolas, 8pt"`), `BackgroundColor`, `TextColor` (kept, unused); ssmygo fields: `width`, `height`, `left`, `top`, `maximized`, `splitter`. Top-level `showPluginOutput` (default false). Unknown fields kept.
- Tooltip ≤ 127 runes; rates line only while traffic flows.

## Review Focus

1. **Huge or binary log lines** (a plugin dumping a blob, invalid UTF-8): the window stays responsive, lines cut at 4 KB, invalid bytes shown as U+FFFD, never a crash. → Task 6.
2. **Log rotation while the window is open** (`logx` renames at 5 MB): no duplicated or lost tail, no stuck reader holding the old file open on Windows (rename must still succeed). → Task 6.
3. **Counting under concurrency and on every relay path** (HTTP CONNECT, plain HTTP GET, SOCKS5, `ReadFrom`/`WriteTo` fast paths): bytes counted once, not twice and not zero. → Task 3.
4. **Burst of traffic toggling the badge** (badge flips every second): icon updated only on state change, no handle growth, never from a non-main thread. → Task 7.
5. **Config written by the original client** (`Font` with odd format, `BackgroundColor` names, missing `logViewer`): loads with defaults, writes the original fields back unchanged. → Task 4.

---

### Task 1: `internal/traffic` — Meter and Sampler

**Files:** Create `internal/traffic/{traffic.go,traffic_test.go,format.go}`.

**Interfaces — Produces:**
```go
type Meter struct{ /* atomic */ }
func (m *Meter) Add(up, down uint64)
func (m *Meter) Totals() (up, down uint64)
type Sample struct{ Up, Down uint64 }
type Snapshot struct { Rate Sample; TotalUp, TotalDown uint64; History []Sample }
func NewSampler(m *Meter, keep int) *Sampler
func (s *Sampler) Tick() Snapshot   // one sample = delta since the previous Tick
func (s *Sampler) Snapshot() Snapshot // copy; safe from any goroutine
func FormatRate(bps uint64) string    // "0 B/s", "12.3 KB/s", "1.5 MB/s" (1024-based, 1 decimal above B)
func FormatBytes(n uint64) string     // "88 MB" style, same rules without "/s"
```

- [ ] Failing tests: `TestMeterConcurrentAdd` (100 goroutines × 1000 `Add(1,2)` → totals 100000/200000); `TestSamplerRates` (Add(10,20), Tick → Rate {10,20}; Tick again → {0,0}; History oldest→newest); `TestSamplerCap` (305 Ticks, keep 300 → len 300, first is tick #6); `TestSnapshotIsCopy` (mutating returned History does not affect next Snapshot); `TestFormatRate` table (0 → "0 B/s", 1023 → "1023 B/s", 12595 → "12.3 KB/s", 1572864 → "1.5 MB/s").
- [ ] Implement; PASS; commit `feat(traffic): meter and sampler`

### Task 2: Patch A4 — TrafficMeter in shadowsocks-go UDP relay; Runner hands out the meter

**Files:** Modify `third_party/shadowsocks-go/service/{server.go,udp_nat.go}`, new `service/meter.go`; test `service/meter_test.go`; `third_party/PATCHES.md` "Patch A4"; `internal/core/{runner.go,runner_test.go}`; `internal/controller/controller.go` (NewRunner call).

**Interfaces — Consumes:** Task 1 `*traffic.Meter` (satisfies the interface). **Produces:**
```go
// service
type TrafficMeter interface{ Add(up, down uint64) }
// ServerConfig field: TrafficMeter TrafficMeter `json:"-"`
// core
func NewRunner(logger *tslog.Logger, meter service.TrafficMeter) *Runner // nil = no counting
```
- `udp_nat.go`: `Add(uint64(queuedPacket.length), 0)` after a successful client-side unpack in the receive loop; `Add(0, uint64(payloadLength))` after a successful write of a reply to the client in `relayNatConnToServerConnGeneric`. Meter passed via `NewUDPNATRelay` (new parameter) from `sc.TrafficMeter`. nil → no calls.
- `Runner.Apply` sets `sc.Servers[i].TrafficMeter = r.meter` for every server before `NewManager`.

- [ ] Failing tests:
  - vendored `TestUDPNATRelayCountsPayload`: Manager with a `socks5` UDP server (TrafficMeter = test counter) + `direct` client; send 100-byte datagram to a UDP echo via SOCKS5 UDP → counter up 100, down 100 (payload, not SOCKS header).
  - `internal/core` `TestRunnerCountsUDPPayload`: legacy aes-256-gcm via `aeadtest.StartUDP`, global mode; 1200-byte datagram → meter +1200/+1200 exactly.
  - `TestRunnerNilMeter`: `NewRunner(logger, nil)` UDP echo still works.
- [ ] Implement; vendored `go test ./service/...` and `go test ./internal/...` PASS; PATCHES.md; commit `feat(third_party): patch A4 — traffic meter in the UDP relay`

### Task 3: `mixed` counts TCP; totals survive re-Apply

**Files:** Modify `internal/core/mixed/{server.go,register.go,server_test.go}`, new `mixed/count.go`; `internal/core/runner_test.go`.

**Interfaces — Consumes:** `service.ServerConfig.TrafficMeter` (Task 2). **Produces:** `New(enableUDP bool, meter service.TrafficMeter) (*Server, error)`; register factories pass `sc.TrafficMeter`.

- Count payload only (spec §2): wrap the `netio.ConnRequest` returned by the inner SOCKS5/HTTP server — its `PendingConn` becomes a wrapper whose `Proceed()` returns a counting conn, and `len(req.Payload)` is added as up when `Proceed` succeeds. Counting conn: `Read` → `Add(n,0)`, `Write` → `Add(0,n)`; if the inner conn has `ReadFrom`/`WriteTo` fast paths, the wrapper implements them and counts the returned n; every other `netio.Conn` method forwards (CloseWrite, CloseRead, deadlines). Handshake bytes are not counted.

- [ ] Failing tests (Review Focus 3):
  - `TestCountsSocks5` — SOCKS5 CONNECT to a TCP echo, 1000 bytes sent and echoed → meter exactly up 1000, down 1000.
  - `TestCountsHTTPConnect` — same through HTTP CONNECT → exactly 1000/1000.
  - `TestCountsHTTPGet` — plain GET via the HTTP proxy to an httptest server with a 10 KB body → down ≥ 10240 and ≤ 10240+1024 (proxy forwards headers), up > 0.
  - `TestCountsHalfClose` — client half-closes after sending 500 bytes; the 500-byte reply still counted → 500/500.
  - `TestNilMeter` — `New(false, nil)` works as before.
  - `internal/core` `TestRunnerTotalsSurviveReapply` — 500 bytes echo, Apply again, 500 more → totals exactly 1000/1000.
- [ ] Implement; PASS; commit `feat(core): count TCP at the local port`

### Task 4: config — `logViewer` and `showPluginOutput`

**Files:** Create `internal/config/logviewer.go`, `logviewer_test.go`; modify `guiconfig.go` (+ testdata).

**Interfaces — Produces:**
```go
type LogViewer struct {
    TopMost, WrapText, ToolbarShown bool
    FontFamily string; FontSize float32 // from/to the original "Consolas, 8pt"
    Width, Height, Left, Top int; Maximized bool; Splitter float32
    extra map[string]json.RawMessage // includes BackgroundColor, TextColor
}
func DefaultLogViewer() LogViewer // Consolas 9, 640×420, splitter 0.7, toolbar shown false (original default)
// Config fields: LogViewer LogViewer `json:"logViewer"`; ShowPluginOutput bool `json:"showPluginOutput"`
func (c *Config) SetLogViewer(v LogViewer) // keeps v's extra from c's current value
```
- `Font` parse: `"<family>, <size>pt"`, size may have decimals ("8.25pt"); unparsable → defaults, raw string kept so it is written back unchanged unless the user changes the font.

- [ ] Failing tests (Review Focus 5): `TestLogViewerRoundTrip` (original JSON with `BackgroundColor: "Black"`, `TextColor: "White"`, `Font: "Consolas, 8pt"`, unknown `"x":1` → load/save → those keys byte-equal); `TestLogViewerMissing` (no `logViewer` → `DefaultLogViewer()`); `TestFontParse` table ("Consolas, 8pt" → Consolas 8; "Microsoft YaHei UI, 8.25pt" → 8.25; "garbage" → defaults + raw kept); `TestShowPluginOutputDefaultFalse`.
- [ ] Implement; PASS; commit `feat(config): logViewer and showPluginOutput`

### Task 5: controller — sampler loop, traffic subscription, log viewer settings

**Files:** Modify `internal/controller/{controller.go,state.go}`, new `traffic.go`, `traffic_test.go`.

**Interfaces — Consumes:** Tasks 1, 2, 4. **Produces:**
```go
func (c *Controller) SubscribeTraffic(fn func(traffic.Snapshot)) (off func())
func (c *Controller) Traffic() traffic.Snapshot
func (c *Controller) LogViewer() config.LogViewer
func (c *Controller) SaveLogViewer(v config.LogViewer) error   // saves gui-config.json
func (c *Controller) SetShowPluginOutput(on bool) error         // saves; State().ShowPluginOutput
func (c *Controller) LogPath() string                           // paths.Logs()/ssmygo.log
// State gains: ShowPluginOutput bool
```
- `Start` launches a 1 s ticker goroutine (stopped by `Close`/`Stop`) calling `sampler.Tick()` and the traffic subscribers (outside any lock). Ticker interval injectable via an unexported field for tests.

- [ ] Failing tests: `TestTrafficSubscription` (interval 10 ms; TCP echo through the local port; a snapshot with TotalUp > 0 arrives within 1 s); `TestTrafficOffStops`; `TestSaveLogViewer` (save → reload controller → equal, extra kept); `TestSetShowPluginOutput` (State flag + persisted).
- [ ] Implement; PASS; commit `feat(controller): traffic sampling and log viewer settings`

### Task 6: `internal/logtail` — follow the log file

**Files:** Create `internal/logtail/{tail.go,tail_test.go}`.

**Interfaces — Produces:**
```go
type Tail struct{ /* path, offset, file identity, partial line */ }
func Open(path string) *Tail
// Poll reads what was appended since the last call. On the first call it
// starts 64 KB before the end, at the next line start. Returns complete
// lines only (a trailing partial line waits). err != nil only for
// "file not available" (missing/unreadable); the next Poll retries.
func (t *Tail) Poll() (lines []string, reset bool, err error) // reset: file rotated/replaced/truncated, caller clears
func CutLine(s string) string            // ≤ 4096 bytes + "…", valid UTF-8 (invalid → U+FFFD)
func IsPluginOutput(line string) bool    // contains ` msg="plugin output"`
type Buffer struct{ /* ring of ≤ max lines */ }
func NewBuffer(max int) *Buffer
func (b *Buffer) Append(lines ...string); func (b *Buffer) Clear(); func (b *Buffer) Lines(filterPlugin bool) []string
```
- Open the file per Poll (read then close) so `logx` rotation (rename) is never blocked on Windows (Review Focus 2). Detect rotation by size < offset or by file identity change (`os.SameFile` against the last stat).

- [ ] Failing tests: `TestPollAppends`; `TestPollPartialLine` (write "abc" then "def\n" → one line "abcdef"); `TestFirstPollLast64KB` (200 KB file → first line is a complete line, total bytes ≤ 64 KB); `TestRotationRename` (rename to `.1`, new file written → reset=true, new lines only, and the rename itself succeeded while a Tail exists); `TestTruncate`; `TestMissingFileThenAppears`; `TestCutLine` (5000 'a' → 4096+"…"; `"\xff\xfe"` → "\uFFFD\uFFFD"); `TestBufferCap` (2500 appended → 2000 newest); `TestPluginFilter`.
- [ ] Implement; PASS; commit `feat(logtail): follow the log file`

### Task 7: tray — badge, rates line, Help menu

**Files:** Modify `internal/ui/{icons.go,icons_test.go,tray.go,tray_test.go}`; i18n gen keys + JSON.

**Interfaces — Consumes:** Task 5. **Produces:**
```go
type Badge int // BadgeNone, BadgeIn, BadgeOut, BadgeBoth
func BadgeFor(rate traffic.Sample) Badge
func TrayIcon(mode controller.Mode, darkTaskbar bool, b Badge) []byte // cached composite
func Tooltip(s controller.State, version string, rate traffic.Sample) string
// MenuActions gains: ShowLogs func(); TogglePluginOutput func(bool)
```
- Badge drawing: down arrow (in, bottom-left 12×12) and up arrow (out, bottom-right 12×12), drawn with `image/draw` over the existing PNG, in the accent colours of the tray icon set; composites cached in a map.
- Help submenu (original order): "Show Logs..." (`ShowLogs`), "Show Plugin Output" checkbox (`State.ShowPluginOutput`). Placed above Quit, after the LAN item separator.
- Tray: `SubscribeTraffic` → compute badge and rates; when badge or rates line changed, `RunOnMain` to `SetIcon`/`SetToolTip` (Review Focus 4: only on change). Icon errors logged once.
- Strings from the original CSV (add to `keys`): "Help", "Show Logs...", "Show Plugin Output". Regenerate with `go run ./internal/i18n/gen -csv C:\Users\a\Desktop\shadowsocks-windows\shadowsocks-csharp\Data\i18n.csv -resx-en …\Localization\Strings.resx -resx-zh …\Localization\Strings.zh-Hans.resx -out internal/i18n`.

- [ ] Failing tests: `TestBadgeFor` table; `TestTrayIconBadgesDiffer` (4 badges → 4 distinct PNGs, decodable, 32×32, cached: same slice pointer on second call); `TestTooltipRates` (rate {0,0} → no "↑"; {12595, 348160} → contains "↑ 12.3 KB/s ↓ 340.0 KB/s"; total ≤ 127 runes with a long server name); `TestHelpMenu` (items present; checkbox reflects `ShowPluginOutput`; clicks call actions); `TestBadgeUpdatesOnlyOnChange` (feed 5 snapshots with same badge → 1 SetIcon via an injected setter).
- [ ] Implement; PASS; commit `feat(ui): tray traffic badge and Help menu`

### Task 8: log window

**Files:** Create `internal/ui/{logwin.go,logwin_test.go,fontwin.go}`; modify `tray.go` (ShowLogs → `OpenLogWindow`), i18n; `docs/acceptance-b.md`.

**Interfaces — Consumes:** Tasks 5, 6, 7. **Produces:** `func OpenLogWindow(ctrl *controller.Controller)` (single instance; second call focuses), and for tests `newLogView(ctrl logSource) *logView` with `logSource` = {LogViewer, SaveLogViewer, Traffic, SubscribeTraffic, LogPath, State, Subscribe} so the view runs under `ui.NewTester` with a fake.

- Layout per spec §3.6: window menu (`Window.SetMenu`) File{"&Open Location", "E&xit"} / View{"&Clear Logs", "Change &Font", "&Wrap Text", "&Top Most", separator, "&Show Toolbar"}; toolbar (Clear Logs, Font…, Wrap Text, Top Most) shown per `ToolbarShown`; `SplitVerticalPercent` with `Splitter`; top: read-only `TextAreaBuffer`, follows end only when scrolled to the end; bottom: rates/totals header + `chart.Plot` with two `chart.Line`s (up, down) over History, y unit auto (B/s, KB/s, MB/s), hover `chart.Select` shows the value.
- 0.5 s timer → `Tail.Poll` → `Buffer` → text; reset clears; error shows "Log file not available: %s" line (ssmygo string) and keeps polling.
- `State().ShowPluginOutput` change → re-render from `Buffer.Lines(filter)`.
- Font window: family list (`ui.FontFamilies()`), size field (6–36), OK/Cancel.
- Close: `SaveLogViewer` with size/position/maximized/splitter/toggles.
- Strings: original "Log Viewer", "&File", "&Open Location", "E&xit", "&View", "&Clear Logs", "Change &Font", "&Wrap Text", "&Top Most", "&Show Toolbar", "&Font", "Inbound", "Outbound" (add any missing from the CSV as ssmygo strings with the original text); ssmygo: `ssmygo.logUnavailable` {"Log file not available: %s", "日志文件不可用：%s"}, `ssmygo.total` {"Total", "累计"}, `ssmygo.fontSize` {"Size", "字号"}.

- [ ] Failing tests (headless `ui.NewTester`, fake source + temp log file): `TestLogViewShowsTail`; `TestLogViewPluginFilter` (toggle State → plugin line disappears/appears); `TestLogViewClear`; `TestLogViewToolbarToggle`; `TestLogViewChartSnapshot` (light/dark × en/zh snapshots, fixed History); `TestLogViewSavesOnClose`; `TestLogViewHugeLine` (Review Focus 1: 1 MB line with invalid UTF-8 → shown cut, no panic); `TestOpenLogWindowSingleInstance` (via injectable opener counter).
- [ ] Implement; PASS; build exe to `build/ssmygo.new.exe`; write `docs/acceptance-b.md` (real download → chart and rates plausible vs Task Manager; badge flashes in/out; plugin output toggle; rotation at 5 MB while open; font/wrap/top-most persist after reopening; original gui-config with logViewer loads); commit `feat(ui): log viewer with traffic chart; B acceptance list`
