# ssmygo P1-A1 设计：SIP003 插件与前置代理

> 日期：2026-11-01　状态：待审阅
> 上级文档：[`requirements.md`](../../requirements.md)（C6、C7、U6）、[P0 设计](2026-11-01-ssmygo-design.md)
> 范围：P1 子项目 A 的第一步（A1）。UDP（C3）放在 A2，不在本文范围内。

## 1. 已确认的决定

| # | 决定 | 来源 |
|---|---|---|
| A1-D1 | P1 拆成 A–E 五个子项目，先做 A；A 再拆为 A1（插件 + 前置代理）、A2（UDP） | 用户选择 |
| A1-D2 | 插件程序首次运行前弹窗确认，按"路径 + SHA-256"记住；文件被替换后重新确认 | 用户选择 A |
| A1-D3 | 当前服务器使用插件时，前置代理不生效（插件自己连接服务器，前置代理接不进去），并在托盘提示里说明 | 原版行为 |
| A1-D4 | 插件程序路径：绝对路径直接用；相对路径相对 exe 目录；只给名字时依次查 exe 目录、`plugins` 子目录、PATH，自动补 `.exe` | 原版 + 扩展 |
| A1-D5 | 插件输出写入日志文件；"显示插件输出"窗口放到子项目 B | 范围划分 |
| A1-D6 | 前置代理只用于连接 SS 服务器，按规则直连的流量不经过它 | 原版行为 |
| A1-D7 | 前置代理窗口按原版布局③（ForwardProxyView，280×400） | P0 D2 |

## 2. 原版行为对照（`Controller/Service/Sip003Plugin.cs`、`Model/ForwardProxyConfig.cs`）

| 项 | 原版 | ssmygo |
|---|---|---|
| 插件环境变量 | `SS_REMOTE_HOST`、`SS_REMOTE_PORT`、`SS_LOCAL_HOST`、`SS_LOCAL_PORT`、`SS_PLUGIN_OPTIONS` | 相同 |
| 插件命令行 | `plugin_args` 原样作为命令行，先展开 `%VAR%`（含上面五个变量） | 相同（Windows 原始命令行，`SysProcAttr.CmdLine`） |
| 本地端口 | 每次启动取一个空闲回环端口 | 相同 |
| 工作目录 | 程序工作目录 | exe 所在目录 |
| 插件窗口 | 默认不显示 | 不显示（`CREATE_NO_WINDOW`） |
| 插件随主程序结束 | Job Object | 相同（`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`） |
| 前置代理字段 | `useProxy`、`proxyType`（0 SOCKS5，1 HTTP）、`proxyServer`、`proxyPort`、`proxyTimeout`、`useAuth`、`authUser`、`authPwd` | 相同，读写 `gui-config.json` 的 `proxy` 节 |
| 前置代理超时 | 默认 3 秒，最大 10 秒 | 相同 |

## 3. 架构

```
cmd/ssmygo ── internal/ui ──┬── forwardwin.go（新：前置代理窗口）
                            ├── trust.go（新：插件确认框）
                            └── tray.go（菜单加"前置代理…"）
                 │
          internal/controller ── 管理插件生命周期、信任列表、前置代理设置
                 │
   ┌─────────────┼───────────────┬──────────────────┐
config       core/plugin（新）  core/forward（新）   core（Build 扩展）
(proxy 节,   进程、Job Object、  fwd-socks5/fwd-http  dialerClient 链、
 信任列表)    重启、就绪探测      + 连接超时            插件端点改写
                                    │
                 third_party/shadowsocks-go（补丁 C：dialerClient）
```

依赖规则不变：`controller` 及以下不 import MyGo。

## 4. 组件设计

### 4.1 config

```go
type ForwardProxy struct {
    UseProxy     bool   `json:"useProxy"`
    ProxyType    int    `json:"proxyType"`    // 0 SOCKS5, 1 HTTP
    ProxyServer  string `json:"proxyServer"`
    ProxyPort    int    `json:"proxyPort"`
    ProxyTimeout int    `json:"proxyTimeout"` // seconds, default 3, max 10
    UseAuth      bool   `json:"useAuth"`
    AuthUser     string `json:"authUser"`
    AuthPwd      string `json:"authPwd"`
}
func (p ForwardProxy) Validate() []FieldError // only when UseProxy: server, port 1–65535, timeout 1–10, user when UseAuth
func (p ForwardProxy) LogValue() slog.Value    // authPwd → "***"

type TrustedPlugin struct {
    Path   string `json:"path"`   // absolute, cleaned
    SHA256 string `json:"sha256"` // hex
}
// Config gains:
//   Proxy          ForwardProxy    `json:"proxy"`
//   TrustedPlugins []TrustedPlugin `json:"ssmygoTrustedPlugins"`
```
默认值：`ProxyTimeout` 为 3，其余为零值（和原版构造函数一致）。`proxy` 节原本作为未知字段被保留，现在改为解析，其中原版有而这里没有的字段仍然保留。

