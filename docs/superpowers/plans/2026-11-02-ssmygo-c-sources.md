# ssmygo P1-C Implementation Plan: online config, server sharing, screen QR scan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** SIP008 subscriptions (window, update, startup update), share a server as `ss://` + QR code, and import from a QR code on screen.

**Architecture:** MyGo-free packages `config` (SIP008 parse, `onlineConfigSource`), `subscribe` (fetch through the local port or direct), `qrcode` (encode/decode), `screencap` (GDI) feed controller operations; `ui` adds two windows, tray items, the scan flow and read-only subscription rows in Edit Servers.

**Tech Stack:** Go 1.27.3, no cgo, MyGo v0.7.4, `github.com/skip2/go-qrcode`, `github.com/makiuchi-d/gozxing`, `golang.org/x/sys/windows`.

**Spec:** `docs/superpowers/specs/2026-11-02-ssmygo-c-sources-design.md`

## Global Constraints

- All earlier constraints hold (no cgo; MyGo only in `cmd/` and `internal/ui`; no secrets in logs; commit trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_0197wVrsfVcKv9s9u4vo2grA`).
- Subscription URLs: shown in full in the UI, logged as host only.
- Fetch: timeout 15 s; body ≤ 1 MB; ≤ 1000 servers; through `http://127.0.0.1:<localPort>` when a server is usable, else direct; no direct retry of a failed proxied attempt.
- Original strings where they exist (i18n.csv / Strings.resx): "Online Config...", "Share Server Config...", "Scan QRCode from Screen...", "No QRCode found. Try to zoom in or move it to the center of the screen.", "Invalid QR Code content: {0}", resx OnlineConfig/ServerSharing window strings; ssmygo strings for the rest.

## Review Focus

1. **Hostile or huge subscription responses** (gzip bomb, deeply nested JSON, 1 MB of servers, HTML login page): bounded memory/time, clear error, old servers kept. → Task 3.
2. **Selection and indices across updates** while the user edits in Edit Servers: no wrong server selected, no lost own-server edits, no index out of range. → Tasks 4, 7.
3. **High-DPI / multi-monitor capture** (150 %, negative-coordinate secondary monitor): every display captured at physical resolution, QR still decodes. → Task 1.
4. **Secrets leaking** (subscription token in logs/notifications, password in "invalid QR" message). → Tasks 3, 7.
5. **Concurrent updates** (Update All at startup while the user presses Update; Remove during an update): no duplicated servers, no resurrected removed subscription. → Task 4.

---

### Task 1: `qrcode` and `screencap` (spike R6 made real)

**Files:** Create `internal/qrcode/{qrcode.go,qrcode_test.go}`, `internal/screencap/{capture_windows.go,capture_test.go}`; `go.mod`.

**Produces:** `qrcode.Encode(text string, px int) ([]byte, error)` (PNG, black on white, quiet zone, medium ECC); `qrcode.Decode(img image.Image) []string` (all codes; tries the image, then 2× upscaled for small codes); `screencap.CaptureAll() ([]image.Image, error)` (one RGBA per display, physical pixels; process made per-monitor DPI aware for the call).

- [ ] Failing tests: `TestRoundTrip` (ss:// AEAD, SS2022 with 44-char key, plugin link with `;` opts); `TestDecodeInScreenshot` (code drawn at 100/150/200 % into a 1920×1080 noisy image → found); `TestDecodeMultiple` (two codes → both); `TestDecodeNone`; `TestCaptureAll` (skip if `CaptureAll` errors with no desktop; else ≥1 image, size ≥ 640×480, not all one colour).
- [ ] Implement; PASS; commit `feat: QR code encode/decode and screen capture`

### Task 2: config — SIP008 and `onlineConfigSource`

**Files:** Create `internal/config/sip008.go`, `sip008_test.go`; modify `guiconfig.go`.

**Produces:** `ParseSIP008(b []byte, group string) (servers []Server, skipped int, err error)` (permissive search; sets `Group`; validates each with `Validate()`; cap 1000; `err` if no servers found or not JSON); `Config.OnlineConfigSource []string json:"onlineConfigSource"`; `SortByGroup(servers []Server) []Server` (own servers first, then groups in first-appearance order).

- [ ] Failing tests: `TestSIP008Official` (version 1 doc with id/remarks/plugin/plugin_opts); `TestSIP008Permissive` (servers nested in objects and arrays, like the original); `TestSIP008SkipsInvalid`; `TestSIP008Cap` (1005 → 1000, skipped 5); `TestSIP008Errors` (HTML, `{}`, `[]`); `TestSortByGroup`; `TestOnlineConfigSourceRoundTrip`.
- [ ] Implement; PASS; commit `feat(config): SIP008 parsing and onlineConfigSource`

### Task 3: `subscribe.Fetch`

**Files:** Create `internal/subscribe/{fetch.go,fetch_test.go}`.

**Consumes:** Task 2. **Produces:** `Fetch(ctx context.Context, url string, proxyPort int) (servers []config.Server, skipped int, err error)`; `Host(url string) string` (for logs).

- HTTP client: `Proxy` = `http://127.0.0.1:proxyPort` if > 0 else none (never the system proxy); timeout 15 s; `io.LimitReader(body, 1<<20+1)` → "response larger than 1 MB"; status ≠ 200 → "HTTP <code>"; transparent gzip is Go's default (decompressed bytes are what the limit counts — Review Focus 1); JSON nesting is bounded by the 1 MB limit.
- Errors never contain the URL's path or query (Review Focus 4): wrap `url.Error` to show `Host(url)` only.

- [ ] Failing tests: `TestFetchOK`; `TestFetch404`; `TestFetchTimeout` (server sleeps; ctx 200 ms); `TestFetchTooLarge` (also gzip-encoded 10 MB of spaces → error); `TestFetchNotJSON`; `TestFetchThroughProxy` (in-process `core.Runner` with no server = direct client on a local port; httptest target; request counted by the runner's meter > 0); `TestFetchErrorHidesToken` (URL `https://127.0.0.1:1/sub?token=SECRET` → error text lacks "SECRET").
- [ ] Implement; PASS; commit `feat(subscribe): fetch SIP008 subscriptions`

### Task 4: controller — subscription operations

**Files:** Modify `internal/controller/{controller.go,state.go}`, new `subscriptions.go`, `subscriptions_test.go`.

**Consumes:** Tasks 2, 3. **Produces:**
```go
type SubResult struct{ Servers, Skipped int; Err error; At time.Time }
func (c *Controller) Subscriptions() []string
func (c *Controller) SubscriptionResults() map[string]SubResult
func (c *Controller) AddSubscription(url string) error            // validates http(s), no duplicate; saves; does not fetch
func (c *Controller) RemoveSubscription(url string) error         // removes URL and its servers
func (c *Controller) UpdateSubscription(ctx context.Context, url string) (SubResult, error)
func (c *Controller) UpdateAllSubscriptions(ctx context.Context) (failed []string)
var ErrBadSubscriptionURL, ErrDuplicateSubscription error
```
- Fetch runs **without** the controller lock; the merge runs under it via `commit` and re-checks that the URL is still subscribed (Review Focus 5: removed during fetch → result dropped). Updates are serialized by a separate `subMu` so two updates of the same URL never interleave.
- Proxy port: `cfg.LocalPort` when `unusable(cfg) == nil` and no plugin failure, else 0 (C-D1).
- Selection per spec §2.1; replacement selection sets `lastNotice` for the UI (State gains `Notice string`, cleared on read by the tray).
- `Start` launches `UpdateAllSubscriptions` in a goroutine after the core is up (C-D3); failures → `State.Notice`.

- [ ] Failing tests (httptest SIP008 servers): `TestUpdateReplacesOnlyGroup`; `TestUpdateKeepsSelection`; `TestUpdateSelectionFallback` (selected subscription server vanished → first of group; group empty → first; Notice set); `TestUpdateFailureKeepsServers`; `TestSubscriptionsSortedAfterOwn`; `TestRemoveDuringUpdate` (handler blocks until Remove done → no servers resurrected); `TestConcurrentUpdatesNoDuplicates`; `TestStartupUpdateDoesNotBlockStart` (handler sleeps 2 s; Start returns < 500 ms); `TestAddSubscriptionValidation`.
- [ ] Implement; PASS; commit `feat(controller): online config subscriptions`

### Task 5: Online Config window

**Files:** Create `internal/ui/{onlinewin.go,onlinewin_test.go}`; i18n.

**Consumes:** Task 4. **Produces:** `OpenOnlineConfigWindow(ctrl)`; `onlineView` with an injectable `subSource` interface (Subscriptions, SubscriptionResults, AddSubscription, RemoveSubscription, UpdateSubscription, UpdateAllSubscriptions) and `run` hook like ForwardWindow.

- Layout spec §3.1; list rows with status; buttons disabled while busy; Remove confirmation via `mui.Modal` with server count; Add validation under the field; Add → Update.

- [ ] Failing tests: `TestOnlineAddValidation` (ftp:// and duplicate → error text; https → added and updated); `TestOnlineBusyDisablesButtons`; `TestOnlineRemoveConfirm` (Cancel keeps, Remove removes); `TestOnlineRowStatus` ("✓ 12", "⚠"); snapshots light/dark (SNAP_DIR).
- [ ] Implement; PASS; commit `feat(ui): online config window`

### Task 6: Share Server Config window

**Files:** Create `internal/ui/{sharewin.go,sharewin_test.go}`.

**Consumes:** Task 1 (`qrcode.Encode`), `config.FormatURL`. **Produces:** `OpenShareWindow(ctrl)`; `shareView{servers []config.Server; sel int}`.

- [ ] Failing tests: `TestShareSelectsCurrent`; `TestShareLinkMatchesSelection`; `TestShareQRDecodesToLink` (render the QR PNG the view shows → `qrcode.Decode` == link); `TestShareCopy` (copy hook gets the link); snapshots.
- [ ] Implement; PASS; commit `feat(ui): share server config window`

### Task 7: tray, scan flow, read-only subscription rows, acceptance

**Files:** Modify `internal/ui/{tray.go,tray_test.go,configwin.go,configwin_test.go}`, new `internal/ui/scan.go`, `scan_test.go`; i18n; `docs/acceptance-c.md`.

**Consumes:** Tasks 1, 4, 5, 6.

- Menu per spec §3.3. `scanScreen(capture, decode, import, askSubscribe, notify)` pure flow in `scan.go` (testable with fakes); the tray wires the real ones off the main thread. "Invalid QR Code content" masks the password via `config.ParseURLs` → `FormatURL` with password replaced by `***` when parsable, else shows only the scheme and host part.
- Edit Servers: rows with `Group != ""` read-only with "From subscription <host>" (`subscribe.Host`); Delete/Duplicate disabled for them; the tray's subscription-change refresh keeps own-server edits (merge: own rows from the window, group rows from the controller).
- Tray shows `State.Notice` once as a notification.

- [ ] Failing tests: `TestScanImportsSS`; `TestScanOffersSubscription` (http link → ask; yes → AddSubscription+Update; no → nothing); `TestScanNone` (original message); `TestScanInvalidMasksPassword`; `TestScanCaptureError`; `TestMenuHasCItems`; `TestConfigWinSubscriptionRowsReadOnly`; `TestConfigWinMergeKeepsOwnEdits`.
- [ ] Implement; PASS; build exe to `build/ssmygo.new.exe`; write `docs/acceptance-c.md`; commit `feat(ui): QR scan, sharing and subscriptions in the tray; C acceptance list`
