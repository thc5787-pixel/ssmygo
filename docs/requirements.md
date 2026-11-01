# ssmygo 需求分析

> **目标**：用 **MyGo native UI**（纯 Go，GPU 自绘，无 webview，无 cgo）重写 **shadowsocks-windows**，**代理核心基于 shadowsocks-go**，**界面布局沿用 shadowsocks-windows**，产物是一个单文件、不依赖 .NET 运行时、不释放外部 exe/dll 的 Windows 托盘代理客户端。
>
> **参考**
> | 角色 | 项目 | 版本 |
> |---|---|---|
> | GUI 框架 | MyGo https://mygo.egoist.dev/ | 文档 0.7.4 |
> | 功能与界面布局 | shadowsocks-windows https://github.com/shadowsocks/shadowsocks-windows | main `891d971`（2025-01-01），最新 tag 4.4.1.0 |
> | 代理核心、系统代理 | shadowsocks-go https://github.com/database64128/shadowsocks-go | main `65ba171`（2026-10-30），Go 1.27，AGPL-3.0 |
>
> 日期：2026-11-01　状态：**已确认**
>
> **已确认的决定**
> - D1 代理核心、系统代理参考 shadowsocks-go
> - D2 界面布局沿用 shadowsocks-windows
> - D3 传统 AEAD 加密为 P0，必须支持
> - D4 许可证 AGPL-3.0（由 D1 决定）
> - D5 首版只做 P0（Q1）
> - D6 "PAC"模式采用方案甲：核心路由分流（Q2）
> - D7 中英双语，跟随系统语言，文案取自原版 resx（Q3）
> - D8 shadowsocks-go 以 vendor/fork 方式集成到 `third_party/`（Q4）
> - D9 ssmygo 目录单独 git init（Q5）

---

## 1. 背景与动机

| 现状（shadowsocks-windows） | 问题 | ssmygo 的做法 |
|---|---|---|
| C# WinForms + WPF，依赖 .NET Framework 4.8 + VC++ 2015 x86 运行库 | 安装门槛高 | 单个 Go exe |
| 运行时释放 `privoxy.exe`、`sysproxy.exe`、`libsscrypto.dll` | 易被杀软误报，往磁盘释放文件 | HTTP 代理、系统代理、加密全部用 Go 实现 |
| 加密依赖 OpenSSL / MbedTLS / libsodium 原生库 | 跨语言绑定复杂 | shadowsocks-go + Go 标准库 / `x/crypto` |
| 只支持传统 AEAD，没有 SS2022 | 协议落后 | 同时支持传统 AEAD 和 SS2022 |
| 维护缓慢（187 个 open issue） | — | — |

## 2. 用户与使用场景

- **主要用户**：Windows 10/11 桌面用户，已有 Shadowsocks 服务端或订阅；包括原版的老用户。
- **核心场景**
  1. **首次启动**：弹出"编辑服务器"窗口，填写或导入服务器，确定后自动启用系统代理，浏览器即可上网。
  2. **日常使用**：开机自启，静默驻留托盘；通过托盘菜单切换服务器和代理模式（禁用 / PAC / 全局）。
  3. **迁移**：把原版的 `gui-config.json` 拷过来，直接能用。
  4. **导入**：复制 `ss://` 链接，或扫描屏幕上的二维码导入；订阅（SIP008）一键更新。
  5. **分享**：把服务器显示成二维码或 `ss://` 链接，给手机用。
  6. **排障**：打开日志窗口，查看连接日志、插件输出和流量曲线。

## 3. 代理核心参考：shadowsocks-go

### 3.1 可直接复用

