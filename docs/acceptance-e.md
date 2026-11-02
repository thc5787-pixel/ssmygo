# P1-E acceptance (Load Balance / High Availability, global hotkeys)

Build: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o build/ssmygo-e.exe ./cmd/ssmygo` → 18.46 MB (C: 18.36 MB).

Automated coverage: `go test ./...` (strategy and hotkey fields round-trip
in gui-config.json; original hotkey strings ↔ MyGo accelerators; core goldens
`strategy-balancing`, `strategy-ha`, `strategy-forward`; plugin servers
excluded; fallback without members; load balance over two local servers
uses both; high availability switches away from a dead server; controller
strategy, toggle and previous/next operations with the system proxy fake;
the tray's strategy items and tooltip; registration with a fake registrar,
including a taken key; the Hotkeys window headless: recording, Backspace,
status dots, Register all, Save, Cancel). Tests never register real
hotkeys. The checks below need real servers and the real keyboard, so the
user runs them.

| # | Check | Steps | Expected | Result |
|---|---|---|---|---|
| 1 | Load Balance | Two working servers; Servers → Load Balance; browse several sites | Tray tooltip shows "Load Balance"; no server is checked; both servers carry connections (check each server's own logs or traffic) | ☐ |
| 2 | High Availability | Two servers, the first one stopped (or a wrong port); Servers → High Availability; wait about 30 s | Browsing works after the first probe; tooltip shows "High Availability" | ☐ |
| 3 | HA starts on the current server | Select a working server, then High Availability; browse at once | Works immediately (the current server is used until the first probe) | ☐ |
| 4 | Plugin servers excluded | Add a server with a plugin; open the Servers menu | Strategy items read "… (plugin servers excluded)"; with a strategy on, no plugin process starts | ☐ |
| 5 | No members | Only plugin servers; choose Load Balance | Notification "No server can join the strategy…"; the selected server keeps working | ☐ |
| 6 | Selecting a server clears it | Strategy on; click a server in the menu, or press OK in Edit Servers | Strategy items unchecked; that server checked; `strategy` is null in gui-config.json | ☐ |
| 7 | Strategy with forward proxy and rules | Forward proxy on, PAC mode, Load Balance | Proxied sites work (every server dials through the forward proxy); direct sites go direct | ☐ |
| 8 | UDP | Load Balance without forward proxy; a UDP app (e.g. DNS over SOCKS5 UDP, a game) | UDP works through the strategy | ☐ |
| 9 | Hotkeys window | Tray → Edit Hotkeys… (after "Allow other Devices to connect") | 320×260 window with six rows, "Register hotkeys at startup", Register all / Save / Cancel; light and dark themes readable | ☐ |
| 10 | Record | Click a field; press Ctrl+Shift+Q | Field shows "Ctrl+Shift+Q"; a lone Ctrl or a key without modifier records nothing; Backspace clears | ☐ |
| 11 | Register all | Record keys for all six rows; Register all; press each | Green dots; each action works: system proxy toggles (and returns to the last mode), PAC ↔ Global, LAN toggles, log window opens, previous/next server (wrapping, clearing a strategy) | ☐ |
| 12 | Taken key | Record a combination another program holds (e.g. one registered by another tray app), Register all | That row's dot is yellow with an explanation on hover; the others are green and work | ☐ |
| 13 | Rapid presses | Press the Allow-LAN hotkey quickly 6 times | The tray follows; final state equals the start (even count); no hang | ☐ |
| 14 | Save | Check "Register hotkeys at startup"; Save; restart ssmygo | The hotkeys work after restart without opening the window | ☐ |
| 15 | Not at startup by default | Uncheck it; Save; restart | No hotkey is registered until Register all | ☐ |
| 16 | Cancel | Register all, change a field, Cancel | Nothing saved; the registered hotkeys keep working | ☐ |
| 17 | Original config | Copy a gui-config.json from the original client with `strategy` and `hotkey` set (e.g. `Ctrl+Alt+D1`) | Strategy and hotkeys shown; after ssmygo saves, the original reads the file and keeps them | ☐ |
