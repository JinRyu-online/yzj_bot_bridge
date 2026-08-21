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