| 能力 | 包 | 对应需求 |
|---|---|---|
| SS2022 客户端（`2022-blake3-aes-128-gcm`、`2022-blake3-aes-256-gcm`），TCP + UDP | `ss2022` | C2、C3 |
| `none`/`plain`、SOCKS5、HTTP 代理的客户端与服务端 | `ssnone` `socks5` `httpproxy` | C1 入站、C6 前置代理 |
| 直连出站 | `direct` | C1 |
| TCP 中继、UDP NAT 中继、UDP 会话中继，生命周期由 `service.Manager.Run(ctx)` 管理 | `service` | 核心运行时 |
| 路由器：按域名集、IP 前缀集、GeoIP 规则选择出站 | `router` `domainset` `prefixset` | S4 |
| 客户端组：`round-robin` / `random` / `availability` / `latency` / `min-max-latency`，带探测 | `clientgroup` `probe` | C8 |
| 内置 DNS | `dns` | — |
| 流量统计 | `stats` | C9 |
| Windows 可构建（`conn/*_windows.go`），无 cgo | — | 非功能需求 |

### 3.2 缺口（需本项目补齐）

| 缺口 | 影响 | 做法 |
|---|---|---|
| **不支持传统 AEAD**：`aes-128/192/256-gcm`、`chacha20-ietf-poly1305`、`xchacha20-ietf-poly1305` | 原版只支持这几种，不补齐，原版用户的服务器都连不上 | **D3：P0**。按 shadowsocks-go 的客户端接口自写 TCP、UDP 两部分，注册进 `service` 的客户端工厂 |
| **没有设置系统代理的代码**：项目面向服务端和 Linux | S1、S2 | 自写 `sysproxy` 包（WinINet `InternetSetOption`）。能参考的只有它的本地 socks5/http 入站 |
| 没有 PAC | 原版的 PAC 模式 | 见 S3、S4，取决于问题 Q2 |
| SOCKS5 和 HTTP 不能**同端口**监听 | C1：原版只有一个"代理端口" | 自写混合入站：嗅探首字节，`0x05` 走 SOCKS5，其余走 HTTP，再交给它的 socks5/httpproxy 处理 |
| 没有 SIP003 插件 | C7 | 自写：启动插件进程监听本地端口，SS 客户端改为拨号到该端口 |
| 没有 `ss://`（SIP002）、SIP008 解析 | M3、M4、M6 | 自写，放在 `config` 包 |
| 不支持 v2fly geosite `dlc.dat` | S4 | 自写 dlc.dat（protobuf）解析，转成它的 `domainset`；或在 PAC 模式下直接生成 PAC |
| 运行中不能热切换服务器或模式，`service` 面向长期运行的命令行进程 | 托盘切换 | 切换时取消旧 ctx 并用新配置重建 `Manager`；耗时和对已有连接的影响要在 spike 中验证 |
| SS2022 只有 AES 两种，没有 `2022-blake3-chacha20-poly1305` | 少数服务端用这个 | P2 |

### 3.3 集成方式

- **A. vendor/fork 到本仓库**（推荐）：在 `service/client.go` 的工厂里加传统 AEAD 分支，改动集中在少数几个文件，手动跟进上游。
- B. 作为 `go get` 依赖：不用改上游代码。但它的客户端工厂是包内的 `switch`（`service/client.go:368` 起），外部无法扩展，只能绕开 `service` 层自己组装中继，或者等上游接受 PR。
- C. 只参考设计，核心全部自写：工作量最大。

### 3.4 许可证

shadowsocks-go 用的是 AGPL-3.0，所以 ssmygo 整体用 **AGPL-3.0** 发布（D4）。AGPL-3.0 第 13 条允许与 GPLv3 代码合并，原版的资源（图标、`abp.js`、多语言文案）可以直接使用。

## 4. 功能需求

优先级：**P0** = 首版必须；**P1** = 对齐原版；**P2** = 可选或以后再做。
（P0/P1 划分本身待确认，见 Q1）

### 4.1 代理核心（不依赖 UI）

