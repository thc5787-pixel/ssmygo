# ssmygo P1-A2 设计：UDP 转发

> 日期：2026-11-02　状态：待审阅
> 上级文档：[`requirements.md`](../../requirements.md)（C3）、[A1 设计](2026-11-01-ssmygo-a1-plugin-forward-design.md)
> 范围：P1 子项目 A 的第二步。SOCKS5 UDP ASSOCIATE 经 SS 服务器转发 UDP。

## 1. 已确认的决定

| # | 决定 | 来源 |
|---|---|---|
| A2-D1 | 插件服务器：UDP 直接发到服务器的 UDP 端口，不经插件（大多数插件本身不转发 UDP） | 用户选择 A；与原版 `UDPRelay.cs` 相同 |
| A2-D2 | 开启前置代理时不转发 UDP：SOCKS5 UDP ASSOCIATE 回复"不支持"，应用退回 TCP | 用户选择 A（原版会绕过前置代理，ssmygo 不这样做） |
| A2-D3 | UDP 默认开启，不加开关 | 原版无开关 |
| A2-D4 | 规则分流对 UDP 与 TCP 相同（私有网段直连；geosite 分组） | 一致性 |
| A2-D5 | 插件没运行时，UDP 同样直接失败（A1 的 fail-closed 客户端也拒绝 UDP） | 与 A1 一致 |

## 2. 数据流

```
应用 ──TCP: SOCKS5 UDP ASSOCIATE──▶ 本地混合端口(TCP)    回复：UDP 发到 127.0.0.1:<同一端口>
应用 ──UDP 数据报───────────────────▶ 本地 UDP 监听(同一端口号) → 路由 → SS UDP 客户端 → 服务器 UDP 端口
```

shadowsocks-go 回复 UDP ASSOCIATE 时，给的是那条 TCP 连接的本地地址（`socks5/stream.go`），所以 UDP 监听必须和 TCP 用同一个端口号。TCP 和 UDP 的端口互不冲突。UDP 数据报由 shadowsocks-go 的 `socks5` 服务端（`direct.Socks5UDPNATServer`）处理。

## 3. 组件设计

### 3.1 补丁 A2（shadowsocks-go，`service/registry.go`、`service/client.go`）

```go
// UDPEnv is what a registered UDP client needs from AddClient.
type UDPEnv struct {
    Addr         conn.Addr                    // the client's UDP server address
    Preference   netio.AddressFamilyPreference
    Resolver     conn.Resolver
    MTU          int
    SocketConfig conn.UDPSocketConfig
}
type UDPClientFactory func(c *ClientConfig, env UDPEnv) (zerocopy.UDPClient, error)
func RegisterUDPClientProtocol(name string, f UDPClientFactory) // panics on duplicate
```
- 补丁 A 在注册分支里对 `EnableUDP` 报错 `UDP not supported`；改为：查 UDP 注册表，有工厂就调用它，没有才报原来的错。
- `DialerClient` + UDP 仍然报错（补丁 C 不变）。
- 测试：注册的 UDP 协议能被创建并收到正确的 `UDPEnv.Addr`（`udpAddress` 与 `endpoint` 分开设置时取 `udpAddress`）；只注册了 TCP 的协议开启 UDP 时仍报 `UDP not supported`。

### 3.2 core/legacyaead：UDP

```go
func NewUDPClient(name, method string, key []byte, env service.UDPEnv) (zerocopy.UDPClient, error)
// register.go: init() also RegisterUDPClientProtocol for each Methods entry.
```
- **包格式**（Shadowsocks AEAD 规范）：`salt(len = keySize) | AEAD.Seal(subkey(key, salt), nonce = 0, SOCKS 地址 + 数据)`。每个包随机生成新 salt，nonce 全零。
- **打包**（`PackInPlace`）：前部预留 `keySize + 最长 SOCKS 地址(1+1+255+2)`，尾部预留 tag 16 字节。目标地址按 SOCKS5 地址格式写在数据前面，原地加密，发往 `env.Addr`（域名用 `env.Resolver` 解析，遵守 `Preference`）。
- **解包**（`UnpackInPlace`）：取出 salt、解密，失败就丢弃这个包（返回错误，由中继记日志）；解析出 SOCKS 地址作为数据来源，返回数据部分。
- **最大包长**：`MTU - IP/UDP 头部开销`，参照 `none` 的 UDP 客户端（`direct.NewShadowsocksNoneUDPClient`）的算法。
- 测试服务端：`aeadtest` 加一个 UDP 服务端，解密后把数据转发给目标，再把回包加密送回（只在测试中使用）。

