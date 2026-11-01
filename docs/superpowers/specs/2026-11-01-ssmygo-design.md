# ssmygo 设计文档（P0 首版）

> 日期：2026-11-01　状态：待审阅
> 输入：[`docs/requirements.md`](../../requirements.md)（决定 D1–D9）、[`docs/spike-findings.md`](../../spike-findings.md)
> 范围：只覆盖 **P0**（D5）。P1/P2 只说明在哪里扩展，不做设计。

---

## 1. 目标与非目标

**目标（P0）**

| 编号 | 能力 | 需求 ID |
|---|---|---|
| G1 | 本地单端口混合代理（SOCKS5 + HTTP/CONNECT），默认 `127.0.0.1:1080`，可开放局域网 | C1、C4 |
| G2 | SS 客户端 TCP：传统 AEAD 5 种 + SS2022 2 种 + `none`/`plain` | C2 |
| G3 | 系统代理：禁用 / PAC（核心路由分流）/ 全局；保存并恢复用户原有设置 | S1–S4 |
| G4 | 托盘：按模式切换图标、动态菜单（模式、服务器、编辑服务器、剪贴板导入、开机启动、局域网、退出） | U1、U3、M2、M3 |
| G5 | 编辑服务器窗口：沿用原版布局，遵循 MyGo 界面规范 | U4、M1 |
| G6 | 兼容原版 `gui-config.json`，原子写入，便携模式 | F1、F3、F4、I5 |
| G7 | 单实例、开机启动、退出时清理、中英双语、日志写文件 | I1、I2、I6、U13 |

**非目标（P0 不做）**：UDP 转发、SIP003 插件、前置代理、多服务器负载策略、日志窗口、快捷键、SIP008 订阅、二维码、在线更新 geosite、自动更新。托盘菜单里**不显示**这些项（U3）。

## 2. 总体架构

```
┌──────────────────────────── cmd/ssmygo ────────────────────────────┐
│ main：语言、单实例、App.Run                                           │
└──────────────┬─────────────────────────────────────────────────────┘
               │
┌──────────────▼──────────── internal/ui ─────────────────────────────┐
│ tray.go（托盘 + 菜单）   configwin.go（编辑服务器窗口）   theme.go      │
│ 只在主线程运行；通过 Controller 的方法发命令，订阅 State 刷新            │
└──────────────┬─────────────────────────────────────────────────────┘
               │  Controller API（Section 4）
┌──────────────▼──────────── internal/controller ─────────────────────┐
│ 持有 Config 和 State；串行化所有修改；调度 core / sysproxy / rules       │
└───┬───────────────┬───────────────┬──────────────┬─────────────────┘
    │               │               │              │
┌───▼────────┐ ┌────▼──────┐ ┌──────▼─────┐ ┌──────▼──────┐
│ config     │ │ core      │ │ sysproxy   │ │ rules       │
│ gui-config │ │ 翻译成     │ │ WinINet    │ │ dlc.dat →   │
│ ss:// 解析  │ │ ssgo 配置  │ │ 读/写/恢复  │ │ domainset   │
└────────────┘ └────┬──────┘ └────────────┘ └─────────────┘
                    │
     ┌──────────────┼───────────────────────────┐
     │ core/mixed   │ core/legacyaead           │  ← 通过扩展点注册
     └──────────────┴──────────────┬────────────┘
                                   │
                  third_party/shadowsocks-go（vendor + 补丁 A、B）
```

**依赖规则**：箭头只向下。`controller` 及以下的包都**不 import MyGo**，可以在无界面环境里单元测试（requirements §5"可测"）。

## 3. 仓库布局

```
ssmygo/
  cmd/ssmygo/main.go
  internal/
    ui/            tray.go  configwin.go  theme.go  icons/（嵌入 PNG）
    controller/    controller.go  state.go
    config/        guiconfig.go  server.go  ssurl.go  paths.go
    core/          build.go（gui-config → service.Config）  runner.go
      mixed/       server.go（SOCKS5/HTTP 嗅探）
      legacyaead/  cipher.go  stream.go  kdf.go  server_test.go
    rules/         dlc.go（protobuf 解析）  build.go（→ domainset gob）
    sysproxy/      sysproxy_windows.go  sysproxy.go（接口 + 测试替身）
    i18n/          strings_en.json  strings_zh-Hans.json  gen/（resx → json 转换器）
    logx/          logx.go（slog → 文件，带大小轮转）
  third_party/shadowsocks-go/   vendor 自 65ba171
  third_party/PATCHES.md        补丁清单
  assets/          dlc.dat.gz（v2fly，MIT）  shadowsocks.svg（用户提供，程序图标和托盘图标的来源）
  docs/
  mygo.json  go.mod（replace github.com/database64128/shadowsocks-go => ./third_party/shadowsocks-go）
  LICENSE（AGPL-3.0）
```

