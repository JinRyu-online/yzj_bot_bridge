# 发布日志（What's Change）

按版本倒序排列，最新在上。每次发版必须在此追加完整发布日志（见 [`release.md`](release.md) 1.5 节），内容与 GitHub Release notes 保持一致。

---

## Unreleased（yzj-cli 轮询入站 + DSH 桌面客户端 runtime）

### DSH 后端改为使用 DSH 桌面客户端
- `dsh_mode: desktop`（默认）：自动探测本机 DeepSeek Harness 桌面客户端，用它的 Electron 以 `ELECTRON_RUN_AS_NODE=1` 作 node、`app.asar` 内自带 dsh runtime 启动官方 SDK JSON-RPC server；不再要求单独安装 node / dsh CLI。
- 桥自动生成并维护 `~/.dsh/profiles/yzj-sdk` profile：复制桌面客户端 `profiles/desktop/cordis.patch.yml` 里的模型 provider 配置，只把 app 层换成 `@deepseek-ai/dsh-sdk-jsonrpc-server`（官方 0.2.1 已内置 `session/resume`，不再使用 `@bridge/dsh-jsonrpc-resume`）。
- 适配官方 0.2.1 协议：`session/prompt` 不再带 `cwd`（改用 `session/working-directory/set` 按会话设置工作目录）、`session/resume` 不再带 contentBlocks、`turn/end` reason 对象解析。
- 无桌面客户端时自动回退旧 `cli` 模式（`dsh_entry` + `node_bin`），保留兼容。
- GUI「AI 设置 → DSH」新增运行方式（desktop/cli）与桌面客户端路径；desktop 模式隐藏 CLI 入口/Node 字段。

## Unreleased（yzj-cli 轮询入站）

### 新功能
- 新增 inbound_mode: yzj_cli：通过官方 yzj-cli im message list 轮询已配置群/个人机器人会话，不再依赖本地登录的云之家桌面端；支持按 yzj_group_id 精确指定、yzj_trigger mention/direct/any、yzj_bot_names/yzj_bot_open_id 识别机器人。
- 图片消息支持：轮询到图片后 yzj-cli file download 下载到 bot workspace incoming_media/YYYYMMDD/，校验大小/类型/魔数并把本地路径注入 prompt；失败在回复中说明；按 keep_days 清理。
- 控制 API 新增 GET /v1/yzj/cli/status、GET /v1/yzj/cli/groups；GUI 新增 yzj-cli 状态卡、群/会话选择、触发方式/图片规则/别名等字段。
- yzj-cli 检测与自动安装：配置 bin → 受控目录 → npm 全局 → PATH；缺失时下载官方 .pak 并校验 SHA256（固定版本 0.1.8）。

### 优化/体验
- 轮询默认 5s（±20% 抖动）；遇 10000429 请求过于频繁/exit 4 自动指数退避（30s→5min），避免持续限流。
- 机器人自身消息（BOT- 开头）永不触发，避免自问自答；消息去重键带 source，兼容 WSS/Webhook/轮询并存。
- 回复出现整段完全重复（Cursor 恢复旧会话时偶发）时自动折叠为一份。

### 修复
- 兼容平台 isReference 返回 bool/字符串两种形态，避免轮询解析失败。

---

## v0.3.3（2026-08-21）

### 新功能
- 更新包改为后台下载：点「立即更新」后左下角显示下载进度条，悬停可查看已下载大小、速度与剩余时间，下载完成再启动安装向导。
- 记忆页锁定字段现在真正只读：锁定的输入框不可编辑、置灰显示；解锁后恢复可编辑。锁定/解锁只作用于单个字段。

### 优化/体验
- 记忆页画像字段重做：手动值与画像器推断值都直接显示在输入框里，推断内容可直接编辑覆盖（保存后成为手动值）；未修改的推断值不会被固化，画像器仍可继续更新。
- 记忆页每个字段标注来源徽标（手动/推断/已锁定），忌口字段与其他字段样式统一。
- 推断值不再另起一行展示，悬停徽标即可查看；锁定字段不再显示多余提示文字。
- 修复锁定按钮点击范围：点击字段标签文字不再误触发锁定，只有点按钮才锁定；点击标签正确聚焦输入框。
- 新建/编辑机器人、通道弹窗的「取消/保存」按钮固定在弹窗底部，不用滚动到底就能保存；记忆页详情操作按钮同样吸底常驻。

### 修复
- 修复记忆页「改不了配置」：清空某字段后保存即可删除手动值（之前会回退显示推断值，看起来改不掉）。
- 修复保存手动字段时把未修改的推断值误固化为手动值的问题，画像器不再被"锁死"。
- 后端加固：锁定字段即使绕过界面直接调 API 也无法改写，解锁后才能改。

### 其他
- 内部：记忆模块后端增加锁定字段 PATCH 拒绝校验与测试；GUI 新增记忆页编辑、锁定、吸底等 e2e 用例（41 个用例全绿）。

---

## v0.3.2（2026-08-21）

### 新功能
- 新增 DSH（DeepSeek Harness）CLI 后端：支持以 DSH 作为机器人推理后端，带共享进程池、会话恢复（resume）与按工作目录隔离会话。
- AI 设置页新增 DSH 配置区：可配置 DSH 入口、默认模型、Node 路径等字段，并自动扫描本机 DSH 安装。
- 设置页支持自动扫描 Cursor / Claude / DSH / Node 的可执行文件路径并自动填充。
- 后端引擎名称带品牌图标（设置页卡片标题 + 机器人后端下拉）。
- 右上角 toast 支持竖向多列排队（最多 3 条，超限挤出最旧）。

### 优化/体验
- 成功提示（「已找到…」「连通成功…」）显示 5 秒后渐隐并释放空间；按结果记忆，切页不再重复弹出。
- 设置页卡片顺序调整，DSH 卡片精简；机器人后端下拉按可用性过滤。
- 进入设置页自动拉取模型（Cursor/Claude/DSH），OpenAI 凭据齐全后自动测试连通。

### 修复
- 修复切页后成功提示重复播放的问题（FadingHint 按 resetKey 记忆，对象/原始值 key 统一处理）。
- 修复设置页用例的自动扫描竞态断言。
- 修复 OpenAI 连通提示在切换页面后再次弹出的问题。

### 其他
- 移除 anthropic_api_key 配置：Claude Code 统一使用本机登录凭据（配置项保留但不再使用）。
- 一键安装 DSH/Node 打开终端命令；打包资源内置 dsh-resources。
- 内部：新增 `docs/dsh-cli-backend-feasibility.md`、`docs/plan-dsh-jsonrpc-backend.md` 等设计与实测文档。

---

## v0.3.1（2026-08-21）

（此前版本的发布日志未按本规范记录，补记占位。）

- 新增版本更新功能：客户端「检查更新」弹窗、一键下载安装包并升级。
