# 发布日志（What's Change）

按版本倒序排列，最新在上。每次发版必须在此追加完整发布日志（见 [`release.md`](release.md) 1.5 节），内容与 GitHub Release notes 保持一致。

---

## v0.3.2（2026-08-21）

### 新功能
- 新增 DSH（DeepSeek Harness）CLI 后端：支持以 DSH 作为机器人推理后端，带共享进程池、会话恢复（resume）与按工作目录隔离会话。
- AI 设置页新增 DSH 配置区：可配置 DSH 入口、默认模型、Node 路径等字段，并自动扫描本机 DSH 安装。
- 设置页支持自动扫描 Cursor / Claude / DSH / Node 的可执行文件路径并自动填充。
- 后端引擎名称带品牌图标（设置页卡片标题 + 机器人后端下拉）。
- 右上角 toast 支持竖向多列排队（最多 3 条，超限挤出最旧）。

### 优化/体验
- 成功提示（「已找到…」「连通成功…」）显示 5 秒后渐隐并释放空间；按结果记忆，切页不再重复弹出。
- 设置页卡片顺序调整，DSH 卡片精简；机器人后端下拉按可用性过滤。
- 进入设置页自动拉取模型（Cursor/Claude/DSH），OpenAI 凭据齐全后自动测试连通。

### 修复
- 修复切页后成功提示重复播放的问题（FadingHint 按 resetKey 记忆，对象/原始值 key 统一处理）。
- 修复设置页用例的自动扫描竞态断言。
- 修复 OpenAI 连通提示在切换页面后再次弹出的问题。

### 其他
- 移除 anthropic_api_key 配置：Claude Code 统一使用本机登录凭据（配置项保留但不再使用）。
- 一键安装 DSH/Node 打开终端命令；打包资源内置 dsh-resources。
- 内部：新增 `docs/dsh-cli-backend-feasibility.md`、`docs/plan-dsh-jsonrpc-backend.md` 等设计与实测文档。

---

## v0.3.1（2026-08-21）

（此前版本的发布日志未按本规范记录，补记占位。）

- 新增版本更新功能：客户端「检查更新」弹窗、一键下载安装包并升级。