## 4. 组件设计

### 4.1 config：配置模型

**`Config`** 字段名和原版 `Model/Configuration.cs` 一致（F1）。P0 只解释下面这些字段，其余字段原样保留（F2，通过 `json.RawMessage` 合并写回）：

| 字段 | 类型 | 默认值 | 用途 |
|---|---|---|---|
| `configs` | `[]Server` | `[]` | 服务器列表（附录 B 的字段） |
| `index` | int | 0 | 当前服务器；越界时修正为 0 |
| `enabled` | bool | false | 系统代理是否开启 |
| `global` | bool | false | true = 全局，false = 规则分流 |
| `localPort` | int | 1080 | 本地端口 |
| `shareOverLan` | bool | false | true 时监听 `0.0.0.0` |
| `portableMode` | bool | true | 配置放在 exe 目录还是 `%AppData%\ssmygo` |
| `geositeDirectGroups` | `[]string` | `["private","cn","geolocation-!cn@cn"]` | 直连组（原版默认值） |
| `geositeProxiedGroups` | `[]string` | `["geolocation-!cn"]` | 代理组 |
| `geositePreferDirect` | bool | false | 黑名单模式：没匹配上的直连 |
| `firstRun` | bool | true | 第一次运行时打开编辑服务器窗口 |
| `sysproxy`（ssmygo 新增） | 对象 | — | 记录用户原有的系统代理设置，用于恢复 |

**`Server.Validate() error`**：地址非空；端口 1–65535；加密方式在支持列表中；SS2022 的密码必须是 base64，解码后长度为 16 或 32 字节；超时 ≥ 0。界面通过 `Field.Error` 显示同样的错误（单一来源）。

**路径（`paths.go`）**：便携模式下是 exe 所在目录，否则是 `App.Path(PathUserData)`。文件包括 `gui-config.json`、`user-rule.txt`、`cache/domainsets/*.gob`、`logs/ssmygo.log`。`config` 包只接收根目录参数，不依赖 MyGo，由 `main` 传入。

**写盘**：先写同目录下的 `gui-config.json.tmp`，`fsync` 后 `os.Rename` 覆盖（F3）。读取失败时，把原文件改名为 `gui-config.json.bad-<时间戳>`，用默认配置启动，并发通知告知用户（原版是弹窗）。

**`ssurl.go`（M3）**
- `Parse(text string) ([]Server, []error)`：逐行解析，支持 SIP002 `ss://base64url(method:password)@host:port/?plugin=…#tag`、SIP002 明文 userinfo（SS2022），以及旧版 `ss://base64(method:password@host:port)#tag`。
- `Format(Server) string`：生成 SIP002 链接，P0 只用于测试往返校验。

### 4.2 core：把配置交给 shadowsocks-go

**`core.Build(cfg *config.Config, rulesDir string) (*service.Config, error)`** 是纯函数，翻译规则如下：

| shadowsocks-go | 取值 |
|---|---|
| `servers[0]` | `{name:"local", protocol:"mixed", tcpListeners:[{address: <127.0.0.1 或 0.0.0.0>:<localPort>}]}` |
| `clients` | `{name:"proxy", protocol:<method>, endpoint:<server:port>, enableTCP:true, psk/password…}` 和 `{name:"direct", protocol:"direct", enableTCP:true}` |
| `router.defaultTCPClientName` | `global` 或 `geositePreferDirect=false` 时为 `"proxy"`，否则为 `"direct"` |
| `router.domainSets` | 规则分流时，由 `rules` 产出的 `direct.gob`、`proxied.gob`（type `gob`） |
| `router.routes` | 规则分流时：`toDomainSets:["direct"] → direct`，`toDomainSets:["proxied"] → proxy`；另加私有网段 `toPrefixes`（RFC1918、回环、链路本地）→ direct |

