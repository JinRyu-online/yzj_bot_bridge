# 发布日志（What's Change）

按版本倒序排列，最新在上。每次发版必须在此追加完整发布日志（见 [`release.md`](release.md) 1.5 节），内容与 GitHub Release notes 保持一致。

---

## v0.4.3（2026-10-23）

### 新功能

- **WSS 入站图片/文件**：`websocket` / `both` 模式现在会解析推送帧里的附件（图片、文件），经 yzj-cli 下载到机器人 workspace，并把本地路径随问题一起交给模型。纯 WSS 部署也能读到附件，不必再靠轮询消息补拉。
- **思考强度「关闭」**：DSH 思考强度新增 `off`（关闭思考），不再产出思考过程，响应更快、更省 token。
- 新增 `defaults.ws_debug_frames`（默认 `false`）：需要排查 WSS 帧结构时可打开，把原始帧（截断 4KB）写入运行日志。开启后会记录消息正文，排障完请关闭。

### 修复

- **Skill 被自动取消勾选**：历史安装目录名与 `SKILL.md` 里的 `name` 不一致时（例如目录 `border-log`、清单 `border_log`），保存机器人配置会把它误判成「未安装」并从 `skills` 里删掉。现在按清单 `name` 兼容解析，不再误删。
- **WSS 模式拿不到图片/文件**：以前只把 `[图片]` / `[文件]:名字` 占位文本交给模型，模型只能拿旧文件硬答或去本地磁盘乱找；现在图片和文件都会真正落盘并注入路径。
- 附件下载失败时（超大小、扩展名被拦截、下载出错）会在提示里明确说明，不再静默跳过；文件失败也不会再被误标成「图片下载失败」。

### 优化

- 附件下载统一走 `yzj_cli.attachment` 配置：`files_enabled`（默认 `true`）、`file_max_mb`（默认 `50`）、`max_files`（默认 `5`）、`file_block_ext`（默认拦截 `.exe/.bat/.ps1` 等可执行文件）。超出上限或被拦截时只提示、不落盘。
- 附件下载器不再依赖轮询群：纯 WSS 部署同样会注入；yzj-cli 未安装/未登录时会明确告知模型「附件不可用」。
- 图片与文件下载共用同一套 workspace 清理策略（`yzj_cli.image.keep_days`）。

### 其他

- 新增 [WSS 帧样本与字段说明](wss-frame-samples.md) 和对应的单元测试 fixture。
- 补充 skills 目录名兼容的回归测试，避免同类问题再次出现。

---

## v0.4.2（2026-10-22）

### 新功能

- **会话空闲重置**：新增 `session_idle_reset_hours`（默认 `3`，`0` 关闭）。同一个机器人/会话超过该小时数没有请求，下一条消息自动清空旧会话并新开一个，避免长期闲置后上下文串味。
- 该配置支持 Cursor CLI、Claude Code、DSH（headless/sdk）等持久会话后端；可在「机器人 → 编辑 → 空闲重置（小时）」里按机器人单独设置。
- `session_mode=oneshot` 本来每条消息都会新会话；`shared` / `per_user` 下按各自的会话 key 分别计时。

### 说明

- 旧版本会话文件没有 `last_used_at`，升级后不会立刻被判定为空闲；第一次请求会补上时间戳，之后开始计时。
- 空闲重置只影响会话上下文，不影响 workspace、项目绑定或机器人配置。

---

## v0.4.1（2026-10-22）

### 新功能

- **DSH 思考强度配置**：新增 `defaults.dsh_reasoning_effort`，并在「AI 设置 → DSH」增加下拉框，可选 `默认 / 低 low / 中 medium / 高 high`。
- 自定义 OpenAI 兼容 provider（如 kuaidi100 的 `deepseek-flash`）会自动声明 `reasoningEfforts` 并关闭 DSH 的 developer role；否则 DSH 会报 `UNSUPPORTED_REASONING_EFFORT` 或网关 422。
- `high` 思考更充分但更慢、更耗 token；不设置时保持模型/provider 默认行为，不影响现有配置。

### 说明

- 思考强度只对支持 reasoning 的 DSH 模型生效；不同 provider 支持的档位可能不同，DSH 返回不支持时会直接报错。
- 配置同时作用于 headless 与 sdk 模式生成的 DSH profile；旧 `cli` 入口保持原行为。

---

## v0.4.0（2026-10-21）

### 新功能

