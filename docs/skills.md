# 统一 Skills

YZJ Bridge 采用与 Cursor / Claude 一致的 [Agent Skills](https://cursor.com/docs/skills) 包格式：目录内 **`SKILL.md`**（YAML frontmatter + Markdown 正文）。

权威安装目录：`~/.yzj-bridge/skills/<name>/`（以 frontmatter `name` 为准；导入源目录名可不同，例如 tgz 解压临时目录）。

| 后端 | 行为 |
|------|------|
| OpenAI 兼容 | **渐进披露**（对齐 OpenCode / Agent Skills）：注册单一 `skill` tool，描述里只列 name + description；模型调用 `skill({ "name": "..." })` 后再加载完整 `SKILL.md`。同时物化到 `workspace/.agents/skills/<name>/` |
| Cursor CLI | 物化到 `workspace/.cursor/skills/<name>/`，并将说明注入 prompt |
| Claude Code | 物化到 `workspace/.claude/skills/<name>/`，并将说明注入 prompt |

OpenAI 路径**不会**把全部 Skill 正文塞进 system prompt，避免 token 膨胀；模型按需通过 `skill` tool 拉取。

## 包格式

```text
my-skill/
├── SKILL.md        # 必填
├── scripts/        # 可选（正文中指引 Agent 执行）
├── references/     # 可选
└── assets/         # 可选
```

```markdown
---
name: my-skill
description: Short description of what this skill does and when to use it.
---

# My Skill

Detailed instructions for the agent.
```

Frontmatter 仅使用官方字段：`name`、`description`，以及可选的 `paths`、`disable-model-invocation`、`metadata`。  
**不支持** 独立的 `SKILL.yaml`，也不使用本桥私有字段（如 `entry` / `tools` / `client_sync`）。

## 按机器人启用

```yaml
bots:
  - id: youkai
    skills: [my-skill]
```

空列表表示不启用。保存时若引用未安装的 name 会失败。

## 安装方式

- `GET /v1/skills`
- `POST /v1/skills/install` — `{ "source": "dir"|"zip"|"tgz"|"md"|"auto", "path": "..." }`
- `DELETE /v1/skills/{id}`

GUI「Skills」页：本地目录 / zip / tar.gz / 单文件 md 导入。
