# P1-B acceptance (traffic stats, log window, tray badge)

Build: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o build/ssmygo.exe ./cmd/ssmygo` → 16.70 MB (A2: 16.50 MB).

Automated coverage: `go test ./...` (byte-exact TCP and UDP counting through
the local port, sampler and history, log following with rotation, the log
window headless in light and dark, tray badge and tooltip). The checks below
need real traffic, a real tray and a real desktop, so the user runs them.

| # | Check | Steps | Expected | Result |
|---|---|---|---|---|
| 1 | Rates are plausible | Start a large download through ssmygo (browser via the system proxy); open Help ▸ Show Logs… | ↓ rate in the window is close to Task Manager's network rate for the browser; the chart rises; Total ↓ grows | ☐ |
| 2 | Upload | Upload a large file | ↑ rate and the orange line rise | ☐ |
| 3 | Tray badge | Watch the tray icon during (1) and when idle | Green ↓ arrow while downloading, orange ↑ while uploading, both when both; plain icon a second after traffic stops; no flicker when idle | ☐ |
| 4 | Tooltip | Hover the tray icon during and after (1) | "↑ … ↓ …" line while traffic flows, gone when idle | ☐ |
| 5 | UDP counted | Run a UDP app through ssmygo (acceptance-a2 #1) | Rates move | ☐ |
| 6 | Core restart keeps totals | Switch servers while the window is open | Totals and chart continue, no reset | ☐ |
| 7 | Plugin output filter | Plugin server running; toggle Help ▸ Show Plugin Output | `msg="plugin output"` lines appear when on, disappear when off; the setting survives a restart | ☐ |
| 8 | Follows the end | Scroll up in the log, wait for new lines; scroll back to the end | Reading position kept while scrolled up; follows again at the end | ☐ |
| 9 | Rotation | Leave the window open while the log passes 5 MB (or replace `logs\ssmygo.log`) | Window clears and continues with the new file; ssmygo keeps logging (rotation not blocked) | ☐ |
| 10 | Toolbar and menus | View ▸ Show Toolbar; Clear Logs; Change Font; Wrap Text; Top Most; File ▸ Open Location; File ▸ Exit | Each works; Open Location shows ssmygo.log selected in Explorer | ☐ |
| 11 | Settings persist | Change font, wrap, top most, toolbar, divider, size and position; close and reopen the window; restart ssmygo | All restored | ☐ |
| 12 | Original config | Copy a gui-config.json written by the original client (with `logViewer`) | Loads; its Font is used; BackgroundColor/TextColor are still in the file after ssmygo saves | ☐ |
| 13 | Single window | Click Show Logs… twice | One window, brought to the front | ☐ |
