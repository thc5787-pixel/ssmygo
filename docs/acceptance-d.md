# P1-D acceptance (geosite online update, user-rule.txt, auto-reload)

Build: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o build/ssmygo-d.exe ./cmd/ssmygo` → 18.63 MB (C: 18.36 MB).

Automated coverage: `go test ./...` (download and sha256 check against
local test servers: OK, checksum mismatch, custom source without a sum,
over 20 MB, not a dlc, missing groups, host-only error texts, fetching
through the local port; the user-rule parser for every supported and
skipped form; user sets ahead of geosite in the core's routes; in the
controller, real connections to "localhost" through the local port that go
direct or to the server depending on geosite and user rules: a manual update
adopting a new group, identical content not rebuilding, broken downloads
keeping the rules and dlc.dat, the weekly check only when stale, user-rule
edits/rename-saves/deletes applied within 3 s, an unreadable file keeping
the previous rules, global mode leaving the system proxy alone; the PAC
menu, the actions and their messages headless). The checks below need the
real internet, a browser and an editor, so the user runs them.

| # | Check | Steps | Expected | Result |
|---|---|---|---|---|
| 1 | Menu | Right-click the tray | A "PAC" submenu after Servers with "Update Local PAC from Geosite" and "Edit User Rule for Geosite..." (Chinese UI: 从 Geosite 更新本地 PAC / 编辑 Geosite 的用户规则...) | ☐ |
| 2 | Manual update through the proxy | With a working server selected (geosite's host blocked direct), PAC ▸ Update Local PAC from Geosite | Within a minute: "PAC updated"; `dlc.dat` appears next to gui-config.json; `ssmygoGeositeUpdatedAt` is set; the log names only github.com | ☐ |
| 3 | Up to date | Run the update again | "No updates found. Please report to Geosite if you have problems with it."; rules not rebuilt (cache\domainsets files unchanged) | ☐ |
| 4 | No server | Fresh config without servers, direct internet; update | Fetched direct; "PAC updated" | ☐ |
| 5 | Failure keeps rules | Set `"geositeUrl"` to a URL that returns an HTML page (or 404); update | A notification with the reason (e.g. "not a geosite file" or "HTTP 404"); browsing in PAC mode unchanged; dlc.dat unchanged | ☐ |
| 6 | Checksum | Set `"geositeUrl"` to a mirror of dlc.dat and `"geositeSha256sumUrl"` to a sum of a different file; update | "Geosite update failed: checksum mismatch."; nothing adopted | ☐ |
| 7 | Weekly check | Set `ssmygoGeositeUpdatedAt` to 8 days ago; restart | The tray works at once; shortly after, "PAC updated" (or nothing new but the date moves); with 6 days ago, no download | ☐ |
| 8 | Edit user rules | PAC ▸ Edit User Rule for Geosite... | user-rule.txt opens in the default editor (created with the commented template the first time; an existing file is not overwritten) | ☐ |
| 9 | User proxy rule | In PAC mode, add `\|\|ipinfo.io^` (or another site geosite sends direct), save | Within ~2 s the browser shows the server's IP for that site (no restart) | ☐ |
| 10 | User direct rule | Add `@@\|\|google.com^`, save | Within ~2 s Google is reached directly (fails or shows your own IP where it is blocked); remove the line → back through the proxy | ☐ |
| 11 | Unsupported lines | Add `/ads/` and `\|\|a.com^$third-party`; save | One notification "user-rule.txt: skipped 2 unsupported rules (lines …)"; the other rules still work; saving again unchanged does not notify again | ☐ |
| 12 | Editors | Save user-rule.txt from Notepad, VS Code and Notepad++ (UTF-8 with BOM too) | Every save applies; no crash | ☐ |
| 13 | Global mode | Switch to Global; edit user-rule.txt; update geosite | Everything still goes through the server; the system proxy setting is untouched | ☐ |
| 14 | Original config | Copy a gui-config.json from the original client with `geositeUrl`/`geositeSha256sumUrl` | Used for updates; after ssmygo saves, the original still reads the file and keeps both fields | ☐ |
| 15 | Broken saved dlc.dat | Replace dlc.dat with a text file; restart | Starts normally with the built-in geosite; PAC mode still routes | ☐ |
