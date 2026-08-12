# Skills 验收清单

## 自动化

```powershell
cd bridge
go test ./... -count=1

cd ../gui
npx tsc --noEmit
# 可选：npm run test:e2e（需本地 Playwright 环境）
```

## 手工

1. 准备一份官方格式 Skill 目录（含 `SKILL.md`，`name` 与目录名一致）。
2. 启动 GUI → **Skills** 页 → 从本地导入该目录（或 zip）。
3. 编辑/新建机器人 → 勾选该 Skill → 保存成功。
4. **OpenAI 机器人**：对话可触发 `skill` tool（参数 `name`），返回完整 `SKILL.md` 正文；workspace 下出现 `.agents/skills/<name>/`。
5. **Cursor/Claude 机器人**：查看 bot workspace 下 `.cursor/skills/<name>/SKILL.md` 或 `.claude/skills/<name>/SKILL.md`。
6. 配置里引用未安装 skill name 后保存 → 应返回明确错误。
7. Skills 页卸载成功。
