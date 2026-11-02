# ssmygo P1-E 设计：多服务器策略与全局快捷键

> 日期：2026-11-02　状态：用户已在对话中确认，要求直接开始
> 上级文档：[`requirements.md`](../../requirements.md)（C8、I3、U7，§4.5 ④）；在 C 分支 `feat/c-sources` 之上开发
> 范围：P1 子项目 E。

## 1. 已确认的决定

| # | 决定 | 来源 |
|---|---|---|
| E-D1 | 策略只提供原版的两种：负载均衡、高可用；`strategy` 字段沿用原版 ID | 用户选择 |
| E-D2 | 快捷键默认不在启动时注册（与原版相同） | 用户选择 |
| E-D3 | 策略组只包含有效且不用插件的服务器；有服务器被排除时菜单项注明 | 设计（A1 同时只运行一个插件） |

## 2. 多服务器策略

- `strategy`（原版字段）：`""`（不用策略，用 `index` 指定的服务器）、`com.shadowsocks.strategy.balancing`（负载均衡）、`com.shadowsocks.strategy.ha`（高可用）。原版其他 ID 当作空。
- 核心配置：每台成员服务器一个客户端（`proxy-0`、`proxy-1`…），加一个 `clientGroups` 项 `proxy`：
  - 负载均衡：`tcp.policy` 和 `udp.policy` 为 `random`。
  - 高可用：`tcp.policy` 为 `min-max-latency`，`udp.policy` 为 `availability`，用 shadowsocks-go 默认的探测设置（探测目标、间隔按其默认值；实现时读 `clientgroup/probe.go` 确认默认值合理并在测试中固定）。
  - 路由仍指向 `proxy`，所以规则分流、UDP、前置代理（每个成员都经前置代理拨号）都照常工作。
- 成员：所有能通过 `Validate()` 且 `Plugin` 为空的服务器。没有成员时退回单服务器模式（用 `index`），并通知。
- 选中某台服务器（菜单或"编辑服务器"确定）时清除策略，与原版相同。
- 托盘：服务器菜单顶部是两个策略单选项，然后分隔线和服务器列表；启用策略时服务器项都不勾选。提示中服务器名换成策略名。
- 状态：`State.Strategy`（空、`balancing`、`ha`）、`State.StrategyExcluded int`（被排除的插件或无效服务器数）。

## 3. 全局快捷键

- 配置 `hotkey`（原版字段）：`SwitchSystemProxy`、`SwitchSystemProxyMode`、`SwitchAllowLan`、`ShowLogs`、`ServerMoveUp`、`ServerMoveDown`（字符串，原版 WPF 格式，如 `Ctrl+Shift+Q`、`Ctrl+Alt+D1`），`RegHotkeysAtStartup`（布尔，默认 false）。
- 格式转换：`internal/hotkey`（不依赖 MyGo）在原版格式与 MyGo 加速键格式之间转换：修饰键 `Ctrl`/`Alt`/`Shift`/`Windows`；按键 `A`–`Z`、`D0`–`D9`↔`0`–`9`、`F1`–`F24`、`NumPad0`–`NumPad9`、常见 `Oem` 键、方向键、`Home`/`End`/`PageUp`/`PageDown`/`Insert`/`Delete`/`Space`/`Tab`/`Enter`。无法转换的视为空并记日志。
- 动作：
  - 切换系统代理：禁用 ↔ 上次启用的模式（默认 PAC）。
  - 切换代理模式：PAC ↔ 全局（禁用时先启用为 PAC）。
  - 允许局域网连接：开关。
  - 打开日志窗口。
  - 上一个 / 下一个服务器：按列表循环选择，并清除策略（与原版相同）。
- 注册：`mygo.GlobalShortcut.Register`；启动时只有 `RegHotkeysAtStartup` 为真才注册。
- 动作在后台运行（与托盘相同的 `runAction`），快捷键回调不阻塞。

### 3.1 快捷键窗口（320×260，原版 HotkeysView）

```
切换系统代理         [Ctrl+Shift+Q ]  ●
切换代理模式         [             ]  ●
允许局域网连接       [             ]  ●
打开日志窗口         [             ]  ●
切换到上一个服务器   [             ]  ●
切换到下一个服务器   [             ]  ●
☐ 启动时注册快捷键
[全部注册]                    [保存] [取消]
```

- 录入框：获得焦点后按下带修饰键的组合即录入；Backspace 清空；单独的修饰键不录入。
- 状态：绿色已注册；黄色被占用或无法解析（鼠标悬停说明）；透明为初始状态。
- 全部注册：按当前录入注册（先注销本程序已注册的），显示每项结果，不保存。
- 保存：写入配置；若勾选了启动时注册或已经点过全部注册，则按新设置重新注册。
- 取消：关闭，不保存；已"全部注册"的保持注册（与原版相同）。
- 托盘菜单：原版"编辑快捷键…"，放在"允许其他设备连入"之后。

## 4. 错误处理

| 情况 | 结果 |
|---|---|
| 快捷键被其他程序占用 | 该项黄色，其余照常注册 |
| 配置中的快捷键无法解析 | 视为空，日志一条 |
| 策略没有可用成员 | 退回单服务器，通知 |
| 成员全部探测失败（高可用） | 由 shadowsocks-go 决定（仍会选一个）；不额外处理 |

## 5. 测试

| 层 | 测试 |
|---|---|
| hotkey | 原版格式 ↔ 加速键表格测试；无法解析的输入 |
| core.Build | 黄金文件 `strategy-balancing`、`strategy-ha`、`strategy-forward`（每个成员有 dialerClient）；插件服务器被排除；无成员退回 |
| runner | 负载均衡两台 aeadtest 服务器都收到连接；高可用选出可用的那台（一台关闭） |
| controller | 选择策略、选服务器清除策略、上一个/下一个循环、切换系统代理记住上次模式、LAN 开关 |
| ui | 菜单策略项与排除说明；快捷键窗口录入、Backspace 清空、状态颜色、保存、全部注册（注入的注册函数）；浅色、深色快照 |
| 人工验收 | 真实快捷键（含被占用的情况）；两台真实服务器的负载均衡与高可用；`docs/acceptance-e.md` |

## 6. 不做

- 原版以外的策略（轮询、按统计等）
- 托盘菜单中显示每台服务器的延迟