全局模式下不生成 domainSets 和 routes。"禁用"模式只关闭系统代理，本地端口照常工作，按 `global` 的值转发，和原版行为一致。

**`core.Runner`**：管理 Manager 的生命周期。

```go
type Runner struct{ /* mu, cur *service.Manager, logger */ }
func (r *Runner) Apply(cfg *service.Config) error // 停旧的 → 建新的 → Start；同步返回启动结果
func (r *Runner) Stop()
```

- 依赖补丁 B（`Manager.Start`/`Stop` 拆分，见 §5），这样 `Apply` 能同步拿到"端口被占用"这类错误，不需要靠超时猜测。
- spike 实测重建一次约 3ms，所以每次配置变化都整体重建，不做增量更新。
- 已建立的连接在重建后继续走旧服务器，直到连接自然结束（spike R4），这是可以接受的行为。

**`core/mixed`**：实现 `netio.StreamServer`。
- `HandleStream` 先读首字节：`0x05` 交给 socks5 的 StreamServer，其余交给 httpproxy 的 ServerConfig。已读的首字节用 `prefixConn` 包装后放回。
- 首字节读取超时 10s，超时就关闭连接。

**`core/legacyaead`**：传统 AEAD 客户端，实现 `netio.StreamClient`，写法参照 `ssnone.StreamClientConfig`。
- 密钥：`EVP_BytesToKey(MD5, password, keyLen)`；子密钥：`HKDF-SHA1(key, salt, "ss-subkey")`。
- 流格式：`salt | [enc(len:2)+tag][enc(payload)+tag]...`，单块最大 0x3FFF，nonce 按小端递增。
- 加密：`aes-128/192/256-gcm` 用 `crypto/aes` + `cipher.NewGCM`；`chacha20-ietf-poly1305` 和 `xchacha20-ietf-poly1305` 用 `x/crypto/chacha20poly1305`。
- 目标地址（SOCKS 地址格式）和首包载荷合并成第一块发送（`NativeInitialPayload: true`）。
- 测试用的服务端（`server_test.go`）只在测试中编译。

### 4.3 rules：geosite → domainset

- `assets/dlc.dat.gz` 编译时嵌入（v2fly domain-list-community，MIT）。首次运行时释放到缓存目录；P1 的在线更新会覆盖这个文件。
- `dlc.go` 用 `google.golang.org/protobuf/encoding/protowire` 手写解析 `GeoSiteList`，只需要 `country_code`、`domain{type,value,attribute}` 三个字段，不引入生成代码。
- 支持组名写法 `cn`、`geolocation-!cn`、`geolocation-!cn@cn`（`@` 后面是属性过滤），和原版一致。
- 类型映射：Plain → keyword，Regex → regexp，Domain（RootDomain）→ suffix，Full → domain。
- `user-rule.txt`（P1 才合并）在 P0 先预留接口：`Build(groups, userRules)`。
- 产出 `cache/domainsets/{direct,proxied}.gob`。缓存键是 `sha256(dlc + 组列表)`，没有变化就跳过重建，以加快启动。

### 4.4 sysproxy：Windows 系统代理

```go
type Settings struct{ Flags uint32; Server, Bypass, PacURL string }
type Proxy interface {
    Get() (Settings, error)
    Set(Settings) error
}
```

- Windows 实现：`InternetQueryOption` / `InternetSetOption(INTERNET_OPTION_PER_CONNECTION_OPTION)`，作用于默认连接和所有 RAS 连接（`RasEnumEntries`），之后发送 `INTERNET_OPTION_SETTINGS_CHANGED` 和 `INTERNET_OPTION_REFRESH`（R7）。通过 `golang.org/x/sys/windows` 加载 `wininet.dll`，不用 cgo。
- 开启时：如果 `config.sysproxy` 还没有记录，先 `Get()` 保存用户原有设置；然后 `Set{Flags: PROXY_TYPE_DIRECT|PROXY_TYPE_PROXY, Server: "127.0.0.1:<port>", Bypass: "localhost;127.*;10.*;172.16.*;…;192.168.*;<local>"}`。PAC 和全局都这样设置（方案甲），两者的区别只在核心的路由。
- 关闭或退出时：`Set(记录的设置)`，然后清除记录。
- 崩溃恢复：启动时如果 `enabled == false`，但当前系统代理指向本程序的端口，说明上次没有正常退出，执行恢复。
- 测试替身 `fakeProxy` 用于 controller 的单元测试；真实的 WinINet 测试放在 `//go:build windows && sysproxy_e2e` 标签下，手动运行。

