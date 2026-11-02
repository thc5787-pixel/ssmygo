# ssmygo P1-A2 Implementation Plan: UDP relay

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** SOCKS5 UDP ASSOCIATE on the local port relays UDP through the SS server (legacy AEAD, SS2022, none), with the A2-D1…D5 rules for plugins, forward proxy and broken plugins.

**Architecture:** Patch A2 adds UDP client registration to shadowsocks-go; `legacyaead` gains a `zerocopy.UDPClient`; `mixed+udp` answers UDP ASSOCIATE while a UDP-only `socks5` server listens on the same port number; `core.Build` wires UDP per spec §3.4; `State.UDPDisabled` feeds one tooltip line.

**Tech Stack:** Go 1.27.3, vendored shadowsocks-go (patches A, B, C, A2), go-shadowsocks2 v0.1.5 (test interop).

**Spec:** `docs/superpowers/specs/2026-11-02-ssmygo-a2-udp-design.md`

## Global Constraints

- All P0/A1 constraints hold (no cgo, MyGo only in `cmd/` and `internal/ui`, no secrets in logs, commit trailer with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_0197wVrsfVcKv9s9u4vo2grA`).
- Legacy AEAD UDP packet: `salt(keySize) | Seal(SubKey(key, salt), nonce=0, socksAddr+payload)`; new random salt per packet; tag 16 bytes.
- Client MTU 1500; local UDP listener on the same address as the TCP listener.
- With the forward proxy on: no UDP anywhere (A1 config unchanged, `mixed` replies CommandNotSupported = 7).

## Review Focus

1. **Large and odd datagrams**: payloads near the MTU limit and 0-byte payloads; oversized ones are rejected cleanly, never truncated silently. → Task 2.
2. **Many concurrent UDP sessions** (a game or DNS burst): sessions don't mix up return traffic; each client address gets its own replies. → Task 4.
3. **Domain targets in UDP** (`SOCKS5 ATYP=3`): resolved on the server side, not locally (no local DNS leak). → Task 2 (packer keeps the domain).
4. **Port conflicts on UDP only** (another program holds UDP :1080 but not TCP): reported as PortInUseError and rolled back like TCP. → Task 4.
5. **Plugin server UDP** goes to the real server even though TCP goes to the plugin, and stops when the plugin isn't running (fail closed). → Task 4.

---

### Task 1: Patch A2 — UDP client registration

**Files:** Modify `third_party/shadowsocks-go/service/{registry.go,client.go}`, test `service/registry_test.go`; `third_party/PATCHES.md`.

**Interfaces — Produces:** `UDPEnv{Addr, Preference, Resolver, MTU, SocketConfig}`, `UDPClientFactory`, `RegisterUDPClientProtocol` (spec §3.1). Registry branch: `EnableUDP` → UDP factory if registered, else `protocol %q: UDP not supported`.

- [ ] Failing tests: `TestRegisterUDPClientProtocol` (factory receives `env.Addr` == `udpAddress` when `endpoint`/`tcpAddress` differ; MTU 1500); `TestRegisteredClientRejectsUDP` stays green for TCP-only protocols; `TestDuplicateUDPRegisterPanics`.
- [ ] Implement; vendored `go test ./service/...` green; PATCHES.md "Patch A2".
- [ ] Commit `feat(third_party): patch A2 — UDP client registration`

### Task 2: legacyaead UDP client + test server

**Files:** Create `internal/core/legacyaead/udp.go`, `udp_test.go`; modify `register.go`, `aeadtest/aeadtest.go` (`StartUDP(t, method, password) netip.AddrPort`).

**Interfaces — Produces:** `NewUDPClient(name, method string, key []byte, env service.UDPEnv) (zerocopy.UDPClient, error)`; registered for all `Methods`.

- [ ] Failing tests (drive the client through a shadowsocks-go Manager: local `socks5` UDP server + client of the method → aeadtest UDP server → UDP echo, with a minimal SOCKS5 UDP client helper `socksUDPEcho(tcpAddr, target, payload)` in `udp_test.go`):
  - `TestUDPRoundTrip/<method>` ×5 with payloads 0, 1, 1200 bytes.
  - `TestUDPTargets`: IPv4, IPv6 (`[::1]`), domain `localhost` targets; for the domain the aeadtest server sees ATYP=3 (records the last target) — Review Focus 3.
  - `TestUDPOversized`: payload larger than MaxPacketSize → no datagram leaves (server counter stays 0), relay keeps working for the next normal packet — Review Focus 1.
  - `TestUDPTampered`: flip a byte of a server reply (aeadtest option) → the reply is dropped, next good reply arrives.
  - `TestUDPInteropGoShadowsocks2`: go-shadowsocks2 `core.PickCipher(...).PacketConn` as the server for aes-128/256-gcm, chacha20.
- [ ] Implement per spec §3.2; PASS; commit `feat(core): legacy AEAD UDP client`

### Task 3: mixed+udp

**Files:** Modify `internal/core/mixed/{server.go,register.go,server_test.go}`.

**Interfaces — Produces:** `const ProtocolUDP = "mixed+udp"`; `New(enableUDP bool)`.

- [ ] Failing tests: `TestAssociateAllowed` (mixed+udp + a `socks5` UDP-only server on the same address → reply 0 with bound address = the TCP local address); `TestAssociateRefused` (`mixed` → reply 7).
- [ ] Implement; PASS; commit `feat(core): mixed+udp answers UDP ASSOCIATE`

### Task 4: core.Build UDP wiring

**Files:** Modify `internal/core/{build.go,build_test.go,runner_test.go}`, goldens; `internal/controller/controller.go` (listen OpError for UDP already handled by `PortInUseError` check — verify); new goldens `udp`, `udp-plugin`, `udp-forward-off`.

**Interfaces — Consumes:** Tasks 1–3. **Produces:** Build per spec §3.4 (UDP on unless `cfg.Proxy.UseProxy`; plugin: `tcpAddress` plugin port, `udpAddress` real server; fail-closed client `enableUDP`, `mtu 1500`).

- [ ] Failing tests:
  - Goldens (`-update`, review: P0/A1 goldens now have UDP fields except `forward-*` / `plugin-and-forward`).
  - `TestRunnerUDPEndToEnd/<legacy aes-256-gcm | 2022-blake3-aes-128-gcm | none>` through the local port (SS2022/none upstream: shadowsocks-go server with `udpListeners`; legacy: `aeadtest.StartUDP`).
  - `TestRunnerUDPConcurrentSessions` (Review Focus 2): 20 SOCKS5 UDP associations in parallel, each sends a unique payload 5 times; each gets only its own echoes.
  - `TestRunnerUDPPluginServer` (Review Focus 5): plugin port = a TCP-only listener; UDP still echoes via the real server; with pluginPort 0 (not running) UDP gets no reply within 1 s.
  - `TestRunnerUDPOffWithForward`: forward proxy on → ASSOCIATE refused (7).
  - `TestRunnerUDPPortBusy` (Review Focus 4): UDP :port held by a test socket, TCP free → `Apply` fails with a listen `*net.OpError`; controller test `TestUDPPortBusyIsPortInUse` → `*PortInUseError`.
- [ ] Implement; `go test ./internal/...` PASS; commit `feat(core): relay UDP through the server`

### Task 5: State.UDPDisabled and tooltip

**Files:** `internal/controller/{state.go,controller.go,controller_test.go}`, `internal/ui/{tray.go,tray_test.go}`, i18n gen + JSON (`ssmygo.udpOff` {"UDP: off (forward proxy in use)", "UDP：已关闭（使用前置代理）"}).

- [ ] Failing tests: `TestStateUDPDisabled` (forward proxy on → true, off → false); `TestTooltipUDPOff`.
- [ ] Implement; PASS; build exe; add UDP checks to `docs/acceptance-a1.md` → new `docs/acceptance-a2.md` (real UDP app, e.g. a DNS query via `dig`/`nslookup` through a SOCKS5-UDP-capable client; game/voice app; forward proxy → apps fall back to TCP); commit `feat: UDP status in the tray; A2 acceptance list`
