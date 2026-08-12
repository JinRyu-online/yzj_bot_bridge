# 统一 Skills 运行时 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立跨后端（OpenAI / Cursor CLI / Claude Code / OpenCode）统一的 Skill 包格式与运行时，支持按机器人勾选可用 Skill，并支持外部包导入与插件式一键安装。

**Architecture:** 以 `~/.yzj-bridge/skills/` 为权威 Skill 仓库（manifest + 脚本/提示词/可选 MCP 描述）。桥在调度任务前按 `bots[].skills` 解析启用集，经 **Backend Adapter** 注入：OpenAI 编成 function tools 并由桥执行；Cursor/Claude 将 Skill 物化到其客户端可发现的目录并做 prompt 引导。导入层支持 zip/目录/URL/内置 catalog 一键安装。

**Tech Stack:** Go（`bridge/internal/skills`）、现有 backends / controlapi / orchestrator、Tauri GUI（Skills 管理页 + 机器人勾选）、YAML manifest、可选 HTTP 下载。

---

## 背景与约束（实现前必读）

### 现状问题
- `bots[].skills` 仅是字符串列表，各后端只拼一句「优先使用这些 skills: …」，**不加载真实 Skill**。
- OpenAI 只有桥内置的 `list_dir/read_file/grep/write_file/run_command`。
- Cursor / Claude 的 Skill/MCP 来自各自客户端目录，与桥配置无联动。

### 设计原则
1. **一种包格式，多后端适配**——不要为每个引擎各维护一套 Skill 定义。
2. **权威源在桥侧**——`~/.yzj-bridge/skills/<id>/`；Cursor/Claude 目录是同步产物，可重建。
3. **按机器人隔离启用**——全局已安装 ≠ 某 bot 可用；`bots[].skills: [id…]` 为白名单。
4. **先 Skill 执行，后 MCP**——MVP 先做本地 script/prompt Skill；MCP 作为 manifest 扩展字段，二期接线。
5. **安全默认**——外部导入要校验 manifest、路径沙箱；`run` 类工具默认受限工作区。

### 非目标（本计划不做）
- 不重写 Cursor/Claude 客户端内部 MCP 协议栈。
- 不在首期做 Skill 在线商店账号体系。
- 不强制所有 Skill 都能在 ask/plan 模式写盘执行（按 mode 降级）。

---

## 文件结构（拟新增 / 修改）

| 路径 | 职责 |
|------|------|
| `bridge/internal/skills/manifest.go` | Skill 包 schema、校验 |
| `bridge/internal/skills/store.go` | 安装目录扫描、CRUD、从 zip/dir/URL 导入 |
| `bridge/internal/skills/runner.go` | 统一执行入口（shell/http/prompt-only） |
| `bridge/internal/skills/adapter.go` | 按 backend 生成 tools / 物化文件 / prompt 片段 |
| `bridge/internal/skills/catalog.go` | 内置/远程插件目录（一键导入源） |
| `bridge/internal/skills/testdata/*` | 样例 Skill 包 |
| `~/.yzj-bridge/skills/<id>/SKILL.yaml` | 用户已安装包（运行时，不提交） |
| `skills-catalog/`（仓库内） | 官方示例包 + catalog.json（可提交） |
| `bridge/internal/controlapi/server.go` | `/v1/skills` API |
| `bridge/internal/orchestrator/orchestrator.go` | Run 前注入 resolved skills |
| `bridge/internal/backends/{openai,cursor,claude,opencode}.go` | 调用 adapter |
| `gui/src/App.tsx` + CSS | 「Skills」页 + 机器人勾选 UI |
| `docs/skills.md` | 包格式与导入说明 |

### Skill 包格式（约定）

```text
~/.yzj-bridge/skills/kdlog/
  SKILL.yaml          # 必填
  SKILL.md            # 可选：长说明，注入 prompt
  scripts/            # 可选：可执行入口
  assets/             # 可选
```

`SKILL.yaml` 示例：

