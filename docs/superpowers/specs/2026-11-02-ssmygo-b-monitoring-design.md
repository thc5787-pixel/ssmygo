# ssmygo P1-B 设计：流量统计、日志窗口与托盘角标

> 日期：2026-11-02　状态：待审阅
> 上级文档：[`requirements.md`](../../requirements.md)（C9、U2、U5，§4.5 ②）、[A1 设计](2026-11-01-ssmygo-a1-plugin-forward-design.md)（A1-D5：插件输出窗口放到 B）
> 范围：P1 子项目 B。实时流量统计、日志查看器（含流量图）、托盘流量角标、"显示插件输出"。

## 1. 已确认的决定

| # | 决定 | 来源 |
|---|---|---|
| B-D1 | 在本地端口计数：经过 ssmygo 本地端口的所有流量都算（走服务器和直连的都算） | 用户选择方案 A |
| B-D2 | TCP 和 UDP 都计数。TCP 在自己的 `mixed` 服务端计数；UDP 需要补丁 A4，在 UDP NAT 中继里计数 | 用户选择 |
| B-D3 | "显示插件输出"是帮助菜单里的勾选项，保存为 `showPluginOutput`（与原版同名）。关闭时日志窗口隐藏插件输出行；日志文件始终保留 | 用户选择 |
| B-D4 | 累计流量只统计本次运行，不跨次保存 | 与原版一致 |
| B-D5 | 流量图保留最近 5 分钟（每秒一个点，300 个点），数据在控制器里，打开窗口时图已经是满的 | 设计 |

shadowsocks-go 自带的 `stats` 只在 TCP 会话结束时累加字节，不能用来显示实时速率，所以不用它。

## 2. 数据流

```
mixed 服务端（TCP，包装每条客户端连接）──┐
UDP NAT 中继（补丁 A4，逐包）────────────┼─▶ traffic.Meter（两个原子计数：上行、下行累计）
                                          │
控制器采样器，每秒一次 ───────────────────┘─▶ 速率 = 累计差值；环形缓冲 300 个点
        │
        ├─▶ 托盘：角标图标（无 / 入 / 出 / 双向）+ 提示行"↑ 12 KB/s ↓ 340 KB/s"
        └─▶ 日志窗口：流量图 + 本次运行累计
```

方向：**上行**（↑、出站）= 应用经 ssmygo 发出的数据；**下行**（↓、入站）= 收到的数据。计的是载荷字节（TCP 的流数据、UDP 数据报的载荷），不含加密和协议开销。

## 3. 组件设计

### 3.1 `internal/traffic`（新包，不依赖 MyGo）

```go
type Meter struct{ /* up, down atomic.Uint64 */ }
func (m *Meter) Add(up, down uint64)
func (m *Meter) Totals() (up, down uint64)

type Sample struct{ Up, Down uint64 } // 这一秒的字节数（即速率 B/s）
type Snapshot struct {
    Rate              Sample   // 最近一秒
    TotalUp, TotalDown uint64
    History           []Sample // 由旧到新，最多 300 个
}

// Sampler 每 interval 读一次 Meter，算出差值，存入环形缓冲。
func NewSampler(m *Meter, interval time.Duration, keep int) *Sampler
func (s *Sampler) Tick()               // 采样一次（测试直接调用）
func (s *Sampler) Snapshot() Snapshot  // 拷贝，可在任意线程调用
```

- Meter 跟应用同寿命，不随 core 重建。`Apply`、切换服务器后累计和图都连续。
- 实现 `service.TrafficMeter` 接口（见 3.3）。

### 3.2 `core/mixed`：TCP 计数

- `HandleStream` 拿到客户端连接后，用计数包装（`Read` 计上行，`Write` 和 `ReadFrom` 计下行），再交给 SOCKS5 或 HTTP 处理。`CloseWrite` 等方法照常转发。
- 计数器从 `service.ServerConfig.TrafficMeter` 取得（注册工厂收到的就是这个配置）；为 nil 时不包装。

### 3.3 补丁 A4（shadowsocks-go，`service`）

```go
// TrafficMeter receives payload byte counts as they are relayed.
type TrafficMeter interface{ Add(up, down uint64) }

// ServerConfig:
TrafficMeter TrafficMeter `json:"-"` // set in Go by the embedding app; nil = off
```

- `server.go` 创建 `UDPNATRelay` 时把 `sc.TrafficMeter` 传进去。
- `udp_nat.go`：接收循环里解包成功后 `Add(length, 0)`；下行把一个包写回客户端成功后 `Add(0, payloadLen)`。为 nil 时不计。
- 其他中继（`UDPSessionRelay`、TCP 中继）不改。
- `core.Runner.Apply(sc)` 在启动前给所有服务端设置 `TrafficMeter`；`core.Build` 不变，黄金文件也不变（字段是 `json:"-"`）。

### 3.4 控制器

- `controller` 持有 Meter 和 Sampler，每秒 `Tick` 一次，然后通知订阅者（与现有 `Subscribe` 机制分开：`SubscribeTraffic(func(traffic.Snapshot))`，避免每秒触发整个 State 刷新）。
- `State` 不加流量字段。

### 3.5 托盘

- **角标**：最近一秒有下行 → "入"箭头；有上行 → "出"箭头；都有 → 两个箭头。与原版 `icon_in / icon_out / icon_both` 相同。
- 图标在运行时用 Go 的 `image` 包把箭头叠加到现有的 32×32 PNG 上合成，按"模式 3 种 × 任务栏深浅 2 种 × 角标 4 种"缓存。
- 只有角标状态变化时才换图标（最多每秒一次）。spike R3 已验证每秒 5 次刷新没有句柄泄漏。
- **提示**：有流量时加一行"↑ 12 KB/s ↓ 340 KB/s"，没有流量时不显示。总长仍限制在 127 字符。