### 4.5 controller：状态中心

```go
type Mode int // Disabled, Rules, Global

type State struct {
    Mode       Mode
    Servers    []string // 菜单显示名
    Current    int
    ShareLAN   bool
    AutoStart  bool
    LastError  string   // 显示在托盘提示和通知中
}

type Controller struct{ /* cfg, runner, sys, rules, subs */ }

func New(paths config.Paths, sys sysproxy.Proxy, logger *slog.Logger) (*Controller, error)
func (c *Controller) Start() error                   // 加载配置 → 生成规则 → runner.Apply → 按 enabled 设置系统代理
func (c *Controller) SetMode(Mode) error
func (c *Controller) SelectServer(i int) error
func (c *Controller) SetShareLAN(bool) error
func (c *Controller) SaveServers(servers []config.Server, port int, portable bool) error // 编辑服务器窗口点确定/应用
func (c *Controller) ImportURLs(text string) (added int, errs []error)
func (c *Controller) Config() config.Config          // 返回副本，供窗口编辑
func (c *Controller) Subscribe(fn func(State)) (off func())
func (c *Controller) Shutdown()                      // 恢复系统代理 → runner.Stop → 保存配置
```

- **并发模型**：所有方法在内部用一把互斥锁串行执行（重建只需毫秒级，加锁无妨）。`Subscribe` 的回调在调用方的 goroutine 里同步触发，UI 层负责用 `mygo.RunOnMain` 切回主线程。controller 本身不知道"主线程"这个概念。
- **事务性**：每个修改操作都按"修改副本 → `core.Build` → `runner.Apply` → 成功才提交并保存配置"的顺序执行。失败时回滚到旧配置，并重新 `Apply` 旧配置，返回的错误由 UI 展示。
- 开机自启（`App.SetOpenAtLogin`）属于 MyGo 的能力，所以放在 UI 层，不进 controller。

### 4.6 ui：托盘和窗口

**托盘（`tray.go`）**
- 图标：由用户提供的 `assets/shadowsocks.svg` 生成（描边加粗到 2.5）。程序图标 `resources/icon.png` 为 1024px 蓝色 `#197DBF`；托盘图标为 32px，3 种模式 × 浅色/深色任务栏，共 6 张 PNG，在开发期通过 `go generate` 预先渲染并嵌入。颜色沿用原版 `SelectColorMask` 的含义：全局蓝色；PAC 在浅色任务栏上深灰、深色任务栏上白色；禁用为灰色。通过 `Tray.Dark()` 和 `OnAppearanceChange` 跟随任务栏深浅色。P1 再加流量角标。
- 提示文字：`ssmygo <版本>\n<服务器名>\n<模式>`，有错误时加一行 `⚠ <LastError>`。
- 左键单击：打开编辑服务器窗口（与原版一致）。右键单击：`SetMenu` → `PopUpMenu` → `SetMenu(nil)`（spike 已验证）。
- P0 菜单（原版附录 A 的子集，文案来自 resx）：
  ```
  系统代理 ▸ 禁用 / PAC / 全局          （单选）
  服务器   ▸ [服务器列表，单选] ─ 编辑服务器… / 从剪贴板导入 URL…
  ─
  开机启动                              （复选）
  允许其他设备连入                       （复选）
  ─
  退出
  ```
  "PAC"沿用原版的名字，实际含义是规则分流（D6）。

**编辑服务器窗口（`configwin.go`）**：按 spike 定下的界面规范实现（`spike-findings.md`）。
- 主题 `Spacing=3`；`Form` → `Fieldset("服务器")` → `Field`；校验用 `Field.Error`，规则和 `Server.Validate` 相同；表单区外包 `Scroll`；底部按钮用 6 列 Grid；按钮 `MinWidth(t.Rem(7))`；Alt+字母 作为助记符；Enter 为确定，Esc 为取消；窗口 582×620，并设为最小尺寸。
- 打开时取 `Controller.Config()` 的副本来编辑；"确定"和"应用"调用 `SaveServers`；有校验错误时，"确定"和"应用"不可点。
- **按需创建**：第一次打开时才 `NewWindow`，关闭时销毁（不是隐藏），满足"只有托盘时内存 ≤ 40MB"。窗口状态（选中项、未保存的修改）在窗口销毁时一并丢弃，和原版"取消"的语义一致。
- 首次运行（`firstRun`）或没有任何服务器时，启动后自动打开这个窗口。