```yaml
id: kdlog
name: 快递100日志查询
version: "1.0.0"
description: 查询 Meta Console / ClickHouse 日志
author: yzj-bridge
tags: [log, kuaidi100]
# 执行方式（MVP 选一种为主）
entry:
  type: shell           # shell | prompt_only | http
  command: "python"
  args: ["scripts/main.py"]
  timeout_sec: 120
# 暴露给模型的 tool schema（OpenAI function / 通用）
tools:
  - name: kdlog_search
    description: 按关键字与时间范围查日志
    parameters:
      type: object
      properties:
        query: { type: string }
        env: { type: string, enum: [dev, test, prod] }
      required: [query]
# 客户端物化提示（Cursor/Claude）
client_sync:
  cursor:
    # 写入 bot workspace 或全局 skills 目录的相对结构
    path: ".cursor/skills/kdlog"
  claude:
    path: ".claude/skills/kdlog"
# 二期
# mcp:
#   command: "npx"
#   args: ["-y", "some-mcp-server"]
```

### 配置扩展

```yaml
# config.yaml
defaults:
  skills_dir: "~/.yzj-bridge/skills"   # 可选覆盖
  skills_catalog_url: ""              # 可选远程 catalog

bots:
  - id: youkai
    backend: claude_code
    skills: [kdlog, consign_log]      # 白名单；空 = 不启用桥管 Skill
```

保留现有 `skills: []` 字段语义，升级为 **已安装 Skill 的 id 列表**（不再是随便写的提示词标签）。

---

## 跨后端适配策略

| Backend | 注入方式 |
|---------|----------|
| **openai** | `adapter.OpenAITools(enabled)` → 并入 `tools`；`tool_call` → `runner.Exec` |
| **cursor_cli** | 将启用 Skill 同步到 `workspace/.cursor/skills/<id>/`（或 Cursor 约定目录）；prompt 追加「可用 skills 与用法」；保留现有 agent 自主调用 |
| **claude_code** | 同步到 `workspace/.claude/skills/<id>/`（或 Claude 约定）；prompt 引导 |
| **opencode** | 与 Cursor 类似：同步到其 skills/插件目录 + prompt；若无标准目录则退化为 prompt + 文档 |

**关键：** Cursor/Claude 不保证 100% 走桥的 `runner`；首期通过「文件物化 + 说明」让客户端 Skill 机制吃到同一份包内容。若某 Skill 的 `entry.type=shell` 且客户端无法直接跑，OpenAI 仍可通过桥 runner 执行；Cursor/Claude 侧在 `SKILL.md` 中写明「请用 Bash 运行 scripts/…」。

二期可选：桥提供本地 `skills://` 或 HTTP tool proxy，供 MCP 化接入。

---

### Task 1: Skill manifest 与校验

**Files:**
- Create: `bridge/internal/skills/manifest.go`
- Create: `bridge/internal/skills/manifest_test.go`
- Create: `bridge/internal/skills/testdata/sample-skill/SKILL.yaml`

- [ ] **Step 1: 写失败测试** — `ParseManifest` 缺 `id` / 非法 `entry.type` 应报错；合法样例通过。
- [ ] **Step 2: 运行测试确认失败**
- [ ] **Step 3: 实现 `Manifest` 结构体与 `LoadDir(path) (*Package, error)`**
- [ ] **Step 4: 测试通过并 commit** — `feat(skills): add skill package manifest parser`

---

### Task 2: Skill Store（扫描 / 安装 / 卸载）

**Files:**
- Create: `bridge/internal/skills/store.go`
- Create: `bridge/internal/skills/store_test.go`
- Modify: `bridge/internal/paths/paths.go`（如需 `SkillsDir()`）

- [ ] **Step 1: 测试** — 空目录 List 为空；CopyInstall 样例包后 List 含 id；Remove 后消失。
- [ ] **Step 2: 实现 `Store{ Root string }`：`List()` `Get(id)` `InstallFromDir` `InstallFromZip` `Uninstall`
- [ ] **Step 3: 安装时拒绝 path traversal；id 与目录名一致**
- [ ] **Step 4: 测试通过并 commit** — `feat(skills): add local skill store install/uninstall`

---

### Task 3: Runner（统一执行）

**Files:**
- Create: `bridge/internal/skills/runner.go`
- Create: `bridge/internal/skills/runner_test.go`

- [ ] **Step 1: 测试** — `prompt_only` 返回 SKILL.md 文本；`shell` 在 temp workspace 跑 echo 成功；越出 workspace 的路径失败。
- [ ] **Step 2: 实现 `Runner.Exec(ctx, pkg, toolName, argsJSON, workspace)`
- [ ] **Step 3: 超时、stdout/stderr 截断（如 256KB）
- [ ] **Step 4: commit** — `feat(skills): add skill runner with workspace sandbox`

