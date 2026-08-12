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

1. 启动 `npm run tauri dev`（或已连接桥的 GUI）。
2. 打开 **Skills** 页 → Catalog 中安装 `hello-workspace`。
3. 编辑/新建机器人 → 勾选 `hello-workspace` → 保存成功。
4. **OpenAI 机器人**：对话触发 tool `skill_hello-workspace__hello_echo`，日志可见 python 回显。
5. **Cursor/Claude 机器人**：查看该 bot workspace 下 `.cursor/skills/hello-workspace` 或 `.claude/skills/hello-workspace` 是否出现 `SKILL.yaml`。
6. 配置里引用未安装 skill id 后保存 → 应返回明确错误。
7. Skills 页卸载 `hello-workspace` 成功。