### 4.2 补丁 C（shadowsocks-go）

- `service.ClientConfig` 新增 `DialerClient string \`json:"dialerClient,omitzero"\``：另一个客户端的名字，这个客户端通过它拨号，不再直连。
- `service.ClientConfig` 新增 `DialTimeout jsoncfg.Duration \`json:"dialTimeout,omitzero"\``：只给注册的协议使用（前置代理的连接超时）。
- 新增 `func (c *ClientConfig) innerStreamClient(...) (netio.StreamClient, error)`：
  - `DialerClient` 为空时，返回原来的 `innerTCPClient`；
  - 否则从 `streamClientByName` 里查这个名字，找不到就报错 `dialer client %q not found (it must be listed before %q)`。
- 改用 `innerStreamClient` 的地方：`none`/`plain`、`socks5`、`http`、`2022-*` 四个分支，以及补丁 A 的注册分支。`direct` 不变。
- 补丁 A 的 `ClientFactory` 签名改为 `func(c *ClientConfig, inner netio.StreamClient) (netio.StreamClient, error)`。`legacyaead` 本来就接受 `netio.StreamClient`，不受影响。
- `DialerClient` 不为空且同时开启 UDP 时报错：前置代理不承载 UDP，A2 再处理。
- 测试：链式拨号能经过一个 socks5 客户端；引用的名字不存在时报错；引用排在后面的客户端时报错。

### 4.3 core/forward

```go
const (ProtocolSOCKS5 = "fwd-socks5"; ProtocolHTTP = "fwd-http")
// init(): service.RegisterClientProtocol for both.
// Factory: builds shadowsocks-go socks5.StreamClientConfig / httpproxy.ClientConfig
// on the given inner client, with auth from ClientConfig.Socks5 / ClientConfig.HTTP,
// and wraps DialStream in context.WithTimeout(ClientConfig.DialTimeout).
```
**超时从哪里来**：补丁 C 顺带给 `ClientConfig` 加一个 `DialTimeout jsoncfg.Duration \`json:"dialTimeout,omitzero"\``。shadowsocks-go 内置协议不读它，只有注册的工厂会用。

### 4.4 core（Build 扩展）

`Build(cfg, rs, plugin *PluginEndpoint)`，第三个参数为可选的插件端点：

| 条件 | clients |
|---|---|
| 当前服务器有插件 | `proxy` 的 endpoint = `127.0.0.1:<插件端口>`；不加前置代理 |
| 无插件，`Proxy.UseProxy` | `forward`（`fwd-socks5` / `fwd-http`，endpoint = 前置代理地址，`dialTimeout` = `proxyTimeout` 秒，带认证）排第一个，`proxy.dialerClient = "forward"` |
| 其他 | 与 P0 相同 |

插件服务器之前会被 `unusable` 拒绝（`ErrPluginUnsupported`），现在去掉这个限制。

### 4.5 core/plugin

```go
type Spec struct {
    Path       string // resolved absolute path
    Opts, Args string // plugin_opts, plugin_args
    RemoteHost string
    RemotePort int
}
type Process struct{ /* cmd, job, localPort, restarts, logger */ }

func Resolve(name, exeDir string) (string, error)          // A1-D4; ErrNotFound with the tried paths
func Start(s Spec, logger *slog.Logger, onCrash func(error)) (*Process, error)
func (p *Process) LocalPort() int
func (p *Process) Stop()                                   // kill + wait; idempotent
```
- **启动**：
  1. 取一个空闲的回环端口；
  2. 设置环境变量，展开 `%VAR%`；
  3. 以 `CREATE_NO_WINDOW` 方式启动进程，并加入 Job Object；
  4. stdout/stderr 逐行写日志（`plugin=<名字>`），保留最近 20 行；
  5. 每 50 ms 探测一次本地端口，最多等 5 秒。超时就停掉进程，返回 `StartError{Tail []string}`。
- **守护**：进程意外退出后按 1、2、4…30 秒的间隔重启，每次用新端口，并调用 `onCrash` 让控制器重建核心。一分钟内失败 3 次后，`onCrash` 收到 `ErrCrashLoop`，此后不再自动重启。
- **停止**：主动 `Stop` 不算崩溃。

### 4.6 controller

- **插件生命周期**：commit 前，如果下一份配置的当前服务器带插件：
  1. 解析插件路径；
  2. 检查信任；
  3. 插件规格变化时才重启（`Spec` 不同，或旧进程已退出）；
  4. 把端点传给 `core.Build`。
  
  commit 失败时停掉新插件；旧插件只要规格不变就一直在用，无需恢复。切到不带插件的服务器时停掉插件。
- **信任**：
  ```go
  type PluginNotTrustedError struct{ Path, SHA256 string }
  func (c *Controller) TrustPlugin(path, sha256 string) error // 追加到 TrustedPlugins 并保存
  ```
  校验时计算文件的 SHA-256，与列表中同一路径的记录比对。
