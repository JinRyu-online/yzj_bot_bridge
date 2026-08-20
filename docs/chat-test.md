# GUI 聊天测试

侧栏「聊天」页用于在本机测试已配置的机器人，**不推送云之家**。

会话持久化：`~/.yzj-bridge/chat_sessions.json`（界面消息历史）。

## Agent 上下文隔离

GUI 与云之家走同一套 `Dispatch`，但 **agent 多轮上下文（`sessions.json` + Cursor/Claude `--resume`）按来源隔离**：

| 来源 | session store key | 受 `session_mode` 控制？ |
|------|-------------------|--------------------------|
| GUI 聊天会话 | `gui-chat:{sessionId}` | **否**，每个 GUI 会话独立 |
| 云之家 IM | 按 bot 配置：`per_user` → openId；`shared` → `__shared__`；`oneshot` → 无持久 session | **是** |

因此 `session_mode: shared` 的机器人（如群里的日志 bot）在**云之家通道内**共享上下文、通道内排队；在 **GUI 里每个测试会话仍互不影响**，也不会串到群聊里的对话。

实现：`sessions.ResolveSessionKey` 对 `gui-chat:` 前缀优先返回独立 key（见 `sessions.GUIChatOpenID`）。

## Control API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/chat/sessions` | 会话摘要列表 |
| POST | `/v1/chat/sessions` | `{ bot_id?, title? }` 新建 |
| GET | `/v1/chat/sessions/{id}` | 含消息 |
| PATCH | `/v1/chat/sessions/{id}` | 更新 `bot_id` / `title` |
| DELETE | `/v1/chat/sessions/{id}` | 删除 |
| POST | `/v1/chat/sessions/{id}/messages` | `{ content }` → 同步 `Dispatch`，返回 `reply` + `session` |
| POST | `/v1/chat/sessions/{id}/messages/stream` | 同上，但 **SSE** 流式推送 `reasoning` / `content` / `tool_*` / `done` |

GUI 聊天页走 stream 端点；思考过程在气泡内可折叠展示。

输入约定：顶栏下拉绑定会话默认机器人；消息前缀 `@bot` 仅改本条路由。右上角 `+` 新建、`历史` 切换会话。

## 相关配置

- `bots[].session_mode`：仅影响云之家（及 webhook）入站；GUI 始终 per-session。
- `sessions.json`：存各 key 对应的 Cursor/Claude chat id；GUI key 形如 `gui-chat:0a7c0510-…`。
- 清空 GUI 某会话 agent 上下文：对该会话发 `--clear`（若 bot 启用了 commands），或删除 GUI 会话后新建。
