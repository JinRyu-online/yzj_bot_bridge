# 开发计划：bot workspace 沙箱（workspace-write）

> **⚠️ 本草案已被取代**：权威计划为 `docs/plan-sandbox.md`（定版 v2，默认 `workspace-write` + read-only 档 + 全自动）。本文件仅作评审历史保留，内容不再维护；其中与定版冲突的表述以定版为准（尤其：审批 `ask` 在 headless 下**不挂起**，fail-closed 拒绝；`cursor_sandbox` 映射规则见定版 §2.2）。

> 日期：2026-08-24  
> 目标：为每个 bot 在其 `workspace` 内配置沙箱，**禁止修改 workspace 外的文件**  
> 范围：bridge 全部后端（cursor_cli / claude_code / openai / dsh）｜ 平台：Windows 为主（macOS/Linux 顺带）  
> 相关：`docs/sandbox-feasibility.md`（只读 + 自运行详版；本计划默认档改为 `workspace-write`）

---

## 1. 结论

**可行。** 目标语义是「仅 bot 自己的 `workspace` 可写，workspace 外禁止改文件」（`workspace-write`）。

现状：`bots[].workspace` 只是工作目录约定，**不是 OS 强制边界**。各后端差距大：

| 后端 | 现状 | 到 workspace-write 的距离 |
|---|---|---|
| **openai** | `safePath` 已限文件工具；`run_command` 仅 `cwd=workspace`，可 `cd`/`>` 逃出 | 小：再包一层进程沙箱即可 |
| **dsh** | 硬编码 `DSH_PERMISSION_MODE=danger-full-access`（`dshpool.go`） | 小：改配置 + 池键加 Mode + 审批 `never` |
| **cursor_cli** | 已透传 `--sandbox`，默认 `disabled`；Windows 原生沙箱弱/未稳 | 中：Win 需进程包装器 |
| **claude_code** | 仅跳过审批；Windows 无原生沙箱 | 中：同 cursor，靠包装器 |

---

## 2. 目标模型

每 bot 一个统一字段：

```yaml
bots:
  - id: fairy
    workspace: ".../workspace/fairy"
    sandbox: workspace-write   # off | workspace-write | read-only
```

| 值 | 语义 |
|---|---|
| `workspace-write`（本计划主目标） | workspace + 会话私有临时目录可写；其余路径写拒绝 |
| `read-only` | 任意写拒绝（可选档） |
| `off` | 现状全权限 |

兼容：`cursor_sandbox` 映射进同一字段，避免两套配置。

### 默认建议

- 新 bot / 默认值：`sandbox: workspace-write`（对齐目标；落地前需产品确认：是否改为默认 `off`、仅按需开启）。
- 需要全盘改机器/系统配置的 bot：显式 `off`。
- 纯问答、零落盘：可再开 `read-only`。

---

## 3. 实施路径

### P1（约 1 天）— DSH：配置即得

1. 去掉 `DSH_PERMISSION_MODE=danger-full-access` 硬编码，按 bot 注入 `workspace-write`（或 `read-only`）。
2. **审批强制 `never`**：否则 `workspace-write` 默认 `ask`，headless 下会挂起/失败。
3. **池键加 Mode**：同模型不同 sandbox 的 bot 不能共池，否则模式串味。
4. GUI / `config.default.yaml` 暴露 `sandbox`。
5. 测试：写文件落在本 bot workspace；写 workspace 外收到拒绝；无审批挂起。

效果：DSH 在 Windows 上已有受限令牌（`dsh-sandbox-windows-acl`），改环境变量即可强制「只写自己的 workspace」。

### P2（约 0.5 天）— OpenAI：工具层收紧 + 可选包装

- 文件类工具：保持 `safePath`。
- `run_command`：`sandbox != off` 时用同一 Windows ACL runner 包一层；否则仍可逃出 workspace。

### P3（约 3–5 天）— Cursor / Claude：进程级包装器

新增 `internal/sandbox`，Windows 复用 DSH 的 runner：

```text
node .../dsh-sandbox-windows-acl/lib/runner.js \
  --workspace <bot.workspace> --temp <scratch> --mode workspace-write \
  -- <cli> <args...>
```

关键取舍：

- CLI 要写 `~/.cursor` / `~/.claude` → 需把状态目录重定向到桥私有 scratch，或把这些路径加入 grants（仍挡住桌面/其它项目）。
- macOS/Linux：优先原生 `--sandbox` / seatbelt；Windows 以包装器为主。
- `.cmd`/`.ps1` 不能被 `CreateProcessAsUserW` 直接拉起，需解析到真实 `node`/`exe` 入口（桥已有类似逻辑）。

接入点：`cursor.go` / `claude.go` 的 `exec.Command`；`openai.go` 的 `run_command`（与 P2 衔接）。

### P4（可选）— 可观测与纵深

- Job Object 管超时杀树；拒绝写写入 logbuf。
- 整桥降权账户作纵深（代价大，后置）。

---

## 4. 配置与代码落点（预期）

| 位置 | 改动 |
|---|---|
| `bot.Config` / `config/config.go` | 新增统一 `Sandbox`（`off` \| `workspace-write` \| `read-only`）；`cursor_sandbox` 兼容映射 |
| `config.default.yaml` + GUI 机器人表单 | 暴露字段与文案 |
| `backends/dshpool.go` | 注入 `DSH_PERMISSION_MODE`；审批 `never`；池键加 Mode |
| `scripts/setup-dsh-profile.ps1` | profile patch：`approval.policy` 可读 `DSH_APPROVAL_POLICY` |
| `backends/openai.go` | `run_command` 外包沙箱（P2） |
| `backends/cursor.go` / `claude.go` | `sandbox != off` 时 argv 外包（P3） |
| `internal/sandbox`（新） | Windows ACL runner 封装；状态目录 grants/重定向 |

---

## 5. 已知边界（需接受）

- 只限**写**；读、网络、进程可见性默认不挡。
- Everyone 可写目录、硬链接等逃逸面需容器/VM 才根治。
- `read-only` 会影响「落盘再分析」类工作流；`workspace-write` 对查日志/改代码影响小。
- Cursor 在 Windows 上单独开 `--sandbox workspace-write` **不能当唯一防线**，需包装器或等官方成熟。

---

## 6. 待验证项

1. DSH profile patch 覆盖 `approval.policy=never` 是否生效（cordis 合并语义）。
2. Cursor agent 在 Windows 上 `--sandbox` 行为（报错/忽略/生效）；状态目录重定向支持。
3. Claude `CLAUDE_CONFIG_DIR` 重定向后，workspace-write 包装下可正常运行。
4. `windows-acl` runner 与桥现有管道捕获、`exec.CommandContext` 超时取消、HideWindow 的兼容性。
5. 双 bot 不同 workspace：各自可写本目录、互不可写对方目录。
6. CLI 入口解析（`.cmd`/`.ps1` → 真实 node/exe）在包装器下可用。

---

## 7. 待确认（落地前）

1. **默认档**：新 bot 默认 `workspace-write`，还是保持 `off`、仅按需开启？
2. **范围**：先做 **P1（DSH）**，还是 **四后端统一 `sandbox` 字段一起上**（P1→P3）？

---

## 8. 参考

- `docs/sandbox-feasibility.md` — 只读 + 自运行、各 harness 对照、Windows 受限令牌边界
- Codex / Claude Code 沙箱文档与 Windows 支持进度（见可行性文档附录链接）
- DSH 包内：`dsh-sandbox-windows-acl`、`dsh-permission-presets`、`dsh-user-approval`
