# third_party patches

## shadowsocks-go

- Upstream: https://github.com/database64128/shadowsocks-go
- Commit: 65ba171eff9787ee8e1a9f4c55bc7272a2fa994c (2026-10-30)
- Vendored: 2026-11-01, `git archive` of the commit, `.github/` removed.
- License: AGPL-3.0 (`shadowsocks-go/LICENSE`).


## Patch A — protocol registry

Purpose: let ssmygo add the `mixed` inbound and legacy AEAD clients from
`internal/` without editing the built-in protocol switches per protocol.

- `service/registry.go` (new): `RegisterClientProtocol`, `RegisterServerProtocol`, lookups.
- `service/client.go`: `ClientConfig.Password` field; the `default:` branch of
  `AddClient`'s protocol switch consults the registry (TCP only; UDP → error).
- `service/server.go`: the `default:` branch of `TCPRelay`'s protocol switch
  consults the registry.

## Patch B — Manager.Start / Manager.Stop

Purpose: synchronous start so callers get bind errors immediately.

- `service/service.go`: `Run` split into `Start(ctx) error` and idempotent
  `Stop() error`; `Run` keeps its signature and behaviour.

Tests for both: `service/registry_test.go`.

Diff vs upstream:
     third_party/shadowsocks-go/service/client.go  | 24 +++++++-
     third_party/shadowsocks-go/service/server.go  | 10 +++-
     third_party/shadowsocks-go/service/service.go | 80 +++++++++++++++++----------
     3 files changed, 82 insertions(+), 32 deletions(-)
    third_party/shadowsocks-go/service/registry.go (new), service/registry_test.go (new)

## Patch C — dialerClient chaining and dialTimeout

Purpose: let ssmygo's SS client dial through a forward proxy client
(P1-A1), and give registered protocols a connection timeout.

- `service/client.go`: `ClientConfig.DialerClient` (name of an earlier
  client to dial through) and `ClientConfig.DialTimeout` (read only by
  registered protocols); `innerStreamClient` used by the none/plain, socks5,
  http, 2022 and registry branches (`direct` unchanged); DialerClient with
  UDP is rejected.
- `service/registry.go`: `ClientFactory` takes `inner netio.StreamClient`.

Tests: `service/dialer_test.go`, `service/registry_test.go`.

Diff vs previous state:
     third_party/shadowsocks-go/service/client.go       | 37 +++++++++++++++++++---
     third_party/shadowsocks-go/service/registry.go     |  5 +--
     .../shadowsocks-go/service/registry_test.go        |  8 ++---
     3 files changed, 39 insertions(+), 11 deletions(-)
    third_party/shadowsocks-go/service/dialer_test.go (new)

## Patch A2 — UDP for registered client protocols

Purpose: legacy AEAD UDP (P1-A2).

- `service/registry.go`: `UDPEnv`, `UDPClientFactory`, `RegisterUDPClientProtocol`.
- `service/client.go`: the registry branch builds the UDP client from the
  UDP factory when `enableUDP` is set (still "UDP not supported" without one).

Tests: `service/registry_test.go`.

Diff vs previous state:
     third_party/shadowsocks-go/service/client.go       | 18 ++++++++--
     third_party/shadowsocks-go/service/registry.go     | 36 +++++++++++++++++++
     .../shadowsocks-go/service/registry_test.go        | 42 ++++++++++++++++++++++
     3 files changed, 94 insertions(+), 2 deletions(-)

## Patch A3 — route every UDP packet (NAT relay)

Upstream `UDPNATRelay` picks the UDP client once, from a client address's
first packet, and sends every later packet from that address through it. In
rules mode that sends traffic meant for the proxy out direct (and past a
fail-closed client) once a socket's first packet went to a LAN address.

- `service/udp_nat.go`: the NAT table is two levels — client address →
  `natClientEntry` (SOCKS unpacker, pktinfo) → one `natEntry` session per
  routed UDP client name. The router runs for every packet in the receive
  loop; the session for that client is created on first use. A client entry
  leaves the table when its last session ends. `Stop` stops every session.
- Constraint: routing in the receive loop must not block, so routes must not
  resolve names. ssmygo configures no resolvers (upstream refuses such
  routes without one). `UDPSessionRelay` (Shadowsocks 2022 server side) is
  unchanged; ssmygo does not use it.

Tests: `internal/core` `TestRunnerUDPRoutesEveryPacket`.

Diff vs previous state:
     third_party/shadowsocks-go/service/udp_nat.go | 108 +++++++++++++++-----------
     1 file changed, 64 insertions(+), 44 deletions(-)

## Patch A4 — traffic meter in the UDP NAT relay

shadowsocks-go's `stats` only adds a session's bytes when it ends, too late
for a live rate display. ssmygo counts bytes as they move.

- `service/meter.go`: `TrafficMeter` interface (`Add(up, down uint64)`).
- `service/server.go`: `ServerConfig.TrafficMeter` (`json:"-"`, set in Go;
  nil = off), passed to `NewUDPNATRelay`.
- `service/udp_nat.go`: `Add(payload, 0)` after a client packet is unpacked;
  `Add(0, payload)` after a reply is written back to the client. Payload
  bytes only (no SOCKS header, no encryption). Other relays are unchanged.

Tests: `service/meter_test.go`; `internal/core` `TestRunnerCountsUDPPayload`.

Diff vs previous state:
     third_party/shadowsocks-go/service/server.go  | 7 ++++++-
     third_party/shadowsocks-go/service/udp_nat.go | 8 ++++++++
     2 files changed, 14 insertions(+), 1 deletion(-)
    ?? third_party/shadowsocks-go/service/meter.go
    ?? third_party/shadowsocks-go/service/meter_test.go

## Patch A5 — probe client groups at once

Probing groups (availability, latency, min-max-latency) waited one whole
interval (30 s by default) before their first probe, keeping the first
client — possibly a dead server — meanwhile. ssmygo rebuilds the core on
every settings change, so High Availability would fall back to a dead
server for 30 s after each change.

- `clientgroup/probe.go`: `immediateTicks` delivers one tick at once, then
  the ticker's; the three probe loops use it.

Tests: `internal/core` `TestRunnerHAProbesAtOnce`.

Diff vs previous state:
     third_party/shadowsocks-go/clientgroup/probe.go | 33 ++++++++++++++++++++++---
     1 file changed, 30 insertions(+), 3 deletions(-)
