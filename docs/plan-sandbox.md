# 计划（定版 v2）：bot 沙箱化——workspace-write 默认 + read-only 档 + 全自动运行

> **版本基线声明**：本文件为唯一权威开发计划，**取代** `docs/plan-sandbox.md` v1（默认 read-only）与 `docs/plan-sandbox-workspace-write.md`（草案）。`docs/sandbox-feasibility.md` 为研究依据（其中"默认档"表述已按本定版同步修正）。评审记录见 git 历史（glm5.2 不可用，以默认模型 subagent 完成）。
>
> **定版决策**（本次拍板，见 §9 决策记录）：
> 1. 默认档 = **`workspace-write`**（存量工作流零破坏、符合 bot 干活定位）；`read-only` 保留为高敏/纯问答 bot 的加强档；
> 2. 统一字段 `sandbox`，`cursor_sandbox` 按 §2.2 规则兼容映射（写死，标记 deprecated）；
> 3. 审批一律 **`never`**（headless 确定性拒绝；`ask` 语义已澄清：无回答者时 fail-closed 拒绝、**不挂起**，但桥内不推荐暴露）；
> 4. DSH 池键加 `Mode`；P4（Job Object + 审计）**必做**（不标可选）。
>
> 实施状态：**未开始**（P1 第一验证点 = §7 待验证项 V1）。

## 1. 目标

每 bot 通过统一配置 `sandbox: workspace-write（默认）| read-only | off` 获得 **OS 级写限制 + 全自动运行**：

1. **写限制**：bot 不能修改 workspace 外的用户文件（Windows=WRITE_RESTRICTED 受限令牌，内核强制、非协作式；Linux=bwrap；macOS=seatbelt）。
2. **全自动**：DSH 审批 `never`（确定性拒绝，无人值守、绝不挂起）；Cursor/Claude 维持 `--force`/`--dangerously-skip-permissions`；OpenAI 无交互。
3. **全后端覆盖**：dsh / cursor_cli / claude_code / openai（opencode 为占位，实现时套同一包装器）。

## 2. 统一配置模型

### 2.1 字段与默认

```yaml
defaults:
  sandbox: workspace-write        # workspace-write（默认）| read-only | off
bots:
  - id: fairy
    sandbox: workspace-write      # 可选覆盖；缺省继承 defaults
```

### 2.2 `cursor_sandbox` 兼容映射（写死，消除多文档分歧）

| 场景 | 结果 |
|---|---|
| 显式配置了 `sandbox` | `sandbox` 生效，`cursor_sandbox` 忽略（deprecated） |
| 未配 `sandbox`，显式配了 `cursor_sandbox`（仅 backend=cursor_cli 时生效） | `disabled`→`off`；`read-only`→`read-only`；`workspace-write`→`workspace-write`；其它值→`off` + warn（unknown sandbox mode） |
| 两者均未配 | 默认 `workspace-write` |

- `cursor_sandbox` 标记 deprecated（v3 移除）；迁移期间 GUI 只展示 `sandbox` 字段。
- 实现点：`config.mapToBotConfig` 内做映射（单一入口，避免各后端各归一次）。

### 2.3 后端粒度语义表（权威，消除"read-only 是否允许写入"的粒度分歧）

| 后端 | 模式 | 写 workspace | 写私有 scratch/临时 | 写用户文件 | 审批 |
|---|---|---|---|---|---|
| dsh | off | ✅ | ✅ | ✅（danger，现状） | never |
| dsh | workspace-write | ✅ | ✅ 会话私有临时（runner 授权） | ✗ | never |
| dsh | read-only | ✗ | ✗ **真零授予**（无 scratch；harness 自身写入在沙箱外，不受影响） | ✗ | never |
| openai | off | ✅（工具层 safePath） | ✅ | ✅（run_command 任意） | — |
| openai | workspace-write | ✅ | ✅ | ✗（run_command 外包沙箱） | — |
| openai | read-only | ✗（工具集只有 list_dir/read_file/grep） | ✗ | ✗ | — |
| cursor/claude | off | ✅ | ✅ | ✅（现状） | 已 --force / skip-permissions |
| cursor/claude | workspace-write | ✅ | ✅ runner 私有临时（TMP/TEMP 指向，已授权） | ✗ | 同上 |
| cursor/claude | read-only | ✗（模型工作目录=真实 workspace，只读视角） | ✅ **仅私有 scratch**（`<userData>/sandbox/<bot-id>/`，CLI 状态必需） | ✗ | 同上 |

要点：
- **read-only 的粒度差异是刻意的**：DSH/OpenAI 是桥自控的工具链，可做到真零授予；外部 CLI（cursor/claude）必须写自身状态才能运行，故 read-only = "私有 scratch 可写 + 其余全只读"（scratch 对模型不可见）。
- 桥自身写入（sessions.json、运行日志、记忆、conversations、skills 落盘）全部由 **bridge 进程直接执行，不在沙箱内**，任何模式下不受影响。

