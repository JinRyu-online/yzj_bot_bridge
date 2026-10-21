# YZJ Bridge（云之家多机器人桥接 · Go + Tauri）

本地常驻 **Go** 服务：接收云之家群组消息（机器人 WebSocket / Webhook，**或**用 yzj-cli 轮询你本人可见的已配置群）→ 按 bot 路由到可插拔后端（Cursor CLI / Claude Code / OpenAI 兼容 `/v1` / DSH（DeepSeek Harness）JSON-RPC / Opencode 占位）→ 经各 bot 的 `sendMsgUrl` 回发。

控制面板为 **Tauri 2 + React**：托盘常驻、AI 设置、通道启停、运行日志。

## 架构

```text
云之家群组
    │ WSS × N  和/或  HTTP Webhook   （机器人入站：只收到 @机器人 的文本）
    │ yzj-cli im message list 轮询    （用户入站：仅配置群，支持图片/引用机器人）
    ▼
bridge/ (yzj-bridge)
  Registry → Orchestrator(route→execute→out)
  Backends: cursor_cli | claude_code | openai | dsh | opencode
  Control API 127.0.0.1:18765
    ▲
gui/ (Tauri) 托盘 + 设置 UI
```

## 前置条件

1. Go 1.24+、Node 18+、Rust（Tauri）
2. 使用 Cursor / Claude 后端时需本机安装对应 CLI
3. 云之家机器人的 `send_msg_url`（含 `yzjtoken`）；`inbound_mode: yzj_cli` 的通道仍用它回发
4. 使用 yzj-cli 轮询入站时：本机安装 `yzj-cli`（`npm i -g @yunzhijia/cli`，缺失时桥会自动安装 0.1.8）并完成 `yzj-cli auth login`；桥不保存云之家凭据

## 构建与运行

```powershell
# 一键构建：NSIS 安装包 + 便携目录
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

产物：

| 文件 | 用途 |
|------|------|
| `dist/YZJBridge-<version>-Windows-x64-setup.exe` | **给别人用的安装包**（开始菜单 + 可选桌面快捷方式） |
| `dist/YZJBridge-Windows-x64-setup.exe` | 同上，不带版本号的副本 |
| `dist/YZJBridge/` | 便携目录（必须整夹分发：exe + `WebView2Loader.dll` + `yzj-bridge.exe`） |

安装包会写入当前用户目录、创建「YZJ Bridge」快捷方式；若系统没有 WebView2，安装器会静默补装运行时。不要只发单个 `YZJBridge.exe`。

Windows 若 `cargo` 报 `link.exe` 参数错误，多半是 PATH 里的 coreutils `link.exe` 抢占了 MSVC 链接器。可改用：

```powershell
rustup default stable-x86_64-pc-windows-gnu
```

## GitHub Release

1. 把 `gui/src-tauri/tauri.conf.json` 里的 `version` 改成新版本（例如 `0.2.1`）。
2. 提交后打 tag 并推送：

```powershell
git tag v0.2.1
git push origin v0.2.1
```

3. GitHub Actions（`.github/workflows/release-windows.yml`）会构建 NSIS 安装包，并挂到该 tag 的 Release 上，资产名为 `YZJBridge-<version>-Windows-x64-setup.exe`。

本地打好包后也可以手动发版：

```powershell
.\build_all.ps1
gh release create v0.2.1 dist/YZJBridge-0.2.1-Windows-x64-setup.exe --title "YZJ Bridge v0.2.1" --generate-notes
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
    openai_compact: true            # 多轮自动摘要；仅 openai。阈值按 DeepSeek V4 Flash 0731 早压上下文
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
- 主要路由：`/health` `/v1/status` `/v1/wss/start|stop` `/v1/config` `/v1/reload` `/v1/logs` `/v1/shutdown` `/v1/skills*`
- yzj-cli：`GET /v1/yzj/cli/status`（bin/版本/登录身份/到期时间）、`GET /v1/yzj/cli/groups?keyword=`（选择群，只读）
- 模型：`GET /v1/backends/cursor/models`、`GET /v1/backends/claude/models`、`POST /v1/backends/openai/probe`
- Skills：见 [docs/skills.md](docs/skills.md)（官方 `SKILL.md` 格式）

## 配置要点

- `defaults` + `bots` + 可选 `channels` 多群展开（runtime id：`{role}__{group}`）
- **yzj-cli 轮询入站**（`inbound_mode: yzj_cli`）：只轮询 `yzj_group_id` 显式配置的群（不会扫全部会话）；`yzj_group_name` 仅用于展示/选择群；默认 5s ±20% 抖动；首次启动只建游标不回放（`startup_mode: baseline`）。文本需 @机器人 触发；纯图片默认仅「引用机器人消息」触发（`yzj_cli.image_trigger: quote_bot|any|off`）
- yzj-cli 配置在 `defaults.yzj_cli`：`bin/version/profile/config_dir/auto_install/timeout_sec/poll_interval_sec/catchup_pages/startup_mode/image_trigger/image.*`；桥不保存凭据，登录态由 `yzj-cli auth login` 管理
- 机器人相关字段：`bots[].yzj_group_id`、`yzj_group_name`、`yzj_profile`、`yzj_bot_names`（@/引用识别别名）、`yzj_bot_open_id`（可选，精确识别机器人）
- `bots[].skills`：已安装 Skill 的 id 白名单（跨 OpenAI / Cursor / Claude）
- `inbound_mode`: `websocket` | `webhook` | `both` | `yzj_cli`（轮询，仅配置群）
- 会话：`session_mode` 为 `shared`（默认，同通道共享上下文且通道内排队）| `per_user`（按人隔离）| `oneshot`
- `cursor_daily_reset`（默认 true）：Cursor CLI 按本机自然日懒重置 `--resume` 会话；字段与判断见 [`sessions.NeedsDailyReset`](bridge/internal/sessions/sessions.go) / [`Cursor.Run`](bridge/internal/backends/cursor.go)
- **工作目录**：仅 `bots[].workspace`；留空自动补全为 `~/.yzj-bridge/workspace/{roleID}`；旧版 `defaults.workspace` / `cursor_workspace` / `claude_workspace` 仍可读作兼容
- **GUI 聊天测试**（侧栏「聊天」页）始终按 `gui-chat:{sessionId}` 独立 agent 上下文，**不受** `session_mode` 影响，与云之家群聊隔离；详见 [docs/chat-test.md](docs/chat-test.md)
- `job_queue`：空则 `shared` 用通道排队、其它用按人排队；也可显式 `user` / `channel`。通道排队时后来者会收到「排在第 N 位」，每次答完更新剩余排位
- 会话文件：`sessions.json` v3；WSS 启停记忆：`wss_enabled.json`
- 运行日志：`~/.yzj-bridge/logs/runtime-YYYY-MM-DD.jsonl`（按日追加；GUI 仍只展示进程内最近 2000 条）
- 参考模板：`config.default.yaml`（占位符，无真实密钥；首次启动会复制到用户目录）

## 目录

| 路径 | 说明 |
|------|------|
| `bridge/` | Go 桥接核心 |
| `gui/` | Tauri 控制面板 |
| `docs/skills.md` | 统一 Skills 格式与 API |
| `config.default.yaml` | 默认配置模板（首次初始化用） |
| `build_all.ps1` | Windows 一键构建安装包 |
| `scripts/` | 打包辅助：sidecar 准备、收集 dist |
| `.github/workflows/release-windows.yml` | tag `v*` 时构建并发布 GitHub Release |
| `deploy/linux/` | systemd 示例 |