**国际化**：`i18n/gen` 是开发期使用的工具，把原版 `Strings.resx`、`Strings.zh-Hans.resx` 转换成 JSON 并提交到仓库；运行时按 MyGo 的 i18n（`docs/i18n.md`）根据系统语言选择，取不到时回退英文。界面代码只用键名。

### 4.7 main：启动流程

```
App.SetName("ssmygo")
RequestSingleInstanceLock() 失败 → 退出（已运行的实例在 OnSecondInstance 里打开编辑服务器窗口）
WhenReady:
  paths ← 根据 portableMode 和 exe 目录确定
  logger ← logx（文件 + 内存环形缓冲，后者留给 P1 的日志窗口）
  ctrl ← controller.New(paths, sysproxy.Windows(), logger)
  tray ← ui.NewTray(ctrl)
  err ← ctrl.Start()；出错时发通知，托盘提示显示错误，程序继续运行（用户可以改端口后重试）
  if firstRun 或没有服务器 → 打开编辑服务器窗口（WasOpenedAtLogin 时不打开）
OnWindowAllClosed: 不退出
OnBeforeQuit: ctrl.Shutdown()
```

## 5. shadowsocks-go 的 vendor 方式与补丁

- 把 `65ba171` 的源码完整复制到 `third_party/shadowsocks-go`，保留它的 LICENSE；`go.mod` 用 `replace` 指向这个目录。
- **补丁 A：协议扩展点**（约 40 行，在 `service/client.go`、`service/server.go` 中）
  ```go
  // service/registry.go（新增）
  type ClientFactory func(c *ClientConfig, inner *netio.TCPClient) (netio.StreamClient, error)
  type ServerFactory func(sc *ServerConfig) (netio.StreamServer, error)
  func RegisterClientProtocol(name string, f ClientFactory)
  func RegisterServerProtocol(name string, f ServerFactory)
  ```
  在 `client.go` / `server.go` 的 `switch` 的 `default` 分支里先查注册表，查不到再报 `unknown protocol`。ssmygo 的 `legacyaead`、`mixed` 在 `init()` 里注册。这样协议代码全部放在 `internal/`，vendor 目录只改这一处。
  传统 AEAD 的密码字段：在 `ClientConfig` 增加 `Password string \`json:"password,omitzero"\``。
- **补丁 B：`Manager.Start(ctx) error` / `Manager.Stop()`**（约 30 行），把现有 `Run` 拆成同步启动和停止两步，`Run` 改为调用这两者，原有行为不变。
- 每个补丁写进 `third_party/PATCHES.md`（目的、涉及文件、对应 diff）。跟进上游时：重新复制，再按清单打补丁。

## 6. 错误处理

| 场景 | 检测 | 用户看到 | 系统状态 |
|---|---|---|---|
| 本地端口被占用 | `runner.Apply` 返回 bind 错误 | 通知"端口 1080 被占用"；托盘提示带 ⚠；编辑服务器窗口的端口框标红 | 不开启系统代理；保留旧配置 |
| 配置文件损坏 | JSON 解析失败 | 通知：已备份为 `.bad-时间戳`，使用默认配置 | 用默认配置启动 |
| 服务器配置不合法 | `Server.Validate` | 窗口里 `Field.Error`，确定按钮不可点 | 不保存 |
| 没有任何服务器 | `len(configs)==0` | 打开编辑服务器窗口；"系统代理"菜单不可用 | 核心只有 direct |
| 设置系统代理失败 | WinINet 返回错误 | 通知加日志 | `enabled` 回滚为 false |
| 剪贴板里没有可解析的链接 | `ssurl.Parse` 结果为空 | 通知"未找到 ss:// 链接" | — |
| 部分链接解析失败 | errs 不为空 | 通知"导入 N 个，失败 M 个"，详情写日志 | 只加入成功的部分 |
| dlc.dat 损坏 | protowire 解析失败 | 通知，自动降级为全局模式并在日志中说明 | 规则分流不可用 |
| 异常退出 | 下次启动时检测（§4.4） | 无感知 | 自动恢复系统代理 |
| 远端连接失败 | shadowsocks-go 记录日志 | 只写日志（和原版一致） | — |

