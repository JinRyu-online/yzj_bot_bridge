# YZJ Bridge（云之家多机器人桥接 · Go + Tauri）

本地常驻 **Go** 服务：通过 WebSocket（可选 Webhook）接收云之家群组机器人消息 → 按 bot 路由到可插拔后端（Cursor CLI / Claude Code / OpenAI 兼容 `/v1` / Opencode 占位）→ 经各 bot 的 `sendMsgUrl` 回发。

控制面板为 **Tauri 2 + React**：托盘常驻、AI 设置、通道启停、运行日志。

## 架构

```text
云之家群组
    │ WSS × N  和/或  HTTP Webhook
    ▼
bridge/ (yzj-bridge)
  Registry → Orchestrator(route→execute→out)
  Backends: cursor_cli | claude_code | openai | opencode
  Control API 127.0.0.1:18765
    ▲
gui/ (Tauri) 托盘 + 设置 UI
```

## 前置条件

1. Go 1.24+、Node 18+、Rust（Tauri）
2. 使用 Cursor / Claude 后端时需本机安装对应 CLI
3. 云之家机器人的 `send_msg_url`（含 `yzjtoken`）

## 构建与运行

```powershell
# 一键构建（产出 dist/YZJBridge/）
.\build_all.ps1

# 仅桥（无 UI）
cd bridge
go build -o bin/yzj-bridge.exe ./cmd/yzj-bridge
.\bin\yzj-bridge.exe

# 开发 GUI（会尝试拉起 bridge/bin/yzj-bridge.exe）
cd gui
npm install
npm run tauri dev
```

Windows 若 `cargo` 报 `link.exe` 参数错误，多半是 PATH 里的 coreutils `link.exe` 抢占了 MSVC 链接器。可改用：

```powershell
rustup default stable-x86_64-pc-windows-gnu
```

配置文件：`~/.yzj-bridge/config.yaml`（首次从仓库 `config.default.yaml` 初始化）。本地真实密钥写在用户目录配置里，**不要**提交 `config.yaml`。

Linux 可用示例 unit：[`deploy/linux/yzj-bridge.service`](deploy/linux/yzj-bridge.service)。

## 后端：OpenAI 兼容自定义模型

```yaml
bots:
  - id: fairy_llm
    name: FairyLLM
    backend: openai
    model: "gpt-4o-mini"
    openai_base_url: "https://api.example.com/v1"
    openai_api_key: ""          # 填真实 Key；勿提交到 Git
    openai_timeout: 120
    openai_max_tool_rounds: 8
    group: default
    send_msg_url: "https://www.yunzhijia.com/gateway/robot/webhook/send?yzjtype=0&yzjtoken=REPLACE_ME"
```

`agent` 模式启用工具：`list_dir` / `read_file` / `write_file` / `grep` / `run_command`（限制在 workspace 内）。`ask`/`plan` 禁用写文件与命令执行。

实现为标准 `POST {base}/chat/completions`（无强制官方 SDK，兼容各类中转）。

## AI 设置中的默认模型

| 字段 | 用途 |
|------|------|
| `cursor_model` | Cursor CLI 默认模型 |
| `claude_model` | Claude Code 默认模型 |
| `openai_model` | OpenAI 兼容默认模型 |

机器人 / 通道上的 `model` 仅作该实体覆盖，不会再把某一引擎的默认值串写到其它引擎。

## 控制 API

- 地址：`127.0.0.1:18765`（可用 `--control-addr`）
- Token：写入 `%TEMP%/yzj-bridge.token`（第一行 token，第二行 addr）
- 主要路由：`/health` `/v1/status` `/v1/wss/start|stop` `/v1/config` `/v1/reload` `/v1/logs` `/v1/shutdown`
- 模型：`GET /v1/backends/cursor/models`、`GET /v1/backends/claude/models`、`POST /v1/backends/openai/probe`

## 配置要点

- `defaults` + `bots` + 可选 `channels` 多群展开（runtime id：`{role}__{group}`）
- `inbound_mode`: `websocket` | `webhook` | `both`
- 会话：`sessions.json` v3；WSS 启停记忆：`wss_enabled.json`
- 参考模板：`config.default.yaml` / `config.example.yaml`（均为占位符，无真实密钥）

## 目录

| 路径 | 说明 |
|------|------|
| `bridge/` | Go 桥接核心 |
| `gui/` | Tauri 控制面板 |
| `config.default.yaml` | 默认配置模板 |
| `config.example.yaml` | 带注释的示例配置 |
| `build_all.ps1` | Windows 一键构建 |
| `deploy/linux/` | systemd 示例 |
