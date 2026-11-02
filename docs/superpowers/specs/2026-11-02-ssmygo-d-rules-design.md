# ssmygo P1-D 设计：规则维护（在线更新 geosite、用户规则、自动重载）

> 日期：2026-11-02　状态：用户已在对话中确认，要求直接开始
> 上级文档：[`requirements.md`](../../requirements.md)（S5、S6、S8）；在 C 分支 `feat/c-sources` 之上开发
> 范围：P1 子项目 D。

## 1. 已确认的决定

| # | 决定 | 来源 |
|---|---|---|
| D-D1 | geosite 更新：菜单"从 Geosite 更新本地 PAC"手动更新；另外启动时若距上次成功更新超过 7 天，后台检查一次 | 用户选择 |
| D-D2 | 获取路径与订阅相同：有可用服务器时经本地端口（CONNECT 隧道），否则直连 | 与 C-D1 一致 |
| D-D3 | `user-rule.txt` 支持 ABP 子集；规则优先级高于 geosite | 设计 |
| D-D4 | 自动重载：每 2 秒检查 `user-rule.txt` 的修改时间与大小，不加新依赖 | 设计 |

## 2. geosite 在线更新

- 源：`geositeUrl`（原版字段），为空时用 `https://github.com/v2fly/domain-list-community/raw/release/dlc.dat`。
- 校验：默认源总是校验 `…/dlc.dat.sha256sum`；自定义源只有 `geositeSha256sumUrl` 不为空时才校验（与原版相同）。sha256sum 文件取第一个字段（十六进制）。
- 限制：下载 ≤ 20 MB（解压后计）；超时 60 s；必须能被 `rules.ParseDLC` 解析，并包含当前配置的所有 geosite 组，否则不采用。
- 保存：`<数据目录>/dlc.dat`，先写临时文件再改名。启动时优先用它，解析失败则用内置的。
- 采用后：重建规则（`rules.Build`），重新应用核心；记录 `ssmygoGeositeUpdatedAt`（RFC 3339）。
- 内容与当前一致（sha256 相同）：只更新时间，不重建、不通知。
- 失败：保留当前规则，手动更新时通知原因，后台检查失败只记日志并通知一次。
- 日志只记主机名（与 C-D6 相同）。

## 3. user-rule.txt

位置：`Paths.UserRule()`（数据目录下）。逐行解析：

| 写法 | 含义 |
|---|---|
| `!…`、`[…]`、空行 | 注释 |
| `\|\|example.com^`、`\|\|example.com` | example.com 及子域名走代理 |
| `@@\|\|example.com^` | example.com 及子域名直连 |
| `\|http://example.com/…`、`\|https://…` | 取主机名，走代理（`@@` 前缀则直连） |
| `example.com`（纯域名） | 同 `\|\|example.com` |
| 正则 `/…/`、含路径或通配的规则、`$` 选项 | 跳过并计数 |

- 路由顺序：私有网段 → 用户直连 → 用户代理 → geosite 直连 → geosite 代理（规则模式下）。全局模式不使用规则。
- 用户规则作为额外的 domain set（gob 文件，与 geosite 的放在一起）。
- 有跳过的行时，通知一次"user-rule.txt：已跳过 N 条不支持的规则"，日志里列出行号。

## 4. 菜单与编辑

- 托盘新增"PAC"子菜单（原版名称），含：
  - "从 Geosite 更新本地 PAC"（`Update Local PAC from Geosite`）
  - "编辑 Geosite 用户规则…"（`Edit User Rule for Geosite...`）：文件不存在时先写入带语法说明的模板，然后用系统默认编辑器打开（`mygo.Shell.OpenPath`）。
- 菜单项不可用的情况：没有。

## 5. 自动重载

- 控制器每 2 秒比较 `user-rule.txt` 的修改时间和大小；变化后重建规则并重新应用核心（规则模式下才需要重新应用，但规则总是重建）。
- 文件解析失败（读不了）：保留上一次的规则，通知一次。
- 文件被删除：视为没有用户规则。

## 6. 错误处理

| 情况 | 结果 |
|---|---|
| 下载失败、超时、超过 20 MB | 保留当前规则；手动时通知原因 |
| sha256 不匹配 | 不采用，通知"校验失败" |
| 下载的文件缺少配置的组 | 不采用，通知缺少哪些组 |
| 保存失败 | 不采用，通知 |
| 用户规则有不支持的行 | 跳过并通知一次 |

## 7. 测试

| 层 | 测试 |
|---|---|
| geosite | `httptest`：正常、sha256 不匹配、自定义源无校验、超过 20 MB、不是 dlc、缺少组；通过本地端口获取；日志只有主机名 |
| userrule | 表格测试各种写法；跳过计数与行号 |
| rules | 用户规则生成 domain set；路由顺序（用户直连优先于 geosite 代理） |
| controller | 手动更新后规则生效（经本地端口实际分流）；7 天检查；相同内容不重建；`user-rule.txt` 修改后 2 秒内生效；读不了时保留旧规则 |
| ui | 菜单项；模板创建 |
| 人工验收 | 真实更新 geosite；编辑用户规则后浏览器行为改变；`docs/acceptance-d.md` |

## 8. 不做

- 在线 PAC（S7，P2）
- 原版 PAC 子菜单中方案乙专用的项（保护本地 PAC、复制本地 PAC 网址）
- 正则和路径规则
