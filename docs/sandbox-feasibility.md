# 沙箱化可行性评估（修订版）：bot 只读 + 全自动运行

> 日期：2026-02（定版 2026-02）｜ 目标：**① bot 无修改权限（定版：默认 workspace-write，read-only 为加强档）② bot 全自动运行、无需人工审批命令**
> 范围：bridge 全部后端（cursor_cli / claude_code / openai / dsh）｜ 平台：Windows 为主（macOS/Linux 顺带）
> 参考：Pi、OpenAI Codex CLI、Claude Code、OpenHands/SWE-agent、DSH（DeepSeek Harness）自带沙箱子系统。
> 结论：**可行，且大部分能力是现成的**——DSH 后端改配置即得"写限制 + 确定性拒绝审批"；cursor/claude 本就 `--force / --dangerously-skip-permissions` 自运行，补一个进程级包装即可；openai 后端已有只读工具集。
> **定版决策**：默认档 = `workspace-write`（权威计划 `docs/plan-sandbox.md` v2，D1）；`read-only` 为可选加强档；审批一律 `never`；`cursor_sandbox` 映射规则见定版 §2.2。本文件为研究依据，默认档表述以定版为准。

---

## 1. 需求澄清

| 需求 | 语义 | 落地层级 |
|---|---|---|
| bot 没有修改权限 | 不能修改 workspace 外的用户文件；默认 **workspace-write**（定版 D1），`read-only` 为可选加强档（真零授予） | 工具层策略 + 进程层 OS 强制 |
| bot 自运行不用审批 | 无人值守：命令直接执行或被确定性拒绝，**绝不弹窗/挂起等人工** | DSH: approval `never`；Cursor/Claude: 保持 `--force`/`--dangerously-skip-permissions`；OpenAI: 本就无交互 |

两者不矛盾：`workspace-write + never` = "只写自己的 workspace，workspace 外写操作被 OS 拒绝并回 `[sandbox: …]` 标记，模型自行调整"；`read-only + never` = 任意写被拒（更严格档）。

---

## 2. 现状：各后端当前权限与自运行状态

| 后端 | 写权限现状 | 自运行现状 |
|---|---|---|
| cursor_cli | 无限制（默认 `cursor_sandbox: disabled` + `--force`） | ✅ 已 `--force` 免确认 |
| claude_code | 无限制（默认 `bypassPermissions` + `--dangerously-skip-permissions`） | ✅ 已跳过权限确认 |
| openai | 工具层 safePath 限 workspace；`run_command` 任意 | ✅ 无交互 |
| dsh | 无限制 —— 桥硬编码 `DSH_PERMISSION_MODE=danger-full-access`（`dshpool.go` L158） | ⚠️ 审批策略受同一环境变量影响（danger→`never`，其它→`ask`） |

`workspace` 只是工作目录约定，不是强制边界。

---

## 3. 参考：成熟 Harness 的只读/自运行做法

### 3.1 DSH（本机已装，直接复用）

沙箱子系统（`dsh-sandbox` / `dsh-sandbox-policy` / `dsh-sandbox-local` / `dsh-bash-sandbox` / `dsh-pwsh-sandbox`）：

- 模式词汇：`read-only` | `workspace-write` | `danger-full-access`。**`read-only` = 不允许任何写**（fs write/edit 拒绝；shell 写入被 OS 拒绝）；`workspace-write` = 仅 workspace + 会话私有临时目录可写。
- Windows 执行器（`dsh-sandbox-windows-acl`）：**WRITE_RESTRICTED 受限令牌** + workspace/临时目录写 SID + DACL 授予；子进程绝不无限制拉起（fail-closed）；同一机制即 Chrome AppContainer 的"受限令牌"族。
- **审批（`dsh-user-approval`）**：策略仅 `ask | never`。关键事实：
  - `never` = "不提示任何人，每次请求确定性返回 `rejected`"——官方定位即 **CI/无人值守**场景；
  - `ask` 在**无回答者**时 fail-closed 落到 `unavailable`（=拒绝，不挂起）；headless JSON-RPC profile 无 UI 回答者。
- 部署钩子（`dsh-base/cordis.patch.yml` L175/L191）：
  ```yaml
  mode:   !!js process.env.DSH_PERMISSION_MODE ?? 'workspace-write'
  policy: !!js (process.env.DSH_PERMISSION_MODE ?? 'workspace-write') === 'danger-full-access' ? 'never' : 'ask'
  ```
  → **桥通过环境变量即可控制模式**；审批策略默认 `ask`（headless 下自动拒绝），可再通过 profile patch 覆盖为 `never`（见 §5.1）。