日志：`logx` 把 slog 写到 `logs/ssmygo.log`，单文件上限 5MB，保留 2 个；**不记录密码和 PSK**，`Server` 实现 `LogValue()` 做脱敏。

## 7. 测试策略

| 层 | 方法 | 关键用例 |
|---|---|---|
| config | 表驱动单元测试 | 原版真实 `gui-config.json` 样本读写往返不丢字段；默认值；原子写入（模拟写入中途失败）；`Validate` 的边界值 |
| ssurl | 单元测试 | SIP002（含 plugin 参数）、明文 userinfo、旧版格式、多行混合、非法输入；`Format`→`Parse` 往返 |
| legacyaead | 单元测试 + 测试服务端 | 已知向量（与 shadowsocks-rust 的输出对照）；5 种加密的往返；分块边界（0x3FFF、0x4000）；篡改 tag 能被检测出来 |
| mixed | 单元测试 | SOCKS5 握手、HTTP CONNECT、普通 HTTP GET、首字节超时 |
| rules | 单元测试 | 用一个小型 dlc 样本测试组名 / `@` 属性 / 类型映射；缓存命中 |
| core.Build | 黄金文件 | 三种模式和局域网开关的组合，生成的 `service.Config` JSON 与黄金文件比对 |
| controller | 单元测试（fakeProxy + 真实 Runner + 进程内服务端） | 切换模式和服务器；端口冲突时回滚；Shutdown 恢复系统代理；崩溃恢复 |
| 端到端 | 进程内：本地端口 → legacyaead / ss2022 → 测试服务端 → echo | 7 种加密都能正常 echo |
| 互通（手动 / CI 可选） | 有 `ssserver`（shadowsocks-rust）时运行 | 传统 AEAD 与真实服务端互通 |
| ui | `ui.NewTester` + 快照 | 编辑服务器窗口：增删改、校验错误显示、Alt 快捷键、浅色和深色快照；托盘菜单结构用 `Tester.Menu()` 断言 |
| sysproxy | 标签 `sysproxy_e2e`，手动 | 真实地写入、读取、恢复 |

## 8. 性能与资源（对应修订后的指标）

- 窗口按需创建、关闭即销毁：只有托盘时约 30MB（spike 实测）。
- exe 体积：spike 界面部分 10.1MB，加上 shadowsocks-go 估计 +3MB，`dlc.dat.gz` 约 +1MB，约 14MB，在 15MB 预算内。如果超出，就去掉 dlc 中用不到的组。
- 启动：domainset 缓存命中时不需要解析 dlc；冷启动（需要解析）目标 ≤ 1s，在实现阶段实测。

## 9. 需求追踪

| 需求 | 设计位置 |
|---|---|
| C1、C4 | §4.2 mixed、Build |
| C2 | §4.2 legacyaead；§5 补丁 A |
| S1–S4 | §4.4、§4.2 Build、§4.3 |
| M1–M3 | §4.6 编辑服务器窗口、§4.1 ssurl、§4.5 ImportURLs |
| U1、U3、U4、U11、U12、U13 | §4.6 |
| I1、I2、I5、I6 | §4.7、§4.1 paths、§4.5 Shutdown |
| F1、F3、F4 | §4.1 |
| 非功能需求 | §7、§8 |

## 10. P1 扩展点（只列位置）

| P1 功能 | 扩展位置 |
|---|---|
| UDP | Build 里开启 `udpListeners` 和 `enableUDP`；legacyaead 增加 `udp.go` |
| SIP003 插件 | `core/plugin`：启动插件进程，Build 把 endpoint 改为插件的本地端口 |
| 前置代理 | 需要 shadowsocks-go 支持客户端链式拨号（目前 `innerTCPClient` 只能直连），P1 再加一个补丁或扩展点 |
| 负载策略 | Build 生成 `clientgroup` |
| 日志窗口 | 读取 logx 的内存环形缓冲；流量数据来自 `stats` |
| 快捷键、SIP008、二维码 | 只涉及 ui 和 controller，加新的方法即可 |
