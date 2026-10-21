# 发布日志（What's Change）

按版本倒序排列，最新在上。每次发版必须在此追加完整发布日志（见 [`release.md`](release.md) 1.5 节），内容与 GitHub Release notes 保持一致。

---

## Unreleased（yzj-cli 轮询入站 + DSH 桌面客户端 runtime）

### DSH 后端改为使用 DSH 桌面客户端的 headless CLI
- `dsh_mode: headless`（默认）：直接执行本机 DSH 桌面客户端自带的 `dsh --profile headless`（一条消息一个短进程，stdin 传任务、stdout 收 NDJSON），用 `--session-id` 续会话；不再要求单独安装 node / dsh CLI，也不是之前的常驻 JSON-RPC server。
- 桥自动生成并维护 `~/.dsh/profiles/yzj-headless`：复制桌面客户端 `profiles/desktop/cordis.patch.yml` 里的模型 provider 配置，重新启用 headless bundle 默认关闭的 credentials，让 headless 能读取桌面写入的 `~/.dsh/.credentials.yaml`。
- 每轮按 `dsh_provider` / 模型生成 `--patch` 覆盖文件，固定本轮 provider/model；工作目录通过进程 cwd 生效。
- 保留两种可选模式：`dsh_mode: sdk`（常驻官方 `@deepseek-ai/dsh-sdk-jsonrpc-server` 进程池）与 `dsh_mode: cli`（旧独立 node + dsh 入口）。
- GUI「AI 设置 → DSH」新增运行方式（headless/sdk/cli）与桌面客户端路径；desktop 类模式隐藏 CLI 入口/Node 字段。


