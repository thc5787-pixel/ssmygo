# P1-C acceptance (online config, server sharing, screen QR scan)

Build: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o build/ssmygo.exe ./cmd/ssmygo` → 18.36 MB (B: 16.70 MB; the QR libraries add ~1.7 MB).

Automated coverage: `go test ./...` (SIP008 parsing, fetching through the
local port or direct with size/time limits, subscription merge and selection
rules, QR encode/decode including scaled codes, screen capture of this
machine, both windows headless, the scan flow with fakes, read-only
subscription rows). The checks below need real subscriptions, real screens
and a phone, so the user runs them.

| # | Check | Steps | Expected | Result |
|---|---|---|---|---|
| 1 | Add a subscription | Tray → Online Config…; paste a real SIP008 URL; Add | Row shows "✓ N"; servers appear in the tray's Servers menu after your own; the log shows only the host, not the token | ☐ |
| 2 | Through the proxy | With a working server selected, Update a subscription whose site is blocked direct | Update succeeds (fetched through ssmygo) | ☐ |
| 3 | No server yet | Fresh config, no servers; add a reachable subscription | Update succeeds (fetched direct) | ☐ |
| 4 | Failure keeps servers | Add a subscription, then make it unreachable (wrong URL path); Update | Row shows "⚠" with the reason on hover; its servers stay | ☐ |
| 5 | Startup update | Restart ssmygo with subscriptions | They update in the background; the tray works at once; a notification only if some fail | ☐ |
| 6 | Selection kept | Select a subscription server; Update | Same server stays selected; if it disappeared, a notification names the new one | ☐ |
| 7 | Remove | Remove a subscription | Confirmation shows the server count; its servers disappear | ☐ |
| 8 | Read-only rows | Edit Servers with a subscription server selected | Note "From subscription <host>"; fields, Delete, Duplicate, Move disabled; own servers unaffected; an update while the window is open keeps unsaved own edits | ☐ |
| 9 | Share | Servers → Share Server Config…; scan the QR code with a phone client (Shadowrocket, SagerNet, etc.) | Phone imports the server and connects; Copy puts the same ss:// link on the clipboard | ☐ |
| 10 | Scan ss:// | Show an ss:// QR code on screen (e.g. Share window of another machine, or a web page); Servers → Scan QRCode from Screen… | Server imported, Edit Servers opens | ☐ |
| 11 | Scan on high DPI / second monitor | Repeat 10 with the code on a 150 %/200 % display and on a secondary monitor left of the primary | Found | ☐ |
| 12 | Scan a subscription link | Show a QR code of an https subscription URL; scan | Asks "Add as online config (SIP008) subscription?" with the URL; Add adds and updates; Cancel does nothing; no browser opens | ☐ |
| 13 | Nothing found | Scan with no QR code on screen | "No QRCode found. Try to zoom in or move it to the center of the screen." | ☐ |
| 14 | Original config | Copy a gui-config.json from the original client with `onlineConfigSource` and grouped servers | Subscriptions listed; servers marked as from subscription; the original can still read the file after ssmygo saves | ☐ |
