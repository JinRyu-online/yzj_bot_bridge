# 桥运行时

进程级行为（单实例、控制口、锁文件）以代码为准，本文只做索引。

| 主题 | 源 |
|------|-----|
| 全局单实例锁 | [`bridge/internal/instancelock`](../bridge/internal/instancelock) |
| 锁路径 | [`paths.InstanceLockPath`](../bridge/internal/paths/paths.go) → `~/.yzj-bridge/runtime/instance.lock`（旁路 `instance.lock.pid`） |
| 启动获取锁 | [`bridge/cmd/yzj-bridge/main.go`](../bridge/cmd/yzj-bridge/main.go) |
| 控制 API 监听失败即退出 | [`controlapi.Server.Start`](../bridge/internal/controlapi/server.go) |