- **云之家 yzj-cli 轮询入站**：不再依赖本地登录的云之家客户端，桥通过 `yzj-cli` 轮询你配置的群/个人机器人会话；支持文本 @、引用机器人消息、图片消息。
- **图片接收**：轮询到图片后自动下载到机器人工作目录，并把本地路径写入提示词，让模型直接读图/读文件；默认单图 20MB、保留 7 天，支持 `quote_bot | any | off` 三种触发方式。
- **每个通道独立轮询配置**：多群机器人可以在「机器人 → 通道 → 编辑」里为每个通道分别设置 `yzj_group_id`、群名、触发方式、机器人别名和 `yzj_bot_open_id`，不会再一个群的消息让所有群一起回复。
- **DSH 桌面客户端 runtime**：新增 `dsh_mode=headless`（默认），直接调用本机 DeepSeek Harness 自带 headless CLI，不再要求单独安装 node/dsh；同时保留 `sdk`（常驻 SDK server）和 `cli`（旧入口）两种兼容模式。
- **DSH 桌面路径自动扫描**：AI 设置里新增「重新扫描」按钮，自动从注册表/常见安装目录找到 DeepSeek Harness，填入并写入配置。
- **DSH 思考过程日志**：headless 模式解析 `thinking` 事件，运行日志会输出 `dsh headless thinking: ...`。
- **Cursor 会话按自然日重置**：新增 `cursor_daily_reset`（默认开启），跨天后自动丢弃旧 `--resume` 会话，避免隔天上下文串味。

### 优化 / 体验

- 机器人列表和详情页的「后端引擎 / 入站模式」改为人类可读标签（如 `DSH（DeepSeek Harness） · yzj-cli 轮询`），不再直接显示 `cursor_cli` / `websocket` 这类内部值。
- 通道卡片会正确展示 yzj-cli 的轮询状态；限流重试不再误报「未连接」，顶部汇总改为「在线」统计。
- 通道编辑弹窗按入站模式展示对应字段；新增通道时不再自动继承角色级会话 ID，并会阻止同一机器人下重复使用同一个 `yzj_group_id`。
- 全局下拉框内容字号缩小到 13px，长列表更紧凑。
- DSH 模型列表兼容桌面客户端 0.2.x 的 `profiles/desktop/cordis.patch.yml`，支持读取多个 profile 并保留当前选中的 vision 模型。

### 修复

- **跨群抢答**：WSS 推送会发给账号下所有机器人连接，之前未按目标机器人过滤，A 群 @机器人会导致其它群机器人一起回复；现在按 `robotName` / `yzj_bot_open_id` 精确过滤。
- **同群多机器人抢答**：同一个群里 @寄件日志查询 时，同城日志查询也会响应；现在没有配置 `yzj_bot_open_id` 时按正文里的 `@机器人名字` 匹配，配置后按 openId 精确匹配。
- 修复 `inbound_mode=both` 时 Webhook 与 WebSocket 同一消息未跨入口去重、可能重复回复的问题。
- 修复 DSH headless 思考过程不输出到运行日志的问题。
- 修复超长回复分块时剩余内容被丢弃的问题；超过 9 条消息上限时明确标记“已截断”，不再静默丢内容。
- 修复轮询补拉历史时接口出错仍推进游标、导致消息永久跳过的问题；补拉期间遇到限流不再推进。
- 多个群轮询增加进程内共享的限流冷却（触发 10000429 后其它群一起等待），降低账号级限流。
- 修复私聊/个人机器人正文含邮箱或 `@scope/pkg` 时被误判成“@了别人”而不回复的问题（私聊不再套用群聊 @ 过滤）。
- yzj-cli 自动安装改为只信任编译进程序的固定校验和；`update.json` 里的 checksum 只做一致性检查，不能自证。
- WSS 支持按机器人 `robot_id` 精确匹配；鉴权帧缺少 `success` 字段时不再误计失败并自动停用通道。
- DSH headless 的 JSON error / 取消 / 超时现在会正确标记为 error/interrupted，不再报 ok；托管 profile 写入串行化。
- 修复切换 workspace 时 `AgentCWD` 被项目路径覆盖、导致会话不会按新目录重置的问题。

### 其他

- 打包脚本支持 `-SkipTests`，Go 测试改为串行执行，降低低内存机器打包失败概率。
- 补充 yzj-cli 轮询、DSH 桌面 runtime、通道路由相关单元测试与 GUI e2e 覆盖。

---

## 更早版本

历史版本发布日志见 GitHub Releases。
