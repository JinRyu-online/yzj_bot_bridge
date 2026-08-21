# 可行性报告：在 AI 设置中新增 DSH CLI 后端

> 日期：2026-06 · 评估对象：YZJ Bridge（Go + Tauri）
> 目标：在「AI 设置」中新增 **DSH（DeepSeek Harness）CLI** 引擎，使云之家机器人 / GUI 聊天可以选用 `dsh --profile headless` 作为执行后端。

---

## 1. 结论（TL;DR）

**可行，工作量低～中。** DSH CLI 与现有 Cursor CLI / Claude Code 属于同一种「命令行型后端」，可直接复用 `bridge/internal/backends/` 的既有模板（`claude.go` / `cursor.go` 为蓝本），Go 侧无新增依赖、GUI 侧无新增 npm 依赖。

> 更新（2026-06，GitHub 生态调研后）：约束 1（无 resume）与约束 3（Windows `.cmd`）**均有可行解法**，详见 [第 9 节](#9-github-生态调研与两个关键问题的解答2026-06-补充)。

但有 **3 个必须正视的产品约束**，其中最关键的是：

1. **DSH headless 是"一次一任务"模式**（每次启动全新 Agent、随机 session、不支持 resume），与桥的 `shared` / `per_user` 多轮会话模型不匹配 → 首版只能支持 `oneshot` 语义（或把历史拼接进 task 文本）。
2. **无 `--model` / `--mode` 参数**：bot 级模型覆盖、`ask`/`plan` 模式无法透传，模型只能用 DSH 全局默认。
3. **无流式 JSON 输出**：只能整段捕获 stdout 最终文本（与现有 CLI 后端最终回发行为一致，影响小）。

---

## 2. 现状盘点（项目侧）

### 2.1 后端抽象（扩展点清晰）

- 后端接口 `bot.Backend`：`Run(prompt, RunOpts) RunResult` + `CreateSession() (string, error)` + `ClearSession(string) (string, error)`
  - `bridge/internal/bot/bot.go:66-70`
- 引擎注册：`SupportedBackends()` 与 `Create()` 工厂 switch
  - `bridge/internal/backends/factory.go:13-30`
  - 当前：`cursor_cli` / `claude_code` / `openai` / `opencode`（opencode 为占位）
- CLI 型后端模板：
  - `cursor.go` / `claude.go`：`exec.CommandContext` + `processutil.HideWindow` + `StdoutPipe` 逐行读流 + `withRunTimeout` 超时 + `cliReplyOrError` 结果归一
  - `cli_stream.go`：`appendCLIPrompt`（`--` 结束选项）、非 JSON 行收集、结果/错误/空回复归一
  - `binresolve.go`：Windows `.cmd` → 优先解析同目录 `.exe` 的兜底
  - `cli_discover.go`：`DiscoverCLI(engine, configured)` 自动扫描 PATH + 常见安装目录，返回版本与官方安装命令（目前只支持 `cursor` / `claude` 两个引擎分支）

### 2.2 配置

- `bot.Config`：`CursorBin/ClaudeBin/OpenCodeBin` 等每引擎一个字段（`bridge/internal/bot/bot.go:9-56`）
- 默认值：`config.defaultMap()`（`config/config.go:23-55`）；映射：`mapToBotConfig()`（`config/config.go:277-336`，含每引擎 workspace 回退、模型优先级：通道/机器人覆盖 > `cursor_model`/`claude_model`/`openai_model`）

### 2.3 控制 API（GUI 数据来源）

- `GET /v1/backends/cursor/models`、`GET /v1/backends/claude/models`、`POST /v1/backends/openai/probe`、`POST|GET /v1/backends/cli/discover`（`bridge/internal/controlapi/server.go:74-77`）

### 2.4 GUI（Tauri + React）

- 引擎列表常量：`BACKENDS = ["cursor_cli", "claude_code", "openai", "opencode"]`（`gui/src/App.tsx:180`）
- AI 设置页：按引擎一个 `settings-group` 卡片（`group-cursor` / `group-claude` / `group-openai`，`App.tsx:2279-2533`），含 bin 路径 + 「重新扫描」/「一键安装」+ 模型下拉 + 保存到 defaults
- 机器人表单：backend 下拉（`testid="bot-backend"`，`App.tsx:3218`）+ 按引擎的条件字段
- GUI 设计规则：`.cursor/rules/gui-design.mdc`（按钮语义、toast 反馈、FancySelect 约定，改 GUI 必须遵守）

### 2.5 测试约束

- `TestBackendSmokeAllEngines`：**对 `SupportedBackends()` 每个引擎必须有冒烟用例**（Cursor/Claude 用本地 stub 可执行文件，OpenAI 用 httptest；不访问真实模型）
- `TestLiveBackendSmoke`：`YZJ_SMOKE=1` 时对本机真实配置逐个引擎发一条 prompt
- 新增引擎必须在 `backend_smoke_test.go` 补 `smokeDSH`，否则 `go test ./...` 会直接 `t.Fatalf("未给后端 %s 编写冒烟用例")`（`backend_smoke_test.go:40`）

---

## 3. DSH CLI 侧能力核实（源码依据）

> 依据本机安装的 `@deepseek-ai/dsh@0.1.0-rc.7`（`npx` 缓存内源码）。

| 项 | 事实 | 源码位置 |
|---|---|---|
| 入口 | `dsh` bin（Windows 为 `dsh.cmd`/`dsh.ps1`），`dsh --version` 可用 | `dsh/lib/bin.js` |
| headless profile | **官方内置模板**：`PROFILE_TEMPLATES.headless = ["@deepseek-ai/dsh-base", "@deepseek-ai/dsh-headless"]`，首次使用 `loadProfile` 自动 `initProfile`，**无需手动安装** | `dsh-app-boot/lib/index.js:323-325, 539-545` |
| 调用形式 | `dsh --profile headless "<task>"`（多词自动 join；task 必填） | `dsh-headless/lib/startup.js:20-39` |
| 会话 | 每次启动新建 Agent，session id 为 `session-<randomUUID>`；**无 `--resume` / `--session-id`** | `dsh-headless/lib/index.js:70-72, 94` |
| 输出 | 仅向 stdout 写**最终 assistant 文本 + `\n`**；无流式 JSON | `dsh-headless/lib/index.js:95-96` |
| 退出码 | turn 正常完成 → `exit 0`；出错 → `exit 1`（错误写入 stderr） | `dsh-headless/lib/index.js:97-98, 53-56` |
| 模型 | 用全局默认模型 `agentDefaultModel.currentSelection()`（settings `agent-default-model` 或 composition 配置），**CLI 无 `--model` 参数** | `dsh-headless/lib/index.js:69-76`；`dsh-agent-default-model/lib/index.js` |
| 工作目录 | `meta.cwd = process.cwd()` → 桥设置 `cmd.Dir = workspace` 即可生效 | `dsh-headless/lib/index.js:72` |
| 模式 | 无 `ask`/`plan` 区分，总是完整 agent 执行 | `dsh-headless/lib/index.js` |

---

## 4. 可行性评估

### 4.1 结论矩阵

| 维度 | 评估 | 说明 |
|---|---|---|
| 后端接入 | ✅ 低工作量 | 复刻 `claude.go` 模板即可（约 150–200 行） |
| 依赖 | ✅ 无新增 | Go 仅用 `os/exec` 标准库；GUI 无新增 npm 包 |
| 冒烟测试 | ✅ 可行 | 可仿照 `smokestub` 用 Go 写一个假 `dsh` 可执行文件，或 `cmd /c echo pong` 包装 |
| 会话连续性 | ⚠️ 受限 | 只能 `oneshot`；`shared`/`per_user` 需降级（历史拼 prompt）或等 DSH 支持 resume |
| 模型覆盖 | ⚠️ 受限 | bot 级 `model` 无法透传，用 DSH 全局默认 |
| ask/plan 模式 | ⚠️ 受限 | 总是 agent 执行；可在 task 文本附加指令弱化 |
| 启动开销 | ⚠️ 注意 | 每次新进程加载 Node + 插件树（秒级冷启动），群聊体验略差于常驻 CLI |
| 输出流式 | ✅ 可接受 | 现有 CLI 后端也是最终回发；`RunOpts.OnStream` 钩子可后续接 |

### 4.2 关键设计约束（实现时必须处理）

1. **Windows `.cmd` 包装**：npm 安装的 `dsh` 在 Windows 上是 `dsh.cmd`（无 `dsh.exe`）。Go 的 `exec.Command` **不能直接执行 `.cmd/.bat`**，`resolveWindowsBin` 的 `.exe` 优先策略对 dsh 无效 → 必须在后端里检测 `.cmd/.bat` 并用 `cmd.exe /c <bin> <args...>` 包装（`exec.Command("cmd.exe", "/c", ...)`）。这是与 cursor/claude 模板最大的实现差异点。
2. **会话语义**：`CreateSession()` 返回什么？建议返回固定占位（如空串或 `"dsh-oneshot"`），`Run` 每次全新执行；`SessionID` 回写保留但不用于 resume。GUI 需在机器人表单对 DSH 提示「每轮独立上下文」。
3. **模型字段**：AI 设置页可保留 `dsh_model` 字段但标注「由 DSH 全局设置决定，此处仅提示」；或暂不提供模型下拉（DSH 侧无 models 列表接口，`/v1/backends/dsh/models` 可返回空 + 提示）。
4. **历史传递（可选增强）**：`session_mode: shared/per_user` 下，桥可把最近 N 轮对话拼进 task 文本（受长度限制），实现「伪多轮」。DSH 自带 memory 机制（`~/.dsh` 持久化），但 headless 新 session 不恢复，属冷启动。

### 4.3 与 Skills 的协同点（加分项）

DSH 的文件系统技能源会扫描 **`workspace/.agents/skills/<name>/SKILL.md`**（项目级 root，rank 200，优先级高于用户级 `~/.agents/skills` 的 rank 500）。
→ 桥现有的 skills 物化逻辑（`docs/skills.md`：OpenAI 物化到 `workspace/.agents/skills/`）**对 DSH 天然生效**：DSH Agent 会自动发现并可按需加载，无需额外适配（注意同名时项目级优先，行为与 Cursor 的 `.cursor/skills` 一致）。`bots[].skills` 白名单的 prompt 注入（`优先使用这些 skills: …`）也仍有效。

---

## 5. 改动清单（含估算）

### Phase 1 — 核心后端（Go，约 0.5–1 天）

| 文件 | 改动 |
|---|---|
| `bridge/internal/backends/dsh.go`（新） | `Dsh` 后端：`CreateSession` 占位；`Run` 组装 `dsh --profile headless <task>`，`.cmd` 用 `cmd.exe /c` 包装，`HideWindow`、`cmd.Dir = workspace`、`withRunTimeout`（复用 `CursorTimeout` 或新增 `dsh_timeout`），捕获 stdout 取末行 / stderr / 退出码 → `cliReplyOrError` 风格归一 |
| `bridge/internal/backends/factory.go` | `SupportedBackends()` 加 `"dsh"`（或 `"dsh_cli"`）；`Create()` 加 case |
| `bridge/internal/bot/bot.go` | `Config` 加 `DshBin string`（`json:"dsh_bin"`）、`DshTimeout int` |
| `bridge/internal/config/config.go` | `defaultMap()` 加 `dsh_bin: "dsh"`、`dsh_model: ""`；`mapToBotConfig` 映射 + workspace 回退 + 模型优先级（`dsh_model`） |
| `bridge/internal/backends/backend_smoke_test.go` | 加 `smokeDSH`（stub 一个假 dsh 可执行文件，输出含 `pong`）；`liveSmoke` 补分支 |
| `bridge/internal/backends/cli_discover.go` | `DiscoverCLI` 加 `dsh` 引擎：names `["dsh"]`、候选目录（npm 全局 bin、`~/.dsh`、npx 缓存）、`dshInstallHint`（`npm install -g @deepseek-ai/dsh`）、版本探测 `dsh --version` |

### Phase 2 — 控制 API + GUI（约 0.5 天）

| 文件 | 改动 |
|---|---|
| `bridge/internal/controlapi/server.go` | `/v1/backends/dsh/models`（返回空列表 + 说明）；`cliDiscover` 支持 `dsh` 引擎读 `dsh_bin` |
| `gui/src/App.tsx` | `BACKENDS` 加 `"dsh"`；AI 设置页加 `group-dsh` 卡片（bin 路径 + 重新扫描 + 一键安装 + 模型说明）；机器人/通道表单自动获得 backend 选项；对 DSH 隐藏/弱化模型选择与 ask/plan 相关 UI，加「每轮独立上下文」提示；保存/加载 defaults 增补 `dsh_bin` |

### Phase 3 — 增强（可选，另计）

- Skills：确认/补齐 DSH 的 `workspace/.agents/skills` 物化路径
- 伪多轮：`shared`/`per_user` 下把历史拼入 task 文本
- 流式：`RunOpts.OnStream` 接 DSH 输出（需 DSH 侧提供流式/事件输出，当前无）
- 模型透传 / resume：等 DSH headless 增加 `--model`、`--resume` 后跟进

---

## 6. 风险与缓解

| 风险 | 等级 | 缓解 |
|---|---|---|
| DSH 为 `0.1.0-rc`，CLI 契约可能变动 | 中 | 版本探测 + 启动失败清晰报错（`agent_missing` 语义）；关注 DSH 发版 |
| 每任务冷启动（Node + 插件树，秒级） | 中 | 首版接受；后续可评估 DSH 常驻进程/预热（如桥内维护一个长驻 `dsh web` 树并走 API，属架构级方案，暂不推荐） |
| 无 `--model`，bot 级模型覆盖失效 | 中 | GUI 明确提示；若 DSH 支持 env 覆盖则透传 |
| Windows `.cmd` 包装 / PATH 差异 | 低 | 显式 `cmd.exe /c`；`DiscoverCLI` 返回绝对路径；允许用户在 GUI 手填 |
| 无流式 → 群聊长任务无中间反馈 | 低 | 与现有 Cursor/Claude 最终回发一致；可复用 `AckPending`/排队提示 |
| npx 缓存路径不稳定（`_npx\xxx\node_modules\.bin`） | 低 | 引导用户 `npm i -g @deepseek-ai/dsh` 获得稳定 `dsh` 命令 |

---

## 7. 建议的落地顺序

1. **Phase 1**：`dsh.go` + factory + config + 冒烟 stub → 在 `config.yaml` 里 `backend: dsh` 即可跑通（可先用真实 `dsh --profile headless` 手工验证）。
2. **Phase 2**：控制 API + GUI AI 设置卡片 + 扫描/安装提示。
3. **验收**：`go test ./...` 全绿（含新冒烟）；`YZJ_SMOKE=1` 对本机 DSH 真实跑一次；GUI 上新建 DSH 机器人并群聊验证。
4. **Phase 3**（视需要）：skills 物化、伪多轮、流式、模型透传。

---

## 8. 参考资料

- 后端模板：`bridge/internal/backends/claude.go`、`cursor.go`、`cli_stream.go`、`cli_discover.go`
- 后端接口与配置：`bridge/internal/bot/bot.go`、`bridge/internal/config/config.go`
- 控制 API：`bridge/internal/controlapi/server.go`
- GUI：`gui/src/App.tsx`（`BACKENDS` 常量、AI 设置页、机器人表单）
- 规则：`.cursor/rules/gui-design.mdc`、`.cursor/skills/feature-plan-flow/SKILL.md`
- DSH 源码（本机 npx 缓存 `@deepseek-ai/dsh@0.1.0-rc.7`）：
  - `dsh/lib/bin.js`（CLI 入口）、`dsh-app-boot/lib/index.js`（profile 模板与自动初始化）
  - `dsh-headless/lib/startup.js`、`dsh-headless/lib/index.js`（headless 行为：一次一任务、无 resume、exit 0/1）
  - `dsh-agent-default-model/lib/index.js`（默认模型选择）
  - `dsh-skill-filesystem/lib/index.js`（技能根目录：`workspace/.agents/skills` rank 200）

---

## 9. GitHub 生态调研与两个关键问题的解答（2026-06 补充）

### 9.1 调研范围与来源

| 来源 | 说明 |
|---|---|
| [deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness) | 官方仓库（本机装的 `0.1.0-rc.7` 即其发布产物） |
| [dsh-tui/dsh-tui](https://github.com/dsh-tui/dsh-tui) | 社区交互式 TUI（官方 TUI 的社区移植版，npm `@dsh-tui/dsh-tui`） |
| [dream12347/dsh-session-manager](https://github.com/dream12347/dsh-session-manager) | Web 会话管理插件（删除/恢复/归档/fork/暂停/压缩阈值） |
| [Anil-matcha/awesome-dsh-plugin](https://github.com/Anil-matcha/awesome-dsh-plugin) | DSH 插件生态清单 |
| [Ephemeral-AI-Lab/dsh-plugins](https://github.com/Ephemeral-AI-Lab/dsh-plugins) | 插件合集 |
| [xuanyuanzhifeng/dsh-plugin-agent-workflow](https://github.com/xuanyuanzhifeng/dsh-plugin-agent-workflow) | Agent 工作流插件 |
| [baihejiangnan/dsh-plugin-pack-web](https://github.com/baihejiangnan/dsh-plugin-pack-web) | Web Profile 一键复刻插件包 |
| [xizheyin/deepseek-harness-rs](https://github.com/xizheyin/deepseek-harness-rs) | Rust 重写版（侧面印证协议开放性） |
| npm `[@z47_rose_1/dsh-session-sync](https://www.npmjs.com/package/@z47_rose_1/dsh-session-sync)` | 会话同步插件 |
| [xmapst/xexec](https://pkg.go.dev/github.com/xmapst/xexec) | Go 侧 Windows cmd 执行库（非 DSH 插件） |

### 9.2 问题一：有没有现成插件能让 headless 支持 resume？

**结论：没有"headless + resume"形态的现成插件，但核心 API 本身支持会话恢复，自研一个轻量插件即可（约几十行）。**

证据链：

1. **官方 headless 无 resume，且 master 分支仍未加**：`packages/bundle/headless/src/startup.ts` 只解析 task 位置参数；`index.ts` 仍是 `SessionId('session-' + randomUUID())` 一次性创建。官方 README「已知限制」明确写：*"只提交一个任务：runner 没有用于交互式后续输入的 surface"*。
2. **但核心会话 API 支持恢复**（本机 `dsh-session` 源码）：
   - `Session` 有 `restore` 构造模式：`new Session(id, seed, header, "restore")`（`dsh-session/lib/index.js:1372`）
   - 注释明言：*"a resumed session's constructor seed is its full stored log"*，且 `session/end-seed` 事件标记恢复边界
   - `ctx.sessions` 服务可 flush / 持久化（`~/.dsh/sessions/*.jsonl`）
3. **社区已证明可用**：
   - `dsh-tui` 支持 `dsh --profile tui --resume <session-id>`（README 原话 "resume a persisted session"），其源码用 `agents.create({ sessionId })` + `assembleContextFor` + `SessionId` 实现——**这证明给 headless 加 resume 只需仿照这套 API**
   - `dsh-session-manager` 用 `ctx.sessionPersistence` / `ctx.agents` / `sessions.fork` 做恢复与 fork（Web 形态）

**推荐路线（自研轻量 bundle 插件，如 `dsh-headless-resume`）：**

- 复制官方 `headless` bundle 的 `cordis.patch.yml` 结构，新增一个 app 插件：
  - CLI 参数：`--resume <session-id>`（可选）+ task 位置参数
  - 有 `--resume` 时：用 `ctx.sessions` 定位/恢复持久化会话（`restore` 模式或按 id 加载），`agents.create({ sessionId: SessionId(id) })` 绑定同一会话，再 `followup` 任务
  - 无 `--resume` 时：行为与官方 headless 一致（兼容回退）
- 安装：`dsh plugin --profile headless add <repo-or-tgz>`（或作为桥的启动前检查）
- 桥侧：`CreateSession()` 返回 DSH 会话 id（首次无则创建）；`Run` 带 `--resume <id>` 续聊 → **`shared`/`per_user` 多轮语义即可成立**

> 备选：若不想自研，退而求其次——桥在 `shared`/`per_user` 下把最近 N 轮历史拼进 task 文本（伪多轮），成本最低但上下文受限；或等官方演进（仓库已有 `persistent-pty-sessions` 已实现特性笔记，说明持久化会话在持续演进）。

### 9.3 问题二：Windows `.cmd` 包装有办法解决吗？

**结论：有，且比 `cmd.exe /c` 更优——直接绕过 `.cmd`，用 `node` 执行 `bin.js`。** npm 的 `dsh.cmd` shim 本质就是 `node <pkg>/lib/bin.js`，Go 完全不需要经过 cmd 解释器。

方案对比（推荐度降序）：

| 方案 | 做法 | 优点 | 缺点 |
|---|---|---|---|
| **A. node 直调（推荐）** | 在 `DiscoverCLI`/`resolveDshInvocation` 阶段解析出 `node` 路径与 `@deepseek-ai/dsh/lib/bin.js` 绝对路径；Go 执行 `exec.Command(node, binJs, "--profile", "headless", "--", task)` | 无 shell、无引号转义、确定性最好、跨 .cmd/.ps1/.sh 统一 | 需要额外解析 bin.js 路径（可从 `dsh.cmd` 内容提取，或 `npm root -g` 定位，或 GUI 让用户填） |
| **B. `cmd.exe /c` 包装** | `exec.Command("cmd.exe", "/c", dshBin, "--profile", "headless", "--", task)`（原报告方案） | 改动最小 | cmd 会二次解析命令行，任务文本含引号/`&`/`|` 时需转义；`HideWindow` 与 cmd 窗口行为需测 |
| **C. 社区库 `xexec`** | `github.com/xmapst/xexec` 封装 Windows cmd 执行 | 已处理常见坑 | 新增第三方依赖（本项目目前零 Go 依赖，需权衡） |
| D. 等官方出 `.exe` | 官方发布原生二进制 | 一劳永逸 | 当前无；不可控 |

**实现要点（方案 A）：**

- `dsh.cmd`（npm 全局 bin shim）首行形如 `node "%~dp0\node_modules\@deepseek-ai\dsh\lib\bin.js" %*`（或 `"%~dp0\node.exe" ...`）——Go 读取该文件即可稳定提取 `bin.js` 绝对路径与 node 位置；提取失败时回退 `exec.LookPath("node")` + `npm root -g`。
- npx 缓存安装（`_npx\<hash>\node_modules\@deepseek-ai\dsh\lib\bin.js`）同理可解析。
- 后端新增 helper：`resolveDshInvocation(bin string) (nodePath, binJs string, err error)`，放在 `dsh.go` 内，配套单测（覆盖 .cmd 解析、node 缺失、绝对路径直给）。
- 这样 Phase 1 的 `dsh.go` 就完全绕开 Windows `.cmd` 陷阱，与 cursor/claude 模板的差异点从"必须 cmd 包装"降级为"多一步入口解析"。

### 9.4 对原报告结论的影响

- 约束 1（无 resume）：**可解**——自研 `dsh-headless-resume` 插件（第 9.2 节），或 Phase 3 再议；首版仍可先 oneshot。
- 约束 3（.cmd 包装）：**可解**——方案 A（node 直调）为推荐实现，写入 Phase 1 的 `dsh.go` 设计。
- 约束 2（无 `--model`）：仍无现成解法；`dsh-tui` 的 `/model` 与 `installModelSelection` 证明核心支持会话级模型选择，若自研 resume 插件，可顺带支持 `--model <id>`（仿 `installModelSelection`），把约束 2 一并解决——**自研插件是同时解锁 resume + 模型覆盖的最优路径**。

---

## 10. 补充评估：直接用 dsh-tui 替代自研插件？（2026-06，方向变更评估）

> 用户原倾向"不自研、直接用 dsh-tui"。本节约 3 小时源码实证后给出**否决结论**。

### 10.1 结论

**直接用 dsh-tui 作为 YZJ Bridge 的 DSH 后端：不可行（生产场景），强烈不建议。**

原因一句话：**dsh-tui 是纯交互式全屏 TUI，硬性要求真实 TTY，且没有任何"命令行传消息、stdout 取结果"的脚本化契约**——而 YZJ Bridge 是无头守护进程（Go sidecar，`HideWindow`，管道 IO），两者形态根本冲突。

### 10.2 证据（dsh-tui v0.1.2 源码，已浅克隆至 `%TEMP%\dsh-tui-src`）

1. **TTY 硬门槛**：`src/index.ts:1904-1905`
   ```ts
   if (!process.stdin.isTTY || !process.stdout.isTTY) {
     throw new Error('ui-tui: both stdin and stdout must be TTYs; use the one-shot @deepseek-ai/dsh-headless')
   }
   ```
   桥用管道启动（或隐藏窗口）→ stdin/stdout 非 TTY → **进程直接抛错退出**。官方自己都指向 headless。
2. **无消息入口**：`src/startup.ts` 的 CLI 仅 `--resume <session>` 一个选项，**没有 task 位置参数**——消息只能靠用户在 TUI 编辑器里敲（键盘输入驱动，`pi-tui` 全屏渲染）。
3. **无输出契约**：回复渲染在终端画布上（ANSI 帧），不是 stdout 纯文本；要拿回复只能**抓屏解析**。
4. **Windows 进一步受限**：in-place `/resume` 依赖 `process.execve`（Unix-only，`startup.ts:83` 判空跳过）；全屏 TUI 在 Windows 上需要 ConPTY。
5. **版本兼容风险**：peerDependencies 钉在 `rc.6`（本机 dsh 为 `rc.7`），README 自述 "expect breakage until upstream stabilizes"。
6. 测试里的 `tests/headless-terminal.ts` 只是**测试用**的 xterm 仿真器（`@xterm/headless`），不是运行时非交互模式。

### 10.3 若强行接入的代价（不推荐，供决策参考）

桥内为每个 DSH bot 分配 **Windows ConPTY**（需引入 Go 原生 PTY 依赖）→ 常驻 `dsh --profile tui --resume <sid>` 进程 → 逐条消息**注入按键** + **轮询抓屏** + ANSI/画布解析取回复 + 空闲判定。任何 DSH/dsh-tui 升级都可能打碎渲染解析；排队、并发、超时控制都更难。工程上属于"hack"，可靠性远低于 headless 路径。

### 10.4 替代路径对比

| 路径 | 自研量 | resume | 模型覆盖 | headless 契约 | 稳健性 | 结论 |
|---|---|---|---|---|---|---|
| 直接用 dsh-tui（PTY+抓屏） | 无（但桥侧 PTY/抓屏工程量大） | ✅ | ✅ | ❌ | 低（渲染耦合） | **不推荐** |
| 官方 headless + 桥侧拼历史（伪多轮） | 零 | ❌（每轮冷启动） | ❌ | ✅ | 高 | 兜底可用 |
| **薄封装 runner（原自研方案缩小版）** | ~50–100 行 | ✅ | ✅（顺带） | ✅ | 高 | **推荐** |
| 等官方 headless 支持 `--resume` | 零 | 待官方 | 待官方 | ✅ | 高 | 长期选项，可监控 |

> 注：所谓"薄封装"就是把 dsh-tui 已验证的 resume 机制（`dsh-agent-loop` 的 `configuredAgentIdentities` / `dsh-session` restore）在官方 headless bundle 上做一个**极小的 runner 替换**（约 50–100 行），输出契约保持官方 headless 不变（stdout 最终文本 + exit 0/1）。这不是"造一个插件体系"，而是 1 个文件的薄层，同时保留"官方支持后可卸载切换"的诉求。

### 10.5 对 feature-plan-flow 计划的影响

方向变更 → 回到 Step 3 更新计划：核心从"开发 dsh-headless-resume 插件包"改为下列之一（待用户拍板）：
- A. **薄封装 runner**（推荐）：`plugin/` 下只放一个小型 bundle（1 个 runner 文件 + patch），桥侧 `dsh.go` 按能力探测选择 `--resume` 或回退官方 headless；
- B. 纯官方 headless + 桥侧伪多轮（零 DSH 侧改动）；
- C. 观望：等官方 `--resume`，桥先按 oneshot 接入。

---

## 11. 补充评估：改用 ccch1mneyyy/dsh-TUI？（2026-06，第二轮方向评估）

> 用户第二轮指向 https://github.com/ccch1mneyyy/dsh-TUI（npm `@deepseek-harness-tui/dsh-tui`，v0.1.2 之外的另一生态 TUI，官方公众号收录）。已浅克隆至 `%TEMP%\dsh-TUI-ccch1mneyyy` 逐文件核实。

### 11.1 结论

**作为 YZJ Bridge 的无头后端：同样不可行**——它同样在启动时硬抛 TTY 错误（`src/dsh-adapter/plugin.ts:57-59`）。但它与上一轮评估的 `dsh-tui/dsh-tui` 有本质差异，且**顺带挖出了官方真正的无头通道**（见 11.4）。

### 11.2 该仓库是什么（与 dsh-tui/dsh-tui 的区别）

| 维度 | dsh-tui/dsh-tui | ccch1mneyyy/dsh-TUI |
|---|---|---|
| 定位 | pi-tui 小 TUI | **Claude Code 风格全功能 TUI**（自研 Ink/Yoga 渲染移植） |
| 发布 | `@dsh-tui/dsh-tui` | `@deepseek-harness-tui/dsh-tui`，官方公众号收录、VS Code 扩展、dshfind 收录 |
| 兼容 | peer 钉 rc.6 | **校验 rc.8，兼容 rc.7 / rc.6**（`src/dsh-adapter/contract.ts` 的 `UPSTREAM_VALIDATED_RC_LINES`）——本机 rc.7 可直接用 |
| 启动器 | `dsh --profile tui` | `dsh-tui` 命令 / `dsh-tui.cmd`（Windows），自动自举 profile、版本对齐检查 |
| 能力 | /resume、/model、/compact | 会话浏览器、fork/rewind、/workspace、presets、MCP、插件生态、/model（fork 续聊）、/export、trajectory 等 |

### 11.3 无头可行性证据（源码实证）

1. **TTY 硬门槛**：`src/dsh-adapter/plugin.ts:57-59`
   ```ts
   if (!process.stdout.isTTY) {
     throw new Error('dsh-tui requires an interactive terminal (stdout must be a TTY).')
   }
   ```
   → 桥管道启动直接抛错退出；同上一轮，无 TTY 则不可用。
2. **有 argv 初始 prompt（好消息）**：`plugin.ts:703-710` + 启动器 `bin/dsh-tui.js:263`——`dsh-tui "run the tests"` 会把位置参数经 `ctx.cmdlineArgs` 作为**首条消息自动提交**（issue #53 的 "batched prompt input"）。消息输入侧无需按键注入。
3. **`--resume` 启动器契约**：`bin/dsh-tui.js:257-304`——`--resume <id>` / `-c` / `--continue` 拦截为 `DSH_TUI_RESUME_SESSION` 环境变量（读 `~/.dsh-tui/resume.txt`）；会话 id 由 TUI 写在 resume.txt（`src/sessionHistory.ts`）。
4. **输出侧无解**：渲染走全屏 Ink 画布（ANSI 帧 + alt-screen），无 plain-text / --print 模式；回复提取只能抓屏解析。进程是交互循环（`/exit`、双击 Ctrl+C 才退出），**不会在一轮后自动退出**。
5. **强行接入仍需**：Windows ConPTY（桥内原生依赖）+ 常驻/每消息冷启动 + 完成判定（帧空闲检测）+ 退出注入。比上一轮 TUI 略好（输入走 argv），但输出/完成/退出三座山依旧，可靠性风险不变。
6. **安全边界**：README 明示 Windows 无沙箱后端，profile 组合退化为 `danger-full-access` 且不弹审批——桥接生产 IM 需自行承担权限面（与桥现有 Cursor/Claude 的 `--dangerously-skip-permissions` 类似，可接受但需知悉）。

### 11.4 顺带挖出的正解：官方 JSON-RPC stdio 通道

评估过程中发现官方已有**无头结构化通道**，npm 搜索实证：

| 包 | 版本 | 说明 |
|---|---|---|
| `@deepseek-ai/dsh-sdk-jsonrpc-server` | 0.0.1-rc.5 | **官方 Stdio JSON-RPC server 插件**（out-of-process SDK 客户端用） |
| `@deepseek-ai/dsh-sdk-jsonrpc-demo` | 0.0.1-rc.5 | `dsh-jsonrpc-agent <cordis.yml>` bin：启动外部 cordis.yml，按换行分隔 JSON-RPC 服务 stdio |
| `@deepseek-ai/dsh-sdk-protocol` | 0.0.1-rc.1 | 协议类型（named request/result/notification） |

要点（`dsh-jsonrpc-agent` README.zh + bin.js 实证）：stdout **只承载 JSON-RPC 帧**（诊断走 stderr）、stdin EOF/SIGTERM 有序退出、无内置配置（须自备 cordis.yml 组合 agent 主干 + jsonrpc 入口 + LLM + 工具）。**这是比任何 TUI 都契合 YZJ Bridge 的接入缝**：无 TTY、结构化消息、可 resume（会话身份由 agent 主干决定）、可模型路由——且全部用官方包组合，零自研插件代码。代价是需维护一份 cordis.yml（约一次性的组合工作）。

### 11.5 建议

- **桥接后端**：优先评估官方 `dsh-sdk-jsonrpc-server`（新增 11.4 结论后，自研 headless-resume 插件与 TUI 路线均不再是首选）。
- **dsh-TUI 的定位**：作为**同环境的人工管理终端**（管理员/开发者在终端里进同一 DSH 工作区做交互、/resume、/model），与桥的 bot 后端互补，可随项目分发（plugin 目录放安装脚本即可）。
- 是否采用 PTY+驱动 dsh-TUI 作后端：**不建议**（理由同 10.3，仅输入侧略简化）。

---

## 12. 官方 JSON-RPC 通道实测验证（2026-06，实机跑通）

> 用户指示验证官方 SDK 通道。已在本机（Windows，Node v24.14.0）**端到端实跑**：真实 LLM 回复、多轮记忆、模型路由全部验证。产物：`~/.dsh/profiles/jsonrpc/`（可用 profile）+ `%TEMP%\dsh-jsonrpc-test\client.mjs`（测试客户端）。

### 12.1 组件与版本（全部官方 npm 包，rc.8 线自洽）

| 包 | 版本 | 作用 |
|---|---|---|
| `@deepseek-ai/dsh-sdk-jsonrpc-server` | **0.1.0-rc.8** | Stdio JSON-RPC server 插件（行插件，非 bundle） |
| `@deepseek-ai/dsh-sdk-protocol` | **0.1.0-rc.8** | 线路协议类型（JsonRpcLineTransport） |
| `@deepseek-ai/dsh-base` | 0.1.0-rc.8 | 主干（agent/session/llm/tools/持久化/沙箱全在） |
| `@deepseek-ai/dsh` | 0.1.0-rc.7 launcher | 启动器（closure 内 rc.8 包，混装可用） |

> 注意版本坑：旧版 server `0.0.1-rc.5` 的 peer 是 `^0.0.1-rc.5`，与 rc.8 主干不符；必须用 **0.1.0-rc.8**（peer `^0.1.0-rc.8` 与 closure 完全对齐）。

### 12.2 组合方式（桥接部署形态）

`~/.dsh/profiles/jsonrpc/`（可用 profile，非 bundle 包，行插件走 patch insert）：
- `package.json`：`dsh.profile.bundles: ["@deepseek-ai/dsh-base"]`
- `cordis.patch.yml`：persona（system-prompt）+ `hmr: disabled` + insert `sdk-jsonrpc-server` 行
- 依赖解析：server/protocol 包放入 flat fallback `~/.dsh/profiles/node_modules/@deepseek-ai/`（实测可行；正式部署可用 `dsh plugin --profile jsonrpc add @deepseek-ai/dsh-sdk-jsonrpc-server`，官方路径未实测）
- 启动：**`node <dsh>/lib/bin.js --profile jsonrpc`**（node 直调，绕过 Windows .cmd——桥接的启动方式，实测 OK）

### 12.3 协议（实测 + 源码实证）

三个请求方法（`dsh-sdk-protocol` types.d.ts 确认，无更多）：
- `initialize { cwd, provider, model, maxTokens? }` → `{ serverInfo }`——**进程级** provider/model 路由；`initialize` 是 Loader 就绪边界（Agent Note：非 timing 睡眠）
- `session/prompt { sessionId, contentBlocks }` → `{ messageId }`——sessionId 未知则惰性创建 agent+session；已知则入队（inbox）
- `shutdown {}` → `{}`，应答 flush 后 dispose 根运行时并 exit 0

四个通知：`session.event {sessionId, event}`（完整会话事件流）、`session.status {sessionId, idle|running}`、`subagent.started`、`subagent.finished`。

**stdout 只承载 JSON-RPC 帧**（实测纯净），诊断走 stderr；stdin EOF/SIGTERM → 有序退出 0，SIGINT → 130。

### 12.4 实测结果（真实 LLM 调用，kuaidi100 provider）

| 用例 | 结果 |
|---|---|
| 单轮（fresh session） | initialize→prompt→事件流→`turn/end(completed)`→shutdown，**约 2.5s/轮**（含 1.2s 冷启动），exit 0 |
| 同进程多轮 | 暗号"蓝月亮"跨轮召回——**进程内会话有记忆**，事件流 `turn/start/step/start/user/message/assistant/chunk×N/assistant/message/step/end/turn/end` 成对出现 |
| 模型路由 | `deepseek-v4-flash` 与 `qwen3.5-plus` 各自按 initialize 生效（qwen 自报家门验证） |
| 权限 | `DSH_PERMISSION_MODE=danger-full-access` → sandbox danger + approval never（事件流 permission/preset/sandbox/mode/approval/policy 实证） |
| 持久化 | 会话实时落盘 `~/.dsh/sessions/<项目目录>/<sessionId>/session.jsonl` |
| 跨进程 resume | **❌ 协议不支持**（见 12.5） |

### 12.5 关键限制：跨进程 resume 不存在（官方设计如此）

- 复测：进程 A 建 `session X` 完成一轮并退出；进程 B 同 `session X` 再 prompt → 抛错：
  `session "X" already has a persisted log on disk that does not match this live session (id collision)`
  （守卫在 `dsh-session-persistence/lib/index.js:810`：`load/resume it instead of creating`）
- 根因：server 的 `createSession` 只调 `ctx.agents.create()`；核心层存在 `ctx.agents.resume()`（"Load a persisted session and resume an agent"，`dsh-agent/lib/index.js:556`），**但协议未暴露该方法**。
- 官方设计意图（Agent Note + 协议类型交叉确认）：**进程生命周期 = 会话生命周期**（"retains Bash state across calls" 全在同进程内）。Python SDK 的 `dsh-jsonrpc-agent-pkg` 同理（Windows 非官方目标，但协议在 Windows 用普通 Node 实测可跑）。

### 12.6 对 YZJ Bridge 的落地方案（A+ 路径，替代第 10 节 A/B/C）

**架构：每 bot 一个长驻 jsonrpc 进程**（取代"每次消息冷启动 + 拼 resume"）：

1. **进程模型**：bot 启动/配置变更时 spawn `node <dsh>/lib/bin.js --profile jsonrpc`（cwd = bot 工作目录，`DSH_PERMISSION_MODE=danger-full-access`）；每进程一个 `initialize(cwd, provider, model, maxTokens?)` → 一个模型；bot 换模型 = 重启进程。进程空闲可保留（1.2s 冷启动摊销），崩溃/超时由桥拉起新进程。
2. **消息流**：IM 消息 → `session/prompt{sessionId=bot 稳定 id, contentBlocks:[{type:text}]}` → 桥读取 `session.event` 流，遇 `turn/end`（reason completed/error/max-tokens）即一轮结束 → 提取最后 `assistant/message` 的 text blocks 作为回复 → 处理下一消息。**每个 session 串行**（inbox 会排队，但桥应自行保证同一 bot 消息有序）。
3. **超时/恢复**：桥侧超时 kill（SIGTERM→exit 0 干净）；新进程用新 sessionId（避免 id-collision）；桥自持消息记录，重启后可在首条 prompt 注入浓缩历史（工具状态丢失可接受）。
4. **权限**：danger-full-access（等同现有 cursor/claude 后端的 skip-permissions 信任面）；workspace-write 模式会触发 ask 审批且 jsonrpc 无交互应答者 → 轮次报错，不可用于 bot。
5. **部署**：桥 setup 脚本创建 jsonrpc profile（package.json + cordis.patch.yml + 包落位）；DSH 升级 rc.8 后即可用官方 `dsh plugin` 路径正式化。

### 12.7 风险与开放点

- 无跨进程 resume：bot 重启丢短期上下文（桥侧可缓解）；长期上下文需等官方协议扩展或自研 wrapper 插件（违背零自研，暂缓）。
- 模型池：每进程单模型 → 多模型 bot 需多进程；桥配置层需 bot→(provider,model,进程) 映射。
- 会话无限累积：进程内会话不主动关闭（`/compact` 语义无暴露）——长跑 bot 需定期重启进程防上下文膨胀。
- 官方 rc 线仍快速演进（rc.8 已对齐，留意升级）。
- 未实测：`dsh plugin --profile jsonrpc add` 官方安装路径；Windows 下 MCP/子进程 stderr 干扰帧（server 侧有 childStderr 守卫，风险低）。

---

## 13. 补充解法：resume + 省内存兼得（2026-06 二轮，实测通过）

> 用户反馈：12.6 的"每 bot 长驻进程"不可接受——① 协议无 resume（续会话是刚需）② 常驻 Node 进程违背 Go+Tauri 省内存初衷。本节省内存实测 + 薄包装插件方案，**两难同时解决**。

### 13.1 内存实测（回应"常驻进程违背初衷"）

jsonrpc 服务进程（node 直调 bin.js，boot 完成、一轮完成后 idle）实测：

| 进程 | WorkingSet | Private |
|---|---|---|
| jsonrpc dsh 进程 | **~160MB** | ~189MB |
| （对照）Web GUI dsh 实例 | ~174MB | — |

→ N 个 bot 各长驻 = **N×160MB**（10 个 bot ≈ 1.6GB），确实违背初衷。

### 13.2 薄包装插件：`session/resume`（~100 行，其余全官方）

官方协议只有 `initialize/session/prompt/shutdown`，但核心层存在 `ctx.agents.resume()`（`dsh-agent/lib/index.js:556` → `dsh-agent-loop` 工厂 `resumeWith`：`persistence.prepare(id)` → `setupAndPublish(..., "resume")`，`dsh-agent-loop/lib/index.js:1279-1314`）。官方 server 未暴露它 → **子类化官方 server 补一个方法**：

- 插件 `@bridge/dsh-jsonrpc-resume`（spike 在 `%TEMP%\dsh-jsonrpc-resume-plugin\`，已装入 flat fallback 并实测）：
  - `class ResumableServer extends HarnessSdkJsonRpcServer`，仅 override `handleRequest` 增加 `session/resume { sessionId, contentBlocks? } -> { messageId? }`
  - 实现：`ctx.agents.resume({ resumeSessionId, agentOptions: { provider, model, maxTokens? } })` → 注册进 server 的 sessions Map（后续 `session/prompt` 直接命中同一活 agent）→ 可选入队用户消息
  - `apply()` 忠实复制官方实现（transport/exit/loader await 全同），唯一差异是 server 类
- **官方支持后可一键退役**：profile patch 行名改回 `@deepseek-ai/dsh-sdk-jsonrpc-server` 即可，零其他改动。

### 13.3 跨进程 resume 实测（决定性验证）

| 步骤 | 结果 |
|---|---|
| 进程 A：`session/prompt`（新会话）"记住暗号：红苹果，只回复：收到" | 回复"收到"，进程退出 |
| **进程 B（全新进程）**：`session/resume` 同 sessionId + "暗号是什么？只回复暗号本身" | **回复"红苹果"** —— 跨进程恢复成功 |
| 进程 B 继续：`session/prompt`（同进程）"再确认一次，还是那个暗号吗？只回复：是" | 回复"是" —— resumed 会话在内存中持续可用 |

- 事件流正常（turn/start/end、assistant/message），不回放历史（桥只收新事件）。
- 不存在的 sessionId → JSON-RPC error（`session "X" not found`），行为干净。

### 13.4 推荐架构：每消息一个短命进程 + resume（替代 12.6 的常驻方案）

```
IM 消息 → 桥 spawn `node <dsh>/bin.js --profile jsonrpc`（cwd=bot 工作区）
        → initialize(cwd, provider, model)
        → session/resume(或新会话 prompt){sessionId=bot 稳定 id, contentBlocks:[text]}
        → 读 session.event 流至 turn/end(completed/error) → 提取 assistant text
        → shutdown → 进程退出，内存全部归还
```

- **空闲零内存**（无任何常驻 Node 进程）——符合 Go+Tauri 初衷；内存只在消息处理瞬间出现（峰值 ~160MB/并发消息），结束时归还。
- **真 resume**：同一 bot 的 sessionId 跨进程复用，会话记忆持续（实测"红苹果"跨进程召回）。
- **延迟**：每消息 ~2.5-3.5s（含 ~1.2s boot + resume），IM 场景可接受；bot 间天然隔离，崩溃互不影响。
- **并发**：同一 bot 的消息串行（同进程同 session）；不同 bot 可并行（各自进程）。
- **恢复**：桥或 dsh 崩溃 → 新进程新消息 resume 同一 sessionId 即可继续（持久化在 `~/.dsh/sessions/<项目>/<id>/`）。
- **兜底档（可选，高流量部署）**：全局 1-2 个进程池（按模型分组）常驻，配合 session/resume 按需冷载会话——160MB/池换 0 冷启动；仍是"池"而非"每 bot 一进程"，内存可控。

### 13.5 与既有结论的关系

- 12.6 的"每 bot 长驻"方案**作废**，由 13.4 取代。
- 10 节 A/B/C 路径最终落点：**A+（薄 runner，官方包组合 + 桥自持 ~100 行可卸载插件）**。
- dsh-TUI / 官方 headless 均不承担后端职责（前者交互用，后者无 resume）。
- 待办：桥接实现（Go 侧 spawn/协议客户端）、插件正式落位项目 `/plugin` 下（feature-plan-flow 走完审批后）、`dsh plugin add` 官方安装路径验证。

---

## 14. 优化评估：进程暂活 5 分钟 + 空闲回收（2026-06，热路径实测）

> 用户诉求：每次对话完后进程**暂活 5 分钟**，窗口内新消息直接复用热进程回答（免冷启动），超时再回收——优化连发消息体验。评估结论：**可行、推荐，协议零改动**，且已有实测数据支撑。

### 14.1 热路径实测收益（同一进程连发 3 轮）

| 轮次 | 时刻 | 耗时 | 说明 |
|---|---|---|---|
| 第 1 轮 | 0→2540ms | ~2.5s | 冷：boot 1.5s + LLM 轮 ~1.1s |
| 第 2 轮（热） | 2544→3575ms | **~1.0s** | 纯 LLM 生成 |
| 第 3 轮（热） | 3578→4208ms | **~0.6s** | 纯 LLM 生成 |

3 轮连发总计 **4.3s**；若每轮冷启动 ≈ 8-10s。**热路径省 60-75% 延迟**，连发对话接近无感。

### 14.2 进程生命周期设计（桥侧状态机）

```
        ┌─ 无进程 → spawn + initialize + session/resume(冷, 2.5-3.5s)
消息到来 ─┤
        └─ 有进程(idle) → session/prompt(热, 0.6-1.1s)
              ↓ turn/end
         idle，记 lastActive
              ↓ 5 分钟无新消息（可配置 TTL）
         桥发 shutdown → exit 0（会话已实时落盘，冷恢复零损失）
```

- **idle 保持无需 DSH 侧配合**：进程生命周期只由 stdin/信号决定（12.3 实证），桥握着 stdin 不 EOF 即不退出（hold 45s 实测无自关机制）。
- **优雅回收**：shutdown 幂等、有超时兜底（SIGTERM）；进程退出后下次消息走冷路径 resume，上下文无损。
- **崩溃恢复**：桥监听 exit；处理中 crash → 新进程 resume 同一 sessionId（持久化实时落盘，12.4 实证进程 B 能读到进程 A 全部历史）。

### 14.3 内存画像与上限控制（回应"省内存初衷"）

- 存活进程数 = **5 分钟窗口内活跃过的 bot 数**（非全量 bot）：典型 IM 场景大部分 bot 低频 → 常态 0-2 个进程（~160MB 每个）。
- **可配硬上限**：`maxWarm`（如 3-4）——超出按 LRU 提前回收，内存有天花板；`ttl`（默认 5 分钟）可调。此档位严格优于 12.6"每 bot 常驻"（N×160MB 无上限）。
- 极端画像：全部 bot 每 <5 分钟都活跃 → 逼近常驻（maxWarm 兜底）；桥配置里可按 bot 设 `keepWarm: true/false` 精细控制。

### 14.4 桥侧实现增量（Go）

- `BotRuntime` 表：`map[botID] → {proc, state(idle/running), lastActive}` + 并发串行化（同 bot 消息入队，turn/end 后才发下一条——协议层 inbox 会排队，但桥应自行保证有序）。
- 空闲回收 goroutine：周期扫描 `now-lastActive > ttl` → shutdown（带 2s 超时 kill）。
- 热/冷路径分流 + 崩溃状态位。**协议与 DSH 侧零改动**（session/prompt、shutdown、session/resume 插件均已验证）。

### 14.5 风险与开放点

- **会话上下文无限增长**：高频长跑 bot 同 sessionId 持续累积（无 /compact 暴露）——桥可按消息数/时长强制"轮换会话"（新 sessionId + 首条 prompt 注入浓缩摘要），与 warm 机制正交。
- **内存峰值**：由 `maxWarm` + 消息并发共同界定，需按 bot 规模调参。
- **窗口边界体验**：恰好 >5 分钟再来消息 → 冷路径（2.5-3.5s），可接受；桥可在 shutdown 前记录进程指标，供后续调 ttl。
- 其余同 12.7（rc 演进、Windows 子进程清理、`dsh plugin add` 未实测）。

### 14.6 结论

- **采纳**：13.4 每消息短命进程 + 本节省活回收 = 最终形态（冷热双路径 + 可回收 + 可上限）。
- 体验：热消息 ~1s（接近原生对话），冷消息 ~3s（可接受），连发场景无感。
- 内存：常态接近零，峰值受 maxWarm 硬上限约束。
- 实现工作量：主要在桥 Go 侧（进程表 + 回收器 + 热/冷分流），DSH 侧仅需部署已验证的 resume 插件。

### 14.7 问题澄清①：上下文过长会自动压缩吗？——会，官方自带（实测确认）

- **`compaction-basic` 默认 `auto: true`**（`dsh-compaction-basic/lib/index.js:74,768` `_registerAutomaticCompaction`），在 **step 边界按 token 压力自动触发**（`compactIfNeeded(agent, "pressure")`，L782）+ provider 确认的上下文溢出恢复（"context-overflow"）。
- 它挂在 **dsh-base 行**上（jsonrpc profile 树已有，默认配置），与 Web GUI 是**同一机制**——本会话顶部的"automatically generated checkpoint"文本即出自它（L255 常量），无头 jsonrpc 路径同样生效。
- 触发条件：`token-meter` 按模型 `contextWindow`（settings.yaml 里 deepseek 模型声明 1000000 token）测压，接近阈值时在 turn 内自动摘要压缩并剪枝旧节点（`compaction/start|summary|prune|end` 事件流入 `session.event` 流，桥可感知）。
- **14.5 的"上下文无限增长"开放点撤销**：无需桥侧自造轮换；桥只需监控 `compaction/*` 事件（或忽略——自动执行）。
- 补充：`/compact`（手动）与自动压缩并存；自动压缩事件必须封闭在 turn 内（`compactRegion` 守卫），不影响跨进程 resume（压缩后的 checkpoint 本身就是持久化摘要）。

### 14.8 问题澄清②：多用户并发会同时存在多个 DSH 实例吗？——取决于架构，进程池可做到"一个实例管全部"（实测）

**单进程多会话并发实测**（`dbl.mjs`，一个 jsonrpc 实例，A/B 两个 sessionId 同时入队）：

| 项 | 结果 |
|---|---|
| 双 prompt 同时入队 | 均立即返回 messageId |
| 双 turn 并发完成 | 均 `completed`（~2.2s LLM 生成，交错执行） |
| 事件分流 | `session.event` 携带 sessionId，A/B 事件正确分离（interleaved=true 仍可精确过滤） |
| 回复 | A→"A完成"，B→"B完成"，互不串扰 |
| 第二轮（warm） | A→"A是"，B→"B是"，双会话保持存活 |
| 全流程 | boot 1.5s + 双轮 6s，shutdown 干净 exit 0 |

**实例数对比**：

| 架构 | 用户A通道A + 用户B通道B 并发 | 内存 |
|---|---|---|
| 每 bot 一进程（13.4/14 的中间态） | **2 个实例**（各跑各的会话） | 2×160MB，随并发线性涨 |
| **进程池：每模型一个实例（推荐）** | **1 个实例**（A/B 各占一个 sessionId，并发互不阻塞） | 1×160MB（全桥所有 bot 共享） |
| 池 + warm/TTL 回收 | 同上；空闲回收后常态 0-1 实例 | 常态接近 0 |

- **池规模 = 模型数**（initialize 为进程级单模型）：默认单模型 → 1 个实例覆盖全部 bot；多模型 → 每模型 1 个。
- **冷载入**：进程重启后 bot 会话走 `session/resume` 载回（已验证），无需常驻。
- **权衡**：池模式单点（进程 crash 影响所有 bot）→ 桥检测 exit 后全部会话自动转冷恢复（会话在磁盘，resume 即回），可配双池冗余；每 bot 独立进程则隔离性更好但内存线性涨。**默认推荐池模式（内存优先），每 bot 独立进程作为隔离性要求高的 bot 的可选配置**（`isolation: true` per bot）。

### 14.9 最终形态（综合 13-14 节）

1. **进程池**：按模型分组（通常 1 个），全 bot 会话共享；每会话独立、并发不阻塞（14.8 实测）。
2. **会话寻址**：`sessionId = bot 稳定 id`（或 bot×通道）；首消息创建，进程重启后 `session/resume` 冷载。
3. **冷热双路径**：会话在池中 → `session/prompt`（~1s 热）；不在 → resume 载入（~2.5-3.5s 冷）。
4. **暂活回收**：池进程 idle 超 TTL（默认 5 分钟）→ shutdown 归还内存；`maxWarm` 硬上限（14 节）。
5. **自动压缩**：官方 compaction-basic 默认开启，上下文自动摘要（14.7）。
6. **故障转移**：桥监听池进程 exit → 全部会话下次消息 resume 恢复。
7. **DSH 侧增量**：仅部署 ~100 行 resume 插件（`@bridge/dsh-jsonrpc-resume`），官方支持后删除。
8. **内存总账**：常态 0-1 个实例（~160MB 或 0），峰值 = 并发模型数 × maxWarm 约束，与 bot 总数无关。
