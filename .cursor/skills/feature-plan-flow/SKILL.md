---
name: feature-plan-flow
description: >-
  功能开发门禁工作流：先 commit 未提交修改、切功能分支、Plan 模式出计划、
  subagent 审批计划并输出结果，用户复审通过后再用 subagent 执行。
  仅在用户明确声明使用本 skill / feature-plan-flow / 计划审批执行流时启用。
disable-model-invocation: true
---

# 功能计划审批执行流

用户声明使用本 skill 时，**严格按下列顺序执行**，不得跳步、不得在复审前写业务代码。

## 工作流（必须按序）

1. **先 commit 未提交修改**
2. **切换功能分支**
3. **切到 plan 模式编写计划并输出**
4. **使用 subagent 审批计划，并输出审批结果**
5. **待用户复审通过后拉起 subagent 执行计划**

---

## Step 1 — Commit 未提交修改

1. 并行检查：`git status`、`git diff`（含 staged）、`git log -5 --oneline`。
2. 若工作区干净：向用户说明「无需 commit」，直接 Step 2。
3. 若有改动：
   - 按仓库既有风格写 1–2 句 commit message（说明 why）。
   - `git add` 相关文件后 commit（遵循用户的 git 安全协议：不改 config、不 `--no-verify`、不 amend 除非规则允许）。
   - **不要 push**，除非用户另行要求。
4. Commit 成功后再进入 Step 2。若用户明确要求跳过 commit，记录其决定后再继续。

## Step 2 — 切换功能分支

1. 从用户任务归纳短 slug（英文 kebab-case，如 `chat-openid-select`）。
2. 基线优先 `main`（若当前不在 main：`git fetch` 后以 `origin/main` 或本地 `main` 为准，有冲突先问用户）。
3. 创建并切换：`git checkout -b feat/<slug>`（若分支已存在则 `git checkout feat/<slug>`）。
4. 向用户确认当前分支名。

## Step 3 — Plan 模式编写计划并输出

1. 调用 `SwitchMode`，`target_mode_id: "plan"`，说明为编写功能计划。
2. 调研代码后用 `CreatePlan`（或等价方式）产出可执行计划：目标、关键文件、步骤、风险、验收标准。
3. **把计划正文完整输出给用户**（摘要 + 关键步骤），供下一步审批与复审。
4. 此步结束时：**停止实现**，进入 Step 4。

## Step 4 — Subagent 审批计划

1. 用 `Task` 拉起 **一个** subagent（`subagent_type: generalPurpose`），只做计划评审，不改代码。
2. Prompt 须包含：用户原始需求、完整计划正文、仓库约束（若涉及 GUI 则要求对照 [docs/gui-design.md](../../../docs/gui-design.md)）。
3. 要求 subagent 输出固定结构：

```markdown
## 计划审批

- 结论: 通过 | 有条件通过 | 驳回
- 问题:
  - [严重/建议] …
- 必须修改后才能执行的项:
  - …
- 可执行的前提:
  - …
```

4. 将审批结果**原样或略整理后展示给用户**。
5. **停住**：告知用户请复审计划与审批意见；未获明确通过前禁止 Step 5。

## Step 5 — 用户复审通过后执行

仅当用户明确表示复审通过（如「通过」「批准」「执行计划」「LGTM」「开始实现」）时继续：

1. 若审批为「有条件通过」或用户要求改计划：先改计划并再次确认，再执行。
2. 用 `Task` 拉起 subagent（`generalPurpose`，复杂可再拆）**执行计划**：
   - Prompt 附上终版计划、审批结论、用户补充约束。
   - 要求遵守项目规则与 `docs/gui-design.md`（若改 GUI）。
   - 要求结束后回报：改动文件列表、如何验证、残留风险。
3. 主代理汇总执行结果给用户；需要时可再跑 typecheck / 相关 e2e。
4. **不要**在未要求时 push 或开 PR。

---

## 硬性约束

- 未完成 Step 4 展示前，不得开始业务实现。
- 未收到用户复审通过，不得进入 Step 5。
- Subagent 审批与执行必须分开两次拉起，禁止「边批边改」。
- 用户中途改需求：回到 Step 3 更新计划，并重新 Step 4。

## 触发示例

- 「用 feature-plan-flow 做 xxx」
- 「按计划审批执行流：实现 xxx」
- 「使用这份 skill：…」