### 3.6 日志窗口（U5，沿用原版 LogForm 布局，按 MyGo 设计文档优化）

```
┌ 日志查看器 ─────────────────────────────────────┐
│ 文件(F)  视图(V)                                │
│ [清空日志] [字体…] ☐自动换行 ☐置顶               │  ← 工具栏，可隐藏
│ ┌─────────────────────────────────────────────┐ │
│ │ 日志文本：只读，跟随末尾                      │ │
│ ├──────────────── 可拖动 ─────────────────────┤ │
│ │ ↑ 12.3 KB/s  ↓ 340 KB/s   累计 ↑ 5.1 MB ↓ 88 MB│
│ │ 流量图：上行、下行速率，最近 5 分钟            │ │
│ └─────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

- **打开**：托盘 帮助 ▸ 显示日志…。只开一个，再点一次把已有窗口提到前面。默认大小 640×420，最小 400×240（原版 384×161 放不下图）。
- **文本来源**：像 `tail -f` 一样跟随 `ssmygo.log`，每 0.5 秒检查一次新内容。
  - 打开时读最后 64 KB，从第一个完整行开始。
  - 最多保留约 2000 行，超出时丢掉最早的。
  - 文件变短或被换成新文件（日志轮转）时，从新文件开头读。
  - 单行超过 4 KB 时，显示时截断并加"…"，文件不改。
  - 只有当前已经在末尾时才自动滚动，用户往上翻看时不打断。
- **清空日志**：只清窗口里的内容，不动文件（与原版相同）。
- **字体…**：MyGo 没有系统字体对话框，用一个小窗口选择字体（`ui.FontFamilies()`）和字号。默认等宽字体 Consolas 9 pt。
- **自动换行、置顶**：勾选项，视图菜单里也有。置顶用 `Window.SetAlwaysOnTop`。
- **菜单**：
  - 文件：打开所在位置（在资源管理器中打开日志文件夹）、退出（关闭窗口）。
  - 视图：清空日志、字体、自动换行、置顶、显示工具栏。
- **插件输出过滤**（B-D3）：`showPluginOutput` 为 false 时，不显示 `msg="plugin output"` 的行。勾选项在托盘 帮助 菜单里，改变后日志窗口立即重新过滤已读的内容。默认 false，与原版相同。
- **颜色**：跟随 MyGo 的浅色、深色主题。原版的 `BackgroundColor`、`TextColor` 字段读入后原样写回，不使用。
- **流量图**：用 MyGo 的 `ui/chart`。上行、下行两条线，纵轴按数值自动用 B/s、KB/s 或 MB/s；鼠标悬停显示该时刻的值。数据来自控制器的历史，每秒更新。

### 3.7 配置（`gui-config.json`）

- `logViewer` 节：读写原版字段 `topMost`、`wrapText`、`toolbarShown`、`Font`（原版格式字符串，如 `"Consolas, 8pt"`）、`BackgroundColor`、`TextColor`。
- ssmygo 自己的字段也放在 `logViewer` 里：`width`、`height`、`left`、`top`、`maximized`、`splitter`（原版把这些存在用户设置里，不在 JSON 里）。
- 顶层 `showPluginOutput`。
- 未知字段原样保留（沿用 `extra` 的做法）。字段缺失或不合法时用默认值。
- 日志窗口关闭时保存；它是 `logViewer` 的唯一写入者。

## 4. 错误处理

| 情况 | 结果 |
|---|---|
| 日志文件不存在或读不了 | 文本区显示一行"日志文件不可用：…"，继续检查，文件出现后恢复 |
| 窗口开着时日志轮转 | 发现文件变短或换了新文件，从新文件开头读 |
| 超长行（如插件输出一大段） | 显示时截断到 4 KB 加"…"，文件不改 |
| core 重启（Apply、切换服务器） | Meter 属于应用，累计和图连续，不中断 |
| 计数溢出 | uint64，实际不会发生 |
| 更新托盘图标失败 | 记一次 Warn 日志，下次状态变化时再试 |
| `logViewer` 缺失或不合法 | 用默认值，保留原版其他字段 |

## 5. 测试

| 层 | 测试 |
|---|---|
| traffic | 并发 `Add`；采样器的速率、历史顺序、300 个点上限（直接调用 `Tick`） |
| mixed | 经本地端口的 TCP 字节两个方向都计到，包括半关闭；Meter 为 nil 时照常工作 |
| 补丁 A4 | UDP 逐包计数，上行和下行；经传统 AEAD UDP 时按载荷字节精确计数（不是加密后的大小）；Meter 为 nil 时照常工作 |
| runner | 重新 Apply 后累计继续 |
| 日志跟随 | 追加能读到；轮转和换文件；首次 64 KB 从行首开始；行数上限；超长行截断；插件输出过滤 |
| config | `logViewer`、`showPluginOutput` 读写往返，未知字段保留，原版 `Font` 字符串解析 |
| ui（`ui.NewTester` 无头） | 日志窗口浅色、深色和中文、英文快照；工具栏开关；角标图标与速率对应；提示行只在有流量时出现 |
| 人工验收 | 真实下载时图和速率正确、角标闪动、插件输出可过滤；新文档 `docs/acceptance-b.md` |

## 6. 不做

- 跨次保存累计流量
- 按服务器或按连接统计（"根据统计"的服务器策略属于子项目 E 的 C8）
- 导出日志、日志搜索
- 原版的自定义背景色、文字色
