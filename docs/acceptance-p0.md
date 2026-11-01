# P0 acceptance (ssmygo 0.1.0)

Build: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o build/ssmygo.exe ./cmd/ssmygo`

## Measured (2026-11-01, Windows 11 26200)

| Item | Target | Result |
|---|---|---|
| exe size | ≤ 15 MB | **16.3 MB — over target** (shadowsocks-go stack ≈ +5 MB over MyGo alone; UPX rejected for AV false positives) |
| start → proxy listening | ≤ 1 s | 651 ms |
| tray-only working set | ≤ 40 MB | 38 MB on every launch after the first; the very first launch of a new exe path shows ~85 MB WS, of which only ~6 MB more private bytes (OS first-run image paging) |
| system proxy untouched while Disabled | — | verified (registry before = after) |
| full test suite | green | 10 packages ok |

## Manual checks — to be run by the user

These change live system state (system proxy, clipboard, autostart, killing the
process), so they were not run by the implementer. Automated coverage is listed
for each.

| # | Check | Steps | Automated coverage | Result |
|---|---|---|---|---|
| 1 | Original config migrates | Copy a real 4.x `gui-config.json` next to the exe, start | config round-trip test on a 4.4.1 sample | ☐ |
| 2 | PAC / Global / Disable | Tray → System Proxy → PAC: a `cn` site direct (log shows route `direct`), a `geolocation-!cn` site via server; Global: all via server; Disable: Windows Settings › Proxy shows the previous values | controller + core tests (fake sysproxy); WinINet only read-tested | ☐ |
| 3 | Clipboard import | Copy two `ss://` links → Servers → Import URL from Clipboard | ssurl + controller tests | ☐ |
| 4 | Busy port | Edit Servers → Proxy Port = a port in use → OK | controller rollback test | ☐ |
| 5 | Crash recovery | PAC on → kill ssmygo in Task Manager → start again → Disable restores the original proxy | controller crash-recovery tests | ☐ |
| 6 | Autostart / single instance | Toggle Start on Boot (Settings › Apps › Startup lists ssmygo); launch a second copy → Edit Servers comes to front | — | ☐ |
| 7 | WinINet write path | `go test -tags sysproxy_e2e ./internal/sysproxy/` (sets and restores the real proxy) | struct-layout + live read tests | ☐ |