### 3.2 OpenAI Codex CLI / Cursor（同源）

- `--sandbox read-only|workspace-write|danger-full-access`：Linux bubblewrap / macOS seatbelt 包装 shell；默认 deny 网络。
- **Windows：官方尚未支持**（在途 PR [#15578](https://github.com/openai/codex/pull/15578)、[#34423](https://github.com/openai/codex/pull/34423)）；macOS/Linux 可用。
- Cursor 已 `--force` 自运行；桥已有 `cursor_sandbox` 透传（`cursor.go` L106-108）。

### 3.3 Claude Code

- macOS：seatbelt 沙箱化 Bash 工具（[官方文档](https://code.claude.com/docs/en/sandboxing)）；**Windows 无原生沙箱**（[#46740](https://github.com/anthropics/claude-code/issues/46740)）。
- 自运行靠 `--dangerously-skip-permissions`（桥已传）；状态目录可经 `CLAUDE_CONFIG_DIR` 重定向（供只读包装用，见 §5.3）。

### 3.4 共性结论

"只读 + 自运行" = **工具层策略（读多写少/拒绝写）+ 进程层 OS 强制（Windows=受限令牌+ACL）+ 审批确定性拒绝（never）**。三件套在 Windows 上 DSH 已全部实现并随包发布。

---

## 4. 方案设计（修订版）

统一建模为每 bot 一个字段：

```yaml
sandbox: workspace-write  # workspace-write（默认，定版）| read-only | off（=当前全权限）
# DSH 专属（内部映射）：dsh_permission_mode + 审批 never
```

| 后端 | read-only 语义 | workspace-write 语义 | 自运行保障 |
|---|---|---|---|
| dsh | `DSH_PERMISSION_MODE=read-only`：写工具/写命令全部 OS 拒绝 | `=workspace-write`：仅 workspace+私有临时可写 | 审批 `never`（确定性拒绝，不弹窗不挂起） |
| cursor_cli | Windows：进程级只读包装器（§5.3）；macOS/Linux：`--sandbox read-only` | 包装器/`--sandbox workspace-write` | 已有 `--force` |
| claude_code | Windows：进程级只读包装器 + `CLAUDE_CONFIG_DIR` 重定向；macOS：sandbox 化 Bash | 包装器 grants workspace+状态目录 | 已有 `--dangerously-skip-permissions` |
| openai | 工具集=只读三件套（list_dir/read_file/grep），无 write/run 工具 | 现有 agent 工具集（safePath 限 workspace） | 无交互 |

### 5.1 方案 A（P1，先行）：DSH 后端「workspace-write 默认 + read-only 档 + 审批 never」

改动点（约 1 天）：

1. `bridge/internal/backends/dshpool.go`：
   - 删除 L158 硬编码 `DSH_PERMISSION_MODE=danger-full-access`；
   - 按 bot 配置注入 `DSH_PERMISSION_MODE=<mode>`（`read-only` | `workspace-write` | `off`→`danger-full-access`）；
   - 注入 `DSH_APPROVAL_POLICY=never`（配合 profile patch）；
   - **池键加 Mode 维度**：`dshPoolKey` 增加 `Mode` 字段（当前键=provider|model|profile|node|entry，同模型不同模式的 bot 会共用进程 → 模式串味；加 Mode 后按模式分池）。
2. `config/config.go` + `bot.Config`：新增统一 `Sandbox` 字段（默认 `workspace-write`，定版 D1）+ `cursor_sandbox` 兼容映射（定版 §2.2）。
3. `scripts/setup-dsh-profile.ps1`：profile 的 `cordis.patch.yml` 追加覆盖审批策略：
   ```yaml
   - id: approval
     config:
       policy: !!js process.env.DSH_APPROVAL_POLICY ?? 'never'
   ```
   （沿用现有 `!!js process.env` 语法；需验证 cordis patch 合并覆盖语义——见 §7 验证点 1。）
4. `config.default.yaml` + GUI「机器人表单 / AI 设置」加字段。
5. 测试：`dsh_live_test.go` 增加只读用例（读文件 ok、写文件收到 `[sandbox: …]` 拒绝、无审批挂起）。

**效果**：DSH bot 可读一切、可跑命令，任何写被 Windows 受限令牌拒绝；升级尝试被 `never` 确定性拒绝；模型从 denial 自纠。

### 5.2 方案 B（P2）：OpenAI 后端只读映射

- `sandbox: read-only` → 后端默认 mode 置 `ask`（`buildOATools(full=false)`，仅 list_dir/read_file/grep；write_file/run_command 已按 mode 拒绝，openai.go L562-564/L609-610）。
- 可选收紧：`run_command` 在 read-only 下整体禁用（不进工具集）。
- 改动极小（约 0.5 天 + GUI 文案）。

### 5.3 方案 C（P3）：CLI 后端进程级只读包装器

`internal/sandbox` 包 + 包装器：

- **Windows**：复用 DSH `dsh-sandbox-windows-acl/lib/runner.js`（通用 argv 包装器）：
  ```
  node <dsh>/node_modules/@deepseek-ai/dsh-sandbox-windows-acl/lib/runner.js \
    --workspace <dir> --temp <temp-root> --mode read-only -- <cli> <args...>
  ```
  `read-only` 零授予（无写 SID、无临时授予）→ 包装进程写任何用户文件都被拒（EPERM 级）。
  入口定位复用桥已有 `resolveDSHEntry`/DSHHome 逻辑；stdout 直通（现有 StdoutPipe 捕获不受影响）。
- **CLI 状态目录问题（关键）**：cursor/claude 自己也要写状态（`~/.cursor`、`~/.claude`）。零授予下会失败。对策：
  1. 状态目录重定向到桥的私有 scratch（`<user-data>/sandbox/<bot-id>/`）：
     - Claude：`CLAUDE_CONFIG_DIR=<scratch>/claude`（官方支持）；
     - Cursor：验证是否支持 `--user-data-dir` / 环境变量（见 §7 验证点 2）；
  2. 不支持重定向时，退化为 grants 白名单 = workspace + CLI 状态目录（仍挡住 Documents/桌面/其它项目/系统目录，但不再是严格只读——文档标注取舍）。
- **Linux / macOS**：`bwrap` / `sandbox-exec` 包装（语义同 DSH），或直接走 CLI 原生 `--sandbox`。
- 接入点：`cursor.go` Run、`claude.go` Run 的 `exec.Command` argv 外包一层（`sandbox != off` 时）；`openai.go` run_command 同样外包（可选）。

**工作量：核心包装器 2-3 天；接入 + 配置/UI 1-2 天。**

### 5.4 方案 D（可选，部署级）：整桥降权运行

- 专用受限 Windows 用户（如 `yzj-bot`）运行整个 bridge：数据目录 ACL 收敛、桌面/文档对其只读。对所有后端一次性生效，作为纵深防御。
- 代价：凭据迁移（`~/.cursor`、`~/.claude`、`settings.yaml`）、跨用户控制 API 打通、Tauri GUI 与受限用户的交互。
- 建议：先做 A/C，D 按需。

---

## 6. Windows 受限令牌已知边界（照抄 DSH 文档）

- 只限制**写**；**读、网络、进程可见性不限制**。`read-only` 同样不挡网络。
- 无控制台隔离（桥只对 runner 用 CREATE_NO_WINDOW，不影响被包装子进程）。
- workspace/可写目录须属主为调用者；workspace 须已存在（桥已自建）。
- 固有逃逸面：Everyone 可写目录（Public 部分）、硬链接 —— 需容器/VM 才根治。
- 建议叠加 Job Object（KILL_ON_JOB_CLOSE + 资源上限），把取消/超时树杀升级为进程组托管。

---

## 7. 实施计划

| 阶段 | 内容 | 工作量 | 收益 |
|---|---|---|---|
| P1 | DSH 沙箱（workspace-write 默认 + read-only 档）+ 审批 never：配置化 + 池键加 Mode + profile patch + GUI/文档 + 测试 | ~1 天 | DSH bot 即刻"写限制+自运行" |
| P2 | OpenAI 只读映射（默认 ask 工具集） | ~0.5 天 | openai bot 只读 |
| P3 | `internal/sandbox` 包装器（win=windows-acl runner，linux=bwrap，mac=seatbelt）+ CLI 状态重定向 + 接入 cursor/claude | 3-5 天 | 全部后端统一只读 |
| P4 | Job Object 资源上限 + `[sandbox: …]` 拒绝审计进 logbuf | 1 天 | 生命周期与可观测性 |
| P5（可选） | 网络白名单（WFP）/ Docker / 专用受限账户 | 1-2 周 | 读/网络/绝对逃逸免疫 |

## 8. 待验证项

1. profile patch 覆盖 `approval.policy` 是否生效（cordis patch 合并语义；`!!js` 在 profile patch 中的支持）。
2. Cursor agent 在 Windows 上 `--sandbox` 行为（报错/忽略/生效）；状态目录重定向支持（`--user-data-dir`?）。
3. Claude `CLAUDE_CONFIG_DIR` 重定向后，read-only 包装下可正常运行（会话恢复、认证读取）。
4. `windows-acl` runner 与桥现有管道捕获、`exec.CommandContext` 超时取消、HideWindow 的兼容性。
5. read-only 下 DSH pwsh（Windows ConstrainedLanguage）对 bot 高频操作（git status/log、dir、读文件）无碍。
6. `dsh_live_test.go` 在 read-only 下的回归（现有用例可能假设可写，需分级：读用例=read-only，写用例=workspace-write/danger）。
7. **本机实测**：cursor agent 是 Node 服务（`%LOCALAPPDATA%\cursor-agent\agent.cmd` → `cursor-agent-svc.js`），claude 是 npm `claude.ps1` shim——包装器需解析到真实入口（node cli.js / exe），`.cmd/.ps1` 无法被 `CreateProcessAsUserW` 直接拉起（P3 已按 dsh 入口解析模式处理）。

---

## 附录：常见工作流影响（FAQ）

### 大日志分析（先暂存本地 → 写脚本分析）会不会被沙箱影响？

| sandbox 模式 | 暂存日志到本地 | 写分析脚本 | 替代姿势 |
|---|---|---|---|
| `read-only`（加强档）| ✗ 写入被 OS 拒绝（含 TMP/TEMP） | ✗ 无法落盘 | 管道流式分析：`Get-Content log \| Select-String ...`；Windows 下 pwsh 为 ConstrainedLanguage（核心 cmdlet 可用，`.NET` 静态调用/Add-Type/COM/反射不可用）；`-Command` 内联执行复杂逻辑不落盘 |
| `workspace-write` | ✅ 可写入 workspace（持久）与会话私有临时目录（会话级） | ✅ 可写可跑，输出落 workspace | 完整流程不受影响；Windows 下为 FullLanguage |
| `off` | ✅ 全权限 | ✅ | 现状 |

要点：
- **读永远不受限**（受限令牌只限制写），受影响的只有"落盘"这一步；
- `read_file` 工具本身有 ~256KB 截断，大日志正确姿势就是 shell 流式（`Get-Content`/`grep`/`Select-String`）而非整读；
- 桥自身写入（sessions.json / 运行日志 / 记忆 / conversations）是 harness 进程操作，不在沙箱内，不受影响；
- 按 bot 用途分档：纯问答→`read-only`；查日志/写脚本分析→`workspace-write`。

## 9. 参考链接

- [Codex CLI sandbox（镜像文档）](https://mintlify.wiki/openai/codex/cli/sandbox) ｜ [Sandboxing 概念](https://mintlify.wiki/openai/codex/concepts/sandboxing)
- [openai/codex#15578 Windows sandbox unified exec runtime](https://github.com/openai/codex/pull/15578) ｜ [#34423](https://github.com/openai/codex/pull/34423) ｜ [#38660](https://github.com/openai/codex/pull/38660)
- [Claude Code 沙箱化 Bash 工具（官方）](https://code.claude.com/docs/en/sandboxing) ｜ [Windows 原生沙箱请求 #46740](https://github.com/anthropics/claude-code/issues/46740)
- [Pi / DSH / Codex Harness 对比](https://grapecity.csdn.net/6a8bb722662f9a54cb9fd46b.html) ｜ [Coding Agents: Harness 架构对比](https://www.cs.ubc.ca/~reacher/blog/harness/)
- [pi-mono（Pi agent toolkit）](https://github.com/CloudEngineHub/pi-mono)
- DSH 本机包内实现：`dsh-sandbox`、`dsh-sandbox-policy`、`dsh-sandbox-local`、`dsh-sandbox-windows-acl`、`dsh-user-approval`、`dsh-permission-presets`、`dsh-base/cordis.patch.yml`