## 3. 迁移策略（补齐两方案共同缺口）

1. **默认破坏面评估**：默认 `workspace-write` 对存量 bot 影响最小——写 workspace 的既有工作流（查日志暂存、写脚本、改代码）全部保留；受影响的只有"写 workspace 外"的存量行为（本就是要限制的）。需要全权限的 bot 显式 `sandbox: off` 恢复现状。
2. **启动告警**：`runtime.Load` 遍历 bots，对未显式配置 `sandbox` 的 bot 打一条 warn 日志（bot id + 生效值），帮助存量排查。
3. **发布说明 + README**：明示默认值、`cursor_sandbox` 映射规则、恢复路径（`off`）、选型指南（纯问答→read-only；分析/开发→workspace-write；全权限→off）。
4. **GUI**：机器人列表显示生效档徽标（含"默认"来源标注）；编辑弹窗展示当前生效值。

## 4. 关键文件与改动

### P1：DSH「workspace-write 默认 + read-only 档 + 审批 never」（约 1 天）

**`bridge/internal/bot/bot.go`**
- `Config` 加：
  - `Sandbox string`（`json:"sandbox"`，`off`|`workspace-write`|`read-only`）
  - `DSHApprovalPolicy string`（`json:"dsh_approval_policy"`，默认 `never`；高级字段，GUI 不暴露）

**`bridge/internal/config/config.go`**
- `defaultMap()`：`"sandbox": "workspace-write"`、`"dsh_approval_policy": "never"`
- `mapToBotConfig`：`sandbox` 读取 + **§2.2 cursor_sandbox 映射**（backend=cursor_cli 时）+ 归一 `effectiveSandbox(cfg)`（未知值→`off`+warn）
- 注意：`CursorSandbox` 旧字段保留读取（映射用），标记 deprecated

**`bridge/internal/backends/dshpool.go`**
- `dshPoolKey` 加 `Mode string`，`String()` 拼入（**防同模型不同模式共池串味**；已核实当前键=`Provider|Model|Profile|NodeBin|DSHEntry`）
- `dshPoolGetOrCreate`/`dshPoolGet` 透传 Mode
- `spawnDshProc`（L157-158）：删除硬编码 `DSH_PERMISSION_MODE=danger-full-access`；注入：
  - `DSH_PERMISSION_MODE=<sandbox→映射>`（read-only→read-only；workspace-write→workspace-write；off→danger-full-access）
  - `DSH_APPROVAL_POLICY=<policy>`（默认 never）

**`bridge/internal/backends/dsh.go`**
- `poolKey(provider, model)` 带 Mode；`effectiveSandbox(cfg)` 单一归一入口（空→`workspace-write`，消除 v1 的"空值/默认值/off 三路归一未收敛"疑问）

**`scripts/setup-dsh-profile.ps1`**
- `cordis.patch.yml` 追加：
  ```yaml
  - id: approval
    config:
      policy: !!js process.env.DSH_APPROVAL_POLICY ?? 'never'
  ```

**`config.default.yaml`**
- `defaults` 加 `sandbox: workspace-write`、`dsh_approval_policy: never` + 注释（选型指南：纯问答→read-only）

**GUI `gui/src/App.tsx`**
- AI 设置 defaults 加 `sandbox` 下拉；`saveAll` 写入（L2392 附近）；回读（L1637 附近）
- 机器人表单（BotForm L58-74、emptyBotForm L543、submitBot payload L2160、openEditBot 回读）加 `sandbox` 下拉；列表行加生效档徽标

**测试**
- `dsh_live_test.go`：`liveDSHBase` 显式 `Sandbox: "workspace-write"`（现有用例依赖写文件验证落点）；新增 `liveSandboxModes` 子用例（read-only 写 workspace 拒 + 读外成功 + 无审批挂起；workspace-write 写内成功写外拒）
- `dsh_test.go`：poolKey-Mode 构成单测；`config_test.go`：cursor_sandbox 映射规则表驱动单测

### P2：OpenAI 工具层 + run_command 包装（约 0.5-1 天）

**`bridge/internal/backends/openai.go`**
- 工具集开关：`full = (mode == "agent") && effectiveSandbox(cfg) != "read-only"`（read-only 只暴露 list_dir/read_file/grep；已核实 `buildOATools` 在 L459-494、ask/plan 拒绝在 L563/L610）
- `Run` 内：`sandbox == "read-only"` 时忽略 opts.Mode 的 agent 覆盖（防显式传 agent 绕过）
- `run_command`（L608-629）：`sandbox != "off"` 时外包 `sandbox.Confine`（P3 依赖，先留接口）

