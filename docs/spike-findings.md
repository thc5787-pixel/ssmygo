# Spike 结论（2026-11-01）

> spike 代码在 `spike/`（已加入 .gitignore，**用后即弃，不进入主线**）。
> 环境：Windows 11 Pro 26200，Go 1.27.3，MyGo v0.7.4，shadowsocks-go `65ba171`。

## 结论摘要

| 风险 | 结论 | 对设计的影响 |
|---|---|---|
| R1 MyGo 成熟度 | ✅ API 足够完整，`ui.NewTester` 能在无窗口时点击、输入、截图，界面可以写自动化测试 | 锁定 v0.7.4 |
| R2 还原原版布局 | ✅ 全部可以做到，有 3 处需要变通（见下） | 定一套 WinForms 风格的紧凑控件封装 |
| R3 托盘 | ✅ 创建、动态菜单、图标每秒刷新 5 次持续 30 秒，句柄数稳定在 451–453，没有泄漏<br>⏳ **左键 / 右键行为需要人工确认**（见文末） | — |
| R4 shadowsocks-go 重建 | ✅ 冷启动 17ms；重建（停止 + 启动 + 可用）平均 **3.4ms**；**重建后已有连接不会断** | 切换服务器 / 模式时直接重建 Manager |
| 非功能：内存 | ⚠️ 只有托盘时工作集 **30MB**；打开一个窗口约 **80MB**（D3D 渲染开销） | 窗口按需创建、关闭即销毁；修订指标 |
| 非功能：体积 | ✅ spike exe 去掉符号后 10.1MB（还没包含代理核心） | 15MB 目标大概率可以达到 |

## R2 细节：MyGo 组件对照表

| 原版控件 | MyGo | 结果 |
|---|---|---|
| ListBox（服务器列表） | `ui.List` + `ListState.Selected` | ✅ |
| GroupBox "服务器" | 没有对应组件；用 `Column().BorderWidth(1).Radius(4)` 加粗体标题模拟 | ✅ 变通 |
| TableLayoutPanel 两列表单 | `ui.Grid().ColumnTracks(FitContent(), Fr(1))`，标签 `JustifySelf(End)` | ✅<br>❌ 不要用 `ui.Form`：它的行高是 macOS 风格（约 44px），表单会超出 553 的高度 |
| TextBox / 密码框 | `TextInput`、`.Password()`、`.Height(25)` | ✅ |
| ComboBox（加密方式） | `ui.Select` | ✅ |
| CheckBox | `ui.Checkbox` | ✅ |
| RadioButton（前置代理类型） | `ui.RadioGroup` + `ui.Radio` | ✅ |
| 控件禁用联动 | `.Disabled(bool)` | ✅ |
| 窗口菜单栏（日志：文件 / 视图） | `Window.SetMenu` | ✅ 真机显示正常 |
| 工具栏 | `ui.Toolbar` | ✅ |
| 可拖动分割（日志 / 流量图） | `ui.SplitVerticalPercent` | ✅ |
| 流量图 | `.Draw` + `Painter.StrokePolyline` | ✅<br>❌ 不要用 `Painter.Line`：它只画水平线或竖线，斜线会画成阶梯 |
| 二维码图片 | `ui.Image` + `ui.DecodeImage(png)` | ✅ 有 API（二维码生成库还没选） |
| 快捷键录入框 | `TextInput.OnTextKey` 拿到 Mods+Key，再交给 `GlobalShortcut.Register` | ✅ 注册成功显示绿色，失败显示黄色 |
| 助记符 `&Add` | **MyGo 不支持**，会把 `&` 原样显示 | ✅ 变通：用 `c.Shortcut(ui.Alt, key)` 实现 Alt+A，测试已通过。标签不显示下划线 |
| 置顶 | `Window.SetAlwaysOnTop` | ✅ |
| 深色模式 | 自动跟随系统 | ✅ 截图确认 |
| 关闭即隐藏、托盘常驻 | `OnClose` + `PreventDefault`，`OnWindowAllClosed` | ✅ |
| 从后台线程刷新界面 | `Window.Update(fn)`、`mygo.RunOnMain` | ✅ 每 200ms 推一条日志，运行正常 |

**尺寸**：使用原版尺寸 582×553 和 25px 行高，编辑服务器窗口能完整放下，渲染截图确认过。"下移"按钮在宽 100 时文字仍被截断，需要按文字宽度自适应或加宽到 110。

## 界面规范（按 MyGo 设计文档修订，2026-11-01）

用户在真机截图中指出：输入框文字偏下、列表没有边框、表单和按钮之间有空洞、按钮文字被截断。根据 MyGo 自带文档（`docs/ui/styling.md`、`form.md`、`layout.md`、`grid.md`）定下以下规则：