### 3.3 core/mixed

- 新增协议 `mixed+udp`：内部的 socks5 StreamServer 设为 `EnableUDP: true`，可以回复 UDP ASSOCIATE。
- 原有的 `mixed` 保持只支持 TCP：遇到 ASSOCIATE 回复 `ReplyCommandNotSupported`，开启前置代理时使用它（A2-D2）。

### 3.4 core.Build

| 情况 | servers | clients | 路由 |
|---|---|---|---|
| UDP 开启（无前置代理） | `local`（`mixed+udp`，TCP 监听）+ `local-udp`（`socks5`，只有 `udpListeners`，同一地址） | `direct`、`proxy` 都开启 UDP，`mtu: 1500` | `DefaultUDPClientName` 与 TCP 相同；分流规则的 `network` 留空（TCP、UDP 都适用） |
| 前置代理开启 | 只有 `local`（`mixed`） | 同 A1（只有 TCP） | 规则 `network: "tcp"`（同 A1） |
| 插件服务器、插件在运行 | 同"UDP 开启" | `proxy`：`tcpAddress = 127.0.0.1:<插件端口>`，`udpAddress = 服务器真实地址`（A2-D1） | 同"UDP 开启" |
| 插件服务器、插件没运行 | 同"UDP 开启" | `proxy` = fail-closed 客户端，另外开启 UDP（拒绝一切地址，A2-D5） | 同"UDP 开启" |

`direct` 客户端开启 UDP 后，按规则直连的 UDP 照常可用。

### 3.5 controller 与 ui

- 控制器逻辑不变（`core.Build` 根据配置自行决定）。`State` 新增 `UDPDisabled bool`：开启前置代理时为 true。
- 托盘提示：`UDPDisabled` 时加一行"UDP：已关闭（使用前置代理）"（`ssmygo.udpOff`，中英文）。

## 4. 错误处理

| 情况 | 结果 |
|---|---|
| 服务器不支持或未开启 UDP | 数据报没有回应，应用自行超时（和原版一样）；中继记 Debug 日志 |
| 解密失败或包格式错误 | 丢弃这个包，记日志，不影响其他会话 |
| 本地 UDP 端口被占用 | 和 TCP 端口被占用一样：`PortInUseError`，整体回滚（`applyCore` 的 listen 错误判断也覆盖 UDP 的 `OpError`） |
| 前置代理开启 | ASSOCIATE 回复"不支持"，应用退回 TCP |

## 5. 测试

| 层 | 测试 |
|---|---|
| 补丁 A2 | 见 §3.1 |
| legacyaead UDP | 5 种加密收发；与 go-shadowsocks2 的 UDP 互通（aes-128/256-gcm、chacha20）；篡改 tag → 丢弃；超长包 → 打包报错；IPv4 地址、IPv6 地址、域名三种目标地址 |
| mixed | `mixed+udp` 的 ASSOCIATE 返回成功；`mixed` 返回 `CommandNotSupported`（7） |
| core.Build | 新黄金文件 `udp`、`udp-plugin`、`udp-forward-off`；A1 及以前的黄金文件只在新增字段上变化，逐个复核 |
| 端到端（进程内） | 测试内写一个最小 SOCKS5 UDP 客户端（先 ASSOCIATE，再发 UDP 数据报，带 RSV/FRAG/地址头）→ 本地 → 传统 AEAD / SS2022 / none → 服务器 → UDP 回显；插件服务器的 UDP 直接到服务器（假插件不转 UDP，仍然收发成功）；前置代理开启时 ASSOCIATE 被拒；插件没运行时 UDP 没有回应 |
| 本地端口 | UDP 端口被占用 → `PortInUseError` |
| ui | 托盘提示"UDP：已关闭（使用前置代理）" |

## 6. 不做

- UDP 经前置代理转发（SOCKS5 前置代理的 UDP ASSOCIATE 链）
- UDP 流量统计（子项目 B）
- 插件的 UDP 转发（少数插件支持，SIP003 也没有统一约定）
