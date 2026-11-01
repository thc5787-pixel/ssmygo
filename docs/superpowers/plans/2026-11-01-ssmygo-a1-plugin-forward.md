# ssmygo P1-A1 Implementation Plan: SIP003 plugins and forward proxy

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Servers with SIP003 plugins work (plugin process managed by ssmygo, confirmed before first run), and a SOCKS5/HTTP forward proxy can carry connections to the SS server.

**Architecture:** `internal/core/plugin` runs plugin processes (Job Object, readiness probe, restart with backoff); `internal/core/forward` registers `fwd-socks5`/`fwd-http` client protocols with a dial timeout; vendored shadowsocks-go gets patch C (`dialerClient`, `dialTimeout`) so the `proxy` client can dial through `forward`. The controller owns plugin lifecycle and the trust list; the UI adds the Forward Proxy window and a trust confirmation dialog.

**Tech Stack:** Go 1.27.3, MyGo v0.7.4, vendored shadowsocks-go (patches A, B, C), `golang.org/x/sys/windows` v0.49.0.

**Spec:** `docs/superpowers/specs/2026-11-01-ssmygo-a1-plugin-forward-design.md` (decisions A1-D1…D7; read §2 for the original client's exact behaviour).

## Global Constraints

- Everything in the P0 plan's Global Constraints still holds (module path, no cgo, AGPL, only `cmd/` and `internal/ui` import MyGo, no passwords in logs, commit trailer).
- Commit messages end with:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_0197wVrsfVcKv9s9u4vo2grA
  ```
- Plugin env vars exactly: `SS_REMOTE_HOST`, `SS_REMOTE_PORT`, `SS_LOCAL_HOST` (=`127.0.0.1`), `SS_LOCAL_PORT`, `SS_PLUGIN_OPTIONS`. `plugin_args` is passed as the raw Windows command line tail after `%VAR%` expansion (those five plus the process environment). Working directory = exe directory. `CREATE_NO_WINDOW` (0x08000000). Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` (0x2000).
- Plugin readiness: probe `127.0.0.1:<local port>` every 50 ms, give up after 5 s; if the process exits first, fail at once. Keep the last 20 output lines; errors show the last 5.
- Restart backoff 1, 2, 4, 8, 16, 30, 30… s; 3 unexpected exits within 60 s → `plugin.ErrCrashLoop`, no more restarts.
- Plugin lookup (A1-D4): absolute path → as is; contains a path separator → relative to exe dir; bare name → `<exeDir>\<name>`, `<exeDir>\plugins\<name>`, then `exec.LookPath`; `.exe` appended when the name has no extension.
- Trust: SHA-256 (lowercase hex) of the resolved file, stored with the cleaned absolute path in `ssmygoTrustedPlugins`; checked on every start, including restarts.
- Forward proxy fields/defaults/limits as in the original (`proxy` section): `proxyType` 0 = SOCKS5, 1 = HTTP; `proxyTimeout` default 3, valid 1–10; used only for the connection to the SS server; ignored when the current server has a plugin (A1-D3).
- New user-visible strings come from the original resx (`Localization/Strings.resx` / `Strings.zh-Hans.resx`, names `ForwardProxy`, `Type`, `NoProxy`, `Details`, `Address`, `Port`, `Timeout`, `CredentialsOptional`, `Username`) or the CSV (`Forward Proxy...`, `Password`, `Cancel`); the rest are `ssmygo.*` keys with hand-written en + zh-Hans.

## Review Focus

1. **Awkward plugin arguments and paths** — `plugin_args` with quotes, spaces and `%SS_LOCAL_PORT%`, plugins under `C:\Program Files\…` or a non-ASCII folder: the plugin receives exactly what the user typed (after expansion). → tests in Task 4.
2. **Plugin that dies immediately** (wrong options, missing DLL): the error appears within about a second and quotes the plugin's own output, rather than a generic 5-second timeout. → test in Task 4.
3. **UI stays responsive while a plugin starts**: tray actions and OK in Edit Servers run controller calls off the main thread, so a slow plugin (up to 5 s) never freezes the tray. → test in Task 8 (action runs on a goroutine; state arrives via subscription).
4. **Credentials with special characters**: forward-proxy username/password containing `:`, `@`, spaces or non-ASCII work for SOCKS5 (≤255 bytes each, else a field error) and HTTP Basic. → tests in Tasks 2 and 3.
5. **Plugin file replaced after trust** (an update, or tampering): the next start, including an automatic restart, asks again instead of running the new file. → test in Task 6.

---

## File Map

| Path | Responsibility | Task |
|---|---|---|
| `third_party/shadowsocks-go/service/{client.go,registry.go,registry_test.go}`, `third_party/PATCHES.md` | patch C | 1 |
| `internal/core/legacyaead/register.go` | factory signature | 1 |
| `internal/config/{forward.go,forward_test.go,guiconfig.go}` | `proxy` section, trusted plugins | 2 |
| `internal/core/forward/{forward.go,forward_test.go}` | `fwd-socks5`, `fwd-http` | 3 |
| `internal/core/plugin/{resolve.go,process.go,job_windows.go,job_other.go,*_test.go,testdata/fakeplugin/main.go}` | plugin processes | 4 |
| `internal/core/{build.go,build_test.go,testdata/golden/*}` | endpoint + forward chain | 5 |
| `internal/controller/{controller.go,plugins.go,state.go,controller_test.go}` | lifecycle, trust, forward proxy | 6 |
| `internal/i18n/{gen/main.go,strings_*.json}` | resx + new strings | 7 |
| `internal/ui/{messages.go,forwardwin.go,trust.go,tray.go,configwin.go,*_test.go}`, `cmd/ssmygo/main.go` | UI | 8 |
| `docs/acceptance-a1.md` | manual checks | 9 |

---

### Task 1: Patch C — `dialerClient` and `dialTimeout`

**Files:** Modify `third_party/shadowsocks-go/service/client.go`, `service/registry.go`; Test `service/registry_test.go`; Modify `internal/core/legacyaead/register.go`, `third_party/PATCHES.md`.

**Interfaces:**
- Produces (package `service`):
  ```go
  // ClientConfig gains:
  DialerClient string           `json:"dialerClient,omitzero"`
  DialTimeout  jsoncfg.Duration `json:"dialTimeout,omitzero"` // read only by registered protocols
  type ClientFactory func(c *ClientConfig, inner netio.StreamClient) (netio.StreamClient, error) // was *netio.TCPClient
  ```
  `innerStreamClient(...) (netio.StreamClient, error)` returns `streamClientByName[c.DialerClient]` when set (error `dialer client %q not found (it must be listed before %q)`), else the direct TCP client. Used by the `none`/`plain`, `socks5`, `http`, `2022-*` branches and the registry branch. `DialerClient` with `EnableUDP` → error `client %q: dialerClient does not carry UDP`.
- Consumes: patch A registry (P0 Task 2).

- [ ] **Step 1: Failing tests** in `registry_test.go`:
  - `TestDialerClientChain`: run a socks5 *server* Manager on a free port counting accepted connections (wrap its listener via a `net.Listener` you start yourself and forward, or count with a TCP proxy in the test); config clients `[{name:"hop",protocol:"socks5",endpoint:<that server>,enableTCP:true},{name:"x",protocol:"none",endpoint:<ssnone server>,enableTCP:true,dialerClient:"hop"}]`; a dial through `x` reaches an echo target and the hop counted 1 connection.
  - `TestDialerClientUnknown`: `dialerClient:"nope"` → error contains `dialer client "nope" not found`.
  - `TestDialerClientOrder`: the hop listed after `x` → same error.
  - `TestDialerClientUDP`: `dialerClient` + `enableUDP:true` + `mtu:1500` → error contains `does not carry UDP`.
  - `TestDialTimeoutReachesFactory`: registered protocol records `c.DialTimeout.Value()`; config `"dialTimeout":"3s"` → recorded `3*time.Second`.
  - Update existing registry tests to the new factory signature.
- [ ] **Step 2:** `cd third_party/shadowsocks-go && go test ./service/ -run 'Dialer|DialTimeout|Register'` → FAIL.
- [ ] **Step 3:** Implement; change `legacyaead.newFromService` to `inner netio.StreamClient`.
- [ ] **Step 4:** `go test ./service/...` in the vendored module → PASS; `go test ./internal/core/...` in the main module → PASS.
- [ ] **Step 5:** Append "Patch C" to `PATCHES.md` (purpose, files, `git diff --stat`).
- [ ] **Step 6: Commit** `feat(third_party): patch C — dialerClient chaining and dialTimeout`

---

### Task 2: config — forward proxy and trusted plugins

**Files:** Create `internal/config/forward.go`; Modify `internal/config/guiconfig.go` (fields, `Default`, `Clone`); Test `internal/config/forward_test.go`.

**Interfaces:**
- Produces: `ForwardProxy`, `TrustedPlugin` exactly as spec §4.1; `Config.Proxy ForwardProxy \`json:"proxy"\``, `Config.TrustedPlugins []TrustedPlugin \`json:"ssmygoTrustedPlugins"\``; constants `ProxySOCKS5 = 0`, `ProxyHTTP = 1`, `DefaultProxyTimeout = 3`, `MaxProxyTimeout = 10`; `func (p ForwardProxy) Validate() []FieldError` (fields `proxyServer`, `proxyPort`, `proxyTimeout`, `authUser`, `authPwd`; nothing checked when `!UseProxy`); `func (p ForwardProxy) LogValue() slog.Value`; `func (c *Config) Trusted(path, sha string) bool`.
- `ForwardProxy` preserves unknown keys of the `proxy` object (same `extra` technique as `Server`).

- [ ] **Step 1: Failing tests:**
  - `TestProxyRoundTrip`: `testdata/original-4.4.1.json` (has a `proxy` object) loads into `Config.Proxy` and saves back deep-equal (existing round-trip test must stay green).
  - `TestProxyDefault`: `Default().Proxy.ProxyTimeout == 3`, `ProxyType == 0`, `!UseProxy`.
  - `TestProxyValidate` table: off → no errors even if empty; on + empty server → `proxyServer`; port 0/65536 → `proxyPort`; timeout 0 and 11 → `proxyTimeout`; 10 → ok; `UseAuth` + empty user → `authUser`; Review Focus 4: user `"a:b@c d"` and password `"密码 p@ss"` → ok for both types; user of 256 bytes with SOCKS5 → `authUser` ("≤255 bytes"); same with HTTP → ok.
  - `TestProxyLogRedacts`: JSON log of `ForwardProxy{AuthPwd:"hunter2"}` lacks `hunter2`.
  - `TestTrusted`: list `[{Path:"C:\\p\\a.exe",SHA256:"ab"}]` → `Trusted("C:\\p\\a.exe","ab")` true, other sha false, other path false; `Clone` deep-copies the list.
- [ ] **Step 2:** `go test ./internal/config/` → FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** PASS (whole package).
- [ ] **Step 5: Commit** `feat(config): forward proxy section and trusted plugins`

---

### Task 3: core/forward — `fwd-socks5` and `fwd-http`

**Files:** Create `internal/core/forward/forward.go`, `internal/core/forward/forward_test.go`.

**Interfaces:**
- Consumes: Task 1 `ClientFactory` (`inner netio.StreamClient`), `ClientConfig.DialTimeout`, `ClientConfig.Socks5{EnableUserPassAuth, Username, Password}`, `ClientConfig.HTTP{Username, Password, UseBasicAuth}`.
- Produces: `const ProtocolSOCKS5 = "fwd-socks5"`, `ProtocolHTTP = "fwd-http"`; registered in `init()`. Each wraps the upstream `socks5.StreamClientConfig`/`httpproxy.ClientConfig` client so `DialStream` runs under `context.WithTimeout(ctx, DialTimeout)` (no timeout when zero).

- [ ] **Step 1: Failing tests** (in-process: a shadowsocks-go Manager with a `socks5` server (optionally with users) and one with an `http` server (optionally Basic auth), each fronted by a counting TCP relay you start in the test; plus an echo target):
  - `TestSOCKS5Hop`: service config with clients `forward(fwd-socks5 → counting relay)` and `x(none → ssnone server, dialerClient:"forward")`; echo through `x` works; relay counted ≥1.
  - `TestHTTPHop`: same with `fwd-http`.
  - `TestAuth/socks5` and `TestAuth/http`: server requires `"a:b@c d"` / `"密码 p@ss"` (Review Focus 4) → works; wrong password → dial error.
  - `TestTimeout`: forward endpoint is a listener that accepts and never answers; `dialTimeout:"1s"`; dial fails in < 2 s.
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS.
- [ ] **Step 5: Commit** `feat(core): forward proxy client protocols with dial timeout`

---

### Task 4: core/plugin — resolve, start, supervise, stop

**Files:** Create `internal/core/plugin/resolve.go`, `process.go`, `job_windows.go`, `job_other.go`, `resolve_test.go`, `process_test.go`, `testdata/fakeplugin/main.go`.

**Interfaces:**
- Produces:
  ```go
  type NotFoundError struct{ Name string; Tried []string }
  func Resolve(name, exeDir string) (string, error)
  func Hash(path string) (string, error) // sha256 lowercase hex
  type Spec struct{ Path, Opts, Args, RemoteHost string; RemotePort int; Dir string }
  type StartError struct{ Name string; Tail []string; Err error } // Tail = last ≤5 lines
  var ErrCrashLoop = errors.New("plugin: crashed 3 times within a minute")
  type Process struct{ /* … */ }
  func Start(s Spec, logger *slog.Logger, verify func(path string) error, onRestart func(port int), onFail func(error)) (*Process, error)
  func (p *Process) LocalPort() int
  func (p *Process) Spec() Spec
  func (p *Process) Stop()
  ```
  `verify` (nil = always ok) runs before every start and restart; the controller passes hash + trust check. `onRestart(port)` after a successful automatic restart (new port); `onFail(ErrCrashLoop)` when giving up; `onFail(err)` when `verify` rejects the file on a restart (Review Focus 5).
- `testdata/fakeplugin/main.go`: reads `SS_*`; `SS_PLUGIN_OPTIONS` keys: `die=1` exit 3 immediately after printing `fake: bad options` to stderr; `slow=1` never listen; `crash-after=N` exit after N accepted connections; otherwise listen on `SS_LOCAL_HOST:SS_LOCAL_PORT` and relay each connection to `SS_REMOTE_HOST:SS_REMOTE_PORT`. It writes `os.Args[1:]` joined with `|` to the file named by `FAKE_ARGS_OUT` if set. Tests build it once with `go build -o <tmp>\fakeplugin.exe` (and a copy under a `C:\…\Program Files ü\` style temp dir for Review Focus 1).

- [ ] **Step 1: Failing tests:**
  - `TestResolveOrder`: temp exe dir with `a.exe` and `plugins\b.exe`; `Resolve("a")`, `Resolve("b")`, `Resolve("b.exe")`, absolute path, `sub\a.exe` relative → expected paths; `Resolve("nope")` → `*NotFoundError` with 3+ tried paths.
  - `TestStartRelays`: fakeplugin in front of an echo server → bytes through `127.0.0.1:LocalPort()` echo.
  - `TestArgsExpanded` (Review Focus 1): exe path containing spaces and `ü`; `Args = "--port %SS_LOCAL_PORT% --name \"a b\""` → `FAKE_ARGS_OUT` has `--port|<port>|--name|a b`.
  - `TestDiesImmediately` (Review Focus 2): `Opts "die=1"` → `*StartError` within 1.5 s, `Tail` contains `fake: bad options`.
  - `TestNeverListens`: `Opts "slow=1"` → `*StartError` after ≈5 s (assert 4.5–6.5 s), process gone.
  - `TestStopKills`: after `Stop()` the PID no longer exists (`os.FindProcess` + signal 0 / `OpenProcess` fails) and `Stop()` again is a no-op.
  - `TestJobKillsOnParentExit` (windows): start fakeplugin from a helper child process (re-exec the test binary with `-test.run=TestHelperParent` and env flag), kill the helper, the plugin PID disappears within 2 s.
  - `TestRestartAfterCrash`: `Opts "crash-after=1"`, `onRestart` receives a new port within 3 s and relaying works on it.
  - `TestCrashLoop`: `Opts "crash-after=0"`-style immediate exits after listening → `onFail(ErrCrashLoop)` and no 4th start.
  - `TestVerifyOnRestart` (Review Focus 5): `verify` returns an error after the first start; crash → `onFail` with that error, no restart.
- [ ] **Step 2:** FAIL. **Step 3:** Implement (Windows: `SysProcAttr{CreationFlags: CREATE_NO_WINDOW, CmdLine: syscall.EscapeArg(path) + " " + expanded}`; job via `CreateJobObject`/`SetInformationJobObject(JobObjectExtendedLimitInformation)`/`AssignProcessToJobObject` on `OpenProcess(PROCESS_SET_QUOTA|PROCESS_TERMINATE)`; non-Windows: plain `exec.Cmd` with args split by `strings.Fields`).
- [ ] **Step 4:** PASS (`go test ./internal/core/plugin/ -count=1`).
- [ ] **Step 5: Commit** `feat(core): SIP003 plugin processes with job object and restart`

---

### Task 5: core.Build — plugin endpoint and forward chain

**Files:** Modify `internal/core/build.go`, `build_test.go`, `runner_test.go`; Create goldens `testdata/golden/{plugin,forward-socks5,forward-http,plugin-and-forward}.json`.

**Interfaces:**
- Produces: `func Build(cfg *config.Config, rs *rules.Result, pluginPort int) (*service.Config, error)` — `pluginPort > 0` means the current server has a running plugin: proxy endpoint `127.0.0.1:<pluginPort>`, no forward. Else when `cfg.Proxy.UseProxy`: client `forward` (`fwd-socks5`/`fwd-http`, endpoint proxy host:port, `dialTimeout` = `proxyTimeout` s, auth fields) first, `proxy.dialerClient = "forward"`. All P0 goldens unchanged.
- Consumes: Tasks 2, 3.

- [ ] **Step 1: Failing tests:** four new goldens (`-update` then review by hand); `TestBuildPluginIgnoresForward`; `TestRunnerThroughForward`: SOCKS5 hop counting relay + aeadtest server; echo through local port works and the hop counted.
- [ ] **Step 2:** FAIL (signature). **Step 3:** Implement; update callers (`controller`) to pass `0` for now. **Step 4:** `go test ./internal/...` PASS.
- [ ] **Step 5: Commit** `feat(core): build plugin endpoints and forward proxy chains`

---

### Task 6: controller — plugin lifecycle, trust, forward proxy

**Files:** Create `internal/controller/plugins.go`; Modify `controller.go`, `state.go`, `controller_test.go`.

**Interfaces:**
- Consumes: Tasks 2, 4, 5. `Deps` gains `ExeDir string` (plugin lookup and working dir).
- Produces:
  ```go
  type PluginNotTrustedError struct{ Path, SHA256 string }
  func (c *Controller) TrustPlugin(path, sha256 string) error
  func (c *Controller) SetForwardProxy(p config.ForwardProxy) error
  // State gains: ForwardIgnored bool
  // removed: ErrPluginUnsupported
  ```
  Plugin errors surface as `*plugin.NotFoundError`, `*PluginNotTrustedError`, `*plugin.StartError`, `plugin.ErrCrashLoop` (in `State.Err`).
  Rules: before each commit whose next current server has a plugin, resolve → hash → trust check → reuse the running process if its `Spec` equals the new one, else start a new one; pass `LocalPort()` to `core.Build`. Commit failure stops a newly started process and keeps the old one. A server without plugin stops any running process. `onRestart` → lock, rebuild core with the new port (no sysproxy change), mark dirty. `onFail` → lock, `lastErr = err`, mark dirty; system proxy untouched. `verify` passed to `plugin.Start` = hash + `Trusted` (Review Focus 5). `Shutdown` stops the process.

- [ ] **Step 1: Failing tests** (fakeplugin built once per package via `TestMain`; `Deps.ExeDir` = a temp dir containing it):
  - `TestPluginServerEndToEnd`: trusted fakeplugin server in front of aeadtest → SOCKS5 echo through local port.
  - `TestUntrustedPluginAsks`: untrusted → `SelectServer` returns `*PluginNotTrustedError` (path + sha) and current server unchanged; `TrustPlugin(path, sha)` then `SelectServer` → ok; on disk `ssmygoTrustedPlugins` has the entry.
  - `TestReplacedPluginAsksAgain` (Review Focus 5): trust, run, then overwrite the exe with different bytes and kill the process → `onFail` path: `State().Err` is `*PluginNotTrustedError`, no new process.
  - `TestPluginNotFoundRollsBack`: plugin `"missing"` → `*plugin.NotFoundError`; old server still serving.
  - `TestSwitchAwayStopsPlugin`: plugin server → plain server → the plugin PID is gone.
  - `TestPluginRestartRebuildsCore`: `crash-after=1` → after the crash, echo works again on the same local port (core rebuilt to the new plugin port).
  - `TestSetForwardProxy`: valid SOCKS5 hop → traffic counted at the hop; invalid (port 0) → `FieldError`, nothing applied; with a plugin server current → `State().ForwardIgnored` true and the hop sees no traffic.
  - Existing tests stay green (remove `TestPluginServerNotUsable`; it is superseded by `TestUntrustedPluginAsks`).
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** `go test ./internal/... -count=1` PASS; MyGo boundary check prints nothing.
- [ ] **Step 5: Commit** `feat(controller): plugin lifecycle, trust list and forward proxy`

---

### Task 7: i18n — resx strings and new messages

**Files:** Modify `internal/i18n/gen/main.go`, regenerate `internal/i18n/strings_en.json`, `strings_zh-Hans.json`; Modify `internal/i18n/i18n_test.go`.

**Interfaces:**
- Produces keys: CSV `Forward Proxy...`; resx (`-resx-en`, `-resx-zh` flags) keys stored as `resx.<Name>` for `ForwardProxy`, `Type`, `NoProxy`, `Details`, `Address`, `Port`, `Timeout`, `CredentialsOptional`, `Username`; literals `SOCKS5`, `HTTP` (same in both); extra `Save` {Save, 保存}; `ssmygo.trustTitle` {"Run plugin program?", "运行插件程序？"}, `ssmygo.trustDetail` {"%s\n\n%s\nSHA-256 %s…\n\nRun it only if you trust where it came from.", "%s\n\n%s\nSHA-256 %s…\n\n请确认来源可信后再运行。"} (name, path, first 16 hex), `ssmygo.trustRun` {"Run", "运行"}, `ssmygo.pluginNotFound` {"Plugin %s not found. Looked in: %s", "找不到插件 %s。已查找：%s"}, `ssmygo.pluginStart` {"Plugin %s did not start: %s", "插件 %s 启动失败：%s"}, `ssmygo.pluginCrashLoop` {"Plugin keeps crashing and was stopped.", "插件反复崩溃，已停止。"}, `ssmygo.forwardIgnored` {"Forward proxy: not used with this server's plugin", "前置代理：当前服务器使用插件，未启用"}. Remove `ssmygo.pluginUnsupported`.

- [ ] **Step 1: Failing test:** `TestResxStrings`: zh-CN `T("resx.NoProxy") == "不使用代理"`, `T("resx.Timeout") == "超时 (秒)"`, `T("Forward Proxy...")` non-empty zh; `TestAllKeysTranslated` stays.
- [ ] **Step 2:** FAIL. **Step 3:** Extend the generator (parse `<data name=…><value>…</value>` with `encoding/xml`), regenerate. **Step 4:** PASS.
- [ ] **Step 5: Commit** `feat(i18n): forward proxy and plugin strings`

---

### Task 8: UI — messages, Forward Proxy window, trust dialog, tray

**Files:** Create `internal/ui/forwardwin.go`, `forwardwin_test.go`, `trust.go`, `trust_test.go`; Modify `messages.go`, `messages_test.go`, `tray.go`, `tray_test.go`, `configwin.go`, `configwin_test.go`, `cmd/ssmygo/main.go` (`ExeDir` in Deps).

**Interfaces:**
- Consumes: Tasks 6, 7.
- Produces:
  ```go
  type ForwardWindow struct{ /* edit copy */ }
  func NewForwardWindow(save func(config.ForwardProxy) error, initial config.ForwardProxy) *ForwardWindow
  func (w *ForwardWindow) View(c *mui.Context)
  func (w *ForwardWindow) OnClose(fn func())
  func OpenForwardWindow(ctrl *controller.Controller) // 280×400, lazy, destroyed on close
  // trust.go
  var confirm = func(title, detail, ok string) bool { /* mygo.Dialog.Message, buttons [ok, Cancel], DefaultButton 0, CancelButton 1 */ }
  func runAction(ctrl *controller.Controller, logger *slog.Logger, act func() error) // goroutine: act(); on *PluginNotTrustedError → confirm → TrustPlugin → act() once more; other errors → Notify(Message(err))
  ```
  Tray actions (`SetMode`, `Select`, `ToggleLAN`) and Edit Servers OK/Apply use `runAction` (Review Focus 3). `Message` covers the new errors with the Task 7 strings. Tray: new item `ID: "forward"`, label `Forward Proxy...`, after the Servers submenu; tooltip adds `ssmygo.forwardIgnored` when `State.ForwardIgnored`. Edit Servers: remove the plugin warning and the plugin red row (P0 deferred fix). Forward window layout = original ③: Type radios (NoProxy/SOCKS5/HTTP), Details (Address, Port, Timeout), Credentials (optional) checkbox + Username, Password (password masked), Save/Cancel; disabled states per spec §4.7; `Field.Error` from `ForwardProxy.Validate`; Esc = Cancel.

- [ ] **Step 1: Failing tests:**
  - `TestForwardWindowFields`: all labels present (en); `NoProxy` selected → Address disabled; auth unchecked → Username disabled.
  - `TestForwardWindowValidation`: SOCKS5 + empty address → error shown, save not called; valid → save called with exact struct (timeout parsed).
  - `TestRunActionTrustFlow`: fake `confirm` returning true; `act` fails first with `*PluginNotTrustedError` → `TrustPlugin` called with its path/sha, `act` called twice; `confirm` false → `act` once, nothing trusted.
  - `TestRunActionOffMainThread` (Review Focus 3): `act` blocks 300 ms; `runAction` returns in < 50 ms.
  - `TestMessagePluginErrors`: en/zh for `NotFoundError`, `StartError` (contains tail line), `ErrCrashLoop`.
  - `TestBuildMenuForward`: item `forward` exists after Servers; `TestTooltipForwardIgnored`.
  - Update `TestPluginServerWarned` → `TestPluginServerNotWarned` (no warning text, row not red).
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** `go test ./... -count=1` PASS; render forward window snapshot light/dark (SNAP_DIR) and review.
- [ ] **Step 5: Commit** `feat(ui): forward proxy window, plugin trust dialog, off-thread actions`

---

### Task 9: Release build and acceptance list

**Files:** Create `docs/acceptance-a1.md`.

- [ ] **Step 1:** `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o build/ssmygo.new.exe ./cmd/ssmygo` (swap in beside a running copy as before); record size.
- [ ] **Step 2:** Write `docs/acceptance-a1.md` with user-run checks: a real `v2ray-plugin` / `simple-obfs` server (first-run dialog, works, Task Manager shows the plugin under ssmygo; quitting ssmygo ends it); kill the plugin in Task Manager → traffic resumes; forward proxy through a real SOCKS5/HTTP proxy (e.g. a corporate one) with and without auth; plugin server + forward proxy → tooltip note.
- [ ] **Step 3: Commit** `docs: A1 acceptance checklist`