| 规则 | 依据 | 原因 |
|---|---|---|
| 密度只通过主题调整：`t.Spacing = 3`（MyGo 的紧凑档），**不在控件上写死 `.Height()`** | styling.md "Spacing is the unit…" | 写死高度会挤掉内边距，文字就偏下了。`Spacing = 2` 会把复选框缩到不可用 |
| 表单用 `ui.Form` → `ui.Fieldset("Server")` → `ui.Field`，不再自己用 Grid 拼 | form.md | 标签右对齐、和控件首行文字对齐、点击标签聚焦控件、无障碍分组，这些都由框架处理 |
| 校验错误用 `Field.Error(msg)` 显示 | form.md / text-input.md | 红色边框加下方提示，读屏软件会读出来；不用弹窗 |
| 尺寸用主题单位：间距 `t.Space(n)`，按钮最小宽度 `t.Rem(7)`；按钮不用固定宽度 | styling.md "Size your own elements with the theme" | 跟随系统字号缩放，不会截断文字 |
| 列表和分组框都加 `Border(1, t.Border).Radius(t.Radius)`，两者 `AlignItems(Stretch)` 等高 | styling.md | 对应原版 Dock=Fill 的外观，不会出现空洞 |
| 底部按钮区用 `ui.Grid` 6 列，`RowStart`/`ColumnStart` 定位 | grid.md | 对应原版的 tableLayoutPanel7 |
| 表单区外层用 `ui.Scroll` | scroll.md | 多条错误提示同时出现时，在框内滚动，不会压住按钮 |
| 不在网格里用空标签的 `Field`（例如代理端口） | 实测 | `Field` 会在上方预留标签行，输入框就和左边的标签错位；改为直接用 `Border(1, t.Danger)` 标红 |
| 默认按钮：`Enter` = 确定，`Escape` = 取消 | button.md "A default button…" | 和原版对话框一致 |
| `ListState.Label`：在列表里输入首字母即可定位 | list.md | 和 Explorer 行为一致 |

**尺寸修订**：MyGo 控件在紧凑档下行高约 38 DIP（原版 WinForms 约 31px），编辑服务器窗口从 582×553 调整为 **582×620**，并把它设为最小尺寸。截图验证过：582×620 浅色、深色、850×618、校验出错四种状态都不会重叠。

## R4 细节：shadowsocks-go

测试拓扑：socks5 客户端 → 客户端 Manager（socks5 → ss2022） → 服务端 Manager（ss2022 → direct） → echo，全部在本机回环地址上。

| 测试项 | 结果 |
|---|---|
| 冷启动到第一次 echo 成功 | 16.7ms |
| 重建 10 次的平均耗时 | 3.35ms（停止约 1ms） |
| 重建前建立的长连接，重建后还能否使用 | **能**。`Stop` 只关闭监听，不关闭正在中继的连接 |
| 端口已被占用时启动 | `Run` 在 2ms 内返回 false，日志会写出 bind 错误 |
| `aes-256-gcm` 客户端 | `unknown protocol: "aes-256-gcm"`，确认需要自己补 |
| 引入方式 | sum.golang.org 还没收录最新的伪版本，`go get` 会失败；用 `replace` 指向本地副本没问题，这也印证了 vendor 方案 |

对设计的影响：
1. 托盘中切换服务器或模式时，直接用新配置重建 Manager，用户基本感觉不到。
2. 已有连接会继续走旧服务器，直到连接自然结束，和原版的行为一致。如果"禁用代理"时要求立刻断开所有连接，需要另外跟踪这些连接，目前不做。
3. 端口冲突要在界面上提示（通知加日志），不能静默失败。

## 需要修订的需求

| 项 | 原来 | 修订 |
|---|---|---|
| 内存 | 空闲 ≤ 40MB | 只有托盘时工作集 ≤ 40MB；打开窗口时 ≤ 120MB；窗口按需创建，关闭即销毁 |
| 助记符 | 和原版一致 | 支持 Alt+字母，不显示下划线 |

## 待人工确认（R3）

运行 `spike\ui` 的程序，确认以下几点：
1. 左键单击托盘图标会打开"Edit Servers"窗口。
2. 右键单击会弹出菜单；"System Proxy"子菜单的单选项可以切换，切换后图标颜色跟着变（蓝 = PAC，绿 = 全局，灰 = 禁用）。
3. 图标角标闪烁，鼠标悬停提示会更新。
4. 关闭窗口后程序还留在托盘里；菜单里的"Quit"才会退出。
