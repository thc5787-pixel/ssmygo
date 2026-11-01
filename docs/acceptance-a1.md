# P1-A1 acceptance (SIP003 plugins, forward proxy)

Build: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o build/ssmygo.exe ./cmd/ssmygo` → 16.47 MB (P0: 16.34 MB).

Automated coverage: `go test ./...` (plugin process tests build a fake SIP003
plugin; forward-proxy tests run in-process SOCKS5/HTTP proxies). The checks
below need real plugins, real proxies or the live system, so they are run by
the user.

| # | Check | Steps | Expected | Result |
|---|---|---|---|---|
| 1 | Real plugin, first run | Put `v2ray-plugin.exe` (or `obfs-local.exe`) next to ssmygo.exe or in `plugins\`; add a server using it; select it from the tray | A dialog asks to run the plugin, showing its path and SHA-256 prefix; after **Run** the server works; the choice is remembered | ☐ |
| 2 | Plugin lives with ssmygo | Task Manager while (1) runs; then quit ssmygo; then kill ssmygo instead of quitting | The plugin process exists while connected and disappears in both cases | ☐ |
| 3 | Plugin crash recovery | Kill only the plugin in Task Manager | Within a few seconds traffic resumes (new plugin process) | ☐ |
| 4 | Plugin updated | Replace the plugin exe with another version, re-select the server | The dialog asks again | ☐ |
| 5 | Wrong plugin options | Set `plugin_opts` to something the plugin rejects | A notification quotes the plugin's own error within about a second | ☐ |
| 6 | Forward proxy SOCKS5 / HTTP | Tray → Forward Proxy…, enter a real proxy (with and without user/password) | Servers are reached through the proxy (check the proxy's log); wrong password → connections fail and the log says why | ☐ |
| 7 | Plugin + forward proxy | Forward proxy on, plugin server selected | Tray tooltip: "Forward proxy: not used with this server's plugin" | ☐ |
| 8 | No freeze | Select a plugin server whose plugin starts slowly | Tray stays responsive | ☐ |
