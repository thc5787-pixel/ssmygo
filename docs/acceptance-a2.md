# P1-A2 acceptance (UDP relay)

Build: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o build/ssmygo.exe ./cmd/ssmygo` → 16.50 MB (A1: 16.47 MB).

Automated coverage: `go test ./...`. It covers the legacy AEAD UDP client
against an in-process server and go-shadowsocks2; SOCKS5 UDP ASSOCIATE
through the local port for legacy AEAD, SS2022 and none; 20 parallel UDP
sessions; plugin servers (UDP goes straight to the server; it fails closed
while the plugin is down); refusal while the forward proxy is on; and a busy
UDP port. The checks below need a real server, real UDP apps or the live
system, so the user runs them.

UDP needs a server with UDP enabled (shadowsocks-rust: `-U` or
`"mode": "tcp_and_udp"`; shadowsocks-libev: `-u`).

| # | Check | Steps | Expected | Result |
|---|---|---|---|---|
| 1 | DNS over UDP | With a SOCKS5-UDP-capable client (for example `dnsproxy -u 8.8.8.8:53 --socks5 127.0.0.1:1080`, or Proxifier/SocksCap with "UDP through proxy"), resolve a name | The answer arrives; the server's log shows a UDP relay | ☐ |
| 2 | Game / voice app | Route a UDP app (game client, Discord voice, QUIC browser with a SOCKS5-UDP tool) through 127.0.0.1:1080 | Works as with the original client | ☐ |
| 3 | Every cipher | Repeat (1) with a legacy AEAD server (aes-256-gcm or chacha20-ietf-poly1305) and a 2022-blake3 server | Both answer | ☐ |
| 4 | Plugin server | Select a v2ray-plugin / obfs server whose server side also listens for UDP on the same port; repeat (1) | DNS answers (UDP bypasses the plugin by design); TCP still uses the plugin | ☐ |
| 5 | Forward proxy on | Tray → Forward Proxy… on; repeat (1) | The tool reports UDP ASSOCIATE refused (or falls back to TCP); tray tooltip shows "UDP: off (forward proxy in use)" / "UDP：已关闭（使用前置代理）" | ☐ |
| 6 | Forward proxy off again | Turn the forward proxy off | The tooltip line disappears; (1) works again | ☐ |
| 7 | UDP port taken | Start something that binds UDP 1080 only (e.g. `ncat -u -l 1080`), then start ssmygo or change the port to 1080 | "Port 1080 is in use by another program." — same as a busy TCP port | ☐ |
| 8 | Server without UDP | Use a server with UDP disabled; repeat (1) | No answer; the app times out; TCP keeps working | ☐ |
