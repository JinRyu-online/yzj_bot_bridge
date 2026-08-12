# GUI 聊天测试

侧栏「聊天」页用于在本机测试已配置的机器人，**不推送云之家**。

会话持久化：`~/.yzj-bridge/chat_sessions.json`。

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
