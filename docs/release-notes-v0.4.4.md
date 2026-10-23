# YZJ Bridge v0.4.4

## 修复

- **「关闭思考」真正生效**：kuaidi100 网关改用 DeepSeek 思考协议，选 `off` 时会显式发送 `thinking:{"type":"disabled"}`；此前只是"不发送 reasoning 参数"，网关仍按默认思考，所以看起来关不掉。
- 其他 provider 不受影响；如遇同类网关可显式配置 `dsh_thinking_format`（`openai` / `deepseek` / `qwen` 等）。

## 优化

- **思考过程实时输出**：DSH headless 运行中逐条写日志（时间戳为真实产生时间），不再等任务结束后一次性刷出；长任务可以实时看到思考进度，方便判断是否卡住或超时。

## 新增配置

- `defaults.dsh_thinking_format`：思考开关协议。留空自动推断（`kuaidi100` → `deepseek`，其余 `openai`），一般无需修改。

## 安装

下载并覆盖安装 `YZJBridge-0.4.4-Windows-x64-setup.exe`，重启桥服务。