### P3：`internal/sandbox` 通用包装器 + CLI 接入（3-5 天）

**新包 `bridge/internal/sandbox/`**

| 文件 | 内容 |
|---|---|
| `sandbox.go` | `type Mode string`；`effectiveMode()` 归一（复用 config 语义）；`Confine(argv, mode, workspace, scratch) ([]string, error)` 平台分发；未知值→off+warn |
| `windows.go` | 定位 `dsh-sandbox-windows-acl/lib/runner.js`（DSHHome→`~/.dsh/profiles/node_modules/...`→全局 npm 包）；校验 workspace/scratch 存在且属主；argv：`[node, runner.js, --workspace <ws>, --temp <tempRoot>, --mode <mode>, --, ...argv]`；tempRoot=`<userData>/sandbox-tmp` |
| `unix.go` | linux：bwrap（只读挂载系统目录 + `--bind workspace`）；darwin：seatbelt profile（临时文件写 tempRoot） |
| `resolve.go` | CLI shim→真实入口（`.cmd/.ps1` 不能被 `CreateProcessAsUserW` 直跑；本机实测：cursor=agent.cmd→cursor-agent-svc.js、claude=claude.ps1→node cli.js；复用 dsh 入口解析模式） |

**CLI 包装语义（定版）**
- `workspace-write`：`--workspace <真实 workspace>` + `--temp <tempRoot>`；runner 自建私有临时并设 TMP/TEMP（已授权）；claude 注入 `CLAUDE_CONFIG_DIR=<workspace>/.claude-state`；cursor 状态重定向待验证（V2）
- `read-only`：`--workspace <私有 scratch>` + `--mode workspace-write`（对 scratch 授权，模型工作目录=真实 workspace 只读视角）；claude 注入 `CLAUDE_CONFIG_DIR=<scratch>/.claude-state`；cursor 若状态无法重定向 → 降级 `workspace-write`（文档标注，V2 决定）
- 包装后 `cmd.Dir`=真实 workspace；`StdoutPipe` 抓 runner 输出（stdio inherit 直通）；`HideWindow` 只作用于 runner

**接入点**
- `cursor.go` Run（argv 构建处，L137 附近）：非 Windows 走 `--sandbox <mode>`（原生，macOS/Linux）；Windows 走 `sandbox.Confine`；`--force` 保留
- `claude.go` Run：同上；`--dangerously-skip-permissions` 保留；env 注入 `CLAUDE_CONFIG_DIR`
- `openai.go` `run_command`：`sandbox.Confine` 外包

**技能/记忆影响（评审缺口，已消解）**：skills 由 **bridge 进程** materialize 进 workspace（非沙箱内，任何模式可写）；agent 侧只读使用，无影响。记忆同理（bridge 进程写，注入 prompt）。

**共享 workspace 场景（评审缺口）**：多 bot 共享同一 workspace → 各自获得同目录写授权；同属主可接受，文档注明；不阻塞。

### P4：Job Object 加固 + 审计（必做，约 1 天）

- `sandbox/windows.go`：包装命令设 `SysProcAttr.JobObject`（Go ≥1.20 windows），`KILL_ON_JOB_CLOSE` + CPU/内存上限（可选配置 `sandbox_cpu_limit`/`sandbox_mem_limit`）；替代/补充现有 taskkill 树杀
- 审计：沙箱拒绝（runner stderr `Access is denied.` / DSH `[sandbox: …]`）记 logbuf（bot 维度），GUI 日志页可见

### P5（可选，暂不做）

网络白名单（WFP）/ Docker / 专用受限账户；记录在案，不排期。

## 5. 实现顺序

1. P1：bot.go/config（含 cursor_sandbox 映射）→ dshpool（Mode+env）→ dsh.go → **先验证 V1（patch 覆盖生效）** → config.default.yaml → GUI → live 沙箱用例
2. P2：openai.go 工具集 + run_command 接口（可与 P1 并行）
3. P3：sandbox 包（resolve→windows→unix）→ cursor/claude/openai 接入 → GUI 徽标 → 冒烟
4. P4：Job Object + 审计
5. 全量回归 + README/发布说明 + 可行性文档实施状态回填

## 6. 验收标准

1. `go build/vet` 全绿；新增单测全绿：sandbox argv/模式、poolKey-Mode、cursor_sandbox 映射表、openai read-only 工具集。
2. 真实环境（`YZJ_SMOKE=1`）：
   - DSH read-only：写 workspace 拒（`[sandbox: …]`）、读外成功、**无审批挂起**；
   - DSH workspace-write：写内成功写外拒；现有 live 用例（首条 cwd/热路径/resume/双 bot 并发/TTL）全绿；
   - 同模型不同模式两 bot 各跑一轮 → 互不串味（poolKey-Mode 生效）。
