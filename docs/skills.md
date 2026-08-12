# 统一 Skills

YZJ Bridge 以 `~/.yzj-bridge/skills/<id>/` 为权威 Skill 仓库。各后端共用同一套包：

| 后端 | 行为 |
|------|------|
| OpenAI | 暴露为 `skill_<id>__<tool>` function tools，由桥执行 |
| Cursor CLI | 物化到 `workspace/.cursor/skills/<id>/`，并追加 prompt 说明 |
| Claude Code | 物化到 `workspace/.claude/skills/<id>/`，并追加 prompt 说明 |

## 包格式

```text
SKILL.yaml   # 必填
SKILL.md     # 可选长说明
scripts/     # shell 入口
```

`SKILL.yaml` 关键字段：`id`、`name`、`entry.type`（`shell` | `prompt_only`）、`tools[]`。

Tool 对外名强制为 `skill_<id>__<toolName>`。

## 按机器人启用

```yaml
bots:
  - id: youkai
    skills: [hello-workspace]
```

空列表表示不启用桥管 Skill。保存配置时若引用未安装的 id 会失败。

## 安装方式

Control API（需 Bearer token）：

- `GET /v1/skills` — 已安装
- `GET /v1/skills/catalog` — 可一键导入目录
- `POST /v1/skills/install` — `{ "source": "catalog"|"dir"|"zip", "catalog_id"|"path": "..." }`
- `DELETE /v1/skills/{id}` — 卸载

GUI「Skills」页支持 Catalog 一键安装与本地路径/zip 导入。

仓库内置样例：`skills-catalog/hello-workspace`。

## MCP

manifest 可预留 `mcp` 字段；本版本不接线 MCP 进程。