- **崩溃回调**：在后台 goroutine 里运行，加锁后重建核心，换上新端口。`ErrCrashLoop` 时把 `lastErr` 设为它（界面翻译为"插件反复崩溃"），系统代理保持不变。
- **前置代理**：
  ```go
  func (c *Controller) SetForwardProxy(p config.ForwardProxy) error // Validate → commit
  ```
  当前服务器有插件时，`State.ForwardIgnored` 为 true（对应 A1-D3）。
- **新增错误类型**（界面会翻译）：`PluginNotFoundError{Name, Tried []string}`、`PluginNotTrustedError`、`plugin.StartError`、`plugin.ErrCrashLoop`。

### 4.7 ui

- **前置代理窗口 `forwardwin.go`**：按原版布局③，280×400，同样按需创建、关闭即销毁。
  - 选"不使用代理"时，下面的输入框禁用；不勾选认证时，用户名和密码禁用。
  - 填写有误时，在对应字段显示 `Field.Error`。
  - "保存"调用 `SetForwardProxy`，"取消"或 Esc 直接关闭。
- **插件确认框 `trust.go`**：托盘或编辑服务器窗口的操作收到 `PluginNotTrustedError` 时，在 goroutine 里调用 `mygo.Dialog.Message`。确认框显示插件名、完整路径和指纹前 16 位，按钮为"运行"（默认）和"取消"。选"运行"则调用 `TrustPlugin`，然后在主线程重试原操作。
- **托盘**：在"服务器"下方新增"前置代理…"。`ForwardIgnored` 时提示里加一行"前置代理：当前服务器使用插件，未启用"。
- **编辑服务器窗口**：去掉 P0 的"此版本暂不支持插件"提示和插件行标红。
- **文案**：原版 CSV 里已有 Forward Proxy、Type、No Proxy、SOCKS5、HTTP、Address、Port、Timeout、Username、Password、Save、Cancel 等，直接复用；没有的补进 `ssmygo.*`。

## 5. 数据流：选择一台带插件的服务器

```
托盘 → ctrl.SelectServer(i)
  → plugin.Resolve(name)              ✗ PluginNotFoundError → 通知
  → 信任检查                          ✗ PluginNotTrustedError → 确认框 → TrustPlugin → 重试
  → plugin.Start → 等端口就绪          ✗ StartError(最近几行输出) → 通知，回滚
  → core.Build(cfg, rules, 端点) → runner.Apply
  → 系统代理（如已开启）→ 保存 → 通知界面
```

## 6. 错误处理

| 情况 | 用户看到 | 状态 |
|---|---|---|
| 找不到插件程序 | 通知 + 托盘提示，附上查找过的路径 | 不切换，保持原来的服务器 |
| 插件未信任 | 确认框；取消则不切换 | 不切换 |
| 插件 5 秒内未开始监听 | 通知，附插件最近 5 行输出 | 停掉插件，回滚 |
| 插件运行中退出 | 自动重启，用户无感 | 核心换到新端口 |
| 插件一分钟内崩溃 3 次 | 通知"插件反复崩溃" | 停止重启；系统代理不变 |
| 前置代理连不上或认证失败 | 那一条连接失败并记日志 | — |
| 前置代理设置有误 | 窗口内 `Field.Error`，不能保存 | 不保存 |
| 插件服务器 + 前置代理 | 托盘提示说明前置代理未启用 | 正常 |

## 7. 测试

| 层 | 测试 |
|---|---|
| 补丁 C | dialerClient 链（经 socks5）；名字不存在；引用排在后面的客户端；dialerClient + UDP 报错 |
| config | `proxy` 节读写，原版样本往返不丢字段；`Validate` 边界值（超时 0 / 11、端口、开启认证但用户名为空）；密码不进日志 |
| plugin | 测试时编译 `testdata/fakeplugin`（读取 `SS_*`，把 TCP 转发到 REMOTE）：完整收发；`%SS_LOCAL_PORT%` 参数展开；`Stop` 后进程确实不在；被杀后自动重启且端口变化；连续崩溃触发 `ErrCrashLoop`；5 秒内不监听返回 `StartError` 带输出；`Resolve` 查找顺序 |
| forward | 测试进程内起 SOCKS5 / HTTP 代理（shadowsocks-go Manager）：流量确实经过它（连接计数）；用户名密码认证；对方不响应时超时 |
| core.Build | 黄金文件：插件端点、前置代理链、插件优先于前置代理 |
| controller | 插件服务器端到端（假插件）；未信任 → `TrustPlugin` → 成功；文件被替换后重新要求确认；找不到插件时回滚；切到无插件服务器后插件停止；`SetForwardProxy` 端到端；`ForwardIgnored` |
| ui | 前置代理窗口：字段、禁用联动、校验、保存；确认流程（假的 confirm 函数）；托盘菜单新增项 |

## 8. 不做（A1 之外）

- UDP（A2）
- "显示插件输出"窗口（B）
- 前置代理用于更新检查和订阅下载：P1 的 C、D 子项目各自决定