---

### Task 4: Backend Adapter

**Files:**
- Create: `bridge/internal/skills/adapter.go`
- Create: `bridge/internal/skills/adapter_test.go`

- [ ] **Step 1: 测试** — 给定 2 个 Skill，`OpenAITools` 生成对应 function schema；`PromptAppendix` 含名称与描述；`SyncToWorkspace` 在 temp dir 写出 cursor/claude 路径。
- [ ] **Step 2: 实现：
  - `Resolve(store, botSkillIDs) ([]*Package, error)`（缺 id 时跳过并记 warn，或严格报错——建议 **严格**：保存配置时已校验）
  - `OpenAITools(pkgs) []ToolSpec`
  - `PromptAppendix(pkgs) string`
  - `Materialize(pkgs, workspace, backend) error`
- [ ] **Step 3: commit** — `feat(skills): add cross-backend skill adapter`

---

### Task 5: 接入 Orchestrator + OpenAI backend

**Files:**
- Modify: `bridge/internal/runtime/runtime.go` — 持有 `*skills.Store`
- Modify: `bridge/internal/orchestrator/orchestrator.go` — Run 前 Resolve + Materialize + 把 tools/appendix 放入 `RunOpts`
- Modify: `bridge/internal/bot/bot.go` — `RunOpts` 增加 `SkillTools` / `SkillPrompt`（或 `ResolvedSkills`）
- Modify: `bridge/internal/backends/openai.go` — 合并 Skill tools；`execTool` 未命中内置时走 `runner`
- Test: `bridge/internal/backends/openai_skills_test.go`（可用 httptest）

- [ ] **Step 1: 扩展 `RunOpts`，orchestrator 注入 resolved skills**
- [ ] **Step 2: OpenAI `buildOATools` + skill tools；未知 tool 名委派 runner**
- [ ] **Step 3: 集成测试：mock chat 返回 tool_call → runner 执行样例 skill**
- [ ] **Step 4: commit** — `feat(skills): wire skills into openai tool loop`

---

### Task 6: 接入 Cursor / Claude / OpenCode

**Files:**
- Modify: `bridge/internal/backends/cursor.go`
- Modify: `bridge/internal/backends/claude.go`
- Modify: `bridge/internal/backends/opencode.go`（若存在）

- [ ] **Step 1: Run 开始时 `Materialize(..., "cursor_cli"|"claude_code")`**
- [ ] **Step 2: 将 `PromptAppendix` 并入 prompt（替换当前仅 id 列表的弱提示）**
- [ ] **Step 3: 手动验收清单写入 `docs/skills.md`（用样例 skill 在真实 agent 上点一次）**
- [ ] **Step 4: commit** — `feat(skills): materialize skills for cursor/claude workspaces`

---

### Task 7: Control API

**Files:**
- Modify: `bridge/internal/controlapi/server.go`
- Create: `bridge/internal/controlapi/skills_handlers.go`
- Test: API 级测试或 `go test` + httptest

API 草案：

| Method | Path | 说明 |
|--------|------|------|
| GET | `/v1/skills` | 已安装列表 |
| GET | `/v1/skills/{id}` | 详情 |
| POST | `/v1/skills/install` | body: `{ source: "zip"|"dir"|"url"|"catalog", path\|url\|catalog_id }` |
| DELETE | `/v1/skills/{id}` | 卸载 |
| GET | `/v1/skills/catalog` | 可一键导入的插件目录 |
| POST | `/v1/skills/validate-bot` | body: `{ skills: [] }` 校验 id 均已安装（供 GUI 保存前） |

- [ ] **Step 1: 实现 handlers + 鉴权复用现有 `auth`**
- [ ] **Step 2: 保存 config 时 ExpandBots/校验：`skills` 引用必须已安装**
- [ ] **Step 3: commit** — `feat(skills): add control API for skill install and catalog`

---

### Task 8: 官方 Catalog + 样例包（一键导入源）

**Files:**
- Create: `skills-catalog/catalog.json`
- Create: `skills-catalog/hello-workspace/SKILL.yaml` + `SKILL.md`（无外网依赖的 hello skill）
- Create: `skills-catalog/README.md`
- Modify: `bridge/internal/skills/catalog.go` — 读内嵌或相对仓库/安装目录的 catalog；支持 `catalog_id` 安装