3. cursor/claude 经包装器正常出结果；尝试写 `%USERPROFILE%\Documents` → 拒绝且进程不崩；workspace-write 下写 workspace 成功。
4. GUI：bot 级 `sandbox` 与 defaults 默认值可配置，保存/重启回读正确；列表徽标含默认来源标注。
5. 迁移：存量未显式配置 sandbox 的 bot 启动 warn；显式 `sandbox: off` 完全恢复现状；`cursor_sandbox` 存量配置按 §2.2 映射生效。

## 7. 待验证项（优先级排序）

| # | 项 | 依赖阶段 | 失败后果与对策 |
|---|---|---|---|
| V1 | DSH profile patch 覆盖 `approval.policy=never` 是否生效（cordis 合并语义） | P1 第一天 | 退化 `ask`（headless fail-closed 拒绝、不挂起，功能可接受但非显式 never）；备选：改 dsh-base patch 环境变量 |
| V2 | cursor 状态目录重定向（CODEX_HOME 类 env）+ Windows `--sandbox` 实际行为 | P3 排期前 | cursor read-only 降级 workspace-write（文档标注） |
| V3 | `windows-acl` runner 与桥 StdoutPipe / `exec.CommandContext` 取消 / HideWindow 兼容 + shim 解析可用 | P3 地基 | 包装器整体换形（native Go 受限令牌实现，~300 行） |
| V4 | claude `CLAUDE_CONFIG_DIR` 重定向后认证读取/会话恢复 | P3 | 回退白名单方案（workspace+状态目录） |
| V5 | read-only 下 CLI 写 cwd（模型工作目录）失败中间态（cursor node 服务索引/缓存） | P3 | 若不可容忍：cursor read-only 视为不支持 → 映射 workspace-write |
| V6 | 非管理员 DACL 变更（workspace 属主） | P1/P3 | 授权失败 fail-closed；workspace 桥已自建（属主=当前用户） |

## 8. 风险表（修订版 R1-R8）

| # | 风险 | 影响 | 对策 |
|---|---|---|---|
| R1 | profile patch 覆盖 approval 不生效 | 退化为 ask（fail-closed 拒绝，不挂起） | V1；备选 dsh-base patch env |
| R2 | cursor 状态目录无法重定向 | read-only 档对 cursor 不可用 | 降级 workspace-write；文档标注 |
| R3 | claude `CLAUDE_CONFIG_DIR` 重定向后认证/会话异常 | 登录态丢失 | V4；回退白名单 |
| R4 | runner 与管道/超时/隐藏窗口兼容 | 输出捕获/取消失效 | V3；Job Object 兜底；native Go 备选 |
| R5 | 默认 workspace-write 对"写外"存量 bot 是变更 | 少数 bot 行为变化 | 迁移策略 §3（warn+README+off 恢复） |
| R6 | 受限令牌边界：读/网络/进程可见性不受限；Everyone 可写目录/硬链接逃逸面 | 非绝对隔离 | 文档明示；绝对免疫=P5 容器/VM |
| R7 | 非管理员 DACL 变更 | 授权失败 | V6；workspace 桥自建 |
| R8 | `.cmd/.ps1` shim 不能直跑 | 包装失败 | resolve.go 解析真实入口（同 dsh 入口模式） |

## 9. 决策记录（定版）

| # | 决策 | 理由 |
|---|---|---|
| D1 | 默认档 `workspace-write` | 存量工作流零破坏、符合 bot 干活定位；read-only 保留为高敏/纯问答加强档（评审结论） |
| D2 | 单一 `sandbox` 字段；`cursor_sandbox` 按 §2.2 映射 | 消除多文档分歧；避免两套配置 |
| D3 | 审批一律 `never`；`ask` 语义澄清（headless fail-closed 拒绝不挂起）但桥内不推荐暴露 | headless 确定性、无人值守 |
| D4 | DSH 池键加 Mode | 防同模型不同模式共池串味 |
| D5 | CLI read-only = 私有 scratch 授权（模型工作目录只读视角）；DSH/OpenAI read-only = 真零授予 | CLI 必须写状态；粒度差异刻意为后端能力边界 |
| D6 | P4 Job Object + 审计必做 | 无人值守的运维底线（超时树杀、拒绝可观测） |
| D7 | 迁移：启动 warn + README + 发布说明（默认破坏面小） | 存量平滑 |
| D8 | 未知 `sandbox` 值 → `off` + warn（fail-safe 到现状，不静默收窄） | 配置容错 |

## 10. 实施状态（追加记录；上方正文为权威设计）

（待实施后回填各阶段完成情况与验证结果；V1-V6 逐项勾销）
