# YZJ Bridge v0.4.2

## 新功能

- **会话空闲重置**：新增 `session_idle_reset_hours`（默认 `3`，`0` 关闭）。同一个机器人/会话超过该小时数没有请求，下一条消息自动清空旧会话并新开一个，避免长期闲置后上下文串味。
- 该配置支持 Cursor CLI、Claude Code、DSH（headless/sdk）等持久会话后端；可在「机器人 → 编辑 → 空闲重置（小时）」里按机器人单独设置。
- `session_mode=oneshot` 本来每条消息都会新会话；`shared` / `per_user` 下按各自的会话 key 分别计时。

## 说明

- 旧版本会话文件没有 `last_used_at`，升级后不会立刻被判定为空闲；第一次请求会补上时间戳，之后开始计时。
- 空闲重置只影响会话上下文，不影响 workspace、项目绑定或机器人配置。

## 安装

下载并覆盖安装 `YZJBridge-0.4.2-Windows-x64-setup.exe`，重启桥服务。
