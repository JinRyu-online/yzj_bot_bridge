# 桥运行时

进程级行为（单实例、控制口、锁文件）以代码为准，本文只做索引。

| 主题 | 源 |
|------|-----|
| 全局单实例锁 | [`bridge/internal/instancelock`](../bridge/internal/instancelock) |
| 锁路径 | [`paths.InstanceLockPath`](../bridge/internal/paths/paths.go) → `~/.yzj-bridge/runtime/instance.lock`（旁路 `instance.lock.pid`） |
| 启动获取锁 | [`bridge/cmd/yzj-bridge/main.go`](../bridge/cmd/yzj-bridge/main.go) |
| 控制 API 监听失败即退出 | [`controlapi.Server.Start`](../bridge/internal/controlapi/server.go) |

## yzj-cli 轮询入站（inbound_mode: yzj_cli）

| 主题 | 源 |
|------|-----|
| CLI 探测/自动安装 | [yzjcli.ResolveBinary](../bridge/internal/yzjcli/install.go) → 配置 bin → 受控目录 ~/.yzj-bridge/bin/yzj-cli-<ver>.exe → npm 全局 → PATH；缺失且 uto_install 时下载官方 .pak 并校验 SHA256 |
| 消息解析 | [yzjcli.Message](../bridge/internal/yzjcli/message.go)：msgType、param.desc（@/图片）、param.file_id、
eplyOpenId/replyPersonName |
| 轮询/游标 | [yzjcli.Watcher](../bridge/internal/yzjcli/watcher.go)：默认 5s±20% 抖动；
ewest + old 补齐；游标持久化 ~/.yzj-bridge/yzj-poll-state.json；首启 baseline 不回放 |
| 限流退避 | 10000429/请求过于频繁、exit 4 → 30s→1m→2m→5m，5 分钟无再限流后恢复默认节奏 |
| 触发规则 | mention（群聊 @机器人）| direct（个人机器人/私聊全部消息）| any；图片 quote_bot/any/off；机器人自身 BOT- 消息永不触发 |
| 图片下载 | [Dispatcher.prepareAttachments](../bridge/internal/inbound/inbound.go)：yzj-cli file download → workspace/incoming_media/YYYYMMDD/，校验大小/类型/魔数，路径写入 prompt；启动按 keep_days 清理 |
| 控制 API | GET /v1/yzj/cli/status（bin/版本/登录身份）、GET /v1/yzj/cli/groups?keyword=（最近会话/搜索） |
| 登录态 | 由 yzj-cli 管理（yzj-cli auth login），桥不保存 token；xit 3 连续 3 次后暂停轮询并提示重新登录 |