- [ ] **Step 1: hello-workspace skill：tool `hello_echo`，shell 打印参数**
- [ ] **Step 2: catalog.json 列出 id/name/description/path**
- [ ] **Step 3: `InstallFromCatalog(id)` 从内置路径复制到 skills_dir**
- [ ] **Step 4: commit** — `feat(skills): add bundled skill catalog and hello sample`

---

### Task 9: GUI — Skills 管理页

**Files:**
- Modify: `gui/src/App.tsx` — 新导航「Skills」
- Modify: `gui/src/App.css`
- Modify: `gui/e2e/ui.spec.ts`

功能：
- 列表：已安装 Skill（id/name/version/description）
- 操作：卸载、从本地 zip/文件夹导入（Tauri 选文件/目录）、从 Catalog 一键安装
- 空态与错误用弹窗内悬浮提示（沿用现有 `ModalFloatMessage` 模式）

- [ ] **Step 1: 导航 + 列表拉取 `GET /v1/skills`**
- [ ] **Step 2: Catalog 区 `GET /v1/skills/catalog` + 一键安装按钮**
- [ ] **Step 3: 导入 zip（`@tauri-apps/plugin-dialog` 或现有 opener/能力；若缺权限则先支持填路径）**
- [ ] **Step 4: e2e：进入 Skills 页可见列表区域**
- [ ] **Step 5: commit** — `feat(gui): add Skills management page`

---

### Task 10: GUI — 每机器人勾选 Skills

**Files:**
- Modify: `gui/src/App.tsx` — 机器人详情 / 编辑弹窗
- Modify: `gui/e2e/ui.spec.ts`

- [ ] **Step 1: 机器人详情展示已启用 skills 标签**
- [ ] **Step 2: 编辑/新建弹窗：多选已安装 skills（开关列表）**
- [ ] **Step 3: 保存写入 `bots[].skills`；未安装 id 禁止保存**
- [ ] **Step 4: commit** — `feat(gui): per-bot skill enablement`

---

### Task 11: 文档与默认配置

**Files:**
- Create: `docs/skills.md`
- Modify: `README.md`（链到 skills 文档）
- Modify: `config.default.yaml` — 注释说明 `skills` / `skills_dir`

- [ ] **Step 1: 写清包格式、导入方式、按 bot 配置、各后端行为差异**
- [ ] **Step 2: commit** — `docs(skills): document unified skills format and workflow`

---

### Task 12: 端到端验收

- [ ] **Step 1: `go test ./...`**
- [ ] **Step 2: 安装 catalog `hello-workspace` → 赋给 OpenAI bot → 对话触发 tool → 日志可见执行**
- [ ] **Step 3: 同一 skill 赋给 Cursor/Claude bot → workspace 下出现物化目录**
- [ ] **Step 4: 卸载 skill 后 bot 仍引用 → 保存/reload 应失败并提示**
- [ ] **Step 5: 最终 commit / PR 说明**

---

## 实施顺序建议

```text
Task 1 → 2 → 3 → 4 → 5 → 6
                ↘ 7 → 8 → 9 → 10 → 11 → 12
```

核心链路（1–5）先打通 OpenAI 真执行；再并行客户端物化（6）与 GUI/Catalog（7–10）。

## 风险与缓解

| 风险 | 缓解 |
|------|------|
| Cursor/Claude Skill 目录约定变更 | adapter 路径做成可配置；文档标明版本 |
| 恶意 zip | 安装时限制大小、禁止 `..`、可选只允许 catalog + 手动确认 |
| Skill 与内置 OpenAI tool 重名 | 强制 skill tool 前缀 `skill_<id>_` 或 `id__tool` |
| 物化污染用户仓库 | 默认写到 `~/.yzj-bridge/workspace/<bot>/...`，或 `.gitignore` 提示 |

## 成功标准

1. 同一 Skill 包可安装一次，被多个不同 backend 的机器人勾选使用。  
2. OpenAI 路径：模型 tool_call → 桥 runner 真实执行。  
3. Cursor/Claude 路径：workspace 内可发现对应 Skill 文件，prompt 含完整说明。  
4. Catalog 一键安装与 zip 导入均可在 GUI 完成。  
5. 按机器人独立启用；未安装引用无法保存生效。