| ID | 需求 | 优先级 | 原版对应 | 来源 |
|---|---|---|---|---|
| C1 | 本地**单个代理端口**（默认 `127.0.0.1:1080`），同时支持 SOCKS5 和 HTTP/HTTPS（CONNECT） | P0 | Listener + privoxy | 混合入站自写，其余复用 |
| C2 | SS 客户端 TCP：**传统 AEAD 五种** + **SS2022 两种** + `none`/`plain` | P0 | Encryption/AEAD | AEAD 自写，SS2022 和 none 复用 |
| C3 | SOCKS5 UDP ASSOCIATE → SS UDP | P1 | UDPRelay | 复用 + 传统 AEAD UDP 自写 |
| C4 | 允许局域网连接（监听 `0.0.0.0`） | P0 | shareOverLan | 复用 |
| C5 | IPv6（本地 `[::1]`，远端 IPv6） | P2 | isIPv6Enabled | 复用 |
| C6 | 前置代理（SOCKS5 / HTTP，可选认证和超时） | P1 | ForwardProxyConfig | 复用（客户端链） |
| C7 | SIP003 插件：设置环境变量 `SS_REMOTE_HOST/PORT`、`SS_LOCAL_HOST/PORT`、`SS_PLUGIN_OPTIONS`，用 Job Object 保证插件随主进程退出；可显示插件输出 | P1 | Sip003Plugin | 自写 |
| C8 | 多服务器策略：负载均衡、高可用等，映射到 `clientgroup` 的各个策略 | P1 | Strategy/* | 复用 |
| C9 | 流量统计：实时上下行速率、累计流量，供托盘角标和日志窗口的流量图使用 | P1 | 流量图 | 复用 `stats` |

> 不实现：已废弃的 Stream 加密（rc4、aes-cfb 等），原版 v4 也已经移除。

### 4.2 系统代理与分流

| ID | 需求 | 优先级 | 原版对应 |
|---|---|---|---|
| S1 | 托盘中的三种模式 **禁用 / PAC / 全局**（保留原版菜单）。用 WinINet 写当前用户的系统代理并通知刷新，替代 `sysproxy.exe` | P0 | SystemProxy |
| S2 | 首次启用前记录用户原来的代理设置，禁用或退出时**恢复** | P0 | SysproxyConfig |
| S3 | **"PAC"模式的实现**（二选一，见 Q2）：<br>**方案甲（推荐）**：系统代理统一指向本地端口，"PAC"模式由核心 `router` 按域名集 / GeoIP 分流，"全局"模式不分流。所有程序行为一致，不依赖程序是否支持 PAC。<br>**方案乙**：和原版一样，系统代理设为本地 PAC URL，由内置 PAC 服务提供 `pac.txt`，核心不分流 | P0 | PACServer |
| S4 | 规则来源：v2fly geosite `dlc.dat`，按 `geositeDirectGroups` / `geositeProxiedGroups` 选组，支持白名单（默认）和黑名单（`geositePreferDirect`）。方案甲转成 `domainset`，方案乙生成 PAC | P0 | GeositeUpdater |
| S5 | 在线更新 geosite（支持自定义 URL，可选 sha256 校验），更新请求走自身代理 | P1 | GeositeUpdater |
| S6 | `user-rule.txt` 用户自定义规则（ABP 语法）合并进规则 | P1 | user-rule.txt |
| S7 | 在线 PAC：使用用户指定的 PAC URL（方案甲下就是直接把系统代理设为该 URL） | P2 | useOnlinePac |
| S8 | 编辑本地 PAC / 用户规则：用系统编辑器打开（`Shell.OpenPath`），文件改动后自动重载 | P1 | PACDaemon |
| S9 | 保护本地 PAC（URL 带 secret）、复制本地 PAC 网址 | 方案乙 P1；方案甲不需要 | PACServer |

### 4.3 服务器管理与导入导出

| ID | 需求 | 优先级 |
|---|---|---|
| M1 | 服务器增、删、复制、上移、下移、编辑；字段见附录 B | P0 |
| M2 | 托盘"服务器"子菜单里单选当前服务器 | P0 |
| M3 | 从剪贴板导入 `ss://` 链接（SIP002 + 旧版 base64 格式，支持多行批量导入） | P0 |
| M4 | 分享服务器：生成 `ss://` 链接和二维码，可复制 | P1 |
| M5 | 扫描屏幕二维码导入（截屏 + 纯 Go 二维码识别） | P1 |
| M6 | 在线配置 SIP008：管理订阅 URL、更新、全部更新，按 group 替换服务器 | P1 |
| M7 | 关联 `ss://` 协议，点击链接即导入到正在运行的实例 | P2 |

### 4.4 托盘与窗口

| ID | 需求 | 优先级 | 布局 |
|---|---|---|---|
| U1 | 托盘图标：根据模式（禁用 / PAC / 全局）切换底图；鼠标提示显示当前服务器和模式 | P0 | — |
| U2 | 托盘图标叠加入站、出站流量角标 | P1 | 依赖 C9 |
| U3 | 托盘菜单结构与原版一致（附录 A），菜单项状态动态更新；未实现的功能先隐藏，不显示禁用的空项 | P0 | 附录 A |
| U4 | 编辑服务器窗口 | P0 | 4.5 ① |
| U5 | 日志查看器（含流量图） | P1 | 4.5 ② |
| U6 | 前置代理窗口 | P1 | 4.5 ③ |
| U7 | 快捷键窗口 | P1 | 4.5 ④ |
| U8 | 在线配置窗口 | P1 | 4.5 ⑤ |
| U9 | 分享服务器配置窗口 | P1 | 4.5 ⑥ |
| U10 | 检查更新和更新提示 | P2 | 4.5 ⑦ |
| U11 | 关闭窗口只是隐藏，只有菜单中的"退出"才真正退出 | P0 | — |
| U12 | 跟随系统深浅色 | P0 | MyGo 自带 |
| U13 | 多语言：首版语言待定（Q3）；文案直接使用原版 `Strings.*.resx` | P0/P1 | — |

### 4.5 界面布局规格（沿用 shadowsocks-windows，D2）

> **依据**：原版 `View/ConfigForm.Designer.cs`、`View/LogForm.Designer.cs`（WinForms）、`Views/*.xaml`（WPF），窗口尺寸取自 `View/MenuViewController.cs`。
>
> **还原原则**
> - 控件种类、排列顺序、分组、按钮位置、默认尺寸和原版一致；助记符只支持 Alt+字母，不显示下划线（MyGo 不支持 `&`，spike 修订）。
> - 原版尺寸按 96 DPI 像素计，ssmygo 按逻辑像素（DIP）使用同样的数值，高 DPI 下自动缩放。
> - MyGo 自己绘制控件，字体渲染和控件样式是 MyGo 风格，并跟随系统深浅色，**不追求和原版像素级一致**。
> - 文案使用原版 `Strings.zh-Hans.resx` / `Strings.resx` 的译文；下面线框图里的中文只是示意。

**① 编辑服务器**（ConfigForm，原版 582×553，ssmygo 为 **582×620**，见 spike-findings 界面规范）：外层是 6 列 × 4 行的网格

```
┌ 编辑服务器 ──────────────────────────────────────────────────┐
│ ┌──────────────┐ ┌ 服务器 ─────────────────────────────────┐ │
│ │ 服务器列表     │ │   服务器地址  [________________________]  │ │
│ │ (ListBox,     │ │   服务器端口  [________________________]  │ │
│ │  宽 180)      │ │         密码  [************************]  │ │
│ │              │ │               ☐ 显示密码                  │ │
│ │              │ │         加密  [chacha20-ietf-poly1305▼]  │ │
│ │              │ │     插件程序  [________________________]  │ │
│ │              │ │     插件选项  [________________________]  │ │
│ │              │ │               ☐ 需要命令行参数            │ │
│ │              │ │     插件参数  [________________________]  │ │
│ │              │ │         备注  [________________________]  │ │
│ │              │ │    超时(秒)  [________________________]  │ │
│ │              │ │         分组  [________________________]  │ │
│ └──────────────┘ └─────────────────────────────────────────┘ │
│ [添加(A)] [删除(D)]   代理端口 [1080________]                  │
│ [复制(C)]             ☐ 便携模式                              │
│ [上移(U)] [下移(O)]   [确定] [取消] [应用]                     │
└──────────────────────────────────────────────────────────────┘
```
- 右侧表单是两列网格：标签右对齐，输入框占满整列（原版 `tableLayoutPanel1`，14 行）。
- 只有勾选"需要命令行参数"后，"插件参数"才可编辑。
- 加密下拉框的选项依次是：传统 AEAD 五种、SS2022 两种、`none`/`plain`。默认 `chacha20-ietf-poly1305`（和原版一致）。
- 选 SS2022 时校验密码必须是长度正确的 base64 PSK。
- 列表中选中一项，右侧立即进入编辑；切换到别的项之前先校验当前表单，有错误就提示并阻止切换（原版行为）。
- "代理端口"就是 C1 的单个端口。

**② 日志查看器**（LogForm，初始 384×161，可调整大小）

```
┌ 日志查看器 ───────────────────────────────────┐
│ 文件(F)  视图(V)                              │  ← 菜单栏
│ [清空日志(C)] [字体(F)] ☐自动换行(W) ☐置顶(T)  │  ← 工具栏（可隐藏）
│ ┌────────────────────────────────────────────┐│
│ │ 日志文本（多行、只读、自动滚到底）            ││
│ ├────────────────────────────────────────────┤│  ← 上下分割，可拖动
│ │ 流量图（入站 / 出站速率曲线）                 ││
│ └────────────────────────────────────────────┘│
└───────────────────────────────────────────────┘
```
- 文件菜单：打开所在位置、退出。视图菜单：清空日志、修改字体、自动换行、置顶、分隔线、显示工具栏。
- 窗口位置、大小、字体、分割位置、是否显示工具栏，都保存在 `logViewer` 配置节。

**③ 前置代理**（ForwardProxyView，280×400）
```
类型
 ○ 不使用代理
 ○ SOCKS5
 ○ HTTP
详细设置
 地址 [__________]
 端口 [__________]
 超时 [__________]
认证（可选）
 用户名 [________]
 密码   [________]
                     [保存] [取消]
```
选"不使用代理"时，下面各项禁用。

**④ 快捷键**（HotkeysView，320×260）：三列网格，分别是标签、录入框、状态
```
切换系统代理         [Ctrl+Shift+Q ]  状态
切换代理模式         [             ]  状态
允许局域网连接       [             ]  状态
打开日志窗口         [             ]  状态
切换到上一个服务器   [             ]  状态
切换到下一个服务器   [             ]  状态
☐ 启动时注册快捷键
[全部注册]                    [保存] [取消]
```
- 录入框：按下组合键即录入，按 Backspace 清空。
- 状态颜色：绿色表示已注册，黄色表示被其他程序占用，透明表示初始状态。

**⑤ 在线配置**（OnlineConfigView，480×510）
```
┌──────────────────────────────┐ [更新]
│ 订阅 URL 列表                │ [全部更新]
│                              │ [复制链接]
│                              │
│                              │ [移除]
└──────────────────────────────┘
URL [______________________________] [添加]
```

**⑥ 分享服务器配置**（ServerSharingView，660×400）：左右两栏各占一半
```
┌──────────────────┐ ┌──────────────────┐
│   二维码          │ │ 服务器列表        │
└──────────────────┘ └──────────────────┘
[ss://......................................] [复制]
```

**⑦ 版本更新提示**（VersionUpdatePromptView）
```
标题：发现新版本
正文：更新说明
[更新]                       [跳过此版本] [以后再说]
```

### 4.6 系统集成

| ID | 需求 | 优先级 | MyGo 能力 |
|---|---|---|---|
| I1 | 单实例；再次启动时唤起已有实例的"编辑服务器"窗口 | P0 | `App.RequestSingleInstanceLock` / `OnSecondInstance` |
| I2 | 开机启动（托盘菜单"开机启动"） | P0 | `App.SetOpenAtLogin` |
| I3 | 全局快捷键：切换系统代理、切换代理模式、切换局域网连接、打开日志窗口、切换到上一个/下一个服务器；可选启动时注册 | P1 | `GlobalShortcut.Register` |
| I4 | 系统通知：模式切换、导入结果、订阅更新结果、错误 | P1 | `NewNotification` |
| I5 | 便携模式（原版默认开）：配置放在 exe 所在目录；关闭时放到 `%AppData%\ssmygo` | P0 | `App.Path` / `SetPath` |
| I6 | 退出时恢复系统代理、结束插件进程 | P0 | `OnBeforeQuit` |
| I7 | 系统休眠恢复或网络变化后自动恢复服务 | P2 | `Network.Status` |

### 4.7 配置兼容

| ID | 需求 | 优先级 |
|---|---|---|
| F1 | 读写 `gui-config.json`，字段名和原版一致（`configs[]`、`index`、`enabled`、`global`、`localPort`、`shareOverLan`、`portableMode`、`proxy`、`hotkey`、`logViewer`、`onlineConfigSource`、`geosite*`、`strategy` 等），原版配置拷过来直接能用 | P0 |
| F2 | 不认识的字段原样保留，读后写回不丢失 | P1 |
| F3 | 原子写入：先写临时文件，再 rename | P0 |
| F4 | `gui-config.json` 是用户配置；shadowsocks-go 的 `service.Config` 只在内存中生成，不写盘 | P0 |

## 5. 非功能需求

| 类别 | 要求 |
|---|---|
| 平台 | Windows 10 1809+ / Windows 11；x64 为 P0，arm64 为 P1 |
| 依赖 | 纯 Go，无 cgo，不释放外部 exe/dll；只有用户自己配置的 SIP003 插件是外部进程 |
| 体积 | 单个 exe ≤ 15 MB |
| 资源 | 只有托盘时工作集 ≤ 40 MB；打开窗口时 ≤ 120 MB（窗口用到时才创建，关闭即销毁）；从启动到托盘图标出现 ≤ 1 s（spike 修订） |
| 性能 | 单连接吞吐不低于原版；1000 个并发连接稳定 |
| 安全 | 日志不记录密码；本地端口默认只绑定回环地址 |
| 可靠 | 单个连接出错不影响其他连接；goroutine 和连接不泄漏；任何退出路径都会恢复系统代理 |
| 可测 | `core`、`config`、`sysproxy`、规则模块不依赖 MyGo，可单独做单元测试；集成测试用 shadowsocks-go 自带服务端（SS2022）和 shadowsocks-rust（传统 AEAD）做对端 |
| 分发 | `mygo build` 产出 exe 和安装包；自动更新为 P2 |
| 许可 | AGPL-3.0（D4） |

## 6. 架构草图（设计阶段细化）

```
cmd/ssmygo/              ← main：MyGo App 生命周期、单实例、托盘
internal/
  ui/                    ← MyGo native 窗口 ①–⑦、托盘菜单
  controller/            ← 状态中心：加载/保存配置，启停核心，向 UI 和托盘广播状态
  config/                ← gui-config.json 读写，ss:// / SIP008 解析
  core/                  ← 把 gui-config 翻译成 shadowsocks-go 的 service.Config，管理 Manager 重建
    mixed/               ← SOCKS5 + HTTP 单端口混合入站
    legacyaead/          ← 传统 AEAD 客户端（TCP + UDP）
    plugin/              ← SIP003 子进程 + Job Object
  rules/                 ← dlc.dat 解析、user-rule，产出 domainset（方案甲）或 PAC（方案乙）
  sysproxy/              ← WinINet 读写系统代理，保存/恢复
  qrcode/                ← 生成二维码，截屏识别
third_party/shadowsocks-go/  ← vendor（集成方式 A）
```
原则：UI 只通过 controller 订阅状态和发命令；controller 以下的代码都不 import MyGo。

## 7. 技术风险与待验证点

| # | 风险 | 应对 |
|---|---|---|
| R1 | MyGo 还很新（0.7.x），API 可能变动 | 锁定版本；先做 spike |
| R2 | 还原原版布局需要的组件 MyGo 是否都有：网格、分组框、单选按钮、密码框、下拉框、窗口菜单栏、可拖动分割视图、自绘曲线、图片显示、按键录入框 | spike 逐项核对，缺的用基础绘制自己拼 |
| R3 | MyGo 托盘：左键打开窗口、右键弹出菜单（文档给了 `OnRightClick` + `PopUpMenu` 的做法）；托盘图标能否频繁更新（流量角标） | spike 验证 |
| R4 | shadowsocks-go 的 `Manager` 重建耗时，以及对已有连接的影响 | spike 验证；必要时只重建出站 |
| R5 | vendor 之后跟进上游有合并成本 | 改动集中在少数文件，单独记录补丁 |
| R6 | 截屏 + 二维码识别的纯 Go 方案（GDI BitBlt + `gozxing`） | spike 验证 |
| R7 | WinINet 设置后要发 `SETTINGS_CHANGED` / `REFRESH` 通知；还要处理拨号 / VPN 连接各自的代理设置 | 参考原版 sysproxy 的行为 |
| R8 | 杀软可能误报"修改系统代理 + 网络监听"的 Go 程序 | 代码签名，不释放外部文件 |

## 8. 范围外

- 服务端功能（shadowsocks-go 有，但 UI 不暴露）
- Stream 加密、ShadowsocksR
- TUN 透明代理
- Linux / macOS 版本（架构上不阻止，但系统代理只做 Windows）

## 9. 已确认问题（2026-11-01，全部按推荐）

| # | 问题 | 结论 |
|---|---|---|
| Q1 | **首版范围**：只做 P0，还是 P0 + P1 一次对齐原版？ | 只做 P0：先出能用的版本 |
| Q2 | **"PAC"模式的实现**：方案甲（核心路由分流）还是方案乙（原版 PAC 文件）？ | 方案甲 |
| Q3 | **界面语言**：只做简体中文，还是中英双语跟随系统？ | 中英双语，文案直接用原版 resx |
| Q4 | **shadowsocks-go 集成方式**：vendor/fork 还是 go 依赖？ | vendor/fork |
| Q5 | **git 仓库**：`C:\Users\a` 本身是一个 git 仓库，是否在 ssmygo 目录下单独 `git init`？ | 单独 init |

**P0 首版范围**：单端口代理（7 种加密 + none）、系统代理三种模式和恢复、geosite 分流、托盘菜单、编辑服务器窗口、剪贴板导入、单实例、开机启动、便携模式、`gui-config.json` 兼容。

---

## 附录 A：原版托盘菜单（main 分支 `MenuViewController.cs`）

```
系统代理 ▸  禁用 / PAC / 全局
服务器   ▸  [服务器列表，单选] ─ 负载均衡 / 高可用 / 根据统计 ─
            编辑服务器… / 分享服务器配置… / 扫描屏幕二维码… / 从剪贴板导入 URL…
PAC      ▸  本地 PAC / 在线 PAC ─ 编辑本地 PAC… / 从 Geosite 更新本地 PAC /
            编辑 Geosite 用户规则… / 保护本地 PAC / 版本更新时重新生成 PAC /
            复制本地 PAC 网址 / 编辑在线 PAC 网址…
前置代理…
在线配置…
开机启动
关联 ss:// 链接
允许其他设备连入
编辑快捷键…
帮助     ▸  显示日志… / 显示插件输出 / 写入翻译模板 /
            更新 ▸ 检查更新… / 启动时检查更新 / 检查预发布版本 / 关于…
退出
```
方案甲下"PAC"子菜单的调整：保护本地 PAC、复制本地 PAC 网址这两项不再需要；"编辑本地 PAC"改为"编辑分流规则"。

## 附录 B：服务器字段（原版 `Model/Server.cs`）

| 字段 | 界面标签 | 说明 |
|---|---|---|
| `server` | 服务器地址 | 域名或 IP |
| `server_port` | 服务器端口 | 1–65535 |
| `password` | 密码 | SS2022 时为 base64 PSK |
| `method` | 加密 | 见 C2 |
| `plugin` | 插件程序 | 路径，相对或绝对 |
| `plugin_opts` | 插件选项 | 通过 `SS_PLUGIN_OPTIONS` 传给插件 |
| `plugin_args` | 插件参数 | 命令行参数 |
| `remarks` | 备注 | 菜单中显示的名字 |
| `group` | 分组 | SIP008 订阅用它来替换服务器 |
| `timeout` | 超时(秒) | — |
